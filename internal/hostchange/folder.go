package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// ActionFolderSetPermissions changes owner, group and the three permission levels of a folder. Its arguments
// are built by FolderArgs.
const ActionFolderSetPermissions = "folder.set-permissions"

// MaxFolderEntries bounds how many entries one folder change may touch, because the journal keeps the
// previous state of every one of them.
const MaxFolderEntries = 5000

// ErrTooManyEntries marks a folder with more entries than MaxFolderEntries.
var ErrTooManyEntries = errors.New("too many entries below the folder")

// Words of the permission levels in the arguments.
const (
	levelNothing   = "nothing"
	levelRead      = "read"
	levelReadWrite = "read-write"

	optionRecursive  = "recursive"
	optionSystemPath = "system-path"
)

// FolderRequest is one folder change as the interface knows it.
type FolderRequest struct {
	// Path is the real path of the folder: absolute, clean and without a link on the way.
	Path         string
	Owner, Group string
	// User, Group and Other are the levels of the three positions: nothing, read, or read and write.
	UserAccess, GroupAccess, OtherAccess hostadmin.Access
	// Recursive applies the change to everything below the folder as well.
	Recursive bool
	// SystemPath acknowledges a system path; the root side refuses such a path without it.
	SystemPath bool
}

// FolderArgs builds the arguments of ActionFolderSetPermissions.
func FolderArgs(r FolderRequest) []string {
	args := []string{r.Path, r.Owner, r.Group, levelWord(r.UserAccess), levelWord(r.GroupAccess), levelWord(r.OtherAccess)}
	if r.Recursive {
		args = append(args, optionRecursive)
	}
	if r.SystemPath {
		args = append(args, optionSystemPath)
	}
	return args
}

func levelWord(a hostadmin.Access) string {
	switch a {
	case hostadmin.AccessRead:
		return levelRead
	case hostadmin.AccessReadWrite:
		return levelReadWrite
	}
	return levelNothing
}

// symbolic is the permission part of one position: read, with traverse for folders, and write.
func symbolic(word string) (string, bool) {
	switch word {
	case levelNothing:
		return "", true
	case levelRead:
		return "rX", true
	case levelReadWrite:
		return "rwX", true
	}
	return "", false
}

func accessWords(word string) string {
	switch word {
	case levelRead:
		return "read"
	case levelReadWrite:
		return "read and write"
	}
	return "no access"
}

// systemRoots are the system paths. The root directory counts only for itself, as in the folder model of
// hostadmin, since everything lies below it; every other root counts with everything below it.
var systemRoots = []string{"/", "/etc", "/usr", "/boot", "/proc", "/sys"}

// IsSystemPath reports whether path, as written and cleaned, is a system path. Only a real path is judged
// reliably; the folder change itself refuses a path with a link.
func IsSystemPath(path string) bool {
	path = filepath.Clean(path)
	for _, root := range systemRoots {
		if path == root || (root != "/" && strings.HasPrefix(path, root+"/")) {
			return true
		}
	}
	return false
}

// isSystemRoot is a system path whose whole tree must not be changed in one step.
func isSystemRoot(path string) bool { return slices.Contains(systemRoots, filepath.Clean(path)) }

func isPseudoFS(path string) bool {
	path = filepath.Clean(path)
	return path == "/proc" || path == "/sys" || strings.HasPrefix(path, "/proc/") || strings.HasPrefix(path, "/sys/")
}

// FolderHost names what the folder action reads as root. Tests point it at an invented host.
type FolderHost struct {
	Accounts *hostadmin.Host
	Stacks   StackCheck
}

// DefaultFolderHost is the real host.
func DefaultFolderHost() FolderHost {
	return FolderHost{Accounts: hostadmin.NewHost(true)}
}

// RegisterFolderActions adds the folder actions, reading the host through h.
func RegisterFolderActions(r *Registry, h FolderHost) error {
	f := folderActions{h}
	return r.Register(Definition{ID: ActionFolderSetPermissions, Plan: f.planSetPermissions, Inverse: f.inverseSetPermissions})
}

type folderActions struct{ h FolderHost }

// folderEntry is the state of one entry before the change.
type folderEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // dir, file, link or other
	UID  int    `json:"uid"`
	GID  int    `json:"gid"`
	// Mode holds the permission and the special bits (setuid, setgid, sticky); it is not restored for a link.
	Mode uint32 `json:"mode"`
	dev  uint64
}

type folderState struct {
	Path      string        `json:"path"`
	Recursive bool          `json:"recursive"`
	Entries   []folderEntry `json:"entries"`
}

func entryOf(path string, info fs.FileInfo) (folderEntry, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return folderEntry{}, errors.New("the file system gives no owner information")
	}
	kind := "other"
	switch {
	case info.IsDir():
		kind = "dir"
	case info.Mode()&fs.ModeSymlink != 0:
		kind = "link"
	case info.Mode().IsRegular():
		kind = "file"
	}
	return folderEntry{Path: path, Kind: kind, UID: int(st.Uid), GID: int(st.Gid), Mode: st.Mode & 0o7777, dev: uint64(st.Dev)}, nil
}

// collectEntries reads the folder and, when asked, everything below it, without following a link. It refuses
// what a change could not restore or should not cross.
func collectEntries(root string, recursive bool) ([]folderEntry, error) {
	var out []folderEntry
	var rootDev uint64
	walk := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return Refusef("%s cannot be read: %v", path, err)
		}
		info, err := d.Info()
		if err != nil {
			return Refusef("%s cannot be read: %v", path, err)
		}
		e, err := entryOf(path, info)
		if err != nil {
			return err
		}
		switch {
		case len(out) == 0:
			rootDev = e.dev
			if e.Kind != "dir" {
				return Refusef("%s is not a folder", path)
			}
		case e.dev != rootDev:
			return Refusef("%s lies on another file system or mount; mounts are not changed together with the folder", path)
		}
		// The symbolic mode clears the sticky bit of every entry, and setuid and setgid of a file; chown
		// drops the latter as well. Special bits stay unchanged, so such an entry is refused.
		if e.Kind != "link" && (e.Mode&0o1000 != 0 || (e.Kind != "dir" && e.Mode&0o6000 != 0)) {
			return Refusef("%s has a special bit (setuid, setgid or sticky) that this change would drop; special bits stay unchanged", path)
		}
		if len(out) >= MaxFolderEntries {
			return fmt.Errorf("%w (more than %d)", ErrTooManyEntries, MaxFolderEntries)
		}
		out = append(out, e)
		if !recursive && e.Kind == "dir" && len(out) == 1 {
			return fs.SkipDir
		}
		return nil
	}
	if err := filepath.WalkDir(root, walk); err != nil {
		return nil, err
	}
	return out, nil
}

// CountFolderEntries counts the entries a change of the folder touches: the folder itself and, when
// recursive, everything below it. A folder with more than MaxFolderEntries entries answers ErrTooManyEntries.
func CountFolderEntries(path string, recursive bool) (int, error) {
	entries, err := collectEntries(path, recursive)
	if err != nil && !errors.Is(err, ErrTooManyEntries) {
		return 0, err
	}
	return len(entries), err
}

func checkFolderPath(path string) error {
	switch {
	case !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\n\r"):
		return Argf("the folder must be an absolute path without control characters")
	case filepath.Clean(path) != path:
		return Argf("the folder must be given as a clean path without \"..\", \"//\" or a trailing \"/\"")
	}
	return nil
}

func (f folderActions) planSetPermissions(args []string, _ []byte) (Plan, error) {
	if len(args) < 6 || len(args) > 8 {
		return Plan{}, Argf("folder.set-permissions needs a path, owner, group, three levels and optionally %s and %s",
			optionRecursive, optionSystemPath)
	}
	path, owner, group := args[0], args[1], args[2]
	if err := checkFolderPath(path); err != nil {
		return Plan{}, err
	}
	if err := checkName("user", owner); err != nil {
		return Plan{}, err
	}
	if err := checkName("group", group); err != nil {
		return Plan{}, err
	}
	var mode []string
	for i, pos := range []string{"u", "g", "o"} {
		sym, ok := symbolic(args[3+i])
		if !ok {
			return Plan{}, Argf("%q is not a level (%s, %s or %s)", args[3+i], levelNothing, levelRead, levelReadWrite)
		}
		mode = append(mode, pos+"="+sym)
	}
	recursive, ack := false, false
	for _, o := range args[6:] {
		switch {
		case o == optionRecursive && !recursive:
			recursive = true
		case o == optionSystemPath && !ack:
			ack = true
		default:
			return Plan{}, Argf("unknown or repeated option %q for folder.set-permissions", o)
		}
	}
	system := IsSystemPath(path)
	switch {
	case isPseudoFS(path):
		return Plan{}, Refusef("%s is a virtual file system; its permissions are not changed", path)
	case recursive && isSystemRoot(path):
		return Plan{}, Refusef("%s is a system path and not changed together with everything below it", path)
	case system && !ack:
		return Plan{}, Refusef("%s is a system path; changing it needs the acknowledgement of the system path", path)
	}

	symMode := strings.Join(mode, ",")
	chown := []string{"chown"}
	chmod := []string{"chmod"}
	if recursive {
		chown = append(chown, "-R", "-h")
		chmod = append(chmod, "-R")
	}
	chown = append(chown, "--", owner+":"+group, path)
	chmod = append(chmod, "--", symMode, path)

	sentence := fmt.Sprintf("Change %s", path)
	if recursive {
		sentence += " and everything below it"
	}
	sentence += fmt.Sprintf(": owner %s, group %s; the owner gets %s, the group %s, everyone else %s.",
		owner, group, accessWords(args[3]), accessWords(args[4]), accessWords(args[5]))
	if system {
		sentence += " This is a system path."
	}

	var state folderState
	var stacks []AffectedStack
	return Plan{
		Sentence: sentence,
		Commands: []Command{{Argv: chown}, {Argv: chmod}},
		// A recursive chown can fail midway; the inverse sets every recorded entry to its recorded value.
		PartialSteps: true,
		Capture: func(ctx context.Context) (json.RawMessage, error) {
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil, Refusef("%s does not exist", path)
				}
				return nil, err
			}
			if real != path {
				return nil, Refusef("%s is or lies behind a link to %s; change the real folder instead", path, real)
			}
			if _, err := f.h.Accounts.Account(owner); err != nil {
				return nil, Refusef("there is no local user %s", owner)
			}
			if _, err := f.h.Accounts.Group(group); err != nil {
				return nil, Refusef("there is no local group %s", group)
			}
			entries, err := collectEntries(path, recursive)
			if errors.Is(err, ErrTooManyEntries) {
				return nil, Refusef("%s has more than %d entries; change a smaller folder", path, MaxFolderEntries)
			}
			if err != nil {
				return nil, err
			}
			if stacks, err = f.h.Stacks.Affected(ctx, path); err != nil {
				return nil, Refusef("the running stacks cannot be checked, so nothing was changed: %v", err)
			}
			state = folderState{Path: path, Recursive: recursive, Entries: entries}
			return json.Marshal(state)
		},
		Check: func(ctx context.Context) error {
			if err := specialBitsUnchanged(state.Entries, lstatEntry); err != nil {
				return err
			}
			return f.h.Stacks.Watch(ctx, stacks)
		},
	}, nil
}

func lstatEntry(path string) (folderEntry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return folderEntry{}, err
	}
	return entryOf(path, info)
}

// specialBitsUnchanged compares setuid, setgid and sticky of every entry with the recorded ones.
func specialBitsUnchanged(entries []folderEntry, stat func(string) (folderEntry, error)) error {
	for _, e := range entries {
		if e.Kind == "link" {
			continue
		}
		now, err := stat(e.Path)
		if err != nil {
			return fmt.Errorf("%s cannot be read after the change: %w", e.Path, err)
		}
		if now.Mode&0o7000 != e.Mode&0o7000 {
			return fmt.Errorf("the special bits of %s changed from %04o to %04o", e.Path, e.Mode&0o7000, now.Mode&0o7000)
		}
	}
	return nil
}

const (
	maxChunkPaths = 1000
	maxChunkBytes = 32 << 10
)

func (f folderActions) inverseSetPermissions(prev json.RawMessage) (Plan, error) {
	var st folderState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := checkFolderPath(st.Path); err != nil || len(st.Entries) == 0 {
		return Plan{}, errors.New("the recorded state of the folder is not usable")
	}
	type owner struct{ uid, gid int }
	owners := map[owner][]string{}
	modes := map[uint32][]string{}
	for _, e := range st.Entries {
		if !lexicallyUnder(st.Path, e.Path) || filepath.Clean(e.Path) != e.Path || e.Mode > 0o7777 || e.UID < 0 || e.GID < 0 ||
			!slices.Contains([]string{"dir", "file", "link", "other"}, e.Kind) {
			return Plan{}, fmt.Errorf("the recorded state of %q is not usable", e.Path)
		}
		o := owner{e.UID, e.GID}
		owners[o] = append(owners[o], e.Path)
		if e.Kind != "link" {
			modes[e.Mode] = append(modes[e.Mode], e.Path)
		}
	}
	var cmds []Command
	ownerKeys := make([]owner, 0, len(owners))
	for o := range owners {
		ownerKeys = append(ownerKeys, o)
	}
	sort.Slice(ownerKeys, func(i, j int) bool {
		return ownerKeys[i].uid < ownerKeys[j].uid || (ownerKeys[i].uid == ownerKeys[j].uid && ownerKeys[i].gid < ownerKeys[j].gid)
	})
	// Owners first: chown may drop special bits, which the modes then bring back.
	for _, o := range ownerKeys {
		for _, chunk := range chunkPaths(owners[o]) {
			cmds = append(cmds, Command{Argv: append([]string{"chown", "-h", "--", fmt.Sprintf("%d:%d", o.uid, o.gid)}, chunk...)})
		}
	}
	modeKeys := make([]uint32, 0, len(modes))
	for m := range modes {
		modeKeys = append(modeKeys, m)
	}
	slices.Sort(modeKeys)
	for _, m := range modeKeys {
		for _, chunk := range chunkPaths(modes[m]) {
			cmds = append(cmds, Command{Argv: append([]string{"chmod", "--", fmt.Sprintf("%04o", m)}, chunk...)})
		}
	}
	what := "the folder " + st.Path
	if st.Recursive {
		what = fmt.Sprintf("the folder %s and everything below it (%d entries)", st.Path, len(st.Entries))
	}
	return Plan{Sentence: "Restore the previous owner, group and permissions of " + what + ".", Commands: cmds}, nil
}

// chunkPaths splits paths so that no command gets too many or too long arguments.
func chunkPaths(paths []string) [][]string {
	var out [][]string
	var cur []string
	size := 0
	for _, p := range paths {
		if len(cur) > 0 && (len(cur) >= maxChunkPaths || size+len(p) > maxChunkBytes) {
			out = append(out, cur)
			cur, size = nil, 0
		}
		cur = append(cur, p)
		size += len(p) + 1
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}
