package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

const demoNotifySecret = "demo-notify-secret-0001"

func TestNotifyTestSendsAndHidesTheSecret(t *testing.T) {
	var hits atomic.Int32
	var event atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		event.Store(r.Header.Get("X-Sysmin-Event"))
	}))
	t.Cleanup(srv.Close)
	sock := startServiceWith(t, true, func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "sysmin.yaml"), []byte("schema: 1\n"), 0o640); err != nil {
			t.Fatal(err)
		}
	})

	c, err := client.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.OpenSession("example-passphrase-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetCredential(control.CredentialSetParams{Name: "demo-hook", Kind: "secret", Fields: map[string]string{"value": demoNotifySecret}}); err != nil {
		t.Fatal(err)
	}
	if res, err := c.AddChannel(control.ChannelParams{Name: "ops-hook", Type: "webhook", URL: srv.URL, Credential: "demo-hook"}); err != nil || res.State != control.StateApproved {
		t.Fatalf("add = %+v, %v", res, err)
	}

	code, stdout, stderr := runCLIArgs("notify", "test", "ops-hook", "--socket", sock)
	if code != exitOK || !strings.Contains(stdout, "sent") || hits.Load() != 1 || event.Load() != "notify.test" {
		t.Fatalf("code = %d, stdout = %q, stderr = %q, hits = %d", code, stdout, stderr, hits.Load())
	}
	code, stdout, stderr = runCLIArgs("notify", "test", "ops-hook", "--socket", sock, "--format", "json")
	if code != exitOK || !strings.Contains(stdout, `"result":"sent"`) || hits.Load() != 2 {
		t.Fatalf("json: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if all := stdout + stderr; strings.Contains(all, demoNotifySecret) {
		t.Fatalf("output holds the secret: %q", all)
	}

	// A failing channel and a missing one end with an error and no secret.
	srv.Close()
	code, stdout, stderr = runCLIArgs("notify", "test", "ops-hook", "--socket", sock)
	if code == exitOK || !strings.Contains(stderr, "did not take") || strings.Contains(stdout+stderr, demoNotifySecret) {
		t.Fatalf("failure: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, _, stderr = runCLIArgs("notify", "test", "no-such", "--socket", sock)
	if code == exitOK || !strings.Contains(stderr, "no approved channel") {
		t.Fatalf("missing: code = %d, stderr = %q", code, stderr)
	}
}

func TestNotifyTestWithLockedVault(t *testing.T) {
	sock := startService(t, false)
	code, _, stderr := runCLIArgs("notify", "test", "ops-hook", "--socket", sock)
	if code == exitOK || !strings.Contains(stderr, "locked") {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
}
