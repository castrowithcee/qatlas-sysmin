package service

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

// The error codes the audit entry of a host change that did not stand carries.
const (
	codeChangeFailed   = "change-failed"
	codeChangeRestored = "change-restored"
)

func (s *Service) registerHost() error {
	// Not Mutating: the sudo call is the proof, and only root may call it.
	return s.server.Register(control.OpHostRecord, daemon.Handler{
		Action: auditlog.OperationHostChange, RootOnly: true, Func: s.handleHostRecord,
	})
}

// handleHostRecord writes the audit entry of a change root made. Everything it takes is a fixed word or an
// identifier checked against its form; the entry names the human behind sudo as its client.
func (s *Service) handleHostRecord(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	var p control.HostRecordParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	host := auditlog.Host{ActionID: p.Action, JournalID: p.JournalID, UndoOf: p.UndoOf}
	if host.Validate() != nil {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	code := ""
	switch p.Result {
	case control.HostResultApplied:
	case control.HostResultFailed:
		code = codeChangeFailed
	case control.HostResultRestored:
		code = codeChangeRestored
	default:
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	c.AuditHost(p.SudoUID, host, code)
	return nil, nil
}
