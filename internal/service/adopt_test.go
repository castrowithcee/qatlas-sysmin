package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry/registrytest"
)

// fakeDocker answers docker ps, docker inspect and docker compose config with recorded text and keeps every
// call it was given.
type fakeDocker struct {
	ps, inspect, compose string
	psFails              bool

	mu    sync.Mutex
	calls []execute.Command
}

func (f *fakeDocker) Run(_ context.Context, c execute.Command) (execute.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	switch {
	case c.Name != "docker":
		return execute.Result{}, errors.New("unexpected program")
	case c.Args[0] == "ps":
		if f.psFails {
			return execute.Result{Output: []byte("Cannot connect to the Docker daemon at unix:///var/run/docker.sock")}, &execute.ExitError{Code: 1}
		}
		return execute.Result{Output: []byte(f.ps)}, nil
	case c.Args[0] == "inspect":
		return execute.Result{Output: []byte(f.inspect)}, nil
	case c.Args[0] == "compose":
		if f.compose == "" {
			return execute.Result{Output: []byte("invalid compose file")}, &execute.ExitError{Code: 1}
		}
		return execute.Result{Output: []byte(f.compose)}, nil
	}
	return execute.Result{}, errors.New("unexpected command")
}

func (f *fakeDocker) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func (f *fakeDocker) recorded() []execute.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// readOnly reports whether every recorded call only reads.
func (f *fakeDocker) readOnly() bool {
	for _, c := range f.recorded() {
		ok := c.Name == "docker" && (c.Args[0] == "ps" || c.Args[0] == "inspect" ||
			(c.Args[0] == "compose" && slices.Contains(c.Args, "config") && !slices.Contains(c.Args, "up")))
		if !ok {
			return false
		}
	}
	return true
}

const (
	adoptIDA = "0123456789ab"
	adoptIDB = "ba9876543210"
)

// adoptStack creates the directory of a running two-container stack of the Compose project project, with
// an override file, and a fake Docker that answers for it.
func adoptStack(t *testing.T, project, compose, envText string) (*fakeDocker, string) {
	t.Helper()
	dir := t.TempDir()
	files := filepath.Join(dir, "docker-compose.yml") + "," + filepath.Join(dir, "docker-compose.override.yml")
	lb, _ := json.Marshal(map[string]string{
		"com.docker.compose.project":              project,
		"com.docker.compose.project.config_files": files,
		"com.docker.compose.service":              "app",
	})
	if envText != "" {
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envText), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return &fakeDocker{ps: adoptIDA + "\n" + adoptIDB + "\n", inspect: string(lb) + "\n" + string(lb) + "\n", compose: compose}, dir
}

const shopCompose = `{"name":"shop-prod","services":{
	"web":{"image":"example.invalid/shop/app:${APP_VERSION}"},
	"worker":{"image":"example.invalid/shop/app:${APP_VERSION}"},
	"db":{"image":"example.invalid/shop/db:${DB_VERSION}"},
	"pg":{"image":"example.invalid/shop/pg:16.2.0"},
	"cache":{"image":"example.invalid/shop/cache:latest"}}}`

// adoptEnv starts an unlocked service that uses f as its Docker.
func adoptEnv(t *testing.T, f *fakeDocker, mods ...func(*Config)) *env {
	t.Helper()
	e := newEnv(t, true, true, append(mods, func(c *Config) { c.Runner = f })...)
	e.write(t, "sysmin.yaml", "schema: 1\n")
	e.start(t)
	return e
}

func openProjects(t *testing.T, c *client.Client) int {
	t.Helper()
	list, err := c.Projects()
	if err != nil {
		t.Fatal(err)
	}
	return len(list)
}

func problemCodes(res control.AdoptResult) map[string]control.AdoptProblem {
	out := map[string]control.AdoptProblem{}
	for _, p := range res.Problems {
		out[p.Code] = p
	}
	return out
}

func TestAdoptDryRunProposesWithoutStoring(t *testing.T) {
	f, dir := adoptStack(t, "shop-prod", shopCompose, "APP_VERSION=1.4.2\nDB_VERSION=16.2.0\nDB_PASSWORD=not-a-real-secret\n")
	e := adoptEnv(t, f)
	c := e.dial(t)

	res, err := c.AdoptProject(control.AdoptParams{Dir: dir, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	p := res.Project
	files := []string{filepath.Join(dir, "docker-compose.yml"), filepath.Join(dir, "docker-compose.override.yml")}
	if res.Saved || res.State != "" || res.Revision != "" || p.Name != "shop-prod" || p.ComposeProject != "shop-prod" ||
		p.Dir != dir || p.Template != "image-tag" || !slices.Equal(p.ComposeFiles, files) || p.EnvFile != filepath.Join(dir, ".env") {
		t.Fatalf("result = %+v", res)
	}
	if len(p.Images) != 2 {
		t.Fatalf("images = %+v", p.Images)
	}
	byRef := map[string]control.AdoptImage{}
	for _, im := range p.Images {
		byRef[im.Ref] = im
	}
	if a := byRef["example.invalid/shop/app"]; a.EnvVar != "APP_VERSION" || a.Current != "1.4.2" || !slices.Equal(a.Services, []string{"web", "worker"}) {
		t.Errorf("app = %+v", a)
	}
	if d := byRef["example.invalid/shop/db"]; d.EnvVar != "DB_VERSION" || d.Current != "16.2.0" || !slices.Equal(d.Services, []string{"db"}) {
		t.Errorf("db = %+v", d)
	}
	probs := problemCodes(res)
	for _, code := range []string{"fixed-tag", "floating-tag"} {
		if pr, ok := probs[code]; !ok || pr.Hint == "" || pr.Blocking {
			t.Errorf("problem %s = %+v", code, probs[code])
		}
	}
	if !strings.Contains(res.File, "template: image-tag") || !strings.Contains(res.File, "mode: manual") ||
		strings.Contains(res.File+res.Project.Dir, "not-a-real-secret") {
		t.Errorf("file = %q", res.File)
	}

	// Nothing was stored.
	if n := openProjects(t, c); n != 0 {
		t.Fatalf("projects = %d", n)
	}
	if items, err := e.admin(t).Approvals(); err != nil || len(items) != 0 {
		t.Fatalf("approvals = %+v, %v", items, err)
	}
	if !f.readOnly() {
		t.Errorf("calls = %+v", f.recorded())
	}
}

func TestAdoptStoresAnOpenApprovalAndRefusesAgain(t *testing.T) {
	f, dir := adoptStack(t, "shop-prod", shopCompose, "APP_VERSION=1.4.2\nDB_VERSION=16.2.0\n")
	e := adoptEnv(t, f)
	c := e.dial(t)

	res, err := c.AdoptProject(control.AdoptParams{Dir: dir})
	if err != nil || !res.Saved || res.State != control.StatePending || res.Revision == "" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	detail, err := c.Project("shop-prod")
	if err != nil || detail.State != control.StatePending || !strings.Contains(detail.File, "mode: manual") || detail.Revision != res.Revision {
		t.Fatalf("detail = %+v, %v", detail, err)
	}
	items, err := e.admin(t).Approvals()
	if err != nil || len(items) != 1 || items[0].Project != "shop-prod" {
		t.Fatalf("approvals = %+v, %v", items, err)
	}

	// The same directory again, then another directory with the same Compose project name.
	again, err := c.AdoptProject(control.AdoptParams{Dir: dir})
	if err != nil || again.Saved {
		t.Fatalf("again = %+v, %v", again, err)
	}
	if pr, ok := problemCodes(again)["already-adopted"]; !ok || !pr.Blocking || !strings.Contains(pr.Message, "shop-prod") || pr.Hint == "" {
		t.Errorf("problems = %+v", again.Problems)
	}
	f2, dir2 := adoptStack(t, "shop-prod", shopCompose, "APP_VERSION=1.4.2\nDB_VERSION=16.2.0\n")
	f.set(func() { f.ps, f.inspect = f2.ps, f2.inspect })
	taken, err := c.AdoptProject(control.AdoptParams{Dir: dir2})
	if err != nil || taken.Saved {
		t.Fatalf("taken = %+v, %v", taken, err)
	}
	if pr, ok := problemCodes(taken)["name-taken"]; !ok || !strings.Contains(pr.Hint, "--name") {
		t.Errorf("problems = %+v", taken.Problems)
	}
	if n := openProjects(t, c); n != 1 {
		t.Fatalf("projects = %d", n)
	}
	named, err := c.AdoptProject(control.AdoptParams{Dir: dir2, Name: "shop-two"})
	if err != nil || !named.Saved || named.Project.Name != "shop-two" {
		t.Fatalf("named = %+v, %v", named, err)
	}
}

func TestAdoptInAdminSessionIsApproved(t *testing.T) {
	f, dir := adoptStack(t, "shop-prod", shopCompose, "APP_VERSION=1.4.2\nDB_VERSION=16.2.0\n")
	e := adoptEnv(t, f)
	res, err := e.admin(t).AdoptProject(control.AdoptParams{Dir: dir})
	if err != nil || !res.Saved || res.State != control.StateApproved {
		t.Fatalf("result = %+v, %v", res, err)
	}
}

func TestAdoptBlockingProblemsStoreNothing(t *testing.T) {
	// Compose cannot read the stack.
	f, dir := adoptStack(t, "shop-prod", "", "")
	e := adoptEnv(t, f)
	c := e.dial(t)
	res, err := c.AdoptProject(control.AdoptParams{Dir: dir})
	if err != nil || res.Saved || !problemCodes(res)["compose-config-failed"].Blocking {
		t.Fatalf("result = %+v, %v", res, err)
	}
	for _, p := range res.Problems {
		if strings.Contains(p.Message+p.Hint, "invalid compose file") {
			t.Error("raw Docker output in the problems")
		}
	}

	// No container runs.
	f.set(func() { f.ps, f.compose = "", shopCompose })
	res, err = c.AdoptProject(control.AdoptParams{Dir: dir})
	if err != nil || res.Saved || !problemCodes(res)["no-running-containers"].Blocking {
		t.Fatalf("result = %+v, %v", res, err)
	}

	// Nothing to track.
	f.set(func() {
		f.ps, f.compose = adoptIDA+"\n", `{"services":{"db":{"image":"example.invalid/shop/db:16"}}}`
		f.inspect = strings.Split(f.inspect, "\n")[0] + "\n"
	})
	res, err = c.AdoptProject(control.AdoptParams{Dir: dir})
	if err != nil || res.Saved || !problemCodes(res)["no-tracked-image"].Blocking {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if n := openProjects(t, c); n != 0 {
		t.Fatalf("projects = %d", n)
	}
}

func TestAdoptRefusals(t *testing.T) {
	f, dir := adoptStack(t, "shop-prod", shopCompose, "APP_VERSION=1.4.2\n")
	e := adoptEnv(t, f)
	c := e.dial(t)
	for name, p := range map[string]control.AdoptParams{
		"relative":  {Dir: "relative/dir"},
		"empty":     {},
		"control":   {Dir: dir + "\n/x"},
		"bad name":  {Dir: dir, Name: "Bad Name"},
		"too long":  {Dir: "/" + strings.Repeat("a", maxAdoptDirLen)},
		"traversal": {Dir: dir, Name: "../x"},
	} {
		_, err := c.AdoptProject(p)
		var ce *control.Error
		if !errors.As(err, &ce) || ce.Code != control.CodeBadRequest {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	_, err := c.AdoptProject(control.AdoptParams{Dir: filepath.Join(dir, "missing")})
	wantCode(t, err, control.CodeNotFound)
	if len(f.recorded()) != 0 {
		t.Errorf("Docker was asked: %+v", f.recorded())
	}

	f.psFails = true
	_, err = c.AdoptProject(control.AdoptParams{Dir: dir})
	wantCode(t, err, control.CodeDockerUnavailable)

	locked := newEnv(t, true, false, func(cfg *Config) { cfg.Runner = f })
	locked.start(t)
	f.psFails = false
	_, err = locked.dial(t).AdoptProject(control.AdoptParams{Dir: dir})
	wantCode(t, err, control.CodeLocked)
}

func TestAdoptedVersionsAreCurrentAfterApproval(t *testing.T) {
	compose := `{"services":{"web":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`
	f, dir := adoptStack(t, "shop-prod", compose, "APP_VERSION=1.4.2\n")
	reg := registrytest.New(t, registrytest.Config{
		Anonymous: true, Repos: map[string][]string{"shop/app": {"1.4.2", "1.5.0", "2.0.0"}},
	})
	e := adoptEnv(t, f, func(cfg *Config) {
		cfg.SourceRegistry.BaseURL = func(host string) string {
			if host == "example.invalid" {
				return reg.URL
			}
			return "http://127.0.0.1:1"
		}
	})
	c := e.dial(t)
	if res, err := c.AdoptProject(control.AdoptParams{Dir: dir}); err != nil || !res.Saved {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if _, err := approveAll(t, e.admin(t)); err != nil {
		t.Fatal(err)
	}
	callsBefore := len(f.recorded())

	res, err := c.SourceTest("shop-prod")
	if err != nil || len(res.Sources) != 1 {
		t.Fatalf("source test = %+v, %v", res, err)
	}
	s := res.Sources[0]
	if s.Error != "" || s.Name != "app" || s.Current != "1.4.2" || !slices.Equal(s.Available, []string{"1.5.0", "2.0.0"}) {
		t.Fatalf("source = %+v", s)
	}
	if !f.readOnly() || len(f.recorded()) != callsBefore {
		t.Errorf("calls = %+v", f.recorded())
	}
}

// closeDir removes the search permission of dir until the test ends.
func closeDir(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root passes any directory")
	}
	if err := os.Chmod(dir, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}

func wantNoAccess(t *testing.T, err error, path string) {
	t.Helper()
	var ce *control.Error
	if !errors.As(err, &ce) || ce.Code != control.CodeNoAccess || !slices.Equal(ce.Paths, []string{path}) {
		t.Fatalf("err = %v (%+v), want no-access %q", err, ce, path)
	}
}

func TestAdoptClosedParentIsNoAccessMissingIsNotFound(t *testing.T) {
	f, dir := adoptStack(t, "shop-prod", shopCompose, "APP_VERSION=1.4.2\n")
	closed := filepath.Join(dir, "closed")
	stack := filepath.Join(closed, "inner", "stack")
	if err := os.MkdirAll(stack, 0o700); err != nil {
		t.Fatal(err)
	}
	e := adoptEnv(t, f)
	c := e.dial(t)
	closeDir(t, closed)

	// The first denied element is named, whatever lies below it, existing or not.
	for _, d := range []string{stack, filepath.Join(stack, "missing"), closed} {
		_, err := c.AdoptProject(control.AdoptParams{Dir: d, DryRun: true})
		wantNoAccess(t, err, closed)
	}
	_, err := c.AdoptProject(control.AdoptParams{Dir: filepath.Join(dir, "missing", "deeper")})
	wantCode(t, err, control.CodeNotFound)
	var ce *control.Error
	if errors.As(err, &ce) && len(ce.Paths) != 0 {
		t.Errorf("not-found carries paths %v", ce.Paths)
	}
}

func TestAdoptDirectoryItselfNotPassable(t *testing.T) {
	f, dir := adoptStack(t, "shop-prod", shopCompose, "APP_VERSION=1.4.2\n")
	e := adoptEnv(t, f)
	c := e.dial(t)
	closeDir(t, dir)
	_, err := c.AdoptProject(control.AdoptParams{Dir: dir, DryRun: true})
	wantNoAccess(t, err, dir)
	if len(f.recorded()) != 0 {
		t.Errorf("Docker was asked: %+v", f.recorded())
	}
}

func TestAdoptUnreadableComposeFileBlocks(t *testing.T) {
	f, dir := adoptStack(t, "shop-prod", shopCompose, "APP_VERSION=1.4.2\n")
	if os.Geteuid() == 0 {
		t.Skip("root reads any file")
	}
	file := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(file, []byte("services: {}\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	res, err := adoptEnv(t, f).dial(t).AdoptProject(control.AdoptParams{Dir: dir})
	if err != nil || res.Saved {
		t.Fatalf("result = %+v, %v", res, err)
	}
	pr, ok := problemCodes(res)["not-readable"]
	if !ok || !pr.Blocking || !strings.Contains(pr.Message, file) || !strings.Contains(pr.Hint, "setfacl") {
		t.Fatalf("problems = %+v", res.Problems)
	}
	if _, wrong := problemCodes(res)["compose-config-failed"]; wrong {
		t.Errorf("misleading compose-config-failed in %+v", res.Problems)
	}
}

func TestAdoptNoAccessAuditNamesNoPath(t *testing.T) {
	f, dir := adoptStack(t, "shop-prod", shopCompose, "APP_VERSION=1.4.2\n")
	closed := filepath.Join(dir, "closed")
	if err := os.MkdirAll(filepath.Join(closed, "stack"), 0o700); err != nil {
		t.Fatal(err)
	}
	e := adoptEnv(t, f)
	c := e.dial(t)
	closeDir(t, closed)
	_, err := c.AdoptProject(control.AdoptParams{Dir: filepath.Join(closed, "stack"), Name: "shop"})
	wantNoAccess(t, err, closed)
	_, err = c.AdoptProject(control.AdoptParams{Dir: filepath.Join(dir, "missing"), Name: "shop"})
	wantCode(t, err, control.CodeNotFound)
	e.cancel()
	<-e.done
	e.done <- nil

	seen := map[string]bool{}
	for _, en := range readEntries(t, filepath.Join(e.cfg.StateDir, "logs")) {
		b, _ := json.Marshal(en)
		if strings.Contains(string(b), dir) {
			t.Errorf("audit entry names a path: %s", b)
		}
		seen[en.ErrorCode] = true
	}
	if !seen[control.CodeNoAccess] || !seen[control.CodeNotFound] {
		t.Errorf("audited codes = %v", seen)
	}
}
