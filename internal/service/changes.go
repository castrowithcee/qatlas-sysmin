package service

import (
	"log/slog"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

// configChange is one change of the configuration for the shared write path. A handler that changes a
// project, a credential reference or the channels describes it this way and calls applyChange.
type configChange struct {
	// target is what the audit entry and the approval are named after: a project name or
	// approval.ChannelsName.
	target string
	// inScope picks the problems of the validation that belong to this change; findings in files the
	// change does not touch must not block it.
	inScope func(config.Problem) bool
	// prepare checks the request against the configuration as loaded and returns the configuration as it
	// will be after the change, which the path validates, and the function that writes it. It returns
	// nil for after when nothing needs validating. Its errors are *control.Error.
	prepare func(m *config.Model, revs config.Revisions) (after *config.Model, save func() (config.Revision, error), err error)
	// approve approves exactly the file save wrote, given the revision save returned.
	approve func(written config.Revision) error
}

// applyChange is the write path of every configuration change that comes over the socket. These requests
// are not guarded: the CLI never has an admin session, and its changes must still be stored. The path
// instead refuses a locked vault, validates before it writes, writes against the revision it loaded, and
// then decides whether the change is approved:
//
//   - A connection with an admin session (or the single change a passphrase covers while the admin timeout
//     is 0) is used up like the guard does and approves what was written.
//   - Any other change stays an open approval.
//
// The result's state says which. Every call leaves one audit entry for ch.target with its outcome.
func (s *Service) applyChange(c *daemon.Conn, ch configChange) (res control.ChangeResult, err error) {
	defer func() { c.Audit(ch.target, codeOfErr(err)) }()
	if s.vaultState() != control.VaultUnlocked {
		return res, &control.Error{Code: control.CodeLocked}
	}
	model, revs, err := s.store.Load()
	if err != nil {
		slog.Warn("cannot load the configuration for a change", "error", err.Error())
		return res, &control.Error{Code: control.CodeInternal}
	}
	after, save, err := ch.prepare(model, revs)
	if err != nil {
		return res, err
	}
	if after != nil {
		infos, err := s.vault.List()
		if err != nil {
			return res, approvalError(err)
		}
		names := make([]string, len(infos))
		for i, in := range infos {
			names[i] = in.Name
		}
		var found []config.Problem
		for _, p := range config.Validate(after, names) {
			if ch.inScope(p) {
				found = append(found, p)
			}
		}
		if len(found) > 0 {
			return res, &control.Error{Code: control.CodeInvalid, Problems: problemsOf(found)}
		}
	}
	rev, err := save()
	if err != nil {
		return res, approvalError(err)
	}
	res = control.ChangeResult{Project: ch.target, Revision: string(rev), State: control.StatePending}
	if s.adminCovers(c) {
		// The file is stored either way; a failed approval leaves it open, which the state then says.
		if err := ch.approve(rev); err != nil {
			slog.Warn("a change made in an admin session could not be approved", "error", err.Error())
		} else if ch.target == approval.ChannelsName {
			s.refreshNotifier()
		}
	}
	res.State = s.stateOf(ch.target)
	return res, nil
}

// stateOf reports whether the target has an open approval. What cannot be determined counts as open.
func (s *Service) stateOf(target string) string {
	items, err := s.approvals.Open()
	if err != nil {
		return control.StatePending
	}
	for _, it := range items {
		if it.Project == target {
			return control.StatePending
		}
	}
	return control.StateApproved
}

// problemsOf converts findings of the validator into the wire form, which drops the file they belong to.
func problemsOf(in []config.Problem) []control.Problem {
	out := make([]control.Problem, len(in))
	for i, p := range in {
		out[i] = control.Problem{Field: p.Field, Code: p.Code, Message: p.Message, Hint: p.Hint}
	}
	return out
}
