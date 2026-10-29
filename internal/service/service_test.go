package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const (
	testPass  = "example-passphrase-1"
	wrongPass = "example-wrong-pass-9"
)

type groupOfTests struct{}

func (groupOfTests) GroupID(string) (uint32, error)           { return uint32(os.Getgid()), nil }
func (groupOfTests) PeerGroups(daemon.Peer) ([]uint32, error) { return nil, nil }

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type env struct {
	cfg    Config
	svc    *Service
	clock  *testClock
	cancel context.CancelFunc
	done   chan error
}

// newEnv creates a vault with testPass (when create is set) and starts a service on it.
func newEnv(t *testing.T, create, bind bool) *env {
	t.Helper()
	vault.LowerWorkFactorForTests()
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	sock, err := os.MkdirTemp("", "sysmin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sock) })
	state := t.TempDir()
	e := &env{clock: &testClock{t: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}, done: make(chan error, 1)}
	e.cfg = Config{
		ConfigDir: t.TempDir(), StateDir: state, SocketPath: filepath.Join(sock, "s.sock"),
		Version: "test", Resolver: groupOfTests{}, Clock: e.clock.now,
	}
	if create {
		v := vault.New(filepath.Join(state, "vault"))
		if err := v.Create(testPass); err != nil {
			t.Fatal(err)
		}
		if bind {
			id, err := v.Identity()
			if err != nil {
				t.Fatal(err)
			}
			cd := t.TempDir()
			if err := os.WriteFile(filepath.Join(cd, "vault-key"), []byte(id.String()+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			e.cfg.CredentialsDir = cd
		}
	}
	e.svc, err = Open(e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) start(t *testing.T) {
	t.Helper()
	if err := e.svc.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	go func() { e.done <- e.svc.Serve(ctx) }()
	t.Cleanup(func() { cancel(); <-e.done })
}

func (e *env) dial(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.Dial(e.cfg.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var ce *control.Error
	if !errors.As(err, &ce) || ce.Code != code {
		t.Fatalf("err = %v, want code %q", err, code)
	}
}

func TestStartsLockedWithoutBinding(t *testing.T) {
	e := newEnv(t, true, false)
	e.start(t)
	st, err := e.dial(t).Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Vault != "locked" || st.Unlock != "manual" || st.Socket != e.cfg.SocketPath || st.Version != "test" || st.Warning != "" {
		t.Fatalf("status = %+v", st)
	}
	vs, err := e.dial(t).VaultStatus()
	if err != nil || vs.Vault != "locked" {
		t.Fatalf("vault status = %+v, %v", vs, err)
	}
}

func TestAbsentVault(t *testing.T) {
	e := newEnv(t, false, false)
	e.start(t)
	c := e.dial(t)
	if st, _ := c.Status(); st.Vault != "absent" {
		t.Fatalf("status = %+v", st)
	}
	wantCode(t, c.Unlock(testPass), control.CodeVaultAbsent)
}

func TestStartsUnlockedWithBinding(t *testing.T) {
	e := newEnv(t, true, true)
	e.start(t)
	st, err := e.dial(t).Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Vault != "unlocked" || st.Unlock != "host" || st.Warning == "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestUnlock(t *testing.T) {
	e := newEnv(t, true, false)
	e.start(t)
	c := e.dial(t)
	wantCode(t, c.Unlock(wrongPass), control.CodeWrongPassphrase)
	if st, _ := c.Status(); st.Vault != "locked" {
		t.Fatalf("status after wrong passphrase = %+v", st)
	}
	if err := c.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	st, _ := c.Status()
	if st.Vault != "unlocked" || st.Unlock != "manual" {
		t.Fatalf("status = %+v", st)
	}
	if err := c.Unlock(testPass); err != nil {
		t.Fatalf("second unlock: %v", err)
	}
	var ce *control.Error
	if err := c.Call(control.OpUnlock, nil, nil); !errors.As(err, &ce) || ce.Code != control.CodeBadRequest {
		t.Fatalf("missing passphrase: %v", err)
	}
}

func TestSessionAndGuard(t *testing.T) {
	e := newEnv(t, true, false)
	ran := 0
	if err := e.svc.Server().Register("change", daemon.Handler{
		Action: auditlog.OperationConfigApply, Mutating: true,
		Func: func(context.Context, *daemon.Conn, json.RawMessage) (any, error) { ran++; return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	e.start(t)
	c := e.dial(t)

	// Locked: the guard refuses before it asks about a session.
	wantCode(t, c.Call("change", nil, nil), control.CodeLocked)
	if err := c.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	// Unlocked without a session.
	wantCode(t, c.Call("change", nil, nil), control.CodeAdminRequired)

	wantCode(t, c.OpenSession(wrongPass), control.CodeWrongPassphrase)
	wantCode(t, c.Call("change", nil, nil), control.CodeAdminRequired)

	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	if err := c.Call("change", nil, nil); err != nil || ran != 1 {
		t.Fatalf("with session: %v, ran = %d", err, ran)
	}

	// The session belongs to its connection.
	wantCode(t, e.dial(t).Call("change", nil, nil), control.CodeAdminRequired)

	// It ends after the idle timeout, which activity renews.
	e.clock.add(10 * time.Minute)
	if err := c.Call("change", nil, nil); err != nil {
		t.Fatalf("renewed: %v", err)
	}
	e.clock.add(10 * time.Minute)
	if err := c.Call("change", nil, nil); err != nil {
		t.Fatalf("renewed again: %v", err)
	}
	e.clock.add(config15m + time.Second)
	wantCode(t, c.Call("change", nil, nil), control.CodeAdminRequired)

	// session.end ends it at once.
	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	if err := c.EndSession(); err != nil {
		t.Fatal(err)
	}
	wantCode(t, c.Call("change", nil, nil), control.CodeAdminRequired)
	if ran != 3 {
		t.Fatalf("ran = %d", ran)
	}
}

const config15m = 15 * time.Minute

func TestAdminTimeoutFromSettings(t *testing.T) {
	e := newEnv(t, true, true)
	e.start(t)
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(e.cfg.ConfigDir, "sysmin.yaml"), []byte(s), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if got := e.svc.adminTimeout(); got != config15m {
		t.Fatalf("missing settings: %v", got)
	}
	write("schema: 1\nvault:\n  admin_timeout: 2m\n")
	if got := e.svc.adminTimeout(); got != 2*time.Minute {
		t.Fatalf("configured: %v", got)
	}
	write("schema: 1\nvault:\n  admin_timeout: 1ms\n")
	if got := e.svc.adminTimeout(); got != config15m {
		t.Fatalf("implausible: %v", got)
	}
	write("schema: 1\nvault:\n  admin_timeout: 0\n")
	if got := e.svc.adminTimeout(); got != 0 {
		t.Fatalf("zero: %v", got)
	}
	write("not: [valid")
	if got := e.svc.adminTimeout(); got != config15m {
		t.Fatalf("broken: %v", got)
	}
}

func TestAuditEntriesAreSignedOnlyAfterUnlock(t *testing.T) {
	e := newEnv(t, true, false)
	e.start(t)
	c := e.dial(t)
	_, _ = c.Status()
	wantCode(t, c.Unlock(wrongPass), control.CodeWrongPassphrase)
	if err := c.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	_, _ = c.Status()
	e.cancel()
	<-e.done
	e.done <- nil

	logDir := filepath.Join(e.cfg.StateDir, "logs")
	entries := readEntries(t, logDir)
	want := []struct {
		action, result, code string
		signed               bool
	}{
		{"control.status", "success", "", false},
		{"vault.unlock", "failure", "wrong-passphrase", false},
		{"vault.unlock", "success", "", true}, // the key is set before the request is recorded
		{"session.open", "success", "", true},
		{"control.status", "success", "", true},
	}
	if len(entries) != len(want) {
		t.Fatalf("entries = %d, want %d: %+v", len(entries), len(want), entries)
	}
	for i, w := range want {
		g := entries[i]
		if g.Action != w.action || g.Result != w.result || g.ErrorCode != w.code || g.ClientUID == nil || *g.ClientUID != uint32(os.Getuid()) {
			t.Errorf("entry %d = %+v, want %+v", i, g, w)
		}
		if (g.MAC != "") != w.signed {
			t.Errorf("entry %d signed = %v, want %v", i, g.MAC != "", w.signed)
		}
	}

	// The signed entries verify with the vault's log key.
	v := vault.New(filepath.Join(e.cfg.StateDir, "vault"))
	if _, err := v.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	key, err := v.LogKey()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := auditlog.Verify(logDir, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range rep.Days {
		if d.Problem != "" {
			t.Fatalf("day %s: %s", d.Date, d.Problem)
		}
	}

	// No passphrase anywhere in the log.
	filepath.WalkDir(e.cfg.StateDir, func(p string, d os.DirEntry, _ error) error {
		if strings.Contains(p, "vault") || d.IsDir() {
			return nil
		}
		data, _ := os.ReadFile(p)
		if strings.Contains(string(data), testPass) || strings.Contains(string(data), wrongPass) {
			t.Errorf("%s contains a passphrase", p)
		}
		return nil
	})
}

func readEntries(t *testing.T, dir string) []auditlog.Entry {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	var out []auditlog.Entry
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			var en auditlog.Entry
			if err := json.Unmarshal([]byte(line), &en); err != nil {
				t.Fatal(err)
			}
			out = append(out, en)
		}
	}
	return out
}

func TestUnlockedStartSignsFromTheFirstEntry(t *testing.T) {
	e := newEnv(t, true, true)
	e.start(t)
	_, _ = e.dial(t).Status()
	e.cancel()
	<-e.done
	e.done <- nil
	entries := readEntries(t, filepath.Join(e.cfg.StateDir, "logs"))
	if len(entries) != 1 || entries[0].MAC == "" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestShutdownLocksAndRemovesSocket(t *testing.T) {
	e := newEnv(t, true, true)
	e.start(t)
	e.dial(t)
	e.cancel()
	if err := <-e.done; err != nil {
		t.Fatal(err)
	}
	e.done <- nil
	if _, err := os.Lstat(e.cfg.SocketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remains: %v", err)
	}
	if st, _ := e.svc.vault.State(); st == vault.StateUnlocked {
		t.Fatal("vault still unlocked")
	}
}

func TestAdminTimeoutZeroAsksEveryTime(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		reuse      bool
	}{{"zero", "schema: 1\nvault:\n  admin_timeout: 0\n", false}, {"default", "schema: 1\n", true}} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, true, false)
			if err := e.svc.Server().Register("change", daemon.Handler{
				Action: auditlog.OperationConfigApply, Mutating: true,
				Func: func(context.Context, *daemon.Conn, json.RawMessage) (any, error) { return nil, nil },
			}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(e.cfg.ConfigDir, "sysmin.yaml"), []byte(tc.yaml), 0o640); err != nil {
				t.Fatal(err)
			}
			e.start(t)
			c := e.dial(t)
			if err := c.Unlock(testPass); err != nil {
				t.Fatal(err)
			}
			if err := c.OpenSession(testPass); err != nil {
				t.Fatal(err)
			}
			// With timeout 0 one passphrase covers exactly one change; without it the window applies.
			for i := 0; i < 2; i++ {
				err := c.Call("change", nil, nil)
				if (tc.reuse || i == 0) && err != nil {
					t.Fatalf("call %d: %v", i, err)
				}
				if !tc.reuse && i == 1 {
					wantCode(t, err, control.CodeAdminRequired)
				}
			}
			if !tc.reuse {
				if err := c.OpenSession(testPass); err != nil {
					t.Fatal(err)
				}
				if err := c.Call("change", nil, nil); err != nil {
					t.Fatalf("after reopen: %v", err)
				}
				wantCode(t, c.Call("change", nil, nil), control.CodeAdminRequired)
				if err := c.OpenSession(testPass); err != nil {
					t.Fatal(err)
				}
				if err := c.EndSession(); err != nil {
					t.Fatal(err)
				}
				wantCode(t, c.Call("change", nil, nil), control.CodeAdminRequired)
			}
		})
	}
}
