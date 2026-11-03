package tui

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// serve answers requests on a socket. Each connection is closed after drop answers, so the client meets a
// broken connection.
func serve(t *testing.T, drop int) (socket string, connections func() int) {
	t.Helper()
	dir, err := os.MkdirTemp("", "qsysmin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket = filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	// The client's channel refuses a directory or socket open to others.
	if err := os.Chmod(socket, 0o600); err != nil {
		t.Fatal(err)
	}
	count := make(chan struct{}, 100)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			count <- struct{}{}
			go func() {
				defer conn.Close()
				br := bufio.NewReader(conn)
				for range drop {
					line, err := control.ReadLine(br, control.MaxResponseBytes)
					if err != nil {
						return
					}
					var req control.Request
					_ = json.Unmarshal(line, &req)
					result, _ := json.Marshal(control.StatusResult{Version: "0.0.0-test"})
					_ = control.WriteLine(conn, control.Response{V: control.Version, OK: true, Result: result})
				}
			}()
		}
	}()
	return socket, func() int { return len(count) }
}

func TestClientBackendDialsAgainAfterTheConnectionBroke(t *testing.T) {
	socket, connections := serve(t, 1)
	b := NewBackend(socket)
	defer b.Close()
	for i := range 3 {
		st, err := b.Status()
		if err != nil || st.Version != "0.0.0-test" {
			t.Fatalf("status %d: %v %v", i, st, err)
		}
	}
	if got := connections(); got != 3 {
		t.Errorf("connections = %d, want one per answer", got)
	}
}

func TestClientBackendWithoutAServiceFailsWithoutPanic(t *testing.T) {
	b := NewBackend(filepath.Join(t.TempDir(), "none"))
	if _, err := b.Status(); err == nil {
		t.Error("status without a service succeeded")
	}
	if err := b.Close(); err != nil {
		t.Errorf("close = %v", err)
	}
}
