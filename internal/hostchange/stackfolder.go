package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// ActionFolderSetupStacks makes the stacks below a folder readable for the service: the folder and every stack
// folder belong to root and the group sysmin, the compose files are read-only for the group, and the env files
// are writable for it. Its argument is built by StackFolderArgs.
const ActionFolderSetupStacks = "folder.setup-stacks"

// The permissions this action sets. Nothing is inherited: no setgid bit, no default ACL, no recursion.
const (
	modeStackFolder uint32 = 0o750
	modeComposeFile uint32 = 0o640
	modeEnvFile     uint32 = 0o660
)

// ComposeFileNames are the file names that make a folder a stack.
var ComposeFileNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// Roles of the entries in a StackFolderChange.
const (
	RoleFolder      = "folder"
	RoleStackFolder = "stack folder"
	RoleComposeFile = "compose file"
	RoleEnvFile     = "env file"
)

// planTimeout bounds the reads of a plan, which has no context of its own.
const planTimeout = 30 * time.Second

// StackFolderArgs builds the arguments of ActionFolderSetupStacks.
func StackFolderArgs(path string) []string { return []string{path} }

// EntryAccess is owner, group and permission bits of one entry, as names where the host knows them.
type EntryAccess struct {
	Owner string
	Group string
	Mode  uint32
}

// MarshalJSON writes {"owner","group","mode"} with the mode as four octal digits, for example "0750".
func (a EntryAccess) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Owner string `json:"owner"`
		Group string `json:"group"`
		Mode  string `json:"mode"`
	}{a.Owner, a.Group, fmt.Sprintf("%04o", a.Mode)})
}

func (a EntryAccess) String() string { return fmt.Sprintf("%s:%s %04o", a.Owner, a.Group, a.Mode) }

// StackFolderChange is one entry that is changed.
type StackFolderChange struct {
	Path   string      `json:"path"`
	Role   string      `json:"role"`
	Before EntryAccess `json:"before"`
	After  EntryAccess `json:"after"`
}

// StackFolderKept is an entry that stays as it is, with the reason in plain words.
type StackFolderKept struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// StackFolderStack is one stack found below the folder.
type StackFolderStack struct {
	// Name is the name of the stack folder.
	Name string `json:"name"`
	Path string `json:"path"`
	// Running says a running container belongs to the stack.
	Running bool                `json:"running"`
	Changes []StackFolderChange `json:"changes"`
	Kept    []StackFolderKept   `json:"kept"`
}

// StackFolderPreview is what the confirmation shows before anything is changed. It is also the JSON of the
// "preview" field of "host preview"; the json tags are its stable field names.
type StackFolderPreview struct {
	Path string `json:"path"`
	// Folder is the change of the folder itself; nil when it is set up already.
	Folder *StackFolderChange `json:"folder"`
	Stacks []StackFolderStack `json:"stacks"`
	// Kept are entries of the folder itself that stay, such as a link to a stack folder.
	Kept []StackFolderKept `json:"kept"`
	// OthersLoseAccess is always true: afterwards only root and the group sysmin reach the folders and files.
	OthersLoseAccess bool `json:"others_lose_access"`
	// NothingToDo says everything is set up already.
	NothingToDo bool `json:"nothing_to_do"`
}

// PreviewStackFolder finds the stacks of the folder and says what the setup changes and what it leaves.
// A refusal comes back as a *RefusedError.
func (h FolderHost) PreviewStackFolder(ctx context.Context, path string) (StackFolderPreview, error) {
	w, err := h.planStackFolder(ctx, path)
	if err != nil {
		return StackFolderPreview{}, err
	}
	p := w.preview
	if p.Kept == nil {
		p.Kept = []StackFolderKept{}
	}
	for i := range p.Stacks {
		if p.Stacks[i].Changes == nil {
			p.Stacks[i].Changes = []StackFolderChange{}
		}
		if p.Stacks[i].Kept == nil {
			p.Stacks[i].Kept = []StackFolderKept{}
		}
	}
	return p, nil
}

func (f folderActions) previewSetupStacks(ctx context.Context, args []string) (any, error) {
	if len(args) != 1 {
		return nil, Argf("%s needs exactly one folder", ActionFolderSetupStacks)
	}
	p, err := f.h.PreviewStackFolder(ctx, args[0])
	if err != nil {
		return nil, err
	}
	return p, nil
}

// stackFiles are the files the service must reach in one stack.
type stackFiles struct {
	name    string
	compose []string
	env     []string // managed env files only
}

type stackFolderWork struct {
	preview  StackFolderPreview
	entries  []folderEntry // the entries that change, as they are now
	commands []Command
	stacks   []stackFiles
}

type stackTarget struct {
	entry folderEntry
	role  string
	mode  uint32
}

func (h FolderHost) serviceGroupGID() (int, error) {
	g, err := h.Accounts.Group(control.ServiceGroup)
	if err != nil {
		return 0, Refusef("there is no local group %s; create it before the stack folder is set up", control.ServiceGroup)
	}
	return g.GID, nil
}

// namer resolves IDs to names for the preview; an unknown ID stays a number.
func (h FolderHost) namer() (user, group func(id int) string) {
	users, groups := map[int]string{}, map[int]string{}
	if us, err := h.Accounts.Users(); err == nil {
		for _, u := range us {
			if _, dup := users[u.UID]; !dup {
				users[u.UID] = u.Name
			}
		}
	}
	if gs, err := h.Accounts.Groups(); err == nil {
		for _, g := range gs {
			if _, dup := groups[g.GID]; !dup {
				groups[g.GID] = g.Name
			}
		}
	}
	pick := func(m map[int]string) func(int) string {
		return func(id int) string {
			if n, ok := m[id]; ok {
				return n
			}
			return strconv.Itoa(id)
		}
	}
	return pick(users), pick(groups)
}

func (h FolderHost) planStackFolder(ctx context.Context, path string) (*stackFolderWork, error) {
	if err := checkFolderPath(path); err != nil {
		return nil, err
	}
	if IsSystemPath(path) {
		return nil, Refusef("%s is a system path; a stack folder there is not set up", path)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, Refusef("%s does not exist", path)
		}
		return nil, err
	}
	if real != path {
		return nil, Refusef("%s is or lies behind a link to %s; give the real folder instead", path, real)
	}
	gid, err := h.serviceGroupGID()
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Lstat(path)
	if err != nil {
		return nil, Refusef("%s cannot be read: %v", path, err)
	}
	rootEntry, err := entryOf(path, rootInfo)
	if err != nil {
		return nil, err
	}
	if rootEntry.Kind != "dir" {
		return nil, Refusef("%s is not a folder", path)
	}
	if rootEntry.Mode&0o7000 != 0 {
		return nil, Refusef("%s has a special bit (setuid, setgid or sticky) that this change would drop; special bits stay unchanged", path)
	}
	dirents, err := os.ReadDir(path)
	if err != nil {
		return nil, Refusef("%s cannot be read: %v", path, err)
	}

	running, details, err := h.Stacks.listRunning(ctx)
	if err != nil {
		return nil, Refusef("the running stacks cannot be checked, so nothing was changed: %v", err)
	}
	// Files that a container mounts directly are never changed: the mount keeps its own view of the file.
	mounted := map[string]bool{}
	for _, c := range details {
		for _, m := range c.Mounts {
			if m.Type == "bind" && filepath.IsAbs(m.Source) {
				mounted[filepath.Clean(m.Source)] = true
			}
		}
	}

	userName, groupName := h.namer()
	access := func(e folderEntry) EntryAccess {
		return EntryAccess{Owner: userName(e.UID), Group: groupName(e.GID), Mode: e.Mode}
	}
	serviceAccess := func(mode uint32) EntryAccess {
		return EntryAccess{Owner: "root", Group: control.ServiceGroup, Mode: mode}
	}
	set := func(e folderEntry, mode uint32) bool { return e.UID == h.rootUID && e.GID == gid && e.Mode == mode }

	w := &stackFolderWork{preview: StackFolderPreview{Path: path, OthersLoseAccess: true}}
	var targets []stackTarget
	total := 1
	add := func(e folderEntry, role string, mode uint32) (*StackFolderChange, *StackFolderKept) {
		if set(e, mode) {
			return nil, &StackFolderKept{e.Path, fmt.Sprintf("already %s", serviceAccess(mode))}
		}
		targets = append(targets, stackTarget{e, role, mode})
		return &StackFolderChange{Path: e.Path, Role: role, Before: access(e), After: serviceAccess(mode)}, nil
	}
	if ch, _ := add(rootEntry, RoleFolder, modeStackFolder); ch != nil {
		w.preview.Folder = ch
	}

	for _, d := range dirents {
		dir := filepath.Join(path, d.Name())
		info, err := os.Lstat(dir)
		if err != nil {
			return nil, Refusef("%s cannot be read: %v", dir, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			if target, err := os.Stat(dir); err == nil && target.IsDir() && len(composeCandidates(dir)) > 0 {
				w.preview.Kept = append(w.preview.Kept, StackFolderKept{dir, "is a link; links are not followed and stay unchanged"})
			}
			continue
		}
		if !info.IsDir() {
			continue
		}
		composes := composeCandidates(dir)
		if len(composes) == 0 {
			continue
		}
		dirEntry, err := entryOf(dir, info)
		if err != nil {
			return nil, err
		}
		switch {
		case dirEntry.dev != rootEntry.dev:
			w.preview.Kept = append(w.preview.Kept, StackFolderKept{dir, "lies on another file system or mount and stays unchanged"})
			continue
		case dirEntry.Mode&0o7000 != 0:
			return nil, Refusef("%s has a special bit (setuid, setgid or sticky) that this change would drop; special bits stay unchanged", dir)
		}

		st := StackFolderStack{Name: d.Name(), Path: dir}
		files := stackFiles{name: d.Name()}
		fileEntry := func(p string) (folderEntry, string) {
			info, err := os.Lstat(p)
			if err != nil {
				return folderEntry{}, "cannot be read: " + err.Error()
			}
			e, err := entryOf(p, info)
			switch {
			case err != nil:
				return folderEntry{}, err.Error()
			case e.Kind == "link":
				return folderEntry{}, "is a link; links are not followed and stay unchanged"
			case e.Kind != "file":
				return folderEntry{}, "is not a regular file"
			case e.dev != rootEntry.dev:
				return folderEntry{}, "lies on another file system or mount"
			case e.Mode&0o7000 != 0:
				return folderEntry{}, "has a special bit (setuid, setgid or sticky) that a change would drop"
			}
			if r, err := filepath.EvalSymlinks(p); err != nil || r != p {
				return folderEntry{}, "lies behind a link"
			}
			return e, ""
		}
		manage := func(p, role string, mode uint32) bool {
			e, why := fileEntry(p)
			if why == "" && (mounted[p]) {
				why = "is mounted into a container and stays unchanged"
			}
			if why != "" {
				st.Kept = append(st.Kept, StackFolderKept{p, why})
				return false
			}
			ch, kept := add(e, role, mode)
			if ch != nil {
				st.Changes = append(st.Changes, *ch)
			} else {
				st.Kept = append(st.Kept, *kept)
			}
			return true
		}

		if ch, kept := add(dirEntry, RoleStackFolder, modeStackFolder); ch != nil {
			st.Changes = append(st.Changes, *ch)
		} else {
			st.Kept = append(st.Kept, *kept)
		}
		for _, c := range composes {
			manage(c, RoleComposeFile, modeComposeFile)
			files.compose = append(files.compose, c)
		}
		envs := []string{filepath.Join(dir, ".env")}
		for _, r := range running {
			if filepath.Clean(r.Dir) != dir {
				continue
			}
			st.Running = true
			for _, id := range r.ContainerIDs {
				for _, f := range splitPaths(details[id].EnvFiles) {
					if lexicallyUnder(dir, f) {
						envs = appendOnce(envs, f)
					} else {
						st.Kept = append(st.Kept, StackFolderKept{f, "is an env file outside the stack folder and stays unchanged"})
					}
				}
			}
		}
		for _, p := range envs {
			if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if manage(p, RoleEnvFile, modeEnvFile) {
				files.env = append(files.env, p)
			}
		}
		if total += 1 + len(composes) + len(envs); total > MaxFolderEntries {
			return nil, Refusef("%s has more than %d entries to set up; use a smaller folder", path, MaxFolderEntries)
		}
		w.preview.Stacks = append(w.preview.Stacks, st)
		w.stacks = append(w.stacks, files)
	}
	if len(w.preview.Stacks) == 0 {
		return nil, Refusef("%s holds no stack: no direct subfolder has a compose file (%s)", path, strings.Join(ComposeFileNames, ", "))
	}

	var chownPaths []string
	chmodPaths := map[uint32][]string{}
	for _, t := range targets {
		w.entries = append(w.entries, t.entry)
		if t.entry.UID != h.rootUID || t.entry.GID != gid {
			chownPaths = append(chownPaths, t.entry.Path)
		}
		if t.entry.Mode != t.mode {
			chmodPaths[t.mode] = append(chmodPaths[t.mode], t.entry.Path)
		}
	}
	sort.Strings(chownPaths)
	for _, chunk := range chunkPaths(chownPaths) {
		w.commands = append(w.commands, Command{Argv: append([]string{"chown", "-h", "--", "root:" + control.ServiceGroup}, chunk...)})
	}
	for _, m := range []uint32{modeStackFolder, modeComposeFile, modeEnvFile} {
		sort.Strings(chmodPaths[m])
		for _, chunk := range chunkPaths(chmodPaths[m]) {
			w.commands = append(w.commands, Command{Argv: append([]string{"chmod", "--", fmt.Sprintf("%04o", m)}, chunk...)})
		}
	}
	w.preview.NothingToDo = len(w.commands) == 0
	return w, nil
}

// composeCandidates are the compose files of a stack folder: regular files and links, so that a link is
// found and then reported rather than overlooked.
func composeCandidates(dir string) []string {
	var out []string
	for _, n := range ComposeFileNames {
		p := filepath.Join(dir, n)
		if info, err := os.Lstat(p); err == nil && (info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0) {
			out = append(out, p)
		}
	}
	return out
}

type stackFolderState struct {
	Path    string        `json:"path"`
	Entries []folderEntry `json:"entries"`
}

func (f folderActions) planSetupStacks(args []string, _ []byte) (Plan, error) {
	if len(args) != 1 {
		return Plan{}, Argf("%s needs exactly one folder", ActionFolderSetupStacks)
	}
	path := args[0]
	if err := checkFolderPath(path); err != nil {
		return Plan{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), planTimeout)
	defer cancel()
	planned, err := f.h.planStackFolder(ctx, path)
	if err != nil {
		return Plan{}, err
	}
	if planned.preview.NothingToDo {
		return Plan{}, Refusef("nothing to change: %s and its %d stacks are set up already", path, len(planned.preview.Stacks))
	}
	names := make([]string, len(planned.preview.Stacks))
	for i, s := range planned.preview.Stacks {
		names[i] = s.Name
	}
	sentence := fmt.Sprintf("Set up the stack folder %s for the service (%d stacks: %s): the folder and the stack folders become root:%s %04o, "+
		"compose files %04o, env files %04o. Other users except root have no access to them afterwards.",
		path, len(names), strings.Join(names, ", "), control.ServiceGroup, modeStackFolder, modeComposeFile, modeEnvFile)

	var work *stackFolderWork
	var affected []AffectedStack
	return Plan{
		Sentence:     sentence,
		Commands:     planned.commands,
		PartialSteps: true,
		Capture: func(ctx context.Context) (json.RawMessage, error) {
			now, err := f.h.planStackFolder(ctx, path)
			if err != nil {
				return nil, err
			}
			if strings.Join(displays(now.commands), "\n") != strings.Join(displays(planned.commands), "\n") {
				return nil, Refusef("%s changed since the setup was planned; nothing was changed, run it again", path)
			}
			if affected, err = f.h.Stacks.Affected(ctx, path); err != nil {
				return nil, Refusef("the running stacks cannot be checked, so nothing was changed: %v", err)
			}
			work = now
			return json.Marshal(stackFolderState{Path: path, Entries: now.entries})
		},
		Check: func(ctx context.Context) error {
			if err := specialBitsUnchanged(work.entries, lstatEntry); err != nil {
				return err
			}
			if err := f.h.Stacks.serviceReaches(work.stacks); err != nil {
				return err
			}
			return f.h.Stacks.Watch(ctx, affected)
		},
	}, nil
}

// serviceReaches asks, as the service user, whether every compose file can be read and every managed env
// file can be read and written in place.
func (s StackCheck) serviceReaches(stacks []stackFiles) error {
	canRead, canWrite := s.CanRead, s.CanWrite
	if canRead == nil {
		canRead = canReadAsServiceUser
	}
	if canWrite == nil {
		canWrite = canWriteAsServiceUser
	}
	for _, st := range stacks {
		for _, p := range append(append([]string{}, st.compose...), st.env...) {
			if err := canRead(p); err != nil {
				return &StackFinding{Stack: st.name, Finding: "the service cannot read " + p + ": " + err.Error()}
			}
		}
		for _, p := range st.env {
			if err := canWrite(p); err != nil {
				return &StackFinding{Stack: st.name, Finding: "the service cannot write " + p + ": " + err.Error()}
			}
		}
	}
	return nil
}

func (f folderActions) inverseSetupStacks(prev json.RawMessage) (Plan, error) {
	var st stackFolderState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := checkFolderPath(st.Path); err != nil || len(st.Entries) == 0 {
		return Plan{}, errors.New("the recorded state of the stack folder is not usable")
	}
	for _, e := range st.Entries {
		if e.Kind != "dir" && e.Kind != "file" {
			return Plan{}, fmt.Errorf("the recorded state of %q is not usable", e.Path)
		}
	}
	cmds, err := restoreCommands(st.Path, st.Entries)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Sentence: fmt.Sprintf("Take back the setup of the stack folder %s: restore the previous owner, group and permissions of %d entries.",
			st.Path, len(st.Entries)),
		Commands: cmds,
	}, nil
}
