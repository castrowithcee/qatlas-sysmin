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
	code, stdout, stderr := h.run(t, "", "uninstall", "--yes", "--format", "json")
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if exists(h.unitPath) || exists(h.binaryPath) {
		t.Error("unit or program left")
	}
	for _, d := range h.dataDirs {
		if !exists(filepath.Join(d, "data")) {
			t.Errorf("%s removed without --purge", d)
		}
	}
	want := "systemctl disable --now sysmin|systemctl daemon-reload|userdel sysmin|groupdel sysmin"
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
	if code, _, stderr := h.run(t, "", "uninstall", "--purge", "--yes"); code != exitOK {
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
	h.fail["userdel sysmin"] = exitStatus(6)
	h.fail["groupdel sysmin"] = exitStatus(6)
	_ = os.Remove(h.binaryPath)
	code, _, stderr := h.run(t, "", "uninstall", "--purge", "--yes")
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
	h.fail["systemctl disable --now sysmin"] = exitStatus(1)
	code, _, _ := h.run(t, "", "uninstall", "--purge", "--yes")
	if code != exitRuntime || !exists(h.unitPath) || !exists(h.binaryPath) || !exists(h.dataDirs[0]) {
		t.Errorf("code = %d; the installation was changed after a failed stop", code)
	}
}

func TestUninstallRootAndConfirmation(t *testing.T) {
	h := newFakeHost(t, nil)
	installFiles(t, h)
	h.root = false
	if code, _, stderr := h.run(t, "", "uninstall", "--yes"); code != exitRuntime || !strings.Contains(stderr, "root") {
		t.Errorf("non-root: code = %d, stderr = %q", code, stderr)
	}
	h.root = true
	if code, _, _ := h.run(t, "", "uninstall"); code != exitUsage {
		t.Errorf("no terminal, no --yes: code = %d", code)
	}
	h.terminal = true
	if code, _, _ := h.run(t, "no\n", "uninstall"); code != exitRuntime || !exists(h.binaryPath) {
		t.Errorf("declined: code = %d", code)
	}
	if len(h.calls) != 0 {
		t.Errorf("calls = %v", h.calls)
	}
}
