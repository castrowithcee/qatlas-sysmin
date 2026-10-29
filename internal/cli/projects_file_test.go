package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

type discardLog struct{}

func (discardLog) Append(auditlog.Fields) error { return nil }

// recordingService serves the control socket with an add operation that records the parameters as they
// arrive, so a test sees the request the way the service receives it.
func recordingService(t *testing.T) (string, func() []control.ProjectParams) {
	t.Helper()
	dir, err := os.MkdirTemp("", "sysmin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	srv, err := daemon.New(daemon.Config{Logger: discardLog{}, Version: "9.9.9", Resolver: testGroups{}})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []control.ProjectParams
	err = srv.Register(control.OpProjectsAdd, daemon.Handler{Action: auditlog.OperationConfigApply,
		Func: func(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
			var p control.ProjectParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &control.Error{Code: control.CodeBadRequest}
			}
			mu.Lock()
			got = append(got, p)
			mu.Unlock()
			return control.ChangeResult{Project: p.Name, State: control.StatePending, Revision: "rev-1"}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s.sock")
	ln, err := daemon.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-done })
	return sock, func() []control.ProjectParams {
		mu.Lock()
		defer mu.Unlock()
		return append([]control.ProjectParams(nil), got...)
	}
}

// The files the guided setup of the terminal interface is tested to send reach the service from
// `project add NAME --file FILE` as the same request: the name and the file, unchanged.
func TestProjectAddFileSendsTheFileUnchanged(t *testing.T) {
	sock, got := recordingService(t)
	for i, c := range []struct{ name, file string }{{"demo-app", "flow_image-tag.yaml"}, {"demo-site", "flow_git-tag.yaml"}} {
		path := filepath.Join("..", "tui", "testdata", c.file)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := runCLIArgs("project", "add", c.name, "--file", path, "--socket", sock, "--format", "json")
		if code != exitOK {
			t.Fatalf("%s: code = %d, stdout = %q, stderr = %q", c.file, code, stdout, stderr)
		}
		requests := got()
		want := control.ProjectParams{Name: c.name, File: string(data)}
		if len(requests) != i+1 || !reflect.DeepEqual(requests[i], want) {
			t.Fatalf("%s: requests = %+v, want %+v", c.file, requests, want)
		}
	}
}
