// Package service is the running qsysmin service: it writes the audit log and serves the control socket of
// package daemon, to which only administrators and root are admitted.
package service

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"path/filepath"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

// DefaultStateDir is the state directory of the service.
const DefaultStateDir = "/var/lib/qsysmin"

// Config configures a service. Only the state directory and the socket are usually set.
type Config struct {
	StateDir   string
	SocketPath string
	Version    string

	// Resolver answers the peer check's group questions; nil uses the system's groups.
	Resolver daemon.Resolver
	// Credentials reads the peer of a connection; nil uses SO_PEERCRED. Tests name a root or a foreign peer.
	Credentials func(net.Conn) (daemon.Peer, error)
	// OnLogError is told about audit log writes that failed; nil logs them with slog.
	OnLogError func(error)
}

// Service is the audit log and the control socket server around it.
type Service struct {
	cfg      Config
	log      *auditlog.Logger
	server   *daemon.Server
	listener *net.UnixListener
}

// Run opens the service, listens on the socket and serves until ctx ends.
func Run(ctx context.Context, cfg Config) error {
	s, err := Open(cfg)
	if err != nil {
		return err
	}
	if err := s.Listen(); err != nil {
		return err
	}
	return s.Serve(ctx)
}

// Open prepares the service. It needs no setup: the first admin's request is served at once. Nothing
// listens yet.
func Open(cfg Config) (*Service, error) {
	if cfg.StateDir == "" {
		cfg.StateDir = DefaultStateDir
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = control.DefaultSocketPath
	}
	s := &Service{cfg: cfg, log: auditlog.New(filepath.Join(cfg.StateDir, "logs"), auditlog.DefaultRetentionDays)}

	srv, err := daemon.New(daemon.Config{
		Logger:      s.log,
		OnLogError:  cfg.OnLogError,
		Version:     cfg.Version,
		Resolver:    cfg.Resolver,
		Credentials: cfg.Credentials,
		Status:      s.fillStatus,
	})
	if err != nil {
		return nil, err
	}
	s.server = srv
	if err := s.register(); err != nil {
		return nil, err
	}
	return s, nil
}

// Server returns the control socket server, so further operations can be registered on it before Serve.
func (s *Service) Server() *daemon.Server { return s.server }

// Listen opens the control socket.
func (s *Service) Listen() error {
	ln, err := daemon.Listen(s.cfg.SocketPath)
	if err != nil {
		return err
	}
	s.listener = ln
	return nil
}

// Serve serves the control socket until ctx ends.
func (s *Service) Serve(ctx context.Context) error {
	if s.listener == nil {
		return errors.New("the service is not listening")
	}
	return s.server.Serve(ctx, s.listener)
}

func (s *Service) fillStatus(_ *daemon.Conn, r *control.StatusResult) {
	r.Socket = s.cfg.SocketPath
}

func (s *Service) register() error {
	for _, reg := range []func() error{s.registerLog, s.registerHost} {
		if err := reg(); err != nil {
			return err
		}
	}
	return nil
}

func internalError(what string, err error) error {
	slog.Warn(what, "error", err.Error())
	return &control.Error{Code: control.CodeInternal}
}
