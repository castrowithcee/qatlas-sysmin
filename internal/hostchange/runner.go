package hostchange

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AuditEntry is what the audit log receives about a change: everything of the journal entry except the
// previous state and the arguments.
type AuditEntry struct {
	ID         string    `json:"id"`
	Time       time.Time `json:"time"`
	Action     string    `json:"action"`
	Sentence   string    `json:"sentence"`
	Commands   []string  `json:"commands"`
	Result     string    `json:"result"`
	SudoUID    int       `json:"sudo_uid"`
	Reversible bool      `json:"reversible"`
	UndoOf     string    `json:"undo_of,omitempty"`
}

// Recorder hands a finished change to the audit log. A failure never undoes the change.
type Recorder func(AuditEntry) error

// Result is the report of an apply or an undo.
type Result struct {
	Entry Entry
	// Warning is set when the change stands but the journal or the audit log could not be completed.
	Warning string
}

// Runner applies actions and undoes journaled changes.
type Runner struct {
	Registry *Registry
	Journal  *Journal
	Exec     Executor
	Now      func() time.Time
	// SudoUID is the human behind the sudo call, 0 when unknown.
	SudoUID int
	Record  Recorder
}

// Apply runs an action. An error means nothing was changed; a change that failed or was taken back is a
// Result whose Entry.Result says so.
func (r *Runner) Apply(ctx context.Context, id string, args []string, secret []byte) (Result, error) {
	def, ok := r.Registry.Lookup(id)
	if !ok {
		return Result{}, fmt.Errorf("unknown action %q", id)
	}
	plan, err := def.Plan(args, secret)
	if err != nil {
		return Result{}, err
	}
	reversible := plan.NotReversible == ""
	if reversible && def.Inverse == nil {
		return Result{}, fmt.Errorf("action %s declares neither an inverse nor a reason", id)
	}
	return r.run(ctx, job{
		action: id, args: args, plan: plan, secret: secret, reversible: reversible,
		restore: func(prev json.RawMessage) (Plan, error) {
			if !reversible {
				return Plan{}, fmt.Errorf("not reversible: %s", plan.NotReversible)
			}
			return def.Inverse(prev)
		},
	})
}

// Undo runs the inverse of a reversible, applied entry.
func (r *Runner) Undo(ctx context.Context, journalID string) (Result, error) {
	unlock, err := r.Journal.Lock()
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	orig, err := r.Journal.Get(journalID)
	if err != nil {
		return Result{}, err
	}
	switch {
	case orig.Result != ResultApplied:
		return Result{}, fmt.Errorf("change %s has the result %q and cannot be undone", orig.ID, orig.Result)
	case !orig.Reversible:
		return Result{}, fmt.Errorf("change %s cannot be undone: %s", orig.ID, orig.NotReversible)
	case orig.UndoneBy != "":
		return Result{}, fmt.Errorf("change %s was already undone", orig.ID)
	}
	def, ok := r.Registry.Lookup(orig.Action)
	if !ok || def.Inverse == nil {
		return Result{}, fmt.Errorf("action %q is not known to this build", orig.Action)
	}
	plan, err := def.Inverse(orig.Previous)
	if err != nil {
		return Result{}, err
	}
	plan.NotReversible = "an undo is not undone"
	res, err := r.runLocked(ctx, job{
		action: orig.Action, args: orig.Args, plan: plan, undoOf: orig.ID,
		restore: func(json.RawMessage) (Plan, error) {
			// The forward plan again; a secret input is not kept, so such a change cannot be redone here.
			if def.WantsSecret {
				return Plan{}, errors.New("the change needs a secret and cannot be redone from the journal")
			}
			return def.Plan(orig.Args, nil)
		},
	})
	if err != nil || res.Entry.Result != ResultApplied {
		return res, err
	}
	orig.Result, orig.UndoneBy = ResultUndone, res.Entry.ID
	if err := r.Journal.Write(orig); err != nil {
		res.Warning = joinWarning(res.Warning, "the original journal entry could not be marked undone: "+err.Error())
	}
	return res, nil
}

type job struct {
	action     string
	args       []string
	plan       Plan
	secret     []byte
	reversible bool
	undoOf     string
	// restore builds the plan that takes the change back, from the captured previous state.
	restore func(previous json.RawMessage) (Plan, error)
}

func (r *Runner) run(ctx context.Context, j job) (Result, error) {
	unlock, err := r.Journal.Lock()
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	return r.runLocked(ctx, j)
}

func (r *Runner) runLocked(ctx context.Context, j job) (Result, error) {
	ctx = withActor(ctx, r.SudoUID)
	for _, c := range j.plan.Commands {
		if err := validateCommand(c); err != nil {
			return Result{}, err
		}
	}
	scrub := scrubber(j.plan.Commands, j.plan.Redact, j.secret)
	now := r.Now().UTC()
	id, err := newID(now)
	if err != nil {
		return Result{}, err
	}
	var prev json.RawMessage
	if j.plan.Capture != nil {
		if prev, err = j.plan.Capture(ctx); err != nil {
			if ref, ok := isRefusal(err); ok {
				return Result{}, &RefusedError{Msg: scrub(ref.Msg)}
			}
			return Result{}, fmt.Errorf("record the previous state: %s", scrub(err.Error()))
		}
	}
	e := Entry{
		ID: id, Time: now, Action: j.action, Args: j.args, Sentence: j.plan.Sentence,
		Commands: displays(j.plan.Commands), Result: ResultPending, SudoUID: r.SudoUID,
		Reversible: j.reversible && j.undoOf == "", NotReversible: j.plan.NotReversible, Previous: prev,
		Check: CheckNone, UndoOf: j.undoOf,
	}
	// The previous state is on disk before the first command runs; if this fails, nothing has changed.
	if err := r.Journal.Write(e); err != nil {
		return Result{}, fmt.Errorf("write the journal: %w", err)
	}

	problem, failed := r.runCommands(ctx, j.plan.Commands, scrub)
	if problem == "" && j.plan.Check != nil {
		if cerr := j.plan.Check(ctx); cerr != nil {
			e.Check, e.CheckError = CheckFailed, scrub(cerr.Error())
			problem = "the check failed: " + e.CheckError
		} else {
			e.Check = CheckPassed
		}
	}
	e.Result = ResultApplied
	switch {
	case problem != "" && failed == 0:
		// No step of this plan stood, so there is nothing of this run to take back. Restoring anyway could
		// act on a state this run did not make, such as an account of the same name that appeared since
		// the previous state was recorded.
		e.Error = problem + "; nothing was taken back because no step of this change completed"
		e.Result = ResultFailed
	case problem != "":
		e.Error = problem
		e.Result = ResultFailed
		if rerr := r.restore(ctx, j, prev, scrub); rerr != "" {
			e.Error += "; restore failed: " + rerr
		} else {
			e.Restored, e.Result = true, ResultRestored
		}
	}

	res := Result{Entry: e}
	if err := r.Journal.Write(e); err != nil {
		res.Warning = joinWarning(res.Warning, "the journal entry could not be completed: "+err.Error())
	}
	if err := r.Journal.Prune(KeepEntries); err != nil {
		res.Warning = joinWarning(res.Warning, "the journal could not be pruned: "+err.Error())
	}
	if r.Record != nil {
		audit := AuditEntry{
			ID: e.ID, Time: e.Time, Action: e.Action, Sentence: e.Sentence, Commands: e.Commands,
			Result: e.Result, SudoUID: e.SudoUID, Reversible: e.Reversible, UndoOf: e.UndoOf,
		}
		if err := r.Record(audit); err != nil {
			res.Warning = joinWarning(res.Warning, "the audit entry is missing: "+scrub(err.Error()))
		}
	}
	return res, nil
}

// restore returns an empty string when the previous state is back, otherwise the reason it is not.
func (r *Runner) restore(ctx context.Context, j job, prev json.RawMessage, scrub func(string) string) string {
	plan, err := j.restore(prev)
	if err != nil {
		return scrub(err.Error())
	}
	outer, inner := scrub, scrubber(plan.Commands, plan.Redact, nil)
	scrub = func(s string) string { return inner(outer(s)) }
	for _, c := range plan.Commands {
		if err := validateCommand(c); err != nil {
			return err.Error()
		}
	}
	if problem, _ := r.runCommands(ctx, plan.Commands, scrub); problem != "" {
		return problem
	}
	if plan.Check != nil {
		if err := plan.Check(ctx); err != nil {
			return "the check after the restore failed: " + scrub(err.Error())
		}
	}
	return ""
}

// runCommands runs the commands in order and stops at the first failure; it returns the problem and the index
// of the failed command, or "" and -1.
func (r *Runner) runCommands(ctx context.Context, cmds []Command, scrub func(string) string) (string, int) {
	for i, c := range cmds {
		out, err := r.Exec.Run(ctx, c)
		if err != nil {
			text := fmt.Sprintf("command %d (%s) failed: %v", i+1, c.Display(), err)
			if tail := strings.TrimSpace(string(out)); tail != "" {
				if len(tail) > 400 {
					tail = tail[:400] + "..."
				}
				text += ": " + tail
			}
			return scrub(text), i
		}
	}
	return "", -1
}

func displays(cmds []Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Display()
	}
	return out
}

// scrubber removes every secret input from text, including the secret handed to the plan and the values the
// plan asks to redact.
func scrubber(cmds []Command, redact [][]byte, extra []byte) func(string) string {
	var secrets [][]byte
	if len(extra) > 0 {
		secrets = append(secrets, extra)
	}
	for _, v := range redact {
		if len(v) > 0 {
			secrets = append(secrets, v)
		}
	}
	for _, c := range cmds {
		if len(c.Secret) > 0 {
			secrets = append(secrets, c.Secret)
		}
	}
	return func(s string) string {
		for _, sec := range secrets {
			s = string(bytes.ReplaceAll([]byte(s), sec, []byte(DefaultSecretLabel)))
		}
		return s
	}
}

func joinWarning(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
