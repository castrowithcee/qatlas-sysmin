// Package daemon is the core of the service's control socket: it serves the service channel of localipc,
// which admits only root and the service group, checks that the peer is an administrator, reads requests
// within size and time limits, hands them to registered handlers, and logs every request and refusal.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// maxConns bounds the connections served at once.
const maxConns = 64

// AuditLogger is where the server records requests; *auditlog.Logger satisfies it.
type AuditLogger interface {
	Append(auditlog.Fields) error
}

// Conn is what a handler learns about the client it serves. State belongs to the connection: Config.NewState
// creates it when the client is accepted, and Config.ConnClosed ends it.
type Conn struct {
	Peer  Peer
	State any

	hostEntry *hostEntry
}

// hostEntry is the outcome of a host change, which is logged for the human behind sudo; code is empty for
// success.
type hostEntry struct {
	uid  uint32
	host auditlog.Host
	code string
}

// AuditHost records the outcome of the running request as a host change made by the user uid, with the
// error code of a change that did not stand or an empty one. The entry names uid as its client instead of
// the peer, which is root. A handler serves one request at a time per connection, so this needs no lock.
func (c *Conn) AuditHost(uid uint32, host auditlog.Host, code string) {
	c.hostEntry = &hostEntry{uid: uid, host: host, code: code}
}

// HandlerFunc serves one request. A non-nil result is encoded as the answer's result. An error that is a
// *control.Error answers with its code; any other answers control.CodeInternal, its text never leaves.
type HandlerFunc func(ctx context.Context, c *Conn, params json.RawMessage) (any, error)

// Handler is one registered operation. Action is the audit action its requests are logged under. Mutating
// marks an operation that changes state; Config.Guard runs before it. RootOnly refuses every peer but root
// with control.CodeNotRoot before the guard and the handler run; the refusal is logged as a control refusal.
type Handler struct {
	Action   string
	Mutating bool
	RootOnly bool
	Func     HandlerFunc
}

// Guard decides whether a client may run the mutating operation op now, per request. A *control.Error
// refuses with its code; any other error refuses with control.CodeInternal.
type Guard func(ctx context.Context, c *Conn, op string) error

// Config configures a Server. Logger is required; the other fields have defaults.
type Config struct {
	// Logger records every request. SetLogger replaces it while the server runs.
	Logger AuditLogger
	// OnLogError is told about a log write that failed; the request's outcome is unaffected. Default: slog.
	OnLogError func(error)
	// Version is the program version the status operation reports.
	Version string
	// Status, if set, completes the answer of the built-in status operation with what only the owner of the
	// server knows, including what belongs to the asking connection.
	Status func(*Conn, *control.StatusResult)
	// IsAdmin reports whether a peer that is not root is an administrator. Default: the group database.
	IsAdmin func(uid uint32) (bool, error)
	// Guard runs before every mutating handler. With none installed, mutating operations are answered
	// control.CodeUnavailable.
	Guard Guard
	// NewState and ConnClosed create and end the per-connection state of Conn.
	NewState   func(*Conn) any
	ConnClosed func(*Conn)

	MaxRequestBytes int
	ReadTimeout     time.Duration
	IdleTimeout     time.Duration
	WriteTimeout    time.Duration
}

// Server serves the control socket.
type Server struct {
	cfg Config

	mu       sync.Mutex
	logger   AuditLogger
	handlers map[string]Handler

	// afterAccept, if set, runs after Accept and before the connection is registered; tests use it to
	// force a shutdown into that window. It gets the channel that closes once the shutdown pass is done.
	afterAccept func(stopped <-chan struct{})
}

// New returns a server with the built-in status operation registered.
func New(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		return nil, errors.New("a server needs an audit logger")
	}
	if cfg.OnLogError == nil {
		cfg.OnLogError = func(err error) { slog.Warn("cannot write the audit log", "error", err.Error()) }
	}
	if cfg.IsAdmin == nil {
		cfg.IsAdmin = systemAdmin
	}
	if cfg.MaxRequestBytes <= 0 {
		cfg.MaxRequestBytes = control.MaxRequestBytes
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = control.ReadTimeout
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = control.IdleTimeout
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = control.WriteTimeout
	}
	s := &Server{cfg: cfg, logger: cfg.Logger, handlers: map[string]Handler{}}
	err := s.Register(control.OpStatus, Handler{
		Action: auditlog.OperationControlStatus,
		Func: func(_ context.Context, c *Conn, _ json.RawMessage) (any, error) {
			st := control.StatusResult{Protocol: control.Version, Version: cfg.Version}
			if cfg.Status != nil {
				cfg.Status(c, &st)
			}
			return st, nil
		},
	})
	return s, err
}

// Register adds the handler for op. An operation is registered once, and its action must be a known one.
func (s *Server) Register(op string, h Handler) error {
	if op == "" || h.Func == nil {
		return errors.New("an operation needs a name and a function")
	}
	if !auditlog.KnownOperation(h.Action) {
		return fmt.Errorf("operation %q: action is not a known operation", op)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.handlers[op]; ok {
		return fmt.Errorf("operation %q is already registered", op)
	}
	s.handlers[op] = h
	return nil
}

// SetLogger replaces the audit logger, for example once the log key is known.
func (s *Server) SetLogger(l AuditLogger) {
	if l == nil {
		return
	}
	s.mu.Lock()
	s.logger = l
	s.mu.Unlock()
}

// Listen opens the channel of ep. A peer the channel refuses is answered control.CodeRefused and logged; it
// never reaches Serve.
func (s *Server) Listen(ep localipc.ServiceEndpoint) (net.Listener, error) {
	return ep.ListenWith(localipc.ListenOptions{Refused: s.refused})
}

// ListenAndServe listens on ep and serves until ctx ends; see Serve.
func (s *Server) ListenAndServe(ctx context.Context, ep localipc.ServiceEndpoint) error {
	ln, err := s.Listen(ep)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

// Serve accepts connections on ln until ctx ends, then closes ln (which removes its socket file), closes the
// open connections and waits for their handlers. It returns nil after a shutdown by ctx.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		conns = map[net.Conn]struct{}{}
	)
	stopped := make(chan struct{})
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		mu.Lock()
		for c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		close(stopped)
	}()
	defer func() { cancel(); <-stopped; wg.Wait() }()

	sem := make(chan struct{}, maxConns)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			var refused *localipc.PeerError
			if errors.As(err, &ne) && ne.Timeout() || errors.As(err, &refused) {
				continue
			}
			return fmt.Errorf("cannot accept connections: %w", err)
		}
		if s.afterAccept != nil {
			s.afterAccept(stopped)
		}
		mu.Lock()
		if ctx.Err() != nil { // the shutdown pass may be over: nobody else would close this connection
			mu.Unlock()
			_ = conn.Close()
			return nil
		}
		conns[conn] = struct{}{}
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				_ = conn.Close()
				mu.Lock()
				delete(conns, conn)
				mu.Unlock()
			}()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				s.serveConn(ctx, conn)
			default:
				s.refuse(conn, control.CodeBusy)
			}
		}()
	}
}

// refuse logs and answers a connection that is not served, with a fixed code and nothing else.
func (s *Server) refuse(conn net.Conn, code string) {
	s.record(peerUID(conn), auditlog.OperationControlRefuse, time.Now(), code)
	s.write(conn, control.Response{V: control.Version, Error: code})
}

// refused answers a peer the channel turned away. It runs under the channel's own short deadline, so it
// writes without setting one.
func (s *Server) refused(conn net.Conn, _ error) {
	s.record(peerUID(conn), auditlog.OperationControlRefuse, time.Now(), control.CodeRefused)
	_ = control.WriteLine(conn, control.Response{V: control.Version, Error: control.CodeRefused})
}

// peerUID is the user of the peer as the kernel reports it, or NoUID if it cannot be read.
func peerUID(conn net.Conn) uint32 {
	uid, err := localipc.PeerUID(conn)
	if err != nil {
		return NoUID
	}
	return uid
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	// The peer is checked before anything is read from it.
	peer := Peer{UID: peerUID(conn)}
	if peer.UID == NoUID {
		s.record(NoUID, auditlog.OperationControlRefuse, time.Now(), control.CodeRefused)
		s.write(conn, control.Response{V: control.Version, Error: control.CodeRefused})
		return
	}
	if !isAdmin(s.cfg.IsAdmin, peer.UID) {
		s.record(peer.UID, auditlog.OperationControlRefuse, time.Now(), control.CodeNotAdmin)
		s.write(conn, control.Response{V: control.Version, Error: control.CodeNotAdmin})
		return
	}

	c := &Conn{Peer: peer}
	if s.cfg.NewState != nil {
		c.State = s.cfg.NewState(c)
	}
	if s.cfg.ConnClosed != nil {
		defer s.cfg.ConnClosed(c)
	}

	br := bufio.NewReaderSize(conn, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(s.cfg.IdleTimeout))
		if _, err := br.Peek(1); err != nil {
			return // closed by the client, idle too long, or shut down: no request was made
		}
		start := time.Now()
		_ = conn.SetReadDeadline(start.Add(s.cfg.ReadTimeout))
		line, err := control.ReadLine(br, s.cfg.MaxRequestBytes)
		if err != nil {
			code := control.CodeBadRequest
			var ne net.Error
			switch {
			case errors.Is(err, control.ErrLineTooLong):
				code = control.CodeTooLarge
			case errors.As(err, &ne) && ne.Timeout():
				code = control.CodeTimeout
			case ctx.Err() != nil:
				return
			}
			s.record(peer.UID, auditlog.OperationControlRequest, start, code)
			s.write(conn, control.Response{V: control.Version, Error: code})
			return
		}

		// Admin rights are read again for every request: a revocation applies to an open connection too.
		if !isAdmin(s.cfg.IsAdmin, peer.UID) {
			clear(line)
			s.record(peer.UID, auditlog.OperationControlRefuse, start, control.CodeNotAdmin)
			s.write(conn, control.Response{V: control.Version, Error: control.CodeNotAdmin})
			return
		}

		c.hostEntry = nil
		action, resp, code := s.dispatch(ctx, c, line)
		clear(line) // a request may carry a secret
		if e := c.hostEntry; e != nil {
			s.recordFields(auditlog.Fields{ClientUID: e.uid, Action: action, Host: &e.host}, start, e.code)
		} else {
			s.record(peer.UID, action, start, code)
		}
		if !s.write(conn, resp) {
			return
		}
	}
}

// dispatch runs one request line and returns the audit action, the answer and the error code, if any.
func (s *Server) dispatch(ctx context.Context, c *Conn, line []byte) (string, control.Response, string) {
	fail := func(action, code string) (string, control.Response, string) {
		return action, control.Response{V: control.Version, Error: code}, code
	}
	var req control.Request
	if err := json.Unmarshal(line, &req); err != nil {
		return fail(auditlog.OperationControlRequest, control.CodeBadRequest)
	}
	if req.V != control.Version {
		return fail(auditlog.OperationControlRequest, control.CodeVersion)
	}
	s.mu.Lock()
	h, ok := s.handlers[req.Op]
	s.mu.Unlock()
	if !ok {
		return fail(auditlog.OperationControlRequest, control.CodeUnknownOp)
	}
	if h.RootOnly && c.Peer.UID != 0 {
		return fail(auditlog.OperationControlRefuse, control.CodeNotRoot)
	}
	if h.Mutating {
		if s.cfg.Guard == nil {
			return fail(h.Action, control.CodeUnavailable)
		}
		if err := s.cfg.Guard(ctx, c, req.Op); err != nil {
			return fail(h.Action, codeOf(err))
		}
	}
	result, err := s.call(ctx, h, c, req.Params)
	if err != nil {
		code := codeOf(err)
		return fail(h.Action, code)
	}
	resp := control.Response{V: control.Version, OK: true}
	if result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			return fail(h.Action, control.CodeInternal)
		}
		resp.Result = data
	}
	return h.Action, resp, ""
}

// call runs the handler; a panic in it becomes an internal error instead of ending the service.
func (s *Server) call(ctx context.Context, h Handler, c *Conn, params json.RawMessage) (result any, err error) {
	defer func() {
		if recover() != nil {
			result, err = nil, &control.Error{Code: control.CodeInternal}
		}
	}()
	return h.Func(ctx, c, params)
}

func codeOf(err error) string {
	var ce *control.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return control.CodeInternal
}

// write sends one answer within the write timeout and reports whether it left.
func (s *Server) write(conn net.Conn, resp control.Response) bool {
	_ = conn.SetWriteDeadline(time.Now().Add(s.cfg.WriteTimeout))
	return control.WriteLine(conn, resp) == nil
}

// record appends the one entry a request leaves. A failed write is reported and changes nothing else.
func (s *Server) record(uid uint32, action string, start time.Time, code string) {
	s.recordFields(auditlog.Fields{ClientUID: uid, Action: action}, start, code)
}

func (s *Server) recordFields(f auditlog.Fields, start time.Time, code string) {
	f.Result, f.Duration = auditlog.ResultSuccess, time.Since(start)
	if code != "" {
		f.Result, f.ErrorCode = auditlog.ResultFailure, code
	}
	s.mu.Lock()
	l := s.logger
	s.mu.Unlock()
	if err := l.Append(f); err != nil {
		s.cfg.OnLogError(err)
	}
}
