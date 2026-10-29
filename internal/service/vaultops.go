package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	s.refreshNotifier()
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

// loadSettingsModel loads the configuration like loadModel, except that a configuration directory without
// sysmin.yaml, as on a fresh install, counts as empty settings with an absent file.
func (s *Service) loadSettingsModel() (*config.Model, config.Revisions, error) {
	_, err := os.Stat(filepath.Join(s.cfg.ConfigDir, config.SettingsFile))
	if errors.Is(err, os.ErrNotExist) {
		return &config.Model{}, config.Revisions{}, nil
	}
	return s.loadModel()
}

func (s *Service) handleSettingsShow(context.Context, *daemon.Conn, json.RawMessage) (any, error) {
	m, _, err := s.loadSettingsModel()
	if err != nil {
		return nil, err
	}
	return control.SettingsResult{
		AdminTimeout: s.adminTimeout().String(), Explicit: m.Settings.Vault.AdminTimeout != nil,
	}, nil
}

// handleAdminTimeout writes vault.admin_timeout and leaves every other setting as it is. Only problems of
// that field refuse the change; findings elsewhere in the settings are not its business.
func (s *Service) handleAdminTimeout(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	var p control.AdminTimeoutParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	d, err := time.ParseDuration(p.AdminTimeout)
	if err != nil {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	m, revs, err := s.loadSettingsModel()
	if err != nil {
		return nil, err
	}
	st := m.Settings
	if st.Schema == 0 {
		st.Schema = config.CurrentSchema
	}
	v := config.Duration(d)
	st.Vault.AdminTimeout = &v
	after := *m
	after.Settings = st
	var found []config.Problem
	for _, pr := range config.Validate(&after, nil) {
		if pr.File == config.SettingsPath() && pr.Field == "vault.admin_timeout" {
			found = append(found, pr)
		}
	}
	if len(found) > 0 {
		return nil, &control.Error{Code: control.CodeInvalid, Problems: problemsOf(found)}
	}
	if _, err := s.store.SaveSettings(st, revs[config.SettingsPath()]); err != nil {
		return nil, approvalError(err)
	}
	return control.SettingsResult{AdminTimeout: d.String(), Explicit: true}, nil
}
