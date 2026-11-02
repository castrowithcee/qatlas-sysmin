package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// The group actions create, delete and rename a group. sudo and sysmin are never deleted or renamed: the
// rights of the administrators and the access to sysmin rest on these two names, and without them nobody could
// undo anything. Every other group, system groups and docker included, may be deleted and renamed; the
// confirmation of the TUI then asks red.
const (
	ActionGroupCreate = "group.create"
	ActionGroupDelete = "group.delete"
	ActionGroupRename = "group.rename"
)

// protectedGroup is a group that is neither deleted nor renamed.
func protectedGroup(name string) bool {
	return name == hostadmin.GroupAdmin || name == hostadmin.GroupSysmin
}

// primaryUsers names the users whose primary group has the GID, in file order.
func (u userActions) primaryUsers(gid int) ([]string, error) {
	users, err := accountRows(u.h.Accounts.PasswdPath)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, f := range users {
		if len(f) >= 7 && f[3] == strconv.Itoa(gid) {
			names = append(names, f[0])
		}
	}
	return names, nil
}

// groupState is the journal state of the group actions: the name and GID of the group the change is bound to,
// and, for a delete, the members the restore puts back in the order the group file listed them.
type groupState struct {
	Name    string   `json:"name"`
	GID     int      `json:"gid"`
	Members []string `json:"members"`
}

// check validates a state strictly, because it is the only input of the inverse. The error names the field,
// never its value.
func (st groupState) check() error {
	switch {
	case checkName("group", st.Name) != nil || protectedGroup(st.Name):
		return errors.New("the name")
	case !validUID(st.GID):
		return errors.New("the GID")
	}
	seen := map[string]bool{}
	for _, m := range st.Members {
		if checkName("user", m) != nil || seen[m] {
			return errors.New("the list of members")
		}
		seen[m] = true
	}
	return nil
}

func (u userActions) singleName(args []string, action string) (string, error) {
	if len(args) != 1 {
		return "", Argf("%s needs a group name", action)
	}
	if err := checkName("group", args[0]); err != nil {
		return "", err
	}
	return args[0], nil
}

// --- create ---

func (u userActions) planGroupCreate(args []string, _ []byte) (Plan, error) {
	name, err := u.singleName(args, ActionGroupCreate)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Sentence: "Create the group " + name + " with a group ID the system chooses.",
		Commands: []Command{{Argv: []string{"groupadd", name}}},
		Capture: func(context.Context) (json.RawMessage, error) {
			groups, err := u.groupRows()
			if err != nil {
				return nil, err
			}
			if _, ok := findGroup(groups, name); ok {
				return nil, Refusef("the group %s exists already", name)
			}
			return json.Marshal(groupState{Name: name, GID: anyUID, Members: []string{}})
		},
		// The GID is known only once the system has assigned it; the undo is bound to it.
		Seal: func(context.Context) (json.RawMessage, error) {
			groups, err := u.groupRows()
			if err != nil {
				return nil, err
			}
			g, ok := findGroup(groups, name)
			if !ok {
				return nil, fmt.Errorf("the group %s was not created", name)
			}
			return json.Marshal(groupState{Name: name, GID: g.gid, Members: []string{}})
		},
		Check: func(context.Context) error {
			groups, err := u.groupRows()
			if err != nil {
				return err
			}
			g, ok := findGroup(groups, name)
			switch {
			case !ok:
				return fmt.Errorf("the group %s does not exist", name)
			case !hostadmin.Regular(g.gid):
				return fmt.Errorf("%s got GID %d, which is not a regular group ID", name, g.gid)
			case len(g.members) > 0:
				return fmt.Errorf("the new group %s already has members", name)
			}
			return nil
		},
	}, nil
}

// inverseGroupCreate deletes exactly the group the change created: the one with this name and this GID, as
// long as it has gained no members and is no user's primary group.
func (u userActions) inverseGroupCreate(prev json.RawMessage) (Plan, error) {
	var st groupState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if st.check() != nil || !hostadmin.Regular(st.GID) || len(st.Members) > 0 {
		return Plan{}, errors.New("the recorded group cannot be undone")
	}
	return Plan{
		Sentence: fmt.Sprintf("Delete the group %s (GID %d) that sysmin created.", st.Name, st.GID),
		Commands: []Command{{Argv: []string{"groupdel", st.Name}}},
		Capture: func(context.Context) (json.RawMessage, error) {
			groups, err := u.groupRows()
			if err != nil {
				return nil, err
			}
			g, ok := findGroup(groups, st.Name)
			switch {
			case !ok:
				return nil, Refusef("there is no group %s any more", st.Name)
			case g.gid != st.GID:
				return nil, Refusef("the group %s now has GID %d instead of %d; it is not the group of the change", st.Name, g.gid, st.GID)
			case len(g.members) > 0:
				return nil, Refusef("the group %s has members now; it is kept", st.Name)
			}
			primary, err := u.primaryUsers(st.GID)
			if err != nil {
				return nil, err
			}
			if len(primary) > 0 {
				return nil, Refusef("the group %s is the primary group of %s now; it is kept", st.Name, listWords(primary))
			}
			return json.Marshal(map[string]any{"name": st.Name, "gid": st.GID})
		},
		Check: func(context.Context) error { return u.checkGroupGone(st.Name) },
	}, nil
}

func (u userActions) checkGroupGone(name string) error {
	groups, err := u.groupRows()
	if err != nil {
		return err
	}
	if _, ok := findGroup(groups, name); ok {
		return fmt.Errorf("the group %s still exists", name)
	}
	return nil
}

// --- delete ---

func (u userActions) planGroupDelete(args []string, _ []byte) (Plan, error) {
	name, err := u.singleName(args, ActionGroupDelete)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Sentence: fmt.Sprintf("Delete the group %s; its members keep their accounts, and files of the group keep its group number.", name),
		Commands: []Command{{Argv: []string{"groupdel", name}}},
		Capture: func(context.Context) (json.RawMessage, error) {
			st, err := u.deleteGroupSnapshot(name)
			if err != nil {
				return nil, err
			}
			return json.Marshal(st)
		},
		Check: func(context.Context) error { return u.checkGroupGone(name) },
	}, nil
}

// deleteGroupSnapshot enforces the rules of a delete and records what the restore needs. A group whose delete
// could not be undone exactly is refused.
func (u userActions) deleteGroupSnapshot(name string) (groupState, error) {
	groups, err := u.groupRows()
	if err != nil {
		return groupState{}, err
	}
	g, ok := findGroup(groups, name)
	if !ok {
		return groupState{}, Refusef("there is no group %s", name)
	}
	if protectedGroup(name) {
		return groupState{}, Refusef("the group %s gives administrators their rights and access to sysmin and is never deleted", name)
	}
	if primary, err := u.primaryUsers(g.gid); err != nil {
		return groupState{}, err
	} else if len(primary) > 0 {
		return groupState{}, Refusef("the group %s is the primary group of %s and cannot be deleted", name, listWords(primary))
	}
	unusual := func(what string) error {
		return Refusef("%s of the group %s is unusual and could not be restored; the delete is refused", what, name)
	}
	if n := slices.IndexFunc(groups, func(o groupRow) bool { return o.gid == g.gid && o.name != name }); n >= 0 {
		return groupState{}, Refusef("the group %s shares GID %d with %s; the delete is refused because it could not be undone exactly", name, g.gid, groups[n].name)
	}
	users, err := accountRows(u.h.Accounts.PasswdPath)
	if err != nil {
		return groupState{}, err
	}
	for _, m := range g.members {
		if rowOf(users, m) == nil {
			return groupState{}, Refusef("%s is listed in the group %s but is no local user; the delete is refused because it could not be undone exactly", m, name)
		}
	}
	if err := u.checkGroupSecrets(name); err != nil {
		return groupState{}, err
	}
	st := groupState{Name: name, GID: g.gid, Members: g.members}
	if st.Members == nil {
		st.Members = []string{}
	}
	if err := st.check(); err != nil {
		return groupState{}, unusual(err.Error())
	}
	return st, nil
}

// checkGroupSecrets refuses a group that has a password or group administrators, which the restore does not
// bring back.
func (u userActions) checkGroupSecrets(name string) error {
	if u.h.GShadow == "" {
		return nil
	}
	rows, err := accountRows(u.h.GShadow)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	f := rowOf(rows, name)
	if len(f) < 3 {
		return nil
	}
	if f[2] != "" || (f[1] != "" && f[1] != "!" && f[1] != "*") {
		return Refusef("the group %s has a group password or group administrators, which could not be restored; the delete is refused", name)
	}
	return nil
}

// inverseGroupDelete creates the group again with the same GID and puts the same members back. It refuses
// rather than restore part of it.
func (u userActions) inverseGroupDelete(prev json.RawMessage) (Plan, error) {
	var st groupState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := st.check(); err != nil {
		return Plan{}, errors.New("the recorded group cannot be restored")
	}
	cmds := []Command{{Argv: []string{"groupadd", "-g", strconv.Itoa(st.GID), st.Name}}}
	sentence := fmt.Sprintf("Create the group %s again with GID %d", st.Name, st.GID)
	if len(st.Members) > 0 {
		cmds = append(cmds, Command{Argv: []string{"gpasswd", "-M", strings.Join(st.Members, ","), st.Name}})
		sentence += " and the members " + listWords(st.Members)
	}
	return Plan{
		Sentence: sentence + ".",
		Commands: cmds,
		Capture: func(context.Context) (json.RawMessage, error) {
			groups, err := u.groupRows()
			if err != nil {
				return nil, err
			}
			if _, ok := findGroup(groups, st.Name); ok {
				return nil, Refusef("a group %s exists again; it is not created a second time", st.Name)
			}
			if i := slices.IndexFunc(groups, func(g groupRow) bool { return g.gid == st.GID }); i >= 0 {
				return nil, Refusef("GID %d belongs to the group %s now; %s is not created again", st.GID, groups[i].name, st.Name)
			}
			users, err := accountRows(u.h.Accounts.PasswdPath)
			if err != nil {
				return nil, err
			}
			for _, m := range st.Members {
				if rowOf(users, m) == nil {
					return nil, Refusef("%s is no user any more; the group %s is not restored with part of its members", m, st.Name)
				}
			}
			return json.Marshal(map[string]any{"name": st.Name, "gid": st.GID})
		},
		Check: func(context.Context) error {
			groups, err := u.groupRows()
			if err != nil {
				return err
			}
			g, ok := findGroup(groups, st.Name)
			if !ok || g.gid != st.GID {
				return fmt.Errorf("the group %s is not as it was", st.Name)
			}
			if !slices.Equal(sortedCopy(g.members), sortedCopy(st.Members)) {
				return fmt.Errorf("the members of the group %s are not as they were", st.Name)
			}
			return nil
		},
	}, nil
}

func sortedCopy(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

// --- rename ---

// renameState binds a rename to the GID of the group, so the undo renames the same group back.
type renameState struct {
	Old string `json:"old"`
	New string `json:"new"`
	GID int    `json:"gid"`
}

func (st renameState) check() error {
	if checkName("group", st.Old) != nil || checkName("group", st.New) != nil || st.Old == st.New ||
		protectedGroup(st.Old) || protectedGroup(st.New) || !validUID(st.GID) {
		return errors.New("the recorded rename cannot be undone")
	}
	return nil
}

func (u userActions) planGroupRename(args []string, _ []byte) (Plan, error) {
	if len(args) != 2 {
		return Plan{}, Argf("%s needs the current and the new group name", ActionGroupRename)
	}
	old, name := args[0], args[1]
	for _, n := range args {
		if err := checkName("group", n); err != nil {
			return Plan{}, err
		}
	}
	if old == name {
		return Plan{}, Argf("the new name is the current one")
	}
	return u.renamePlan(renameState{Old: old, New: name, GID: anyUID}, fmt.Sprintf("Rename the group %s to %s.", old, name)), nil
}

// renamePlan renames st.Old to st.New. An unpinned GID takes the group that has the old name; a pinned one also
// requires that group to have this GID, which is how the undo finds the group it renamed.
func (u userActions) renamePlan(st renameState, sentence string) Plan {
	var members []string
	gid, captured := st.GID, false
	return Plan{
		Sentence: sentence,
		Commands: []Command{{Argv: []string{"groupmod", "-n", st.New, st.Old}}},
		Capture: func(context.Context) (json.RawMessage, error) {
			groups, err := u.groupRows()
			if err != nil {
				return nil, err
			}
			g, ok := findGroup(groups, st.Old)
			switch {
			case !ok:
				return nil, Refusef("there is no group %s", st.Old)
			case st.GID != anyUID && g.gid != st.GID:
				return nil, Refusef("the group %s now has GID %d instead of %d; it is not the group of the change", st.Old, g.gid, st.GID)
			case protectedGroup(st.Old):
				return nil, Refusef("the group %s is addressed by its name for administrator rights and access to sysmin and is never renamed", st.Old)
			}
			if _, taken := findGroup(groups, st.New); taken {
				return nil, Refusef("the group %s exists already", st.New)
			}
			members, gid, captured = g.members, g.gid, true
			return json.Marshal(renameState{Old: st.Old, New: st.New, GID: g.gid})
		},
		Check: func(context.Context) error {
			groups, err := u.groupRows()
			if err != nil {
				return err
			}
			g, ok := findGroup(groups, st.New)
			switch {
			case !ok || (gid != anyUID && g.gid != gid):
				return fmt.Errorf("the group %s does not have GID %d", st.New, gid)
			case captured && !slices.Equal(g.members, members):
				return fmt.Errorf("the members of the group %s changed", st.New)
			}
			if _, ok := findGroup(groups, st.Old); ok {
				return fmt.Errorf("the group %s still exists", st.Old)
			}
			return nil
		},
	}
}

// inverseGroupRename renames the group back, bound to its GID, and refuses where the old name is taken.
func (u userActions) inverseGroupRename(prev json.RawMessage) (Plan, error) {
	var st renameState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := st.check(); err != nil {
		return Plan{}, err
	}
	return u.renamePlan(renameState{Old: st.New, New: st.Old, GID: st.GID},
		fmt.Sprintf("Rename the group %s back to %s.", st.New, st.Old)), nil
}
