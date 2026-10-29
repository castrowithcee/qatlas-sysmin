// Package daemon is the core of the service's control socket: it accepts connections, checks who connected,
// reads requests within size and time limits, hands them to registered handlers, and logs every request.
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
}

// HandlerFunc serves one request. A non-nil result is encoded as the answer's result. An error that is a
// *control.Error answers with its code; any other answers control.CodeInternal, its text never leaves.
type HandlerFunc func(ctx context.Context, c *Conn, params json.RawMessage) (any, error)

// Handler is one registered operation. Action is the audit action its requests are logged under. Mutating
// marks an operation that changes state; Config.Guard runs before it.
type Handler struct {
	Action   string
	Mutating bool
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
	// server knows.
	Status func(*control.StatusResult)
	// Resolver answers the peer check's group questions. Default: SystemResolver.
	Resolver Resolver
	// Credentials reads the peer of a connection. Default: SO_PEERCRED.
	Credentials func(net.Conn) (Peer, error)
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
}

// New returns a server with the built-in status operation registered.
func New(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		return nil, errors.New("a server needs an audit logger")
	}
	if cfg.OnLogError == nil {
		cfg.OnLogError = func(err error) { slog.Warn("cannot write the audit log", "error", err.Error()) }
	}
	if cfg.Resolver == nil {
		cfg.Resolver = SystemResolver{}
	}
	if cfg.Credentials == nil {
		cfg.Credentials = peerCred
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
		Func: func(context.Context, *Conn, json.RawMessage) (any, error) {
			st := control.StatusResult{Protocol: control.Version, Version: cfg.Version}
			if cfg.Status != nil {
				cfg.Status(&st)
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
	if err := (auditlog.Fields{Action: h.Action, Result: auditlog.ResultSuccess}).Validate(); err != nil {
		return fmt.Errorf("operation %q: %w", op, err)
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

// ListenAndServe listens on path and serves until ctx ends; see Serve.
func (s *Server) ListenAndServe(ctx context.Context, path string) error {
	ln, err := Listen(path)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

// Serve accepts connections on ln until ctx ends, then closes ln (which removes a Unix socket file), closes
// the open connections and waits for their handlers. It returns nil after a shutdown by ctx.
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
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return fmt.Errorf("cannot accept connections: %w", err)
		}
		mu.Lock()
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

// refuse logs and answers a connection that is not served, with a fixed code and nothing else. The peer is
// read again for the log, so the entry names who was turned away.
func (s *Server) refuse(conn net.Conn, code string) {
	uid := NoUID
	if p, err := s.cfg.Credentials(conn); err == nil {
		uid = p.UID
	}
	s.record(uid, auditlog.OperationControlRefuse, time.Now(), code)
	s.write(conn, control.Response{V: control.Version, Error: code})
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	// The peer is checked before anything is read from it.
	peer, err := s.cfg.Credentials(conn)
	if err != nil {
		s.record(NoUID, auditlog.OperationControlRefuse, time.Now(), control.CodeRefused)
		s.write(conn, control.Response{V: control.Version, Error: control.CodeRefused})
		return
	}
	if !allowed(s.cfg.Resolver, peer) {
		s.record(peer.UID, auditlog.OperationControlRefuse, time.Now(), control.CodeRefused)
		s.write(conn, control.Response{V: control.Version, Error: control.CodeRefused})
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

		action, resp, code := s.dispatch(ctx, c, line)
		clear(line) // a request may carry a passphrase
		s.record(peer.UID, action, start, code)
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
		return fail(h.Action, codeOf(err))
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
	f := auditlog.Fields{ClientUID: uid, Action: action, Result: auditlog.ResultSuccess, Duration: time.Since(start)}
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
