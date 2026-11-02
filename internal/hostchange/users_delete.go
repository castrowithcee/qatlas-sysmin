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
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// Deleting a user keeps its home unless with-home asks otherwise. Without the home the delete is undone by
// creating the account again exactly as it was: the same UID, primary group, supplementary groups, passwd
// fields and shadow entry. With the home it is final.

// deleteState is the journal state of user.delete without the home: everything the restore needs. It holds the
// password hash, like the state of user.set-password.
type deleteState struct {
	Name  string `json:"name"`
	UID   int    `json:"uid"`
	Group string `json:"group"`
	GID   int    `json:"gid"`
	// OwnGroup says the delete removed the primary group, the user's own group, so the restore creates it again.
	OwnGroup bool     `json:"own_group"`
	Groups   []string `json:"groups"`
	GECOS    string   `json:"gecos"`
	Home     string   `json:"home"`
	Shell    string   `json:"shell"`
	Hash     string   `json:"hash"`
	Lastchg  string   `json:"lastchg"`
	Min      string   `json:"min"`
	Max      string   `json:"max"`
	Warn     string   `json:"warn"`
	Inactive string   `json:"inactive"`
	Expire   string   `json:"expire"`
}

// passwd is the passwd line the state restores.
func (st deleteState) passwd() []string {
	return []string{st.Name, "x", strconv.Itoa(st.UID), strconv.Itoa(st.GID), st.GECOS, st.Home, st.Shell}
}

// shadow is the shadow line the state restores, without the reserved last field.
func (st deleteState) shadow() []string {
	return []string{st.Name, st.Hash, st.Lastchg, st.Min, st.Max, st.Warn, st.Inactive, st.Expire}
}

// check validates the state strictly, because it is the only input of the restore. The error names the field,
// never its value.
func (st deleteState) check() error {
	switch {
	case checkName("user", st.Name) != nil:
		return errors.New("the name")
	case !validUID(st.UID) || !validUID(st.GID):
		return errors.New("the UID or GID")
	case checkName("group", st.Group) != nil:
		return errors.New("the name of the primary group")
	case st.OwnGroup && st.Group != st.Name:
		return errors.New("the own group")
	case !cleanPath(st.Home):
		return errors.New("the home")
	case !cleanPath(st.Shell):
		return errors.New("the shell")
	case !validGECOS(st.GECOS):
		return errors.New("the comment field")
	case !validHash(st.Hash):
		return errors.New("the password")
	}
	for _, d := range []string{st.Lastchg, st.Min, st.Max, st.Warn, st.Inactive, st.Expire} {
		if !validDay(d) {
			return errors.New("a field of the password aging")
		}
	}
	if !slices.IsSorted(st.Groups) || len(slices.Compact(slices.Clone(st.Groups))) != len(st.Groups) {
		return errors.New("the list of groups")
	}
	for _, g := range st.Groups {
		if checkName("group", g) != nil || g == st.Group {
			return errors.New("the list of groups")
		}
	}
	return nil
}

// cleanPath is an absolute path without a redundant part or a character the account files cannot hold.
func cleanPath(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && !strings.ContainsRune(p, ':') && printable(p)
}

func validGECOS(s string) bool { return !strings.ContainsRune(s, ':') && printable(s) }

func printable(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// accountRows reads an account database as fields, without blank and comment lines.
func accountRows(path string) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows [][]string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			rows = append(rows, strings.Split(line, ":"))
		}
	}
	return rows, nil
}

// rowOf is the first row of the name; nil when there is none.
func rowOf(rows [][]string, name string) []string {
	for _, f := range rows {
		if f[0] == name {
			return f
		}
	}
	return nil
}

// groupRow is one line of the group file as it is written, members as listed.
type groupRow struct {
	name    string
	gid     int
	members []string
}

func (u userActions) groupRows() ([]groupRow, error) {
	rows, err := accountRows(u.h.Accounts.GroupPath)
	if err != nil {
		return nil, err
	}
	var out []groupRow
	for _, f := range rows {
		if len(f) < 4 {
			continue
		}
		gid, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		g := groupRow{name: f[0], gid: gid}
		for _, m := range strings.Split(f[3], ",") {
			if m = strings.TrimSpace(m); m != "" {
				g.members = append(g.members, m)
			}
		}
		out = append(out, g)
	}
	return out, nil
}

// listedIn are the names of the groups that list the user as a member, sorted.
func listedIn(groups []groupRow, name string) []string {
	var names []string
	for _, g := range groups {
		if slices.Contains(g.members, name) {
			names = append(names, g.name)
		}
	}
	slices.Sort(names)
	return names
}

func findGroup(groups []groupRow, name string) (groupRow, bool) {
	i := slices.IndexFunc(groups, func(g groupRow) bool { return g.name == name })
	if i < 0 {
		return groupRow{}, false
	}
	return groups[i], true
}

// hasProcesses reports whether a process runs with the user's UID as its real or effective UID. A process that
// ends while the table is read is skipped.
func (u userActions) hasProcesses(uid int) (bool, error) {
	if u.h.Proc == "" {
		return false, errors.New("the processes of the host cannot be read")
	}
	entries, err := os.ReadDir(u.h.Proc)
	if err != nil {
		return false, err
	}
	want := strconv.Itoa(uid)
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(u.h.Proc, e.Name(), "status"))
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return false, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if rest, ok := strings.CutPrefix(line, "Uid:"); ok {
				if ids := strings.Fields(rest); len(ids) >= 2 && (ids[0] == want || ids[1] == want) {
					return true, nil
				}
				break
			}
		}
	}
	return false, nil
}

// planDelete reads "<name>" or "<name> with-home".
func (u userActions) planDelete(args []string, _ []byte) (Plan, error) {
	if len(args) < 1 || len(args) > 2 || len(args) == 2 && args[1] != optionWithHome {
		return Plan{}, Argf("user.delete needs a user name and optionally %s", optionWithHome)
	}
	name := args[0]
	if err := checkName("user", name); err != nil {
		return Plan{}, err
	}
	gone := func(context.Context) error {
		if _, err := u.account(name); !errors.Is(err, hostadmin.ErrNotFound) {
			return fmt.Errorf("%s still exists", name)
		}
		return nil
	}
	if len(args) == 2 {
		var home string
		return Plan{
			Sentence:      "Delete the user " + name + " together with its home and mail spool.",
			Commands:      []Command{{Argv: []string{"userdel", "-r", name}}},
			NotReversible: "the home and mail spool are deleted",
			Capture: func(ctx context.Context) (json.RawMessage, error) {
				usr, err := u.deletable(ctx, name)
				if err != nil {
					return nil, err
				}
				// What else the home holds is the admin's choice, named in the confirmation; only a path that
				// cannot be named safely is refused.
				if !cleanPath(usr.Home) {
					return nil, Refusef("the home %q of %s is unusual and is not deleted; delete the user and keep the home", usr.Home, name)
				}
				home = usr.Home
				return json.Marshal(map[string]any{"name": name, "uid": usr.UID, "home": home})
			},
			Check: func(ctx context.Context) error {
				if err := gone(ctx); err != nil {
					return err
				}
				if _, err := os.Lstat(home); !errors.Is(err, fs.ErrNotExist) {
					return fmt.Errorf("the home %s still exists", home)
				}
				return nil
			},
		}, nil
	}
	var st deleteState
	return Plan{
		Sentence: "Delete the user " + name + "; its home and files stay.",
		Commands: []Command{{Argv: []string{"userdel", name}}},
		Capture: func(ctx context.Context) (json.RawMessage, error) {
			usr, err := u.deletable(ctx, name)
			if err != nil {
				return nil, err
			}
			if st, err = u.deleteSnapshot(usr); err != nil {
				return nil, err
			}
			return json.Marshal(st)
		},
		// Whether userdel removed the own group depends on its settings; the restore follows what it did.
		Seal: func(context.Context) (json.RawMessage, error) {
			groups, err := u.groupRows()
			if err != nil {
				return nil, err
			}
			g, ok := findGroup(groups, st.Group)
			st.OwnGroup = st.Group == st.Name && !(ok && g.gid == st.GID)
			return json.Marshal(st)
		},
		Check: gone,
	}, nil
}

// deletable reads the user a delete takes and enforces the rules every delete shares.
func (u userActions) deletable(ctx context.Context, name string) (hostadmin.User, error) {
	usr, err := u.target(name, anyUID)
	if err != nil {
		return hostadmin.User{}, err
	}
	if actor := ActingUID(ctx); actor != 0 && actor == usr.UID {
		return hostadmin.User{}, Refusef("you cannot delete your own account")
	}
	if err := u.guardRevoke(ctx, usr); err != nil {
		return hostadmin.User{}, err
	}
	users, err := accountRows(u.h.Accounts.PasswdPath)
	if err != nil {
		return hostadmin.User{}, err
	}
	groups, err := u.groupRows()
	if err != nil {
		return hostadmin.User{}, err
	}
	own, hasOwn := findGroup(groups, name)
	for _, f := range users {
		if len(f) < 7 || f[0] == name {
			continue
		}
		if f[2] == strconv.Itoa(usr.UID) {
			return hostadmin.User{}, Refusef("%s shares UID %d with %s; the delete is refused because it could not be undone exactly", name, usr.UID, f[0])
		}
		if hasOwn && f[3] == strconv.Itoa(own.gid) {
			return hostadmin.User{}, Refusef("the group %s is the primary group of %s; the delete is refused because it could not be undone exactly", name, f[0])
		}
	}
	running, err := u.hasProcesses(usr.UID)
	if err != nil {
		return hostadmin.User{}, fmt.Errorf("read the running processes: %w", err)
	}
	if running {
		return hostadmin.User{}, Refusef("%s still has running processes; end them first", name)
	}
	return usr, nil
}

// HomeRiskKind names one way deleting a home reaches beyond the user's own files.
type HomeRiskKind string

const (
	HomeSystemPath   HomeRiskKind = "system-path"
	HomeHoldsHome    HomeRiskKind = "holds-home"
	HomeLink         HomeRiskKind = "link"
	HomeNotDirectory HomeRiskKind = "not-directory"
	HomeForeignOwner HomeRiskKind = "foreign-owner"
)

// HomeRisk is one such way. Who names the accounts whose homes it holds, or the owner by name or else by UID.
type HomeRisk struct {
	Kind HomeRiskKind
	Who  []string
}

// HomeRisks reads what deleting the home of the user would reach beyond its own files. It reads only what any
// user may read, so the confirmation can name the risks before sudo is asked. A missing home is only checked
// as a path.
func HomeRisks(h *hostadmin.Host, name, home string) ([]HomeRisk, error) {
	rows, err := accountRows(h.PasswdPath)
	if err != nil {
		return nil, err
	}
	var risks []HomeRisk
	if h.Protected(home) {
		risks = append(risks, HomeRisk{Kind: HomeSystemPath})
	}
	uid, below := -1, strings.TrimSuffix(home, "/")+"/"
	var held []string
	for _, f := range rows {
		switch {
		case len(f) < 7:
		case f[0] == name:
			if id, err := strconv.Atoi(f[2]); err == nil {
				uid = id
			}
		case (f[5] == home || strings.HasPrefix(f[5], below)) && !slices.Contains(held, f[0]):
			held = append(held, f[0])
		}
	}
	if len(held) > 0 {
		risks = append(risks, HomeRisk{Kind: HomeHoldsHome, Who: held})
	}
	info, err := os.Lstat(home)
	if errors.Is(err, fs.ErrNotExist) {
		return risks, nil
	}
	if err != nil {
		return risks, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return append(risks, HomeRisk{Kind: HomeLink}), nil
	}
	if !info.IsDir() {
		risks = append(risks, HomeRisk{Kind: HomeNotDirectory})
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != uid {
		owner := strconv.FormatUint(uint64(st.Uid), 10)
		for _, f := range rows {
			if len(f) >= 7 && f[2] == owner {
				owner = f[0]
				break
			}
		}
		risks = append(risks, HomeRisk{Kind: HomeForeignOwner, Who: []string{owner}})
	}
	return risks, nil
}

// deleteSnapshot records what the restore needs and refuses an account it could not restore exactly.
func (u userActions) deleteSnapshot(usr hostadmin.User) (deleteState, error) {
	name := usr.Name
	unusual := func(what string) error {
		return Refusef("%s of %s is unusual and could not be restored; the delete is refused", what, name)
	}
	users, err := accountRows(u.h.Accounts.PasswdPath)
	if err != nil {
		return deleteState{}, err
	}
	pw := rowOf(users, name)
	if len(pw) != 7 {
		return deleteState{}, unusual("the passwd entry")
	}
	if pw[1] != "x" {
		return deleteState{}, unusual("the password field in the passwd file")
	}
	uid, err1 := strconv.Atoi(pw[2])
	gid, err2 := strconv.Atoi(pw[3])
	if err1 != nil || err2 != nil {
		return deleteState{}, unusual("the passwd entry")
	}
	groups, err := u.groupRows()
	if err != nil {
		return deleteState{}, err
	}
	var primary []groupRow
	for _, g := range groups {
		if g.gid == gid {
			primary = append(primary, g)
		}
	}
	if len(primary) != 1 {
		return deleteState{}, unusual(fmt.Sprintf("the primary group (GID %d)", gid))
	}
	if slices.Contains(primary[0].members, name) {
		return deleteState{}, unusual("the membership in the own primary group")
	}
	shadowRows, err := accountRows(u.h.Accounts.ShadowPath)
	if err != nil {
		return deleteState{}, err
	}
	sh := rowOf(shadowRows, name)
	if len(sh) != 9 || sh[8] != "" {
		return deleteState{}, unusual("the shadow entry")
	}
	st := deleteState{
		Name: name, UID: uid, Group: primary[0].name, GID: gid,
		OwnGroup: primary[0].name == name && len(primary[0].members) == 0,
		Groups:   listedIn(groups, name), GECOS: pw[4], Home: pw[5], Shell: pw[6],
		Hash: sh[1], Lastchg: sh[2], Min: sh[3], Max: sh[4], Warn: sh[5], Inactive: sh[6], Expire: sh[7],
	}
	if st.Groups == nil {
		st.Groups = []string{}
	}
	if err := st.check(); err != nil {
		return deleteState{}, unusual(err.Error())
	}
	return st, nil
}

// dayArg is a day field as chage takes it: -1 clears the field.
func dayArg(d string) string {
	if d == "" {
		return "-1"
	}
	return d
}

// inverseDelete creates the account again as recorded: the own group first where the delete removed it, the
// account without a home of its own making, then the shadow entry, the hash through chpasswd -e on standard
// input and the aging fields through chage. It never deletes anything.
func (u userActions) inverseDelete(prev json.RawMessage) (Plan, error) {
	var st deleteState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := st.check(); err != nil {
		return Plan{}, errors.New("the recorded user cannot be restored")
	}
	var cmds []Command
	sentence := fmt.Sprintf("Create the user %s again with UID %d and the primary group %s", st.Name, st.UID, st.Group)
	if st.OwnGroup {
		cmds = append(cmds, Command{Argv: []string{"groupadd", "-g", strconv.Itoa(st.GID), st.Group}})
		sentence += fmt.Sprintf(" (created again with GID %d)", st.GID)
	}
	useradd := []string{"useradd", "-u", strconv.Itoa(st.UID), "-g", st.Group}
	if len(st.Groups) > 0 {
		useradd = append(useradd, "-G", strings.Join(st.Groups, ","))
		sentence += ", in " + groupWords(st.Groups)
	}
	useradd = append(useradd, "-d", st.Home, "-M", "-s", st.Shell, "-c", st.GECOS, st.Name)
	cmds = append(cmds,
		Command{Argv: useradd},
		Command{Argv: []string{"chpasswd", "-e"}, Secret: []byte(st.Name + ":" + st.Hash + "\n"), SecretLabel: previousHashLabel},
		Command{Argv: []string{"chage", "-d", dayArg(st.Lastchg), "-m", dayArg(st.Min), "-M", dayArg(st.Max),
			"-W", dayArg(st.Warn), "-I", dayArg(st.Inactive), "-E", dayArg(st.Expire), st.Name}},
	)
	sentence += ", with the previous password, its aging and the expiry; the home " + st.Home + " is used as it is."
	return Plan{
		Sentence: sentence,
		Commands: cmds,
		Redact:   [][]byte{[]byte(st.Hash)},
		Capture: func(context.Context) (json.RawMessage, error) {
			if _, err := u.account(st.Name); err == nil {
				return nil, Refusef("a user %s exists again; it is not created a second time", st.Name)
			} else if !errors.Is(err, hostadmin.ErrNotFound) {
				return nil, err
			}
			users, err := accountRows(u.h.Accounts.PasswdPath)
			if err != nil {
				return nil, err
			}
			for _, f := range users {
				if len(f) >= 3 && f[2] == strconv.Itoa(st.UID) {
					return nil, Refusef("UID %d belongs to %s now; %s is not created again", st.UID, f[0], st.Name)
				}
			}
			groups, err := u.groupRows()
			if err != nil {
				return nil, err
			}
			g, ok := findGroup(groups, st.Group)
			switch {
			case st.OwnGroup && ok:
				return nil, Refusef("a group %s exists again; %s is not created again", st.Group, st.Name)
			case st.OwnGroup:
				if i := slices.IndexFunc(groups, func(g groupRow) bool { return g.gid == st.GID }); i >= 0 {
					return nil, Refusef("GID %d belongs to the group %s now; %s is not created again", st.GID, groups[i].name, st.Name)
				}
			case !ok:
				return nil, Refusef("there is no group %s any more; %s is not created again", st.Group, st.Name)
			case g.gid != st.GID:
				return nil, Refusef("the group %s now has GID %d instead of %d; %s is not created again", st.Group, g.gid, st.GID, st.Name)
			}
			for _, name := range st.Groups {
				if _, ok := findGroup(groups, name); !ok {
					return nil, Refusef("there is no group %s any more; %s is not created again", name, st.Name)
				}
			}
			return json.Marshal(map[string]any{"name": st.Name, "uid": st.UID})
		},
		Check: func(context.Context) error {
			users, err := accountRows(u.h.Accounts.PasswdPath)
			if err != nil {
				return err
			}
			if !slices.Equal(rowOf(users, st.Name), st.passwd()) {
				return fmt.Errorf("the passwd entry of %s is not as it was", st.Name)
			}
			groups, err := u.groupRows()
			if err != nil {
				return err
			}
			if g, ok := findGroup(groups, st.Group); !ok || g.gid != st.GID {
				return fmt.Errorf("the primary group %s of %s is not as it was", st.Group, st.Name)
			}
			if !slices.Equal(listedIn(groups, st.Name), st.Groups) {
				return fmt.Errorf("the groups of %s are not as they were", st.Name)
			}
			shadowRows, err := accountRows(u.h.Accounts.ShadowPath)
			if err != nil {
				return err
			}
			if sh := rowOf(shadowRows, st.Name); len(sh) < 8 || !slices.Equal(sh[:8], st.shadow()) {
				return fmt.Errorf("the shadow entry of %s is not as it was", st.Name)
			}
			return nil
		},
	}, nil
}
