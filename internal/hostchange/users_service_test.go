package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const serviceShell = "/usr/sbin/nologin"

func TestCreateServiceWithoutHome(t *testing.T) {
	f := newUserFixture(t, 1000)
	res := f.apply(t, ActionUserCreateService, "", "demo-bot")
	want := []string{"useradd --system --no-create-home --home-dir /nonexistent --user-group --shell " + serviceShell + " demo-bot"}
	sameList(t, "executed", f.calls, want)
	sameList(t, "journaled", res.Entry.Commands, want)
	u := f.user("demo-bot")
	if u == nil || u.uid >= 1000 || f.shells["demo-bot"] != serviceShell || u.hash != "!" {
		t.Fatalf("%+v", u)
	}
	if _, err := os.Lstat(filepath.Join(f.dir, "home", "demo-bot")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("home: %v", err)
	}
	if res.Entry.Check != CheckPassed || !res.Entry.Reversible || !strings.Contains(res.Entry.Sentence, "without login, without a home") {
		t.Fatalf("%+v", res.Entry)
	}
	var st createServiceState
	if err := json.Unmarshal(res.Entry.Previous, &st); err != nil || st != (createServiceState{Name: "demo-bot", UID: u.uid, Home: "/nonexistent"}) {
		t.Fatalf("state %+v %v", st, err)
	}
}

func TestCreateServiceWithHomeDockerAndGroups(t *testing.T) {
	f := newUserFixture(t, 1000)
	home := filepath.Join(f.dir, "home", "demo-bot")
	res := f.apply(t, ActionUserCreateService, "", "demo-bot", "home", "docker", "group=projects", "group=media")
	want := []string{"useradd --system --create-home --home-dir " + home + " --user-group --shell " + serviceShell + " -G docker,media,projects demo-bot"}
	sameList(t, "executed", f.calls, want)
	for _, g := range []string{"docker", "media", "projects"} {
		if !f.member("demo-bot", g) {
			t.Errorf("not in %s", g)
		}
	}
	if f.member("demo-bot", "sudo") || f.member("demo-bot", "qsysmin") {
		t.Error("a service account is never an administrator")
	}
	if fi, err := os.Stat(home); err != nil || !fi.IsDir() {
		t.Fatalf("home: %v", err)
	}
	var st createServiceState
	if err := json.Unmarshal(res.Entry.Previous, &st); err != nil || !st.HomeCreated || st.Home != home || st.UID != f.user("demo-bot").uid {
		t.Fatalf("state %+v %v", st, err)
	}
	for _, part := range []string{"with the home " + home, "access to Docker", "media and projects"} {
		if !strings.Contains(res.Entry.Sentence, part) {
			t.Errorf("sentence %q lacks %q", res.Entry.Sentence, part)
		}
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "chpasswd") || strings.HasPrefix(c, "chage") || strings.Contains(c, "authorized-keys") {
			t.Errorf("unexpected %q", c)
		}
	}
}

func TestCreateServiceRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		setup  func(*userFixture)
		reason string
	}{
		{"existing user", []string{"demo-user"}, nil, "exists already"},
		{"existing group", []string{"projects"}, nil, "group projects exists already"},
		{"existing home", []string{"demo-bot", "home"}, func(f *userFixture) { _ = os.Mkdir(filepath.Join(f.dir, "home", "demo-bot"), 0o755) }, "exists already"},
		{"existing directory without home", []string{"demo-bot"}, func(f *userFixture) { _ = os.Mkdir(filepath.Join(f.dir, "home", "demo-bot"), 0o755) }, "exists already"},
		{"existing mail spool", []string{"demo-bot"}, func(f *userFixture) { _ = os.WriteFile(filepath.Join(f.dir, "mail", "demo-bot"), nil, 0o600) }, "exists already"},
		{"unknown group", []string{"demo-bot", "group=nothere"}, nil, "no group nothere"},
		{"system group", []string{"demo-bot", "group=adm"}, nil, "system group"},
		{"fixed group as further", []string{"demo-bot", "group=sudo"}, nil, "own switch"},
		{"admin is not offered", []string{"demo-bot", "admin"}, nil, "unknown option"},
		{"unknown option", []string{"demo-bot", "wheel"}, nil, "unknown option"},
		{"twice", []string{"demo-bot", "home", "home"}, nil, "twice"},
		{"no name", nil, nil, "needs a user name"},
		{"upper case", []string{"Demo"}, nil, "not a valid user name"},
		{"leading dash", []string{"-demo"}, nil, "not a valid user name"},
		{"too long", []string{strings.Repeat("d", 33)}, nil, "not a valid user name"},
		{"root", []string{"root"}, nil, "root"},
		{"bad group name", []string{"demo-bot", "group=a,b"}, nil, "not a valid group name"},
		{"own group", []string{"demo-bot", "group=demo-bot"}, nil, "own group"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			if tc.setup != nil {
				tc.setup(f)
			}
			_, err := f.runner.Apply(context.Background(), ActionUserCreateService, tc.args, nil)
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err %v, want %q", err, tc.reason)
			}
			if len(f.calls) != 0 || f.journalText(t) != "" {
				t.Errorf("something ran: %q", f.calls)
			}
		})
	}
}

func TestCreateServiceCheckFailureTakesItBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		last string
	}{
		{"with home", []string{"demo-bot", "home"}, "userdel -r demo-bot"},
		{"without home", []string{"demo-bot"}, "userdel demo-bot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			f.useraddShell = "/bin/bash"
			res, err := f.runner.Apply(context.Background(), ActionUserCreateService, tc.args, nil)
			if err != nil {
				t.Fatal(err)
			}
			if res.Entry.Result != ResultRestored || res.Entry.Check != CheckFailed || f.calls[len(f.calls)-1] != tc.last || f.user("demo-bot") != nil {
				t.Fatalf("%+v calls %q", res.Entry, f.calls)
			}
			if _, err := os.Stat(filepath.Join(f.dir, "home", "demo-bot")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("home survived: %v", err)
			}
		})
	}
}

func TestCreateServiceUndo(t *testing.T) {
	t.Run("home created", func(t *testing.T) {
		f := newUserFixture(t, 1000)
		home := filepath.Join(f.dir, "home", "demo-bot")
		res := f.apply(t, ActionUserCreateService, "", "demo-bot", "home", "docker")
		f.calls = nil
		undo := f.undo(t, res.Entry.ID)
		sameList(t, "undo", f.calls, []string{"userdel -r demo-bot"})
		if f.user("demo-bot") != nil || f.member("demo-bot", "docker") || undo.Entry.Check != CheckPassed {
			t.Fatalf("still there: %+v", undo.Entry)
		}
		if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("home survived: %v", err)
		}
		if !strings.Contains(undo.Entry.Sentence, "elsewhere stay") {
			t.Errorf("sentence %q", undo.Entry.Sentence)
		}
	})
	t.Run("no home", func(t *testing.T) {
		f := newUserFixture(t, 1000)
		res := f.apply(t, ActionUserCreateService, "", "demo-bot")
		// A directory that appeared under the usual home path since is not the account's to delete.
		stray := filepath.Join(f.dir, "home", "demo-bot")
		if err := os.Mkdir(stray, 0o755); err != nil {
			t.Fatal(err)
		}
		f.calls = nil
		f.undo(t, res.Entry.ID)
		sameList(t, "undo", f.calls, []string{"userdel demo-bot"})
		if f.user("demo-bot") != nil {
			t.Error("still there")
		}
		if _, err := os.Stat(stray); err != nil {
			t.Errorf("directory was deleted: %v", err)
		}
	})
}

func TestCreateServiceUndoRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		actor  int
		args   []string
		setup  func(*userFixture)
		reason string
	}{
		{"uid changed", 1000, []string{"demo-bot", "home"}, func(f *userFixture) { f.user("demo-bot").uid = 996; f.flush() }, "instead of"},
		{"name taken over", 1000, []string{"demo-bot"}, func(f *userFixture) {
			u := f.user("demo-bot")
			u.uid, u.home = 1500, "/nonexistent"
			f.flush()
		}, "instead of"},
		{"shell changed", 1000, []string{"demo-bot"}, func(f *userFixture) { f.shells["demo-bot"] = "/bin/bash"; f.flush() }, "no longer has the shell"},
		{"home changed", 1000, []string{"demo-bot", "home"}, func(f *userFixture) { f.user("demo-bot").home = "/elsewhere"; f.flush() }, "home"},
		{"own account", 997, []string{"demo-bot"}, nil, "your own account"},
		{"gone", 1000, []string{"demo-bot"}, func(f *userFixture) {
			f.users = f.users[:len(f.users)-1]
			f.flush()
		}, "no local user"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			res := f.apply(t, ActionUserCreateService, "", tc.args...)
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
			if !errors.As(err, &ref) || len(f.calls) != 0 {
				t.Errorf("%T calls %q", err, f.calls)
			}
		})
	}
}

func TestCreateServiceUndoRefusesAnAccountThatIsNoLongerSystem(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.user("demo-user").home = "/nonexistent"
	f.flush()
	def, _ := f.runner.Registry.Lookup(ActionUserCreateService)
	plan, err := def.Inverse(json.RawMessage(`{"name":"demo-user","uid":1001,"home":"/nonexistent","home_created":false}`))
	if err != nil {
		t.Fatal(err)
	}
	f.host.Accounts.Privileged = true
	if _, err := plan.Capture(context.Background()); err == nil || !strings.Contains(err.Error(), "not a system account") {
		t.Fatalf("err %v", err)
	}
}

func TestCreateServiceInverseRefusesInvalidRecordedStates(t *testing.T) {
	f := newUserFixture(t, 1000)
	def, _ := f.runner.Registry.Lookup(ActionUserCreateService)
	home := filepath.Join(f.dir, "home", "demo-bot")
	for name, state := range map[string]string{
		"negative uid":         `{"name":"demo-bot","uid":-1,"home":"/nonexistent","home_created":false}`,
		"root uid":             `{"name":"demo-bot","uid":0,"home":"/nonexistent","home_created":false}`,
		"no uid":               `{"name":"demo-bot","home":"/nonexistent","home_created":false}`,
		"root":                 `{"name":"root","uid":5,"home":"/nonexistent","home_created":false}`,
		"bad name":             `{"name":"Demo Bot","uid":5,"home":"/nonexistent","home_created":false}`,
		"relative home":        `{"name":"demo-bot","uid":5,"home":"home/demo-bot","home_created":true}`,
		"unclean home":         `{"name":"demo-bot","uid":5,"home":"` + home + `/../demo-bot","home_created":true}`,
		"foreign home":         `{"name":"demo-bot","uid":5,"home":"` + filepath.Join(f.dir, "home", "demo-user") + `","home_created":true}`,
		"system path as home":  `{"name":"demo-bot","uid":5,"home":"/etc","home_created":true}`,
		"created but nonexist": `{"name":"demo-bot","uid":5,"home":"/nonexistent","home_created":true}`,
		"not created but home": `{"name":"demo-bot","uid":5,"home":"` + home + `","home_created":false}`,
		"unknown field":        `{"name":"demo-bot","uid":5,"home":"/nonexistent","home_created":false,"extra":1}`,
		"no state":             `{}`,
	} {
		if _, err := def.Inverse(json.RawMessage(state)); err == nil || !strings.Contains(err.Error(), "recorded") {
			t.Errorf("%s: err %v, want a refusal of the recorded state", name, err)
		}
	}
	if _, err := def.Inverse(json.RawMessage(`{"name":"demo-bot","uid":5,"home":"` + home + `","home_created":true}`)); err != nil {
		t.Errorf("valid state: %v", err)
	}
}
