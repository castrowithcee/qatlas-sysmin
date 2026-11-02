package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const marker = "hunter-demo-42"

func createInput(t *testing.T, pw string, keys ...string) string {
	t.Helper()
	b, err := json.Marshal(CreateInput{Password: pw, Keys: keys})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func sameList(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%s:\n got  %q\n want %q", what, got, want)
	}
}

func TestDefaultRegistersTheUserActions(t *testing.T) {
	sameList(t, "ids", Default().IDs(), []string{
		"folder.set-permissions", "folder.setup-stacks", "user.add-groups", "user.add-key", "user.create", "user.create-service", "user.enable", "user.lock", "user.remove-groups", "user.remove-key",
		"user.set-admin", "user.set-docker", "user.set-password", "user.unlock",
	})
}

func TestCreatePlainUser(t *testing.T) {
	f := newUserFixture(t, 1000)
	home := filepath.Join(f.dir, "home", "demo-new")
	res := f.apply(t, ActionUserCreate, createInput(t, marker), "demo-new")
	want := []string{
		"useradd -m -d " + home + " -U -s /bin/bash demo-new",
		"chpasswd < <password>",
		"chage -d 0 demo-new",
	}
	sameList(t, "executed", f.calls, want)
	sameList(t, "journaled", res.Entry.Commands, want)
	if f.stdins["chpasswd < <password>"] != "demo-new:"+marker+"\n" {
		t.Errorf("chpasswd input %q", f.stdins["chpasswd < <password>"])
	}
	plan, err := f.runner.Registry.Describe(ActionUserCreate, []string{"demo-new"}, []byte(createInput(t, marker)))
	if err != nil {
		t.Fatal(err)
	}
	sameList(t, "described", displays(plan.Commands), want)
	if res.Entry.Check != CheckPassed || !res.Entry.Reversible || f.user("demo-new").lastchg != "0" {
		t.Fatalf("%+v %+v", res.Entry, f.user("demo-new"))
	}
	if !strings.Contains(res.Entry.Sentence, "must be changed at the first login") {
		t.Errorf("sentence %q", res.Entry.Sentence)
	}
}

func TestCreateWithAdminDockerGroupsAndKeys(t *testing.T) {
	f := newUserFixture(t, 1000)
	k1, k2 := newDemoKey(t, "demo-new@demo-laptop"), newDemoKey(t, "")
	home := filepath.Join(f.dir, "home", "demo-new")
	res := f.apply(t, ActionUserCreate, createInput(t, marker, k1.line, k2.line), "demo-new", "admin", "docker", "group=projects", "group=media")
	want := []string{
		"useradd -m -d " + home + " -U -s /bin/bash -G sudo,sysmin,docker,media,projects demo-new",
		"chpasswd < <password>",
		"chage -d 0 demo-new",
		"[builtin] authorized-keys-add demo-new < <ssh key " + k1.fp + ">",
		"[builtin] authorized-keys-add demo-new < <ssh key " + k2.fp + ">",
	}
	sameList(t, "executed", f.calls, want)
	sameList(t, "journaled", res.Entry.Commands, want)
	for _, g := range []string{"sudo", "sysmin", "docker", "media", "projects"} {
		if !f.member("demo-new", g) {
			t.Errorf("not in %s", g)
		}
	}
	data, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil || string(data) != k1.line+"\n"+k2.line+"\n" {
		t.Fatalf("authorized_keys %q %v", data, err)
	}
	di, _ := os.Stat(filepath.Join(home, ".ssh"))
	fi, _ := os.Stat(filepath.Join(home, ".ssh", "authorized_keys"))
	if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
		t.Errorf("modes %v %v", di.Mode().Perm(), fi.Mode().Perm())
	}
	for _, fp := range []string{k1.fp, k2.fp} {
		if !strings.Contains(res.Entry.Sentence, fp) {
			t.Errorf("sentence lacks %s: %q", fp, res.Entry.Sentence)
		}
	}
}

func TestCreateFailureDeletesTheNewAccountAgain(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.failOn = "chage -d 0 demo-new"
	res, err := f.runner.Apply(context.Background(), ActionUserCreate, []string{"demo-new"}, []byte(createInput(t, marker)))
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry.Result != ResultRestored || f.calls[len(f.calls)-1] != "userdel -r demo-new" || f.user("demo-new") != nil {
		t.Fatalf("%+v calls %q", res.Entry, f.calls)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "home", "demo-new")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("home survived: %v", err)
	}
}

func TestCreateRefusals(t *testing.T) {
	k := newDemoKey(t, "")
	good := createInput(t, marker)
	for _, tc := range []struct {
		name   string
		args   []string
		input  string
		setup  func(*userFixture)
		reason string
	}{
		{"existing user", []string{"demo-user"}, good, nil, "exists already"},
		{"existing group", []string{"projects"}, good, nil, "group projects exists already"},
		{"existing home", []string{"demo-new"}, good, func(f *userFixture) { _ = os.Mkdir(filepath.Join(f.dir, "home", "demo-new"), 0o755) }, "exists already"},
		{"existing mail spool", []string{"demo-new"}, good, func(f *userFixture) { _ = os.WriteFile(filepath.Join(f.dir, "mail", "demo-new"), nil, 0o600) }, "exists already"},
		{"unknown group", []string{"demo-new", "group=nothere"}, good, nil, "no group nothere"},
		{"system group", []string{"demo-new", "group=adm"}, good, nil, "system group"},
		{"no docker group", []string{"demo-new", "docker"}, good, func(f *userFixture) {
			f.groups = slices.DeleteFunc(f.groups, func(g worldGroup) bool { return g.name == "docker" })
			f.flush()
		}, "no group docker"},
		{"fixed group as further", []string{"demo-new", "group=sudo"}, good, nil, "own switch"},
		{"upper case", []string{"Demo"}, good, nil, "not a valid user name"},
		{"leading dash", []string{"-demo"}, good, nil, "not a valid user name"},
		{"space", []string{"demo user"}, good, nil, "not a valid user name"},
		{"too long", []string{strings.Repeat("d", 33)}, good, nil, "not a valid user name"},
		{"root", []string{"root"}, good, nil, "root"},
		{"bad group name", []string{"demo-new", "group=a,b"}, good, nil, "not a valid group name"},
		{"unknown option", []string{"demo-new", "wheel"}, good, nil, "unknown option"},
		{"twice", []string{"demo-new", "admin", "admin"}, good, nil, "twice"},
		{"no input", []string{"demo-new"}, "", nil, "standard input"},
		{"not json", []string{"demo-new"}, marker, nil, "not the expected JSON"},
		{"unknown field", []string{"demo-new"}, `{"pass":"x"}`, nil, "not the expected JSON"},
		{"trailing data", []string{"demo-new"}, good + "{}", nil, "after the JSON"},
		{"empty password", []string{"demo-new"}, createInput(t, ""), nil, "1 to 256 bytes"},
		{"control character", []string{"demo-new"}, createInput(t, "demo\nother:x"), nil, "control characters"},
		{"key with options", []string{"demo-new"}, createInput(t, marker, `command="/bin/true" `+k.line), nil, "options"},
		{"not a key", []string{"demo-new"}, createInput(t, marker, "ssh-ed25519 notbase64"), nil, "authorized_keys format"},
		{"two lines", []string{"demo-new"}, createInput(t, marker, k.line+"\n"+k.line), nil, "one line"},
		{"key twice", []string{"demo-new"}, createInput(t, marker, k.line, k.line), nil, "twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			if tc.setup != nil {
				tc.setup(f)
			}
			var in []byte
			if tc.input != "" {
				in = []byte(tc.input)
			}
			_, err := f.runner.Apply(context.Background(), ActionUserCreate, tc.args, in)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err %v, want %q", err, tc.reason)
			}
			if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), k.blob) {
				t.Errorf("the error carries the input: %v", err)
			}
			if len(f.calls) != 0 || f.journalText(t) != "" {
				t.Errorf("something ran: %q", f.calls)
			}
		})
	}
}

func TestCreateUndoDeletesAnAccountThatNeverLoggedIn(t *testing.T) {
	f := newUserFixture(t, 1000)
	res := f.apply(t, ActionUserCreate, createInput(t, marker), "demo-new", "admin")
	f.calls = nil
	undo := f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"userdel -r demo-new"})
	if f.user("demo-new") != nil || f.member("demo-new", "sudo") || undo.Entry.Check != CheckPassed {
		t.Fatalf("still there: %+v", undo.Entry)
	}
	if !strings.Contains(undo.Entry.Sentence, "never logged in") {
		t.Errorf("sentence %q", undo.Entry.Sentence)
	}
}

func TestCreateUndoKeepsAnAccountThatMayHaveBeenUsed(t *testing.T) {
	logged := func(f *userFixture) {
		rec := make([]byte, lastlogRecord)
		copy(rec, "\x01\x02\x03\x04pts/0")
		fh, err := os.OpenFile(f.host.Lastlog, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer fh.Close()
		if _, err := fh.WriteAt(rec, int64(f.user("demo-new").uid)*lastlogRecord); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		actor  int
		setup  func(*userFixture)
		reason string
	}{
		{"login recorded", 1000, logged, "has logged in"},
		{"no lastlog", 1000, func(f *userFixture) { _ = os.Remove(f.host.Lastlog) }, "cannot be determined"},
		{"lastlog2 in use", 1000, func(f *userFixture) { _ = os.WriteFile(f.host.Lastlog2, nil, 0o600) }, "cannot be determined"},
		{"password changed", 1000, func(f *userFixture) { f.user("demo-new").lastchg = "20100"; f.flush() }, "no longer pending"},
		{"own account", 1004, nil, "your own account"},
		{"other home", 1000, func(f *userFixture) { f.user("demo-new").home = "/elsewhere"; f.flush() }, "home"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			res := f.apply(t, ActionUserCreate, createInput(t, marker), "demo-new")
			if tc.setup != nil {
				tc.setup(f)
			}
			f.runner.SudoUID = tc.actor
			f.calls = nil
			_, err := f.runner.Undo(context.Background(), res.Entry.ID)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err %v, want %q", err, tc.reason)
			}
			var ref *RefusedError
			if !errors.As(err, &ref) || len(f.calls) != 0 || f.user("demo-new") == nil {
				t.Errorf("%T calls %q", err, f.calls)
			}
		})
	}
}

func TestSetPasswordAndUndoRestoreTheShadowEntry(t *testing.T) {
	f := newUserFixture(t, 1000)
	res := f.apply(t, ActionUserSetPassword, marker, "demo-user", "must-change")
	sameList(t, "executed", f.calls, []string{"chpasswd < <password>", "chage -d 0 demo-user"})
	if f.stdins["chpasswd < <password>"] != "demo-user:"+marker+"\n" || f.user("demo-user").lastchg != "0" || res.Entry.Check != CheckPassed {
		t.Fatalf("%+v %+v", f.user("demo-user"), res.Entry)
	}
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"chpasswd -e < <previous password hash>", "chage -d 19000 demo-user"})
	if f.stdins["chpasswd -e < <previous password hash>"] != "demo-user:$6$demo-u\n" {
		t.Errorf("chpasswd -e input %q", f.stdins["chpasswd -e < <previous password hash>"])
	}
	if u := f.user("demo-user"); u.hash != "$6$demo-u" || u.lastchg != "19000" {
		t.Fatalf("not restored: %+v", u)
	}

	f.calls = nil
	f.apply(t, ActionUserSetPassword, marker, "demo-user")
	sameList(t, "without a forced change", f.calls, []string{"chpasswd < <password>"})
	if f.user("demo-user").lastchg == "0" {
		t.Error("a change is forced")
	}
}

func TestSetPasswordUndoRestoresAnEmptyLastChange(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.user("demo-user").lastchg = ""
	f.flush()
	res := f.apply(t, ActionUserSetPassword, marker, "demo-user")
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"chpasswd -e < <previous password hash>", "chage -d -1 demo-user"})
	if f.user("demo-user").lastchg != "" {
		t.Errorf("lastchg %q", f.user("demo-user").lastchg)
	}
}

func TestSetPasswordRefusals(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		input  string
		reason string
	}{
		{[]string{"demo-ghost"}, marker, "no local user"},
		{[]string{"demo-user", "later"}, marker, "unknown option"},
		{[]string{"demo-user"}, "", "standard input"},
		{[]string{"demo-user"}, "a\x00b", "control characters"},
		{[]string{"Demo"}, marker, "not a valid user name"},
	} {
		f := newUserFixture(t, 1000)
		var in []byte
		if tc.input != "" {
			in = []byte(tc.input)
		}
		_, err := f.runner.Apply(context.Background(), ActionUserSetPassword, tc.args, in)
		if err == nil || !strings.Contains(err.Error(), tc.reason) || len(f.calls) != 0 {
			t.Errorf("%v: err %v calls %q, want %q", tc.args, err, f.calls, tc.reason)
		}
	}
}

func TestMembershipChangesAndTheirUndo(t *testing.T) {
	for _, tc := range []struct {
		id         string
		args       []string
		user       string
		want, undo []string
		after      map[string]bool
	}{
		{ActionUserSetAdmin, []string{"demo-user", "on"}, "demo-user",
			[]string{"usermod -aG sudo,sysmin demo-user"}, []string{"gpasswd -d demo-user sudo", "gpasswd -d demo-user sysmin"},
			map[string]bool{"sudo": true, "sysmin": true}},
		{ActionUserSetAdmin, []string{"demo-boss", "on"}, "demo-boss",
			[]string{"usermod -aG sudo,sysmin demo-boss"}, []string{"gpasswd -d demo-boss sysmin"},
			map[string]bool{"sudo": true, "sysmin": true}},
		{ActionUserSetAdmin, []string{"demo-other", "off"}, "demo-other",
			[]string{"gpasswd -d demo-other sudo", "gpasswd -d demo-other sysmin"}, []string{"usermod -aG sudo,sysmin demo-other"},
			map[string]bool{"sudo": false, "sysmin": false}},
		{ActionUserSetAdmin, []string{"demo-boss", "off", "without-sysmin"}, "demo-boss",
			[]string{"gpasswd -d demo-boss sudo"}, []string{"usermod -aG sudo demo-boss"},
			map[string]bool{"sudo": false, "sysmin": false}},
		{ActionUserSetDocker, []string{"demo-other", "on"}, "demo-other",
			[]string{"usermod -aG docker demo-other"}, []string{"gpasswd -d demo-other docker"}, map[string]bool{"docker": true}},
		{ActionUserSetDocker, []string{"demo-user", "off"}, "demo-user",
			[]string{"gpasswd -d demo-user docker"}, []string{"usermod -aG docker demo-user"}, map[string]bool{"docker": false}},
		{ActionUserEnable, []string{"demo-boss"}, "demo-boss",
			[]string{"usermod -aG sysmin demo-boss"}, []string{"gpasswd -d demo-boss sysmin"}, map[string]bool{"sysmin": true}},
		{ActionUserAddGroups, []string{"demo-other", "projects", "media"}, "demo-other",
			[]string{"usermod -aG media,projects demo-other"}, []string{"gpasswd -d demo-other media", "gpasswd -d demo-other projects"},
			map[string]bool{"projects": true, "media": true}},
		{ActionUserAddGroups, []string{"demo-user", "projects", "media"}, "demo-user",
			[]string{"usermod -aG media,projects demo-user"}, []string{"gpasswd -d demo-user projects"},
			map[string]bool{"projects": true, "media": true}},
		{ActionUserRemoveGroups, []string{"demo-user", "media"}, "demo-user",
			[]string{"gpasswd -d demo-user media"}, []string{"usermod -aG media demo-user"}, map[string]bool{"media": false}},
	} {
		t.Run(tc.id+" "+strings.Join(tc.args, " "), func(t *testing.T) {
			f := newUserFixture(t, 1000)
			before := map[string]bool{}
			for g := range tc.after {
				before[g] = f.member(tc.user, g)
			}
			res := f.apply(t, tc.id, "", tc.args...)
			sameList(t, "executed", f.calls, tc.want)
			sameList(t, "journaled", res.Entry.Commands, tc.want)
			if res.Entry.Check != CheckPassed {
				t.Fatalf("%+v", res.Entry)
			}
			for g, want := range tc.after {
				if f.member(tc.user, g) != want {
					t.Errorf("%s in %s: %v", tc.user, g, !want)
				}
			}
			f.calls = nil
			f.undo(t, res.Entry.ID)
			sameList(t, "undo", f.calls, tc.undo)
			for g, want := range before {
				if f.member(tc.user, g) != want {
					t.Errorf("after undo %s in %s: %v", tc.user, g, !want)
				}
			}
		})
	}
}

func TestPartialRemovalIsTakenBack(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.group("projects").members = []string{"demo-user"}
	f.flush()
	f.failOn = "gpasswd -d demo-user projects"
	res, err := f.runner.Apply(context.Background(), ActionUserRemoveGroups, []string{"demo-user", "media", "projects"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sameList(t, "executed", f.calls, []string{"gpasswd -d demo-user media", "gpasswd -d demo-user projects", "usermod -aG media,projects demo-user"})
	if res.Entry.Result != ResultRestored || !f.member("demo-user", "media") || !f.member("demo-user", "projects") {
		t.Fatalf("%+v", res.Entry)
	}
}

func TestAFailingFirstCommandIsNotTakenBack(t *testing.T) {
	// useradd fails, for example because an account of the same name appeared after the previous state was
	// recorded; that account is not this run's to delete.
	f := newUserFixture(t, 1000)
	home := filepath.Join(f.dir, "home", "demo-new")
	f.failOn = "useradd -m -d " + home + " -U -s /bin/bash demo-new"
	res, err := f.runner.Apply(context.Background(), ActionUserCreate, []string{"demo-new"}, []byte(createInput(t, marker)))
	if err != nil {
		t.Fatal(err)
	}
	sameList(t, "executed", f.calls, []string{f.failOn})
	e := res.Entry
	if e.Result != ResultFailed || e.Restored || !strings.Contains(e.Error, "nothing was taken back") || strings.Contains(e.Error, marker) {
		t.Fatalf("%+v", e)
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "userdel") {
			t.Fatalf("userdel ran: %q", f.calls)
		}
	}
}

func TestProtectionRulesRefuseBeforeAnythingRuns(t *testing.T) {
	onlyOneAdmin := func(f *userFixture) {
		for _, g := range []string{"sudo", "sysmin"} {
			f.group(g).members = []string{"demo-admin", "demo-user"}
		}
		f.group("sysmin").members = []string{"demo-admin"}
		f.flush()
	}
	for _, tc := range []struct {
		name   string
		actor  int
		setup  func(*userFixture)
		id     string
		args   []string
		reason string
	}{
		{"own admin", 1000, nil, ActionUserSetAdmin, []string{"demo-admin", "off"}, "from yourself"},
		{"last admin", 0, onlyOneAdmin, ActionUserSetAdmin, []string{"demo-admin", "off"}, "last administrator"},
		{"last admin who is in sudo only besides", 1001, onlyOneAdmin, ActionUserSetAdmin, []string{"demo-admin", "off"}, "last administrator"},
		{"unknown user", 1000, nil, ActionUserSetDocker, []string{"demo-ghost", "on"}, "no local user"},
		{"invalid user name", 1000, nil, ActionUserSetDocker, []string{"demo;x", "on"}, "not a valid user name"},
		{"invalid on/off", 1000, nil, ActionUserSetDocker, []string{"demo-user", "yes"}, "on or off"},
		{"unknown group", 1000, nil, ActionUserAddGroups, []string{"demo-user", "nothere"}, "no group nothere"},
		{"sudo as further group", 1000, nil, ActionUserAddGroups, []string{"demo-user", "sudo"}, "own switch"},
		{"sysmin removal as further group", 1000, nil, ActionUserRemoveGroups, []string{"demo-admin", "sysmin"}, "own switch"},
		{"primary group", 1000, nil, ActionUserRemoveGroups, []string{"demo-user", "demo-user"}, "primary group"},
		{"invalid group name", 1000, nil, ActionUserAddGroups, []string{"demo-user", "../x"}, "not a valid group name"},
		{"nothing to change", 1000, nil, ActionUserSetDocker, []string{"demo-user", "on"}, "nothing to change"},
		{"enable a non-admin", 1000, nil, ActionUserEnable, []string{"demo-user"}, "no administrator"},
		{"remove a group the user is not in", 1000, nil, ActionUserRemoveGroups, []string{"demo-user", "media", "projects"}, "not in the group projects"},
		{"admin off for an admin without sysmin", 1000, nil, ActionUserSetAdmin, []string{"demo-boss", "off"}, "not in the group sysmin"},
		{"docker off for a non-member", 1000, nil, ActionUserSetDocker, []string{"demo-other", "off"}, "not in the group docker"},
		{"without-sysmin only with off", 1000, nil, ActionUserSetAdmin, []string{"demo-boss", "on", "without-sysmin"}, "on or off"},
		{"enable an enabled admin", 1000, nil, ActionUserEnable, []string{"demo-other"}, "nothing to change"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, tc.actor)
			if tc.setup != nil {
				tc.setup(f)
			}
			_, err := f.runner.Apply(context.Background(), tc.id, tc.args, nil)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err %v, want %q", err, tc.reason)
			}
			if len(f.calls) != 0 || f.journalText(t) != "" || len(f.audited) != 0 {
				t.Errorf("something ran: %q", f.calls)
			}
		})
	}
}

func TestUndoIsGuardedLikeAChange(t *testing.T) {
	// demo-other makes demo-user an administrator; demo-user cannot undo that for themselves.
	f := newUserFixture(t, 1002)
	res := f.apply(t, ActionUserSetAdmin, "", "demo-user", "on")
	f.runner.SudoUID = 1001
	f.calls = nil
	if _, err := f.runner.Undo(context.Background(), res.Entry.ID); err == nil || !strings.Contains(err.Error(), "from yourself") || len(f.calls) != 0 {
		t.Fatalf("err %v calls %q", err, f.calls)
	}
	// A different account under the same name is not the user of the change.
	f.runner.SudoUID = 1000
	f.user("demo-user").uid = 1009
	f.flush()
	if _, err := f.runner.Undo(context.Background(), res.Entry.ID); err == nil || !strings.Contains(err.Error(), "not the user of the change") {
		t.Fatalf("err %v", err)
	}
}

func TestKeyActionsAndTheirUndo(t *testing.T) {
	f := newUserFixture(t, 1000)
	k := newDemoKey(t, "demo-user@demo-laptop")
	keys := filepath.Join(f.dir, "home", "demo-user", ".ssh", "authorized_keys")
	res := f.apply(t, ActionUserAddKey, k.line+"\n", "demo-user")
	sameList(t, "add", f.calls, []string{"[builtin] authorized-keys-add demo-user < <ssh key " + k.fp + ">"})
	if b, _ := os.ReadFile(keys); string(b) != k.line+"\n" {
		t.Fatalf("file %q", b)
	}
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo add", f.calls, []string{"[builtin] authorized-keys-remove demo-user " + k.fp})
	if b, _ := os.ReadFile(keys); string(b) != "" {
		t.Fatalf("file %q", b)
	}

	// Removing keeps every other line; the undo writes the removed line back as it was, options included.
	other := newDemoKey(t, "")
	withOptions := `from="192.0.2.1" ` + k.line
	if err := os.WriteFile(keys, []byte("# demo keys\n"+withOptions+"\n"+other.line), 0o600); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	res = f.apply(t, ActionUserRemoveKey, "", "demo-user", k.fp)
	sameList(t, "remove", f.calls, []string{"[builtin] authorized-keys-remove demo-user " + k.fp})
	if b, _ := os.ReadFile(keys); string(b) != "# demo keys\n"+other.line {
		t.Fatalf("file %q", b)
	}
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo remove", f.calls, []string{"[builtin] authorized-keys-add demo-user < <ssh key " + k.fp + ">"})
	if b, _ := os.ReadFile(keys); string(b) != "# demo keys\n"+other.line+"\n"+withOptions+"\n" {
		t.Fatalf("file %q", b)
	}

	for _, tc := range []struct {
		id     string
		args   []string
		input  string
		reason string
	}{
		{ActionUserAddKey, []string{"demo-user"}, k.line, "already"},
		{ActionUserRemoveKey, []string{"demo-user", newDemoKey(t, "").fp}, "", "no SSH key"},
		{ActionUserRemoveKey, []string{"demo-user", "MD5:aa"}, "", "not a SHA256"},
		{ActionUserAddKey, []string{"demo-user"}, "", "standard input"},
	} {
		f.calls = nil
		var in []byte
		if tc.input != "" {
			in = []byte(tc.input)
		}
		if _, err := f.runner.Apply(context.Background(), tc.id, tc.args, in); err == nil || !strings.Contains(err.Error(), tc.reason) || len(f.calls) != 0 {
			t.Errorf("%s %v: %v %q", tc.id, tc.args, err, f.calls)
		}
	}
}

func TestKeyActionsRefuseALinkedSSHDirectory(t *testing.T) {
	f := newUserFixture(t, 1000)
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(f.dir, "home", "demo-user", ".ssh")); err != nil {
		t.Fatal(err)
	}
	k := newDemoKey(t, "")
	_, err := f.runner.Apply(context.Background(), ActionUserAddKey, []string{"demo-user"}, []byte(k.line))
	if err == nil || !strings.Contains(err.Error(), "cannot be read safely") || len(f.calls) != 0 {
		t.Fatalf("err %v calls %q", err, f.calls)
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Errorf("wrote through the link: %v", entries)
	}
}

func TestSecretsStayOutOfArgvDisplaysJournalAndAudit(t *testing.T) {
	f := newUserFixture(t, 1000)
	k := newDemoKey(t, "")
	var results []Result
	results = append(results, f.apply(t, ActionUserCreate, createInput(t, marker, k.line), "demo-new", "admin"))
	results = append(results, f.apply(t, ActionUserSetPassword, marker+"-2", "demo-user"))
	results = append(results, f.apply(t, ActionUserAddKey, newDemoKey(t, "").line, "demo-other"))
	results = append(results, f.undo(t, results[1].Entry.ID))

	// A failing chpasswd that echoes its input is scrubbed from the error text.
	f.failOn = "chpasswd < <password>"
	failed, err := f.runner.Apply(context.Background(), ActionUserSetPassword, []string{"demo-boss"}, []byte(marker+"-3"))
	if err != nil || failed.Entry.Result == ResultApplied {
		t.Fatalf("%+v %v", failed, err)
	}
	results = append(results, failed)

	var visible []string
	visible = append(visible, f.calls...)
	for _, r := range results {
		b, _ := json.Marshal(r.Entry)
		visible = append(visible, string(b), r.Warning)
	}
	for _, a := range f.audited {
		b, _ := json.Marshal(a)
		visible = append(visible, string(b))
	}
	visible = append(visible, f.journalText(t))
	all := strings.Join(visible, "\n")
	for _, s := range []string{marker, k.blob} {
		if strings.Contains(all, s) {
			t.Errorf("%q leaked", s)
		}
	}
	// The previous hash is kept only in the root-only previous state, never in a command or an audit entry.
	shown := strings.Join(f.calls, "\n") + auditText(f.audited)
	for _, r := range results {
		shown += strings.Join(r.Entry.Commands, "\n") + r.Entry.Error
	}
	if strings.Contains(shown, "$6$demo-u") {
		t.Error("the previous hash leaked")
	}
	if !strings.Contains(failed.Entry.Error, "<password>") {
		t.Errorf("error %q", failed.Entry.Error)
	}
}

func auditText(a []AuditEntry) string {
	b, _ := json.Marshal(a)
	return string(b)
}

// bounds are the IDs around the edges of the regular range.
var bounds = []struct {
	id     int
	system bool
}{{0, true}, {999, true}, {1000, false}, {60000, false}, {60001, true}, {65534, true}}

func TestSystemAndRootAccountsAreChangeable(t *testing.T) {
	k := newDemoKey(t, "")
	for _, b := range bounds {
		t.Run(fmt.Sprint(b.id), func(t *testing.T) {
			f := newUserFixture(t, 1002)
			home := filepath.Join(f.host.HomeBase, "demo-edge")
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			f.users = append(f.users, worldUser{"demo-edge", b.id, 1500, home, "$6$demo-e", "19000"})
			f.groups = append(f.groups, worldGroup{"demo-edge", 1500, nil})
			f.flush()
			if usr, err := f.host.Accounts.User("demo-edge"); err != nil || usr.System != b.system {
				t.Fatalf("user %+v err %v, want system %v", usr, err, b.system)
			}
			for _, step := range []struct {
				id    string
				args  []string
				input string
			}{
				{ActionUserSetDocker, []string{"demo-edge", "on"}, ""},
				{ActionUserSetAdmin, []string{"demo-edge", "on"}, ""},
				{ActionUserAddGroups, []string{"demo-edge", "adm", "nogroup"}, ""},
				{ActionUserSetPassword, []string{"demo-edge"}, marker},
				{ActionUserAddKey, []string{"demo-edge"}, k.line},
			} {
				var in []byte
				if step.input != "" {
					in = []byte(step.input)
				}
				res, err := f.runner.Apply(context.Background(), step.id, step.args, in)
				if err != nil {
					t.Fatalf("%s: %v", step.id, err)
				}
				if res.Entry.Result != ResultApplied {
					t.Fatalf("%s: %+v", step.id, res.Entry)
				}
				if _, err := f.runner.Undo(context.Background(), res.Entry.ID); err != nil {
					t.Fatalf("undo of %s: %v", step.id, err)
				}
			}
		})
	}
}

func TestSystemAccountsKeepTheLockoutRules(t *testing.T) {
	// The only administrators are system accounts; none of them may take it from themselves or leave the host without one.
	setup := func(f *userFixture) {
		f.users = append(f.users, worldUser{"demo-svc", 999, 999, "/nonexistent", "*", "19000"})
		f.groups = append(f.groups, worldGroup{"demo-svc", 999, nil})
		for _, g := range []string{"sudo", "sysmin"} {
			f.group(g).members = []string{"demo-svc"}
		}
		f.flush()
	}
	f := newUserFixture(t, 999)
	setup(f)
	if _, err := f.runner.Apply(context.Background(), ActionUserSetAdmin, []string{"demo-svc", "off"}, nil); err == nil || !strings.Contains(err.Error(), "from yourself") {
		t.Fatalf("own admin: %v", err)
	}
	f = newUserFixture(t, 0)
	setup(f)
	if _, err := f.runner.Apply(context.Background(), ActionUserSetAdmin, []string{"demo-svc", "off"}, nil); err == nil || !strings.Contains(err.Error(), "last administrator") {
		t.Fatalf("last admin: %v", err)
	}
	// A system administrator counts as the remaining one.
	f = newUserFixture(t, 0)
	f.group("sudo").members = []string{"demo-admin", "root"}
	f.group("sysmin").members = []string{"demo-admin", "root"}
	f.flush()
	if _, err := f.runner.Apply(context.Background(), ActionUserSetAdmin, []string{"demo-admin", "off"}, nil); err != nil {
		t.Fatalf("another administrator exists: %v", err)
	}
}

func TestInversesRefuseANegativeRecordedUID(t *testing.T) {
	f := newUserFixture(t, 1000)
	fp := newDemoKey(t, "").fp
	line := `ssh-ed25519 AAAA demo`
	for id, state := range map[string]string{
		ActionUserSetPassword: `{"name":"demo-user","uid":-1,"hash":"$6$x","lastchg":"20000"}`,
		ActionUserSetDocker:   `{"name":"demo-user","uid":-1,"add":true,"groups":["docker"],"before":{"docker":false}}`,
		ActionUserAddKey:      `{"name":"demo-user","uid":-1,"fingerprint":"` + fp + `"}`,
		ActionUserRemoveKey:   `{"name":"demo-user","uid":-1,"fingerprint":"` + fp + `","lines":["` + line + `"]}`,
	} {
		def, ok := f.runner.Registry.Lookup(id)
		if !ok {
			t.Fatalf("no action %s", id)
		}
		if _, err := def.Inverse(json.RawMessage(state)); err == nil || !strings.Contains(err.Error(), "recorded") || strings.Contains(err.Error(), "cannot be read") {
			t.Errorf("%s: err %v, want a refusal of the recorded state", id, err)
		}
	}
}
