package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

type groupOfTests struct{ admin bool }

func (groupOfTests) GroupID(string) (uint32, error)           { return uint32(os.Getgid()), nil }
func (groupOfTests) PeerGroups(daemon.Peer) ([]uint32, error) { return nil, nil }
func (g groupOfTests) IsAdmin(uint32) (bool, error)           { return g.admin, nil }

type env struct {
	cfg    Config
	svc    *Service
	cancel context.CancelFunc
	done   chan error
}

// newEnv opens a service on an empty state directory; nothing is set up beforehand.
func newEnv(t *testing.T, mods ...func(*Config)) *env {
	t.Helper()
	sock, err := os.MkdirTemp("", "sysmin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sock) })
	e := &env{done: make(chan error, 1)}
	e.cfg = Config{
		StateDir: t.TempDir(), SocketPath: filepath.Join(sock, "s.sock"),
		Version: "test", Resolver: groupOfTests{admin: true},
	}
	for _, mod := range mods {
		mod(&e.cfg)
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

// A fresh installation needs no setup: the first request of an administrator is answered at once.
func TestAnswersAnAdminOnAFreshStateDirectory(t *testing.T) {
	e := newEnv(t)
	e.start(t)
	st, err := e.dial(t).Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Socket != e.cfg.SocketPath || st.Version != "test" || st.Protocol != control.Version {
		t.Fatalf("status = %+v", st)
	}
}

func TestRefusesAPeerThatIsNoAdmin(t *testing.T) {
	e := newEnv(t, func(cfg *Config) { cfg.Resolver = groupOfTests{admin: false} })
	e.start(t)
	if _, err := e.dial(t).Status(); !errors.Is(err, client.ErrReserved) {
		t.Fatalf("err = %v, want %v", err, client.ErrReserved)
	}
}

func TestAuditEntriesCarryNoSignature(t *testing.T) {
	e := newEnv(t)
	e.start(t)
	c := e.dial(t)
	_, _ = c.Status()
	_ = c.Call("no.such-op", nil, nil)
	_, _ = c.Status()
	e.cancel()
	<-e.done
	e.done <- nil

	logDir := filepath.Join(e.cfg.StateDir, "logs")
	entries := readEntries(t, logDir)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3: %+v", len(entries), entries)
	}
	for i, g := range entries {
		if g.ClientUID == nil || *g.ClientUID != uint32(os.Getuid()) || g.MAC != "" {
			t.Errorf("entry %d = %+v", i, g)
		}
	}
	files, _ := filepath.Glob(filepath.Join(logDir, "*.jsonl"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		if strings.Contains(string(data), `"mac"`) {
			t.Errorf("%s carries a mac field", f)
		}
	}
	rep, err := auditlog.Verify(logDir)
	if err != nil || rep.Broken {
		t.Fatalf("verify = %+v, %v", rep, err)
	}
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

func TestShutdownRemovesSocket(t *testing.T) {
	e := newEnv(t)
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
}

func TestServesOnlyChannelLogAndHostOperations(t *testing.T) {
	e := newEnv(t, asPeer(0))
	e.start(t)
	c := e.dial(t)
	if _, err := c.Status(); err != nil {
		t.Fatal(err)
	}
	if err := c.RecordHostChange(control.HostRecordParams{
		Action: "demo.set", Result: control.HostResultApplied, SudoUID: 1000, JournalID: testJournalID,
	}); err != nil {
		t.Fatal(err)
	}
	if res, err := c.LogEntries(control.LogListParams{}); err != nil || len(res.Entries) == 0 {
		t.Fatalf("log.list = %+v, %v", res, err)
	}
	if res, err := c.VerifyLog(); err != nil || !res.Intact {
		t.Fatalf("log.verify = %+v, %v", res, err)
	}
	for _, op := range []string{
		"vault.status", "unlock", "session.open", "session.end", "vault.create", "vault.passphrase",
		"settings.show", "settings.admin-timeout", "projects.list", "deploy.status",
	} {
		wantCode(t, c.Call(op, nil, nil), control.CodeUnknownOp)
	}
}
