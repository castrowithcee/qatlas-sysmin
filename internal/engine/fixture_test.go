package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/notify"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
	"github.com/castrowithcee/qatlas-sysmin/internal/source"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/version"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const (
	demoProject = "demo-shop"
	demoSecret  = "demo-secret-value-0042"
)

var demoStart = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// fakeApprovals hands out fixed approved states; locked makes it fail like a locked vault.
type fakeApprovals struct {
	mu       sync.Mutex
	projects map[string]approval.Approved
	locked   bool
}

func (a *fakeApprovals) Approved(name string) (approval.Approved, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.locked {
		return approval.Approved{}, vault.ErrLocked
	}
	ap, ok := a.projects[name]
	if !ok {
		return approval.Approved{}, approval.ErrNotApproved
	}
	return ap, nil
}

func (a *fakeApprovals) set(p config.Project) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.projects == nil {
		a.projects = map[string]approval.Approved{}
	}
	steps := p.Steps
	p.Steps = nil
	a.projects[p.Name] = approval.Approved{Name: p.Name, Fingerprint: "demo-fingerprint", Project: p, Steps: steps,
		Credentials: map[string]string{}}
}

type fakeVault struct {
	mu     sync.Mutex
	locked bool
}

func (v *fakeVault) State() (vault.State, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.locked {
		return vault.StateLocked, nil
	}
	return vault.StateUnlocked, nil
}

// fakeSources reads the current tag of an image from the env file, as the service does, and selects from
// a fixed tag list per source. The git source keeps its current tag in git.
type fakeSources struct {
	mu    sync.Mutex
	tags  map[string][]string
	git   string
	reads map[string]int
	// block, when set, is waited on by every check after started is signaled.
	started chan string
	block   chan struct{}
}

func (s *fakeSources) setTags(name string, tags ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tags == nil {
		s.tags = map[string][]string{}
	}
	s.tags[name] = tags
}

func (s *fakeSources) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads[name]
}

func (s *fakeSources) hit(name string) []string {
	s.mu.Lock()
	if s.reads == nil {
		s.reads = map[string]int{}
	}
	s.reads[name]++
	tags := s.tags[name]
	started, block := s.started, s.block
	s.mu.Unlock()
	if started != nil {
		started <- name
	}
	if block != nil {
		<-block
	}
	return tags
}

func (s *fakeSources) CurrentImage(_ context.Context, ap approval.Approved, img config.Image) (string, error) {
	v, found, err := execute.ReadEnvValue(ap.Project.EnvFile, img.EnvVar)
	if err != nil || !found {
		return "", errors.New("not set")
	}
	return v, nil
}

func (s *fakeSources) CurrentGit(context.Context, approval.Approved) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.git, nil
}

func (s *fakeSources) CheckImage(_ context.Context, _ approval.Approved, img config.Image, current string) (source.ImageResult, error) {
	tags := s.hit(img.Name)
	sel := version.Select(current, tags, img.VersionRule)
	res := source.ImageResult{Selection: sel}
	if sel.Target != "" {
		res.Digest = "sha256:demo-" + sel.Target
	}
	return res, nil
}

func (s *fakeSources) CheckGit(_ context.Context, ap approval.Approved, current string) (source.GitResult, error) {
	tags := s.hit("")
	return source.GitResult{Selection: version.Select(current, tags, ap.Project.Git.VersionRule)}, nil
}

func (s *fakeSources) ImageDigest(_ context.Context, _ approval.Approved, _ config.Image, tag string) (string, error) {
	return "sha256:demo-" + tag, nil
}

// fakeRunner answers every command with success unless fail says otherwise, and records the shell lines.
type fakeRunner struct {
	mu    sync.Mutex
	lines []string
	envs  [][]string
	fail  func(line string) bool
	// onLine runs outside the lock for every command, before it is answered.
	onLine func(line string)
}

func (r *fakeRunner) Run(_ context.Context, c execute.Command) (execute.Result, error) {
	line := c.Name + " " + strings.Join(c.Args, " ")
	if c.Name == "/bin/sh" && len(c.Args) == 2 {
		line = c.Args[1]
	}
	r.mu.Lock()
	r.lines = append(r.lines, line)
	r.envs = append(r.envs, c.Env)
	fail, onLine := r.fail, r.onLine
	r.mu.Unlock()
	if onLine != nil {
		onLine(line)
	}
	if fail != nil && fail(line) {
		return execute.Result{Output: []byte("check failed for " + demoSecret + "\n"), ExitCode: 1}, &execute.ExitError{Code: 1}
	}
	return execute.Result{}, nil
}

func (r *fakeRunner) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

func (r *fakeRunner) count(line string) int {
	n := 0
	for _, l := range r.calls() {
		if l == line {
			n++
		}
	}
	return n
}

type fakeNotifier struct {
	mu     sync.Mutex
	events []notify.Event
	chans  [][]string
}

func (n *fakeNotifier) Notify(ev notify.Event, channels ...string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, ev)
	n.chans = append(n.chans, channels)
}

func (n *fakeNotifier) types() []notify.EventType {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []notify.EventType
	for _, ev := range n.events {
		out = append(out, ev.Type)
	}
	return out
}

func (n *fakeNotifier) last() notify.Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.events[len(n.events)-1]
}

type fakeAudit struct {
	mu      sync.Mutex
	entries []auditlog.Fields
}

func (a *fakeAudit) Append(f auditlog.Fields) error {
	if err := f.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, f)
	return nil
}

func (a *fakeAudit) list() []auditlog.Fields {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]auditlog.Fields(nil), a.entries...)
}

// fakeClock moves only when the test advances it. Every After call is announced on waiting.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	timers  []fakeTimer
	waiting chan struct{}
}

type fakeTimer struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: demoStart, waiting: make(chan struct{}, 100)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
	} else {
		c.timers = append(c.timers, fakeTimer{at: c.now.Add(d), ch: ch})
	}
	c.mu.Unlock()
	c.waiting <- struct{}{}
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.timers[:0]
	for _, t := range c.timers {
		if t.at.After(c.now) {
			kept = append(kept, t)
			continue
		}
		t.ch <- c.now
	}
	c.timers = kept
}

type fixture struct {
	t         *testing.T
	stateDir  string
	backups   string
	projDir   string
	envFile   string
	approvals *fakeApprovals
	vault     *fakeVault
	sources   *fakeSources
	runner    *fakeRunner
	notifier  *fakeNotifier
	audit     *fakeAudit
	clock     *fakeClock
	projects  []string
	eng       *Engine
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, stateDir: filepath.Join(t.TempDir(), "state"), projDir: t.TempDir(),
		approvals: &fakeApprovals{}, vault: &fakeVault{}, sources: &fakeSources{}, runner: &fakeRunner{},
		notifier: &fakeNotifier{}, audit: &fakeAudit{}, clock: newFakeClock()}
	f.backups = filepath.Join(f.stateDir, "backups")
	f.envFile = filepath.Join(f.projDir, ".env")
	f.writeEnv("APP_TAG=1.0.0\nDB_TAG=2.0.0\n")
	f.eng = f.newEngine()
	return f
}

// newEngine starts a new engine on the same directories, as after a restart.
func (f *fixture) newEngine() *Engine {
	f.t.Helper()
	e, err := New(Config{
		StateDir: f.stateDir, Approvals: f.approvals, Vault: f.vault, Sources: f.sources,
		Executor: func(approval.Approved) (*execute.Executor, error) {
			r := &redact.Redactor{}
			r.Add(demoSecret)
			return &execute.Executor{Runner: f.runner, Redactor: r}, nil
		},
		Notifier: f.notifier, Audit: f.audit, Clock: f.clock,
		Projects:     func() []string { return f.projects },
		PollInterval: func() time.Duration { return 10 * time.Minute },
		Jitter:       func(time.Duration) time.Duration { return 0 },
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}

func (f *fixture) writeEnv(content string) {
	f.t.Helper()
	if err := os.WriteFile(f.envFile, []byte(content), 0o640); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) env(key string) string {
	f.t.Helper()
	v, _, err := execute.ReadEnvValue(f.envFile, key)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

func demoImage(name, envVar string, rule config.VersionRule) config.Image {
	return config.Image{Name: name, Ref: "registry.example.com/demo/" + name, EnvVar: envVar,
		Services: []string{name}, VersionRule: rule}
}

var auto = config.VersionRule{Mode: config.ModeAuto}

// project approves a project with a backup, a set-env, a deploy command per image and a check command.
func (f *fixture) project(images ...config.Image) config.Project {
	p := config.Project{
		Schema: config.CurrentSchema, Name: demoProject, Dir: f.projDir, EnvFile: f.envFile,
		Steps: []config.Step{
			{Type: config.StepBackup},
			{Type: config.StepSetEnv, Assign: []string{"{var}={version}"}},
			{Type: config.StepCommand, Run: "deploy {version}"},
			{Type: config.StepCommand, Run: "verify"},
		},
		Backup: &config.Step{Command: "make-backup", RestoreCommand: "restore-backup"},
		Notify: &config.Notify{Channels: []string{"demo-ops"}, Events: []string{config.EventUpdateAvailable,
			config.EventUpdateSucceeded, config.EventUpdateFailed}},
		Images: images,
	}
	f.approvals.set(p)
	return p
}

func (f *fixture) runNow() Outcome {
	f.t.Helper()
	out, err := f.eng.RunNow(context.Background(), demoProject)
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *fixture) status() Status {
	f.t.Helper()
	st, err := f.eng.Status(demoProject)
	if err != nil {
		f.t.Fatal(err)
	}
	return st
}

func (f *fixture) source(name string) SourceState {
	f.t.Helper()
	st := f.status()
	for _, s := range st.Sources {
		if s.Name == name {
			return s
		}
	}
	f.t.Fatalf("no state for source %q: %+v", name, st.Sources)
	return SourceState{}
}

// failVerify makes the check command fail the first n times, or always for n < 0.
func (f *fixture) failVerify(n int) {
	var mu sync.Mutex
	f.runner.fail = func(line string) bool {
		if line != "verify" {
			return false
		}
		mu.Lock()
		defer mu.Unlock()
		if n < 0 {
			return true
		}
		if n > 0 {
			n--
			return true
		}
		return false
	}
}
