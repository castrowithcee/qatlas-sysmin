package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// accountFiles are the lines of passwd, group and shadow, each file and each member list sorted, so that a
// restore that appends the account at the end compares equal to the state before.
func (w *userWorld) accountFiles(t *testing.T) string {
	t.Helper()
	var out []string
	for _, path := range []string{w.host.Accounts.PasswdPath, w.host.Accounts.GroupPath, w.host.Accounts.ShadowPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if path == w.host.Accounts.GroupPath {
			for i, line := range lines {
				f := strings.Split(line, ":")
				members := strings.Split(f[3], ",")
				slices.Sort(members)
				f[3] = strings.Join(members, ",")
				lines[i] = strings.Join(f, ":")
			}
		}
		slices.Sort(lines)
		out = append(out, strings.Join(lines, "\n"))
	}
	return strings.Join(out, "\n--\n")
}

func deleteRecord(t *testing.T, res Result) deleteState {
	t.Helper()
	var st deleteState
	if err := decodeState(res.Entry.Previous, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestDeleteKeepsTheHomeAndUndoCreatesTheAccountAgainExactly(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.gecos["demo-user"] = "Demo User,,,"
	f.aging["demo-user"] = [4]string{"1", "90", "14", "30"}
	f.expires["demo-user"] = "30000"
	f.flush()
	before := f.accountFiles(t)
	home := f.user("demo-user").home

	res := f.apply(t, ActionUserDelete, "", "demo-user")
	sameList(t, "delete", f.calls, []string{"userdel demo-user"})
	if f.user("demo-user") != nil || f.group("demo-user") != nil || f.member("demo-user", "adm") {
		t.Fatalf("not deleted: %+v %+v", f.user("demo-user"), f.group("demo-user"))
	}
	if _, err := os.Stat(home); err != nil {
		t.Fatalf("the home must stay: %v", err)
	}
	if !res.Entry.Reversible || res.Entry.Check != CheckPassed || !strings.Contains(res.Entry.Sentence, "home and files stay") {
		t.Fatalf("%+v", res.Entry)
	}
	want := deleteState{
		Name: "demo-user", UID: 1001, Group: "demo-user", GID: 1001, OwnGroup: true,
		Groups: []string{"adm", "docker", "media"}, GECOS: "Demo User,,,", Home: home, Shell: "/bin/bash",
		Hash: "$6$demo-u", Lastchg: "19000", Min: "1", Max: "90", Warn: "14", Inactive: "30", Expire: "30000",
	}
	if st := deleteRecord(t, res); !slices.Equal(st.Groups, want.Groups) || st.passwd()[4] != want.GECOS ||
		!slices.Equal(st.shadow(), want.shadow()) || !slices.Equal(st.passwd(), want.passwd()) || st.OwnGroup != want.OwnGroup || st.Group != want.Group {
		t.Fatalf("state %+v\nwant  %+v", st, want)
	}

	f.calls = nil
	undo := f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{
		"groupadd -g 1001 demo-user",
		"useradd -u 1001 -g demo-user -G adm,docker,media -d " + home + " -M -s /bin/bash -c 'Demo User,,,' demo-user",
		"chpasswd -e < <previous password hash>",
		"chage -d 19000 -m 1 -M 90 -W 14 -I 30 -E 30000 demo-user",
	})
	if f.stdins["chpasswd -e < <previous password hash>"] != "demo-user:$6$demo-u\n" {
		t.Errorf("stdin %q", f.stdins)
	}
	if undo.Entry.Check != CheckPassed || undo.Entry.Reversible {
		t.Errorf("%+v", undo.Entry)
	}
	if got := f.accountFiles(t); got != before {
		t.Errorf("not restored exactly:\n%s\nwant\n%s", got, before)
	}
}

func TestDeleteUndoKeepsALockedAndEndedAccountLockedAndEnded(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.user("demo-user").hash, f.user("demo-user").lastchg = "!$6$demo-u", ""
	f.expires["demo-user"] = "1"
	f.flush()
	before := f.accountFiles(t)
	res := f.apply(t, ActionUserDelete, "", "demo-user")
	f.calls = nil
	f.undo(t, res.Entry.ID)
	if f.calls[3] != "chage -d -1 -m 0 -M 99999 -W 7 -I -1 -E 1 demo-user" {
		t.Errorf("chage %q", f.calls[3])
	}
	if got := f.accountFiles(t); got != before {
		t.Errorf("not restored exactly:\n%s\nwant\n%s", got, before)
	}
	usr, err := f.host.Accounts.User("demo-user")
	if err != nil || usr.Locked == nil || !*usr.Locked || usr.Expired == nil || !*usr.Expired {
		t.Errorf("%+v %v", usr, err)
	}
}

func TestDeleteOfAUserInASharedPrimaryGroupKeepsTheGroup(t *testing.T) {
	f := newUserFixture(t, 1000)
	// demo-other is an administrator whose primary group is projects, shared with demo-boss; its own group exists apart.
	f.user("demo-other").gid = 2000
	f.user("demo-boss").gid = 2000
	f.flush()
	before := f.accountFiles(t)
	res := f.apply(t, ActionUserDelete, "", "demo-other")
	if f.group("projects") == nil || f.group("demo-other") == nil {
		t.Fatalf("groups gone: %+v", f.groups)
	}
	if st := deleteRecord(t, res); st.OwnGroup || st.Group != "projects" || st.GID != 2000 || !slices.Equal(st.Groups, []string{"sudo", "sysmin"}) {
		t.Fatalf("state %+v", st)
	}
	f.calls = nil
	f.undo(t, res.Entry.ID)
	if f.calls[0] != "useradd -u 1002 -g projects -G sudo,sysmin -d "+f.user("demo-other").home+" -M -s /bin/bash -c '' demo-other" {
		t.Errorf("calls %q", f.calls)
	}
	if got := f.accountFiles(t); got != before {
		t.Errorf("not restored exactly:\n%s\nwant\n%s", got, before)
	}
}

func TestDeleteRecordsWhatUserdelDidWithTheOwnGroup(t *testing.T) {
	t.Run("a member keeps the group", func(t *testing.T) {
		f := newUserFixture(t, 1000)
		f.group("demo-user").members = []string{"demo-boss"}
		f.flush()
		res := f.apply(t, ActionUserDelete, "", "demo-user")
		if f.group("demo-user") == nil || deleteRecord(t, res).OwnGroup {
			t.Fatalf("%+v", deleteRecord(t, res))
		}
		f.calls = nil
		f.undo(t, res.Entry.ID)
		if !strings.HasPrefix(f.calls[0], "useradd -u 1001 -g demo-user ") {
			t.Errorf("calls %q", f.calls)
		}
	})
	t.Run("userdel keeps own groups", func(t *testing.T) {
		f := newUserFixture(t, 1000)
		f.keepGroups = true
		before := f.accountFiles(t)
		res := f.apply(t, ActionUserDelete, "", "demo-user")
		if f.group("demo-user") == nil || deleteRecord(t, res).OwnGroup {
			t.Fatalf("the record must follow userdel: %+v", deleteRecord(t, res))
		}
		f.calls = nil
		f.undo(t, res.Entry.ID)
		if strings.HasPrefix(f.calls[0], "groupadd") {
			t.Errorf("calls %q", f.calls)
		}
		if got := f.accountFiles(t); got != before {
			t.Errorf("not restored exactly:\n%s\nwant\n%s", got, before)
		}
	})
}

func TestDeleteWithHomeIsFinal(t *testing.T) {
	f := newUserFixture(t, 1000)
	home := f.user("demo-user").home
	spool := filepath.Join(f.host.MailDir, "demo-user")
	if err := os.WriteFile(spool, []byte("demo mail"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := f.apply(t, ActionUserDelete, "", "demo-user", "with-home")
	sameList(t, "delete", f.calls, []string{"userdel -r demo-user"})
	if res.Entry.Reversible || res.Entry.NotReversible != "the home and mail spool are deleted" || res.Entry.Check != CheckPassed {
		t.Fatalf("%+v", res.Entry)
	}
	for _, path := range []string{home, spool} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s stays: %v", path, err)
		}
	}
	if strings.Contains(string(res.Entry.Previous), "$6$") {
		t.Errorf("a final delete needs no password: %s", res.Entry.Previous)
	}
	if _, err := f.runner.Undo(context.Background(), res.Entry.ID); err == nil || !strings.Contains(err.Error(), "cannot be undone") {
		t.Fatalf("undo: %v", err)
	}
}

func TestDeleteAllowsSystemAccountsAndUndoesThem(t *testing.T) {
	for _, name := range []string{"svc-demo", "nobody", "root"} {
		t.Run(name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			before := f.accountFiles(t)
			res := f.apply(t, ActionUserDelete, "", name)
			if f.user(name) != nil {
				t.Fatal("not deleted")
			}
			f.undo(t, res.Entry.ID)
			if got := f.accountFiles(t); got != before {
				t.Errorf("not restored exactly:\n%s\nwant\n%s", got, before)
			}
		})
	}
}

func TestDeleteRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		actor  int
		args   []string
		setup  func(*userFixture)
		reason string
	}{
		{"self", 1001, []string{"demo-user"}, nil, "your own account"},
		{"self with home", 1001, []string{"demo-user", "with-home"}, nil, "your own account"},
		{"last administrator", 1000, []string{"demo-other"}, func(f *userFixture) {
			f.group("sysmin").members = []string{"demo-other"}
		}, "last administrator"},
		{"running process", 1000, []string{"demo-user"}, func(f *userFixture) { f.process(4711, 1001, 1001) }, "still has running processes; end them first"},
		{"effective UID", 1000, []string{"demo-user", "with-home"}, func(f *userFixture) { f.process(4712, 0, 1001) }, "running processes"},
		{"root with its processes", 1000, []string{"root"}, func(f *userFixture) { f.process(1, 0, 0) }, "running processes"},
		{"shared UID", 1000, []string{"demo-user"}, func(f *userFixture) {
			f.users = append(f.users, worldUser{"demo-alias", 1001, 2001, "/nonexistent", "*", "19000"})
		}, "shares UID 1001 with demo-alias"},
		{"own group is the primary group of another", 1000, []string{"demo-user"}, func(f *userFixture) {
			f.users = append(f.users, worldUser{"demo-twin", 1500, 1001, "/nonexistent", "*", "19000"})
		}, "the group demo-user is the primary group of demo-twin"},
		{"unknown user", 1000, []string{"demo-nobody"}, nil, "no local user"},
		{"empty password", 1000, []string{"demo-user"}, func(f *userFixture) { f.user("demo-user").hash = "" }, "the password of demo-user is unusual"},
		{"unusual aging", 1000, []string{"demo-user"}, func(f *userFixture) {
			f.aging["demo-user"] = [4]string{"-5", "90", "7", ""}
		}, "aging of demo-user is unusual"},
		{"comment with a control character", 1000, []string{"demo-user"}, func(f *userFixture) { f.gecos["demo-user"] = "a\tb" }, "comment field"},
		{"empty shell", 1000, []string{"demo-user"}, func(f *userFixture) { f.shells["demo-user"] = " " }, "the shell"},
		{"listed in its own primary group", 1000, []string{"demo-user"}, func(f *userFixture) {
			f.group("demo-user").members = []string{"demo-user"}
		}, "membership in the own primary group"},
		{"primary GID without a group", 1000, []string{"demo-user"}, func(f *userFixture) { f.user("demo-user").gid = 3333 }, "primary group (GID 3333)"},
		{"home that cannot be named safely", 1000, []string{"demo-user", "with-home"}, func(f *userFixture) {
			f.user("demo-user").home = f.dir + "/home/../home/demo-user"
		}, "is unusual and is not deleted"},
		{"missing name", 1000, nil, nil, "needs a user name"},
		{"unknown option", 1000, []string{"demo-user", "home"}, nil, "needs a user name"},
		{"too many arguments", 1000, []string{"demo-user", "with-home", "with-home"}, nil, "needs a user name"},
		{"invalid name", 1000, []string{"../x"}, nil, "not a valid user name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, tc.actor)
			if tc.setup != nil {
				tc.setup(f)
				f.flush()
			}
			_, err := f.runner.Apply(context.Background(), ActionUserDelete, tc.args, nil)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err %v, want %q", err, tc.reason)
			}
			if len(f.calls) != 0 || f.journalText(t) != "" || len(f.audited) != 0 {
				t.Errorf("something ran: %q", f.calls)
			}
		})
	}
}

func TestDeleteWithHomeLeavesTheRisksToTheAdmin(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(f *userFixture) string // returns the home of demo-user
		risk  HomeRiskKind
		stays string // a path that must survive the delete
	}{
		{"system path", func(f *userFixture) string {
			root := filepath.Join(f.dir, "sys")
			f.host.Accounts.ProtectedRoots = append(f.host.Accounts.ProtectedRoots, root)
			return filepath.Join(root, "demo-user")
		}, HomeSystemPath, ""},
		{"holding the home of another account", func(f *userFixture) string {
			shared := filepath.Join(f.dir, "shared")
			f.user("demo-other").home = filepath.Join(shared, "demo-other")
			return shared
		}, HomeHoldsHome, ""},
		{"a link", func(f *userFixture) string {
			link := filepath.Join(f.dir, "demo-link")
			if err := os.Symlink(f.user("demo-other").home, link); err != nil {
				t.Fatal(err)
			}
			return link
		}, HomeLink, "demo-other"},
		// Every file of the fake host belongs to the test's own UID, which on some CI runners is demo-user's 1001.
		{"owned by someone else", func(f *userFixture) string {
			if u := f.user("demo-user"); u.uid == os.Getuid() {
				u.uid = 1004
			}
			return ""
		}, HomeForeignOwner, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			if home := tc.setup(f); home != "" {
				f.user("demo-user").home = home
			}
			home := f.user("demo-user").home
			for _, d := range []string{home, f.user("demo-other").home} {
				if _, err := os.Lstat(d); errors.Is(err, os.ErrNotExist) {
					if err := os.MkdirAll(d, 0o755); err != nil {
						t.Fatal(err)
					}
				}
			}
			f.flush()
			risks, err := HomeRisks(f.host.Accounts, "demo-user", home)
			if err != nil || !slices.ContainsFunc(risks, func(r HomeRisk) bool { return r.Kind == tc.risk }) {
				t.Fatalf("risks %+v %v, want %s", risks, err, tc.risk)
			}
			res := f.apply(t, ActionUserDelete, "", "demo-user", "with-home")
			if res.Entry.Check != CheckPassed || res.Entry.Reversible {
				t.Fatalf("%+v", res.Entry)
			}
			if _, err := os.Lstat(home); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the home stays: %v", err)
			}
			if tc.stays != "" {
				if _, err := os.Stat(f.user(tc.stays).home); err != nil {
					t.Errorf("the target of the link is gone: %v", err)
				}
			}
		})
	}
}

func TestHomeRisks(t *testing.T) {
	f := newUserFixture(t, 1000)
	me := os.Getuid()
	// demo-user owns the files of the fake host here, so its own home has no risk; nobody else has that UID.
	for i := range f.users {
		if f.users[i].uid == me {
			f.users[i].uid = 5000 + i
		}
	}
	f.user("demo-user").uid = me
	file := filepath.Join(f.dir, "demo-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.dir, "demo-link")
	if err := os.Symlink(f.user("demo-other").home, link); err != nil {
		t.Fatal(err)
	}
	f.flush()
	h := f.host.Accounts
	risk := func(kind HomeRiskKind, who ...string) []HomeRisk { return []HomeRisk{{Kind: kind, Who: who}} }
	owner := "demo-user"
	for _, tc := range []struct {
		name, user, home string
		want             []HomeRisk
	}{
		{"own home", "demo-user", f.user("demo-user").home, nil},
		{"missing home", "demo-user", filepath.Join(f.dir, "gone"), nil},
		{"system path", "demo-user", "/usr/local/demo-gone", risk(HomeSystemPath)},
		{"root directory", "demo-user", "/", nil},
		{"holds homes", "demo-user", f.host.HomeBase, risk(HomeHoldsHome, "demo-admin", "demo-other", "demo-boss")},
		{"link", "demo-user", link, risk(HomeLink)},
		{"file", "demo-user", file, risk(HomeNotDirectory)},
		{"owner by name", "demo-other", f.user("demo-other").home, risk(HomeForeignOwner, owner)},
	} {
		got, err := HomeRisks(h, tc.user, tc.home)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.name == "root directory" {
			if len(got) < 2 || got[0].Kind != HomeSystemPath || got[1].Kind != HomeHoldsHome || !slices.Contains(got[1].Who, "demo-admin") {
				t.Errorf("%s: %+v", tc.name, got)
			}
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
			continue
		}
		for i := range got {
			if got[i].Kind != tc.want[i].Kind || !slices.Equal(got[i].Who, tc.want[i].Who) {
				t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
			}
		}
	}
	// An owner without an account is named by UID.
	f.user("demo-user").uid = 5999
	f.flush()
	got, err := HomeRisks(h, "demo-other", f.user("demo-other").home)
	if err != nil || len(got) != 1 || !slices.Equal(got[0].Who, []string{strconv.Itoa(me)}) {
		t.Errorf("%+v %v", got, err)
	}
}

func TestDeleteUndoRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		user   string
		setup  func(*userFixture)
		reason string
	}{
		{"name taken", "demo-user", func(f *userFixture) {
			f.users = append(f.users, worldUser{"demo-user", 1500, 1500, "/nonexistent", "*", "19000"})
		}, "a user demo-user exists again"},
		{"UID taken", "demo-user", func(f *userFixture) {
			f.users = append(f.users, worldUser{"demo-new", 1001, 2000, "/nonexistent", "*", "19000"})
		}, "UID 1001 belongs to demo-new"},
		{"GID taken", "demo-user", func(f *userFixture) {
			f.groups = append(f.groups, worldGroup{"demo-grp", 1001, nil})
		}, "GID 1001 belongs to the group demo-grp"},
		{"group name taken", "demo-user", func(f *userFixture) {
			f.groups = append(f.groups, worldGroup{"demo-user", 3000, nil})
		}, "a group demo-user exists again"},
		{"further group gone", "demo-user", func(f *userFixture) {
			f.groups = slices.DeleteFunc(f.groups, func(g worldGroup) bool { return g.name == "media" })
		}, "no group media any more"},
		{"kept primary group gone", "demo-other", func(f *userFixture) {
			f.groups = slices.DeleteFunc(f.groups, func(g worldGroup) bool { return g.name == "projects" })
		}, "no group projects any more"},
		{"kept primary group renumbered", "demo-other", func(f *userFixture) { f.group("projects").gid = 2100 }, "now has GID 2100 instead of 2000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			f.user("demo-other").gid = 2000
			f.flush()
			res := f.apply(t, ActionUserDelete, "", tc.user)
			tc.setup(f)
			f.flush()
			f.calls = nil
			if _, err := f.runner.Undo(context.Background(), res.Entry.ID); err == nil || !strings.Contains(err.Error(), tc.reason) || len(f.calls) != 0 {
				t.Fatalf("err %v calls %q, want %q", err, f.calls, tc.reason)
			}
		})
	}
}

func TestDeleteUndoThatFailsDeletesTheAccountAgainAndKeepsTheHashOut(t *testing.T) {
	f := newUserFixture(t, 1000)
	del := f.apply(t, ActionUserDelete, "", "demo-user")
	f.failOn = "chpasswd -e < <previous password hash>"
	res, err := f.runner.Undo(context.Background(), del.Entry.ID)
	if err != nil || res.Entry.Result != ResultRestored {
		t.Fatalf("%+v %v", res.Entry, err)
	}
	if f.user("demo-user") != nil || f.group("demo-user") != nil {
		t.Errorf("the half-made account stays: %+v", f.users)
	}
	shown := strings.Join(f.calls, "\n") + auditText(f.audited)
	for _, r := range []Result{del, res} {
		b, _ := json.Marshal(struct {
			Commands []string
			Error    string
			Check    string
			Warning  string
		}{r.Entry.Commands, r.Entry.Error, r.Entry.CheckError, r.Warning})
		shown += string(b)
	}
	if strings.Contains(shown, "$6$demo-u") {
		t.Errorf("the hash leaked:\n%s", shown)
	}
	if !strings.Contains(res.Entry.Error, "<password>") {
		t.Errorf("error %q", res.Entry.Error)
	}
	// The journal keeps it only in the root-only previous state, as for a password change.
	if !strings.Contains(string(del.Entry.Previous), "$6$demo-u") {
		t.Error("the previous state lacks the hash")
	}
}

func TestDeleteInverseRefusesInvalidRecordedStates(t *testing.T) {
	valid := map[string]any{
		"name": "demo-user", "uid": 1001, "group": "demo-user", "gid": 1001, "own_group": true,
		"groups": []string{"adm", "docker"}, "gecos": "Demo", "home": "/home/demo-user", "shell": "/bin/bash",
		"hash": "$6$x", "lastchg": "19000", "min": "0", "max": "99999", "warn": "7", "inactive": "", "expire": "",
	}
	raw := func(change map[string]any) json.RawMessage {
		m := map[string]any{}
		for k, v := range valid {
			m[k] = v
		}
		for k, v := range change {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		return b
	}
	u := userActions{}
	if _, err := u.inverseDelete(raw(nil)); err != nil {
		t.Fatalf("the valid state is refused: %v", err)
	}
	for name, change := range map[string]map[string]any{
		"name":                {"name": "../x"},
		"uid":                 {"uid": -1},
		"gid":                 {"gid": -1},
		"group":               {"group": "Bad Group"},
		"own group elsewhere": {"group": "projects"},
		"relative home":       {"home": "home/demo-user"},
		"unclean home":        {"home": "/home/../etc"},
		"home with colon":     {"home": "/home/a:b"},
		"empty shell":         {"shell": ""},
		"shell with newline":  {"shell": "/bin/bash\n"},
		"gecos colon":         {"gecos": "a:b"},
		"gecos newline":       {"gecos": "a\nb"},
		"empty hash":          {"hash": ""},
		"hash colon":          {"hash": "$6$a:b"},
		"hash newline":        {"hash": "$6$a\nb"},
		"negative day":        {"lastchg": "-1"},
		"date":                {"expire": "2030-01-01"},
		"unsorted groups":     {"groups": []string{"docker", "adm"}},
		"double group":        {"groups": []string{"adm", "adm"}},
		"primary as further":  {"groups": []string{"demo-user"}},
		"bad further group":   {"groups": []string{"a,b"}},
		"unknown field":       {"more": 1},
	} {
		if _, err := u.inverseDelete(raw(change)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := u.inverseDelete(json.RawMessage(`[]`)); err == nil {
		t.Error("not an object: accepted")
	}
}

func TestProcessesCountByTheirRealOrEffectiveUID(t *testing.T) {
	w := newUserWorld(t)
	u := userActions{w.host}
	// A process that ended between the listing and the read has no status any more.
	if err := os.Mkdir(filepath.Join(w.host.Proc, "99"), 0o755); err != nil {
		t.Fatal(err)
	}
	w.process(10, 1001, 1001)
	w.process(11, 0, 1002)
	w.process(13, 0, 0)
	if err := os.WriteFile(filepath.Join(w.host.Proc, "13", "status"), []byte("Uid:\t0\t0\t1003\t1003\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for uid, want := range map[int]bool{1001: true, 1002: true, 0: true, 1003: false, 1004: false} {
		if got, err := u.hasProcesses(uid); err != nil || got != want {
			t.Errorf("UID %d: %v %v, want %v", uid, got, err, want)
		}
	}
	u.h.Proc = ""
	if _, err := u.hasProcesses(1001); err == nil {
		t.Error("an unknown process table must not count as empty")
	}
}
