package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const demoHookSecret = "demo-hook-secret-0001"

type hookServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newHookServer(t *testing.T, status int) *hookServer {
	t.Helper()
	h := &hookServer{}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.bodies = append(h.bodies, string(b))
		h.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *hookServer) received() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.bodies...)
}

func TestNotifyTestSendsToApprovedChannel(t *testing.T) {
	e := approvedEnv(t)
	hook := newHookServer(t, 200)
	if _, err := e.svc.vault.Add("demo-hook", vault.KindSecret, map[string]string{vault.FieldValue: demoHookSecret}); err != nil {
		t.Fatal(err)
	}
	admin := e.admin(t)
	if res, err := admin.AddChannel(control.ChannelParams{Name: "ops-hook", Type: "webhook", URL: hook.URL, Credential: "demo-hook"}); err != nil || res.State != control.StateApproved {
		t.Fatalf("add = %+v, %v", res, err)
	}

	// Reading needs no admin session.
	res, err := e.dial(t).TestChannel("ops-hook")
	if err != nil || res.Channel != "ops-hook" || res.Event != "notify.test" {
		t.Fatalf("test = %+v, %v", res, err)
	}
	got := hook.received()
	if len(got) != 1 || !strings.Contains(got[0], `"notify.test"`) || strings.Contains(got[0], demoHookSecret) {
		t.Fatalf("received = %q", got)
	}
}

func TestNotifyTestRefusesUnapprovedAndUnknownChannel(t *testing.T) {
	e := approvedEnv(t)
	hook := newHookServer(t, 200)
	c := e.dial(t)
	if _, err := c.AddChannel(control.ChannelParams{Name: "ops-hook", Type: "webhook", URL: hook.URL}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ops-hook", "no-such"} {
		_, err := c.TestChannel(name)
		wantCode(t, err, control.CodeNotFound)
	}
	_, err := c.TestChannel("Bad Name")
	wantCode(t, err, control.CodeBadRequest)
	if got := hook.received(); len(got) != 0 {
		t.Fatalf("received = %q", got)
	}
}

func TestNotifyTestFailureHasOnlyACode(t *testing.T) {
	e := approvedEnv(t)
	hook := newHookServer(t, 500)
	if _, err := e.admin(t).AddChannel(control.ChannelParams{Name: "ops-hook", Type: "webhook", URL: hook.URL}); err != nil {
		t.Fatal(err)
	}
	_, err := e.dial(t).TestChannel("ops-hook")
	wantCode(t, err, control.CodeSendFailed)
	if got := hook.received(); len(got) != 1 {
		t.Fatalf("received = %q", got)
	}
}

func TestNotifyTestNeedsAnUnlockedVault(t *testing.T) {
	e := newEnv(t, true, false)
	e.start(t)
	_, err := e.dial(t).TestChannel("ops-hook")
	wantCode(t, err, control.CodeLocked)
}
