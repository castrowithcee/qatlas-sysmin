package hostchange

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLockAndUndoRestoreTheExactState(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.expires["demo-user"] = "30000"
	f.flush()
	res := f.apply(t, ActionUserLock, "", "demo-user")
	sameList(t, "lock", f.calls, []string{"usermod -L -e 1 demo-user"})
	if u := f.user("demo-user"); u.hash != "!$6$demo-u" || f.expires[u.name] != "1" || res.Entry.Check != CheckPassed {
		t.Fatalf("%+v %+v", u, res.Entry)
	}
	if !strings.Contains(res.Entry.Sentence, "or an SSH key") {
		t.Errorf("sentence %q", res.Entry.Sentence)
	}
	var st lockState
	if err := json.Unmarshal(res.Entry.Previous, &st); err != nil || st != (lockState{Name: "demo-user", UID: 1001, Hash: "$6$demo-u", Expire: "30000"}) {
		t.Fatalf("state %+v %v", st, err)
	}
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"usermod -U -e 30000 demo-user"})
	if u := f.user("demo-user"); u.hash != "$6$demo-u" || f.expires[u.name] != "30000" {
		t.Fatalf("not restored: %+v", u)
	}
}

func TestLockCompletesAPasswordOnlyLockAndUndoKeepsIt(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.user("demo-user").hash = "!$6$demo-u"
	f.flush()
	res := f.apply(t, ActionUserLock, "", "demo-user")
	if f.expires["demo-user"] != "1" {
		t.Fatalf("%+v", f.user("demo-user"))
	}
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"usermod -e -1 demo-user"})
	if u := f.user("demo-user"); u.hash != "!$6$demo-u" || f.expires[u.name] != "" {
		t.Fatalf("not restored: %+v", u)
	}
}

func TestLockOfAccountsWithoutLoginPasswordUndoesExactly(t *testing.T) {
	f := newUserFixture(t, 1000)
	res := f.apply(t, ActionUserLock, "", "svc-demo")
	if f.user("svc-demo").hash != "!*" {
		t.Fatalf("%+v", f.user("svc-demo"))
	}
	f.undo(t, res.Entry.ID)
	if u := f.user("svc-demo"); u.hash != "*" || f.expires[u.name] != "" {
		t.Fatalf("not restored: %+v", u)
	}
}

func TestUnlockAndUndo(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.user("demo-user").hash = "!$6$demo-u"
	f.expires["demo-user"] = "1"
	f.flush()
	res := f.apply(t, ActionUserUnlock, "", "demo-user")
	sameList(t, "unlock", f.calls, []string{"usermod -U -e -1 demo-user"})
	if u := f.user("demo-user"); u.hash != "$6$demo-u" || f.expires[u.name] != "" || res.Entry.Check != CheckPassed {
		t.Fatalf("%+v %+v", u, res.Entry)
	}
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"usermod -L -e 1 demo-user"})
	if u := f.user("demo-user"); u.hash != "!$6$demo-u" || f.expires[u.name] != "1" {
		t.Fatalf("not restored: %+v", u)
	}
}

func TestUnlockWhenOnlyTheAccountEnded(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.expires["demo-user"] = "100"
	f.flush()
	res := f.apply(t, ActionUserUnlock, "", "demo-user")
	if f.expires["demo-user"] != "" {
		t.Fatal("expiry stays")
	}
	f.undo(t, res.Entry.ID)
	if f.expires["demo-user"] != "100" || f.user("demo-user").hash != "$6$demo-u" {
		t.Fatalf("%+v", f.user("demo-user"))
	}
}

func TestLockAllowsRootAndSystemAccounts(t *testing.T) {
	for _, name := range []string{"root", "nobody"} {
		f := newUserFixture(t, 1000)
		f.apply(t, ActionUserLock, "", name)
		if f.expires[name] != "1" {
			t.Errorf("%s: %+v", name, f.user(name))
		}
	}
}

func TestLockRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		actor  int
		id     string
		args   []string
		setup  func(*userFixture)
		reason string
	}{
		{"self", 1001, ActionUserLock, []string{"demo-user"}, nil, "lock yourself"},
		{"last administrator", 1000, ActionUserLock, []string{"demo-other"}, func(f *userFixture) {
			f.group("sysmin").members = []string{"demo-other"}
			f.flush()
		}, "last administrator"},
		{"expired administrator does not remain", 1000, ActionUserLock, []string{"demo-other"}, func(f *userFixture) {
			f.expires["demo-admin"] = "1"
			f.flush()
		}, "last administrator"},
		{"key-only admin remains", 1000, ActionUserLock, []string{"demo-other"}, nil, ""},
		{"unknown user", 1000, ActionUserLock, []string{"demo-nobody"}, nil, "no local user"},
		{"locked already", 1000, ActionUserLock, []string{"demo-user"}, func(f *userFixture) {
			f.user("demo-user").hash, f.expires["demo-user"] = "!x", "5"
			f.flush()
		}, "locked already"},
		{"empty password", 1000, ActionUserLock, []string{"demo-user"}, func(f *userFixture) {
			f.user("demo-user").hash = ""
			f.flush()
		}, "could not be restored"},
		{"unusual expiry", 1000, ActionUserLock, []string{"demo-user"}, func(f *userFixture) {
			f.expires["demo-user"] = "2030-01-01"
			f.flush()
		}, "could not be restored"},
		{"missing name", 1000, ActionUserLock, nil, nil, "needs a user name"},
		{"invalid name", 1000, ActionUserUnlock, []string{"../x"}, nil, "not a valid user name"},
		{"not locked", 1000, ActionUserUnlock, []string{"demo-user"}, nil, "not locked"},
		{"unknown user to unlock", 1000, ActionUserUnlock, []string{"demo-nobody"}, nil, "no local user"},
		{"no password and no expiry", 1000, ActionUserUnlock, []string{"svc-demo"}, nil, "nothing to change"},
		{"only a lock mark", 1000, ActionUserUnlock, []string{"demo-user"}, func(f *userFixture) {
			f.user("demo-user").hash = "!"
			f.flush()
		}, "nothing to change"},
		{"extra argument", 1000, ActionUserUnlock, []string{"demo-user", "now"}, nil, "needs a user name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, tc.actor)
			if tc.setup != nil {
				tc.setup(f)
			}
			_, err := f.runner.Apply(context.Background(), tc.id, tc.args, nil)
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("err %v, want %q", err, tc.reason)
			}
			if len(f.calls) != 0 || f.journalText(t) != "" || len(f.audited) != 0 {
				t.Errorf("something ran: %q", f.calls)
			}
		})
	}
}

func TestLockCheckFailureIsReported(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.failOn = "usermod -L -e 1 demo-user"
	res, err := f.runner.Apply(context.Background(), ActionUserLock, []string{"demo-user"}, nil)
	if err == nil && res.Entry.Result == ResultApplied {
		t.Fatalf("%+v", res.Entry)
	}
}

func TestLockUndoRefusals(t *testing.T) {
	lock := func(t *testing.T, f *userFixture) Result { return f.apply(t, ActionUserLock, "", "demo-user") }
	t.Run("uid changed", func(t *testing.T) {
		f := newUserFixture(t, 1000)
		res := lock(t, f)
		f.user("demo-user").uid = 1009
		f.flush()
		f.calls = nil
		if _, err := f.runner.Undo(context.Background(), res.Entry.ID); err == nil || !strings.Contains(err.Error(), "not the user of the change") || len(f.calls) != 0 {
			t.Fatalf("err %v calls %q", err, f.calls)
		}
	})
	t.Run("undoing an unlock locks and is guarded", func(t *testing.T) {
		f := newUserFixture(t, 1000)
		f.user("demo-other").hash, f.expires["demo-other"] = "!$6$demo-o", "1"
		f.flush()
		res := f.apply(t, ActionUserUnlock, "", "demo-other")
		f.runner.SudoUID = 1002
		f.calls = nil
		if _, err := f.runner.Undo(context.Background(), res.Entry.ID); err == nil || !strings.Contains(err.Error(), "lock yourself") || len(f.calls) != 0 {
			t.Fatalf("err %v calls %q", err, f.calls)
		}
	})
}

func TestKeyOnlyAccountLockUnlockAndUndos(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.user("demo-user").hash = "!"
	f.expires["demo-user"] = "30000"
	f.flush()
	lock := f.apply(t, ActionUserLock, "", "demo-user")
	if f.user("demo-user").hash != "!" || f.expires["demo-user"] != "1" {
		t.Fatalf("%+v %v", f.user("demo-user"), f.expires)
	}
	unlock := f.apply(t, ActionUserUnlock, "", "demo-user")
	if f.user("demo-user").hash != "!" || f.expires["demo-user"] != "" || unlock.Entry.Check != CheckPassed {
		t.Fatalf("%+v %v", f.user("demo-user"), f.expires)
	}
	f.calls = nil
	f.undo(t, unlock.Entry.ID)
	sameList(t, "undo unlock", f.calls, []string{"usermod -e 1 demo-user"})
	if f.user("demo-user").hash != "!" || f.expires["demo-user"] != "1" {
		t.Fatalf("%+v %v", f.user("demo-user"), f.expires)
	}
	f.calls = nil
	f.undo(t, lock.Entry.ID)
	sameList(t, "undo lock", f.calls, []string{"usermod -e 30000 demo-user"})
	if f.user("demo-user").hash != "!" || f.expires["demo-user"] != "30000" {
		t.Fatalf("%+v %v", f.user("demo-user"), f.expires)
	}
}

func TestKeyOnlyAdminCountsAsRemaining(t *testing.T) {
	f := newUserFixture(t, 1001)
	f.group("sysmin").members = []string{"demo-other"}
	f.user("demo-other").hash = "!"
	f.flush()
	// demo-other has no password but can log in by key, so demo-admin may be locked.
	f.apply(t, ActionUserLock, "", "demo-admin")
}

func TestLockUndoIsRefusedWhenThePasswordChanged(t *testing.T) {
	f := newUserFixture(t, 1000)
	res := f.apply(t, ActionUserLock, "", "demo-user")
	f.user("demo-user").hash = "!$6$demo-new"
	f.flush()
	f.calls = nil
	if _, err := f.runner.Undo(context.Background(), res.Entry.ID); err == nil || !strings.Contains(err.Error(), "changed since") || len(f.calls) != 0 {
		t.Fatalf("err %v calls %q", err, f.calls)
	}
}

func TestInverseLockRejectsInvalidStates(t *testing.T) {
	u := userActions{}
	for name, state := range map[string]string{
		"name":     `{"name":"../x","uid":1001,"hash":"$6$x","expire":""}`,
		"uid":      `{"name":"demo-user","uid":-1,"hash":"$6$x","expire":""}`,
		"hash":     `{"name":"demo-user","uid":1001,"hash":"","expire":""}`,
		"hash:":    `{"name":"demo-user","uid":1001,"hash":"a:b","expire":""}`,
		"expiry":   `{"name":"demo-user","uid":1001,"hash":"$6$x","expire":"-5"}`,
		"expiry 2": `{"name":"demo-user","uid":1001,"hash":"$6$x","expire":"2030-01-01"}`,
		"unknown":  `{"name":"demo-user","uid":1001,"hash":"$6$x","expire":"","more":1}`,
		"not json": `[]`,
	} {
		if _, err := u.inverseLock(json.RawMessage(state)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
