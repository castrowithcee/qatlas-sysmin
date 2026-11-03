package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// ActionGroupSetMembers adds and removes members of one group in one step:
//
//	group.set-members <group> (+<user> | -<user> | -<user>:without-qsysmin)...
//
// "+user" adds, "-user" removes, each user at most once. In the group sudo the change follows the user-side
// administrator switch: adding a user also adds the user to qsysmin, and "-user" also removes the user from
// qsysmin; "-user:without-qsysmin" removes only from sudo, for an administrator who is not in qsysmin. The
// option is refused for every other group.
const ActionGroupSetMembers = "group.set-members"

const (
	maxMemberChanges = 64
	optionNoQsysmin  = ":" + optionWithoutQsysmin
)

// memberChange is one user of a member change. Qsysmin says that the user is also added to or removed from
// qsysmin; in a recorded state it says that qsysmin really changed.
type memberChange struct {
	Name    string `json:"name"`
	UID     int    `json:"uid"`
	Add     bool   `json:"add"`
	Qsysmin bool   `json:"qsysmin"`
}

// membersState is the journal state: the group the change is bound to and what happened to each user.
type membersState struct {
	Group   string         `json:"group"`
	GID     int            `json:"gid"`
	Changes []memberChange `json:"changes"`
}

// check validates a state strictly, because it is the only input of the inverse.
func (st membersState) check() error {
	if checkName("group", st.Group) != nil || !validUID(st.GID) {
		return errors.New("the group")
	}
	if len(st.Changes) == 0 || len(st.Changes) > maxMemberChanges {
		return errors.New("the list of changes")
	}
	seen := map[string]bool{}
	for _, c := range st.Changes {
		if checkName("user", c.Name) != nil || !validUID(c.UID) || seen[c.Name] ||
			(c.Qsysmin && st.Group != hostadmin.GroupAdmin) {
			return errors.New("the list of changes")
		}
		seen[c.Name] = true
	}
	return nil
}

func (u userActions) planGroupSetMembers(args []string, _ []byte) (Plan, error) {
	if len(args) < 2 || len(args) > maxMemberChanges+1 {
		return Plan{}, Argf("%s needs a group name and 1 to %d changes (+user or -user)", ActionGroupSetMembers, maxMemberChanges)
	}
	group := args[0]
	if err := checkName("group", group); err != nil {
		return Plan{}, err
	}
	var changes []memberChange
	seen := map[string]bool{}
	for _, a := range args[1:] {
		if len(a) < 2 || (a[0] != '+' && a[0] != '-') {
			return Plan{}, Argf("%q is not a change: write +user to add or -user to remove", a)
		}
		c := memberChange{UID: anyUID, Add: a[0] == '+', Qsysmin: group == hostadmin.GroupAdmin}
		name := a[1:]
		if rest, ok := strings.CutSuffix(name, optionNoQsysmin); ok {
			if c.Add || !c.Qsysmin {
				return Plan{}, Argf("%q: without-qsysmin applies only to removing a user from %s", a, hostadmin.GroupAdmin)
			}
			name, c.Qsysmin = rest, false
		}
		if err := checkName("user", name); err != nil {
			return Plan{}, err
		}
		if seen[name] {
			return Plan{}, Argf("%s is given twice", name)
		}
		seen[name] = true
		c.Name = name
		changes = append(changes, c)
	}
	return u.membersPlan(group, anyUID, changes, ""), nil
}

// membersPlan applies the changes in the group. A pinned gid also requires the group to have it, which is how
// the undo finds the group it changed; a pinned UID does the same for each user. An empty sentence is built
// from the changes.
func (u userActions) membersPlan(group string, gid int, changes []memberChange, sentence string) Plan {
	var cmds []Command
	for _, c := range changes {
		switch {
		case c.Add:
			cmds = append(cmds, Command{Argv: []string{"gpasswd", "-a", c.Name, group}})
			if c.Qsysmin {
				// usermod -aG leaves a user who is in qsysmin already as is, where gpasswd -a is not safe to repeat.
				cmds = append(cmds, Command{Argv: []string{"usermod", "-aG", hostadmin.GroupQsysmin, c.Name}})
			}
		default:
			cmds = append(cmds, Command{Argv: []string{"gpasswd", "-d", c.Name, group}})
			if c.Qsysmin {
				cmds = append(cmds, Command{Argv: []string{"gpasswd", "-d", c.Name, hostadmin.GroupQsysmin}})
			}
		}
	}
	if sentence == "" {
		sentence = membersSentence(group, changes)
	}
	return Plan{
		Sentence: sentence,
		Commands: cmds,
		Capture: func(ctx context.Context) (json.RawMessage, error) {
			groups, err := u.groupRows()
			if err != nil {
				return nil, err
			}
			g, ok := findGroup(groups, group)
			switch {
			case !ok:
				return nil, Refusef("there is no group %s", group)
			case gid != anyUID && g.gid != gid:
				return nil, Refusef("the group %s now has GID %d instead of %d; it is not the group of the change", group, g.gid, gid)
			}
			qsysmin, hasQsysmin := findGroup(groups, hostadmin.GroupQsysmin)
			recorded := make([]memberChange, 0, len(changes))
			for _, c := range changes {
				want := c.UID
				usr, err := u.target(c.Name, want)
				if err != nil {
					return nil, err
				}
				listed := slices.Contains(g.members, c.Name)
				switch {
				case c.Add && (listed || usr.PrimaryGroup == group):
					return nil, Refusef("%s is in the group %s already", c.Name, group)
				case !c.Add && usr.PrimaryGroup == group:
					return nil, Refusef("%s is the primary group of %s and cannot be removed", group, c.Name)
				case !c.Add && !listed:
					return nil, Refusef("%s is not a member of the group %s", c.Name, group)
				}
				rc := memberChange{Name: c.Name, UID: usr.UID, Add: c.Add}
				if c.Qsysmin {
					if !hasQsysmin {
						return nil, Refusef("there is no group %s", hostadmin.GroupQsysmin)
					}
					inQsysmin := slices.Contains(qsysmin.members, c.Name)
					switch {
					case c.Add:
						rc.Qsysmin = !inQsysmin && usr.PrimaryGroup != hostadmin.GroupQsysmin
					case usr.PrimaryGroup == hostadmin.GroupQsysmin:
						return nil, Refusef("%s is the primary group of %s and cannot be removed", hostadmin.GroupQsysmin, c.Name)
					case !inQsysmin:
						return nil, Refusef("%s is not in the group %s; remove with %s%s to leave qsysmin as it is", c.Name, hostadmin.GroupQsysmin, c.Name, optionNoQsysmin)
					default:
						rc.Qsysmin = true
					}
				}
				recorded = append(recorded, rc)
			}
			if err := u.guardMembers(ctx, group, recorded); err != nil {
				return nil, err
			}
			return json.Marshal(membersState{Group: group, GID: g.gid, Changes: recorded})
		},
		Check: func(context.Context) error {
			groups, err := u.groupRows()
			if err != nil {
				return err
			}
			g, ok := findGroup(groups, group)
			if !ok || (gid != anyUID && g.gid != gid) {
				return fmt.Errorf("the group %s is not as it was", group)
			}
			qsysmin, _ := findGroup(groups, hostadmin.GroupQsysmin)
			for _, c := range changes {
				if slices.Contains(g.members, c.Name) != c.Add {
					return fmt.Errorf("the membership of %s in %s did not change", c.Name, group)
				}
				if c.Qsysmin && slices.Contains(qsysmin.members, c.Name) != c.Add {
					return fmt.Errorf("the membership of %s in %s did not change", c.Name, hostadmin.GroupQsysmin)
				}
			}
			return nil
		},
	}
}

func membersSentence(group string, changes []memberChange) string {
	var add, remove []string
	for _, c := range changes {
		if c.Add {
			add = append(add, c.Name)
		} else {
			remove = append(remove, c.Name)
		}
	}
	var parts []string
	if len(add) > 0 {
		parts = append(parts, "add "+listWords(add)+" to the group "+group)
	}
	if len(remove) > 0 {
		parts = append(parts, "remove "+listWords(remove)+" from the group "+group)
	}
	s := strings.ToUpper(parts[0][:1]) + parts[0][1:]
	if len(parts) > 1 {
		s += " and " + parts[1]
	}
	if group == hostadmin.GroupAdmin {
		s += "; administrators keep or lose access to qsysmin (group qsysmin) with it"
	}
	return s + "."
}

// guardMembers keeps the administrators, as guardRevoke does for one user: nobody leaves sudo or qsysmin
// themselves, and after the change someone else remains who is in both and can log in. Every added user counts
// for that, because adding to sudo adds to qsysmin.
func (u userActions) guardMembers(ctx context.Context, group string, changes []memberChange) error {
	if group != hostadmin.GroupAdmin && group != hostadmin.GroupQsysmin {
		return nil
	}
	removing := false
	gone := map[string]memberChange{}
	for _, c := range changes {
		if !c.Add {
			removing = true
			gone[c.Name] = c
			if actor := ActingUID(ctx); actor != 0 && actor == c.UID {
				return Refusef("you cannot take administrator rights or access to qsysmin from yourself")
			}
		}
	}
	if !removing {
		return nil
	}
	added := map[string]bool{}
	for _, c := range changes {
		added[c.Name] = c.Add
	}
	users, err := u.h.Accounts.Users()
	if err != nil {
		return err
	}
	for _, o := range users {
		sudo, qsysmin := o.Admin, memberOf(o, hostadmin.GroupQsysmin)
		if c, ok := gone[o.Name]; ok {
			switch group {
			case hostadmin.GroupAdmin:
				sudo, qsysmin = false, qsysmin && !c.Qsysmin
			default:
				qsysmin = false
			}
		}
		if added[o.Name] {
			sudo, qsysmin = sudo || group == hostadmin.GroupAdmin, true
		}
		if sudo && qsysmin && !lockedOut(o) {
			return nil
		}
	}
	return Refusef("the change would leave no administrator with access to qsysmin; one stays")
}

// inverseGroupSetMembers takes back exactly what the change did, guarded like a forward change: users it added
// are removed again, users it removed are added again, bound to the GID of the group and the UIDs of the users.
func (u userActions) inverseGroupSetMembers(prev json.RawMessage) (Plan, error) {
	var st membersState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := st.check(); err != nil {
		return Plan{}, errors.New("the recorded member change cannot be undone")
	}
	back := make([]memberChange, 0, len(st.Changes))
	for _, c := range st.Changes {
		c.Add = !c.Add
		back = append(back, c)
	}
	return u.membersPlan(st.Group, st.GID, back, ""), nil
}
