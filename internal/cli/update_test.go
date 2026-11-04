package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/redact"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"github.com/castrowithcee/qatlas-kit/selfupdate"
)

type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitStatus) ExitCode() int { return int(e) }

// fakeHost is a system that records commands and never touches the real host.
type fakeHost struct {
	system
	dir    string
	root   bool
	calls  []string
	fail   map[string]error
	server *fakeServer
	keys   []ssh.PublicKey
}

// noNetwork fails the test on any request: a test without a release server must never reach the network.
type noNetwork struct{ t *testing.T }

func (n noNetwork) RoundTrip(r *http.Request) (*http.Response, error) {
	n.t.Errorf("unexpected request to %s", r.URL)
	return nil, fmt.Errorf("no network in this test")
}

func newFakeHost(t *testing.T, server *fakeServer) *fakeHost {
	t.Helper()
	dir := t.TempDir()
	h := &fakeHost{dir: dir, root: true, fail: map[string]error{}, server: server}
	h.system = system{
		euid: func() int { return map[bool]int{true: 0, false: 1000}[h.root] },
		command: func(_ context.Context, name string, args ...string) error {
			call := strings.TrimSpace(name + " " + strings.Join(args, " "))
			h.calls = append(h.calls, call)
			return h.fail[call]
		},
		binaryPath: filepath.Join(dir, "bin", "qsysmin"),
		unitPath:   filepath.Join(dir, "qsysmin.service"),
		manPath:    filepath.Join(dir, "share", "man", "man1", "qsysmin.1"),
		groupPath:  filepath.Join(dir, "group"),
		dataDirs:   []string{filepath.Join(dir, "etc"), filepath.Join(dir, "var")},
	}
	h.system.updater = func(v, apiURL string) *selfupdate.Client {
		c := newUpdateClient(v, apiURL)
		c.HTTPClient = &http.Client{Transport: noNetwork{t}}
		return c
	}
	if server != nil {
		h.system.updater = func(v, apiURL string) *selfupdate.Client {
			c := newUpdateClient(v, apiURL)
			c.ArchiveName = archiveNamer("amd64")
			c.TrustedKeys = []ssh.PublicKey{server.Signer.PublicKey()}
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
	var out, errOut bytes.Buffer
	code := run(opts, strings.NewReader(stdin), args, &out, &errOut)
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
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new program"}))
	code, stdout, stderr := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API(), "--output", "json")
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if binary(t, h) != "new program" {
		t.Error("program not replaced")
	}
	if want := []string{"systemctl daemon-reload", "systemctl restart qsysmin"}; !slices.Equal(h.calls, want) {
		t.Errorf("calls = %v, want %v", h.calls, want)
	}
	if b, _ := os.ReadFile(h.unitPath); !strings.Contains(string(b), "v1.1.0") {
		t.Errorf("unit = %q", b)
	}
	if b, _ := os.ReadFile(h.manPath); !strings.Contains(string(b), "v1.1.0") {
		t.Errorf("manpage = %q", b)
	}
	for _, want := range []string{`"unit_updated":true`, `"previous":"v1.0.0"`, `"version":"v1.1.0"`, `"updated":true`, `"restarted":true`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q lacks %s", stdout, want)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestUpdateRefusesBadReleaseWithoutChangeOrRestart(t *testing.T) {
	withVersion(t, "v1.0.0")
	foreign := releasetest.NewSigner(t)
	for name, rel := range map[string]fakeRelease{
		"checksum":  {Tag: "v1.1.0", Program: "x", BadChecksum: true},
		"namespace": {Tag: "v1.1.0", Program: "x", Namespace: "other-namespace"},
		"key":       {Tag: "v1.1.0", Program: "x", Signer: foreign},
	} {
		t.Run(name, func(t *testing.T) {
			h := newFakeHost(t, newFakeServer(t, "amd64", rel))
			code, _, _ := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API())
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
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new"}))
	h.fail["systemctl restart qsysmin"] = exitStatus(1)
	code, _, stderr := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API())
	if code != exitRuntime || !strings.Contains(stderr, "restarting the service failed") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestUpdateRejectsNonRootBeforeAnyRequest(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, nil)
	h.root = false
	code, _, stderr := h.run(t, "", "update", "--yes", "--confirm", "--api-url", "http://127.0.0.1:1")
	if code != exitRuntime || !strings.Contains(stderr, "root") || len(h.calls) != 0 {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestUpdateNeedsConfirmBeforeAnyRequest(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, nil) // any request to the release server fails the test
	for _, args := range [][]string{
		{"update"}, {"update", "--yes"}, {"update", "--agent", "--yes"}, {"update", "--to", "v1.1.0", "--yes"},
	} {
		code, stdout, stderr := h.run(t, "", append(args, "--api-url", "http://127.0.0.1:1")...)
		lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
		if code != exitUsage || stdout != "" || len(lines) != 2 ||
			lines[0] != "qsysmin: confirmation-required: this request needs --confirm" || !strings.Contains(lines[1], "--confirm") {
			t.Errorf("%v: code = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
		}
	}
	if len(h.calls) != 0 || binary(t, h) != "old program" {
		t.Errorf("calls = %v, program = %q", h.calls, binary(t, h))
	}
}

func TestUpdateInAgentModeNeedsYes(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new program"}))
	code, _, stderr := h.run(t, "", "update", "--agent", "--confirm", "--api-url", h.server.API())
	if code != exitUsage || !strings.HasPrefix(stderr, "qsysmin: confirmation-required: this request needs an explicit answer\n") ||
		binary(t, h) != "old program" || len(h.calls) != 0 {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestUpdateDevBuildAndUpToDate(t *testing.T) {
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.0.0", Program: "x"}))
	withVersion(t, "dev")
	if code, _, stderr := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API()); code != exitRuntime || !strings.Contains(stderr, "dev build") {
		t.Errorf("dev: code = %d, stderr = %q", code, stderr)
	}
	version = "v1.0.0"
	code, stdout, _ := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API(), "--output", "json")
	if code != exitOK || !strings.Contains(stdout, `"updated":false`) || len(h.calls) != 0 || binary(t, h) != "old program" {
		t.Errorf("up to date: code = %d, stdout = %q, calls = %v", code, stdout, h.calls)
	}
}

// startingProgram is a release program that starts and ends well; rollback validation runs it.
const startingProgram = "#!/bin/sh\nexit 0\n"

func TestUpdateToChosenVersion(t *testing.T) {
	withVersion(t, "v1.2.0")
	h := newFakeHost(t, newFakeServer(t, "amd64",
		fakeRelease{Tag: "v1.0.0", Program: startingProgram},
		fakeRelease{Tag: "v1.2.0", Program: "program two"}))
	code, _, stderr := h.run(t, "", "update", "--to", "v1.0.0", "--yes", "--confirm", "--api-url", h.server.API())
	if code != exitOK || binary(t, h) != startingProgram {
		t.Errorf("code = %d, program = %q, stderr = %q", code, binary(t, h), stderr)
	}
	if want := []string{"systemctl daemon-reload", "systemctl restart qsysmin"}; !slices.Equal(h.calls, want) {
		t.Errorf("calls = %v, want %v", h.calls, want)
	}
	if code, _, _ := h.run(t, "", "update", "--to", "v7.7.7", "--yes", "--confirm", "--api-url", h.server.API()); code != exitUsage {
		t.Errorf("unknown version: code = %d", code)
	}
}

func TestUpdateRollbackNeedsAProgramThatStarts(t *testing.T) {
	withVersion(t, "v1.2.0")
	for name, program := range map[string]string{"fails": "#!/bin/sh\nexit 1\n", "no program": "not a program"} {
		t.Run(name, func(t *testing.T) {
			h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.0.0", Program: program}))
			code, _, stderr := h.run(t, "", "update", "--to", "v1.0.0", "--yes", "--confirm", "--api-url", h.server.API())
			if code != exitRuntime || !strings.Contains(stderr, "rollback validation") || binary(t, h) != "old program" || len(h.calls) != 0 {
				t.Errorf("code = %d, program = %q, calls = %v, stderr = %q", code, binary(t, h), h.calls, stderr)
			}
		})
	}
}

func TestUpdateRefusesVersionBelowMinimum(t *testing.T) {
	withVersion(t, "v1.2.0")
	h := newFakeHost(t, nil) // any request to the release server fails the test
	code, _, stderr := h.run(t, "", "update", "--to", "v0.6.9", "--yes", "--confirm", "--api-url", "http://127.0.0.1:1"+fakeRepoPath)
	if code != exitUsage || !strings.Contains(stderr, "minimum") || len(h.calls) != 0 {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestUpdateRefusesReleaseKitRejects(t *testing.T) {
	withVersion(t, "v1.0.0")
	const evil = "../../evil"
	tests := map[string]struct {
		rel       fakeRelease
		code      int
		errorText string
	}{
		"archive asset outside the API": {fakeRelease{Tag: "v1.1.0", Program: "x", ArchiveURL: "https://example.invalid/repos/castrowithcee/qatlas-sysmin/releases/assets/9"}, exitUsage, "unpublished, unsigned, or incomplete"},
		"archive asset without ID":      {fakeRelease{Tag: "v1.1.0", Program: "x", ArchiveURL: "@API@/releases/assets/v1.1.0"}, exitUsage, "unpublished, unsigned, or incomplete"},
		"duplicate asset":               {fakeRelease{Tag: "v1.1.0", Program: "x", DuplicateArchive: true}, exitUsage, "unpublished, unsigned, or incomplete"},
		"unsafe archive path": {fakeRelease{Tag: "v1.1.0", Files: []releasetest.File{
			{Name: "bin/qsysmin", Body: []byte("x")}, {Name: evil, Body: []byte("x")}, {Name: unitEntry, Body: []byte("unit")}}}, exitRuntime, "unsafe archive entry"},
		"duplicate archive entry": {fakeRelease{Tag: "v1.1.0", Files: []releasetest.File{
			{Name: "bin/qsysmin", Body: []byte("x")}, {Name: "bin/qsysmin", Body: []byte("y")}, {Name: unitEntry, Body: []byte("unit")}}}, exitRuntime, "duplicate archive entry"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			server := newFakeServer(t, "amd64")
			tt.rel.ArchiveURL = strings.ReplaceAll(tt.rel.ArchiveURL, "@API@", server.API())
			server.publish(t, "amd64", tt.rel)
			h := newFakeHost(t, server)
			code, _, stderr := h.run(t, "", "update", "--to", "v1.1.0", "--yes", "--confirm", "--api-url", server.API())
			if code != tt.code || !strings.Contains(stderr, tt.errorText) || binary(t, h) != "old program" || len(h.calls) != 0 || exists(h.unitPath) {
				t.Errorf("code = %d, program = %q, calls = %v, stderr = %q", code, binary(t, h), h.calls, stderr)
			}
			// Without --to the release is not even offered.
			code, stdout, _ := h.run(t, "", "update", "--yes", "--confirm", "--api-url", server.API(), "--output", "json")
			if tt.code == exitUsage && (code != exitOK || !strings.Contains(stdout, `"updated":false`)) {
				t.Errorf("without --to: code = %d, stdout = %q", code, stdout)
			}
		})
	}
}

func TestUpdateList(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, newFakeServer(t, "amd64",
		fakeRelease{Tag: "v1.0.0", Program: "x"},
		fakeRelease{Tag: "v1.1.0", Program: "x"},
		fakeRelease{Tag: "v1.2.0-rc.1", Prerelease: true, Program: "x"},
		fakeRelease{Tag: "v1.3.0", Program: "x", NoSignature: true}))
	h.root = false // listing changes nothing
	code, stdout, stderr := h.run(t, "", "update", "--list", "--api-url", h.server.API(), "--output", "json")
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
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new", Unit: "same unit"}))
	if err := os.WriteFile(h.unitPath, []byte("same unit"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API(), "--output", "json")
	if code != exitOK || !slices.Equal(h.calls, []string{"systemctl restart qsysmin"}) || !strings.Contains(stdout, `"unit_updated":false`) {
		t.Errorf("code = %d, calls = %v, stdout = %q, stderr = %q", code, h.calls, stdout, stderr)
	}
}

func TestUpdateDaemonReloadFailureIsReported(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new"}))
	h.fail["systemctl daemon-reload"] = exitStatus(1)
	code, _, stderr := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API())
	if code != exitRuntime || !strings.Contains(stderr, "reloading the service manager failed") || slices.Contains(h.calls, "systemctl restart qsysmin") {
		t.Errorf("code = %d, calls = %v, stderr = %q", code, h.calls, stderr)
	}
}

func TestUpdateWithoutUnitInArchiveChangesNothing(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new", NoUnit: true}))
	code, _, _ := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API())
	if code != exitRuntime || binary(t, h) != "old program" || len(h.calls) != 0 {
		t.Errorf("code = %d, calls = %v", code, h.calls)
	}
}

func TestUpdateRejectsReleaseURLFlag(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new"}))
	if code, _, _ := h.run(t, "", "update", "--yes", "--confirm", "--release-url", h.server.API()); code != exitUsage {
		t.Errorf("code = %d, want %d", code, exitUsage)
	}
}

func TestUpdateWithUnchangedManpageLeavesItAndNeedsNoReload(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new", Unit: "same unit", Manpage: ".TH same 1\n"}))
	for path, body := range map[string]string{h.unitPath: "same unit", h.manPath: ".TH same 1\n"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Unix(1_000_000_000, 0)
	if err := os.Chtimes(h.manPath, old, old); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API())
	info, err := os.Stat(h.manPath)
	if err != nil || code != exitOK || !info.ModTime().Equal(old) || !slices.Equal(h.calls, []string{"systemctl restart qsysmin"}) {
		t.Errorf("code = %d, calls = %v, stat = %v, %v, stderr = %q", code, h.calls, info, err, stderr)
	}
}

func TestUpdateReplacesChangedManpageWithoutReload(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new", Unit: "same unit", Manpage: ".TH new 1\n"}))
	if err := os.WriteFile(h.unitPath, []byte("same unit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(h.manPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.manPath, []byte(".TH old 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API())
	if b, _ := os.ReadFile(h.manPath); code != exitOK || string(b) != ".TH new 1\n" || !slices.Equal(h.calls, []string{"systemctl restart qsysmin"}) {
		t.Errorf("code = %d, calls = %v, manpage = %q, stderr = %q", code, h.calls, b, stderr)
	}
}

func TestUpdateWithoutManpageInArchiveChangesNothing(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new", NoManpage: true}))
	code, _, _ := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API())
	if code != exitRuntime || binary(t, h) != "old program" || len(h.calls) != 0 || exists(h.manPath) {
		t.Errorf("code = %d, calls = %v", code, h.calls)
	}
}

func TestUpdateRemovesServiceUserFromGroupDocker(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new program"}))
	if err := os.WriteFile(h.groupPath, []byte("root:x:0:\ndocker:x:998:demo-user,qsysmin\nqsysmin:x:997:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API())
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	want := []string{"gpasswd -d qsysmin docker", "systemctl daemon-reload", "systemctl restart qsysmin"}
	if !slices.Equal(h.calls, want) {
		t.Errorf("calls = %v, want %v", h.calls, want)
	}
}

func TestUpdateLeavesGroupDockerWithoutServiceUserAlone(t *testing.T) {
	withVersion(t, "v1.0.0")
	for name, content := range map[string]string{
		"other members": "docker:x:998:demo-user,qsysmin-extra\n",
		"no members":    "docker:x:998:\n",
		"no group":      "root:x:0:\n",
		"no file":       "",
	} {
		t.Run(name, func(t *testing.T) {
			h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new program"}))
			if content != "" {
				if err := os.WriteFile(h.groupPath, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if code, _, stderr := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API()); code != exitOK {
				t.Fatalf("code = %d, stderr = %q", code, stderr)
			}
			for _, c := range h.calls {
				if strings.HasPrefix(c, "gpasswd") {
					t.Errorf("unexpected call %q", c)
				}
			}
		})
	}
}

func TestUpdateGroupRemovalFailureIsReported(t *testing.T) {
	withVersion(t, "v1.0.0")
	h := newFakeHost(t, newFakeServer(t, "amd64", fakeRelease{Tag: "v1.1.0", Program: "new program"}))
	if err := os.WriteFile(h.groupPath, []byte("docker:x:998:qsysmin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.fail["gpasswd -d qsysmin docker"] = exitStatus(1)
	code, _, stderr := h.run(t, "", "update", "--yes", "--confirm", "--api-url", h.server.API())
	if code != exitRuntime || !strings.Contains(stderr, "removing the service user from the group docker failed") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}
