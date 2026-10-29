package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

// The templates a new project starts from when the request names none, and the example version a check
// resolves with.
const (
	defaultImageTemplate = "image-tag"
	defaultGitTemplate   = "git-tag"
	checkVersion         = "1.0.0"
	checkPrevious        = "0.9.0"
)

// codeInvalidFile marks a project file that cannot be read as a project.
const codeInvalidFile = "invalid-file"

func (s *Service) registerProjects() error {
	for op, h := range map[string]daemon.Handler{
		control.OpProjectsList:   {Action: auditlog.OperationConfigRead, Func: s.handleProjectsList},
		control.OpProjectsShow:   {Action: auditlog.OperationConfigRead, Func: s.handleProjectsShow},
		control.OpProjectsCheck:  {Action: auditlog.OperationConfigRead, Func: s.handleProjectsCheck},
		control.OpProjectsAdd:    {Action: auditlog.OperationConfigApply, Func: s.handleProjectsAdd},
		control.OpProjectsEdit:   {Action: auditlog.OperationConfigApply, Func: s.handleProjectsEdit},
		control.OpProjectsRemove: {Action: auditlog.OperationConfigApply, Func: s.handleProjectsRemove},
	} {
		if err := s.server.Register(op, h); err != nil {
			return err
		}
	}
	return nil
}

func decodeParams[T any](params json.RawMessage, name func(T) string) (T, error) {
	var p T
	if err := json.Unmarshal(params, &p); err != nil || !config.ValidName(name(p)) {
		return p, &control.Error{Code: control.CodeBadRequest}
	}
	return p, nil
}

func refOf(params json.RawMessage) (control.ProjectRef, error) {
	return decodeParams(params, func(r control.ProjectRef) string { return r.Name })
}

func internalError(what string, err error) error {
	slog.Warn(what, "error", err.Error())
	return &control.Error{Code: control.CodeInternal}
}

// pending returns the projects with an open approval, or nil while the vault is locked.
func (s *Service) pending() map[string]bool {
	if s.vaultState() != control.VaultUnlocked {
		return nil
	}
	items, err := s.approvals.Open()
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, it := range items {
		out[it.Project] = true
	}
	return out
}

func summaryOfProject(p config.Project, open map[string]bool) control.ProjectSummary {
	sum := control.ProjectSummary{Name: p.Name, Template: p.Template, Dir: p.Dir, State: control.StateUnknown}
	switch {
	case p.Git != nil:
		sum.Source = "git"
	case len(p.Images) > 0:
		sum.Source = "images"
	}
	if open != nil {
		sum.State = control.StateApproved
		if open[p.Name] {
			sum.State = control.StatePending
		}
	}
	return sum
}

func (s *Service) loadModel() (*config.Model, config.Revisions, error) {
	m, revs, err := s.store.Load()
	if err != nil {
		return nil, nil, internalError("cannot load the configuration", err)
	}
	return m, revs, nil
}

func (s *Service) handleProjectsList(context.Context, *daemon.Conn, json.RawMessage) (any, error) {
	m, _, err := s.loadModel()
	if err != nil {
		return nil, err
	}
	open := s.pending()
	out := control.ProjectsListResult{Projects: make([]control.ProjectSummary, 0, len(m.Projects))}
	for _, p := range m.Projects {
		out.Projects = append(out.Projects, summaryOfProject(p, open))
	}
	sort.Slice(out.Projects, func(i, j int) bool { return out.Projects[i].Name < out.Projects[j].Name })
	return out, nil
}

func (s *Service) handleProjectsShow(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	ref, err := refOf(params)
	if err != nil {
		return nil, err
	}
	m, revs, err := s.loadModel()
	if err != nil {
		return nil, err
	}
	p, ok := m.Project(ref.Name)
	if !ok {
		return nil, &control.Error{Code: control.CodeNotFound}
	}
	data, err := os.ReadFile(filepath.Join(s.cfg.ConfigDir, filepath.FromSlash(config.ProjectPath(p.Name))))
	if err != nil {
		return nil, internalError("cannot read a project file", err)
	}
	return control.ProjectDetail{
		ProjectSummary: summaryOfProject(*p, s.pending()),
		Revision:       string(revs[config.ProjectPath(p.Name)]),
		File:           string(data),
	}, nil
}

// inProject picks the problems that belong to one project's file.
func inProject(name string) func(config.Problem) bool {
	return func(p config.Problem) bool { return p.Project == name }
}

func (s *Service) handleProjectsCheck(_ context.Context, _ *daemon.Conn, params json.RawMessage) (any, error) {
	ref, err := refOf(params)
	if err != nil {
		return nil, err
	}
	if s.vaultState() != control.VaultUnlocked {
		// The credential names the validation compares with live in the vault.
		return nil, &control.Error{Code: control.CodeLocked}
	}
	m, _, err := s.loadModel()
	if err != nil {
		return nil, err
	}
	p, ok := m.Project(ref.Name)
	if !ok {
		return nil, &control.Error{Code: control.CodeNotFound}
	}
	infos, err := s.vault.List()
	if err != nil {
		return nil, approvalError(err)
	}
	names := make([]string, len(infos))
	for i, in := range infos {
		names[i] = in.Name
	}
	var found []config.Problem
	for _, pr := range config.Validate(m, names) {
		if pr.Project == ref.Name {
			found = append(found, pr)
		}
	}
	if len(found) > 0 {
		return nil, &control.Error{Code: control.CodeInvalid, Problems: problemsOf(found)}
	}
	version := ref.Version
	if version == "" {
		version = checkVersion
	}
	var targets []config.Target
	if p.Git != nil {
		targets = []config.Target{{Version: version, PreviousVersion: checkPrevious}}
	}
	for _, im := range p.Images {
		targets = append(targets, config.Target{Name: im.Name, Version: version, PreviousVersion: checkPrevious})
	}
	plan, err := m.Resolve(ref.Name, targets)
	if err != nil {
		var pe *config.ProblemsError
		if errors.As(err, &pe) {
			return nil, &control.Error{Code: control.CodeInvalid, Problems: problemsOf(pe.Problems)}
		}
		return nil, internalError("cannot resolve a project", err)
	}
	out := control.CheckResult{Project: ref.Name, Version: version, Steps: make([]control.CheckStep, 0, len(plan.Steps))}
	for _, st := range plan.Steps {
		out.Steps = append(out.Steps, control.CheckStep{
			Type: string(st.Type), Timeout: time.Duration(st.Timeout).String(), Free: st.IsFree(), Detail: stepDetail(st),
		})
	}
	return out, nil
}

// stepDetail lists what a resolved step would run with, as key=value words. Values with spaces are quoted.
func stepDetail(st config.ResolvedStep) string {
	var parts []string
	add := func(key, v string) {
		if v != "" {
			parts = append(parts, key+"="+quote(v))
		}
	}
	add("dir", st.Dir)
	add("env_file", st.EnvFile)
	add("compose_files", strings.Join(st.ComposeFiles, ","))
	add("compose_project", st.ComposeProject)
	add("services", strings.Join(st.Services, ","))
	for _, a := range st.Assignments {
		add("assign", a.Key+"="+a.Value)
	}
	add("tag", st.Tag)
	add("containers", strings.Join(st.Containers, ","))
	add("url", st.URL)
	if st.Status != 0 {
		add("status", fmt.Sprint(st.Status))
	}
	add("contains", st.Contains)
	if b := st.Backup; b != nil {
		add("backup_command", b.Command)
		add("backup_volume", b.Volume)
		if b.Keep != 0 {
			add("keep", fmt.Sprint(b.Keep))
		}
		add("restore_command", b.RestoreCommand)
	}
	add("run", st.Shell)
	return strings.Join(parts, " ")
}

func quote(v string) string {
	if strings.ContainsAny(v, " \t\"") {
		return fmt.Sprintf("%q", v)
	}
	return v
}

func (s *Service) handleProjectsAdd(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	p, err := decodeParams(params, func(p control.ProjectParams) string { return p.Name })
	if err != nil {
		return nil, err
	}
	if p.File != "" && p.HasFields() {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	return s.applyChange(c, configChange{
		target: p.Name, inScope: inProject(p.Name),
		prepare: func(m *config.Model, revs config.Revisions) (*config.Model, func() (config.Revision, error), error) {
			if _, ok := m.Project(p.Name); ok {
				return nil, nil, &control.Error{Code: control.CodeExists}
			}
			np, err := buildProject(m, config.Project{Schema: config.CurrentSchema, Name: p.Name}, p, true)
			if err != nil {
				return nil, nil, err
			}
			after := *m
			after.Projects = append(append([]config.Project(nil), m.Projects...), np)
			rev := revs[config.ProjectPath(p.Name)]
			return &after, func() (config.Revision, error) { return s.store.SaveProject(np, rev) }, nil
		},
		approve: func(w config.Revision) error { return s.approvals.ApproveCurrent(p.Name, w) },
	})
}

func (s *Service) handleProjectsEdit(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	p, err := decodeParams(params, func(p control.ProjectParams) string { return p.Name })
	if err != nil {
		return nil, err
	}
	if p.File != "" && p.HasFields() {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	return s.applyChange(c, configChange{
		target: p.Name, inScope: inProject(p.Name),
		prepare: func(m *config.Model, revs config.Revisions) (*config.Model, func() (config.Revision, error), error) {
			cur, rev, err := existing(m, revs, p.Name, p.Revision)
			if err != nil {
				return nil, nil, err
			}
			np, err := buildProject(m, *cur, p, false)
			if err != nil {
				return nil, nil, err
			}
			after := *m
			after.Projects = append([]config.Project(nil), m.Projects...)
			for i := range after.Projects {
				if after.Projects[i].Name == p.Name {
					after.Projects[i] = np
				}
			}
			return &after, func() (config.Revision, error) { return s.store.SaveProject(np, rev) }, nil
		},
		approve: func(w config.Revision) error { return s.approvals.ApproveCurrent(p.Name, w) },
	})
}

func (s *Service) handleProjectsRemove(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	ref, err := refOf(params)
	if err != nil {
		return nil, err
	}
	return s.applyChange(c, configChange{
		target: ref.Name, inScope: inProject(ref.Name),
		prepare: func(m *config.Model, revs config.Revisions) (*config.Model, func() (config.Revision, error), error) {
			_, rev, err := existing(m, revs, ref.Name, ref.Revision)
			if err != nil {
				return nil, nil, err
			}
			return nil, func() (config.Revision, error) {
				return config.RevisionAbsent, s.store.DeleteProject(ref.Name, rev)
			}, nil
		},
		approve: func(w config.Revision) error { return s.approvals.ApproveCurrent(ref.Name, w) },
	})
}

// existing returns the stored project and its revision, or CodeNotFound. A non-empty want that differs from
// the stored revision is CodeChanged.
func existing(m *config.Model, revs config.Revisions, name, want string) (*config.Project, config.Revision, error) {
	cur, ok := m.Project(name)
	if !ok {
		return nil, "", &control.Error{Code: control.CodeNotFound}
	}
	rev := revs[config.ProjectPath(name)]
	if want != "" && config.Revision(want) != rev {
		return nil, "", &control.Error{Code: control.CodeChanged}
	}
	return cur, rev, nil
}

// buildProject returns base with the request applied, or a CodeInvalid error for a request that cannot be
// applied at all. What is merely wrong in the result is left to the validation.
func buildProject(m *config.Model, base config.Project, p control.ProjectParams, isNew bool) (config.Project, error) {
	if p.File != "" {
		return projectFromFile(p)
	}
	var probs []control.Problem
	bad := func(field, code, format string, args ...any) {
		probs = append(probs, control.Problem{Field: field, Code: code, Message: fmt.Sprintf(format, args...)})
	}
	out := base
	switch {
	case p.Template != "":
		out.Template = p.Template
	case isNew && out.Template == "":
		out.Template = defaultImageTemplate
		if p.GitURL != "" {
			out.Template = defaultGitTemplate
		}
	}
	setIf := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	setIf(&out.Dir, p.Dir)
	setIf(&out.EnvFile, p.EnvFile)
	setIf(&out.ComposeProject, p.ComposeProject)
	if len(p.ComposeFiles) > 0 {
		out.ComposeFiles = p.ComposeFiles
	}
	if p.PollInterval != "" {
		d, err := time.ParseDuration(p.PollInterval)
		if err != nil {
			bad("poll_interval", config.CodeInvalidDuration, "poll interval %q is not a duration; use a form such as \"30m\" or \"1h\"", p.PollInterval)
		}
		out.PollInterval = config.Duration(d)
	}
	if len(p.Values) > 0 || len(p.UnsetValues) > 0 {
		values := make(map[string]string, len(out.Values)+len(p.Values))
		for k, v := range out.Values {
			values[k] = v
		}
		for _, k := range p.UnsetValues {
			if _, ok := values[k]; !ok {
				bad("values."+k, config.CodeInvalidValue, "value %q does not exist and cannot be removed", k)
			}
			delete(values, k)
		}
		for k, v := range p.Values {
			values[k] = v
		}
		out.Values = values
		if len(values) == 0 {
			out.Values = nil
		}
	}
	out.Images = append([]config.Image(nil), out.Images...)
	for _, name := range p.RemoveImages {
		i := imageIndex(out.Images, name)
		if i < 0 {
			bad("images", config.CodeInvalidValue, "image %q does not exist and cannot be removed", name)
			continue
		}
		out.Images = append(out.Images[:i], out.Images[i+1:]...)
	}
	for _, im := range p.Images {
		i := imageIndex(out.Images, im.Name)
		if i < 0 {
			out.Images = append(out.Images, config.Image{Name: im.Name})
			i = len(out.Images) - 1
		}
		img := &out.Images[i]
		setIf(&img.Ref, im.Ref)
		setIf(&img.EnvVar, im.EnvVar)
		setIf(&img.Credential, im.Credential)
		if len(im.Services) > 0 {
			img.Services = im.Services
		}
	}
	if len(out.Images) == 0 {
		out.Images = nil
	}
	if p.GitURL != "" || p.GitCredential != "" {
		g := config.GitSource{}
		if out.Git != nil {
			g = *out.Git
		}
		setIf(&g.URL, p.GitURL)
		setIf(&g.Credential, p.GitCredential)
		out.Git = &g
	}
	if len(probs) > 0 {
		for i := range probs {
			probs[i].Hint = "Correct the request."
		}
		return out, &control.Error{Code: control.CodeInvalid, Problems: probs}
	}
	return out, nil
}

func imageIndex(images []config.Image, name string) int {
	for i, im := range images {
		if im.Name == name {
			return i
		}
	}
	return -1
}

// projectFromFile reads the project file of a request. The name in the file must be the requested one; a file
// without a name takes it.
func projectFromFile(p control.ProjectParams) (config.Project, error) {
	invalid := func(field, code, msg, hint string) error {
		return &control.Error{Code: control.CodeInvalid, Problems: []control.Problem{{Field: field, Code: code, Message: msg, Hint: hint}}}
	}
	np, err := config.DecodeProject([]byte(p.File))
	if err != nil {
		return np, invalid("", codeInvalidFile, err.Error(), "Pass a project file with schema: 1 and only known fields.")
	}
	if np.Name == "" {
		np.Name = p.Name
	}
	if np.Name != p.Name {
		return np, invalid("name", config.CodeInvalidName, fmt.Sprintf("the file names the project %q, not %q", np.Name, p.Name), "Make the name in the file match the project name.")
	}
	return np, nil
}
