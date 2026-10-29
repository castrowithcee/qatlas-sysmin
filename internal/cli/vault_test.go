package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/autounlock"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

type cliRunner struct{ calls int }

func (r *cliRunner) Run(_ context.Context, name string, args []string, _ []byte) ([]byte, error) {
	r.calls++
	if name == "systemd-creds" {
		return nil, os.WriteFile(args[len(args)-1], []byte("sealed"), 0o600)
	}
	return []byte("no\n"), errors.New("exit status 1")
}

func runVault(t *testing.T, root bool, r autounlock.Runner, args ...string) (int, string, string) {
	t.Helper()
	opts := &Options{Redactor: &redact.Redactor{}}
	opts.binder = func(dir string) *autounlock.Binder {
		return &autounlock.Binder{Dir: dir, Runner: r, IsRoot: func() bool { return root },
			Passphrase: func(io.Reader) (string, error) { return "example-passphrase-1", nil }}
	}
	var out, errOut bytes.Buffer
	code := run(newRootCommand(opts), opts, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVaultBindWithoutRoot(t *testing.T) {
	r := &cliRunner{}
	code, _, stderr := runVault(t, false, r, "vault", "bind", "--vault-dir", t.TempDir())
	if code == exitOK || !strings.Contains(stderr, "root") || r.calls != 0 {
		t.Errorf("code = %d, stderr = %q, runner calls = %d", code, stderr, r.calls)
	}
	code, _, stderr = runVault(t, false, r, "vault", "unbind", "--vault-dir", t.TempDir())
	if code == exitOK || !strings.Contains(stderr, "root") {
		t.Errorf("unbind code = %d, stderr = %q", code, stderr)
	}
}

func TestVaultBindAndUnbind(t *testing.T) {
	vault.LowerWorkFactorForTests()
	dir := filepath.Join(t.TempDir(), "vault")
	if err := vault.New(dir).Create("example-passphrase-1"); err != nil {
		t.Fatal(err)
	}
	r := &cliRunner{}
	code, stdout, stderr := runVault(t, true, r, "vault", "bind", "--vault-dir", dir, "--format", "json")
	if code != exitOK || !strings.Contains(stdout, `"unlock":"host"`) || !strings.Contains(stdout, "warning") {
		t.Fatalf("bind: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "AGE-SECRET-KEY") {
		t.Error("output leaks key material")
	}
	if _, err := os.Stat(filepath.Join(dir, "key.cred")); err != nil {
		t.Error(err)
	}
	code, stdout, _ = runVault(t, true, r, "vault", "unbind", "--vault-dir", dir, "--format", "json")
	if code != exitOK || !strings.Contains(stdout, `"removed":true`) {
		t.Errorf("unbind: code = %d, stdout = %q", code, stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "key.cred")); err == nil {
		t.Error("key.cred still exists")
	}
}
