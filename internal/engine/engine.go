package engine

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/notify"
	"github.com/castrowithcee/qatlas-sysmin/internal/source"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// DefaultStateDir holds the state of every project when Config.StateDir is empty.
const DefaultStateDir = "/var/lib/sysmin"

// Errors, to be matched with errors.Is.
var (
	// ErrBusy means a run of the project is active; the request was dropped.
	ErrBusy = errors.New("a run of the project is active")
	// ErrLocked means the vault is locked, so nothing is deployed or restored.
	ErrLocked = errors.New("the vault is locked")
	// ErrPaused means the project is paused.
	ErrPaused = errors.New("the project is paused")
	// ErrPinned means the project is pinned to its versions.
	ErrPinned = errors.New("the project is pinned")
	// ErrVersion means a requested version is not an available update under the version rule.
	ErrVersion = errors.New("the version is not an available update")
	// ErrBackup means the backup is not one of the project's backups.
	ErrBackup = errors.New("the backup is not a backup of the project")
	// ErrNoRestore means the approved backup step has no restore command.
	ErrNoRestore = errors.New("the project has no restore command")
)

// Approvals hands out the approved state of a project; *approval.Manager does.
type Approvals interface {
	Approved(name string) (approval.Approved, error)
}

// VaultState reports whether the vault is unlocked; *vault.Vault does.
type VaultState interface {
	State() (vault.State, error)
}

// Sources reads the version sources of an approved project. An implementation uses only the credentials
// of the approved state, by the vault ids in approval.Approved.Credentials, and returns errors without
// secret values.
type Sources interface {
	// CurrentImage returns the tag the image runs on now.
	CurrentImage(ctx context.Context, ap approval.Approved, img config.Image) (string, error)
	// CurrentGit returns the tag the working copy of the project has checked out.
	CurrentGit(ctx context.Context, ap approval.Approved) (string, error)
	CheckImage(ctx context.Context, ap approval.Approved, img config.Image, current string) (source.ImageResult, error)
	CheckGit(ctx context.Context, ap approval.Approved, current string) (source.GitResult, error)
	// ImageDigest returns the manifest digest of one tag of the image.
	ImageDigest(ctx context.Context, ap approval.Approved, img config.Image, tag string) (string, error)
}

// Notifier reports an event over the named channels without blocking; *notify.Notifier does.
type Notifier interface {
	Notify(ev notify.Event, channels ...string)
}

// Auditor records one audit entry; *auditlog.Logger does.
type Auditor interface {
	Append(auditlog.Fields) error
}

// Clock is the time source of the engine and its planner.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Config wires the engine. Approvals, Vault, Sources and Executor are required.
type Config struct {
	// StateDir holds deploy/<project>/; empty means DefaultStateDir.
	StateDir string
	// BackupRoot is where backups are kept; empty means StateDir/backups.
	BackupRoot string
	Approvals  Approvals
	Vault      VaultState
	Sources    Sources
	// Executor returns the executor for a run of the approved project, with its runner, redactor and git
	// environment. The engine sets Log, BackupRoot and Now.
	Executor func(ap approval.Approved) (*execute.Executor, error)
	// Notifier and Audit may be nil.
	Notifier Notifier
	Audit    Auditor
	// Projects lists the projects the planner polls; nil means none.
	Projects func() []string
	// PollInterval is the interval of a project that sets none; nil means config.DefaultPollInterval.
	PollInterval func() time.Duration
	// Clock defaults to the system clock.
	Clock Clock
	// Jitter returns a random delay in [0, max); nil means a uniform one.
	Jitter func(max time.Duration) time.Duration
}

// Engine deploys projects. It is safe for concurrent use.
type Engine struct {
	cfg   Config
	store *store

	mu      sync.Mutex
	running map[string]bool
	// approved caches the last approved state verified per project, for checks of public sources while
	// the vault is locked.
	approved map[string]approval.Approved

	// The planner: base is its context while Run is active; wg counts the runs it and Trigger started.
	plannerMu sync.Mutex
	base      context.Context
	wg        sync.WaitGroup
}

// New returns an engine for cfg.
func New(cfg Config) (*Engine, error) {
	if cfg.Approvals == nil || cfg.Vault == nil || cfg.Sources == nil || cfg.Executor == nil {
		return nil, errors.New("engine needs approvals, vault, sources and an executor")
	}
	if cfg.StateDir == "" {
		cfg.StateDir = DefaultStateDir
	}
	if cfg.BackupRoot == "" {
		cfg.BackupRoot = filepath.Join(cfg.StateDir, "backups")
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	if cfg.Jitter == nil {
		cfg.Jitter = func(max time.Duration) time.Duration {
			if max <= 0 {
				return 0
			}
			return rand.N(max)
		}
	}
	if cfg.PollInterval == nil {
		cfg.PollInterval = func() time.Duration { return config.DefaultPollInterval }
	}
	return &Engine{cfg: cfg, store: &store{dir: cfg.StateDir}, running: map[string]bool{},
		approved: map[string]approval.Approved{}}, nil
}

func checkName(project string) error {
	if !config.ValidName(project) {
		return fmt.Errorf("invalid project name %q", project)
	}
	return nil
}

// acquire marks a run of project active; it fails with ErrBusy if one is.
func (e *Engine) acquire(project string) error {
	if err := checkName(project); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running[project] {
		return ErrBusy
	}
	e.running[project] = true
	return nil
}

func (e *Engine) release(project string) {
	e.mu.Lock()
	delete(e.running, project)
	e.mu.Unlock()
}

func (e *Engine) unlocked() bool {
	st, err := e.cfg.Vault.State()
	return err == nil && st == vault.StateUnlocked
}

// approvedState reads the approved state and remembers it; while the vault is locked it returns the one
// remembered, if any, with fresh false.
func (e *Engine) approvedState(project string) (ap approval.Approved, fresh bool, err error) {
	ap, err = e.cfg.Approvals.Approved(project)
	if err == nil {
		e.mu.Lock()
		e.approved[project] = ap
		e.mu.Unlock()
		return ap, true, nil
	}
	if errors.Is(err, vault.ErrLocked) {
		e.mu.Lock()
		cached, ok := e.approved[project]
		e.mu.Unlock()
		if ok {
			return cached, false, nil
		}
		return approval.Approved{}, false, ErrLocked
	}
	if errors.Is(err, approval.ErrNotApproved) || errors.Is(err, approval.ErrIntegrity) {
		e.mu.Lock()
		delete(e.approved, project)
		e.mu.Unlock()
	}
	return approval.Approved{}, false, err
}

// Status is the state of a project and whether a run is active.
type Status struct {
	State
	Running bool
}

// Status returns the stored state of a project.
func (e *Engine) Status(project string) (Status, error) {
	if err := checkName(project); err != nil {
		return Status{}, err
	}
	st, err := e.store.load(project)
	if err != nil {
		return Status{}, err
	}
	e.mu.Lock()
	running := e.running[project]
	e.mu.Unlock()
	return Status{State: st, Running: running}, nil
}

// History returns the latest runs of a project, oldest first.
func (e *Engine) History(project string) ([]RunRecord, error) {
	st, err := e.Status(project)
	return st.History, err
}

func (e *Engine) change(project string, fn func(*State)) error {
	if err := checkName(project); err != nil {
		return err
	}
	_, err := e.store.update(project, fn)
	return err
}

// Pause stops automatic and manual deploys of a project.
func (e *Engine) Pause(project string) error {
	return e.change(project, func(s *State) { s.Paused = true })
}

// Resume lets a project deploy again and clears its error state.
func (e *Engine) Resume(project string) error {
	return e.change(project, func(s *State) { s.Paused, s.Fault = false, nil })
}

// Pin holds a project on the versions it is on now.
func (e *Engine) Pin(project string) error {
	now := e.cfg.Clock.Now().UTC()
	return e.change(project, func(s *State) {
		p := &Pin{Time: now, Versions: map[string]string{}}
		for _, src := range s.Sources {
			if src.Current != "" {
				p.Versions[src.Name] = src.Current
			}
		}
		s.Pin = p
	})
}

// Unpin releases a pinned project.
func (e *Engine) Unpin(project string) error {
	return e.change(project, func(s *State) { s.Pin = nil })
}

func (e *Engine) audit(action, project, code string, d time.Duration) {
	if e.cfg.Audit == nil {
		return
	}
	f := auditlog.Fields{ClientUID: uint32(os.Getuid()), Action: action, Project: project,
		Result: auditlog.ResultSuccess, Duration: d}
	if code != "" {
		f.Result, f.ErrorCode = auditlog.ResultFailure, code
	}
	// A failed audit write must not change the outcome of a run that already happened.
	_ = e.cfg.Audit.Append(f)
}

// eventSubscription maps an event to the project notification event that subscribes to it.
var eventSubscription = map[notify.EventType]string{
	notify.EventDeploySucceeded:  config.EventUpdateSucceeded,
	notify.EventDeployFailed:     config.EventUpdateFailed,
	notify.EventDeployRolled:     config.EventUpdateFailed,
	notify.EventRollbackFailed:   config.EventUpdateFailed,
	notify.EventUpdateAvailable:  config.EventUpdateAvailable,
	notify.EventRestoreSucceeded: config.EventRestoreSucceed,
	notify.EventRestoreFailed:    config.EventRestoreFailed,
}

// notify sends ev over the channels of the approved project, if it subscribes to the event. A project
// without notify settings sends nothing.
func (e *Engine) notify(p config.Project, ev notify.Event) {
	if e.cfg.Notifier == nil || p.Notify == nil || len(p.Notify.Channels) == 0 {
		return
	}
	want := eventSubscription[ev.Type]
	for _, sub := range p.Notify.Events {
		if sub == want {
			ev.Project = p.Name
			e.cfg.Notifier.Notify(ev, p.Notify.Channels...)
			return
		}
	}
}
