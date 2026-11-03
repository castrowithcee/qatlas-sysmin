package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

func (s *Service) registerVault() error {
	for op, h := range map[string]daemon.Handler{
		control.OpVaultCreate:          {Action: auditlog.OperationVaultCreate, Func: s.handleVaultCreate},
		control.OpVaultPassphrase:      {Action: auditlog.OperationVaultRekey, Mutating: true, Func: s.handleVaultPassphrase},
		control.OpSettingsShow:         {Action: auditlog.OperationConfigRead, Func: s.handleSettingsShow},
		control.OpSettingsAdminTimeout: {Action: auditlog.OperationConfigApply, Mutating: true, Func: s.handleAdminTimeout},
	} {
		if err := s.server.Register(op, h); err != nil {
			return err
		}
	}
	return nil
}

// handleVaultCreate is the one change without an admin session: while no vault exists there is no passphrase
// to prove. It is serialized with unlocking, and vault.Create itself refuses to replace an existing vault.
// Afterwards the service is in the state an unlock leaves it in.
func (s *Service) handleVaultCreate(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	pass, err := passphrase(params)
	if err != nil {
		return nil, err
	}
	s.unlockMu.Lock()
	defer s.unlockMu.Unlock()
	state, err := s.vault.State()
	if err != nil {
		return nil, internalError("cannot read the vault state", err)
	}
	if state != vault.StateAbsent {
		return nil, &control.Error{Code: control.CodeExists}
	}
	if err := s.vault.Create(pass); err != nil {
		if errors.Is(err, vault.ErrExists) || errors.Is(err, vault.ErrEmptyPassphrase) {
			return nil, vaultCreateError(err)
		}
		return nil, internalError("cannot create the vault", err)
	}
	if err := s.startSigning(); err != nil {
		s.vault.Forget()
		return nil, &control.Error{Code: control.CodeInternal}
	}
	return nil, nil
}

func vaultCreateError(err error) error {
	if errors.Is(err, vault.ErrExists) {
		return &control.Error{Code: control.CodeExists}
	}
	return &control.Error{Code: control.CodeBadRequest}
}

// handleVaultPassphrase runs behind the guard: the vault is unlocked and the admin session covered this
// change. The old passphrase is verified again by the vault.
func (s *Service) handleVaultPassphrase(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	defer clear(params)
	var p control.PassphraseChangeParams
	if err := json.Unmarshal(params, &p); err != nil || p.Old == "" || p.New == "" {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	if err := s.vault.ChangePassphrase(p.Old, p.New); err != nil {
		return nil, vaultError(err)
	}
	return nil, nil
}

func (s *Service) handleSettingsShow(context.Context, *daemon.Conn, json.RawMessage) (any, error) {
	st, err := config.ReadSettings(s.cfg.ConfigDir)
	if err != nil {
		return nil, internalError("cannot load the configuration", err)
	}
	return control.SettingsResult{
		AdminTimeout: s.adminTimeout().String(), Explicit: st.Vault.AdminTimeout != nil,
	}, nil
}

// handleAdminTimeout writes vault.admin_timeout and leaves every other setting as it is.
func (s *Service) handleAdminTimeout(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	var p control.AdminTimeoutParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	d, err := time.ParseDuration(p.AdminTimeout)
	if err != nil {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	if found := config.ValidateAdminTimeout(d); len(found) > 0 {
		return nil, &control.Error{Code: control.CodeInvalid, Problems: problemsOf(found)}
	}
	if err := s.store.SetAdminTimeout(config.Duration(d)); err != nil {
		return nil, settingsError(err)
	}
	return control.SettingsResult{AdminTimeout: d.String(), Explicit: true}, nil
}

// settingsError maps a failed settings write to a fixed code; its text, which could name paths, stays in
// the service's own log.
func settingsError(err error) error {
	switch {
	case errors.Is(err, vault.ErrLocked), errors.Is(err, vault.ErrAbsent):
		return &control.Error{Code: control.CodeLocked}
	}
	return internalError("cannot save the settings", err)
}

func problemsOf(in []config.Problem) []control.Problem {
	out := make([]control.Problem, len(in))
	for i, p := range in {
		out[i] = control.Problem{Field: p.Field, Code: p.Code, Message: p.Message, Hint: p.Hint}
	}
	return out
}

func internalError(what string, err error) error {
	slog.Warn(what, "error", err.Error())
	return &control.Error{Code: control.CodeInternal}
}
