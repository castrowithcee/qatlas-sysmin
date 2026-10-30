package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/engine"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
	"github.com/castrowithcee/qatlas-sysmin/internal/source"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/gitrepo"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// maxBackupNameLength bounds the backup name of a restore request; the engine compares it with the list.
const maxBackupNameLength = 128

// newEngine wires the deploy engine to the service: its version sources, its executor and its planner's
// project list read what the service holds.
func (s *Service) newEngine() (*engine.Engine, error) {
	return engine.New(engine.Config{
		StateDir:     s.cfg.StateDir,
		Approvals:    s.approvals,
		Vault:        s.vault,
		Sources:      deploySources{s},
		Executor:     s.executor,
		Notifier:     s.notifier,
		Audit:        notifyAudit{s},
		Projects:     s.projectNames,
		PollInterval: s.pollInterval,
	})
}

func (s *Service) projectNames() []string {
	m, _, err := s.store.Load()
	if err != nil {
		slog.Warn("cannot load the configuration for the planner", "error", err.Error())
		return nil
	}
	names := make([]string, 0, len(m.Projects))
	for _, p := range m.Projects {
		names = append(names, p.Name)
	}
	return names
}

// pollInterval reads the default poll interval from sysmin.yaml when the planner needs it.
func (s *Service) pollInterval() time.Duration {
	settings, err := config.ReadSettings(s.cfg.ConfigDir)
	if err != nil {
		return config.DefaultPollInterval
	}
	return settings.EffectivePollInterval()
}

// deploySources adapts the source helpers of the service to the engine. Every call reads credentials only
// by the vault ids of the approved state and returns messages without secret values.
type deploySources struct{ s *Service }

func (d deploySources) checker(ap approval.Approved) (*source.Checker, *approvedCredentials) {
	creds := &approvedCredentials{vault: d.s.vault, ids: ap.Credentials}
	return &source.Checker{Credentials: creds, Registry: d.s.cfg.SourceRegistry, Git: d.s.cfg.SourceGit}, creds
}

func (d deploySources) CurrentImage(_ context.Context, ap approval.Approved, img config.Image) (string, error) {
	tag, problem := currentImageTag(ap.Project, img)
	if problem != nil {
		return "", errors.New(problem.Message)
	}
	return tag, nil
}

func (d deploySources) CurrentGit(ctx context.Context, ap approval.Approved) (string, error) {
	checker, _ := d.checker(ap)
	tag, problem := currentGitTag(ctx, checker, ap.Project)
	if problem != nil {
		return "", errors.New(problem.Message)
	}
	return tag, nil
}

func (d deploySources) CheckImage(ctx context.Context, ap approval.Approved, img config.Image, current string) (source.ImageResult, error) {
	checker, creds := d.checker(ap)
	res, err := checker.CheckImage(ctx, img, current)
	if err != nil {
		_, msg := classify(ctx, err, creds.secrets)
		return source.ImageResult{}, errors.New(msg)
	}
	return res, nil
}

func (d deploySources) CheckGit(ctx context.Context, ap approval.Approved, current string) (source.GitResult, error) {
	checker, creds := d.checker(ap)
	if ap.Project.Git == nil {
		return source.GitResult{}, errors.New("the project has no git source")
	}
	res, err := checker.CheckGit(ctx, *ap.Project.Git, current)
	if err != nil {
		_, msg := classify(ctx, err, creds.secrets)
		return source.GitResult{}, errors.New(msg)
	}
	return res, nil
}

func (d deploySources) ImageDigest(ctx context.Context, ap approval.Approved, img config.Image, tag string) (string, error) {
	repo, err := registry.ParseRepo(img.Ref)
	if err != nil {
		return "", errors.New("the image reference is not valid")
	}
	opts := d.s.cfg.SourceRegistry
	creds := &approvedCredentials{vault: d.s.vault, ids: ap.Credentials}
	if img.Credential != "" {
		if opts.Credential, err = creds.Registry(img.Credential); err != nil {
			_, msg := classify(ctx, err, creds.secrets)
			return "", errors.New(msg)
		}
	}
	digest, err := registry.New(opts).Digest(ctx, repo, tag)
	if err != nil {
		_, msg := classify(ctx, err, creds.secrets)
		return "", errors.New(msg)
	}
	return digest, nil
}

// executor builds the executor of one run: the runner, a redactor that knows every secret of the approved
// credentials, and the git login of the project's repository.
func (s *Service) executor(ap approval.Approved) (*execute.Executor, error) {
	red := &redact.Redactor{}
	creds := &approvedCredentials{vault: s.vault, ids: ap.Credentials}
	for _, id := range ap.Credentials {
		e, err := s.vault.Get(id)
		if err != nil {
			return nil, errors.New("an approved credential cannot be read")
		}
		for _, f := range e.Fields {
			red.Add(f.Reveal())
		}
	}
	var inner execute.Runner = &execute.ExecRunner{}
	if s.cfg.DeployRunner != nil {
		inner = s.cfg.DeployRunner
	}
	runner := &gitAuthRunner{inner: inner, opts: s.cfg.SourceGit}
	if g := ap.Project.Git; g != nil && g.Credential != "" {
		cred, err := creds.Git(g.Credential)
		if err != nil {
			return nil, errors.New("the git credential cannot be used")
		}
		red.Add(cred.Token, cred.PrivateKey)
		runner.url, runner.cred, runner.use = g.URL, cred, true
	}
	return &execute.Executor{Runner: runner, Redactor: red}, nil
}

// gitAuthRunner gives every git call the login of the project's approved credential. The files git needs
// for it live in a private temporary directory for the duration of one call only, so no secret outlives
// the command.
type gitAuthRunner struct {
	inner execute.Runner
	opts  gitrepo.Options
	use   bool
	url   string
	cred  gitrepo.Credential
}

func (r *gitAuthRunner) Run(ctx context.Context, c execute.Command) (execute.Result, error) {
	if !r.use || c.Name != "git" {
		return r.inner.Run(ctx, c)
	}
	dir, err := os.MkdirTemp("", "sysmin-*")
	if err != nil {
		return execute.Result{}, errors.New("cannot prepare the git credential")
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return execute.Result{}, errors.New("cannot prepare the git credential")
	}
	env, err := gitrepo.New(r.opts).AuthEnv(dir, r.url, r.cred)
	if err != nil {
		return execute.Result{}, fmt.Errorf("cannot prepare the git credential: %w", err)
	}
	c.Env = append(slices.Clone(c.Env), env...)
	return r.inner.Run(ctx, c)
}

func (s *Service) registerDeploy() error {
	for op, h := range map[string]daemon.Handler{
		control.OpDeployStatus:  {Action: auditlog.OperationDeployRead, Func: s.handleDeployStatus},
		control.OpDeployHistory: {Action: auditlog.OperationDeployRead, Func: s.handleDeployHistory},
		control.OpBackupsList:   {Action: auditlog.OperationDeployRead, Func: s.handleBackupsList},
		control.OpDeployRunNow:  {Action: auditlog.OperationDeployRun, Func: s.handleDeployRunNow},
		control.OpDeployPause:   {Action: auditlog.OperationDeployControl, Func: s.deployControl(s.engine.Pause)},
		control.OpDeployResume:  {Action: auditlog.OperationDeployControl, Func: s.deployControl(s.engine.Resume)},
		control.OpDeployPin:     {Action: auditlog.OperationDeployControl, Func: s.deployControl(s.engine.Pin)},
		control.OpDeployUnpin:   {Action: auditlog.OperationDeployControl, Func: s.deployControl(s.engine.Unpin)},
		control.OpDeployUpdate:  {Action: auditlog.OperationDeployRun, Mutating: true, Func: s.handleDeployUpdate},
		control.OpDeployRestore: {Action: auditlog.OperationDeployRestore, Mutating: true, Func: s.handleDeployRestore},
	} {
		if err := s.server.Register(op, h); err != nil {
			return err
		}
	}
	return nil
}

// deployCode maps an error of the engine to a fixed code; its text stays out of the answer.
func deployCode(err error) string {
	switch {
	case errors.Is(err, engine.ErrBusy):
		return control.CodeBusy
	case errors.Is(err, engine.ErrLocked), errors.Is(err, vault.ErrLocked), errors.Is(err, vault.ErrAbsent):
		return control.CodeLocked
	case errors.Is(err, engine.ErrPaused):
		return control.CodePaused
	case errors.Is(err, engine.ErrPinned):
		return control.CodePinned
	case errors.Is(err, engine.ErrVersion):
		return control.CodeVersionRefused
	case errors.Is(err, engine.ErrBackup):
		return control.CodeBackupUnknown
	case errors.Is(err, engine.ErrNoRestore):
		return control.CodeNoRestore
	case errors.Is(err, approval.ErrNotApproved), errors.Is(err, approval.ErrIntegrity):
		return control.CodeNotApproved
	}
	slog.Warn("a deploy operation failed", "error", err.Error())
	return control.CodeInternal
}

// deployRef decodes a project name and checks that the project exists, so state is kept only for projects
// of the configuration.
func (s *Service) deployRef(params json.RawMessage) (string, error) {
	ref, err := decodeParams(params, func(r control.DeployRef) string { return r.Project })
	if err != nil {
		return "", err
	}
	return ref.Project, s.existing(ref.Project)
}

func (s *Service) existing(project string) error {
	m, _, err := s.loadModel()
	if err != nil {
		return err
	}
	if _, ok := m.Project(project); !ok {
		return &control.Error{Code: control.CodeNotFound}
	}
	return nil
}

func deployRun(rec engine.RunRecord) *control.DeployRun {
	out := &control.DeployRun{
		ID: rec.ID, Action: rec.Action, Started: rec.Started, DurationMS: rec.Duration.Milliseconds(),
		Result: rec.Result, ErrorCode: rec.ErrorCode, FailedStep: rec.FailedStep, Error: rec.Error, Backup: rec.Backup,
	}
	for _, c := range rec.Changes {
		out.Changes = append(out.Changes, control.DeployChange{Kind: kindOf(c.Name), Name: c.Name, From: c.From, To: c.To})
	}
	return out
}

func kindOf(name string) string {
	if name == "" {
		return control.SourceGit
	}
	return control.SourceImage
}

func (s *Service) handleDeployStatus(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	project, err := s.deployRef(params)
	if err != nil {
		return nil, err
	}
	st, err := s.engine.Status(project)
	if err != nil {
		return nil, audited(c, project, &control.Error{Code: deployCode(err)})
	}
	out := control.DeployStatusResult{
		Project: project, Running: st.Running, Paused: st.Paused, Pinned: st.Pin != nil,
		Locked: s.vaultState() != control.VaultUnlocked, LastCheck: st.LastCheck, CheckError: st.CheckError,
		Sources: []control.DeploySource{},
	}
	if st.Fault != nil {
		out.Fault = &control.DeployFault{Time: st.Fault.Time, Run: st.Fault.Run, FailedStep: st.Fault.FailedStep, Error: st.Fault.Error}
	}
	for _, src := range st.Sources {
		out.Sources = append(out.Sources, control.DeploySource{Kind: kindOf(src.Name), Name: src.Name, Current: src.Current,
			Previous: src.Previous, Available: src.Available, Failed: src.Failed, Error: src.Error})
	}
	if n := len(st.History); n > 0 {
		out.LastRun = deployRun(st.History[n-1])
	}
	c.Audit(project, "")
	return out, nil
}

func (s *Service) handleDeployHistory(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	project, err := s.deployRef(params)
	if err != nil {
		return nil, err
	}
	history, err := s.engine.History(project)
	if err != nil {
		return nil, audited(c, project, &control.Error{Code: deployCode(err)})
	}
	out := control.DeployHistoryResult{Project: project, Runs: []control.DeployRun{}}
	for _, rec := range history {
		out.Runs = append(out.Runs, *deployRun(rec))
	}
	c.Audit(project, "")
	return out, nil
}

func (s *Service) handleBackupsList(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	project, err := s.deployRef(params)
	if err != nil {
		return nil, err
	}
	names, err := s.engine.Backups(project)
	if err != nil {
		return nil, audited(c, project, &control.Error{Code: deployCode(err)})
	}
	if names == nil {
		names = []string{}
	}
	c.Audit(project, "")
	return control.BackupsListResult{Project: project, Backups: names}, nil
}

// deployControl serves a state change of a project that needs no admin session: it holds a project back
// or lets it go on under its approved rules and changes no version or file of the host.
func (s *Service) deployControl(fn func(string) error) daemon.HandlerFunc {
	return func(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
		project, err := s.deployRef(params)
		if err != nil {
			return nil, err
		}
		if err := fn(project); err != nil {
			return nil, audited(c, project, &control.Error{Code: deployCode(err)})
		}
		c.Audit(project, "")
		return nil, nil
	}
}

func (s *Service) opsCtx() context.Context {
	if s.base != nil {
		return s.base
	}
	return context.Background()
}

// await runs fn in the background, on the context of the service so a client that gives up does not stop
// it, and reports whether it finished within the wait. The service waits for it before it shuts down.
func (s *Service) await(ctx context.Context, fn func(context.Context)) bool {
	done := make(chan struct{})
	s.ops.Add(1)
	go func() {
		defer s.ops.Done()
		defer close(done)
		fn(s.opsCtx())
	}()
	wait := s.cfg.DeployWait
	if wait <= 0 {
		wait = control.DeployWait
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
	case <-ctx.Done():
	}
	return false
}

func (s *Service) handleDeployRunNow(ctx context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	project, err := s.deployRef(params)
	if err != nil {
		return nil, err
	}
	var (
		out    engine.Outcome
		runErr error
	)
	finished := s.await(ctx, func(opCtx context.Context) { out, runErr = s.engine.RunNow(opCtx, project) })
	if !finished {
		c.Audit(project, "")
		return control.DeployRunNowResult{Project: project, Pending: true}, nil
	}
	if runErr != nil {
		return nil, audited(c, project, &control.Error{Code: deployCode(runErr)})
	}
	res := control.DeployRunNowResult{Project: project, Skipped: out.Skipped}
	code := ""
	if out.Run != nil {
		res.Run = deployRun(*out.Run)
		code = out.Run.ErrorCode
	}
	c.Audit(project, code)
	return res, nil
}

func (s *Service) handleDeployRestore(ctx context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	var p control.DeployRestoreParams
	if err := json.Unmarshal(params, &p); err != nil || !config.ValidName(p.Project) || p.Backup == "" || len(p.Backup) > maxBackupNameLength {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	if err := s.existing(p.Project); err != nil {
		return nil, audited(c, p.Project, err)
	}
	var (
		rec    engine.RunRecord
		runErr error
	)
	finished := s.await(ctx, func(opCtx context.Context) { rec, runErr = s.engine.Restore(opCtx, p.Project, p.Backup) })
	if !finished {
		c.Audit(p.Project, "")
		return control.DeployRestoreResult{Project: p.Project, Pending: true}, nil
	}
	if runErr != nil {
		return nil, audited(c, p.Project, &control.Error{Code: deployCode(runErr)})
	}
	c.Audit(p.Project, rec.ErrorCode)
	return control.DeployRestoreResult{Project: p.Project, Run: deployRun(rec)}, nil
}

// updateRequest is a validated OpDeployUpdate request: the versions per project, keyed by image name and
// the empty name for git.
type updateRequest struct {
	project  string
	versions map[string]string
}

func parseUpdate(params json.RawMessage) ([]updateRequest, error) {
	var p control.DeployUpdateParams
	if err := json.Unmarshal(params, &p); err != nil || len(p.Updates) == 0 || len(p.Updates) > control.MaxUpdateProjects {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	var reqs []updateRequest
	seen := map[string]bool{}
	for _, u := range p.Updates {
		if !config.ValidName(u.Project) || seen[u.Project] || len(u.Versions) == 0 {
			return nil, &control.Error{Code: control.CodeBadRequest}
		}
		seen[u.Project] = true
		r := updateRequest{project: u.Project, versions: map[string]string{}}
		for _, v := range u.Versions {
			_, dup := r.versions[v.Name]
			bad := v.Version == "" || len(v.Version) > maxCurrentLength || dup
			switch v.Kind {
			case control.SourceImage:
				bad = bad || v.Name == ""
			case control.SourceGit:
				bad = bad || v.Name != ""
			default:
				bad = true
			}
			if bad {
				return nil, &control.Error{Code: control.CodeBadRequest}
			}
			r.versions[v.Name] = v.Version
		}
		reqs = append(reqs, r)
	}
	return reqs, nil
}

// updateProgress collects the outcomes of the projects of one request as they finish.
type updateProgress struct {
	mu      sync.Mutex
	results []control.ProjectUpdateResult
}

func (u *updateProgress) add(r control.ProjectUpdateResult) {
	u.mu.Lock()
	u.results = append(u.results, r)
	u.mu.Unlock()
}

// handleDeployUpdate updates the projects one after the other. Each project has its own outcome; a project
// that is not finished when the wait ends is reported as pending and goes on in the background.
func (s *Service) handleDeployUpdate(ctx context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	reqs, err := parseUpdate(params)
	if err != nil {
		return nil, err
	}
	var progress updateProgress
	s.await(ctx, func(opCtx context.Context) {
		for _, r := range reqs {
			res := control.ProjectUpdateResult{Project: r.project}
			if err := s.existing(r.project); err != nil {
				res.Error = codeOfErr(err)
			} else if rec, err := s.engine.Update(opCtx, r.project, r.versions); err != nil {
				res.Error = deployCode(err)
			} else {
				res.Run = deployRun(rec)
			}
			progress.add(res)
		}
	})
	progress.mu.Lock()
	out := control.DeployUpdateResult{Results: slices.Clone(progress.results)}
	progress.mu.Unlock()
	done := map[string]bool{}
	for _, r := range out.Results {
		done[r.Project] = true
	}
	for _, r := range reqs {
		if !done[r.project] {
			out.Results = append(out.Results, control.ProjectUpdateResult{Project: r.project, Pending: true})
		}
	}
	for _, r := range out.Results {
		code := r.Error
		if r.Run != nil {
			code = r.Run.ErrorCode
		}
		c.Audit(r.Project, code)
	}
	return out, nil
}
