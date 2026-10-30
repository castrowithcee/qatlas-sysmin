package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

func (s *Service) registerApprovals() error {
	for op, h := range map[string]daemon.Handler{
		control.OpApprovalsList:       {Action: auditlog.OperationApprovalList, Func: s.handleApprovalsList},
		control.OpApprovalsShow:       {Action: auditlog.OperationApprovalShow, Func: s.handleApprovalsShow},
		control.OpApprovalsApprove:    {Action: auditlog.OperationApprovalGrant, Mutating: true, Func: s.handleApprove},
		control.OpApprovalsApproveAll: {Action: auditlog.OperationApprovalGrant, Mutating: true, Func: s.handleApproveAll},
		control.OpApprovalsDiscard:    {Action: auditlog.OperationApprovalDeny, Mutating: true, Func: s.handleDiscard},
		control.OpApprovalsDiscardAll: {Action: auditlog.OperationApprovalDeny, Mutating: true, Func: s.handleDiscardAll},
	} {
		if err := s.server.Register(op, h); err != nil {
			return err
		}
	}
	return nil
}

// approvalCode maps an error of the approval manager to a fixed code; its text, which could name paths,
// stays in the service's own log.
func approvalCode(err error) string {
	switch {
	case errors.Is(err, vault.ErrLocked), errors.Is(err, vault.ErrAbsent):
		return control.CodeLocked
	case errors.Is(err, approval.ErrNothingOpen):
		return control.CodeNothingOpen
	case errors.Is(err, approval.ErrChanged), errors.Is(err, config.ErrConflict):
		return control.CodeChanged
	case errors.Is(err, approval.ErrIntegrity), errors.Is(err, approval.ErrNotApproved):
		return control.CodeCannotDiscard
	}
	slog.Warn("an approval operation failed", "error", err.Error())
	return control.CodeInternal
}

func approvalError(err error) error { return &control.Error{Code: approvalCode(err)} }

// projectParams decodes the parameters of a request about one project or about the channels.
func projectParams(params json.RawMessage) (control.ApprovalParams, error) {
	var p control.ApprovalParams
	if err := json.Unmarshal(params, &p); err != nil || !(config.ValidName(p.Project) || p.Project == approval.ChannelsName) {
		return p, &control.Error{Code: control.CodeBadRequest}
	}
	return p, nil
}

func summaryOf(it approval.Item) control.ApprovalSummary {
	return control.ApprovalSummary{
		Project: it.Project, Kind: string(it.Kind), Fingerprint: it.Fingerprint, Changes: len(it.Changes),
		FreeCommands: it.FreeCommands(), Unverified: it.Unverified,
	}
}

// openItems lists the open approvals. A locked vault is answered before the configuration is read, since
// without it nothing can be compared.
func (s *Service) openItems() ([]approval.Item, error) {
	if s.vaultState() != control.VaultUnlocked {
		return nil, &control.Error{Code: control.CodeLocked}
	}
	items, err := s.approvals.Open()
	if err != nil {
		return nil, approvalError(err)
	}
	return items, nil
}

// openItem returns the open approval of a project, or control.CodeNothingOpen.
func (s *Service) openItem(name string) (approval.Item, error) {
	items, err := s.openItems()
	if err != nil {
		return approval.Item{}, err
	}
	for _, it := range items {
		if it.Project == name {
			return it, nil
		}
	}
	return approval.Item{}, &control.Error{Code: control.CodeNothingOpen}
}

func (s *Service) handleApprovalsList(context.Context, *daemon.Conn, json.RawMessage) (any, error) {
	items, err := s.openItems()
	if err != nil {
		return nil, err
	}
	out := control.ApprovalsListResult{Approvals: make([]control.ApprovalSummary, 0, len(items))}
	for _, it := range items {
		out.Approvals = append(out.Approvals, summaryOf(it))
	}
	return out, nil
}

func (s *Service) handleApprovalsShow(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	p, err := projectParams(params)
	if err != nil {
		return nil, err
	}
	it, err := s.openItem(p.Project)
	if err != nil {
		return nil, err
	}
	out := control.ApprovalDetail{ApprovalSummary: summaryOf(it), Details: make([]control.ApprovalChange, 0, len(it.Changes))}
	for _, c := range it.Changes {
		out.Details = append(out.Details, control.ApprovalChange{Path: c.Path, Old: c.Old, New: c.New, Free: c.Free})
	}
	return out, nil
}

// audited records the outcome for one project and passes the error on.
func audited(c *daemon.Conn, project string, err error) error {
	c.Audit(project, codeOfErr(err))
	return err
}

func (s *Service) handleApprove(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	p, err := projectParams(params)
	if err != nil {
		return nil, err
	}
	err = s.approve(p)
	if err == nil && p.Project == approval.ChannelsName {
		s.refreshNotifier()
	}
	return nil, audited(c, p.Project, err)
}

// approve approves the state p names. The manager binds it under its lock: to the fingerprint, or, when
// that is empty, to a project whose file is gone, so a file written meanwhile is refused as changed.
func (s *Service) approve(p control.ApprovalParams) error {
	if p.Fingerprint != "" {
		if paths := s.unwritablePaths(p.Project); len(paths) > 0 {
			return &control.Error{Code: control.CodeNotWritable, Paths: paths}
		}
	}
	if err := s.approvals.Approve(p.Project, p.Fingerprint); err != nil {
		return approvalError(err)
	}
	return nil
}

// unwritablePaths returns what the deploy of the named project writes and the service may not write, from
// the configuration on disk. A project that is not in it (deleted, or the channels) has none.
func (s *Service) unwritablePaths(name string) []string {
	m, _, err := s.store.Load()
	if err != nil {
		return nil
	}
	p, ok := m.Project(name)
	if !ok {
		return nil
	}
	return s.notWritable(*p)
}

func (s *Service) handleDiscard(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	p, err := projectParams(params)
	if err != nil {
		return nil, err
	}
	return nil, audited(c, p.Project, s.discard(p))
}

func (s *Service) discard(p control.ApprovalParams) error {
	it, err := s.openItem(p.Project)
	if err != nil {
		return err
	}
	if it.Fingerprint != p.Fingerprint {
		return &control.Error{Code: control.CodeChanged}
	}
	if it.Kind == approval.KindCredential {
		return &control.Error{Code: control.CodeCannotDiscard}
	}
	return s.discardShown(p)
}

// discardShown discards the state p names; the manager binds it under its lock as approve does.
func (s *Service) discardShown(p control.ApprovalParams) error {
	if err := s.approvals.Discard(p.Project, p.Fingerprint); err != nil {
		return approvalError(err)
	}
	return nil
}

func (s *Service) handleApproveAll(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	return s.each(c, params, false)
}

func (s *Service) handleDiscardAll(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	return s.each(c, params, true)
}

// shownParams decodes the approvals a client showed for an all-at-once request. Without them the request
// is refused, never served on what is open now.
func shownParams(params json.RawMessage) ([]control.ShownApproval, error) {
	var p control.ApprovalsAllParams
	if err := json.Unmarshal(params, &p); err != nil || p.Shown == nil {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	return p.Shown, nil
}

// sameApprovals reports whether items are exactly the shown approvals, by project, kind and fingerprint.
// Projects are unique in items, so equal lengths leave no room for a duplicate in shown.
func sameApprovals(shown []control.ShownApproval, items []approval.Item) bool {
	if len(shown) != len(items) {
		return false
	}
	have := make(map[control.ShownApproval]bool, len(shown))
	for _, a := range shown {
		have[a] = true
	}
	for _, it := range items {
		if !have[control.ShownApproval{Project: it.Project, Kind: string(it.Kind), Fingerprint: it.Fingerprint}] {
			return false
		}
	}
	return true
}

// each handles the shown approvals one by one, so each project leaves its own audit entry and a failure
// for one does not stop the others. Unless exactly the shown approvals are open, nothing is handled. The
// comparison alone does not bind: each approval or discard is checked again against its shown state under
// the manager's lock. Discarding leaves out an item that restoring the file cannot undo.
func (s *Service) each(c *daemon.Conn, params json.RawMessage, discard bool) (any, error) {
	shown, err := shownParams(params)
	if err != nil {
		return nil, err
	}
	items, err := s.openItems()
	if err != nil {
		return nil, err
	}
	if !sameApprovals(shown, items) {
		return nil, &control.Error{Code: control.CodeChanged}
	}
	out := control.ApprovalsAllResult{Done: []string{}}
	for _, it := range items {
		p := control.ApprovalParams{Project: it.Project, Fingerprint: it.Fingerprint}
		var err error
		if discard {
			if it.Kind == approval.KindCredential {
				continue
			}
			err = s.discardShown(p)
		} else {
			err = s.approve(p)
		}
		c.Audit(it.Project, codeOfErr(err))
		if err != nil {
			out.Failed = append(out.Failed, control.ApprovalFailure{Project: it.Project, Code: codeOfErr(err), Paths: pathsOfErr(err)})
			continue
		}
		out.Done = append(out.Done, it.Project)
	}
	if !discard && slices.Contains(out.Done, approval.ChannelsName) {
		s.refreshNotifier()
	}
	return out, nil
}

func codeOfErr(err error) string {
	if err == nil {
		return ""
	}
	return err.(*control.Error).Code
}

func pathsOfErr(err error) []string {
	if err == nil {
		return nil
	}
	return err.(*control.Error).Paths
}
