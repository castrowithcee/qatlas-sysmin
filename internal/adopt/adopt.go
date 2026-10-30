// Package adopt analyses the directory of a running Docker Compose stack and proposes a sysmin project for it.
// It only reads: it asks Docker for the labels of the running containers and for the uninterpolated Compose
// configuration, reads the environment file and, for a git working copy, the checked-out tag. Nothing is
// deployed, written or changed. All values taken from labels and Compose files are untrusted.
package adopt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/gitrepo"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/version"
	"github.com/castrowithcee/qatlas-sysmin/internal/writecheck"
)

// Problem codes.
const (
	CodeNoContainers      = "no-running-containers"
	CodeConflictingLabels = "conflicting-labels"
	CodeInvalidLabel      = "invalid-label"
	CodeNoComposeFiles    = "compose-files-unknown"
	CodeMultipleEnvFiles  = "multiple-env-files"
	CodeComposeConfig     = "compose-config-failed"
	CodeInvalidName       = "invalid-name"
	CodeFixedTag          = "fixed-tag"
	CodeFloatingTag       = "floating-tag"
	CodePrereleaseTag     = "prerelease-tag"
	CodeComplexTag        = "complex-tag"
	CodeVariableRepo      = "variable-repository"
	CodeSharedVariable    = "shared-variable"
	CodeMissingVariable   = "missing-variable"
	CodeEnvFileUnreadable = "env-file-unreadable"
	CodeBuildService      = "build-service"
	CodeInvalidService    = "invalid-service"
	CodeNoTrackedImage    = "no-tracked-image"
	CodeNotWritable       = "not-writable"
	CodeGitOrigin         = "git-origin"
	CodeNoTag             = "no-tag-checked-out"
	CodeTagUnreadable     = "git-tag-unreadable"
)

// Compose labels set on every container of a stack.
const (
	labelProject    = "com.docker.compose.project"
	labelConfig     = "com.docker.compose.project.config_files"
	labelWorkingDir = "com.docker.compose.project.working_dir"
	labelEnvFile    = "com.docker.compose.project.environment_file"
	labelOneOff     = "com.docker.compose.oneoff"
)

const maxContainers = 64

var (
	containerIDRe = regexp.MustCompile(`^[0-9a-f]{12,64}$`)
	composeNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	serviceRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	repoRe        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
	nameCharRe    = regexp.MustCompile(`[^a-z0-9-]+`)
)

// Problem is a finding with a hint for the operator.
type Problem struct {
	Code    string
	Message string
	Hint    string
}

// Proposal is what Inspect found. Project is always in manual mode and is complete only when Problems does
// not name what is missing.
type Proposal struct {
	Project config.Project
	// Versions maps an image name, or "git" for a git source, to the version the stack runs now.
	Versions map[string]string
	Problems []Problem
}

// Options configures Inspect.
type Options struct {
	// Runner runs docker and git; it must not be nil.
	Runner execute.Runner
	// Git configures the reading of the checked-out tag.
	Git gitrepo.Options
}

// Adopted returns the name of the project that already manages dir.
func Adopted(m *config.Model, dir string) (string, bool) {
	dir = filepath.Clean(dir)
	for _, p := range m.Projects {
		if filepath.Clean(p.Dir) == dir {
			return p.Name, true
		}
	}
	return "", false
}

type inspector struct {
	opts Options
	dir  string
	prop Proposal
	// imageProblems are dropped when the stack is tracked through git, where the checkout sets the images.
	imageProblems []Problem
}

func (in *inspector) problem(code, msg, hint string) {
	in.prop.Problems = append(in.prop.Problems, Problem{Code: code, Message: msg, Hint: hint})
}

func (in *inspector) imageProblem(code, msg, hint string) {
	in.imageProblems = append(in.imageProblems, Problem{Code: code, Message: msg, Hint: hint})
}

// Inspect proposes a project for the stack that runs from dir. dir must be absolute. name is the project name
// to propose; empty means the Compose project name. Findings about the stack are returned as Problems; the
// error is reserved for bad arguments and for a Docker that cannot be asked.
func Inspect(ctx context.Context, opts Options, dir, name string) (Proposal, error) {
	if opts.Runner == nil {
		return Proposal{}, errors.New("adopt: no runner")
	}
	if !filepath.IsAbs(dir) || strings.ContainsAny(dir, "\x00\n\r") {
		return Proposal{}, fmt.Errorf("directory %q must be an absolute path", dir)
	}
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Proposal{}, fmt.Errorf("%q is not a directory", dir)
	}
	in := &inspector{opts: opts, dir: dir}
	in.prop.Project = config.Project{Schema: config.CurrentSchema, Dir: dir}
	in.prop.Versions = map[string]string{}

	labels, err := in.stackLabels(ctx)
	if err != nil {
		return Proposal{}, err
	}
	if labels == nil {
		in.problem(CodeNoContainers, "no running container belongs to a Compose project in this directory",
			"Start the stack with docker compose up -d from this directory, then adopt it.")
		in.prop.Project.Name = name
		return in.prop, nil
	}
	p := &in.prop.Project
	p.ComposeProject, p.ComposeFiles, p.EnvFile = labels.project, labels.files, labels.envFile
	p.Name = name
	if p.Name == "" {
		p.Name = labels.project
	}
	if !config.ValidName(p.Name) {
		in.problem(CodeInvalidName, fmt.Sprintf("%q cannot be a project name", p.Name),
			"Choose a name of lowercase letters, digits and dashes, at most 63 characters.")
	}

	services := in.composeServices(ctx)
	images := in.images(services)

	gitMode := false
	if hasGit(dir) {
		usesVar := false
		for _, im := range images {
			usesVar = usesVar || im.EnvVar != ""
		}
		gitMode = !usesVar
	}
	if gitMode {
		in.gitSource(ctx)
	} else {
		in.prop.Problems = append(in.prop.Problems, in.imageProblems...)
		in.imageSource(images, len(services) > 0)
	}
	return in.prop, nil
}

type stackLabels struct {
	project string
	files   []string
	envFile string
}

type rawLabels struct{ project, files, envFile string }

// stackLabels reads the labels of the running containers of the directory. It returns nil when there are none.
func (in *inspector) stackLabels(ctx context.Context) (*stackLabels, error) {
	res, err := in.opts.Runner.Run(ctx, execute.Command{Name: "docker", Args: []string{
		"ps", "-q", "--no-trunc", "--filter", "label=" + labelWorkingDir + "=" + in.dir}})
	if err != nil {
		return nil, fmt.Errorf("docker ps failed: %w", err)
	}
	var ids []string
	for _, f := range strings.Fields(string(res.Output)) {
		if containerIDRe.MatchString(f) && len(ids) < maxContainers {
			ids = append(ids, f)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	res, err = in.opts.Runner.Run(ctx, execute.Command{Name: "docker",
		Args: append([]string{"inspect", "--format", "{{json .Config.Labels}}"}, ids...)})
	if err != nil {
		return nil, fmt.Errorf("docker inspect failed: %w", err)
	}

	var first *rawLabels
	conflict := false
	for _, line := range strings.Split(string(res.Output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var lb map[string]string
		// One-off containers (docker compose run) are not part of the running stack.
		if json.Unmarshal([]byte(line), &lb) != nil || lb[labelOneOff] == "True" || lb[labelProject] == "" {
			continue
		}
		cur := rawLabels{lb[labelProject], lb[labelConfig], lb[labelEnvFile]}
		switch {
		case first == nil:
			first = &cur
		case cur != *first:
			conflict = true
		}
	}
	if first == nil {
		return nil, nil
	}
	if conflict {
		in.problem(CodeConflictingLabels, "the running containers disagree about the Compose project, its files or its environment file",
			"Recreate the stack with one docker compose up -d so that all containers carry the same labels.")
	}

	out := &stackLabels{project: first.project}
	if !composeNameRe.MatchString(out.project) {
		in.problem(CodeInvalidLabel, fmt.Sprintf("the Compose project name %q is not valid", out.project),
			"Recreate the stack with a Compose project name of lowercase letters, digits, dashes and underscores.")
		out.project = ""
	}
	for _, f := range splitList(first.files) {
		c, ok := cleanAbs(f)
		if !ok {
			in.problem(CodeInvalidLabel, fmt.Sprintf("the Compose file %q is not a usable absolute path", f),
				"Recreate the stack with absolute -f paths.")
			out.files = nil
			break
		}
		out.files = append(out.files, c)
	}
	if len(out.files) == 0 {
		in.problem(CodeNoComposeFiles, "the containers do not say which Compose files they were started with",
			"Set compose_files by hand; without the right files Compose may create duplicate containers.")
	}
	out.envFile = filepath.Join(in.dir, ".env")
	if envs := splitList(first.envFile); len(envs) > 0 {
		if len(envs) > 1 {
			in.problem(CodeMultipleEnvFiles, "the stack uses several environment files, sysmin manages one",
				"Keep the image variable in a single env file and recreate the stack with it.")
		}
		// Later files win in Compose, so the last one holds the effective value.
		if c, ok := cleanAbs(envs[len(envs)-1]); ok {
			out.envFile = c
		}
	}
	return out, nil
}

func splitList(s string) []string {
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// cleanAbs accepts an absolute path without control characters and returns it cleaned.
func cleanAbs(p string) (string, bool) {
	if !filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\n\r") {
		return "", false
	}
	return filepath.Clean(p), true
}

type service struct {
	Image string          `json:"image"`
	Build json.RawMessage `json:"build"`
}

// composeServices returns the services of the uninterpolated Compose configuration, or nil when Compose
// cannot produce it.
func (in *inspector) composeServices(ctx context.Context) map[string]service {
	p := &in.prop.Project
	args := []string{"compose"}
	if p.ComposeProject != "" {
		args = append(args, "-p", p.ComposeProject)
	}
	for _, f := range p.ComposeFiles {
		args = append(args, "-f", f)
	}
	args = append(args, "config", "--no-interpolate", "--format", "json")
	res, err := in.opts.Runner.Run(ctx, execute.Command{Name: "docker", Args: args, Dir: in.dir})
	fail := func() map[string]service {
		in.problem(CodeComposeConfig, "docker compose config failed, so the images cannot be read",
			"Run docker compose config in the directory and fix what it reports.")
		return nil
	}
	if err != nil {
		return fail()
	}
	// Warnings may precede the single JSON document.
	out := string(res.Output)
	start := 0
	if !strings.HasPrefix(out, "{") {
		i := strings.Index(out, "\n{")
		if i < 0 {
			return fail()
		}
		start = i + 1
	}
	var doc struct {
		Services map[string]service `json:"services"`
	}
	if err := json.NewDecoder(strings.NewReader(out[start:])).Decode(&doc); err != nil {
		return fail()
	}
	return doc.Services
}

var tagVarRe = regexp.MustCompile(`^(?:\$\{([A-Za-z_][A-Za-z0-9_]*)(?:(:?-)([^}$]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*))$`)

// splitImage separates the repository from the tag expression. A digest counts as fixed.
func splitImage(ref string) (repo, tag string, digest bool) {
	if i := strings.IndexByte(ref, '@'); i >= 0 {
		return ref[:i], "", true
	}
	slash := strings.LastIndexByte(ref, '/')
	// The colon of ${VAR:-default} is part of the tag expression.
	if i := strings.LastIndex(ref, ":${"); i > slash {
		return ref[:i], ref[i+1:], false
	}
	if i := strings.LastIndexByte(ref, ':'); i > slash {
		return ref[:i], ref[i+1:], false
	}
	return ref, "", false
}

type imageGroup struct {
	repo, envVar, def string
	hasDefault        bool
	services          []string
}

// images groups the services whose tag is an environment variable into config images and records a problem
// for every service that cannot be tracked.
func (in *inspector) images(services map[string]service) []config.Image {
	names := make([]string, 0, len(services))
	for n := range services {
		names = append(names, n)
	}
	sort.Strings(names)

	var groups []*imageGroup
	for _, n := range names {
		sv := services[n]
		if !serviceRe.MatchString(n) {
			in.imageProblem(CodeInvalidService, fmt.Sprintf("service %q has a name sysmin cannot use", n), "Rename the service.")
			continue
		}
		if sv.Image == "" {
			in.imageProblem(CodeBuildService, fmt.Sprintf("service %q has no image (it is built locally)", n),
				"sysmin cannot track a built service by image; track the repository it is built from as a git source instead.")
			continue
		}
		repo, tag, digest := splitImage(sv.Image)
		switch {
		case strings.Contains(repo, "$") || !repoRe.MatchString(repo):
			in.imageProblem(CodeVariableRepo, fmt.Sprintf("the image of service %q does not have a plain repository name", n),
				"Write the repository literally and keep only the tag in a variable.")
			continue
		case digest:
			in.imageProblem(CodeFixedTag, fmt.Sprintf("service %q is pinned to a digest (%s)", n, repo),
				fixedHint(repo))
			continue
		case tag == "":
			in.imageProblem(CodeFloatingTag, fmt.Sprintf("service %q uses %s without a tag, which means latest", n, repo),
				fixedHint(repo))
			continue
		}
		m := tagVarRe.FindStringSubmatch(tag)
		if m == nil {
			if strings.Contains(tag, "$") {
				in.imageProblem(CodeComplexTag, fmt.Sprintf("the tag of service %q mixes a variable with other text", n),
					"Make the whole tag one variable, such as image: "+repo+":${APP_VERSION}.")
				continue
			}
			code := CodeFixedTag
			if floating(tag) {
				code = CodeFloatingTag
			}
			in.imageProblem(code, fmt.Sprintf("service %q uses the fixed tag %q of %s", n, tag, repo), fixedHint(repo))
			continue
		}
		v := m[1]
		if v == "" {
			v = m[4]
		}
		var g *imageGroup
		for _, c := range groups {
			if c.repo == repo && c.envVar == v {
				g = c
			}
		}
		if g == nil {
			g = &imageGroup{repo: repo, envVar: v, def: m[3], hasDefault: m[2] != ""}
			groups = append(groups, g)
		}
		g.services = append(g.services, n)
	}

	var out []config.Image
	used := map[string]string{} // variable -> repository
	taken := map[string]bool{}
	for _, g := range groups {
		if r, ok := used[g.envVar]; ok && r != g.repo {
			in.imageProblem(CodeSharedVariable, fmt.Sprintf("%s is the tag of %s and of %s", g.envVar, r, g.repo),
				"Give each image its own variable.")
			continue
		}
		used[g.envVar] = g.repo
		im := config.Image{
			Name: uniqueName(g.repo, taken), Ref: g.repo, EnvVar: g.envVar, Services: g.services,
			VersionRule: config.VersionRule{Mode: config.ModeManual},
		}
		in.currentVersion(im, g)
		out = append(out, im)
	}
	return out
}

func floating(tag string) bool {
	sel := version.Select(tag, nil, config.VersionRule{})
	for _, p := range sel.Problems {
		if p.Code == version.CodeFloatingTag {
			return true
		}
	}
	return false
}

func fixedHint(repo string) string {
	return "Change the compose file to image: " + repo + ":${APP_VERSION} and set APP_VERSION=<current version> in the env file; sysmin does not edit compose files."
}

// uniqueName derives an image name from the last path component of the repository.
func uniqueName(repo string, taken map[string]bool) string {
	base := repo[strings.LastIndexByte(repo, '/')+1:]
	base = strings.Trim(nameCharRe.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if len(base) > 56 {
		base = strings.Trim(base[:56], "-")
	}
	if base == "" {
		base = "image"
	}
	name := base
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	taken[name] = true
	return name
}

// currentVersion reads the value of the image variable from the env file and records it, or the problem.
func (in *inspector) currentVersion(im config.Image, g *imageGroup) {
	val, found, err := execute.ReadEnvValue(in.prop.Project.EnvFile, im.EnvVar)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		err, found = nil, false
	case err != nil:
		in.imageProblem(CodeEnvFileUnreadable, "the env file cannot be read", "Make the env file a regular file that sysmin may read.")
		return
	}
	if !found || val == "" {
		in.imageProblem(CodeMissingVariable, fmt.Sprintf("%s is not set in the env file", im.EnvVar),
			"Set "+im.EnvVar+"=<current version> in the env file, so that sysmin can read and change it.")
		if !g.hasDefault || g.def == "" {
			return
		}
		val = g.def
	}
	in.prop.Versions[im.Name] = val
	in.versionProblem(fmt.Sprintf("the value of %s", im.EnvVar), val, true)
}

// versionProblem reports a version that is not a complete release version.
func (in *inspector) versionProblem(what, val string, image bool) {
	add := in.problem
	if image {
		add = in.imageProblem
	}
	short := val
	if len(short) > 64 {
		short = short[:64]
	}
	sel := version.Select(val, nil, config.VersionRule{})
	for _, p := range sel.Problems {
		switch p.Code {
		case version.CodeFloatingTag:
			add(CodeFloatingTag, fmt.Sprintf("%s is %q, which is not a complete version", what, short),
				"Set it to the exact version the stack runs, such as 1.4.2 or 1.4.2-alpine; sysmin does not change running tags.")
		case version.CodePrereleaseTag:
			add(CodePrereleaseTag, fmt.Sprintf("%s is the pre-release %q", what, short), p.Hint)
		}
	}
}

func hasGit(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// imageSource finishes a project that tracks images through env variables.
func (in *inspector) imageSource(images []config.Image, haveServices bool) {
	p := &in.prop.Project
	p.Images = images
	p.Template = "image-tag"
	if len(images) == 0 {
		p.Template = ""
		if !haveServices {
			return
		}
		in.problem(CodeNoTrackedImage, "no image tag is an environment variable, so there is nothing sysmin could update",
			"Use image: repository:${APP_VERSION} in the compose file, or adopt a git working copy.")
	}
	if err := writecheck.File(p.EnvFile); err != nil {
		in.problem(CodeNotWritable, "sysmin may not write the env file", "Give the account that runs sysmin write access to "+p.EnvFile+".")
	}
}

// gitSource finishes a project that tracks the tags of a git working copy.
func (in *inspector) gitSource(ctx context.Context) {
	p := &in.prop.Project
	p.Template = "git-tag"
	p.EnvFile = ""
	if err := writecheck.Dir(in.dir); err != nil {
		in.problem(CodeNotWritable, "sysmin may not write the working copy", "Give the account that runs sysmin write access to "+in.dir+".")
	}

	g := &config.GitSource{VersionRule: config.VersionRule{Mode: config.ModeManual}}
	p.Git = g
	res, err := in.opts.Runner.Run(ctx, execute.Command{Name: "git",
		Args: []string{"-C", in.dir, "remote", "get-url", "origin"}, Env: []string{"GIT_TERMINAL_PROMPT=0"}})
	if err != nil {
		in.problem(CodeGitOrigin, "the remote origin of the working copy cannot be read", "Set it with git remote add origin <url>.")
	} else if g.URL = originURL(strings.TrimSpace(string(res.Output))); g.URL == "" {
		in.problem(CodeGitOrigin, "the remote origin is not an https or ssh repository address",
			"Set origin to an https:// or ssh:// URL or to user@host:path.")
	}

	tag, err := gitrepo.New(in.opts.Git).CurrentTag(ctx, in.dir)
	switch {
	case err != nil:
		in.problem(CodeTagUnreadable, "the checked-out tag cannot be read", "Check that git works in the directory.")
	case tag == "":
		in.problem(CodeNoTag, "the working copy is not on a release tag", "Check out the release tag that runs now: git checkout <tag>.")
	default:
		in.prop.Versions["git"] = tag
		in.versionProblem("the checked-out tag", tag, false)
	}
}

// originURL returns the address without secrets, or "" when it is not one sysmin can use. The user of an
// ssh address stays, because the address needs it; passwords and the user of https addresses are dropped.
func originURL(raw string) string {
	if raw == "" || strings.HasPrefix(raw, "-") || strings.ContainsAny(raw, " \t\r\n\x00") {
		return ""
	}
	if !strings.Contains(raw, "://") {
		if scpRe.MatchString(raw) {
			return raw
		}
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "ssh") {
		return ""
	}
	if u.Scheme == "https" || u.User == nil {
		u.User = nil
	} else {
		u.User = url.User(u.User.Username())
	}
	return u.String()
}

var scpRe = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^\s]+$`)
