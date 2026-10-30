package adopt

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
)

// fakeDocker answers docker ps, docker inspect, docker compose config and git remote get-url from fixed text.
type fakeDocker struct {
	ps      string
	stacks  string
	inspect string
	compose string
	origin  string
	calls   []execute.Command
}

func (f *fakeDocker) Run(_ context.Context, c execute.Command) (execute.Result, error) {
	f.calls = append(f.calls, c)
	var out string
	switch {
	case c.Name == "git":
		out = f.origin
	case c.Args[0] == "ps" && slices.Contains(c.Args, "--format"):
		out = f.stacks
	case c.Args[0] == "ps":
		out = f.ps
	case c.Args[0] == "inspect":
		out = f.inspect
	case c.Args[0] == "compose":
		if f.compose == "" {
			return execute.Result{Output: []byte("invalid compose file")}, &execute.ExitError{Code: 1}
		}
		out = f.compose
	}
	return execute.Result{Output: []byte(out)}, nil
}

const (
	idA = "0123456789ab"
	idB = "ba9876543210"
)

func labels(project, files, env string) string {
	m := map[string]string{
		"com.docker.compose.project":              project,
		"com.docker.compose.project.config_files": files,
		"com.docker.compose.service":              "app",
	}
	if env != "" {
		m["com.docker.compose.project.environment_file"] = env
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// stack builds a running two-container stack in a temporary directory.
func stack(t *testing.T, compose string) (*fakeDocker, string) {
	t.Helper()
	dir := t.TempDir()
	files := filepath.Join(dir, "docker-compose.yml") + "," + filepath.Join(dir, "docker-compose.override.yml")
	l := labels("shop-prod", files, "")
	return &fakeDocker{ps: idA + "\n" + idB + "\n", inspect: l + "\n" + l + "\n", compose: compose}, dir
}

func writeEnv(t *testing.T, dir, text string) string {
	t.Helper()
	p := filepath.Join(dir, ".env")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func codes(p Proposal) []string {
	var c []string
	for _, pr := range p.Problems {
		c = append(c, pr.Code)
	}
	return c
}

func inspect(t *testing.T, f *fakeDocker, dir string) Proposal {
	t.Helper()
	p, err := Inspect(context.Background(), Options{Runner: f}, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func validate(t *testing.T, p Proposal) {
	t.Helper()
	if probs := config.Validate(&config.Model{Settings: config.Settings{Schema: config.CurrentSchema}, Projects: []config.Project{p.Project}}, nil); len(probs) > 0 {
		t.Fatalf("proposal does not validate: %v", probs)
	}
}

func TestOverrideFilesAndProjectName(t *testing.T) {
	f, dir := stack(t, `{"name":"shop-prod","services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`)
	writeEnv(t, dir, "APP_VERSION=1.4.2\nDB_PASSWORD=not-a-real-secret\n")
	p := inspect(t, f, dir)
	if len(p.Problems) != 0 {
		t.Fatalf("problems: %+v", p.Problems)
	}
	pr := p.Project
	if pr.Name != "shop-prod" || pr.ComposeProject != "shop-prod" || pr.Dir != dir || pr.Template != "image-tag" {
		t.Errorf("project = %+v", pr)
	}
	want := []string{filepath.Join(dir, "docker-compose.yml"), filepath.Join(dir, "docker-compose.override.yml")}
	if !slices.Equal(pr.ComposeFiles, want) {
		t.Errorf("compose files = %v", pr.ComposeFiles)
	}
	if pr.EnvFile != filepath.Join(dir, ".env") {
		t.Errorf("env file = %q", pr.EnvFile)
	}
	if len(pr.Images) != 1 || pr.Images[0].Name != "app" || pr.Images[0].Ref != "example.invalid/shop/app" ||
		pr.Images[0].EnvVar != "APP_VERSION" || pr.Images[0].VersionRule.Mode != config.ModeManual ||
		!slices.Equal(pr.Images[0].Services, []string{"app"}) {
		t.Errorf("images = %+v", pr.Images)
	}
	if p.Versions["app"] != "1.4.2" {
		t.Errorf("versions = %v", p.Versions)
	}
	validate(t, p)

	// Compose receives the project and the files in order, in the directory, and does not interpolate.
	last := f.calls[len(f.calls)-1]
	wantArgs := append([]string{"compose", "-p", "shop-prod", "-f", want[0], "-f", want[1]}, "config", "--no-interpolate", "--format", "json")
	if !slices.Equal(last.Args, wantArgs) || last.Dir != dir {
		t.Errorf("compose call = %+v", last)
	}
}

func TestNameArgumentAndInvalidName(t *testing.T) {
	f, dir := stack(t, `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`)
	writeEnv(t, dir, "APP_VERSION=1.4.2\n")
	p, err := Inspect(context.Background(), Options{Runner: f}, dir, "shop")
	if err != nil || p.Project.Name != "shop" || p.Project.ComposeProject != "shop-prod" {
		t.Errorf("name = %q, err %v", p.Project.Name, err)
	}
	f.inspect = strings.ReplaceAll(f.inspect, "shop-prod", "Shop_Prod")
	p = inspect(t, f, dir)
	if !slices.Contains(codes(p), CodeInvalidLabel) {
		t.Errorf("codes = %v", codes(p))
	}
}

func TestTwoImages(t *testing.T) {
	f, dir := stack(t, `{"services":{
		"worker":{"image":"example.invalid/shop/app:${APP_VERSION:-0.0.1}"},
		"web":{"image":"example.invalid/shop/app:${APP_VERSION:-0.0.1}"},
		"db":{"image":"example.invalid/shop/db:$DB_VERSION"},
		"other":{"image":"example.invalid/other/app:${OTHER_VERSION}"}}}`)
	writeEnv(t, dir, "APP_VERSION=2.0.0\nDB_VERSION=v16.2.1-alpine\nOTHER_VERSION=3.1.0\n")
	p := inspect(t, f, dir)
	if len(p.Problems) != 0 {
		t.Fatalf("problems: %+v", p.Problems)
	}
	var got []string
	for _, im := range p.Project.Images {
		got = append(got, fmt.Sprintf("%s|%s|%s|%v", im.Name, im.Ref, im.EnvVar, im.Services))
	}
	want := []string{
		"db|example.invalid/shop/db|DB_VERSION|[db]",
		"app|example.invalid/other/app|OTHER_VERSION|[other]",
		"app-2|example.invalid/shop/app|APP_VERSION|[web worker]",
	}
	if !slices.Equal(got, want) {
		t.Errorf("images = %q", got)
	}
	if p.Versions["app-2"] != "2.0.0" || p.Versions["db"] != "v16.2.1-alpine" || p.Versions["app"] != "3.1.0" {
		t.Errorf("versions = %v", p.Versions)
	}
	validate(t, p)
}

func TestImageNameCollision(t *testing.T) {
	f, dir := stack(t, `{"services":{"a":{"image":"example.invalid/x/app:${A_V}"},"b":{"image":"example.invalid/y/App:${B_V}"}}}`)
	writeEnv(t, dir, "A_V=1.0.0\nB_V=1.0.0\n")
	p := inspect(t, f, dir)
	if len(p.Project.Images) != 2 || p.Project.Images[0].Name != "app" || p.Project.Images[1].Name != "app-2" {
		t.Errorf("images = %+v", p.Project.Images)
	}
	validate(t, p)
}

func TestFixedAndFloatingTags(t *testing.T) {
	f, dir := stack(t, `{"services":{
		"pinned":{"image":"example.invalid/shop/app:1.4.2"},
		"latest":{"image":"example.invalid/shop/web:latest"},
		"bare":{"image":"example.invalid/shop/bare"},
		"major":{"image":"example.invalid/shop/db:16"},
		"build":{"build":{"context":"."}}}}`)
	writeEnv(t, dir, "")
	p := inspect(t, f, dir)
	byMsg := map[string]Problem{}
	for _, pr := range p.Problems {
		if pr.Hint == "" || pr.Message == "" {
			t.Errorf("problem without text: %+v", pr)
		}
		byMsg[pr.Code+"|"+pr.Message] = pr
	}
	want := []string{
		CodeBuildService + `|service "build" has no image (it is built locally)`,
		CodeFloatingTag + `|service "latest" uses the fixed tag "latest" of example.invalid/shop/web`,
		CodeFloatingTag + `|service "major" uses the fixed tag "16" of example.invalid/shop/db`,
		CodeFloatingTag + `|service "bare" uses example.invalid/shop/bare without a tag, which means latest`,
		CodeFixedTag + `|service "pinned" uses the fixed tag "1.4.2" of example.invalid/shop/app`,
	}
	for _, w := range want {
		if _, ok := byMsg[w]; !ok {
			t.Errorf("missing problem %q in %v", w, codes(p))
		}
	}
	if !slices.Contains(codes(p), CodeNoTrackedImage) || len(p.Project.Images) != 0 {
		t.Errorf("codes = %v", codes(p))
	}
}

func TestFloatingAndUnsupportedVariables(t *testing.T) {
	f, dir := stack(t, `{"services":{
		"a":{"image":"example.invalid/shop/a:${A_V}"},
		"b":{"image":"example.invalid/shop/b:${B_V}-alpine"},
		"c":{"image":"${REG}/shop/c:${C_V}"},
		"d":{"image":"example.invalid/shop/d:${D_V}"},
		"e":{"image":"example.invalid/shop/e:${D_V}"}}}`)
	writeEnv(t, dir, "A_V=latest\nB_V=1.0.0\nC_V=1.0.0\nD_V=1.0.0\n")
	p := inspect(t, f, dir)
	c := codes(p)
	for _, w := range []string{CodeFloatingTag, CodeComplexTag, CodeVariableRepo, CodeSharedVariable} {
		if !slices.Contains(c, w) {
			t.Errorf("missing %s in %v", w, c)
		}
	}
}

func TestMissingVariable(t *testing.T) {
	for name, env := range map[string]string{"unset": "OTHER=1\n", "empty": "APP_VERSION=\n"} {
		t.Run(name, func(t *testing.T) {
			f, dir := stack(t, `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`)
			writeEnv(t, dir, env)
			p := inspect(t, f, dir)
			if !slices.Equal(codes(p), []string{CodeMissingVariable}) || p.Problems[0].Hint == "" {
				t.Errorf("problems = %+v", p.Problems)
			}
			if _, ok := p.Versions["app"]; ok {
				t.Errorf("versions = %v", p.Versions)
			}
		})
	}
	t.Run("no env file", func(t *testing.T) {
		f, dir := stack(t, `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`)
		if p := inspect(t, f, dir); !slices.Equal(codes(p), []string{CodeMissingVariable}) {
			t.Errorf("problems = %+v", p.Problems)
		}
	})
}

func TestNoRunningContainers(t *testing.T) {
	f := &fakeDocker{}
	dir := t.TempDir()
	p := inspect(t, f, dir)
	if !slices.Equal(codes(p), []string{CodeNoContainers}) || p.Problems[0].Hint == "" {
		t.Errorf("problems = %+v", p.Problems)
	}
	if len(f.calls) != 1 || !slices.Contains(f.calls[0].Args, "label=com.docker.compose.project.working_dir="+dir) {
		t.Errorf("calls = %+v", f.calls)
	}
}

func TestConflictingLabels(t *testing.T) {
	f, dir := stack(t, `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`)
	writeEnv(t, dir, "APP_VERSION=1.4.2\n")
	f.inspect = labels("shop-prod", filepath.Join(dir, "a.yml"), "") + "\n" + labels("shop-test", filepath.Join(dir, "a.yml"), "") + "\n"
	if p := inspect(t, f, dir); !slices.Contains(codes(p), CodeConflictingLabels) {
		t.Errorf("codes = %v", codes(p))
	}
}

func TestEnvFileLabelAndOneOff(t *testing.T) {
	f, dir := stack(t, `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`)
	env := filepath.Join(dir, "prod.env")
	if err := os.WriteFile(env, []byte("APP_VERSION=1.4.2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oneOff := `{"com.docker.compose.project":"other","com.docker.compose.oneoff":"True"}`
	f.inspect = labels("shop-prod", filepath.Join(dir, "a.yml"), env) + "\n" + oneOff + "\nwarning: noise\n"
	p := inspect(t, f, dir)
	if len(p.Problems) != 0 || p.Project.EnvFile != env || p.Versions["app"] != "1.4.2" {
		t.Errorf("proposal = %+v", p)
	}
}

func TestComposeConfigFailureAndWarnings(t *testing.T) {
	f, dir := stack(t, "")
	if p := inspect(t, f, dir); !slices.Equal(codes(p), []string{CodeComposeConfig}) {
		t.Errorf("codes = %v", codes(p))
	}
	writeEnv(t, dir, "APP_VERSION=1.4.2\n")
	f.compose = "time=\"x\" level=warning msg=\"something\"\n" + `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`
	if p := inspect(t, f, dir); len(p.Problems) != 0 {
		t.Errorf("problems = %+v", p.Problems)
	}
}

func TestNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to read-only files")
	}
	f, dir := stack(t, `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`)
	env := writeEnv(t, dir, "APP_VERSION=1.4.2\n")
	if err := os.Chmod(env, 0o444); err != nil {
		t.Fatal(err)
	}
	p := inspect(t, f, dir)
	if !slices.Equal(codes(p), []string{CodeNotWritable}) || p.Problems[0].Hint == "" {
		t.Errorf("problems = %+v", p.Problems)
	}
}

func TestNoSecretInProposalOrProblems(t *testing.T) {
	f, dir := stack(t, `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`)
	writeEnv(t, dir, "APP_VERSION=latest\nDB_PASSWORD=not-a-real-secret\n")
	p := inspect(t, f, dir)
	if b, _ := json.Marshal(p); strings.Contains(string(b), "not-a-real-secret") {
		t.Errorf("secret in proposal: %s", b)
	}
}

func TestRejectsBadArguments(t *testing.T) {
	for _, dir := range []string{"relative/dir", "", "/does/not/exist-sysmin"} {
		if _, err := Inspect(context.Background(), Options{Runner: &fakeDocker{}}, dir, ""); err == nil {
			t.Errorf("dir %q accepted", dir)
		}
	}
	if _, err := Inspect(context.Background(), Options{}, t.TempDir(), ""); err == nil {
		t.Error("nil runner accepted")
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Demo", "GIT_AUTHOR_EMAIL=demo@example.invalid",
		"GIT_COMMITTER_NAME=Demo", "GIT_COMMITTER_EMAIL=demo@example.invalid",
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func gitStack(t *testing.T, tag string) (*fakeDocker, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	f, dir := stack(t, `{"services":{"site":{"build":{"context":"."}},"db":{"image":"example.invalid/shop/db:16"}}}`)
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	gitIn(t, dir, "tag", "v1.0.0")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "second")
	if tag != "" {
		gitIn(t, dir, "tag", tag)
	}
	f.origin = "https://demo-user:demo-token@example.invalid/shop/site.git\n"
	return f, dir
}

func TestGitWorkingCopy(t *testing.T) {
	f, dir := gitStack(t, "v1.2.0")
	p := inspect(t, f, dir)
	if len(p.Problems) != 0 {
		t.Fatalf("problems: %+v", p.Problems)
	}
	pr := p.Project
	if pr.Git == nil || pr.Git.URL != "https://example.invalid/shop/site.git" || len(pr.Images) != 0 ||
		pr.Template != "git-tag" || pr.Git.VersionRule.Mode != config.ModeManual {
		t.Errorf("project = %+v git %+v", pr, pr.Git)
	}
	if p.Versions["git"] != "v1.2.0" {
		t.Errorf("versions = %v", p.Versions)
	}
	validate(t, p)
}

func TestGitWorkingCopyProblems(t *testing.T) {
	f, dir := gitStack(t, "")
	f.origin = "/srv/local/repo.git\n"
	c := codes(inspect(t, f, dir))
	if !slices.Contains(c, CodeNoTag) || !slices.Contains(c, CodeGitOrigin) {
		t.Errorf("codes = %v", c)
	}
}

func TestGitWithImageVariableUsesImages(t *testing.T) {
	f, dir := gitStack(t, "v1.2.0")
	f.compose = `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`
	writeEnv(t, dir, "APP_VERSION=1.4.2\n")
	p := inspect(t, f, dir)
	if p.Project.Git != nil || len(p.Project.Images) != 1 || p.Versions["app"] != "1.4.2" {
		t.Errorf("proposal = %+v", p)
	}
}

func TestOriginURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://u:p@example.invalid/a/b.git":  "https://example.invalid/a/b.git",
		"ssh://git:pw@example.invalid/a/b.git": "ssh://git@example.invalid/a/b.git",
		"git@example.invalid:a/b.git":          "git@example.invalid:a/b.git",
		"http://example.invalid/a/b.git":       "",
		"-oProxyCommand=x":                     "",
		"/srv/repo.git":                        "",
		"https://example.invalid/a b":          "",
	} {
		if got := originURL(in); got != want {
			t.Errorf("originURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAdopted(t *testing.T) {
	m := &config.Model{Projects: []config.Project{{Name: "one", Dir: "/srv/example/one"}, {Name: "two", Dir: "/srv/example/two/"}}}
	if n, ok := Adopted(m, "/srv/example/two"); !ok || n != "two" {
		t.Errorf("Adopted = %q, %v", n, ok)
	}
	if n, ok := Adopted(m, "/srv/example/one/../one/"); !ok || n != "one" {
		t.Errorf("Adopted = %q, %v", n, ok)
	}
	if _, ok := Adopted(m, "/srv/example/three"); ok {
		t.Error("unknown directory reported as adopted")
	}
}
