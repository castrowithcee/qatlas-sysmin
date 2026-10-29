package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/service"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry/registrytest"
)

// okRunner answers every program of a deploy with success.
type okRunner struct{}

func (okRunner) Run(context.Context, execute.Command) (execute.Result, error) {
	return execute.Result{}, nil
}

// startDeployEnv is a service with the approved project demo-app on a local registry (1.0.0 running; 1.1.0
// and 2.0.0 available, deployed automatically) and a runner that does nothing.
func startDeployEnv(t *testing.T, mode string) string {
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
env_file: %[1]s/.env
steps:
  - type: backup
  - type: set-env
    assign: ["{var}={version}"]
  - type: command
    run: "deploy {version}"
backup:
  command: make-backup
  restore_command: restore-backup
images:
  - name: app
    ref: registry.example.com/demo/app
    env_var: DEMO_APP_TAG
    credential: demo-registry
    version_rule:
      mode: %[2]s
`, dir, mode)
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
		cfg.DeployRunner = okRunner{}
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
	if _, err := c.ApproveAll(); err != nil {
		t.Fatal(err)
	}
	return sock
}

func TestDeployStatusAndRunNowJSON(t *testing.T) {
	sock := startDeployEnv(t, "auto")
	code, stdout, stderr := runCLIArgs("deploy", "status", "demo-app", "--socket", sock, "--format", "json")
	obj := jsonObject(t, stdout)
	if code != exitOK || obj["project"] != "demo-app" || obj["running"] != false || obj["paused"] != false ||
		obj["pinned"] != false || obj["locked"] != false || obj["last_run"] != "" {
		t.Fatalf("status: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}

	code, stdout, stderr = runCLIArgs("deploy", "run-now", "demo-app", "--socket", sock, "--format", "json")
	obj = jsonObject(t, stdout)
	if code != exitOK || obj["status"] != "deployed" || obj["result"] != "succeeded" ||
		obj["changes"] != "app 1.0.0 -> 2.0.0" || obj["backup"] == "" {
		t.Fatalf("run-now: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}

	code, stdout, _ = runCLIArgs("deploy", "status", "demo-app", "--socket", sock, "--format", "json")
	obj = jsonObject(t, stdout)
	if code != exitOK || obj["sources"] != "app 2.0.0" || !strings.Contains(obj["last_run"].(string), "auto succeeded") {
		t.Errorf("status after the run: %q", stdout)
	}

	code, stdout, stderr = runCLIArgs("deploy", "run-now", "demo-app", "--socket", sock, "--format", "json")
	if obj = jsonObject(t, stdout); code != exitOK || obj["status"] != "nothing-to-deploy" {
		t.Errorf("second run-now: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}

	code, stdout, _ = runCLIArgs("deploy", "history", "demo-app", "--socket", sock, "--format", "json")
	rows := jsonRows(t, stdout)
	if code != exitOK || len(rows) != 1 || rows[0]["action"] != "auto" || rows[0]["result"] != "succeeded" ||
		rows[0]["changes"] != "app 1.0.0 -> 2.0.0" || rows[0]["duration_ms"] == nil {
		t.Errorf("history: code = %d, stdout = %q", code, stdout)
	}

	code, stdout, _ = runCLIArgs("backup", "list", "demo-app", "--socket", sock, "--format", "json")
	rows = jsonRows(t, stdout)
	if code != exitOK || len(rows) != 1 || rows[0]["backup"] == "" {
		t.Errorf("backup list: code = %d, stdout = %q", code, stdout)
	}
}

func TestDeployTables(t *testing.T) {
	sock := startDeployEnv(t, "auto")
	runCLIArgs("deploy", "run-now", "demo-app", "--socket", sock)
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"deploy", "status", "demo-app"}, []string{"project", "demo-app", "sources", "app 2.0.0"}},
		{[]string{"deploy", "history", "demo-app"}, []string{"ID", "ACTION", "RESULT", "auto", "succeeded", "app 1.0.0 -> 2.0.0"}},
		{[]string{"backup", "list", "demo-app"}, []string{"BACKUP"}},
	} {
		code, stdout, stderr := runCLIArgs(append(tc.args, "--socket", sock)...)
		if code != exitOK {
			t.Fatalf("%v: code = %d, stderr = %q", tc.args, code, stderr)
		}
		for _, w := range tc.want {
			if !strings.Contains(stdout, w) {
				t.Errorf("%v lacks %q: %s", tc.args, w, stdout)
			}
		}
	}
}

func TestDeployPauseResumePinUnpin(t *testing.T) {
	sock := startDeployEnv(t, "auto")
	cli := func(args ...string) (int, string, string) {
		return runCLIArgs(append(args, "--socket", sock, "--format", "json")...)
	}
	for _, step := range []struct{ verb, state, flag string }{
		{"pause", "paused", "paused"}, {"resume", "resumed", ""}, {"pin", "pinned", "pinned"}, {"unpin", "unpinned", ""},
	} {
		code, stdout, stderr := cli("deploy", step.verb, "demo-app")
		if obj := jsonObject(t, stdout); code != exitOK || obj["project"] != "demo-app" || obj["state"] != step.state {
			t.Fatalf("%s: code = %d, stdout = %q, stderr = %q", step.verb, code, stdout, stderr)
		}
		_, stdout, _ = cli("deploy", "status", "demo-app")
		obj := jsonObject(t, stdout)
		if step.flag != "" && obj[step.flag] != true {
			t.Errorf("after %s: %q", step.verb, stdout)
		}
		if step.flag == "" && (obj["paused"] != false || obj["pinned"] != false) {
			t.Errorf("after %s: %q", step.verb, stdout)
		}
	}

	// A paused project and a pinned one deploy nothing.
	for _, hold := range []string{"pause", "pin"} {
		skipped := map[string]string{"pause": "paused", "pin": "pinned"}[hold]
		if code, _, stderr := cli("deploy", hold, "demo-app"); code != exitOK {
			t.Fatalf("%s: %q", hold, stderr)
		}
		code, stdout, stderr := cli("deploy", "run-now", "demo-app")
		obj := jsonObject(t, stdout)
		if code != exitOK || obj["status"] != "skipped" || obj["skipped"] != skipped {
			t.Errorf("run-now after %s: code = %d, stdout = %q, stderr = %q", hold, code, stdout, stderr)
		}
		cli("deploy", map[string]string{"pause": "resume", "pin": "unpin"}[hold], "demo-app")
	}
	_, stdout, _ := cli("backup", "list", "demo-app")
	if len(jsonRows(t, stdout)) != 0 {
		t.Errorf("a held project made a backup: %q", stdout)
	}
}

func TestDeployManualModeOnlyReports(t *testing.T) {
	sock := startDeployEnv(t, "manual")
	code, stdout, stderr := runCLIArgs("deploy", "run-now", "demo-app", "--socket", sock, "--format", "json")
	if obj := jsonObject(t, stdout); code != exitOK || obj["status"] != "nothing-to-deploy" {
		t.Fatalf("run-now: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	_, stdout, _ = runCLIArgs("deploy", "status", "demo-app", "--socket", sock, "--format", "json")
	if src := jsonObject(t, stdout)["sources"]; src != "app 1.0.0 (newer: 1.1.0,2.0.0)" {
		t.Errorf("sources = %v", src)
	}
}

func TestDeployErrors(t *testing.T) {
	sock := startDeployEnv(t, "auto")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"deploy", "status", "demo-none"}, "the project does not exist"},
		{[]string{"deploy", "pause", "demo-none"}, "the project does not exist"},
		{[]string{"backup", "list", "demo-none"}, "the project does not exist"},
		{[]string{"deploy", "history", "Bad Name"}, "check the project name"},
	} {
		code, stdout, stderr := runCLIArgs(append(tc.args, "--socket", sock)...)
		if code != exitRuntime || stdout != "" || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: code = %d, stdout = %q, stderr = %q", tc.args, code, stdout, stderr)
		}
	}
	for _, args := range [][]string{{"deploy", "status"}, {"deploy", "run-now", "a", "b"}, {"backup", "list"}, {"deploy", "update", "demo-app"}, {"deploy", "restore", "demo-app"}, {"backup", "restore", "demo-app"}} {
		if code, _, _ := runCLIArgs(append(args, "--socket", sock)...); code != exitUsage {
			t.Errorf("%v: code = %d, want usage error", args, code)
		}
	}
	if code, _, stderr := runCLIArgs("deploy", "status", "demo-app", "--socket", filepath.Join(t.TempDir(), "none.sock")); code != exitRuntime ||
		!strings.Contains(stderr, "is the service running") {
		t.Errorf("no service: code = %d, stderr = %q", code, stderr)
	}
}

func TestDeployFailedRunExitsWithAnError(t *testing.T) {
	// A runner that fails the deploy command ends the run with a rollback failure.
	reg := registrytest.New(t, registrytest.Config{User: "demo-user", Token: demoToken,
		Repos: map[string][]string{"demo/app": {"1.0.0", "1.1.0"}}})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DEMO_APP_TAG=1.0.0\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	project := fmt.Sprintf("schema: 1\nname: demo-app\ndir: %[1]s\nenv_file: %[1]s/.env\nsteps:\n  - type: command\n    run: \"deploy {version}\"\nimages:\n  - name: app\n    ref: registry.example.com/demo/app\n    env_var: DEMO_APP_TAG\n    credential: demo-registry\n    version_rule:\n      mode: auto\n", dir)
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
		cfg.DeployRunner = failRunner{}
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
	if _, err := c.ApproveAll(); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLIArgs("deploy", "run-now", "demo-app", "--socket", sock, "--format", "json")
	obj := jsonObject(t, stdout)
	if code != exitRuntime || obj["status"] != "deployed" || obj["result"] == "succeeded" || !strings.Contains(stderr, "the run ended with") {
		t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	for _, s := range []string{stdout, stderr} {
		if strings.Contains(s, demoToken) {
			t.Errorf("a secret is in the output: %q", s)
		}
	}
}

type failRunner struct{}

func (failRunner) Run(context.Context, execute.Command) (execute.Result, error) {
	return execute.Result{ExitCode: 1}, &execute.ExitError{Code: 1}
}
