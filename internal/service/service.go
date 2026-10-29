// Package service is the running sysmin service: it holds the vault, opens it automatically or on request,
// keeps one admin session per connection and lets only an unlocked vault and a proven admin change
// anything. It configures the control socket server of package daemon.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/admin"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/autounlock"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// The default directories of the service.
const (
	DefaultConfigDir = "/etc/sysmin"
	DefaultStateDir  = "/var/lib/sysmin"
)

// Admin timeouts outside this range are ignored in favor of the default; the configuration validation
// enforces the same range.
const (
	minAdminTimeout = time.Second
	maxAdminTimeout = 24 * time.Hour
)

// Config configures a service. Only the directories and the socket are usually set.
type Config struct {
	ConfigDir  string
	StateDir   string
	SocketPath string
	Version    string

	// CredentialsDir is where the service manager put the vault key credential; empty reads
	// CREDENTIALS_DIRECTORY.
	CredentialsDir string
	// Resolver answers the peer check's group questions; nil uses the system's groups.
	Resolver daemon.Resolver
	// Clock is the admin sessions' clock; nil is time.Now.
	Clock func() time.Time
	// OnLogError is told about audit log writes that failed; nil logs them with slog.
	OnLogError func(error)
}

// connState is what a connection owns: its admin session. A connection serves one request at a time, so
// the session needs no lock.
type connState struct{ session admin.Session }

// Service is the vault, the audit log and the control socket server around them.
type Service struct {
	cfg    Config
	vault  *vault.Vault
	mode   autounlock.Mode
	log    *auditlog.Logger
	server *daemon.Server

	unlockMu sync.Mutex
	mu       sync.Mutex // guards logKey
	logKey   *auditlog.Key
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

// Open prepares the service: it opens the vault with the credential the service manager handed over, if
// there is one, and stays locked otherwise. Nothing listens yet.
func Open(cfg Config) (*Service, error) {
	if cfg.ConfigDir == "" {
		cfg.ConfigDir = DefaultConfigDir
	}
	if cfg.StateDir == "" {
		cfg.StateDir = DefaultStateDir
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = control.DefaultSocketPath
	}
	s := &Service{cfg: cfg, log: auditlog.New(filepath.Join(cfg.StateDir, "logs"), auditlog.DefaultRetentionDays)}

	vaultDir := filepath.Join(cfg.StateDir, "vault")
	var st autounlock.Status
	if cfg.CredentialsDir != "" {
		s.vault, st = autounlock.Load(vaultDir, cfg.CredentialsDir)
	} else {
		s.vault, st = autounlock.LoadFromEnv(vaultDir)
	}
	s.mode = st.Unlock
	if s.vault == nil {
		s.vault = vault.New(vaultDir)
	}

	srv, err := daemon.New(daemon.Config{
		Logger:     s.log,
		OnLogError: cfg.OnLogError,
		Version:    cfg.Version,
		Resolver:   cfg.Resolver,
		Status:     s.fillStatus,
		Guard:      s.guard,
		NewState:   func(*daemon.Conn) any { return &connState{session: admin.NewSession(cfg.Clock)} },
		ConnClosed: func(c *daemon.Conn) { c.State.(*connState).session.End() },
	})
	if err != nil {
		return nil, err
	}
	s.server = srv
	if err := s.register(); err != nil {
		return nil, err
	}
	if state, _ := s.vault.State(); state == vault.StateUnlocked {
		if err := s.startSigning(); err != nil {
			s.vault.Forget()
			return nil, err
		}
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

// Serve serves the control socket until ctx ends. It then drops the vault's key and the log key.
func (s *Service) Serve(ctx context.Context) error {
	defer s.shutdown()
	if s.listener == nil {
		return errors.New("the service is not listening")
	}
	return s.server.Serve(ctx, s.listener)
}

func (s *Service) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vault.Forget()
	s.logKey.Clear()
	s.logKey = nil
}

// startSigning has the audit log sign its entries with the key derived from the unlocked vault.
func (s *Service) startSigning() error {
	key, err := s.vault.LogKey()
	if err != nil {
		return fmt.Errorf("cannot derive the audit log key: %w", err)
	}
	s.mu.Lock()
	old := s.logKey
	s.logKey = key
	s.mu.Unlock()
	s.server.SetLogger(s.log.WithKey(key))
	old.Clear()
	return nil
}

// adminTimeout reads the admin timeout from sysmin.yaml at the moment it is needed, so a changed
// configuration applies to the next session. A missing, unreadable or implausible file gives the default.
func (s *Service) adminTimeout() time.Duration {
	data, err := os.ReadFile(filepath.Join(s.cfg.ConfigDir, config.SettingsFile))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("cannot read the settings; using the default admin timeout")
		}
		return config.DefaultAdminTimeout
	}
	settings, err := config.DecodeSettings(data)
	if err != nil {
		slog.Warn("cannot decode the settings; using the default admin timeout")
		return config.DefaultAdminTimeout
	}
	d := settings.EffectiveAdminTimeout()
	if d < minAdminTimeout || d > maxAdminTimeout {
		return config.DefaultAdminTimeout
	}
	return d
}

func (s *Service) vaultState() string {
	st, err := s.vault.State()
	if err != nil {
		return control.VaultLocked
	}
	return string(st)
}

func (s *Service) fillStatus(r *control.StatusResult) {
	r.Vault, r.Unlock, r.Warning = s.vaultState(), string(s.mode), s.mode.Warning()
	r.Socket = s.cfg.SocketPath
}

// guard admits a changing request only for an unlocked vault and a connection with an active admin
// session, which each such request uses up one step of.
func (s *Service) guard(_ context.Context, c *daemon.Conn, _ string) error {
	if s.vaultState() != control.VaultUnlocked {
		return &control.Error{Code: control.CodeLocked}
	}
	if _, ok := c.State.(*connState).session.Authorize(s.adminTimeout()); !ok {
		return &control.Error{Code: control.CodeAdminRequired}
	}
	return nil
}

func (s *Service) register() error {
	for op, h := range map[string]daemon.Handler{
		control.OpVaultStatus: {Action: auditlog.OperationVaultStatus, Func: s.handleVaultStatus},
		control.OpUnlock:      {Action: auditlog.OperationVaultUnlock, Func: s.handleUnlock},
		control.OpSessionOpen: {Action: auditlog.OperationSessionOpen, Func: s.handleSessionOpen},
		control.OpSessionEnd:  {Action: auditlog.OperationSessionEnd, Func: s.handleSessionEnd},
	} {
		if err := s.server.Register(op, h); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) handleVaultStatus(context.Context, *daemon.Conn, json.RawMessage) (any, error) {
	return control.VaultStatusResult{Vault: s.vaultState(), Unlock: string(s.mode), Warning: s.mode.Warning()}, nil
}

// passphrase decodes the parameters and wipes their bytes. The decoded string cannot be wiped.
func passphrase(params json.RawMessage) (string, error) {
	defer clear(params)
	var p control.PassphraseParams
	if err := json.Unmarshal(params, &p); err != nil || p.Passphrase == "" {
		return "", &control.Error{Code: control.CodeBadRequest}
	}
	return p.Passphrase, nil
}

// vaultError maps a vault error to a fixed code; its text, which could name paths, never leaves.
func vaultError(err error) error {
	switch {
	case errors.Is(err, vault.ErrWrongPassphrase):
		return &control.Error{Code: control.CodeWrongPassphrase}
	case errors.Is(err, vault.ErrAbsent):
		return &control.Error{Code: control.CodeVaultAbsent}
	case errors.Is(err, vault.ErrEmptyPassphrase):
		return &control.Error{Code: control.CodeBadRequest}
	}
	return &control.Error{Code: control.CodeInternal}
}

func (s *Service) handleUnlock(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	pass, err := passphrase(params)
	if err != nil {
		return nil, err
	}
	// Unlocking is serialized so two requests do not both start signing; an unlocked vault stays as it is.
	s.unlockMu.Lock()
	defer s.unlockMu.Unlock()
	if s.vaultState() == control.VaultUnlocked {
		return nil, nil
	}
	if _, err := s.vault.Unlock(pass); err != nil {
		return nil, vaultError(err)
	}
	if err := s.startSigning(); err != nil {
		s.vault.Forget()
		return nil, &control.Error{Code: control.CodeInternal}
	}
	return nil, nil
}

func (s *Service) handleSessionOpen(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	pass, err := passphrase(params)
	if err != nil {
		return nil, err
	}
	sess := &c.State.(*connState).session
	if err := sess.Open(s.vault, pass, s.adminTimeout()); err != nil {
		return nil, vaultError(err)
	}
	return nil, nil
}

func (s *Service) handleSessionEnd(_ context.Context, c *daemon.Conn, _ json.RawMessage) (any, error) {
	c.State.(*connState).session.End()
	return nil, nil
}
