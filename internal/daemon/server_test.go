package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

type memLog struct {
	mu      sync.Mutex
	entries []auditlog.Fields
	err     error
}

func (m *memLog) Append(f auditlog.Fields) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := f.Validate(); err != nil {
		return err
	}
	m.entries = append(m.entries, f)
	return m.err
}

func (m *memLog) wait(t *testing.T, n int) []auditlog.Fields {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		m.mu.Lock()
		got := append([]auditlog.Fields(nil), m.entries...)
		m.mu.Unlock()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %d log entries, have %d", n, len(got))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type fakeResolver struct {
	gid    uint32
	groups []uint32
	err    error
}

func (f fakeResolver) GroupID(string) (uint32, error)    { return f.gid, f.err }
func (f fakeResolver) PeerGroups(Peer) ([]uint32, error) { return f.groups, nil }

type harness struct {
	path   string
	log    *memLog
	srv    *Server
	cancel context.CancelFunc
	done   chan error
}

func start(t *testing.T, cfg Config) *harness {
	t.Helper()
	dir, err := os.MkdirTemp("", "sysmin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	h := &harness{path: filepath.Join(dir, "s.sock"), log: &memLog{}, done: make(chan error, 1)}
	cfg.Logger = h.log
	cfg.Version = "test"
	if cfg.Resolver == nil {
		cfg.Resolver = fakeResolver{gid: 990, groups: []uint32{990}}
	}
	h.srv, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	ln, err := Listen(h.path)
	if err != nil {
		t.Fatal(err)
	}
	go func() { h.done <- h.srv.Serve(ctx, ln) }()
	t.Cleanup(func() { cancel(); <-h.done })
	return h
}

func (h *harness) raw(t *testing.T) (net.Conn, *bufio.Reader) {
	t.Helper()
	c, err := net.Dial("unix", h.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	return c, bufio.NewReader(c)
}

func readResp(t *testing.T, br *bufio.Reader) control.Response {
	t.Helper()
	line, err := control.ReadLine(br, control.MaxResponseBytes)
	if err != nil {
		t.Fatalf("read answer: %v", err)
	}
	var r control.Response
	if err := json.Unmarshal(line, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAllowedPeerIsServedViaClient(t *testing.T) {
	h := start(t, Config{})
	c, err := client.Dial(h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	st, err := c.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Protocol != control.Version || st.Version != "test" {
		t.Fatalf("status = %+v", st)
	}
	e := h.log.wait(t, 1)[0]
	if e.Action != auditlog.OperationControlStatus || e.Result != auditlog.ResultSuccess || e.ClientUID != uint32(os.Getuid()) {
		t.Fatalf("entry = %+v", e)
	}
	info, err := os.Stat(h.path)
	if err != nil || info.Mode().Perm() != SocketMode {
		t.Fatalf("socket mode = %v, %v", info.Mode(), err)
	}
}

func TestPeerCheck(t *testing.T) {
	cases := []struct {
		name string
		peer Peer
		res  fakeResolver
		want bool
	}{
		{"root", Peer{UID: 0, GID: 5}, fakeResolver{err: errors.New("no group")}, true},
		{"primary group", Peer{UID: 1234, GID: 990}, fakeResolver{gid: 990}, true},
		{"supplementary group", Peer{UID: 1234, GID: 100}, fakeResolver{gid: 990, groups: []uint32{4, 990}}, true},
		{"stranger", Peer{UID: 1234, GID: 100}, fakeResolver{gid: 990, groups: []uint32{4}}, false},
		{"group missing", Peer{UID: 1234, GID: 100}, fakeResolver{err: errors.New("no group")}, false},
	}
	for _, tc := range cases {
		if got := allowed(tc.res, tc.peer); got != tc.want {
			t.Errorf("%s: allowed = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestSimulatedPeers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		peer    Peer
		wantOK  bool
		wantUID uint32
	}{
		{"root", Peer{UID: 0, GID: 0}, true, 0},
		{"group member", Peer{UID: 4242, GID: 990}, true, 4242},
		{"stranger", Peer{UID: 4243, GID: 100}, false, 4243},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := start(t, Config{Resolver: fakeResolver{gid: 990}, Credentials: func(net.Conn) (Peer, error) { return tc.peer, nil }})
			conn, br := h.raw(t)
			if tc.wantOK {
				control.WriteLine(conn, control.Request{V: control.Version, Op: control.OpStatus})
			}
			resp := readResp(t, br)
			e := h.log.wait(t, 1)[0]
			if e.ClientUID != tc.wantUID {
				t.Errorf("logged uid = %d", e.ClientUID)
			}
			if tc.wantOK {
				if !resp.OK || e.Result != auditlog.ResultSuccess {
					t.Errorf("resp = %+v entry = %+v", resp, e)
				}
				return
			}
			if resp.OK || resp.Error != control.CodeRefused || resp.Result != nil {
				t.Errorf("resp = %+v", resp)
			}
			if e.Action != auditlog.OperationControlRefuse || e.Result != auditlog.ResultFailure || e.ErrorCode != control.CodeRefused {
				t.Errorf("entry = %+v", e)
			}
			// The refused peer is disconnected.
			if _, err := br.ReadByte(); err == nil {
				t.Error("connection stayed open")
			}
		})
	}
}

func TestRealPeerCredentials(t *testing.T) {
	h := start(t, Config{Resolver: fakeResolver{gid: uint32(os.Getgid())}})
	c, err := client.Dial(h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Status(); err != nil {
		t.Fatal(err)
	}
	if e := h.log.wait(t, 1)[0]; e.ClientUID != uint32(os.Getuid()) {
		t.Fatalf("uid = %d", e.ClientUID)
	}
}

func TestOversizedRequestAborts(t *testing.T) {
	h := start(t, Config{MaxRequestBytes: 256})
	conn, br := h.raw(t)
	big := make([]byte, 4096)
	for i := range big {
		big[i] = 'a'
	}
	_, _ = conn.Write(big)
	resp := readResp(t, br)
	if resp.Error != control.CodeTooLarge {
		t.Fatalf("resp = %+v", resp)
	}
	e := h.log.wait(t, 1)[0]
	if e.Action != auditlog.OperationControlRequest || e.ErrorCode != control.CodeTooLarge || e.Result != auditlog.ResultFailure {
		t.Fatalf("entry = %+v", e)
	}
	if _, err := br.ReadByte(); err == nil {
		t.Fatal("connection stayed open")
	}
}

func TestSlowRequestAborts(t *testing.T) {
	h := start(t, Config{ReadTimeout: 100 * time.Millisecond})
	conn, br := h.raw(t)
	_, _ = conn.Write([]byte(`{"v":1,"op":`)) // never finished
	resp := readResp(t, br)
	if resp.Error != control.CodeTimeout {
		t.Fatalf("resp = %+v", resp)
	}
	e := h.log.wait(t, 1)[0]
	if e.ErrorCode != control.CodeTimeout || e.Result != auditlog.ResultFailure {
		t.Fatalf("entry = %+v", e)
	}
}

func TestIdleConnectionClosesWithoutEntry(t *testing.T) {
	h := start(t, Config{IdleTimeout: 100 * time.Millisecond})
	_, br := h.raw(t)
	if _, err := br.ReadByte(); err == nil {
		t.Fatal("idle connection stayed open")
	}
	h.log.mu.Lock()
	defer h.log.mu.Unlock()
	if len(h.log.entries) != 0 {
		t.Fatalf("entries = %+v", h.log.entries)
	}
}

func TestInvalidRequestsAreLogged(t *testing.T) {
	h := start(t, Config{})
	conn, br := h.raw(t)
	for i, tc := range []struct{ line, code string }{
		{`{"v":1,"op":"nope"}`, control.CodeUnknownOp},
		{`{"v":99,"op":"status"}`, control.CodeVersion},
		{`not json`, control.CodeBadRequest},
	} {
		_, _ = conn.Write([]byte(tc.line + "\n"))
		if r := readResp(t, br); r.OK || r.Error != tc.code {
			t.Fatalf("%d: resp = %+v", i, r)
		}
		e := h.log.wait(t, i+1)[i]
		if e.Action != auditlog.OperationControlRequest || e.ErrorCode != tc.code || e.ClientUID != uint32(os.Getuid()) {
			t.Fatalf("%d: entry = %+v", i, e)
		}
	}
}

func TestMutatingNeedsGuard(t *testing.T) {
	var calls int
	reg := func(t *testing.T, h *harness) {
		err := h.srv.Register("change", Handler{Action: auditlog.OperationVaultLock, Mutating: true,
			Func: func(context.Context, *Conn, json.RawMessage) (any, error) { calls++; return nil, nil }})
		if err != nil {
			t.Fatal(err)
		}
	}
	h := start(t, Config{})
	reg(t, h)
	conn, br := h.raw(t)
	control.WriteLine(conn, control.Request{V: 1, Op: "change"})
	if r := readResp(t, br); r.Error != control.CodeUnavailable || calls != 0 {
		t.Fatalf("resp = %+v calls = %d", r, calls)
	}

	h = start(t, Config{Guard: func(context.Context, *Conn, string) error { return &control.Error{Code: "locked"} }})
	reg(t, h)
	conn, br = h.raw(t)
	control.WriteLine(conn, control.Request{V: 1, Op: "change"})
	if r := readResp(t, br); r.Error != "locked" || calls != 0 {
		t.Fatalf("resp = %+v calls = %d", r, calls)
	}
}

func TestHandlerErrorsAndPanicsHideText(t *testing.T) {
	h := start(t, Config{})
	h.srv.Register("boom", Handler{Action: auditlog.OperationVaultStatus,
		Func: func(context.Context, *Conn, json.RawMessage) (any, error) { panic("secret-value") }})
	h.srv.Register("fail", Handler{Action: auditlog.OperationVaultStatus,
		Func: func(context.Context, *Conn, json.RawMessage) (any, error) { return nil, errors.New("secret-value") }})
	conn, br := h.raw(t)
	for _, op := range []string{"boom", "fail"} {
		control.WriteLine(conn, control.Request{V: 1, Op: op})
		if r := readResp(t, br); r.Error != control.CodeInternal {
			t.Fatalf("%s: resp = %+v", op, r)
		}
	}
	if err := h.srv.Register("fail", Handler{Action: auditlog.OperationVaultStatus, Func: func(context.Context, *Conn, json.RawMessage) (any, error) { return nil, nil }}); err == nil {
		t.Error("duplicate registration accepted")
	}
	if err := h.srv.Register("x", Handler{Action: "no.such", Func: func(context.Context, *Conn, json.RawMessage) (any, error) { return nil, nil }}); err == nil {
		t.Error("unknown action accepted")
	}
}

func TestConnStateAndClose(t *testing.T) {
	closed := make(chan any, 1)
	h := start(t, Config{
		NewState:   func(*Conn) any { return "state-1" },
		ConnClosed: func(c *Conn) { closed <- c.State },
	})
	h.srv.Register("state", Handler{Action: auditlog.OperationVaultStatus,
		Func: func(_ context.Context, c *Conn, _ json.RawMessage) (any, error) { return c.State, nil }})
	conn, br := h.raw(t)
	control.WriteLine(conn, control.Request{V: 1, Op: "state"})
	if r := readResp(t, br); string(r.Result) != `"state-1"` {
		t.Fatalf("resp = %+v", r)
	}
	conn.Close()
	select {
	case s := <-closed:
		if s != "state-1" {
			t.Fatalf("state = %v", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ConnClosed not called")
	}
}

func TestLogFailureDoesNotChangeResult(t *testing.T) {
	var reported = make(chan error, 4)
	h := start(t, Config{OnLogError: func(err error) { reported <- err }})
	h.log.err = errors.New("disk full")
	c, err := client.Dial(h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Status(); err != nil {
		t.Fatalf("status failed: %v", err)
	}
	select {
	case <-reported:
	case <-time.After(3 * time.Second):
		t.Fatal("log failure not reported")
	}
}

func TestSetLoggerSwaps(t *testing.T) {
	h := start(t, Config{})
	second := &memLog{}
	h.srv.SetLogger(second)
	c, _ := client.Dial(h.path)
	defer c.Close()
	if _, err := c.Status(); err != nil {
		t.Fatal(err)
	}
	second.wait(t, 1)
}

func TestShutdownRemovesSocket(t *testing.T) {
	h := start(t, Config{})
	conn, _ := h.raw(t) // an open connection must not hold the shutdown
	_ = conn
	h.cancel()
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
	h.done <- nil
	if _, err := os.Lstat(h.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remains: %v", err)
	}
}

func TestListenReplacesOnlyDeadSockets(t *testing.T) {
	dir, _ := os.MkdirTemp("", "sysmin-")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s.sock")

	// A dead socket is replaced.
	dead, _ := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	dead.SetUnlinkOnClose(false)
	dead.Close()
	ln, err := Listen(path)
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	// A live one is refused.
	if _, err := Listen(path); !errors.Is(err, ErrRunning) {
		t.Fatalf("live socket: %v", err)
	}
	ln.Close()

	// A regular file is left alone.
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(path); err == nil {
		t.Fatal("regular file replaced")
	}
	if b, _ := os.ReadFile(path); string(b) != "keep" {
		t.Fatal("file was changed")
	}

	if _, err := Listen(filepath.Join(dir, string(make([]byte, 120)))); err == nil {
		t.Fatal("long path accepted")
	}
}

func TestParseGroups(t *testing.T) {
	got, err := parseGroups(stringsReader("Name:\tx\nGroups:\t4 24 990 \nUid:\t1\n"))
	if err != nil || len(got) != 3 || got[2] != 990 {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := parseGroups(stringsReader("Groups:\tx\n")); err == nil {
		t.Fatal("malformed list accepted")
	}
	g, err := SystemResolver{}.PeerGroups(Peer{PID: int32(os.Getpid())})
	if err != nil {
		t.Fatal(err)
	}
	_ = g
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }

func TestHandlerAuditsPerProject(t *testing.T) {
	h := start(t, Config{})
	err := h.srv.Register("multi", Handler{
		Action: auditlog.OperationApprovalGrant,
		Func: func(_ context.Context, c *Conn, _ json.RawMessage) (any, error) {
			c.Audit("demo-app", "")
			c.Audit("demo-two", control.CodeChanged)
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.srv.Register("plain", Handler{
		Action: auditlog.OperationApprovalList,
		Func:   func(context.Context, *Conn, json.RawMessage) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	c, err := client.Dial(h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Call("multi", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call("plain", nil, nil); err != nil {
		t.Fatal(err)
	}
	got := h.log.wait(t, 3)
	if len(got) != 3 {
		t.Fatalf("entries = %+v", got)
	}
	if got[0].Project != "demo-app" || got[0].Result != auditlog.ResultSuccess ||
		got[1].Project != "demo-two" || got[1].Result != auditlog.ResultFailure || got[1].ErrorCode != control.CodeChanged ||
		got[2].Action != auditlog.OperationApprovalList || got[2].Project != "" {
		t.Fatalf("entries = %+v", got)
	}
}
