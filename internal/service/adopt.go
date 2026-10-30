package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/castrowithcee/qatlas-sysmin/internal/adopt"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

const (
	// adoptMaxOutput leaves room for the whole output of docker compose config.
	adoptMaxOutput = 1 << 20
	maxAdoptDirLen = 4096
)

// Problem codes of the service itself; the others come from package adopt.
const (
	codeAlreadyAdopted = "already-adopted"
	codeNameTaken      = "name-taken"
)

// blockingCodes are the findings that keep a proposal from being stored: without the fact they name, the
// project would be wrong or incomplete. Everything the validation rejects blocks as well.
var blockingCodes = map[string]bool{
	codeAlreadyAdopted:          true,
	codeNameTaken:               true,
	adopt.CodeNoContainers:      true,
	adopt.CodeInvalidName:       true,
	adopt.CodeNoTrackedImage:    true,
	adopt.CodeComposeConfig:     true,
	adopt.CodeConflictingLabels: true,
	adopt.CodeInvalidLabel:      true,
	adopt.CodeNoComposeFiles:    true,
	adopt.CodeGitOrigin:         true,
	adopt.CodeNotReadable:       true,
}

func (s *Service) registerAdopt() error {
	return s.server.Register(control.OpProjectsAdopt, daemon.Handler{Action: auditlog.OperationConfigApply, Func: s.handleProjectsAdopt})
}

// adoptDir accepts an absolute path without control characters and returns it cleaned.
func adoptDir(dir string) (string, bool) {
	if dir == "" || len(dir) > maxAdoptDirLen || !filepath.IsAbs(dir) {
		return "", false
	}
	for _, r := range dir {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return filepath.Clean(dir), true
}

// failDir answers a directory the service cannot use: not-found when it is missing, no-access with the
// first denied part of the requested path when the service user may not pass it. The audit names no path.
func failDir(c *daemon.Conn, name string, err error) (any, error) {
	var access *adopt.AccessError
	if errors.As(err, &access) {
		c.Audit(name, control.CodeNoAccess)
		return nil, &control.Error{Code: control.CodeNoAccess, Paths: []string{access.Path}}
	}
	c.Audit(name, control.CodeNotFound)
	return nil, &control.Error{Code: control.CodeNotFound}
}

func (s *Service) handleProjectsAdopt(ctx context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	var p control.AdoptParams
	if err := json.Unmarshal(params, &p); err != nil || (p.Name != "" && !config.ValidName(p.Name)) {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	dir, ok := adoptDir(p.Dir)
	if !ok {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	// fail records the refusal under the project name, never under the path.
	fail := func(code string) (any, error) {
		c.Audit(p.Name, code)
		return nil, &control.Error{Code: code}
	}
	if s.vaultState() != control.VaultUnlocked {
		return fail(control.CodeLocked)
	}
	if err := adopt.CheckDir(dir); err != nil {
		return failDir(c, p.Name, err)
	}

	ictx, cancel := context.WithTimeout(ctx, control.AdoptTimeout)
	defer cancel()
	prop, err := adopt.Inspect(ictx, adopt.Options{Runner: s.cfg.Runner, Git: s.cfg.SourceGit}, dir, p.Name)
	if err != nil {
		var access *adopt.AccessError
		if errors.As(err, &access) || errors.Is(err, adopt.ErrNotDirectory) {
			return failDir(c, p.Name, err)
		}
		slog.Warn("cannot inspect a stack", "error", err.Error())
		if errors.Is(ictx.Err(), context.DeadlineExceeded) {
			return fail(control.CodeTimeout)
		}
		return fail(control.CodeDockerUnavailable)
	}
	m, _, err := s.store.Load()
	if err != nil {
		c.Audit(prop.Project.Name, control.CodeInternal)
		return nil, internalError("cannot load the configuration", err)
	}

	name := prop.Project.Name
	problems := make([]control.AdoptProblem, 0, len(prop.Problems)+2)
	for _, pr := range prop.Problems {
		problems = append(problems, control.AdoptProblem{Code: pr.Code, Message: pr.Message, Hint: pr.Hint, Blocking: blockingCodes[pr.Code]})
	}
	blocked := adoptTaken(m, dir, name, &problems)
	for _, pr := range problems {
		blocked = blocked || pr.Blocking
	}
	if !blocked {
		// The validation sees what the stored project would be; a rejection blocks like any other finding.
		found, err := s.validateNew(m, prop.Project)
		if err != nil {
			c.Audit(name, codeOfErr(err))
			return nil, err
		}
		for _, f := range found {
			problems = append(problems, control.AdoptProblem{Code: f.Code, Message: f.Message, Hint: f.Hint, Blocking: true})
			blocked = true
		}
	}

	res := control.AdoptResult{Project: adoptProject(prop), Problems: problems}
	if data, err := config.EncodeProject(prop.Project); err == nil {
		res.File = string(data)
	}
	if p.DryRun || blocked {
		c.Audit(name, "")
		return res, nil
	}

	np := prop.Project
	change, err := s.applyChange(c, configChange{
		target: name, inScope: inProject(name),
		prepare: func(m *config.Model, revs config.Revisions) (*config.Model, func() (config.Revision, error), error) {
			// The configuration may have changed while the stack was inspected.
			var again []control.AdoptProblem
			if adoptTaken(m, dir, name, &again) {
				return nil, nil, &control.Error{Code: control.CodeExists}
			}
			after := *m
			after.Projects = append(append([]config.Project(nil), m.Projects...), np)
			rev := revs[config.ProjectPath(name)]
			return &after, func() (config.Revision, error) { return s.store.SaveProject(np, rev) }, nil
		},
		approve: func(w config.Revision) error { return s.approvals.ApproveCurrent(name, w) },
	})
	if err != nil {
		return nil, err
	}
	res.Saved, res.State, res.Revision = true, change.State, change.Revision
	return res, nil
}

// adoptTaken adds the problems of a directory or a name that the configuration uses already and reports
// whether there are any.
func adoptTaken(m *config.Model, dir, name string, problems *[]control.AdoptProblem) bool {
	taken := false
	owner, adopted := adopt.Adopted(m, dir)
	if adopted {
		taken = true
		*problems = append(*problems, control.AdoptProblem{
			Code: codeAlreadyAdopted, Blocking: true,
			Message: "this directory is managed by the project " + owner + " already",
			Hint:    "Use sysmin project show " + owner + " to see it, or remove that project before adopting the stack again.",
		})
	}
	if _, exists := m.Project(name); exists && name != "" && !(adopted && owner == name) {
		taken = true
		*problems = append(*problems, control.AdoptProblem{
			Code: codeNameTaken, Blocking: true,
			Message: "a project named " + name + " exists already",
			Hint:    "Choose another name with --name.",
		})
	}
	return taken
}

// validateNew returns the findings of the validation about project np added to m.
func (s *Service) validateNew(m *config.Model, np config.Project) ([]config.Problem, error) {
	infos, err := s.vault.List()
	if err != nil {
		return nil, approvalError(err)
	}
	names := make([]string, len(infos))
	for i, in := range infos {
		names[i] = in.Name
	}
	after := *m
	after.Projects = append(append([]config.Project(nil), m.Projects...), np)
	var found []config.Problem
	for _, pr := range config.Validate(&after, names) {
		if inProject(np.Name)(pr) {
			found = append(found, pr)
		}
	}
	return found, nil
}

// adoptProject converts a proposal into its wire form. The git address loses any user information.
func adoptProject(prop adopt.Proposal) control.AdoptProject {
	pr := prop.Project
	out := control.AdoptProject{
		Name: pr.Name, Dir: pr.Dir, ComposeProject: pr.ComposeProject, ComposeFiles: pr.ComposeFiles,
		EnvFile: pr.EnvFile, Template: pr.Template,
	}
	for _, im := range pr.Images {
		out.Images = append(out.Images, control.AdoptImage{
			Name: im.Name, Ref: im.Ref, EnvVar: im.EnvVar, Services: im.Services, Current: prop.Versions[im.Name],
		})
	}
	if g := pr.Git; g != nil {
		out.Git = &control.AdoptGit{URL: stripUserinfo(g.URL), Current: prop.Versions["git"]}
	}
	return out
}

func stripUserinfo(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		u.User = nil
		return u.String()
	}
	// scp-like address user@host:path
	if i := strings.IndexByte(raw, '@'); i >= 0 && !strings.Contains(raw[:i], "/") {
		return raw[i+1:]
	}
	return raw
}
