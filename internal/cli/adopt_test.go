package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/service"
)

// adoptDocker answers docker with recorded text for one running stack and keeps the calls.
type adoptDocker struct {
	compose string
	fails   bool

	mu    sync.Mutex
	calls []execute.Command
	ps    string
	lb    string
}

func (d *adoptDocker) Run(_ context.Context, c execute.Command) (execute.Result, error) {
	d.mu.Lock()
	d.calls = append(d.calls, c)
	d.mu.Unlock()
	switch {
	case d.fails:
		return execute.Result{Output: []byte("Cannot connect to the Docker daemon")}, &execute.ExitError{Code: 1}
	case c.Args[0] == "ps":
		return execute.Result{Output: []byte(d.ps)}, nil
	case c.Args[0] == "inspect":
		return execute.Result{Output: []byte(d.lb)}, nil
	case c.Args[0] == "compose" && d.compose != "":
		return execute.Result{Output: []byte(d.compose)}, nil
	}
	return execute.Result{Output: []byte("invalid compose file")}, &execute.ExitError{Code: 1}
}

func (d *adoptDocker) askedFor(text string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.calls {
		if strings.Contains(strings.Join(c.Args, " "), text) {
			return true
		}
	}
	return false
}

const adoptCompose = `{"services":{
	"web":{"image":"example.invalid/shop/app:${APP_VERSION}"},
	"pg":{"image":"example.invalid/shop/pg:16.2.0"}}}`

// adoptStackDir creates the directory of a running stack and the Docker that knows it.
func adoptStackDir(t *testing.T, compose string) (*adoptDocker, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_VERSION=1.4.2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lb, _ := json.Marshal(map[string]string{
		"com.docker.compose.project":              "shop-prod",
		"com.docker.compose.project.config_files": filepath.Join(dir, "docker-compose.yml"),
	})
	return &adoptDocker{compose: compose, ps: "0123456789ab\n", lb: string(lb) + "\n"}, dir
}

func startAdoptService(t *testing.T, bound bool, d execute.Runner) string {
	t.Helper()
	sock, _, _ := startServiceCfg(t, bound, projectSetup(t), func(c *service.Config) { c.Runner = d })
	return sock
}

func TestProjectAdoptDryRunThenStore(t *testing.T) {
	d, dir := adoptStackDir(t, adoptCompose)
	sock := startAdoptService(t, true, d)

	// A relative directory reaches the service as an absolute one.
	t.Chdir(filepath.Dir(dir))
	rel := filepath.Base(dir)
	code, stdout, stderr := runCLIArgs("project", "adopt", rel, "--dry-run", "--socket", sock, "--output", "json")
	if code != exitOK {
		t.Fatalf("dry run: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if !d.askedFor("working_dir=" + dir) {
		t.Errorf("Docker was not asked for %s: %+v", dir, d.calls)
	}
	byType := map[string][]map[string]any{}
	for _, r := range jsonRows(t, stdout) {
		byType[r["type"].(string)] = append(byType[r["type"].(string)], r)
	}
	if p := byType["project"]; len(p) != 1 || p[0]["name"] != "shop-prod" || p[0]["ref"] != dir {
		t.Errorf("project rows = %v", p)
	}
	if im := byType["image"]; len(im) != 1 || im[0]["ref"] != "example.invalid/shop/app" || im[0]["current"] != "1.4.2" ||
		im[0]["env_var"] != "APP_VERSION" || im[0]["services"] != "web" {
		t.Errorf("image rows = %v", im)
	}
	if pr := byType["problem"]; len(pr) != 1 || pr[0]["code"] != "fixed-tag" || pr[0]["hint"] == "" || pr[0]["blocking"] != false {
		t.Errorf("problem rows = %v", pr)
	}
	if st := byType["status"]; len(st) != 1 || st[0]["name"] != "dry-run" {
		t.Errorf("status rows = %v", st)
	}
	code, stdout, _ = runCLIArgs("project", "list", "--socket", sock, "--output", "json")
	if code != exitOK || strings.Contains(stdout, "shop-prod") {
		t.Fatalf("dry run stored something: %q", stdout)
	}

	code, stdout, stderr = runCLIArgs("project", "adopt", dir, "--socket", sock, "--output", "json")
	if code != exitOK || !strings.Contains(stdout, `"name":"pending approval"`) {
		t.Fatalf("adopt: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, _ = runCLIArgs("project", "show", "shop-prod", "--socket", sock, "--output", "json")
	if code != exitOK || !strings.Contains(stdout, "mode: manual") {
		t.Fatalf("show: code = %d, stdout = %q", code, stdout)
	}

	// The table shows the same.
	code, stdout, _ = runCLIArgs("project", "adopt", dir, "--dry-run", "--socket", sock)
	if code != exitOK || !strings.Contains(stdout, "already-adopted") || !strings.Contains(stdout, "dry-run") {
		t.Fatalf("table: code = %d, stdout = %q", code, stdout)
	}
	code, _, stderr = runCLIArgs("project", "adopt", dir, "--socket", sock)
	if code != exitRuntime || !strings.Contains(stderr, "not stored") {
		t.Fatalf("second adopt: code = %d, stderr = %q", code, stderr)
	}
}

func TestProjectAdoptErrors(t *testing.T) {
	d, dir := adoptStackDir(t, "")
	sock := startAdoptService(t, true, d)
	code, stdout, stderr := runCLIArgs("project", "adopt", dir, "--socket", sock, "--output", "json")
	if code != exitRuntime || !strings.Contains(stdout, "compose-config-failed") || strings.Contains(stdout+stderr, "invalid compose file") {
		t.Errorf("blocked: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, _, stderr = runCLIArgs("project", "adopt", filepath.Join(dir, "missing"), "--socket", sock)
	if code != exitRuntime || !strings.Contains(stderr, "does not exist") {
		t.Errorf("missing: code = %d, stderr = %q", code, stderr)
	}
	code, _, stderr = runCLIArgs("project", "adopt", dir, "--name", "Bad Name", "--socket", sock)
	if code != exitRuntime || !strings.Contains(stderr, "did not accept") {
		t.Errorf("bad name: code = %d, stderr = %q", code, stderr)
	}
	if code, _, _ = runCLIArgs("project", "adopt", "--socket", sock); code != exitUsage {
		t.Errorf("no directory: code = %d", code)
	}

	down := startAdoptService(t, true, &adoptDocker{fails: true})
	code, _, stderr = runCLIArgs("project", "adopt", dir, "--socket", down)
	if code != exitRuntime || !strings.Contains(stderr, "cannot ask Docker") || strings.Contains(stderr, "Cannot connect") {
		t.Errorf("docker down: code = %d, stderr = %q", code, stderr)
	}

	locked := startAdoptService(t, false, d)
	code, _, stderr = runCLIArgs("project", "adopt", dir, "--socket", locked)
	if code != exitRuntime || !strings.Contains(stderr, "vault is locked") {
		t.Errorf("locked: code = %d, stderr = %q", code, stderr)
	}
}

func TestProjectAdoptNoAccessNamesPartAndNextStep(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root passes any directory")
	}
	d, dir := adoptStackDir(t, adoptCompose)
	closed := filepath.Join(dir, "closed")
	stack := filepath.Join(closed, "demo-stack")
	if err := os.MkdirAll(stack, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(closed, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o700) })
	sock := startAdoptService(t, true, d)

	code, _, stderr := runCLIArgs("project", "adopt", stack, "--socket", sock)
	if code != exitRuntime || strings.Contains(stderr, "does not exist") ||
		!strings.Contains(stderr, "may not pass "+closed) || !strings.Contains(stderr, "sudo setfacl -m u:sysmin:x "+closed) {
		t.Errorf("no access: code = %d, stderr = %q", code, stderr)
	}
	code, _, stderr = runCLIArgs("project", "adopt", filepath.Join(dir, "missing"), "--socket", sock)
	if code != exitRuntime || !strings.Contains(stderr, "does not exist") || strings.Contains(stderr, "setfacl") {
		t.Errorf("missing: code = %d, stderr = %q", code, stderr)
	}
}
