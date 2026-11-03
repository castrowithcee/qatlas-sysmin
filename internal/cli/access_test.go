package cli

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/service"
)

var (
	reservedLines   = "qsysmin: admin-required: " + client.ReservedMessage + "\n" + client.ErrReserved.NextStep + "\n"
	notEnabledLines = "qsysmin: not-enabled: " + client.NotEnabledMessage + "\n" + client.ErrNotEnabled.NextStep + "\n"
)

func TestCommandsSayReservedOnANotAdminAnswer(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root needs no admin right")
	}
	sock := startServiceCfg(t, func(c *service.Config) { c.IsAdmin = func(uint32) (bool, error) { return false, nil } })
	code, stdout, stderr := runCLIArgs("status", "--socket", sock)
	if code != exitRuntime || stdout != "" || stderr != reservedLines {
		t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

// refusingService answers every connection the way the service answers a peer its channel refused.
func refusingService(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "qsysmin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	if err := os.Chmod(sock, 0o600); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = control.WriteLine(conn, control.Response{V: control.Version, Error: control.CodeRefused})
			conn.Close()
		}
	}()
	return sock
}

func TestCommandsNameTheStandingOnARefusedAnswer(t *testing.T) {
	sock := refusingService(t)
	for _, tc := range []struct {
		name           string
		admin, enabled bool
		want           string
	}{
		{"not an administrator", false, false, reservedLines},
		{"administrator, not enabled", true, false, notEnabledLines},
	} {
		restore := client.SetStandingForTests(tc.admin, tc.enabled)
		code, stdout, stderr := runCLIArgs("status", "--socket", sock)
		restore()
		if code != exitRuntime || stdout != "" || stderr != tc.want {
			t.Errorf("%s: code = %d, stdout = %q, stderr = %q", tc.name, code, stdout, stderr)
		}
	}
}

func TestCommandsNameTheStandingOnSocketPermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root is never refused by file permissions")
	}
	dir, err := os.MkdirTemp("", "qsysmin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	if err := os.Chmod(sock, 0); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name           string
		admin, enabled bool
		want           string
	}{
		{"not an administrator", false, false, reservedLines},
		{"administrator, not enabled", true, false, notEnabledLines},
	} {
		restore := client.SetStandingForTests(tc.admin, tc.enabled)
		code, stdout, stderr := runCLIArgs("status", "--socket", sock)
		restore()
		if code != exitRuntime || stdout != "" || stderr != tc.want {
			t.Errorf("%s: code = %d, stdout = %q, stderr = %q", tc.name, code, stdout, stderr)
		}
	}
	restore := client.SetStandingForTests(true, true)
	defer restore()
	if _, _, stderr := runCLIArgs("status", "--socket", sock); strings.Contains(stderr, "reserved") || strings.Contains(stderr, "enabled for qsysmin") {
		t.Errorf("an enabled administrator got an access line: %q", stderr)
	}
}
