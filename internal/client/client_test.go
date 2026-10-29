package client_test

import (
	"errors"
	"net"
	"os"
	"path/filepath"
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
