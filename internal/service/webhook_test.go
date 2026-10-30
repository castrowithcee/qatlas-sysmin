package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const webhookSecret = "example-webhook-secret"

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func listening(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func postHook(t *testing.T, addr, project, body, sig string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/hooks/"+project, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if sig != "" {
		req.Header.Set("X-Hub-Signature-256", sig)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestWebhookListenerFollowsTheApprovedWebhooksAndEndsWithServe(t *testing.T) {
	f := newDeployFixture(t, true, func(cfg *Config) { cfg.WebhookRecheck = 20 * time.Millisecond })
	addr := freeAddr(t)
	f.write(t, "sysmin.yaml", fmt.Sprintf("schema: 1\nwebhook:\n  listen: %s\n", addr))
	admin := f.admin(t)

	// No project has a webhook: nothing listens, even though everything is approved.
	if _, err := approveAll(t, admin); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if listening(addr) {
		t.Fatal("the listener runs without a webhook")
	}

	// A webhook that is written but not approved changes nothing.
	if _, err := f.svc.vault.Add("demo-hook", vault.KindSecret, map[string]string{vault.FieldValue: webhookSecret}); err != nil {
		t.Fatal(err)
	}
	f.write(t, "projects/demo-app.yaml", deployProject("demo-app", f.dirs["demo-app"])+"webhook:\n  kind: github\n  secret: demo-hook\n")
	time.Sleep(100 * time.Millisecond)
	if listening(addr) {
		t.Fatal("the listener runs for an unapproved webhook")
	}

	if _, err := approveAll(t, admin); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the listener", func() bool { return listening(addr) })

	body := `{"ref":"example"}`
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write([]byte(body))
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := postHook(t, addr, "demo-app", body, "sha256=00"); got != http.StatusUnauthorized {
		t.Errorf("bad signature = %d", got)
	}
	if got := postHook(t, addr, "demo-two", body, good); got != http.StatusNotFound {
		t.Errorf("project without webhook = %d", got)
	}
	if n := f.runner.count("deploy"); n != 0 {
		t.Fatalf("a rejected call deployed: %v", f.runner.calls())
	}
	if got := postHook(t, addr, "demo-app", body, good); got != http.StatusAccepted {
		t.Fatalf("valid call = %d", got)
	}
	waitFor(t, "the triggered check", func() bool { return f.runner.count("deploy demo-app '2.0.0'") == 1 })

	f.cancel()
	if err := <-f.done; err != nil {
		t.Fatal(err)
	}
	f.done <- nil
	if listening(addr) {
		t.Error("the listener outlives Serve")
	}

	type key struct{ action, project, result, code string }
	got := map[key]bool{}
	for _, en := range readEntries(t, filepath.Join(f.cfg.StateDir, "logs")) {
		got[key{en.Action, en.Project, en.Result, en.ErrorCode}] = true
	}
	for _, k := range []key{
		{auditlog.OperationWebhookReceive, "demo-app", auditlog.ResultSuccess, ""},
		{auditlog.OperationWebhookReject, "demo-app", auditlog.ResultFailure, "invalid-signature"},
		{auditlog.OperationWebhookReject, "", auditlog.ResultFailure, "not-found"},
	} {
		if !got[k] {
			t.Errorf("no entry for %+v in %v", k, got)
		}
	}
}

func TestWebhookWithLockedVaultAnswers401AndStartsAfterUnlock(t *testing.T) {
	f := newDeployFixture(t, false, func(cfg *Config) { cfg.WebhookRecheck = time.Hour })
	addr := freeAddr(t)
	f.write(t, "sysmin.yaml", fmt.Sprintf("schema: 1\nwebhook:\n  listen: %s\n", addr))
	f.write(t, "projects/demo-app.yaml", deployProject("demo-app", f.dirs["demo-app"])+"webhook:\n  kind: github\n  secret: demo-hook\n")
	time.Sleep(100 * time.Millisecond)
	if listening(addr) {
		t.Fatal("the listener runs with a locked vault")
	}

	if err := f.dial(t).Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	f.addCredential(t)
	if _, err := f.svc.vault.Add("demo-hook", vault.KindSecret, map[string]string{vault.FieldValue: webhookSecret}); err != nil {
		t.Fatal(err)
	}
	if _, err := approveAll(t, f.admin(t)); err != nil {
		t.Fatal(err)
	}
	// The unlock woke the supervisor before the approval; the long recheck interval would leave the
	// listener off, so wake it again as the next unlock would.
	f.svc.wakeWebhook()
	waitFor(t, "the listener", func() bool { return listening(addr) })

	f.svc.vault.Forget()
	if got := postHook(t, addr, "demo-app", "{}", "sha256=00"); got != http.StatusUnauthorized {
		t.Errorf("locked vault = %d", got)
	}
	if n := f.runner.count("deploy"); n != 0 {
		t.Errorf("deployed: %v", f.runner.calls())
	}
}
