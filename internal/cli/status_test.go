package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
	"github.com/castrowithcee/qatlas-sysmin/internal/service"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

type testGroups struct{}

func (testGroups) GroupID(string) (uint32, error)           { return uint32(os.Getgid()), nil }
func (testGroups) PeerGroups(daemon.Peer) ([]uint32, error) { return nil, nil }

func runCLIArgs(args ...string) (int, string, string) {
	opts := &Options{Redactor: &redact.Redactor{}}
	var out, errOut bytes.Buffer
	code := run(newRootCommand(opts), opts, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// startService runs the service through runService, as sysmin run does, on temporary directories.
func startService(t *testing.T, bound bool) string {
	t.Helper()
	return startServiceWith(t, bound, nil)
}

// startServiceWith also lets setup fill the service's configuration directory before it starts.
func startServiceWith(t *testing.T, bound bool, setup func(configDir string)) string {
	t.Helper()
	sock, _, _ := startServiceDirs(t, bound, setup)
	return sock
}

// startServiceDirs also returns the state and the configuration directory of the service.
func startServiceDirs(t *testing.T, bound bool, setup func(configDir string)) (sock, state, configDir string) {
	t.Helper()
	return startServiceCfg(t, bound, setup, nil)
}

// startServiceCfg also lets mod change the service's configuration before it starts.
func startServiceCfg(t *testing.T, bound bool, setup func(configDir string), mod func(*service.Config)) (sock, state, configDir string) {
	t.Helper()
	vault.LowerWorkFactorForTests()
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	sockDir, err := os.MkdirTemp("", "sysmin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	state = t.TempDir()
	v := vault.New(filepath.Join(state, "vault"))
	if err := v.Create("example-passphrase-1"); err != nil {
		t.Fatal(err)
	}
	credentials := ""
	if bound {
		id, _ := v.Identity()
		credentials = t.TempDir()
		if err := os.WriteFile(filepath.Join(credentials, "vault-key"), []byte(id.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	configDir = t.TempDir()
	if setup != nil {
		setup(configDir)
	}
	cfg := service.Config{
		ConfigDir: configDir, StateDir: state, SocketPath: filepath.Join(sockDir, "s.sock"),
		Version: "9.9.9", Resolver: testGroups{}, CredentialsDir: credentials,
	}
	if mod != nil {
		mod(&cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runService(ctx, cfg) }()
	t.Cleanup(func() { cancel(); <-done })
	for i := 0; i < 300; i++ {
		if _, err := os.Lstat(cfg.SocketPath); err == nil {
			return cfg.SocketPath, state, configDir
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("service did not start")
	return "", "", ""
}

func TestStatusAgainstLockedService(t *testing.T) {
	sock := startService(t, false)
	code, stdout, stderr := runCLIArgs("status", "--socket", sock, "--format", "json")
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{`"version":"9.9.9"`, `"vault":"locked"`, `"unlock":"manual"`, `"socket":"` + sock + `"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q lacks %s", stdout, want)
		}
	}
	code, stdout, _ = runCLIArgs("vault", "status", "--socket", sock, "--format", "compact")
	if code != exitOK || !strings.Contains(stdout, "locked") {
		t.Errorf("vault status: code = %d, stdout = %q", code, stdout)
	}
}

func TestStatusAgainstUnlockedService(t *testing.T) {
	sock := startService(t, true)
	code, stdout, stderr := runCLIArgs("status", "--socket", sock, "--format", "json")
	if code != exitOK || !strings.Contains(stdout, `"vault":"unlocked"`) || !strings.Contains(stdout, `"warning":"`) {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestStatusWithoutService(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "none.sock")
	for _, args := range [][]string{{"status"}, {"vault", "status"}} {
		code, stdout, stderr := runCLIArgs(append(args, "--socket", sock)...)
		if code != exitRuntime || stdout != "" || !strings.Contains(stderr, "cannot reach the service") {
			t.Errorf("%v: code = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
		}
	}
}
