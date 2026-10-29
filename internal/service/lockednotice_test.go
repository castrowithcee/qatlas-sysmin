package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// hookReceiver records the bodies and signature headers it gets.
func hookReceiver(t *testing.T) (*httptest.Server, func() ([]string, []string)) {
	t.Helper()
	var mu sync.Mutex
	var bodies, sigs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		sigs = append(sigs, r.Header.Get("X-Sysmin-Signature"))
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return srv, func() ([]string, []string) {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...), append([]string(nil), sigs...)
	}
}

// approveHook approves one webhook channel without credential in the vault of cfg, before the service
// opens.
func approveHook(t *testing.T, url string) func(*Config) {
	return func(cfg *Config) {
		v := vault.New(filepath.Join(cfg.StateDir, "vault"))
		if _, err := v.Unlock(testPass); err != nil {
			t.Fatal(err)
		}
		defer v.Forget()
		settings := "schema: 1\nchannels:\n  - name: ops-hook\n    type: webhook\n    url: " + url + "\n"
		if err := os.WriteFile(filepath.Join(cfg.ConfigDir, "sysmin.yaml"), []byte(settings), 0o640); err != nil {
			t.Fatal(err)
		}
		m := approval.New(config.NewStore(cfg.ConfigDir), v, cfg.StateDir)
		if err := m.Approve(approval.ChannelsName, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStartLockedReportsVaultLockedOnce(t *testing.T) {
	srv, got := hookReceiver(t)
	e := newEnv(t, true, false, approveHook(t, srv.URL))
	e.start(t)
	e.svc.notifier.Wait()
	bodies, sigs := got()
	if len(bodies) != 1 || sigs[0] != "" {
		t.Fatalf("messages = %d, signatures = %q", len(bodies), sigs)
	}
	var p struct {
		Event string `json:"event"`
		Host  string `json:"host"`
		Time  string `json:"time"`
	}
	if err := json.Unmarshal([]byte(bodies[0]), &p); err != nil {
		t.Fatal(err)
	}
	host, _ := os.Hostname()
	if p.Event != "vault.locked" || p.Host != host || p.Time == "" || strings.Contains(bodies[0], testPass) {
		t.Errorf("payload = %s", bodies[0])
	}
}

func TestStartUnlockedOrAbsentReportsNothing(t *testing.T) {
	srv, got := hookReceiver(t)
	e := newEnv(t, true, true, approveHook(t, srv.URL))
	e.start(t)
	e.svc.notifier.Wait()
	if bodies, _ := got(); len(bodies) != 0 {
		t.Errorf("unlocked start sent %d messages", len(bodies))
	}

	e = newEnv(t, false, false)
	e.start(t)
	e.svc.notifier.Wait()
	if bodies, _ := got(); len(bodies) != 0 {
		t.Errorf("absent vault sent %d messages", len(bodies))
	}
	// Without approved channels nothing could arrive anyway, so the audit log shows that no send was tried.
	for _, en := range readEntries(t, filepath.Join(e.cfg.StateDir, "logs")) {
		if en.Action == "notify.send" {
			t.Errorf("absent vault tried to send: %+v", en)
		}
	}
}
