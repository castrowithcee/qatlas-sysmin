package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/service"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry/registrytest"
)

const sourceWrongToken = "wrong-token-not-real"

// sourceEnv is a service with the project demo-app on a registry that the test serves locally.
type sourceEnv struct {
	sock string
	reg  *registrytest.Server
}

func startSourceEnv(t *testing.T, token string, approve bool) *sourceEnv {
	t.Helper()
	reg := registrytest.New(t, registrytest.Config{
		User: "demo-user", Token: demoToken, Labels: map[string]string{registry.LabelSource: "https://example.invalid/demo/app"},
		Repos: map[string][]string{"demo/app": {"1.0.0", "1.1.0", "2.0.0"}},
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DEMO_APP_TAG=1.0.0\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	project := fmt.Sprintf(`schema: 1
name: demo-app
dir: %[1]s
template: image-tag
env_file: %[1]s/.env
images:
  - name: app
    ref: registry.example.com/demo/app
    env_var: DEMO_APP_TAG
    services: [app]
    credential: demo-registry
`, dir)
	sock, _, _ := startServiceCfg(t, true, func(cfgDir string) {
		for rel, content := range map[string]string{"sysmin.yaml": "schema: 1\n", "projects/demo-app.yaml": project} {
			path := filepath.Join(cfgDir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
				t.Fatal(err)
			}
		}
	}, func(cfg *service.Config) {
		cfg.SourceRegistry.BaseURL = func(string) string { return reg.URL }
	})
	c, err := client.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.OpenSession(demoPass); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetCredential(control.CredentialSetParams{Name: "demo-registry", Kind: "registry",
		Fields: map[string]string{"user": "demo-user", "token": token}}); err != nil {
		t.Fatal(err)
	}
	if approve {
		if _, err := c.ApproveAll(); err != nil {
			t.Fatal(err)
		}
	}
	return &sourceEnv{sock: sock, reg: reg}
}

func noSecretIn(t *testing.T, what, text string) {
	t.Helper()
	for _, secret := range []string{demoToken, sourceWrongToken, registrytest.Bearer} {
		if strings.Contains(text, secret) {
			t.Errorf("%s contains a secret: %q", what, text)
		}
	}
}

func TestSourceTestJSON(t *testing.T) {
	e := startSourceEnv(t, demoToken, true)
	code, stdout, stderr := runCLIArgs("source", "test", "demo-app", "--socket", e.sock, "--format", "json")
	rows := jsonRows(t, stdout)
	if code != exitOK || len(rows) != 1 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	r := rows[0]
	for field, want := range map[string]string{
		"source": "app", "kind": "image", "ref": "registry.example.com/demo/app", "status": "ok", "current": "1.0.0",
		"available": "1.1.0,2.0.0", "target": "2.0.0", "source_repo": "https://example.invalid/demo/app", "problems": "", "message": "",
	} {
		if r[field] != want {
			t.Errorf("%s = %v, want %q", field, r[field], want)
		}
	}
	if d, _ := r["digest"].(string); !strings.HasPrefix(d, "sha256:") {
		t.Errorf("digest = %v", r["digest"])
	}
	noSecretIn(t, "stdout", stdout)
	noSecretIn(t, "stderr", stderr)
}

func TestSourceTestTable(t *testing.T) {
	e := startSourceEnv(t, demoToken, true)
	code, stdout, stderr := runCLIArgs("source", "test", "demo-app", "--socket", e.sock)
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"SOURCE", "STATUS", "TARGET", "app", "image", "1.1.0,2.0.0", "2.0.0"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("table lacks %q: %s", want, stdout)
		}
	}
	noSecretIn(t, "stdout", stdout)
}

func TestSourceTestNeedsAnApprovedProject(t *testing.T) {
	e := startSourceEnv(t, demoToken, false)
	code, stdout, stderr := runCLIArgs("source", "test", "demo-app", "--socket", e.sock)
	if code != exitRuntime || stdout != "" || !strings.Contains(stderr, "no approved state") || !strings.Contains(stderr, "approve") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if got := e.reg.Requests(); len(got) != 0 {
		t.Fatalf("the registry was asked: %v", got)
	}
}

func TestSourceTestUnreadableSourceFailsTheCommand(t *testing.T) {
	e := startSourceEnv(t, sourceWrongToken, true)
	code, stdout, stderr := runCLIArgs("source", "test", "demo-app", "--socket", e.sock, "--format", "json")
	rows := jsonRows(t, stdout)
	if code != exitRuntime || len(rows) != 1 || rows[0]["status"] != "denied" || !strings.Contains(rows[0]["message"].(string), "denied") ||
		!strings.Contains(stderr, "1 of 1 sources could not be read") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	noSecretIn(t, "stdout", stdout)
	noSecretIn(t, "stderr", stderr)
}

func TestSourceTestLockedVaultAndUsage(t *testing.T) {
	sock := startService(t, false)
	code, _, stderr := runCLIArgs("source", "test", "demo-app", "--socket", sock)
	if code != exitRuntime || !strings.Contains(stderr, "vault is locked") {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	code, _, stderr = runCLIArgs("source", "test", "--socket", sock)
	if code != exitUsage || !strings.Contains(stderr, "exactly one project name") {
		t.Fatalf("no name: code = %d, stderr = %q", code, stderr)
	}
}
