package client_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

func TestDialFailsWithoutService(t *testing.T) {
	dir, _ := os.MkdirTemp("", "sysmin-")
	defer os.RemoveAll(dir)
	if _, err := client.Dial(filepath.Join(dir, "none.sock")); err == nil {
		t.Fatal("dial succeeded")
	}
}

func TestCallReturnsFixedCode(t *testing.T) {
	dir, _ := os.MkdirTemp("", "sysmin-")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
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
	dir := t.TempDir()
	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	for _, lacks := range []bool{true, false} {
		restore := client.SetSessionLacksGroup(lacks)
		_, err := client.Dial(path)
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
	dir, _ := os.MkdirTemp("", "sysmin-")
	t.Cleanup(func() { os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
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
	c, err := client.Dial(filepath.Join(dir, "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, got
}

func TestAllAtOnceCallsCarryTheShownApprovals(t *testing.T) {
	c, got := requestsOf(t)
	shown := []control.ShownApproval{
		{Project: "demo-app", Kind: "changed", Fingerprint: "fp-demo-0001"},
		{Project: "gone-app", Kind: "deleted"},
	}
	if _, err := c.ApproveAll(shown); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DiscardAll(shown); err != nil {
		t.Fatal(err)
	}
	if err := c.Discard("demo-app", "fp-demo-0001"); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{control.OpApprovalsApproveAll, control.OpApprovalsDiscardAll} {
		req := <-got
		var p control.ApprovalsAllParams
		if err := json.Unmarshal(req.Params, &p); err != nil || req.Op != op || !reflect.DeepEqual(p.Shown, shown) {
			t.Errorf("request = %s %s, %v", req.Op, req.Params, err)
		}
	}
	req := <-got
	var p control.ApprovalParams
	if err := json.Unmarshal(req.Params, &p); err != nil || req.Op != control.OpApprovalsDiscard || p != (control.ApprovalParams{Project: "demo-app", Fingerprint: "fp-demo-0001"}) {
		t.Errorf("request = %s %s, %v", req.Op, req.Params, err)
	}
}

// listen opens a socket in a short temp directory and hands every accepted connection to h.
func listen(t *testing.T, h func(net.Conn)) string {
	t.Helper()
	dir, _ := os.MkdirTemp("", "sysmin-")
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
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

func TestDialRefusesAForeignPeerBeforeSendingAnything(t *testing.T) {
	received := make(chan int, 1)
	path := listen(t, func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 64)
		n, _ := conn.Read(buf) // returns at once with EOF once the client closed
		received <- n
	})
	own := uint32(os.Geteuid())
	defer client.SetPeer(4321, own+1, own+2, true)()
	c, err := client.Dial(path)
	if err == nil {
		c.Close()
		t.Fatal("dial succeeded")
	}
	var pe *client.PeerError
	if !errors.As(err, &pe) || pe.PID != 4321 || pe.UID != own+1 || pe.Silent {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"pid 4321", "uid", "sudo kill 4321", "sudo systemctl restart sysmin"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if n := <-received; n != 0 {
		t.Errorf("the peer received %d bytes", n)
	}
}

func TestDialFailsClosedWithoutPeerCredentials(t *testing.T) {
	path := listen(t, func(conn net.Conn) { conn.Close() })
	defer client.FailPeerRead()()
	if c, err := client.Dial(path); err == nil {
		c.Close()
		t.Fatal("dial succeeded")
	}
}

func TestDialAllowsTheCallerAndTheServiceUser(t *testing.T) {
	path := listen(t, func(conn net.Conn) { conn.Close() })
	own := uint32(os.Geteuid())
	for name, c := range map[string]struct {
		uid, svc uint32
		exists   bool
	}{
		"own uid":                      {own, own + 1, true},
		"own uid without service user": {own, 0, false},
		"service user":                 {own + 7, own + 7, true},
	} {
		t.Run(name, func(t *testing.T) {
			defer client.SetPeer(99, c.uid, c.svc, c.exists)()
			cl, err := client.Dial(path)
			if err != nil {
				t.Fatal(err)
			}
			cl.Close()
		})
	}
	// Without a service user only the caller is allowed.
	defer client.SetPeer(99, own+7, own+7, false)()
	if cl, err := client.Dial(path); err == nil {
		cl.Close()
		t.Fatal("a foreign uid was allowed without a service user")
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
	if !errors.As(err, &pe) || !pe.Silent || !strings.Contains(err.Error(), "timeout") || !strings.Contains(err.Error(), "sudo systemctl restart sysmin") {
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
