package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
)

func TestDialErrorNamesSudoForASessionWithoutTheGroup(t *testing.T) {
	err := dialError(fmt.Errorf("cannot reach the service: %w", client.ErrSessionGroup))
	if !errors.Is(err, client.ErrSessionGroup) || !strings.Contains(err.Error(), `"sudo -g sysmin" in front of the command`) {
		t.Errorf("session error: %v", err)
	}
	if err := dialError(errors.New("cannot reach the service: refused")); !strings.Contains(err.Error(), "is the service running") {
		t.Errorf("other error: %v", err)
	}
}

func TestExecWithGroupRunsTheSameCommandUnderSudo(t *testing.T) {
	var gotPath string
	var gotArgv []string
	oldLook, oldExe, oldExec := lookPath, executable, execv
	t.Cleanup(func() { lookPath, executable, execv = oldLook, oldExe, oldExec })
	lookPath = func(string) (string, error) { return "/usr/bin/sudo", nil }
	executable = func() (string, error) { return "/usr/local/bin/sysmin", nil }
	execv = func(path string, argv, _ []string) error { gotPath, gotArgv = path, argv; return nil }

	if err := execWithGroup([]string{"sysmin", "tui", "--socket", "/tmp/example.sock"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"sudo", "-g", "sysmin", "/usr/local/bin/sysmin", "tui", "--socket", "/tmp/example.sock"}
	if gotPath != "/usr/bin/sudo" || !slices.Equal(gotArgv, want) {
		t.Errorf("exec %s %q", gotPath, gotArgv)
	}

	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if err := execWithGroup([]string{"sysmin", "tui"}); err == nil || !strings.Contains(err.Error(), "log in again") {
		t.Errorf("without sudo: %v", err)
	}
}
