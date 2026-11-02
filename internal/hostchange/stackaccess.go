package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// ActionStackSetupAccess sets up exactly one stack folder for the service by the rule of ActionFolderSetupStacks
// and lets the group sysmin pass every parent folder the service user cannot pass, by an ACL entry that gives
// search only. Its argument is built by StackAccessArgs.
const ActionStackSetupAccess = "stack.setup-access"

// StackAccessArgs builds the arguments of ActionStackSetupAccess: the stack folder.
func StackAccessArgs(stackPath string) []string { return []string{stackPath} }

// StackAccessParent is a parent folder that gets the search entry for the group sysmin.
type StackAccessParent struct {
	Path string `json:"path"`
	// Current is owner, group and permission bits of the folder now; the entry changes none of them, but the
	// group bits of a folder with an ACL show its mask.
	Current EntryAccess `json:"current"`
	// HasACL says the folder has an ACL already, to which the entry is added.
	HasACL bool `json:"has_acl"`
}

// StackAccessPreview is what the confirmation shows before anything is changed. It is also the JSON of the
// "preview" field of "host preview"; the json tags are its stable field names.
type StackAccessPreview struct {
	Path  string           `json:"path"`
	Stack StackFolderStack `json:"stack"`
	// Parents are the folders above the stack that the service user cannot pass, from the top down.
	Parents []StackAccessParent `json:"parents"`
	// OthersLoseAccess says the stack folder or a file changes, so that afterwards only root and the group
	// sysmin reach them.
	OthersLoseAccess bool `json:"others_lose_access"`
	// NothingToDo says everything is set up already.
	NothingToDo bool `json:"nothing_to_do"`
}

// serviceIdentity is the user the service runs as, with all its groups.
type serviceIdentity struct {
	uid  int
	gids []int
}

func lookupServiceIdentity() (serviceIdentity, error) {
	u, err := user.Lookup(client.ServiceUser)
	if err != nil {
		return serviceIdentity{}, Refusef("the user %s is unknown: %v", client.ServiceUser, err)
	}
	uid, err1 := strconv.Atoi(u.Uid)
	gid, err2 := strconv.Atoi(u.Gid)
	if err1 != nil || err2 != nil {
		return serviceIdentity{}, Refusef("the user %s has no numeric identity", client.ServiceUser)
	}
	id := serviceIdentity{uid: uid, gids: []int{gid}}
	if names, err := u.GroupIds(); err == nil {
		for _, g := range names {
			if n, err := strconv.Atoi(g); err == nil {
				id.gids = append(id.gids, n)
			}
		}
	}
	return id, nil
}

func (h FolderHost) serviceIdentity() (serviceIdentity, error) {
	if h.service != nil {
		return h.service()
	}
	return lookupServiceIdentity()
}

func (h FolderHost) readParentACL(path string) ([]byte, error) {
	if h.readACL != nil {
		return h.readACL(path)
	}
	return readACL(path)
}

// parentGrant is a parent folder as it is now, which gets the search entry.
type parentGrant struct {
	entry folderEntry
	acl   []byte // nil: the folder has none
}

// planParents finds the folders above dir that the service user cannot pass, judged one by one by their own
// owner, group, mode and ACL, since the kernel only reports the first closed folder on a path.
func (h FolderHost) planParents(dir string, gid int) ([]parentGrant, error) {
	id, err := h.serviceIdentity()
	if err != nil {
		return nil, err
	}
	var parents []string
	for p := filepath.Dir(dir); ; p = filepath.Dir(p) {
		parents = append([]string{p}, parents...)
		if p == "/" {
			break
		}
	}
	var out []parentGrant
	for _, p := range parents {
		e, err := lstatEntry(p)
		if err != nil {
			return nil, Refusef("%s cannot be read: %v", p, err)
		}
		if e.Kind != "dir" {
			return nil, Refusef("%s is not a folder", p)
		}
		acl, err := h.readParentACL(p)
		unsupported := errors.Is(err, errACLUnsupported)
		if err != nil && !unsupported {
			return nil, Refusef("%s cannot get an access entry: %v", p, err)
		}
		if searchAllowed(id.uid, id.gids, e.UID, e.GID, e.Mode, acl) {
			continue
		}
		if IsSystemPath(p) {
			return nil, Refusef("the service user cannot pass %s, which is a system path; it gets no access entry", p)
		}
		if unsupported {
			return nil, Refusef("%s cannot get an access entry: %v", p, errACLUnsupported)
		}
		next, err := grantSearch(acl, e.Mode, uint32(gid))
		if err != nil {
			return nil, Refusef("%s cannot get an access entry: %v", p, err)
		}
		if !searchAllowed(id.uid, id.gids, e.UID, e.GID, e.Mode, next) {
			return nil, Refusef("the service user would still not pass %s with the entry for the group %s; its owner or another entry decides", p, control.ServiceGroup)
		}
		out = append(out, parentGrant{e, acl})
	}
	return out, nil
}

type stackAccessWork struct {
	preview  StackAccessPreview
	entries  []folderEntry
	parents  []parentGrant
	commands []Command
	files    stackFiles
}

func (h FolderHost) planStackAccess(ctx context.Context, path string) (*stackAccessWork, error) {
	if err := resolveFolder(path); err != nil {
		return nil, err
	}
	gid, err := h.serviceGroupGID()
	if err != nil {
		return nil, err
	}
	dirEntry, err := folderRoot(path)
	if err != nil {
		return nil, err
	}
	composes := composeCandidates(path)
	if len(composes) == 0 {
		return nil, Refusef("%s is not a stack: it has no compose file (%s)", path, strings.Join(ComposeFileNames, ", "))
	}
	parents, err := h.planParents(path, gid)
	if err != nil {
		return nil, err
	}
	scan, err := h.newStackScan(ctx, gid, dirEntry.dev)
	if err != nil {
		return nil, err
	}
	st, files, err := scan.stack(filepath.Base(path), path, dirEntry, composes, path)
	if err != nil {
		return nil, err
	}

	w := &stackAccessWork{parents: parents, files: files}
	w.preview = StackAccessPreview{Path: path, Stack: st, Parents: []StackAccessParent{}, OthersLoseAccess: len(st.Changes) > 0}
	if w.preview.Stack.Changes == nil {
		w.preview.Stack.Changes = []StackFolderChange{}
	}
	if w.preview.Stack.Kept == nil {
		w.preview.Stack.Kept = []StackFolderKept{}
	}
	userName, groupName := h.namer()
	for _, p := range parents {
		w.preview.Parents = append(w.preview.Parents, StackAccessParent{
			Path:    p.entry.Path,
			Current: EntryAccess{Owner: userName(p.entry.UID), Group: groupName(p.entry.GID), Mode: p.entry.Mode},
			HasACL:  p.acl != nil,
		})
		w.commands = append(w.commands, aclGrantCommand(p.entry.Path))
	}
	cmds, entries := scan.commands()
	w.commands = append(w.commands, cmds...)
	w.entries = entries
	w.preview.NothingToDo = len(w.commands) == 0
	return w, nil
}

// PreviewStackAccess says what the setup of the stack changes and which parent folders get the entry. A
// refusal comes back as a *RefusedError.
func (h FolderHost) PreviewStackAccess(ctx context.Context, path string) (StackAccessPreview, error) {
	w, err := h.planStackAccess(ctx, path)
	if err != nil {
		return StackAccessPreview{}, err
	}
	return w.preview, nil
}

func (f folderActions) previewStackAccess(ctx context.Context, args []string) (any, error) {
	if len(args) != 1 {
		return nil, Argf("%s needs exactly one stack folder", ActionStackSetupAccess)
	}
	p, err := f.h.PreviewStackAccess(ctx, args[0])
	if err != nil {
		return nil, err
	}
	return p, nil
}

// parentState is a parent folder before the entry: its owner, group and permission bits and its previous ACL
// attribute, which is null when there was none.
type parentState struct {
	folderEntry
	ACL []byte `json:"acl"`
}

type stackAccessState struct {
	Path    string        `json:"path"`
	Entries []folderEntry `json:"entries"`
	Parents []parentState `json:"parents"`
}

func (f folderActions) planStackAccess(args []string, _ []byte) (Plan, error) {
	if len(args) != 1 {
		return Plan{}, Argf("%s needs exactly one stack folder", ActionStackSetupAccess)
	}
	path := args[0]
	if err := checkFolderPath(path); err != nil {
		return Plan{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), planTimeout)
	defer cancel()
	planned, err := f.h.planStackAccess(ctx, path)
	if err != nil {
		return Plan{}, err
	}
	if planned.preview.NothingToDo {
		return Plan{}, Refusef("nothing to change: the stack %s is set up already", path)
	}

	var sentence string
	if len(planned.preview.Stack.Changes) > 0 {
		sentence = fmt.Sprintf("Set up the stack folder %s for the service: the folder becomes root:%s %04o, compose files %04o, env files %04o, "+
			"and other users except root have no access to them afterwards",
			path, control.ServiceGroup, modeStackFolder, modeComposeFile, modeEnvFile)
	}
	if n := len(planned.parents); n > 0 {
		names := make([]string, n)
		for i, p := range planned.parents {
			names[i] = p.entry.Path
		}
		noun, pronoun := "folder", "it"
		if n > 1 {
			noun, pronoun = "folders", "them"
		}
		parent := fmt.Sprintf("let the group %s pass, without reading or listing, the parent %s %s through an ACL entry; owner and group of %s stay",
			control.ServiceGroup, noun, strings.Join(names, ", "), pronoun)
		if sentence == "" {
			parent = strings.ToUpper(parent[:1]) + parent[1:]
		} else {
			parent = "; " + parent
		}
		sentence += parent
	}
	sentence += "."

	var work *stackAccessWork
	var affected []AffectedStack
	return Plan{
		Sentence:     sentence,
		Commands:     planned.commands,
		PartialSteps: true,
		Capture: func(ctx context.Context) (json.RawMessage, error) {
			now, err := f.h.planStackAccess(ctx, path)
			if err != nil {
				return nil, err
			}
			if strings.Join(displays(now.commands), "\n") != strings.Join(displays(planned.commands), "\n") {
				return nil, Refusef("%s or its parent folders changed since the setup was planned; nothing was changed, run it again", path)
			}
			if affected, err = f.h.Stacks.Affected(ctx, path); err != nil {
				return nil, Refusef("the running stacks cannot be checked, so nothing was changed: %v", err)
			}
			work = now
			st := stackAccessState{Path: path, Entries: now.entries}
			for _, p := range now.parents {
				st.Parents = append(st.Parents, parentState{p.entry, p.acl})
			}
			return json.Marshal(st)
		},
		Check: func(ctx context.Context) error {
			if err := specialBitsUnchanged(work.entries, lstatEntry); err != nil {
				return err
			}
			for _, p := range work.parents {
				now, err := lstatEntry(p.entry.Path)
				if err != nil {
					return fmt.Errorf("%s cannot be read after the change: %w", p.entry.Path, err)
				}
				if now.UID != p.entry.UID || now.GID != p.entry.GID || now.Mode&0o7000 != p.entry.Mode&0o7000 {
					return fmt.Errorf("owner, group or special bits of %s changed", p.entry.Path)
				}
			}
			if err := f.h.Stacks.serviceReaches([]stackFiles{work.files}); err != nil {
				return err
			}
			return f.h.Stacks.Watch(ctx, affected)
		},
	}, nil
}

func (f folderActions) inverseStackAccess(prev json.RawMessage) (Plan, error) {
	var st stackAccessState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	bad := errors.New("the recorded state of the stack is not usable")
	if err := checkFolderPath(st.Path); err != nil || len(st.Entries)+len(st.Parents) == 0 {
		return Plan{}, bad
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
	for _, p := range st.Parents {
		if checkFolderPath(p.Path) != nil || p.Path == st.Path || !lexicallyUnder(p.Path, st.Path) || IsSystemPath(p.Path) || p.Mode > 0o7777 {
			return Plan{}, bad
		}
		if p.ACL != nil {
			if _, err := parseACL(p.ACL); err != nil {
				return Plan{}, bad
			}
		}
		cmds = append(cmds, aclRestoreCommand(p.Path, p.Mode, p.ACL))
	}
	return Plan{
		Sentence: fmt.Sprintf("Take back the setup of the stack %s: restore the previous owner, group and permissions of %d entries and the previous ACL of %d parent folders.",
			st.Path, len(st.Entries), len(st.Parents)),
		Commands: cmds,
	}, nil
}
