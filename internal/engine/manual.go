package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/notify"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/version"
)

// Update deploys the versions a human chose, keyed by image name (the git source has the empty name),
// together in one run with the same backup, check and rollback as an automatic run. The mode and the
// update cap of the version rule do not apply. Each version must be an available update of its source,
// which is never lower than the current version; under stepwise major steps it must not skip the highest
// version of the current major or a whole major. A paused or pinned project is refused.
//
// The engine does not check the admin session: the caller does before it calls Update.
func (e *Engine) Update(ctx context.Context, project string, versions map[string]string) (RunRecord, error) {
	if err := e.acquire(project); err != nil {
		return RunRecord{}, err
	}
	defer e.release(project)
	ap, err := e.ready(project, true)
	if err != nil {
		return RunRecord{}, err
	}
	if len(versions) == 0 {
		return RunRecord{}, fmt.Errorf("%w: no version was given", ErrVersion)
	}
	for name := range versions {
		if !hasSource(ap.Project, name) {
			return RunRecord{}, fmt.Errorf("source %q is not part of project %s", display(name), project)
		}
	}
	var targets []target
	for _, f := range e.read(ctx, ap, false, true) {
		want, ok := versions[f.name]
		if !ok {
			continue
		}
		if f.err != "" {
			return RunRecord{}, fmt.Errorf("%s: %s", display(f.name), f.err)
		}
		if !allowed(f.current, f.sel.Available, f.rule, want) {
			return RunRecord{}, fmt.Errorf("%w: %s %s -> %s", ErrVersion, display(f.name), f.current, want)
		}
		t := target{name: f.name, from: f.current, to: want}
		switch {
		case f.img == nil:
		case want == f.sel.Target:
			t.digest = f.digest
		default:
			if d, err := e.cfg.Sources.ImageDigest(ctx, ap, *f.img, want); err == nil {
				t.digest = d
			}
		}
		targets = append(targets, t)
	}
	return e.deploy(ctx, ap, ActionManual, targets), nil
}

// ready checks that a project may run a changing action now and returns its approved state.
func (e *Engine) ready(project string, deploys bool) (approval.Approved, error) {
	st, err := e.store.load(project)
	if err != nil {
		return approval.Approved{}, err
	}
	if deploys && st.Paused {
		return approval.Approved{}, ErrPaused
	}
	if deploys && st.Pin != nil {
		return approval.Approved{}, ErrPinned
	}
	if !e.unlocked() {
		return approval.Approved{}, ErrLocked
	}
	ap, fresh, err := e.approvedState(project)
	if err != nil {
		return approval.Approved{}, err
	}
	if !fresh {
		return approval.Approved{}, ErrLocked
	}
	return ap, nil
}

func hasSource(p config.Project, name string) bool {
	if name == "" {
		return p.Git != nil
	}
	return slices.ContainsFunc(p.Images, func(im config.Image) bool { return im.Name == name })
}

// allowed reports whether want is an available update of current. Under stepwise major steps the highest
// allowed version is the target the rule would choose without an update cap.
func allowed(current string, available []string, rule config.VersionRule, want string) bool {
	i := slices.Index(available, want)
	if i < 0 || want == current {
		return false
	}
	if rule.MajorSteps != config.MajorStep {
		return true
	}
	uncapped := rule
	uncapped.MaxUpdate = config.LevelMajor
	limit := slices.Index(available, version.Select(current, available, uncapped).Target)
	return limit >= 0 && i <= limit
}

// Backups lists the backups of a project by name, oldest first.
func (e *Engine) Backups(project string) ([]string, error) {
	if err := checkName(project); err != nil {
		return nil, err
	}
	dirs, err := execute.ListBackups(e.cfg.BackupRoot, project)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(dirs))
	for i, d := range dirs {
		names[i] = baseName(d)
	}
	return names, nil
}

func baseName(path string) string { return filepath.Base(path) }

// Restore restores the data of a project from one of its backups, named as Backups lists it, with the
// restore command of the approved backup step. Versions are left as they are. A paused or pinned project
// may be restored.
//
// The engine does not check the admin session: the caller does before it calls Restore.
func (e *Engine) Restore(ctx context.Context, project, backup string) (RunRecord, error) {
	if err := e.acquire(project); err != nil {
		return RunRecord{}, err
	}
	defer e.release(project)
	names, err := e.Backups(project)
	if err != nil {
		return RunRecord{}, err
	}
	if !slices.Contains(names, backup) {
		return RunRecord{}, fmt.Errorf("%w: %q", ErrBackup, backup)
	}
	ap, err := e.ready(project, false)
	if err != nil {
		return RunRecord{}, err
	}
	step, err := e.restoreStep(ctx, ap)
	if err != nil {
		return RunRecord{}, err
	}

	clock := e.cfg.Clock
	rec := RunRecord{Action: ActionRestore, Started: clock.Now().UTC(), Backup: backup}
	rec = func() RunRecord {
		logFile, ex, code, err := e.prepare(ap, &rec)
		if logFile != nil {
			defer logFile.Close()
		}
		if err != nil {
			rec.Result, rec.ErrorCode, rec.Error = ResultFailed, code, err.Error()
			return rec
		}
		log := runLog{w: logFile, ex: ex, now: clock.Now}
		log.printf("restore from backup %s", backup)
		dir := filepath.Join(e.cfg.BackupRoot, project, backup)
		if _, err := ex.Restore(context.WithoutCancel(ctx), project, step, dir); err != nil {
			rec.Result, rec.ErrorCode, rec.Error = ResultFailed, codeRestore, err.Error()
			log.printf("result=failed")
			return rec
		}
		rec.Result = ResultSucceeded
		log.printf("result=succeeded")
		return rec
	}()
	rec.Duration = clock.Now().Sub(rec.Started)
	if _, err := e.store.update(project, func(s *State) { s.addRun(rec) }); err != nil {
		rec.Error = joinText(rec.Error, err.Error())
	}
	ev := notify.Event{Type: notify.EventRestoreSucceeded, Project: project, Time: rec.Started}
	if rec.Result != ResultSucceeded {
		ev.Type, ev.Error = notify.EventRestoreFailed, rec.Error
	}
	e.notify(ap.Project, ev)
	e.audit(auditlog.OperationDeployRestore, project, rec.ErrorCode, rec.Duration)
	return rec, nil
}

// restoreStep resolves the approved steps for the versions the project is on and returns the first backup
// step that has a restore command.
func (e *Engine) restoreStep(ctx context.Context, ap approval.Approved) (config.ResolvedStep, error) {
	st, err := e.store.load(ap.Name)
	if err != nil {
		return config.ResolvedStep{}, err
	}
	var targets []config.Target
	for _, f := range e.read(ctx, ap, false, false) {
		cur := f.current
		if cur == "" {
			cur = st.source(f.name).Current
		}
		if cur == "" {
			return config.ResolvedStep{}, fmt.Errorf("the current version of %s is not known", display(f.name))
		}
		prev := st.source(f.name).Previous
		if prev == "" {
			prev = cur
		}
		targets = append(targets, config.Target{Name: f.name, Version: cur, PreviousVersion: prev})
	}
	model := &config.Model{Projects: []config.Project{ap.Runnable()}}
	plan, err := model.Resolve(ap.Name, targets)
	if err != nil {
		return config.ResolvedStep{}, err
	}
	for _, s := range plan.Steps {
		if s.Type == config.StepBackup && s.Backup != nil && s.Backup.RestoreCommand != "" {
			return s, nil
		}
	}
	return config.ResolvedStep{}, ErrNoRestore
}
