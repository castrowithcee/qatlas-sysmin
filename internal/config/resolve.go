package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Target is the version change of one image in a run. For a git source the run has exactly one target with
// an empty Name.
type Target struct {
	Name            string
	Version         string
	PreviousVersion string
}

// Plan is the concrete step sequence of one run, used for the dry run and for execution.
type Plan struct {
	Project string
	Steps   []ResolvedStep
}

// Resolve turns a project and its target versions into concrete steps.
//
// Several images of a project are updated together in one run. Placeholders are filled as follows:
//   - A step that uses {version}, {previous_version} or {var} is repeated once per image of the run, in the
//     order of the project's images. In set-env this applies per assignment, so one set-env step carries one
//     assignment per image.
//   - {services} is the union of the services of the images in the run, without repeats, in image order.
//     Compose steps therefore name every service of the run once. For a git source it is empty, which
//     compose treats as all services.
//   - {dir}, {env_file} and the project values are the same everywhere.
//
// Resolve also applies the static step rules. Credentials are not checked here; use Validate for that.
func (m *Model) Resolve(project string, targets []Target) (Plan, error) {
	p, ok := m.Project(project)
	if !ok {
		return Plan{}, fmt.Errorf("project %q does not exist", project)
	}
	var probs []Problem
	sc := scope{out: &probs, file: ProjectPath(p.Name), project: p.Name}
	validateProjectSteps(sc, *p)
	if len(p.Steps) == 0 && p.Template != "" {
		if t, ok := m.Template(p.Template); ok {
			tsc := scope{out: &probs, file: TemplatePath(t.Name), template: t.Name}
			for i, s := range t.Steps {
				validateStep(tsc, s, fmt.Sprintf("steps[%d]", i), true)
			}
		}
	}
	steps, ok := resolveProject(sc, m, *p, targets)
	probs = dedupe(probs)
	if len(probs) > 0 || !ok {
		return Plan{}, &ProblemsError{Problems: probs}
	}
	return Plan{Project: p.Name, Steps: steps}, nil
}

func sampleTargets(p Project) []Target {
	if p.Git != nil {
		return []Target{{Version: "1.0.0", PreviousVersion: "0.9.0"}}
	}
	var ts []Target
	for _, im := range p.Images {
		ts = append(ts, Target{Name: im.Name, Version: "1.0.0", PreviousVersion: "0.9.0"})
	}
	return ts
}

// image is one image of the run with its resolved values.
type image struct {
	version, previous, envVar string
	services                  []string
}

type run struct {
	p        Project
	images   []image
	services []string
}

func resolveProject(sc scope, m *Model, p Project, targets []Target) ([]ResolvedStep, bool) {
	c, ok := newRun(sc, p, targets)
	if !ok {
		return nil, false
	}
	steps, prefix := p.Steps, "steps"
	if len(steps) == 0 {
		t, ok := m.Template(p.Template)
		if !ok {
			sc.add("template", CodeUnknownTemplate, "template %q does not exist", p.Template)
			return nil, false
		}
		steps, prefix = t.Steps, "template.steps"
	}
	var out []ResolvedStep
	for i, s := range steps {
		out = append(out, c.resolveStep(sc, s, fmt.Sprintf("%s[%d]", prefix, i))...)
	}
	return out, true
}

func newRun(sc scope, p Project, targets []Target) (*run, bool) {
	c := &run{p: p}
	ok := true
	bad := func(format string, args ...any) {
		sc.add("", CodeInvalidTarget, format, args...)
		ok = false
	}
	checkVersions := func(t Target) {
		if !validVersion(t.Version) {
			bad("version %q of %q is not a valid version", t.Version, t.Name)
		}
		if !validVersion(t.PreviousVersion) {
			bad("previous version %q of %q is not a valid version", t.PreviousVersion, t.Name)
		}
	}
	if p.Git != nil {
		if len(targets) != 1 || targets[0].Name != "" {
			bad("a git source takes exactly one target without a name")
			return c, false
		}
		checkVersions(targets[0])
		c.images = []image{{version: targets[0].Version, previous: targets[0].PreviousVersion}}
		return c, ok
	}
	byName := map[string]Target{}
	for _, t := range targets {
		if _, dup := byName[t.Name]; dup {
			bad("image %q appears more than once in the targets", t.Name)
			continue
		}
		byName[t.Name] = t
		known := false
		for _, im := range p.Images {
			known = known || im.Name == t.Name
		}
		if !known {
			bad("image %q is not part of the project", t.Name)
			continue
		}
		checkVersions(t)
	}
	if len(targets) == 0 {
		bad("a run needs at least one image target")
	}
	seen := map[string]bool{}
	for _, im := range p.Images {
		t, in := byName[im.Name]
		if !in {
			continue
		}
		c.images = append(c.images, image{version: t.Version, previous: t.PreviousVersion, envVar: im.EnvVar, services: im.Services})
		for _, s := range im.Services {
			if !seen[s] {
				seen[s] = true
				c.services = append(c.services, s)
			}
		}
	}
	return c, ok
}

func validVersion(v string) bool {
	return versionRe.MatchString(v) && !strings.Contains(v, "..")
}

// lookup returns the placeholder lookup for one image of the run; img is nil outside per-image steps.
func (c *run) lookup(img *image) lookupFunc {
	return func(name string) (value, *phError) {
		switch name {
		case PhDir:
			return value{scalar: c.p.Dir}, nil
		case PhEnvFile:
			if c.p.EnvFile == "" {
				return value{}, &phError{CodeMissingEnvFile, "{env_file} is used but the project sets no env_file"}
			}
			return value{scalar: c.p.EnvFile}, nil
		case PhServices:
			return value{list: c.services, isList: true}, nil
		case PhVersion:
			return value{scalar: img.version}, nil
		case PhPreviousVersion:
			return value{scalar: img.previous}, nil
		case PhVar:
			if c.p.Git != nil {
				return value{}, &phError{CodeUnknownPlaceholder, "{var} is not available for a git source"}
			}
			if img.envVar == "" {
				return value{}, &phError{CodeMissingEnvVar, "{var} is used but an image of the run has no env_var"}
			}
			return value{scalar: img.envVar}, nil
		}
		if v, ok := c.p.Values[name]; ok {
			return value{scalar: v}, nil
		}
		return value{}, &phError{CodeUnknownPlaceholder, fmt.Sprintf("placeholder {%s} is unknown", name)}
	}
}

func usesPerImage(texts ...string) bool {
	for _, t := range texts {
		for _, n := range textNames(t) {
			if isPerImage(n) {
				return true
			}
		}
	}
	return false
}

// resolveStep expands bare backup and check steps to the project's configuration, then resolves.
func (c *run) resolveStep(sc scope, s Step, field string) []ResolvedStep {
	inherit := func(spec Step, f string) []ResolvedStep {
		if spec.Timeout == 0 {
			spec.Timeout = s.Timeout
		}
		return c.resolveConcrete(sc, spec, f)
	}
	switch {
	case s.Type == StepBackup && s.bare():
		if c.p.Backup == nil {
			return nil
		}
		spec := *c.p.Backup
		spec.Type = StepBackup
		return inherit(spec, "backup")
	case (s.Type == StepCheckContainer || s.Type == StepCheckHTTP) && s.bare():
		var out []ResolvedStep
		for j, ch := range c.p.Checks {
			if ch.Type == s.Type {
				out = append(out, inherit(ch, fmt.Sprintf("checks[%d]", j))...)
			}
		}
		if len(out) == 0 && s.Type == StepCheckContainer {
			rs := c.base(s)
			rs.Services = c.services
			out = append(out, rs)
		}
		return out
	}
	return c.resolveConcrete(sc, s, field)
}

func (c *run) base(s Step) ResolvedStep {
	rs := ResolvedStep{
		Type: s.Type, Timeout: DefaultTimeout(s.Type), Dir: c.p.Dir, EnvFile: c.p.EnvFile,
		ComposeFiles: append([]string(nil), c.p.ComposeFiles...), ComposeProject: c.p.ComposeProject,
	}
	if s.Timeout != 0 {
		rs.Timeout = time.Duration(s.Timeout)
	}
	return rs
}

// resolveConcrete resolves a step that carries its own parameters.
func (c *run) resolveConcrete(sc scope, s Step, field string) []ResolvedStep {
	if !s.Type.Known() {
		sc.add(field+".type", CodeUnknownStepType, "step type %q is not a building block", s.Type)
		return nil
	}
	if s.Type == StepSetEnv {
		return c.resolveSetEnv(sc, s, field)
	}
	texts := append(append(append([]string{s.Tag, s.Command, s.Volume, s.RestoreCommand, s.URL, s.Contains, s.Run},
		s.Services...), s.Containers...), s.Assign...)
	if !usesPerImage(texts...) {
		return c.build(sc, s, field, nil)
	}
	var out []ResolvedStep
	for i := range c.images {
		out = append(out, c.build(sc, s, field, &c.images[i])...)
	}
	return out
}

func (c *run) resolveSetEnv(sc scope, s Step, field string) []ResolvedStep {
	rs := c.base(s)
	if c.p.EnvFile == "" {
		sc.add(field, CodeMissingEnvFile, "set-env needs the project to set env_file")
		return nil
	}
	keys := map[string]bool{}
	add := func(i int, raw string, img *image) {
		f := fmt.Sprintf("%s.assign[%d]", field, i)
		text, perr := substitute(raw, false, c.lookup(img))
		if perr != nil {
			sc.add(f, perr.code, "%s", perr.msg)
			return
		}
		k, v, found := strings.Cut(text, "=")
		switch {
		case !found || !envNameRe.MatchString(k):
			sc.add(f, CodeInvalidValue, "assignment %q must have the form KEY=value with a valid variable name", text)
		case strings.ContainsAny(v, "\x00\n\r"):
			sc.add(f, CodeInvalidValue, "the value of %s contains a line break or control character", k)
		case keys[k]:
			sc.add(f, CodeDuplicateEnvVar, "environment variable %q is assigned more than once in the step", k)
		default:
			keys[k] = true
			rs.Assignments = append(rs.Assignments, Assignment{Key: k, Value: v})
		}
	}
	for i, raw := range s.Assign {
		if usesPerImage(raw) {
			for j := range c.images {
				add(i, raw, &c.images[j])
			}
		} else {
			add(i, raw, nil)
		}
	}
	return []ResolvedStep{rs}
}

// build resolves one instance of a step; img is the image of a per-image instance.
func (c *run) build(sc scope, s Step, field string, img *image) []ResolvedStep {
	lk := c.lookup(img)
	ok := true
	fail := func(f string, perr *phError) {
		sc.add(f, perr.code, "%s", perr.msg)
		ok = false
	}
	arg := func(name, raw string) string {
		v, perr := substitute(raw, false, lk)
		if perr != nil {
			fail(field+"."+name, perr)
		}
		return v
	}
	shell := func(name, raw string) string {
		v, perr := substitute(raw, true, lk)
		if perr != nil {
			fail(field+"."+name, perr)
		}
		return v
	}
	list := func(name string, raw []string) []string {
		vs, perr := expandList(raw, lk)
		if perr != nil {
			fail(field+"."+name, perr)
			return nil
		}
		seen := map[string]bool{}
		var out []string
		for _, v := range vs {
			if !serviceRe.MatchString(v) {
				sc.add(field+"."+name, CodeInvalidValue, "%q is not a valid service or container name", v)
				ok = false
			}
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
		return out
	}

	rs := c.base(s)
	switch s.Type {
	case StepComposePull, StepComposeUp:
		rs.Services = list("services", s.Services)
	case StepGitCheckout:
		rs.Tag = arg("tag", s.Tag)
		if s.Tag != "" && ok && !validVersion(rs.Tag) {
			sc.add(field+".tag", CodeInvalidValue, "tag %q is not a valid git tag", rs.Tag)
			ok = false
		}
	case StepBackup:
		b := &BackupParams{Keep: s.Keep, Command: shell("command", s.Command), Volume: arg("volume", s.Volume),
			RestoreCommand: shell("restore_command", s.RestoreCommand)}
		if b.Keep == 0 {
			b.Keep = DefaultBackupKeep
		}
		if b.Volume != "" && !serviceRe.MatchString(b.Volume) {
			sc.add(field+".volume", CodeInvalidValue, "%q is not a valid volume name", b.Volume)
			ok = false
		}
		rs.Backup = b
	case StepCheckContainer:
		rs.Services = list("services", s.Services)
		rs.Containers = list("containers", s.Containers)
	case StepCheckHTTP:
		rs.URL = arg("url", s.URL)
		rs.Contains = arg("contains", s.Contains)
		rs.Status = s.Status
		if rs.Status == 0 {
			rs.Status = DefaultHTTPStatus
		}
		if u, err := url.Parse(rs.URL); ok && s.URL != "" && (err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "") {
			sc.add(field+".url", CodeInvalidValue, "url %q must be an http or https URL with a host", rs.URL)
			ok = false
		}
	case StepCommand:
		rs.Shell = shell("run", s.Run)
	}
	if !ok {
		return nil
	}
	return []ResolvedStep{rs}
}
