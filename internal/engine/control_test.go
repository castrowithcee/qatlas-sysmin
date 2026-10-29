package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/notify"
)

// idle waits until no run of the project is active. A run signals its start through the fakes but its end
// only by releasing the project, so this spins on that; it never decides an outcome, it only waits.
func (f *fixture) idle(project string) {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		f.eng.mu.Lock()
		busy := f.eng.running[project]
		f.eng.mu.Unlock()
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatal("run did not finish")
		}
		runtime.Gosched()
	}
}

// wait receives from ch or fails the test after a generous bound; it only guards against a hang.
func wait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("timed out")
	}
	var zero T
	return zero
}

func TestParallelTriggerIsDropped(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", auto))
	f.sources.setTags("app", "1.0.0", "1.1.0")
	inDeploy, release := make(chan struct{}), make(chan struct{})
	f.runner.onLine = func(line string) {
		if line == "deploy '1.1.0'" {
			close(inDeploy)
			<-release
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	plannerDone := make(chan error)
	go func() { plannerDone <- f.eng.Run(ctx) }()
	wait(t, f.clock.waiting) // the planner is active and has no project to poll

	first := make(chan Outcome)
	go func() {
		out, err := f.eng.RunNow(context.Background(), demoProject)
		if err != nil {
			t.Error(err)
		}
		first <- out
	}()
	wait(t, inDeploy)

	if _, err := f.eng.RunNow(context.Background(), demoProject); !errors.Is(err, ErrBusy) {
		t.Errorf("second run: %v", err)
	}
	if f.eng.Trigger(demoProject) {
		t.Error("trigger during a run was not dropped")
	}
	if _, err := f.eng.Update(context.Background(), demoProject, map[string]string{"app": "1.1.0"}); !errors.Is(err, ErrBusy) {
		t.Errorf("manual update during a run: %v", err)
	}
	if _, err := f.eng.Restore(context.Background(), demoProject, "20260301T120000Z"); !errors.Is(err, ErrBusy) {
		t.Errorf("restore during a run: %v", err)
	}
	if st := f.status(); !st.Running {
		t.Error("status does not show the active run")
	}
	close(release)
	if out := wait(t, first); out.Run == nil || out.Run.Result != ResultSucceeded {
		t.Errorf("first run: %+v", out.Run)
	}
	if n := f.runner.count("deploy '1.1.0'"); n != 1 {
		t.Errorf("deployed %d times", n)
	}
	if f.sources.count("app") != 1 {
		t.Errorf("dropped trigger read the source: %d", f.sources.count("app"))
	}

	// Once the run is over, a trigger starts one again.
	f.sources.mu.Lock()
	f.sources.started = make(chan string, 1)
	f.sources.mu.Unlock()
	if !f.eng.Trigger(demoProject) {
		t.Error("trigger after the run was dropped")
	}
	wait(t, f.sources.started)
	cancel()
	if err := wait(t, plannerDone); err != nil {
		t.Error(err)
	}
	if f.eng.Trigger(demoProject) {
		t.Error("trigger without an active planner started a run")
	}
}

func TestPlannerPollsAndStops(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", config.VersionRule{Mode: config.ModeManual}))
	f.sources.setTags("app", "1.0.0")
	f.projects = []string{demoProject, "Not-Valid"}
	f.sources.started = make(chan string, 10)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- f.eng.Run(ctx) }()

	// Without jitter the first poll is due at once.
	wait(t, f.sources.started)
	wait(t, f.clock.waiting)
	f.idle(demoProject)
	// The planner sleeps at most refreshEvery; nothing is due before the interval.
	for range 9 {
		f.clock.Advance(time.Minute)
		wait(t, f.clock.waiting)
	}
	if n := f.sources.count("app"); n != 1 {
		t.Fatalf("polls before the interval: %d", n)
	}
	f.clock.Advance(time.Minute)
	wait(t, f.sources.started)
	f.idle(demoProject)

	// Run returns only after the run it started has finished.
	block := make(chan struct{})
	var finished atomic.Bool
	f.sources.mu.Lock()
	f.sources.block = block
	f.sources.mu.Unlock()
	wait(t, f.clock.waiting)
	f.clock.Advance(10 * time.Minute)
	wait(t, f.sources.started)
	cancel()
	go func() {
		finished.Store(true)
		close(block)
	}()
	if err := wait(t, done); err != nil {
		t.Error(err)
	}
	if !finished.Load() {
		t.Error("Run returned before its run finished")
	}
}

func TestPlannerUsesProjectInterval(t *testing.T) {
	f := newFixture(t)
	p := f.project(demoImage("app", "APP_TAG", config.VersionRule{Mode: config.ModeManual}))
	p.PollInterval = config.Duration(3 * time.Minute)
	f.approvals.set(p)
	f.sources.setTags("app", "1.0.0")
	if got := f.eng.interval(demoProject); got != 10*time.Minute {
		t.Errorf("default interval = %s", got)
	}
	f.runNow()
	if got := f.eng.interval(demoProject); got != 3*time.Minute {
		t.Errorf("project interval = %s", got)
	}
	var max time.Duration
	f.eng.cfg.Jitter = func(m time.Duration) time.Duration { max = m; return 0 }
	f.eng.spread(time.Hour)
	if max != maxSpread {
		t.Errorf("spread of an hour = %s", max)
	}
	f.eng.spread(10 * time.Minute)
	if max != time.Minute {
		t.Errorf("spread of ten minutes = %s", max)
	}
}

func TestRestore(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", auto))
	f.sources.setTags("app", "1.0.0", "1.1.0")
	out := f.runNow()
	if out.Run == nil || out.Run.Backup == "" {
		t.Fatalf("no backup: %+v", out.Run)
	}
	names, err := f.eng.Backups(demoProject)
	if err != nil || !slices.Equal(names, []string{out.Run.Backup}) {
		t.Fatalf("backups = %v %v", names, err)
	}

	// A backup of another project, an unknown one and a path are refused before anything runs.
	other := filepath.Join(f.backups, "demo-other", "20250101T000000Z")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	calls := len(f.runner.calls())
	for _, b := range []string{"20250101T000000Z", "20990101T000000Z", other, "../demo-other/20250101T000000Z", ""} {
		if _, err := f.eng.Restore(context.Background(), demoProject, b); !errors.Is(err, ErrBackup) {
			t.Errorf("%q: %v", b, err)
		}
	}
	if len(f.runner.calls()) != calls {
		t.Fatalf("refused restore ran: %v", f.runner.calls())
	}

	rec, err := f.eng.Restore(context.Background(), demoProject, out.Run.Backup)
	if err != nil || rec.Result != ResultSucceeded || rec.Action != ActionRestore || rec.Backup != out.Run.Backup {
		t.Fatalf("restore: %+v %v", rec, err)
	}
	if f.runner.count("restore-backup") != 1 {
		t.Errorf("calls = %v", f.runner.calls())
	}
	f.runner.mu.Lock()
	env := f.runner.envs[len(f.runner.envs)-1]
	f.runner.mu.Unlock()
	if want := "SYSMIN_BACKUP_DIR=" + filepath.Join(f.backups, demoProject, out.Run.Backup); !slices.Contains(env, want) {
		t.Errorf("env = %v", env)
	}
	if f.env("APP_TAG") != "1.1.0" {
		t.Errorf("restore changed the version: %s", f.env("APP_TAG"))
	}
	entries := f.audit.list()
	if last := entries[len(entries)-1]; last.Action != auditlog.OperationDeployRestore || last.Result != auditlog.ResultSuccess {
		t.Errorf("audit = %+v", last)
	}
	if h := f.status().History; len(h) != 2 || h[1].Action != ActionRestore {
		t.Errorf("history = %+v", h)
	}

	// A paused project may be restored; a locked vault refuses it.
	if err := f.eng.Pause(demoProject); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.Restore(context.Background(), demoProject, out.Run.Backup); err != nil {
		t.Errorf("restore while paused: %v", err)
	}
	f.vault.locked = true
	if _, err := f.eng.Restore(context.Background(), demoProject, out.Run.Backup); !errors.Is(err, ErrLocked) {
		t.Errorf("restore while locked: %v", err)
	}
}

func TestRestoreNotifies(t *testing.T) {
	f := newFixture(t)
	p := f.project(demoImage("app", "APP_TAG", auto))
	p.Notify.Events = append(p.Notify.Events, config.EventRestoreSucceed, config.EventRestoreFailed)
	f.approvals.set(p)
	f.sources.setTags("app", "1.0.0", "1.1.0")
	out := f.runNow()
	if out.Run == nil {
		t.Fatal("no run")
	}
	rec, err := f.eng.Restore(context.Background(), demoProject, out.Run.Backup)
	if err != nil || rec.Result != ResultSucceeded {
		t.Fatalf("restore: %+v %v", rec, err)
	}
	if ev := f.notifier.last(); ev.Type != notify.EventRestoreSucceeded || ev.Project != demoProject || ev.Error != "" {
		t.Errorf("event = %+v", ev)
	}

	f.runner.fail = func(line string) bool { return line == "restore-backup" }
	rec, err = f.eng.Restore(context.Background(), demoProject, out.Run.Backup)
	if err != nil || rec.Result != ResultFailed {
		t.Fatalf("failing restore: %+v %v", rec, err)
	}
	if ev := f.notifier.last(); ev.Type != notify.EventRestoreFailed || ev.Error == "" {
		t.Errorf("event = %+v", ev)
	}
}

func TestRestoreNeedsRestoreCommand(t *testing.T) {
	f := newFixture(t)
	p := f.project(demoImage("app", "APP_TAG", auto))
	p.Backup.RestoreCommand = ""
	f.approvals.set(p)
	f.sources.setTags("app", "1.0.0", "1.1.0")
	out := f.runNow()
	if _, err := f.eng.Restore(context.Background(), demoProject, out.Run.Backup); !errors.Is(err, ErrNoRestore) {
		t.Errorf("err = %v", err)
	}
}
