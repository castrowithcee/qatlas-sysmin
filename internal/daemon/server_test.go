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
	"sync/atomic"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/sockettest"
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

func anyAdmin(uint32) (bool, error) { return true, nil }

// endpoint is a channel in a fresh directory that admits the test process by its group.
func endpoint(t *testing.T) localipc.ServiceEndpoint {
	t.Helper()
	path, group := sockettest.Path(t)
	ep, err := control.Endpoint(path)
	if err != nil {
		t.Fatal(err)
	}
	ep.Group = group
	return ep
}

type harness struct {
	path   string
	log    *memLog
	srv    *Server
	cancel context.CancelFunc
	done   chan error
}

func start(t *testing.T, cfg Config) *harness {
	t.Helper()
	return startWith(t, cfg, nil)
}

// startWith also lets wrap stand between the channel's listener and Serve.
func startWith(t *testing.T, cfg Config, wrap func(net.Listener) net.Listener) *harness {
	t.Helper()
	ep := endpoint(t)
	path, err := ep.Path()
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{path: path, log: &memLog{}, done: make(chan error, 1)}
	cfg.Logger = h.log
	cfg.Version = "test"
	if cfg.IsAdmin == nil {
		cfg.IsAdmin = anyAdmin
	}
	h.srv, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	ln, err := h.srv.Listen(ep)
	if err != nil {
		t.Fatal(err)
	}
	if wrap != nil {
		ln = wrap(ln)
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
	info, err := os.Lstat(h.path)
	if err != nil || info.Mode().Perm() != 0o660 {
		t.Fatalf("socket = %v, %v", info, err)
	}
	if info, err := os.Lstat(h.path + ".lock"); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("lock = %v, %v", info, err)
	}
}

func TestAdminCheck(t *testing.T) {
	no := func(uint32) (bool, error) { return false, nil }
	broken := func(uint32) (bool, error) { return true, errors.New("unreadable") }
	for _, tc := range []struct {
		name       string
		uid        uint32
		inDatabase func(uint32) (bool, error)
		want       bool
	}{
		{"root without database", 0, broken, true},
		{"administrator", 1234, anyAdmin, true},
		{"no administrator", 1234, no, false},
		{"database error", 1234, broken, false},
	} {
		if got := isAdmin(tc.inDatabase, tc.uid); got != tc.want {
			t.Errorf("%s: isAdmin = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAdminInDatabase(t *testing.T) {
	const accounts = "root:x:0:0::/root:/bin/sh\ndemo-user:x:1001:1001::/home/demo-user:/bin/sh\n" +
		"demo-admin:x:1002:1002::/home/demo-admin:/bin/sh\ndemo-primary:x:1003:27::/home/p:/bin/sh\n" +
		"broken\ndemo-other:x:1004:1004::/home/o:/bin/sh\n"
	const groups = "qsysmin:x:990:demo-user,demo-admin\nsudo:x:27:demo-admin,demo-other-name\n"
	for _, tc := range []struct {
		name             string
		groups, accounts string
		uid              uint32
		want             bool
	}{
		{"listed member", groups, accounts, 1002, true},
		{"primary group", groups, accounts, 1003, true},
		{"not a member", groups, accounts, 1001, false},
		{"name prefix is no member", groups, accounts, 1004, false},
		{"unknown uid", groups, accounts, 4000, false},
		{"group missing", "qsysmin:x:990:demo-admin\n", accounts, 1002, false},
		{"malformed group line", "sudo:27\n", accounts, 1002, false},
	} {
		got, err := adminInDatabase(strings.NewReader(tc.groups), strings.NewReader(tc.accounts), tc.uid)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

func TestRevocationAppliesToOpenConnection(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root needs no admin right")
	}
	var revoked atomic.Bool
	var asked atomic.Uint32
	h := start(t, Config{IsAdmin: func(uid uint32) (bool, error) { asked.Store(uid); return !revoked.Load(), nil }})
	conn, br := h.raw(t)
	control.WriteLine(conn, control.Request{V: control.Version, Op: control.OpStatus})
	if resp := readResp(t, br); !resp.OK {
		t.Fatalf("first resp = %+v", resp)
	}
	h.log.wait(t, 1)
	if asked.Load() != uint32(os.Getuid()) {
		t.Fatalf("admin right asked for uid %d", asked.Load())
	}
	revoked.Store(true)
	control.WriteLine(conn, control.Request{V: control.Version, Op: control.OpStatus})
	resp := readResp(t, br)
	if resp.OK || resp.Error != control.CodeNotAdmin || resp.Result != nil {
		t.Fatalf("second resp = %+v", resp)
	}
	e := h.log.wait(t, 2)[1]
	if e.Action != auditlog.OperationControlRefuse || e.ErrorCode != control.CodeNotAdmin || e.ClientUID != uint32(os.Getuid()) || e.Result != auditlog.ResultFailure {
		t.Fatalf("entry = %+v", e)
	}
	if _, err := br.ReadByte(); err == nil {
		t.Error("connection stayed open")
	}
}

func TestNoAdminIsRefusedBeforeItSendsAnything(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root needs no admin right")
	}
	h := start(t, Config{IsAdmin: func(uint32) (bool, error) { return false, nil }})
	_, br := h.raw(t)
	if resp := readResp(t, br); resp.OK || resp.Error != control.CodeNotAdmin || resp.Result != nil {
		t.Fatalf("resp = %+v", resp)
	}
	e := h.log.wait(t, 1)[0]
	if e.Action != auditlog.OperationControlRefuse || e.ErrorCode != control.CodeNotAdmin || e.ClientUID != uint32(os.Getuid()) {
		t.Fatalf("entry = %+v", e)
	}
	if _, err := br.ReadByte(); err == nil {
		t.Error("connection stayed open")
	}
}

// unixPair is a connected pair of Unix sockets, so the kernel reports a peer on either end.
func unixPair(t *testing.T) (server, peer net.Conn) {
	t.Helper()
	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "p.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()
	peer, err = net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server = <-accepted
	if server == nil {
		t.Fatal("no connection accepted")
	}
	t.Cleanup(func() { server.Close(); peer.Close() })
	return server, peer
}

func TestRefusedPeerIsAnsweredAndLogged(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pair    func(t *testing.T) (net.Conn, net.Conn)
		wantUID uint32
	}{
		{"peer with credentials", unixPair, uint32(os.Getuid())},
		{"peer without credentials", func(*testing.T) (net.Conn, net.Conn) { return net.Pipe() }, NoUID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &memLog{}
			srv, err := New(Config{Logger: log, IsAdmin: anyAdmin})
			if err != nil {
				t.Fatal(err)
			}
			server, peer := tc.pair(t)
			_ = peer.SetDeadline(time.Now().Add(5 * time.Second))
			go func() {
				srv.refused(server, &localipc.PeerError{Err: localipc.ErrRefused})
				server.Close()
			}()
			br := bufio.NewReader(peer)
			if resp := readResp(t, br); resp.OK || resp.Error != control.CodeRefused || resp.Result != nil {
				t.Fatalf("resp = %+v", resp)
			}
			e := log.wait(t, 1)[0]
			if e.Action != auditlog.OperationControlRefuse || e.Result != auditlog.ResultFailure ||
				e.ErrorCode != control.CodeRefused || e.ClientUID != tc.wantUID {
				t.Fatalf("entry = %+v", e)
			}
		})
	}
}

// refusingFirst is a listener whose first Accept reports a peer the channel refused.
type refusingFirst struct {
	net.Listener
	once sync.Once
}

func (l *refusingFirst) Accept() (net.Conn, error) {
	refused := false
	l.once.Do(func() { refused = true })
	if refused {
		return nil, &localipc.PeerError{Err: localipc.ErrRefused}
	}
	return l.Listener.Accept()
}

func TestServeGoesOnAfterARefusedPeer(t *testing.T) {
	h := startWith(t, Config{}, func(ln net.Listener) net.Listener { return &refusingFirst{Listener: ln} })
	c, err := client.Dial(h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Status(); err != nil {
		t.Fatal(err)
	}
}

func TestSecondListenerIsRefused(t *testing.T) {
	ep := endpoint(t)
	srv, err := New(Config{Logger: &memLog{}})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := srv.Listen(ep)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, err := srv.Listen(ep); !errors.Is(err, localipc.ErrRunning) {
		t.Fatalf("second listener: %v", err)
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
		err := h.srv.Register("change", Handler{Action: auditlog.OperationConfigApply, Mutating: true,
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
	h.srv.Register("boom", Handler{Action: auditlog.OperationConfigRead,
		Func: func(context.Context, *Conn, json.RawMessage) (any, error) { panic("secret-value") }})
	h.srv.Register("fail", Handler{Action: auditlog.OperationConfigRead,
		Func: func(context.Context, *Conn, json.RawMessage) (any, error) { return nil, errors.New("secret-value") }})
	conn, br := h.raw(t)
	for _, op := range []string{"boom", "fail"} {
		control.WriteLine(conn, control.Request{V: 1, Op: op})
		if r := readResp(t, br); r.Error != control.CodeInternal {
			t.Fatalf("%s: resp = %+v", op, r)
		}
	}
	if err := h.srv.Register("fail", Handler{Action: auditlog.OperationConfigRead, Func: func(context.Context, *Conn, json.RawMessage) (any, error) { return nil, nil }}); err == nil {
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
	h.srv.Register("state", Handler{Action: auditlog.OperationConfigRead,
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

func TestShutdownDuringAcceptDoesNotHang(t *testing.T) {
	ep := endpoint(t)
	path, _ := ep.Path()
	srv, err := New(Config{Logger: &memLog{}, IsAdmin: anyAdmin})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// End the server after a connection is accepted and once the shutdown pass has finished.
	srv.afterAccept = func(stopped <-chan struct{}) { cancel(); <-stopped }
	ln, err := srv.Listen(ep)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, ln) }()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return: a connection accepted during shutdown was left open")
	}
}

func TestHandlerAuditsHostChangeForTheNamedUser(t *testing.T) {
	h := start(t, Config{})
	err := h.srv.Register("host", Handler{
		Action: auditlog.OperationHostChange,
		Func: func(_ context.Context, c *Conn, _ json.RawMessage) (any, error) {
			c.AuditHost(1234, auditlog.Host{ActionID: "demo.change", JournalID: "20260101T000000.000000Z-abcdef"}, control.CodeInternal)
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.srv.Register("plain", Handler{
		Action: auditlog.OperationConfigRead,
		Func:   func(context.Context, *Conn, json.RawMessage) (any, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	c, err := client.Dial(h.path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Call("host", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Call("plain", nil, nil); err != nil {
		t.Fatal(err)
	}
	got := h.log.wait(t, 2)
	if len(got) != 2 {
		t.Fatalf("entries = %+v", got)
	}
	if got[0].ClientUID != 1234 || got[0].Host == nil || got[0].Host.ActionID != "demo.change" ||
		got[0].Result != auditlog.ResultFailure || got[0].ErrorCode != control.CodeInternal ||
		got[1].Action != auditlog.OperationConfigRead || got[1].Host != nil {
		t.Fatalf("entries = %+v", got)
	}
}

// The test process's own user decides the case: root runs the handler, everyone else is refused.
func TestRootOnlyHandlerRefusesOthersBeforeItRuns(t *testing.T) {
	uid, wantRan := uint32(os.Getuid()), os.Geteuid() == 0
	var ran bool
	h := start(t, Config{})
	err := h.srv.Register("rooted", Handler{Action: auditlog.OperationConfigRead, RootOnly: true,
		Func: func(context.Context, *Conn, json.RawMessage) (any, error) { ran = true; return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	conn, br := h.raw(t)
	control.WriteLine(conn, control.Request{V: control.Version, Op: "rooted"})
	resp := readResp(t, br)
	e := h.log.wait(t, 1)[0]
	if ran != wantRan || resp.OK != wantRan {
		t.Fatalf("ran = %v, resp = %+v", ran, resp)
	}
	if !wantRan && (resp.Error != control.CodeNotRoot || e.Action != auditlog.OperationControlRefuse ||
		e.ErrorCode != control.CodeNotRoot || e.ClientUID != uid || e.Result != auditlog.ResultFailure) {
		t.Fatalf("resp = %+v entry = %+v", resp, e)
	}
}
