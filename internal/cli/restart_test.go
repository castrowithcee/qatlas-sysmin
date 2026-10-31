package cli

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/tui"
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

func TestUpdateAndRestartInstallsThenStartsTheInterfaceAgain(t *testing.T) {
	var ran []string
	var restarted []string
	oldLook, oldExe, oldExec, oldRun := lookPath, executable, execv, runCommand
	t.Cleanup(func() { lookPath, executable, execv, runCommand = oldLook, oldExe, oldExec, oldRun })
	lookPath = func(string) (string, error) { return "/usr/bin/sudo", nil }
	executable = func() (string, error) { return "/usr/local/bin/sysmin", nil }
	runCommand = func(c *exec.Cmd) error { ran = c.Args; return nil }
	execv = func(path string, argv, _ []string) error { restarted = append([]string{path}, argv...); return nil }

	if err := updateAndRestart("v0.1.3", []string{"sysmin", "tui"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"/usr/bin/sudo", "/usr/local/bin/sysmin", "update", "--to", "v0.1.3", "--yes"}; !slices.Equal(ran, want) {
		t.Errorf("update ran %q", ran)
	}
	if want := []string{"/usr/local/bin/sysmin", "sysmin", "tui"}; !slices.Equal(restarted, want) {
		t.Errorf("restart %q", restarted)
	}
}

func TestDialErrorKeepsTheNextStepOfAForeignPeer(t *testing.T) {
	err := dialError(fmt.Errorf("x: %w", &client.PeerError{PID: 4321, UID: 1234}))
	var pe *client.PeerError
	if !errors.As(err, &pe) || strings.Contains(err.Error(), "is the service running") ||
		!strings.Contains(err.Error(), "sudo kill 4321") || !strings.Contains(err.Error(), "sudo systemctl restart sysmin") {
		t.Errorf("err = %v", err)
	}
}

func TestHostRunnerBuildsTheSudoCallWithoutPassingASecret(t *testing.T) {
	oldLook, oldExe := lookPath, executable
	t.Cleanup(func() { lookPath, executable = oldLook, oldExe })
	lookPath = func(string) (string, error) { return "/usr/bin/sudo", nil }
	executable = func() (string, error) { return "/usr/local/bin/sysmin", nil }

	cmd, err := hostRunner(tui.HostRequest{Args: []string{"journal", "--output", "json"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/bin/sudo", "/usr/local/bin/sysmin", "host", "journal", "--output", "json"}
	if !slices.Equal(cmd.Args, want) || cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
		t.Errorf("args %q stdin %v stdout %v stderr %v", cmd.Args, cmd.Stdin, cmd.Stdout, cmd.Stderr)
	}
	validate, err := hostRunner(tui.HostRequest{Validate: true})
	if err != nil || !slices.Equal(validate.Args, []string{"/usr/bin/sudo", "-v"}) {
		t.Errorf("validate: %v %v", validate, err)
	}
	marker := "invented-input-marker"
	secret, err := hostRunner(tui.HostRequest{Args: []string{"apply", "--yes", "--", "demo.login"}, Secret: []byte(marker)})
	if err != nil {
		t.Fatal(err)
	}
	wantSecret := []string{"/usr/bin/sudo", "-n", "/usr/local/bin/sysmin", "host", "apply", "--yes", "--", "demo.login"}
	if !slices.Equal(secret.Args, wantSecret) {
		t.Errorf("secret args %q", secret.Args)
	}
	body, _ := io.ReadAll(secret.Stdin)
	if string(body) != marker || strings.Contains(strings.Join(secret.Args, " ")+strings.Join(secret.Env, " "), marker) {
		t.Errorf("the secret must be the standard input only: stdin %q", body)
	}
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := hostRunner(tui.HostRequest{Args: []string{"journal"}}); !errors.Is(err, tui.ErrNoSudo) {
		t.Errorf("without sudo: %v", err)
	}
}
