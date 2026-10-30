package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
	"github.com/castrowithcee/qatlas-sysmin/internal/release/releasetest"
	"github.com/castrowithcee/qatlas-sysmin/internal/selfupdate"
	"github.com/castrowithcee/qatlas-sysmin/internal/selfupdate/selfupdatetest"
)

type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitStatus) ExitCode() int { return int(e) }

// fakeHost is a system that records commands and never touches the real host.
type fakeHost struct {
	system
	dir      string
	root     bool
	terminal bool
	calls    []string
	fail     map[string]error
	server   *selfupdatetest.Server
	keys     []ssh.PublicKey
}

// noNetwork fails the test on any request: a test without a release server must never reach the network.
type noNetwork struct{ t *testing.T }

func (n noNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	n.t.Errorf("unexpected request to %s", r.URL)
	return nil, fmt.Errorf("no network in this test")
}

func newFakeHost(t *testing.T, server *selfupdatetest.Server) *fakeHost {
	t.Helper()
	dir := t.TempDir()
	h := &fakeHost{dir: dir, root: true, fail: map[string]error{}, server: server}
	h.system = system{
		euid:        func() int { return map[bool]int{true: 0, false: 1000}[h.root] },
		interactive: func(io.Reader) bool { return h.terminal },
		command: func(_ context.Context, name string, args ...string) error {
			call := strings.TrimSpace(name + " " + strings.Join(args, " "))
			h.calls = append(h.calls, call)
			return h.fail[call]
		},
		binaryPath: filepath.Join(dir, "bin", "sysmin"),
		unitPath:   filepath.Join(dir, "sysmin.service"),
		dataDirs:   []string{filepath.Join(dir, "etc"), filepath.Join(dir, "var")},
	}
	h.system.updater = func(v, apiURL string) *selfupdate.Client {
		c := selfupdate.New(v, apiURL)
		c.HTTPClient = &http.Client{Transport: noNetwork{t}}
		return c
	}
	if server != nil {
		h.system.updater = func(v, apiURL string) *selfupdate.Client {
			c := selfupdate.New(v, apiURL)
			c.GOARCH = "amd64"
			c.SigningKeys = []ssh.PublicKey{server.Signer.PublicKey()}
			return c
		}
	}
	for _, d := range append([]string{filepath.Dir(h.binaryPath)}, h.dataDirs...) {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(h.binaryPath, []byte("old program"), 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *fakeHost) run(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	opts := &Options{Redactor: &redact.Redactor{}, sys: &h.system}
	cmd := newRootCommand(opts)
	cmd.SetIn(strings.NewReader(stdin))
	var out, errOut bytes.Buffer
	code := run(cmd, opts, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func withVersion(t *testing.T, v string) {
	t.Helper()
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

func binary(t *testing.T, h *fakeHost) string {
	t.Helper()
	b, err := os.ReadFile(h.binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUpdateInstallsThenRestarts(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new program"}))
	code, stdout, stderr := h.run(t, "", "update", "--yes", "--api-url", h.server.URL, "--format", "json")
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if binary(t, h) != "new program" {
		t.Error("program not replaced")
	}
	if want := []string{"systemctl daemon-reload", "systemctl restart sysmin"}; !slices.Equal(h.calls, want) {
		t.Errorf("calls = %v, want %v", h.calls, want)
	}
	if b, _ := os.ReadFile(h.unitPath); !strings.Contains(string(b), "v1.1.0") {
		t.Errorf("unit = %q", b)
	}
	for _, want := range []string{`"unit_updated":true`, `"previous":"v1.0.0"`, `"version":"v1.1.0"`, `"updated":true`, `"restarted":true`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q lacks %s", stdout, want)
		}
	}
	if !strings.Contains(stderr, "v1.1.0") || !strings.Contains(stderr, "vault is locked") {
		t.Errorf("stderr %q lacks the confirmation text", stderr)
	}
}

func TestUpdateRefusesBadReleaseWithoutChangeOrRestart(t *testing.T) {
	withVersion(t, "v1.0.0")
	foreign := releasetest.NewSigner(t)
	for name, rel := range map[string]selfupdatetest.Release{
		"checksum":  {Tag: "v1.1.0", Program: "x", BadChecksum: true},
		"namespace": {Tag: "v1.1.0", Program: "x", Namespace: "other-namespace"},
		"key":       {Tag: "v1.1.0", Program: "x", Signer: foreign},
	} {
		t.Run(name, func(t *testing.T) {
			h := newFakeHost(t, selfupdatetest.New(t, "amd64", rel))
			code, _, _ := h.run(t, "", "update", "--yes", "--api-url", h.server.URL)
			if code != exitRuntime {
				t.Errorf("code = %d", code)
			}
			if binary(t, h) != "old program" || len(h.calls) != 0 {
				t.Errorf("program = %q, calls = %v", binary(t, h), h.calls)
			}
		})
	}
}

func TestUpdateRestartFailureIsReported(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new"}))
	h.fail["systemctl restart sysmin"] = exitStatus(1)
	code, _, stderr := h.run(t, "", "update", "--yes", "--api-url", h.server.URL)
	if code != exitRuntime || !strings.Contains(stderr, "restarting the service failed") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestUpdateRejectsNonRootBeforeAnyRequest(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, nil)
	h.root = false
	code, _, stderr := h.run(t, "", "update", "--yes", "--api-url", "http://127.0.0.1:1")
	if code != exitRuntime || !strings.Contains(stderr, "root") || len(h.calls) != 0 {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestUpdateNeedsConfirmationChannel(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, nil)
	code, _, stderr := h.run(t, "", "update", "--api-url", "http://127.0.0.1:1")
	if code != exitUsage || !strings.Contains(stderr, "--yes") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestUpdateAsksOnTerminal(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new program"}))
	h.terminal = true
	code, _, stderr := h.run(t, "n\n", "update", "--api-url", h.server.URL)
	if code != exitRuntime || !strings.Contains(stderr, "cancelled") || binary(t, h) != "old program" || len(h.calls) != 0 {
		t.Errorf("declined: code = %d, stderr = %q", code, stderr)
	}
	if code, _, stderr := h.run(t, "y\n", "update", "--api-url", h.server.URL); code != exitOK || binary(t, h) != "new program" {
		t.Errorf("accepted: code = %d, stderr = %q", code, stderr)
	}
}

func TestUpdateDevBuildAndUpToDate(t *testing.T) {
	h := newFakeHost(t, selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.0.0", Program: "x"}))
	withVersion(t, "dev")
	if code, _, stderr := h.run(t, "", "update", "--yes", "--api-url", h.server.URL); code != exitRuntime || !strings.Contains(stderr, "dev build") {
		t.Errorf("dev: code = %d, stderr = %q", code, stderr)
	}
	version = "v1.0.0"
	code, stdout, _ := h.run(t, "", "update", "--yes", "--api-url", h.server.URL, "--format", "json")
	if code != exitOK || !strings.Contains(stdout, `"updated":false`) || len(h.calls) != 0 || binary(t, h) != "old program" {
		t.Errorf("up to date: code = %d, stdout = %q, calls = %v", code, stdout, h.calls)
	}
}

func TestUpdateToChosenVersion(t *testing.T) {
	withVersion(t, "v1.2.0")
	h := newFakeHost(t, selfupdatetest.New(t, "amd64",
		selfupdatetest.Release{Tag: "v1.0.0", Program: "program one"},
		selfupdatetest.Release{Tag: "v1.2.0", Program: "program two"}))
	code, _, stderr := h.run(t, "", "update", "--to", "v1.0.0", "--yes", "--api-url", h.server.URL)
	if code != exitOK || binary(t, h) != "program one" || !strings.Contains(stderr, "Roll back") {
		t.Errorf("code = %d, program = %q, stderr = %q", code, binary(t, h), stderr)
	}
	if code, _, _ := h.run(t, "", "update", "--to", "v7.7.7", "--yes", "--api-url", h.server.URL); code != exitUsage {
		t.Errorf("unknown version: code = %d", code)
	}
}

func TestUpdateList(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, selfupdatetest.New(t, "amd64",
		selfupdatetest.Release{Tag: "v1.0.0", Program: "x"},
		selfupdatetest.Release{Tag: "v1.1.0", Program: "x"},
		selfupdatetest.Release{Tag: "v1.2.0-rc.1", Prerelease: true, Program: "x"},
		selfupdatetest.Release{Tag: "v1.3.0", Program: "x", NoSignature: true}))
	h.root = false // listing changes nothing
	code, stdout, stderr := h.run(t, "", "update", "--list", "--api-url", h.server.URL, "--format", "json")
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{`"v1.2.0-rc.1"`, `"prerelease"`, `"v1.1.0"`, `"current"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q lacks %s", stdout, want)
		}
	}
	if strings.Contains(stdout, "v1.3.0") {
		t.Errorf("unsigned release listed: %q", stdout)
	}
	if code, _, _ := h.run(t, "", "update", "--list", "--to", "v1.1.0"); code != exitUsage {
		t.Errorf("--list with --to: code = %d", code)
	}
}

func TestUpdateWithUnchangedUnitSkipsDaemonReload(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new", Unit: "same unit"}))
	if err := os.WriteFile(h.unitPath, []byte("same unit"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := h.run(t, "", "update", "--yes", "--api-url", h.server.URL, "--format", "json")
	if code != exitOK || !slices.Equal(h.calls, []string{"systemctl restart sysmin"}) || !strings.Contains(stdout, `"unit_updated":false`) {
		t.Errorf("code = %d, calls = %v, stdout = %q, stderr = %q", code, h.calls, stdout, stderr)
	}
}

func TestUpdateDaemonReloadFailureIsReported(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new"}))
	h.fail["systemctl daemon-reload"] = exitStatus(1)
	code, _, stderr := h.run(t, "", "update", "--yes", "--api-url", h.server.URL)
	if code != exitRuntime || !strings.Contains(stderr, "reloading the service manager failed") || slices.Contains(h.calls, "systemctl restart sysmin") {
		t.Errorf("code = %d, calls = %v, stderr = %q", code, h.calls, stderr)
	}
}

func TestUpdateWithoutUnitInArchiveChangesNothing(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new", NoUnit: true}))
	code, _, _ := h.run(t, "", "update", "--yes", "--api-url", h.server.URL)
	if code != exitRuntime || binary(t, h) != "old program" || len(h.calls) != 0 {
		t.Errorf("code = %d, calls = %v", code, h.calls)
	}
}

func TestUpdateRejectsReleaseURLFlag(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new"}))
	if code, _, _ := h.run(t, "", "update", "--yes", "--release-url", h.server.URL); code != exitUsage {
		t.Errorf("code = %d, want %d", code, exitUsage)
	}
}
