package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// startChangelogEnv is a service with the project demo-app on version 1.0.0, whose registry points at a
// local GitHub that lists the releases v1.1.0 and v2.0.0. status makes that GitHub refuse every request.
func startChangelogEnv(t *testing.T, approve bool, status int) string {
	t.Helper()
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		var out []map[string]any
		for _, tag := range []string{"v2.0.0", "v1.1.0", "v1.0.0"} {
			out = append(out, map[string]any{"tag_name": tag, "name": "Release " + tag, "body": "notes of " + tag})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(gh.Close)
	reg := registrytest.New(t, registrytest.Config{
		User: "demo-user", Token: demoToken, Labels: map[string]string{registry.LabelSource: "https://github.com/example-org/example-app"},
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
		cfg.Changelog.BaseURL = gh.URL
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
		Fields: map[string]string{"user": "demo-user", "token": demoToken}}); err != nil {
		t.Fatal(err)
	}
	if approve {
		if _, err := approveAll(t, c); err != nil {
			t.Fatal(err)
		}
	}
	return sock
}

func TestProjectChangelogJSON(t *testing.T) {
	sock := startChangelogEnv(t, true, 0)
	code, stdout, stderr := runCLIArgs("project", "changelog", "demo-app", "--image", "app", "--socket", sock, "--output", "json")
	rows := jsonRows(t, stdout)
	if code != exitOK || len(rows) != 2 {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	for i, want := range [][3]string{{"2.0.0", "Release v2.0.0", "notes of v2.0.0"}, {"1.1.0", "Release v1.1.0", "notes of v1.1.0"}} {
		r := rows[i]
		if r["version"] != want[0] || r["name"] != want[1] || r["body"] != want[2] || r["source"] != "app" ||
			r["status"] != "ok" || r["current"] != "1.0.0" || r["target"] != "2.0.0" || r["repo"] != "example-org/example-app" {
			t.Errorf("row %d = %v", i, r)
		}
	}
	noSecretIn(t, "stdout", stdout)
}

func TestProjectChangelogTableAndTarget(t *testing.T) {
	sock := startChangelogEnv(t, true, 0)
	code, stdout, stderr := runCLIArgs("project", "changelog", "demo-app", "--to", "1.1.0", "--socket", sock)
	if code != exitOK || !strings.Contains(stdout, "notes of v1.1.0") || strings.Contains(stdout, "notes of v2.0.0") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, _, stderr = runCLIArgs("project", "changelog", "demo-app", "--image", "nope", "--socket", sock)
	if code != exitRuntime || !strings.Contains(stderr, "did not accept the request") {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
}

func TestProjectChangelogUnreadableSourceFailsTheCommand(t *testing.T) {
	sock := startChangelogEnv(t, true, http.StatusTooManyRequests)
	code, stdout, stderr := runCLIArgs("project", "changelog", "demo-app", "--socket", sock, "--output", "json")
	rows := jsonRows(t, stdout)
	if code != exitRuntime || len(rows) != 1 || rows[0]["status"] != control.ChangelogRateLimited ||
		!strings.Contains(stderr, "1 of 1 sources could not be read") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestProjectChangelogNeedsApprovalAndUnlockedVault(t *testing.T) {
	sock := startChangelogEnv(t, false, 0)
	code, stdout, stderr := runCLIArgs("project", "changelog", "demo-app", "--socket", sock)
	if code != exitRuntime || stdout != "" || !strings.Contains(stderr, "no approved state") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	locked := startService(t, false)
	code, _, stderr = runCLIArgs("project", "changelog", "demo-app", "--socket", locked)
	if code != exitRuntime || !strings.Contains(stderr, "vault is locked") {
		t.Fatalf("locked: code = %d, stderr = %q", code, stderr)
	}
	code, _, stderr = runCLIArgs("project", "changelog", "--socket", locked)
	if code != exitUsage || !strings.Contains(stderr, "exactly one project name") {
		t.Fatalf("no name: code = %d, stderr = %q", code, stderr)
	}
}
