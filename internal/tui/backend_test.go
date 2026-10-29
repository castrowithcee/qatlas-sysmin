package tui

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// serve answers requests on a socket. Each connection is closed after drop answers, so the client meets a
// broken connection.
func serve(t *testing.T, drop int) (socket string, connections func() int) {
	t.Helper()
	socket = filepath.Join(t.TempDir(), "s")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
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
					result, _ := json.Marshal(control.StatusResult{Version: "0.0.0-test", Vault: control.VaultUnlocked})
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
		if err != nil || st.Vault != control.VaultUnlocked {
			t.Fatalf("status %d: %v %v", i, st, err)
		}
	}
	if got := connections(); got != 3 {
		t.Errorf("connections = %d, want one per answer", got)
	}
}

func TestClientBackendDoesNotRepeatAChangingCall(t *testing.T) {
	socket, connections := serve(t, 1)
	b := NewBackend(socket)
	defer b.Close()
	if _, err := b.Status(); err != nil {
		t.Fatal(err)
	}
	err := b.OpenSession(testPass)
	var ce *control.Error
	if err == nil || errors.As(err, &ce) {
		t.Fatalf("err = %v, want a transport failure", err)
	}
	if got := connections(); got != 1 {
		t.Errorf("connections = %d, want 1", got)
	}
	if got := errorText(err); got != "Service unreachable." {
		t.Errorf("errorText = %q", got)
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
