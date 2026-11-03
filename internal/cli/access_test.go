package cli

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/service"
)

// notAdminGroups lets the peer into the group qsysmin but knows it as no administrator.
type notAdminGroups struct{ testGroups }

func (notAdminGroups) IsAdmin(uint32) (bool, error) { return false, nil }

// outsideGroups keeps the peer out of the group qsysmin.
type outsideGroups struct{ testGroups }

func (outsideGroups) GroupID(string) (uint32, error)           { return uint32(os.Getgid()) + 1, nil }
func (outsideGroups) PeerGroups(daemon.Peer) ([]uint32, error) { return nil, nil }

const reservedLine = "qsysmin is reserved for administrators (sudo)\n"

func TestCommandsSayReservedOnANotAdminAnswer(t *testing.T) {
	sock := startServiceCfg(t, func(c *service.Config) { c.Resolver = notAdminGroups{} })
	code, stdout, stderr := runCLIArgs("status", "--socket", sock)
	if code != exitRuntime || stdout != "" || stderr != reservedLine {
		t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestCommandsNameTheStandingOnARefusedAnswer(t *testing.T) {
	sock := startServiceCfg(t, func(c *service.Config) { c.Resolver = outsideGroups{} })
	for _, tc := range []struct {
		name           string
		admin, enabled bool
		want           string
	}{
		{"not an administrator", false, false, reservedLine},
		{"administrator, not enabled", true, false, client.NotEnabledMessage + "\n"},
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
		{"not an administrator", false, false, reservedLine},
		{"administrator, not enabled", true, false, client.NotEnabledMessage + "\n"},
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
