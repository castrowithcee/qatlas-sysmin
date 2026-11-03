package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// maxLogActionFilter is the longest action filter. The filter is only checked for its shape, not against the
// known actions, so an action that older entries record and sysmin no longer writes finds them.
const maxLogActionFilter = 64

func (s *Service) registerLog() error {
	for op, h := range map[string]daemon.Handler{
		control.OpLogList:   {Action: auditlog.OperationConfigRead, Func: s.handleLogList},
		control.OpLogVerify: {Action: auditlog.OperationConfigRead, Func: s.handleLogVerify},
	} {
		if err := s.server.Register(op, h); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) logDir() string { return filepath.Join(s.cfg.StateDir, "logs") }

// logQuery validates the parameters of log.list; anything unknown or malformed is a bad request.
func logQuery(p control.LogListParams) (auditlog.Query, error) {
	bad := &control.Error{Code: control.CodeBadRequest}
	q := auditlog.Query{Action: p.Action, Result: p.Result, After: p.After, Limit: p.Limit}
	for _, t := range []struct {
		text string
		dst  *time.Time
	}{{p.From, &q.From}, {p.To, &q.To}} {
		if t.text == "" {
			continue
		}
		v, err := time.Parse(time.RFC3339, t.text)
		if err != nil {
			return q, bad
		}
		*t.dst = v
	}
	if !q.From.IsZero() && !q.To.IsZero() && q.From.After(q.To) {
		return q, bad
	}
	switch {
	case q.Limit == 0:
		q.Limit = control.LogDefaultLimit
	case q.Limit < 0 || q.Limit > control.LogMaxLimit:
		return q, bad
	}
	if len(q.Action) > maxLogActionFilter || !utf8.ValidString(q.Action) || strings.IndexFunc(q.Action, unicode.IsControl) >= 0 {
		return q, bad
	}
	if q.Result != "" && q.Result != control.LogResultSuccess && q.Result != control.LogResultFailure {
		return q, bad
	}
	return q, nil
}

// handleLogList returns the parsed fields of entries only, never a raw line, and at most control.LogMaxLimit
// of them, which stays far below the size limit of an answer.
func (s *Service) handleLogList(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	var p control.LogListParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &control.Error{Code: control.CodeBadRequest}
		}
	}
	q, err := logQuery(p)
	if err != nil {
		return nil, err
	}
	page, err := auditlog.Read(s.logDir(), q)
	if err != nil {
		if errors.Is(err, auditlog.ErrBadCursor) {
			return nil, &control.Error{Code: control.CodeBadRequest}
		}
		return nil, internalError("cannot read the action log", err)
	}
	out := control.LogListResult{Entries: make([]control.LogEntry, len(page.Entries)), Next: page.Next}
	for i, e := range page.Entries {
		out.Entries[i] = control.LogEntry{
			Seq: e.Seq, Time: e.Time, ClientUID: e.ClientUID, Action: e.Action,
			Result: e.Result, ErrorCode: e.ErrorCode, DurationMS: e.DurationMS, Signed: e.MAC != "",
		}
		if e.Host != nil {
			out.Entries[i].Host = &control.LogHost{ActionID: e.Host.ActionID, JournalID: e.Host.JournalID, UndoOf: e.Host.UndoOf}
		}
	}
	return out, nil
}

// handleLogVerify checks the check values only while the vault is unlocked, with a key of its own that it
// clears afterwards; otherwise the days report their signatures as not checked.
func (s *Service) handleLogVerify(context.Context, *daemon.Conn, json.RawMessage) (any, error) {
	var checker auditlog.Checker
	if s.vaultState() == control.VaultUnlocked {
		key, err := s.vault.LogKey()
		if err != nil && !errors.Is(err, vault.ErrLocked) {
			return nil, internalError("cannot derive the audit log key", err)
		}
		if err == nil {
			defer key.Clear()
			checker = key
		}
	}
	report, err := auditlog.Verify(s.logDir(), checker)
	if err != nil {
		return nil, internalError("cannot verify the action log", err)
	}
	out := control.LogVerifyResult{Intact: !report.Broken, Checked: report.Checked, Days: make([]control.LogDay, 0, len(report.Days))}
	for _, d := range report.Days {
		out.Days = append(out.Days, control.LogDay{
			Date: d.Date, Entries: d.Entries, Intact: d.Problem == "", Damaged: d.Damaged,
			Signatures: strings.ReplaceAll(d.SignatureState(report.Checked), " ", "-"),
		})
		if d.Problem == "" {
			continue
		}
		if len(out.Problems) == control.LogVerifyMaxProblems {
			out.MoreProblems = true
			continue
		}
		out.Problems = append(out.Problems, control.LogProblem{Date: d.Date, Seq: d.ProblemSeq, Line: d.ProblemLine, Code: d.ProblemCode})
	}
	return out, nil
}
