package client_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// socket listens in a fresh private directory on a socket closed to others, as the channel requires.
func socket(t *testing.T) (net.Listener, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "qsysmin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return ln, path
}

func TestDialFailsWithoutService(t *testing.T) {
	dir, _ := os.MkdirTemp("", "qsysmin-")
	defer os.RemoveAll(dir)
	if _, err := client.Dial(filepath.Join(dir, "none.sock")); err == nil {
		t.Fatal("dial succeeded")
	}
}

func TestCallReturnsFixedCode(t *testing.T) {
	ln, path := socket(t)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 256)
		_, _ = conn.Read(buf)
		_ = control.WriteLine(conn, control.Response{V: control.Version, Error: control.CodeUnknownOp})
	}()
	c, err := client.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var ce *control.Error
	if err := c.Call("x", nil, nil); !errors.As(err, &ce) || ce.Code != control.CodeUnknownOp {
		t.Fatalf("err = %v", err)
	}
}

func TestDialNamesASessionWithoutTheGroup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root is never refused by file permissions")
	}
	_, path := socket(t)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	for _, lacks := range []bool{true, false} {
		restore := client.SetSessionLacksGroup(lacks)
		restoreStanding := client.SetStandingForTests(true, true)
		_, err := client.Dial(path)
		restoreStanding()
		restore()
		if err == nil {
			t.Fatal("dial succeeded")
		}
		if got := errors.Is(err, client.ErrSessionGroup); got != lacks {
			t.Errorf("lacks %v: session error = %v (%v)", lacks, got, err)
		}
	}
}

// requestsOf answers every request of one connection with success and hands each request on.
func requestsOf(t *testing.T) (*client.Client, <-chan control.Request) {
	t.Helper()
	ln, path := socket(t)
	got := make(chan control.Request, 4)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		for {
			line, err := control.ReadLine(r, control.MaxRequestBytes)
			if err != nil {
				return
			}
			var req control.Request
			_ = json.Unmarshal(line, &req)
			got <- req
			_ = control.WriteLine(conn, control.Response{V: control.Version, OK: true, Result: json.RawMessage(`{"done":[]}`)})
		}
	}()
	c, err := client.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, got
}

// listen opens a socket in a short temp directory and hands every accepted connection to h.
func listen(t *testing.T, h func(net.Conn)) string {
	t.Helper()
	ln, path := socket(t)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go h(conn)
		}
	}()
	return path
}

// The channel checks the socket's files before it connects, so a refused socket never sees the caller.
func TestDialRefusesASocketOpenToOthersBeforeConnecting(t *testing.T) {
	accepted := make(chan struct{}, 1)
	path := listen(t, func(conn net.Conn) {
		accepted <- struct{}{}
		conn.Close()
	})
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	c, err := client.Dial(path)
	if err == nil {
		c.Close()
		t.Fatal("dial succeeded")
	}
	if !errors.Is(err, localipc.ErrRefused) {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-accepted:
		t.Error("the listener was reached")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDialRefusesAPathTheChannelCannotName(t *testing.T) {
	for _, path := range []string{"relative/s.sock", "/run/qsysmin/qsysmin"} {
		if c, err := client.Dial(path); err == nil {
			c.Close()
			t.Errorf("%q accepted", path)
		}
	}
}

func TestDialAllowsARealSocketOfTheSameUser(t *testing.T) {
	path := listen(t, func(conn net.Conn) {
		defer conn.Close()
		r := bufio.NewReader(conn)
		if _, err := control.ReadLine(r, control.MaxRequestBytes); err == nil {
			_ = control.WriteLine(conn, control.Response{V: control.Version, OK: true, Result: json.RawMessage(`{}`)})
		}
	})
	c, err := client.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Call(control.OpStatus, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestSilentPeerEndsWithTimeoutAfterTheFirstDeadline(t *testing.T) {
	path := listen(t, func(conn net.Conn) {
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	})
	defer client.SetFirstAnswerTimeout(100 * time.Millisecond)()
	c, err := client.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start := time.Now()
	err = c.Call(control.OpStatus, nil, nil)
	var pe *client.PeerError
	if !errors.As(err, &pe) || !pe.Silent || !strings.Contains(err.Error(), "timeout") || !strings.Contains(err.Error(), "sudo systemctl restart qsysmin") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

func TestAnswerAfterTheFirstOneMayTakeLonger(t *testing.T) {
	path := listen(t, func(conn net.Conn) {
		defer conn.Close()
		r := bufio.NewReader(conn)
		for i := 0; i < 2; i++ {
			if _, err := control.ReadLine(r, control.MaxRequestBytes); err != nil {
				return
			}
			if i == 1 {
				time.Sleep(300 * time.Millisecond)
			}
			_ = control.WriteLine(conn, control.Response{V: control.Version, OK: true})
		}
	})
	defer client.SetFirstAnswerTimeout(150 * time.Millisecond)()
	c, err := client.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 2; i++ {
		if err := c.Call(control.OpStatus, nil, nil); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}

// answering serves one connection that answers every request with code.
func answering(t *testing.T, code string) string {
	t.Helper()
	ln, path := socket(t)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 256)
		_, _ = conn.Read(buf)
		_ = control.WriteLine(conn, control.Response{V: control.Version, Error: code})
	}()
	return path
}

func TestDialSocketPermissionNamesTheCallersStanding(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root is never refused by file permissions")
	}
	_, path := socket(t)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name           string
		admin, enabled bool
		lacks          bool
		want           error
	}{
		{"not an administrator", false, false, true, client.ErrReserved},
		{"not an administrator, enabled", false, true, true, client.ErrReserved},
		{"administrator, not enabled", true, false, false, client.ErrNotEnabled},
		{"enabled administrator, old session", true, true, true, client.ErrSessionGroup},
	} {
		restoreStanding := client.SetStandingForTests(tc.admin, tc.enabled)
		restoreSession := client.SetSessionLacksGroup(tc.lacks)
		_, err := client.Dial(path)
		restoreSession()
		restoreStanding()
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	restoreStanding := client.SetStandingForTests(true, true)
	restoreSession := client.SetSessionLacksGroup(false)
	defer restoreSession()
	defer restoreStanding()
	if _, err := client.Dial(path); err == nil || errors.Is(err, client.ErrReserved) || errors.Is(err, client.ErrNotEnabled) ||
		errors.Is(err, client.ErrSessionGroup) {
		t.Errorf("enabled administrator with the group: err = %v, want the plain connect error", err)
	}
}

func TestCallNamesTheCallersStandingForANotAdminAnswer(t *testing.T) {
	defer client.SetStandingForTests(true, true)()
	c, err := client.Dial(answering(t, control.CodeNotAdmin))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Status(); !errors.Is(err, client.ErrReserved) {
		t.Errorf("err = %v, want ErrReserved even for an administrator in the database", err)
	}
}

func TestCallNamesTheCallersStandingForARefusedAnswer(t *testing.T) {
	for _, tc := range []struct {
		name           string
		admin, enabled bool
		lacks          bool
		want           error
	}{
		{"not an administrator", false, false, false, client.ErrReserved},
		{"administrator, not enabled", true, false, false, client.ErrNotEnabled},
		{"enabled administrator, old session", true, true, true, client.ErrSessionGroup},
	} {
		restoreStanding := client.SetStandingForTests(tc.admin, tc.enabled)
		restoreSession := client.SetSessionLacksGroup(tc.lacks)
		c, err := client.Dial(answering(t, control.CodeRefused))
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Status()
		c.Close()
		restoreSession()
		restoreStanding()
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}
