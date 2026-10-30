package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/gitrepo"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry/registrytest"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const (
	wrongSecret    = "wrong-token-value-0002"
	replacedSecret = "replaced-token-value-0003"
	demoLabel      = "https://example.invalid/demo/app"
)

// sourceFixture is a service whose registry requests go to local test servers: registry.example.com is the
// real one of the approved project, evil.example.com a server the approved project never names.
type sourceFixture struct {
	*env
	reg, evil *registrytest.Server
	dir       string

	mu    sync.Mutex
	hosts []string
}

func (f *sourceFixture) askedHosts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.hosts)
}

func demoRegistryConfig(token string) registrytest.Config {
	return registrytest.Config{
		User: "demo-user", Token: token, Labels: map[string]string{registry.LabelSource: demoLabel},
		Repos: map[string][]string{"demo/app": {"1.0.0", "1.1.0", "2.0.0", "latest"}},
	}
}

func imageProject(dir, ref string) string {
	return fmt.Sprintf(`schema: 1
name: demo-app
dir: %[1]s
template: image-tag
env_file: %[1]s/.env
images:
  - name: app
    ref: %[2]s
    env_var: DEMO_APP_TAG
    services: [app]
    credential: demo-registry
`, dir, ref)
}

// newSourceFixture starts a service with the credential demo-registry (holding token), the project demo-app
// and its env file, which names the tag 1.0.0. Nothing is approved yet.
func newSourceFixture(t *testing.T, token string, mods ...func(*Config)) *sourceFixture {
	t.Helper()
	f := &sourceFixture{dir: t.TempDir()}
	f.reg = registrytest.New(t, demoRegistryConfig(demoSecret))
	f.evil = registrytest.New(t, demoRegistryConfig(demoSecret))
	// The fixture's own setting comes first, so a mod can replace it.
	mods = append([]func(*Config){func(cfg *Config) {
		cfg.SourceRegistry.BaseURL = func(host string) string {
			f.mu.Lock()
			f.hosts = append(f.hosts, host)
			f.mu.Unlock()
			switch host {
			case "registry.example.com":
				return f.reg.URL
			case "evil.example.com":
				return f.evil.URL
			}
			return "http://127.0.0.1:1"
		}
	}}, mods...)
	f.env = newEnv(t, true, true, mods...)
	if _, err := f.svc.vault.Add("demo-registry", vault.KindRegistry, map[string]string{
		vault.FieldUser: "demo-user", vault.FieldToken: token}); err != nil {
		t.Fatal(err)
	}
	f.write(t, "sysmin.yaml", "schema: 1\n")
	f.write(t, "projects/demo-app.yaml", imageProject(f.dir, "registry.example.com/demo/app"))
	if err := os.WriteFile(filepath.Join(f.dir, ".env"), []byte("DEMO_APP_TAG=1.0.0\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	f.start(t)
	return f
}

func (f *sourceFixture) approve(t *testing.T) {
	t.Helper()
	if _, err := approveAll(t, f.admin(t)); err != nil {
		t.Fatal(err)
	}
}

// logText stops the service and returns everything it wrote to its log.
func (f *sourceFixture) logText(t *testing.T) string {
	t.Helper()
	f.cancel()
	<-f.done
	f.done <- nil
	var all strings.Builder
	files, _ := filepath.Glob(filepath.Join(f.cfg.StateDir, "logs", "*"))
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		all.Write(data)
	}
	return all.String()
}

func noSecret(t *testing.T, what, text string) {
	t.Helper()
	for _, secret := range []string{demoSecret, wrongSecret, replacedSecret, registrytest.Bearer} {
		if strings.Contains(text, secret) {
			t.Errorf("%s contains a secret: %s", what, text)
		}
	}
}

func TestSourceTestShowsVersionsOfTheApprovedImage(t *testing.T) {
	f := newSourceFixture(t, demoSecret)
	f.approve(t)
	reader := f.dial(t) // reading needs no admin session
	res, err := reader.SourceTest("demo-app")
	if err != nil || len(res.Sources) != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	s := res.Sources[0]
	if s.Kind != "image" || s.Name != "app" || s.Ref != "registry.example.com/demo/app" || s.Error != "" ||
		s.Current != "1.0.0" || fmt.Sprint(s.Available) != "[1.1.0 2.0.0]" || s.AvailableTotal != 2 || s.Target != "2.0.0" ||
		!strings.HasPrefix(s.Digest, "sha256:") || s.SourceRepo != demoLabel || len(s.Problems) != 0 {
		t.Fatalf("source = %+v", s)
	}
	if !slices.Contains(f.reg.Requests(), "GET /token") {
		t.Fatalf("the registry login did not happen: %v", f.reg.Requests())
	}
	noSecret(t, "result", fmt.Sprintf("%+v", res))

	log := f.logText(t)
	noSecret(t, "log", log)
	if !strings.Contains(log, `"action":"credential.use"`) || !strings.Contains(log, `"project":"demo-app"`) {
		t.Errorf("log lacks the entry: %s", log)
	}
}

func TestSourceTestNeedsAnApprovedState(t *testing.T) {
	f := newSourceFixture(t, demoSecret)
	wantCode(t, func() error { _, err := f.dial(t).SourceTest("demo-app"); return err }(), control.CodeNotApproved)
	wantCode(t, func() error { _, err := f.dial(t).SourceTest("other-app"); return err }(), control.CodeNotApproved)
	wantCode(t, func() error { _, err := f.dial(t).SourceTest("../x"); return err }(), control.CodeBadRequest)
	if got := f.reg.Requests(); len(got) != 0 {
		t.Fatalf("the registry was asked without an approval: %v", got)
	}
	if got := f.askedHosts(); len(got) != 0 {
		t.Fatalf("a registry address was resolved without an approval: %v", got)
	}
}

func TestSourceTestIgnoresUnapprovedChanges(t *testing.T) {
	f := newSourceFixture(t, demoSecret)
	f.approve(t)

	// A changed address and a replaced credential stay open approvals; the test keeps to the approved ones.
	f.write(t, "projects/demo-app.yaml", imageProject(f.dir, "evil.example.com/demo/app"))
	if _, err := f.admin(t).SetCredential(control.CredentialSetParams{Name: "demo-registry", Kind: "registry",
		Fields: map[string]string{vault.FieldUser: "demo-user", vault.FieldToken: replacedSecret}}); err != nil {
		t.Fatal(err)
	}
	res, err := f.dial(t).SourceTest("demo-app")
	if err != nil || len(res.Sources) != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if s := res.Sources[0]; s.Error != "" || s.Ref != "registry.example.com/demo/app" || s.Target != "2.0.0" {
		t.Fatalf("source = %+v", s)
	}
	if got := f.evil.Requests(); len(got) != 0 {
		t.Fatalf("the unapproved address was asked: %v", got)
	}
	if hosts := f.askedHosts(); len(hosts) == 0 || slices.ContainsFunc(hosts, func(h string) bool { return h != "registry.example.com" }) {
		t.Fatalf("hosts asked = %v", hosts)
	}
}

func TestSourceTestWrongCredentialIsClear(t *testing.T) {
	f := newSourceFixture(t, wrongSecret)
	f.approve(t)
	res, err := f.dial(t).SourceTest("demo-app")
	if err != nil || len(res.Sources) != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	s := res.Sources[0]
	if s.Error != control.SourceDenied || !strings.Contains(s.Message, "denied") || s.Target != "" || len(s.Available) != 0 {
		t.Fatalf("source = %+v", s)
	}
	noSecret(t, "result", fmt.Sprintf("%+v", res))
	noSecret(t, "log", f.logText(t))
}

func TestSourceTestCurrentUnknown(t *testing.T) {
	f := newSourceFixture(t, demoSecret)
	f.approve(t)
	envFile := filepath.Join(f.dir, ".env")
	for name, content := range map[string]string{"variable missing": "OTHER=1\n", "variable empty": "DEMO_APP_TAG=\n"} {
		if err := os.WriteFile(envFile, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
		res, err := f.dial(t).SourceTest("demo-app")
		if err != nil || len(res.Sources) != 1 {
			t.Fatalf("%s: result = %+v, %v", name, res, err)
		}
		s := res.Sources[0]
		if s.Error != "" || s.Current != "" || s.Target != "" || fmt.Sprint(s.Available) != "[1.0.0 1.1.0 2.0.0]" ||
			len(s.Problems) != 1 || s.Problems[0].Code != control.CodeCurrentUnknown || s.Problems[0].Hint == "" {
			t.Fatalf("%s: source = %+v", name, s)
		}
	}
	if err := os.Remove(envFile); err != nil {
		t.Fatal(err)
	}
	res, err := f.dial(t).SourceTest("demo-app")
	if err != nil || res.Sources[0].Problems[0].Code != control.CodeCurrentUnknown || strings.Contains(res.Sources[0].Problems[0].Message, f.dir) {
		t.Fatalf("missing env file: %+v, %v", res, err)
	}
}

func TestSourceTestOneFailingSourceKeepsTheOthers(t *testing.T) {
	f := newSourceFixture(t, demoSecret)
	two := imageProject(f.dir, "registry.example.com/demo/app") +
		"  - name: gone\n    ref: registry.example.com/demo/gone\n    env_var: DEMO_GONE_TAG\n    services: [gone]\n    credential: demo-registry\n" +
		"  - name: broken\n    ref: registry.example.com/demo/app:1.0.0\n    env_var: DEMO_BROKEN_TAG\n    services: [broken]\n"
	f.write(t, "projects/demo-app.yaml", two)
	f.approve(t)
	res, err := f.dial(t).SourceTest("demo-app")
	if err != nil || len(res.Sources) != 3 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	got := map[string]control.SourceResult{}
	for _, s := range res.Sources {
		got[s.Name] = s
	}
	if got["app"].Error != "" || got["app"].Target != "2.0.0" {
		t.Errorf("app = %+v", got["app"])
	}
	if got["gone"].Error != control.SourceNotFound {
		t.Errorf("gone = %+v", got["gone"])
	}
	if got["broken"].Error != control.SourceInvalid {
		t.Errorf("broken = %+v", got["broken"])
	}
}

func TestSourceTestLockedVault(t *testing.T) {
	f := newSourceFixture(t, demoSecret)
	f.approve(t)
	// A second service on the same directories cannot unlock itself and stays locked.
	locked := newEnv(t, true, false)
	locked.start(t)
	wantCode(t, func() error { _, err := locked.dial(t).SourceTest("demo-app"); return err }(), control.CodeLocked)
}

func gitRun(t *testing.T, dir string, args ...string) {
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

func TestSourceTestGitTags(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	bare, work := filepath.Join(root, "demo.git"), filepath.Join(root, "work")
	for _, d := range []string{bare, work} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, bare, "init", "--bare", "-q")
	gitRun(t, work, "init", "-q")
	for _, tag := range []string{"v1.0.0", "v1.1.0", "v2.0.0"} {
		gitRun(t, work, "commit", "-q", "--allow-empty", "-m", tag)
		gitRun(t, work, "tag", tag)
	}
	gitRun(t, work, "push", "-q", bare, "--tags")
	gitRun(t, work, "checkout", "-q", "v1.0.0")

	f := newSourceFixture(t, demoSecret, func(cfg *Config) {
		cfg.SourceGit = gitrepo.Options{ExtraProtocols: []string{"file"}, Run: func(cmd *exec.Cmd) error {
			if slices.Contains(cmd.Args, "ls-remote") {
				cmd.Args[len(cmd.Args)-1] = "file://" + bare // the project names an https address
			}
			return cmd.Run()
		}}
	})
	f.write(t, "projects/demo-app.yaml", fmt.Sprintf(`schema: 1
name: demo-app
dir: %s
template: git-tag
git:
  url: https://git.example.com/demo/app.git
`, work))
	f.approve(t)

	res, err := f.dial(t).SourceTest("demo-app")
	if err != nil || len(res.Sources) != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	s := res.Sources[0]
	if s.Kind != "git" || s.Ref != "https://git.example.com/demo/app.git" || s.Error != "" || s.Current != "v1.0.0" ||
		fmt.Sprint(s.Available) != "[v1.1.0 v2.0.0]" || s.Target != "v2.0.0" || len(s.Problems) != 0 {
		t.Fatalf("source = %+v", s)
	}

	// A working copy on no tag is a problem, not a failure; the versions are still listed.
	gitRun(t, work, "commit", "-q", "--allow-empty", "-m", "untagged")
	res, err = f.dial(t).SourceTest("demo-app")
	if err != nil {
		t.Fatal(err)
	}
	s = res.Sources[0]
	if s.Error != "" || s.Current != "" || s.Target != "" || len(s.Available) != 3 || len(s.Problems) != 1 ||
		s.Problems[0].Code != control.CodeCurrentUnknown || strings.Contains(s.Problems[0].Message, work) {
		t.Fatalf("untagged source = %+v", s)
	}
}

func TestDisplayURLDropsUserInformation(t *testing.T) {
	login := "demo-user" + "@" // assembled so the source holds no user@host literal
	for in, want := range map[string]string{
		"https://" + login + "git.example.com/a.git": "https://git.example.com/a.git",
		"ssh://" + login + "git.example.com/a.git":   "ssh://git.example.com/a.git",
		login + "git.example.com:team/a.git":         "git.example.com:team/a.git",
		"https://git.example.com/a.git":              "https://git.example.com/a.git",
	} {
		if got := displayURL(in); got != want {
			t.Errorf("displayURL(%q) = %q, want %q", in, got, want)
		}
	}
}
