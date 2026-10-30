package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/notify"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/version"
)

// sourceTimeout bounds the reading of one source, so a slow one leaves time for the others.
const sourceTimeout = time.Minute

// Reasons a check was skipped.
const (
	SkipPaused = "paused"
	SkipPinned = "pinned"
	SkipLocked = "locked"
)

// Error codes of a run, in the state and the audit log.
const (
	codeRunLog       = "run-log-unavailable"
	codeExecutor     = "executor-unavailable"
	codeInvalidPlan  = "invalid-plan"
	codeStepFailed   = "step-failed"
	codeRolledBack   = "rolled-back"
	codeRollbackFail = "rollback-failed"
	codeRestore      = "restore-failed"
)

// Outcome is the result of a check.
type Outcome struct {
	// Skipped is SkipPaused, SkipPinned or SkipLocked when nothing was deployed for that reason.
	Skipped string
	// Run is the deploy that ran; nil when nothing was deployed.
	Run *RunRecord
}

// RunNow checks the sources of a project at once and deploys what its version rules allow without a human.
// It fails with ErrBusy while a run of the project is active.
func (e *Engine) RunNow(ctx context.Context, project string) (Outcome, error) {
	if err := e.acquire(project); err != nil {
		return Outcome{}, err
	}
	defer e.release(project)
	return e.check(ctx, project)
}

// found is what one source reported.
type found struct {
	name    string // empty for the git source
	img     *config.Image
	rule    config.VersionRule
	current string
	sel     version.Selection
	digest  string
	err     string
}

// display is the name of a source in messages.
func display(name string) string {
	if name == "" {
		return "git"
	}
	return name
}

// read determines the current version of each source and, with versions, its selection. With publicOnly,
// sources that need a credential are left out.
func (e *Engine) read(ctx context.Context, ap approval.Approved, publicOnly, versions bool) []found {
	p := ap.Project
	var out []found
	for i := range p.Images {
		img := &p.Images[i]
		if publicOnly && img.Credential != "" {
			continue
		}
		f := found{name: img.Name, img: img, rule: img.VersionRule}
		func() {
			sctx, cancel := context.WithTimeout(ctx, sourceTimeout)
			defer cancel()
			cur, err := e.cfg.Sources.CurrentImage(sctx, ap, *img)
			if err != nil || cur == "" {
				f.err = errText("the current version is not known", err)
				return
			}
			f.current = cur
			if !versions {
				return
			}
			r, err := e.cfg.Sources.CheckImage(sctx, ap, *img, cur)
			if err != nil {
				f.err = errText("the source could not be read", err)
				return
			}
			f.sel, f.digest = r.Selection, r.Digest
		}()
		out = append(out, f)
	}
	if g := p.Git; g != nil && !(publicOnly && g.Credential != "") {
		f := found{rule: g.VersionRule}
		func() {
			sctx, cancel := context.WithTimeout(ctx, sourceTimeout)
			defer cancel()
			cur, err := e.cfg.Sources.CurrentGit(sctx, ap)
			if err != nil || cur == "" {
				f.err = errText("the current version is not known", err)
				return
			}
			f.current = cur
			if !versions {
				return
			}
			r, err := e.cfg.Sources.CheckGit(sctx, ap, cur)
			if err != nil {
				f.err = errText("the source could not be read", err)
				return
			}
			f.sel = r.Selection
		}()
		out = append(out, f)
	}
	return out
}

func errText(prefix string, err error) string {
	if err == nil {
		return prefix
	}
	return prefix + ": " + err.Error()
}

// target is the version change of one source in a deploy.
type target struct {
	name     string
	from, to string
	digest   string // of to, if known
}

func (e *Engine) check(ctx context.Context, project string) (Outcome, error) {
	st, err := e.store.load(project)
	if err != nil {
		return Outcome{}, err
	}
	switch {
	case st.Paused:
		return Outcome{Skipped: SkipPaused}, nil
	case st.Pin != nil:
		return Outcome{Skipped: SkipPinned}, nil
	}
	unlocked := e.unlocked()
	ap, fresh, err := e.approvedState(project)
	if errors.Is(err, ErrLocked) {
		return Outcome{Skipped: SkipLocked}, nil
	}
	if err != nil {
		msg := err.Error()
		_, _ = e.store.update(project, func(s *State) { s.CheckError = msg })
		return Outcome{}, err
	}
	locked := !unlocked || !fresh

	sources := e.read(ctx, ap, locked, true)
	now := e.cfg.Clock.Now().UTC()
	var targets []target
	var report []notify.ImageVersion
	st, err = e.store.update(project, func(s *State) {
		s.LastCheck, s.CheckError = now, ""
		if locked {
			s.CheckError = "the vault is locked; only sources without a credential were read and nothing was deployed"
		}
		for _, f := range sources {
			src := s.source(f.name)
			src.Error = f.err
			if f.err != "" {
				continue
			}
			if src.Current != f.current {
				src.Current, src.Digest = f.current, ""
			}
			deploy := !locked && f.rule.Mode == config.ModeAuto && f.sel.Target != "" &&
				f.sel.Target != src.Failed && slices.Contains(f.sel.Available, f.sel.Target)
			if deploy {
				targets = append(targets, target{name: f.name, from: f.current, to: f.sel.Target, digest: f.digest})
				src.Available = after(f.sel.Available, f.sel.Target)
				continue
			}
			src.Available = f.sel.Available
			src.Reported = keep(src.Reported, src.Available)
			// While the vault is locked the notifier sends nothing, so nothing is marked as reported.
			if locked || len(src.Available) == 0 || allIn(src.Available, src.Reported) {
				continue
			}
			src.Reported = append([]string(nil), src.Available...)
			report = append(report, notify.ImageVersion{Image: display(f.name), Current: f.current,
				Target: src.Available[len(src.Available)-1]})
		}
	})
	if err != nil {
		return Outcome{}, err
	}
	if len(report) > 0 {
		e.notify(ap.Project, notify.Event{Type: notify.EventUpdateAvailable, Images: report, Time: now})
	}
	if locked {
		return Outcome{Skipped: SkipLocked}, nil
	}
	if len(targets) == 0 {
		return Outcome{}, nil
	}
	rec := e.deploy(ctx, ap, ActionAuto, targets)
	return Outcome{Run: &rec}, nil
}

// after returns the entries of list behind v.
func after(list []string, v string) []string {
	i := slices.Index(list, v)
	if i < 0 {
		return list
	}
	return append([]string(nil), list[i+1:]...)
}

// keep returns the entries of list that are also in in.
func keep(list, in []string) []string {
	var out []string
	for _, v := range list {
		if slices.Contains(in, v) {
			out = append(out, v)
		}
	}
	return out
}

func allIn(list, in []string) bool {
	for _, v := range list {
		if !slices.Contains(in, v) {
			return false
		}
	}
	return true
}

// runLog writes the engine's own lines into a run log, through the redactor of the executor.
type runLog struct {
	w   io.Writer
	ex  *execute.Executor
	now func() time.Time
}

func (l runLog) printf(format string, args ...any) {
	if l.w == nil {
		return
	}
	line := fmt.Sprintf("%s %s\n", l.now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
	if l.ex != nil && l.ex.Redactor != nil {
		line = l.ex.Redactor.Apply(line)
	}
	_, _ = io.WriteString(l.w, line)
}

// prepare creates the run log and the executor of a run. The caller closes the log file.
func (e *Engine) prepare(ap approval.Approved, rec *RunRecord) (io.WriteCloser, *execute.Executor, string, error) {
	f, id, err := e.store.createRunLog(ap.Name, rec.Started)
	if err != nil {
		return nil, nil, codeRunLog, err
	}
	rec.ID, rec.Log = id, id+".log"
	ex, err := e.cfg.Executor(ap)
	if err == nil && ex == nil {
		err = errors.New("no executor")
	}
	if err != nil {
		return f, nil, codeExecutor, fmt.Errorf("the executor is not available: %w", err)
	}
	ex.Log, ex.BackupRoot, ex.Now = f, e.cfg.BackupRoot, e.cfg.Clock.Now
	return f, ex, "", nil
}

// deploy runs the plan for targets and, when a step after the backup fails, rolls every target back. The
// plan runs to its end even when ctx ends, so a deploy is never left half done; every step has its own
// time limit.
func (e *Engine) deploy(ctx context.Context, ap approval.Approved, action string, targets []target) RunRecord {
	project := ap.Name
	clock := e.cfg.Clock
	rec := RunRecord{Action: action, Started: clock.Now().UTC()}
	for _, t := range targets {
		rec.Changes = append(rec.Changes, Change{Name: t.name, From: t.from, To: t.to})
	}
	ctx = context.WithoutCancel(ctx)

	fail := func(code string, err error) RunRecord {
		rec.Result, rec.ErrorCode, rec.Error = ResultFailed, code, err.Error()
		return rec
	}
	rec = func() RunRecord {
		logFile, ex, code, err := e.prepare(ap, &rec)
		if logFile != nil {
			defer logFile.Close()
		}
		if err != nil {
			return fail(code, err)
		}
		log := runLog{w: logFile, ex: ex, now: clock.Now}
		model := &config.Model{Projects: []config.Project{ap.Runnable()}}
		forward, backward := make([]config.Target, 0, len(targets)), make([]config.Target, 0, len(targets))
		for _, t := range targets {
			log.printf("%s %s: %s -> %s", action, display(t.name), t.from, t.to)
			forward = append(forward, config.Target{Name: t.name, Version: t.to, PreviousVersion: t.from})
			backward = append(backward, config.Target{Name: t.name, Version: t.from, PreviousVersion: t.to})
		}
		plan, err := model.Resolve(project, forward)
		if err != nil {
			log.printf("the plan cannot be resolved: %v", err)
			return fail(codeInvalidPlan, err)
		}
		rep, err := ex.Execute(ctx, plan)
		rec.Backup = backupName(rep)
		if err == nil {
			rec.Result = ResultSucceeded
			log.printf("result=succeeded")
			return rec
		}
		rec.Result, rec.Error = ResultFailed, err.Error()
		var se *execute.StepError
		failed := -1
		if errors.As(err, &se) {
			failed = se.Index
			rec.FailedStep = fmt.Sprintf("step %d (%s)", se.Index+1, se.Type)
		}
		rec.ErrorCode = codeStepFailed
		if !shouldRollBack(plan, failed) {
			log.printf("result=failed; no step changed anything that could be undone, so nothing is rolled back")
			return rec
		}

		log.printf("rollback of every image of the run to its previous version")
		rec.Result, rec.ErrorCode = ResultRolledBack, codeRolledBack
		rb, err := model.Resolve(project, backward)
		if err == nil {
			rb.Steps = slices.DeleteFunc(rb.Steps, func(s config.ResolvedStep) bool { return s.Type == config.StepBackup })
			_, err = ex.Execute(ctx, rb)
		}
		if err != nil {
			rec.Result, rec.ErrorCode = ResultRollbackFailed, codeRollbackFail
			rec.Error += "; rollback: " + err.Error()
			log.printf("result=rollback-failed; the project is paused")
			return rec
		}
		log.printf("result=rolled-back")
		return rec
	}()
	rec.Duration = clock.Now().Sub(rec.Started)
	e.finish(ap, rec, targets)
	return rec
}

// shouldRollBack reports whether a run that failed at plan step failed has changes to undo: an earlier
// step changed something, or the failing step itself can leave partial changes. A failure in or before
// the backup never rolls back.
func shouldRollBack(plan config.Plan, failed int) bool {
	if failed < 0 || failed >= len(plan.Steps) || failed <= firstBackup(plan) {
		return false
	}
	if plan.Steps[failed].Effect() == config.EffectPartial {
		return true
	}
	for _, s := range plan.Steps[:failed] {
		if s.Effect() != config.EffectNone {
			return true
		}
	}
	return false
}

func firstBackup(plan config.Plan) int {
	for i, s := range plan.Steps {
		if s.Type == config.StepBackup {
			return i
		}
	}
	return -1
}

func backupName(rep execute.Report) string {
	for _, s := range rep.Steps {
		if s.Type == config.StepBackup && s.Status == execute.StatusOK && s.BackupDir != "" {
			return baseName(s.BackupDir)
		}
	}
	return ""
}

// finish stores, reports and audits the result of a deploy.
func (e *Engine) finish(ap approval.Approved, rec RunRecord, targets []target) {
	project := ap.Name
	_, err := e.store.update(project, func(s *State) {
		s.addRun(rec)
		for _, t := range targets {
			src := s.source(t.name)
			switch rec.Result {
			case ResultSucceeded:
				if src.Current == t.from {
					src.PreviousDigest = src.Digest
				} else {
					src.PreviousDigest = ""
				}
				src.Previous, src.Current, src.Digest, src.Failed = t.from, t.to, t.digest, ""
				src.Available = after(src.Available, t.to)
				src.Reported = keep(src.Reported, src.Available)
			case ResultRolledBack, ResultRollbackFailed:
				src.Failed = t.to
				if !slices.Contains(src.Available, t.to) {
					src.Available = append([]string{t.to}, src.Available...)
				}
			}
		}
		if rec.Result == ResultRollbackFailed {
			s.Paused = true
			s.Fault = &Fault{Time: rec.Started, Run: rec.ID, FailedStep: rec.FailedStep, Error: rec.Error}
		}
	})
	if err != nil {
		// The run already happened; a state that cannot be written is recorded beside it.
		rec.Error = joinText(rec.Error, err.Error())
	}

	ev := notify.Event{Project: project, FailedStep: rec.FailedStep, Error: rec.Error, Time: rec.Started}
	for _, t := range targets {
		iv := notify.ImageVersion{Image: display(t.name), Current: t.from, Target: t.to}
		if rec.Result == ResultSucceeded {
			iv = notify.ImageVersion{Image: display(t.name), Current: t.to, Previous: t.from}
		}
		ev.Images = append(ev.Images, iv)
	}
	switch rec.Result {
	case ResultSucceeded:
		ev.Type = notify.EventDeploySucceeded
	case ResultRolledBack:
		ev.Type = notify.EventDeployRolled
	case ResultRollbackFailed:
		ev.Type = notify.EventRollbackFailed
	default:
		ev.Type = notify.EventDeployFailed
	}
	e.notify(ap.Project, ev)

	switch rec.Result {
	case ResultRolledBack:
		e.audit(auditlog.OperationDeployRun, project, rec.ErrorCode, rec.Duration)
		e.audit(auditlog.OperationDeployRollback, project, "", rec.Duration)
	case ResultRollbackFailed:
		e.audit(auditlog.OperationDeployRun, project, codeStepFailed, rec.Duration)
		e.audit(auditlog.OperationDeployRollback, project, rec.ErrorCode, rec.Duration)
	default:
		e.audit(auditlog.OperationDeployRun, project, rec.ErrorCode, rec.Duration)
	}
}

func joinText(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
