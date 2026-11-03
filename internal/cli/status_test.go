package cli

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/redact"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/service"
)

type testGroups struct{}

func (testGroups) GroupID(string) (uint32, error)           { return uint32(os.Getgid()), nil }
func (testGroups) PeerGroups(daemon.Peer) ([]uint32, error) { return nil, nil }
func (testGroups) IsAdmin(uint32) (bool, error)             { return true, nil }

func runCLIArgs(args ...string) (int, string, string) {
	opts := &Options{Redactor: &redact.Redactor{}}
	var out, errOut bytes.Buffer
	code := run(opts, nil, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// startService runs the service through runService, as qsysmin run does, on temporary directories.
func startService(t *testing.T) string {
	t.Helper()
	return startServiceCfg(t, nil)
}

// startServiceCfg also lets mod change the service's configuration before it starts.
func startServiceCfg(t *testing.T, mod func(*service.Config)) string {
	t.Helper()
	sockDir, err := os.MkdirTemp("", "qsysmin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	cfg := service.Config{
		StateDir: t.TempDir(), SocketPath: filepath.Join(sockDir, "s.sock"),
		Version: "9.9.9", Resolver: testGroups{},
	}
	if mod != nil {
		mod(&cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runService(ctx, cfg) }()
	t.Cleanup(func() { cancel(); <-done })
	for i := 0; i < 300; i++ {
		// The socket file exists between bind and listen; only a successful dial means the service accepts.
		if c, err := net.Dial("unix", cfg.SocketPath); err == nil {
			c.Close()
			return cfg.SocketPath
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("service did not start")
	return ""
}

func TestStatusAgainstService(t *testing.T) {
	sock := startService(t)
	code, stdout, stderr := runCLIArgs("status", "--socket", sock, "--output", "json")
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{`"version":"9.9.9"`, `"socket":"` + sock + `"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout %q lacks %s", stdout, want)
		}
	}
}

func TestStatusWithoutService(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "none.sock")
	code, stdout, stderr := runCLIArgs("status", "--socket", sock)
	if code != exitRuntime || stdout != "" || !strings.Contains(stderr, "cannot reach the service") {
		t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}
