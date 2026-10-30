package client_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"

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
