package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/notify"
)

func TestDeploySucceeds(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", auto))
	f.sources.setTags("app", "1.0.0", "1.1.0", "1.2.0-rc1")

	out := f.runNow()
	if out.Run == nil || out.Run.Result != ResultSucceeded {
		t.Fatalf("outcome = %+v", out)
	}
	if got := f.env("APP_TAG"); got != "1.1.0" {
		t.Errorf("APP_TAG = %q", got)
	}
	src := f.source("app")
	if src.Current != "1.1.0" || src.Previous != "1.0.0" || src.Digest != "sha256:demo-1.1.0" || len(src.Available) != 0 {
		t.Errorf("state = %+v", src)
	}
	if got := f.notifier.types(); !reflect.DeepEqual(got, []notify.EventType{notify.EventDeploySucceeded}) {
		t.Fatalf("events = %v", got)
	}
	ev := f.notifier.last()
	if ev.Project != demoProject || len(ev.Images) != 1 || ev.Images[0] != (notify.ImageVersion{Image: "app", Current: "1.1.0", Previous: "1.0.0"}) {
		t.Errorf("event = %+v", ev)
	}
	if !reflect.DeepEqual(f.notifier.chans[0], []string{"demo-ops"}) {
		t.Errorf("channels = %v", f.notifier.chans)
	}
	entries := f.audit.list()
	if len(entries) != 1 || entries[0].Action != auditlog.OperationDeployRun || entries[0].Result != auditlog.ResultSuccess || entries[0].Project != demoProject {
		t.Errorf("audit = %+v", entries)
	}
	if want := []string{"make-backup", "deploy '1.1.0'", "verify"}; !reflect.DeepEqual(f.runner.calls(), want) {
		t.Errorf("calls = %v", f.runner.calls())
	}

	// State and run log are private files.
	dir := filepath.Join(f.stateDir, deployDir, demoProject)
	for path, want := range map[string]os.FileMode{dir: dirMode, filepath.Join(dir, stateFile): fileMode,
		filepath.Join(dir, runsDir, out.Run.Log): fileMode} {
		fi, err := os.Stat(path)
		if err != nil || fi.Mode().Perm() != want {
			t.Errorf("%s: %v %v", path, fi.Mode(), err)
		}
	}

	// Nothing newer: the next run does nothing.
	if out := f.runNow(); out.Run != nil {
		t.Errorf("second run deployed: %+v", out.Run)
	}
}

func TestFailedCheckRollsBack(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", auto))
	f.sources.setTags("app", "1.0.0", "1.1.0")
	f.failVerify(1)

	out := f.runNow()
	if out.Run == nil || out.Run.Result != ResultRolledBack || out.Run.FailedStep != "step 4 (command)" {
		t.Fatalf("outcome = %+v", out.Run)
	}
	if want := []string{"make-backup", "deploy '1.1.0'", "verify", "deploy '1.0.0'", "verify"}; !reflect.DeepEqual(f.runner.calls(), want) {
		t.Errorf("calls = %v", f.runner.calls())
	}
	if got := f.env("APP_TAG"); got != "1.0.0" {
		t.Errorf("APP_TAG = %q", got)
	}
	src := f.source("app")
	if src.Current != "1.0.0" || src.Previous != "" || src.Failed != "1.1.0" {
		t.Errorf("state = %+v", src)
	}
	if st := f.status(); st.Paused || st.Fault != nil {
		t.Errorf("paused after a rollback: %+v", st)
	}
	if got := f.notifier.types(); !reflect.DeepEqual(got, []notify.EventType{notify.EventDeployRolled}) {
		t.Fatalf("events = %v", got)
	}
	ev := f.notifier.last()
	if ev.FailedStep == "" || ev.Images[0] != (notify.ImageVersion{Image: "app", Current: "1.0.0", Target: "1.1.0"}) {
		t.Errorf("event = %+v", ev)
	}
	var actions []string
	for _, e := range f.audit.list() {
		actions = append(actions, e.Action+"/"+e.Result)
	}
	if want := []string{"deploy.run/failure", "deploy.rollback/success"}; !reflect.DeepEqual(actions, want) {
		t.Errorf("audit = %v", actions)
	}

	// No secret in the run log, the state or the event.
	log, err := os.ReadFile(filepath.Join(f.stateDir, deployDir, demoProject, runsDir, out.Run.Log))
	if err != nil {
		t.Fatal(err)
	}
	state, err := os.ReadFile(filepath.Join(f.stateDir, deployDir, demoProject, stateFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "rollback") || strings.Contains(string(log), demoSecret) || strings.Contains(string(state), demoSecret) || strings.Contains(ev.Error, demoSecret) {
		t.Errorf("secret leaked or rollback missing:\n%s\n%s", log, state)
	}

	// The rolled-back version is not deployed again by itself; it is reported as available once.
	if out := f.runNow(); out.Run != nil {
		t.Errorf("deployed the rolled-back version again: %+v", out.Run)
	}
	if got := f.notifier.types(); len(got) != 2 || got[1] != notify.EventUpdateAvailable {
		t.Errorf("events = %v", got)
	}
}

func TestFailedRollbackPauses(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", auto))
	f.sources.setTags("app", "1.0.0", "1.1.0")
	f.failVerify(-1)

	out := f.runNow()
	if out.Run == nil || out.Run.Result != ResultRollbackFailed {
		t.Fatalf("outcome = %+v", out.Run)
	}
	st := f.status()
	if !st.Paused || st.Fault == nil || st.Fault.Run != out.Run.ID || st.Fault.FailedStep == "" {
		t.Errorf("state = %+v", st.State)
	}
	if got := f.notifier.types(); !reflect.DeepEqual(got, []notify.EventType{notify.EventRollbackFailed}) {
		t.Errorf("events = %v", got)
	}
	var codes []string
	for _, e := range f.audit.list() {
		codes = append(codes, e.Action+"/"+e.ErrorCode)
	}
	if want := []string{"deploy.run/" + codeStepFailed, "deploy.rollback/" + codeRollbackFail}; !reflect.DeepEqual(codes, want) {
		t.Errorf("audit = %v", codes)
	}

	n := len(f.runner.calls())
	if out := f.runNow(); out.Skipped != SkipPaused || len(f.runner.calls()) != n {
		t.Errorf("paused project ran: %+v", out)
	}
	if err := f.eng.Resume(demoProject); err != nil {
		t.Fatal(err)
	}
	if st := f.status(); st.Paused || st.Fault != nil {
		t.Errorf("resume left %+v", st.State)
	}
}

func TestFailureInBackupDoesNotRollBack(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", auto))
	f.sources.setTags("app", "1.0.0", "1.1.0")
	f.runner.fail = func(line string) bool { return line == "make-backup" }

	out := f.runNow()
	if out.Run == nil || out.Run.Result != ResultFailed || out.Run.ErrorCode != codeStepFailed {
		t.Fatalf("outcome = %+v", out.Run)
	}
	if want := []string{"make-backup"}; !reflect.DeepEqual(f.runner.calls(), want) {
		t.Errorf("calls = %v", f.runner.calls())
	}
	if got := f.env("APP_TAG"); got != "1.0.0" {
		t.Errorf("APP_TAG = %q", got)
	}
	if got := f.notifier.types(); !reflect.DeepEqual(got, []notify.EventType{notify.EventDeployFailed}) {
		t.Errorf("events = %v", got)
	}
	if src := f.source("app"); src.Failed != "" || src.Current != "1.0.0" {
		t.Errorf("state = %+v", src)
	}
}

func TestTwoImagesRollBackTogether(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", auto), demoImage("db", "DB_TAG", auto))
	f.sources.setTags("app", "1.0.0", "1.1.0")
	f.sources.setTags("db", "2.0.0", "2.0.1")
	f.failVerify(1)

	out := f.runNow()
	if out.Run == nil || out.Run.Result != ResultRolledBack || len(out.Run.Changes) != 2 {
		t.Fatalf("outcome = %+v", out.Run)
	}
	want := []string{"make-backup", "deploy '1.1.0'", "deploy '2.0.1'", "verify", "deploy '1.0.0'", "deploy '2.0.0'", "verify"}
	if !reflect.DeepEqual(f.runner.calls(), want) {
		t.Errorf("calls = %v", f.runner.calls())
	}
	if f.env("APP_TAG") != "1.0.0" || f.env("DB_TAG") != "2.0.0" {
		t.Errorf("env = %s %s", f.env("APP_TAG"), f.env("DB_TAG"))
	}
	if a, d := f.source("app"), f.source("db"); a.Current != "1.0.0" || d.Current != "2.0.0" || a.Failed != "1.1.0" || d.Failed != "2.0.1" {
		t.Errorf("state = %+v %+v", a, d)
	}
	if ev := f.notifier.last(); ev.Type != notify.EventDeployRolled || len(ev.Images) != 2 {
		t.Errorf("event = %+v", ev)
	}
}

func TestManualModeReportsOnce(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", config.VersionRule{Mode: config.ModeManual}))
	f.sources.setTags("app", "1.0.0", "1.0.1", "1.1.0")

	f.runNow()
	f.runNow()
	if len(f.runner.calls()) != 0 {
		t.Fatalf("manual mode deployed: %v", f.runner.calls())
	}
	if got := f.notifier.types(); !reflect.DeepEqual(got, []notify.EventType{notify.EventUpdateAvailable}) {
		t.Fatalf("events = %v", got)
	}
	if ev := f.notifier.last(); ev.Images[0] != (notify.ImageVersion{Image: "app", Current: "1.0.0", Target: "1.1.0"}) {
		t.Errorf("event = %+v", ev)
	}
	if src := f.source("app"); !reflect.DeepEqual(src.Available, []string{"1.0.1", "1.1.0"}) {
		t.Errorf("available = %v", src.Available)
	}

	// After a restart the same versions are not reported again; a new one is, once.
	f.eng = f.newEngine()
	f.runNow()
	if n := len(f.notifier.types()); n != 1 {
		t.Errorf("reported again after restart: %d events", n)
	}
	f.sources.setTags("app", "1.0.0", "1.0.1", "1.1.0", "1.2.0")
	f.runNow()
	f.runNow()
	if got := f.notifier.types(); len(got) != 2 || f.notifier.last().Images[0].Target != "1.2.0" {
		t.Errorf("events = %v", got)
	}
	if len(f.runner.calls()) != 0 {
		t.Errorf("manual mode deployed: %v", f.runner.calls())
	}
}

func TestAboveCapIsOnlyReported(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", config.VersionRule{Mode: config.ModeAuto, MaxUpdate: config.LevelPatch}))
	f.sources.setTags("app", "1.0.0", "1.0.1", "1.1.0")

	if out := f.runNow(); out.Run == nil || out.Run.Result != ResultSucceeded || f.env("APP_TAG") != "1.0.1" {
		t.Fatalf("outcome = %+v", out.Run)
	}
	calls := len(f.runner.calls())
	f.runNow()
	f.runNow()
	if len(f.runner.calls()) != calls {
		t.Errorf("deployed above the cap: %v", f.runner.calls())
	}
	if got := f.notifier.types(); !reflect.DeepEqual(got, []notify.EventType{notify.EventDeploySucceeded, notify.EventUpdateAvailable}) {
		t.Errorf("events = %v", got)
	}
	if src := f.source("app"); !reflect.DeepEqual(src.Available, []string{"1.1.0"}) {
		t.Errorf("available = %v", src.Available)
	}
}

func TestLockedVaultRunsNothing(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", auto))
	f.sources.setTags("app", "1.0.0", "1.1.0")
	f.vault.locked, f.approvals.locked = true, true

	if out := f.runNow(); out.Skipped != SkipLocked || out.Run != nil {
		t.Errorf("outcome = %+v", out)
	}
	if len(f.runner.calls()) != 0 || f.sources.count("app") != 0 {
		t.Errorf("locked vault ran: %v", f.runner.calls())
	}
	if _, err := f.eng.Update(context.Background(), demoProject, map[string]string{"app": "1.1.0"}); !errors.Is(err, ErrLocked) {
		t.Errorf("manual update while locked: %v", err)
	}
}

func TestLockedVaultReadsOnlyPublicSources(t *testing.T) {
	f := newFixture(t)
	private := demoImage("db", "DB_TAG", config.VersionRule{Mode: config.ModeManual})
	private.Credential = "demo-registry"
	f.project(demoImage("app", "APP_TAG", auto), private)
	f.sources.setTags("app", "1.0.0")
	f.sources.setTags("db", "2.0.0")
	f.runNow() // remembers the approved state while unlocked

	f.vault.locked, f.approvals.locked = true, true
	f.sources.setTags("app", "1.0.0", "1.1.0")
	f.sources.setTags("db", "2.0.0", "2.1.0")
	if out := f.runNow(); out.Skipped != SkipLocked {
		t.Errorf("outcome = %+v", out)
	}
	if len(f.runner.calls()) != 0 || f.sources.count("db") != 1 || len(f.notifier.types()) != 0 {
		t.Errorf("calls %v, db reads %d, events %v", f.runner.calls(), f.sources.count("db"), f.notifier.types())
	}
	if src := f.source("app"); !reflect.DeepEqual(src.Available, []string{"1.1.0"}) {
		t.Errorf("available = %v", src.Available)
	}

	// Unlocked again, the update is deployed.
	f.vault.locked, f.approvals.locked = false, false
	if out := f.runNow(); out.Run == nil || out.Run.Result != ResultSucceeded {
		t.Errorf("outcome = %+v", out)
	}
}

func TestPinAndPausePreventDeploys(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", auto))
	f.sources.setTags("app", "1.0.0")
	f.runNow()
	f.sources.setTags("app", "1.0.0", "1.1.0")
	ctx := context.Background()

	if err := f.eng.Pin(demoProject); err != nil {
		t.Fatal(err)
	}
	if st := f.status(); st.Pin == nil || st.Pin.Versions["app"] != "1.0.0" {
		t.Errorf("pin = %+v", st.Pin)
	}
	if out := f.runNow(); out.Skipped != SkipPinned {
		t.Errorf("pinned: %+v", out)
	}
	if _, err := f.eng.Update(ctx, demoProject, map[string]string{"app": "1.1.0"}); !errors.Is(err, ErrPinned) {
		t.Errorf("manual update while pinned: %v", err)
	}
	if err := f.eng.Unpin(demoProject); err != nil {
		t.Fatal(err)
	}
	if err := f.eng.Pause(demoProject); err != nil {
		t.Fatal(err)
	}
	if out := f.runNow(); out.Skipped != SkipPaused {
		t.Errorf("paused: %+v", out)
	}
	if _, err := f.eng.Update(ctx, demoProject, map[string]string{"app": "1.1.0"}); !errors.Is(err, ErrPaused) {
		t.Errorf("manual update while paused: %v", err)
	}
	if len(f.runner.calls()) != 0 {
		t.Errorf("deployed: %v", f.runner.calls())
	}
	if err := f.eng.Resume(demoProject); err != nil {
		t.Fatal(err)
	}
	if out := f.runNow(); out.Run == nil || out.Run.Result != ResultSucceeded {
		t.Errorf("after resume: %+v", out)
	}
}

func TestManualUpdateRules(t *testing.T) {
	f := newFixture(t)
	f.writeEnv("APP_TAG=2.0.0\n")
	f.project(demoImage("app", "APP_TAG", config.VersionRule{Mode: config.ModeManual, MajorSteps: config.MajorStep}))
	f.sources.setTags("app", "1.9.0", "2.0.0", "2.1.0", "3.0.0", "3.1.0", "4.0.0")
	ctx := context.Background()

	for _, v := range []string{"1.9.0", "2.0.0", "3.0.0", "4.0.0", "2.2.0", "latest"} {
		if _, err := f.eng.Update(ctx, demoProject, map[string]string{"app": v}); !errors.Is(err, ErrVersion) {
			t.Errorf("%s: %v", v, err)
		}
	}
	if _, err := f.eng.Update(ctx, demoProject, map[string]string{"web": "2.1.0"}); err == nil {
		t.Error("unknown image accepted")
	}
	if len(f.runner.calls()) != 0 {
		t.Fatalf("refused update ran: %v", f.runner.calls())
	}

	for _, v := range []string{"2.1.0", "3.1.0"} {
		rec, err := f.eng.Update(ctx, demoProject, map[string]string{"app": v})
		if err != nil || rec.Result != ResultSucceeded || rec.Action != ActionManual || f.env("APP_TAG") != v {
			t.Fatalf("%s: %+v %v", v, rec, err)
		}
	}
	if src := f.source("app"); src.Current != "3.1.0" || src.Previous != "2.1.0" || src.Digest != "sha256:demo-3.1.0" {
		t.Errorf("state = %+v", src)
	}

	// With direct major steps any available version may be chosen.
	g := newFixture(t)
	g.project(demoImage("app", "APP_TAG", config.VersionRule{Mode: config.ModeManual}))
	g.sources.setTags("app", "1.0.0", "1.1.0", "2.0.0", "3.0.0")
	if rec, err := g.eng.Update(ctx, demoProject, map[string]string{"app": "3.0.0"}); err != nil || rec.Result != ResultSucceeded {
		t.Errorf("direct: %+v %v", rec, err)
	}
}

func TestNeverDowngradesAutomatically(t *testing.T) {
	f := newFixture(t)
	f.writeEnv("APP_TAG=2.0.0\n")
	f.project(demoImage("app", "APP_TAG", auto))
	f.sources.setTags("app", "1.0.0", "1.9.9", "2.0.0")
	if out := f.runNow(); out.Run != nil || len(f.runner.calls()) != 0 {
		t.Errorf("outcome = %+v", out.Run)
	}
}

func TestStateSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	f.project(demoImage("app", "APP_TAG", auto))
	f.sources.setTags("app", "1.0.0", "1.1.0")
	f.runNow()
	before := f.status()
	if err := f.eng.Pause(demoProject); err != nil {
		t.Fatal(err)
	}

	f.eng = f.newEngine()
	after := f.status()
	if !after.Paused || !reflect.DeepEqual(after.Sources, before.Sources) || len(after.History) != 1 || after.History[0].ID != before.History[0].ID {
		t.Errorf("before %+v\nafter %+v", before.State, after.State)
	}
	if h, err := f.eng.History(demoProject); err != nil || len(h) != 1 || h[0].Result != ResultSucceeded {
		t.Errorf("history = %+v %v", h, err)
	}
	if path, err := f.eng.RunLogPath(demoProject, h0(after).Log); err != nil {
		t.Error(err)
	} else if _, err := os.Stat(path); err != nil {
		t.Error(err)
	}
}

func h0(s Status) RunRecord { return s.History[0] }

func TestInvalidProjectNames(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"", "../demo", "Demo", "demo/shop"} {
		if _, err := f.eng.Status(name); err == nil {
			t.Errorf("status %q accepted", name)
		}
		if err := f.eng.Pause(name); err == nil {
			t.Errorf("pause %q accepted", name)
		}
		if _, err := f.eng.RunNow(context.Background(), name); err == nil {
			t.Errorf("run %q accepted", name)
		}
	}
	if _, err := f.eng.RunLogPath(demoProject, "../state.json"); err == nil {
		t.Error("run log outside the runs directory accepted")
	}
}

func TestGitSourceDeploys(t *testing.T) {
	f := newFixture(t)
	p := config.Project{
		Schema: config.CurrentSchema, Name: demoProject, Dir: f.projDir,
		Steps: []config.Step{{Type: config.StepCommand, Run: "checkout {version}"}, {Type: config.StepCommand, Run: "verify"}},
		Git:   &config.GitSource{URL: "https://git.example.com/demo/shop.git", VersionRule: auto},
	}
	f.approvals.set(p)
	f.sources.git = "v1.0.0"
	f.sources.setTags("", "v1.0.0", "v1.1.0")
	f.runner.onLine = func(line string) {
		if v, ok := strings.CutPrefix(line, "checkout "); ok {
			f.sources.mu.Lock()
			f.sources.git = strings.Trim(v, "'")
			f.sources.mu.Unlock()
		}
	}
	out := f.runNow()
	if out.Run == nil || out.Run.Result != ResultSucceeded || f.source("").Current != "v1.1.0" || f.source("").Previous != "v1.0.0" {
		t.Fatalf("outcome = %+v state %+v", out.Run, f.status().Sources)
	}
}

// stepsProject approves a project without backup that runs the given steps.
func (f *fixture) stepsProject(steps ...config.Step) {
	p := config.Project{
		Schema: config.CurrentSchema, Name: demoProject, Dir: f.projDir, EnvFile: f.envFile,
		Steps:  steps,
		Images: []config.Image{demoImage("app", "APP_TAG", auto)},
	}
	f.approvals.set(p)
	f.sources.setTags("app", "1.0.0", "1.1.0")
}

func TestRollbackOnlyWhenSomethingChanged(t *testing.T) {
	setEnv := config.Step{Type: config.StepSetEnv, Assign: []string{"{var}={version}"}}
	pull := config.Step{Type: config.StepComposePull, Services: []string{"{services}"}}
	up := config.Step{Type: config.StepComposeUp, Services: []string{"{services}"}}
	cmd := config.Step{Type: config.StepCommand, Run: "work"}
	partial := config.Step{Type: config.StepCommand, Run: "work", PartialChanges: true}
	failOn := func(sub string) func(string) bool {
		return func(line string) bool { return strings.Contains(line, sub) }
	}

	cases := []struct {
		name     string
		steps    []config.Step
		fail     func(string) bool
		breakEnv bool
		want     string
	}{
		{"atomic set-env fails first", []config.Step{setEnv, pull, up}, nil, true, ResultFailed},
		{"pull fails after set-env", []config.Step{setEnv, pull, up}, failOn(" pull"), false, ResultRolledBack},
		{"compose-up fails first", []config.Step{pull, up}, failOn(" up"), false, ResultRolledBack},
		{"pull fails first", []config.Step{pull, up}, failOn(" pull"), false, ResultFailed},
		{"command fails first", []config.Step{cmd}, failOn("work"), false, ResultFailed},
		{"marked command fails first", []config.Step{partial}, failOn("work"), false, ResultRolledBack},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.breakEnv {
				if os.Geteuid() == 0 {
					t.Skip("directory permissions do not apply to root")
				}
				// The atomic env write cannot create its temporary file in a read-only directory.
				if err := os.Chmod(filepath.Dir(f.envFile), 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(filepath.Dir(f.envFile), 0o700) })
			}
			f.stepsProject(tc.steps...)
			if tc.fail != nil {
				// Fail the step once; the rollback then runs it again and succeeds.
				failed := false
				f.runner.fail = func(line string) bool {
					if failed || !tc.fail(line) {
						return false
					}
					failed = true
					return true
				}
			}
			out := f.runNow()
			if out.Run == nil || out.Run.Result != tc.want {
				t.Fatalf("outcome = %+v, calls %v", out.Run, f.runner.calls())
			}
			if tc.want == ResultFailed && f.status().Paused {
				t.Error("project paused without a rollback")
			}
		})
	}
}
