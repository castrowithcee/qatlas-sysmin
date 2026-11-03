package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installFiles(t *testing.T, h *fakeHost) {
	t.Helper()
	if err := os.WriteFile(h.unitPath, []byte("[Unit]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(h.manPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.manPath, []byte(".TH QSYSMIN 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range h.dataDirs {
		if err := os.WriteFile(filepath.Join(d, "data"), []byte("example"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

func TestUninstallKeepsDataWithoutPurge(t *testing.T) {
	h := newFakeHost(t, nil)
	installFiles(t, h)
	code, stdout, stderr := h.run(t, "", "uninstall", "--yes", "--confirm", "--output", "json")
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if exists(h.unitPath) || exists(h.binaryPath) || exists(h.manPath) {
		t.Error("unit, program or manpage left")
	}
	for _, d := range h.dataDirs {
		if !exists(filepath.Join(d, "data")) {
			t.Errorf("%s removed without --purge", d)
		}
	}
	want := "systemctl disable --now qsysmin|systemctl daemon-reload|userdel qsysmin|groupdel qsysmin"
	if got := strings.Join(h.calls, "|"); got != want {
		t.Errorf("calls = %s", got)
	}
	if !strings.Contains(stdout, `"purged":false`) {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestUninstallPurgeRemovesData(t *testing.T) {
	h := newFakeHost(t, nil)
	installFiles(t, h)
	if code, _, stderr := h.run(t, "", "uninstall", "--purge", "--yes", "--confirm"); code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, d := range h.dataDirs {
		if exists(d) {
			t.Errorf("%s left after --purge", d)
		}
	}
}

func TestUninstallIsIdempotent(t *testing.T) {
	h := newFakeHost(t, nil)
	h.fail["userdel qsysmin"] = exitStatus(6)
	h.fail["groupdel qsysmin"] = exitStatus(6)
	_ = os.Remove(h.binaryPath)
	code, _, stderr := h.run(t, "", "uninstall", "--purge", "--yes", "--confirm")
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, c := range h.calls {
		if strings.HasPrefix(c, "systemctl") {
			t.Errorf("systemctl called without a unit: %v", h.calls)
		}
	}
}

func TestUninstallStopsOnFailure(t *testing.T) {
	h := newFakeHost(t, nil)
	installFiles(t, h)
	h.fail["systemctl disable --now qsysmin"] = exitStatus(1)
	code, _, _ := h.run(t, "", "uninstall", "--purge", "--yes", "--confirm")
	if code != exitRuntime || !exists(h.unitPath) || !exists(h.binaryPath) || !exists(h.dataDirs[0]) {
		t.Errorf("code = %d; the installation was changed after a failed stop", code)
	}
}

func TestUninstallRootAndConfirmation(t *testing.T) {
	h := newFakeHost(t, nil)
	installFiles(t, h)
	h.root = false
	if code, _, stderr := h.run(t, "", "uninstall", "--yes", "--confirm"); code != exitRuntime ||
		!strings.HasPrefix(stderr, "qsysmin: root-required: uninstall must run as root\n") {
		t.Errorf("non-root: code = %d, stderr = %q", code, stderr)
	}
	h.root = true
	for _, args := range [][]string{{"uninstall"}, {"uninstall", "--yes"}, {"uninstall", "--purge", "--agent", "--yes"}} {
		code, _, stderr := h.run(t, "", args...)
		if code != exitUsage || !strings.HasPrefix(stderr, "qsysmin: confirmation-required: this request needs --confirm\n") {
			t.Errorf("%v: code = %d, stderr = %q", args, code, stderr)
		}
	}
	if code, _, stderr := h.run(t, "", "uninstall", "--agent", "--confirm"); code != exitUsage ||
		!strings.HasPrefix(stderr, "qsysmin: confirmation-required: this request needs an explicit answer\n") {
		t.Errorf("agent without --yes: code = %d, stderr = %q", code, stderr)
	}
	if !exists(h.binaryPath) || !exists(h.unitPath) || len(h.calls) != 0 {
		t.Errorf("a refused uninstall changed the host: calls = %v", h.calls)
	}
}
