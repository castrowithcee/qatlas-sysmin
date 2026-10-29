package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/notify"
)

// notifyTestTimeout bounds one test message, longer than a single delivery attempt.
const notifyTestTimeout = 20 * time.Second

func (s *Service) registerNotify() error {
	return s.server.Register(control.OpNotifyTest, daemon.Handler{Action: auditlog.OperationConfigRead, Func: s.handleNotifyTest})
}

// handleNotifyTest sends one test message to an approved channel. The delivery leaves its own audit entry;
// the answer carries only a fixed code, since a delivery error text comes from a remote peer.
func (s *Service) handleNotifyTest(ctx context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	var p control.NotifyTestParams
	if err := json.Unmarshal(params, &p); err != nil || !config.ValidName(p.Channel) {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	if s.vaultState() != control.VaultUnlocked {
		return nil, &control.Error{Code: control.CodeLocked}
	}
	ctx, cancel := context.WithTimeout(ctx, notifyTestTimeout)
	defer cancel()
	err := s.notifier.Send(ctx, p.Channel, notify.Event{Type: notify.EventTest})
	switch {
	case err == nil:
		return control.NotifyTestResult{Channel: p.Channel, Event: string(notify.EventTest)}, nil
	case errors.Is(err, notify.ErrUnknownChannel), errors.Is(err, approval.ErrNotApproved):
		return nil, &control.Error{Code: control.CodeNotFound}
	case errors.Is(err, notify.ErrNeedsSecret):
		return nil, &control.Error{Code: control.CodeLocked}
	}
	return nil, &control.Error{Code: control.CodeSendFailed}
}
