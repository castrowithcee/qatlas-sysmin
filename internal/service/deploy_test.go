package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/gitrepo"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry/registrytest"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// deployRunner records the programs of a deploy and answers them with success unless handle says
// otherwise.
type deployRunner struct {
	mu     sync.Mutex
	lines  []string
	handle func(line string) (execute.Result, error)
}

func (r *deployRunner) Run(_ context.Context, c execute.Command) (execute.Result, error) {
	line := c.Name + " " + strings.Join(c.Args, " ")
	if c.Name == "/bin/sh" && len(c.Args) == 2 {
		line = c.Args[1]
	}
	r.mu.Lock()
	r.lines = append(r.lines, line)
	handle := r.handle
	r.mu.Unlock()
	if handle != nil {
		return handle(line)
	}
	return execute.Result{}, nil
}

func (r *deployRunner) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.lines)
}

func (r *deployRunner) count(prefix string) int {
	n := 0
	for _, l := range r.calls() {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func deployProject(name, dir string) string {
	return fmt.Sprintf(`schema: 1
name: %[1]s
dir: %[2]s
env_file: %[2]s/.env
steps:
  - type: backup
  - type: set-env
    assign: ["{var}={version}"]
  - type: command
    run: "deploy %[1]s {version}"
backup:
  command: make-backup
  restore_command: restore-backup
images:
  - name: app
    ref: registry.example.com/demo/app
    env_var: DEMO_APP_TAG
    credential: demo-registry
    version_rule:
      mode: auto
`, name, dir)
}

type deployFixture struct {
	*env
	runner *deployRunner
	dirs   map[string]string
}

// newDeployFixture starts a service with the projects demo-app and demo-two on a local registry that
// serves 1.0.0, 1.1.0 and 2.0.0 and a fake runner; both projects run on 1.0.0. Nothing is approved yet.
func newDeployFixture(t *testing.T, bind bool, mods ...func(*Config)) *deployFixture {
	t.Helper()
	reg := registrytest.New(t, demoRegistryConfig(demoSecret))
	f := &deployFixture{runner: &deployRunner{}, dirs: map[string]string{}}
	mods = append(mods, func(cfg *Config) {
		cfg.SourceRegistry.BaseURL = func(string) string { return reg.URL }
		cfg.DeployRunner = f.runner
	})
	f.env = newEnv(t, true, bind, mods...)
	f.write(t, "sysmin.yaml", "schema: 1\n")
	for _, name := range []string{"demo-app", "demo-two"} {
		dir := t.TempDir()
		f.dirs[name] = dir
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DEMO_APP_TAG=1.0.0\n"), 0o640); err != nil {
			t.Fatal(err)
		}
		f.write(t, "projects/"+name+".yaml", deployProject(name, dir))
	}
	if bind {
		f.addCredential(t)
	}
	f.start(t)
	return f
}

func (f *deployFixture) addCredential(t *testing.T) {
	t.Helper()
	if _, err := f.svc.vault.Add("demo-registry", vault.KindRegistry, map[string]string{
		vault.FieldUser: "demo-user", vault.FieldToken: demoSecret}); err != nil {
		t.Fatal(err)
	}
}

func (f *deployFixture) envTag(t *testing.T, project string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dirs[project], ".env"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func (f *deployFixture) approved(t *testing.T) (*client.Client, *client.Client) {
	t.Helper()
	admin := f.admin(t)
	if _, err := admin.ApproveAll(); err != nil {
		t.Fatal(err)
	}
	return admin, f.dial(t)
}

func appUpdate(project, version string) control.ProjectUpdate {
	return control.ProjectUpdate{Project: project, Versions: []control.DeployVersion{{Kind: control.SourceImage, Name: "app", Version: version}}}
}

func TestDeployUpdateAndRestoreNeedTheAdminSession(t *testing.T) {
	f := newDeployFixture(t, true)
	_, reader := f.approved(t)
	_, err := reader.DeployUpdate([]control.ProjectUpdate{appUpdate("demo-app", "1.1.0")})
	wantCode(t, err, control.CodeAdminRequired)
	_, err = reader.DeployRestore("demo-app", "20260301T120000Z")
	wantCode(t, err, control.CodeAdminRequired)
	if n := len(f.runner.calls()); n != 0 {
		t.Fatalf("a refused request ran %v", f.runner.calls())
	}
	if tag := f.envTag(t, "demo-app"); tag != "DEMO_APP_TAG=1.0.0" {
		t.Fatalf("env = %q", tag)
	}
}

func TestDeployUpdateWithTheAdminSession(t *testing.T) {
	f := newDeployFixture(t, true)
	admin, reader := f.approved(t)

	res, err := admin.DeployUpdate([]control.ProjectUpdate{appUpdate("demo-app", "1.1.0"), appUpdate("demo-two", "9.9.9")})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 2 {
		t.Fatalf("results = %+v", res.Results)
	}
	first, second := res.Results[0], res.Results[1]
	if first.Project != "demo-app" || first.Run == nil || first.Run.Result != "succeeded" || first.Run.Action != "manual" ||
		len(first.Run.Changes) != 1 || first.Run.Changes[0] != (control.DeployChange{Kind: "image", Name: "app", From: "1.0.0", To: "1.1.0"}) {
		t.Errorf("first = %+v", first)
	}
	if second.Project != "demo-two" || second.Run != nil || second.Error != control.CodeVersionRefused {
		t.Errorf("second = %+v", second)
	}
	if f.envTag(t, "demo-app") != "DEMO_APP_TAG=1.1.0" || f.envTag(t, "demo-two") != "DEMO_APP_TAG=1.0.0" {
		t.Errorf("env files: %q %q", f.envTag(t, "demo-app"), f.envTag(t, "demo-two"))
	}
	if f.runner.count("deploy demo-app '1.1.0'") != 1 || f.runner.count("deploy demo-two") != 0 {
		t.Errorf("calls = %v", f.runner.calls())
	}

	st, err := reader.DeployStatus("demo-app")
	if err != nil || st.Running || st.Paused || len(st.Sources) != 1 || st.Sources[0].Current != "1.1.0" || st.LastRun == nil || st.LastRun.Result != "succeeded" {
		t.Errorf("status = %+v %v", st, err)
	}
	h, err := reader.DeployHistory("demo-app")
	if err != nil || len(h.Runs) != 1 || h.Runs[0].Backup == "" {
		t.Fatalf("history = %+v %v", h, err)
	}

	// Bad requests are refused before anything runs.
	for _, u := range [][]control.ProjectUpdate{
		nil,
		{{Project: "demo-app"}},
		{{Project: "Bad Name", Versions: appUpdate("x", "1").Versions}},
		{appUpdate("demo-app", "1.1.0"), appUpdate("demo-app", "2.0.0")},
		{{Project: "demo-app", Versions: []control.DeployVersion{{Kind: "other", Name: "app", Version: "1"}}}},
		{{Project: "demo-app", Versions: []control.DeployVersion{{Kind: "git", Name: "app", Version: "1"}}}},
	} {
		if _, err := admin.DeployUpdate(u); err == nil {
			t.Errorf("accepted %+v", u)
		} else {
			wantCode(t, err, control.CodeBadRequest)
		}
	}
	// An unknown project is an outcome of its own, not a failure of the request.
	res, err = admin.DeployUpdate([]control.ProjectUpdate{appUpdate("demo-none", "1.1.0")})
	if err != nil || len(res.Results) != 1 || res.Results[0].Error != control.CodeNotFound {
		t.Errorf("unknown project: %+v %v", res, err)
	}
}

func TestDeployRestore(t *testing.T) {
	f := newDeployFixture(t, true)
	admin, reader := f.approved(t)
	if _, err := admin.DeployUpdate([]control.ProjectUpdate{appUpdate("demo-app", "1.1.0")}); err != nil {
		t.Fatal(err)
	}
	names, err := reader.Backups("demo-app")
	if err != nil || len(names) != 1 {
		t.Fatalf("backups = %v %v", names, err)
	}
	if other, err := reader.Backups("demo-two"); err != nil || len(other) != 0 {
		t.Errorf("backups of demo-two = %v %v", other, err)
	}

	_, err = admin.DeployRestore("demo-app", "20990101T000000Z")
	wantCode(t, err, control.CodeBackupUnknown)
	_, err = admin.DeployRestore("demo-none", names[0])
	wantCode(t, err, control.CodeNotFound)
	_, err = admin.DeployRestore("demo-app", "")
	wantCode(t, err, control.CodeBadRequest)

	res, err := admin.DeployRestore("demo-app", names[0])
	if err != nil || res.Run == nil || res.Run.Result != "succeeded" || res.Run.Action != "restore" || res.Run.Backup != names[0] {
		t.Fatalf("restore = %+v %v", res, err)
	}
	if f.runner.count("restore-backup") != 1 {
		t.Errorf("calls = %v", f.runner.calls())
	}
}

func TestDeployControlsNeedNoAdminSession(t *testing.T) {
	f := newDeployFixture(t, true)
	_, reader := f.approved(t)
	admin := f.admin(t)

	if err := reader.DeployControl(control.OpDeployPause, "demo-app"); err != nil {
		t.Fatal(err)
	}
	st, err := reader.DeployStatus("demo-app")
	if err != nil || !st.Paused {
		t.Fatalf("status = %+v %v", st, err)
	}
	res, err := reader.DeployRunNow("demo-app")
	if err != nil || res.Skipped != control.SkipPaused || res.Run != nil {
		t.Fatalf("run-now while paused: %+v %v", res, err)
	}
	if r, err := admin.DeployUpdate([]control.ProjectUpdate{appUpdate("demo-app", "1.1.0")}); err != nil || r.Results[0].Error != control.CodePaused {
		t.Errorf("update while paused: %+v %v", r, err)
	}
	if n := f.runner.count("deploy"); n != 0 {
		t.Fatalf("a paused project deployed: %v", f.runner.calls())
	}
	if err := reader.DeployControl(control.OpDeployResume, "demo-app"); err != nil {
		t.Fatal(err)
	}

	if err := reader.DeployControl(control.OpDeployPin, "demo-app"); err != nil {
		t.Fatal(err)
	}
	if st, _ := reader.DeployStatus("demo-app"); !st.Pinned {
		t.Errorf("not pinned: %+v", st)
	}
	if res, err := reader.DeployRunNow("demo-app"); err != nil || res.Skipped != control.SkipPinned {
		t.Errorf("run-now while pinned: %+v %v", res, err)
	}
	if r, _ := admin.DeployUpdate([]control.ProjectUpdate{appUpdate("demo-app", "1.1.0")}); r.Results[0].Error != control.CodePinned {
		t.Errorf("update while pinned: %+v", r)
	}
	if n := f.runner.count("deploy"); n != 0 {
		t.Fatalf("a pinned project deployed: %v", f.runner.calls())
	}
	if err := reader.DeployControl(control.OpDeployUnpin, "demo-app"); err != nil {
		t.Fatal(err)
	}

	// The version rule of the project allows the highest version without a human.
	res, err = reader.DeployRunNow("demo-app")
	if err != nil || res.Run == nil || res.Run.Result != "succeeded" || res.Run.Action != "auto" || res.Run.Changes[0].To != "2.0.0" {
		t.Fatalf("run-now: %+v %v", res, err)
	}
	if res, err := reader.DeployRunNow("demo-app"); err != nil || res.Run != nil || res.Skipped != "" {
		t.Errorf("run-now on the newest version: %+v %v", res, err)
	}
	if h, err := reader.DeployHistory("demo-app"); err != nil || len(h.Runs) != 1 {
		t.Errorf("history = %+v %v", h, err)
	}

	// Unknown and invalid names.
	_, err = reader.DeployStatus("demo-none")
	wantCode(t, err, control.CodeNotFound)
	wantCode(t, reader.DeployControl(control.OpDeployPause, "demo-none"), control.CodeNotFound)
	_, err = reader.Backups("Bad Name")
	wantCode(t, err, control.CodeBadRequest)
	wantCode(t, reader.DeployControl(control.OpDeployPin, ""), control.CodeBadRequest)
}

func TestDeployRequestsAreLogged(t *testing.T) {
	f := newDeployFixture(t, true)
	admin, reader := f.approved(t)
	if _, err := reader.DeployStatus("demo-app"); err != nil {
		t.Fatal(err)
	}
	if err := reader.DeployControl(control.OpDeployPause, "demo-app"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.DeployUpdate([]control.ProjectUpdate{appUpdate("demo-two", "1.1.0"), appUpdate("demo-app", "1.1.0")}); err != nil {
		t.Fatal(err)
	}
	f.cancel()
	<-f.done
	f.done <- nil

	type key struct{ action, project, result string }
	got := map[key]bool{}
	for _, en := range readEntries(t, filepath.Join(f.cfg.StateDir, "logs")) {
		got[key{en.Action, en.Project, en.Result}] = true
		if strings.HasPrefix(en.Action, "deploy.") && (en.ClientUID == nil || *en.ClientUID != uint32(os.Getuid())) {
			t.Errorf("entry %+v names another user", en)
		}
	}
	for _, k := range []key{
		{auditlog.OperationDeployRead, "demo-app", auditlog.ResultSuccess},
		{auditlog.OperationDeployControl, "demo-app", auditlog.ResultSuccess},
		{auditlog.OperationDeployRun, "demo-two", auditlog.ResultSuccess},
		{auditlog.OperationDeployRun, "demo-app", auditlog.ResultFailure}, // paused
	} {
		if !got[k] {
			t.Errorf("no entry for %+v in %v", k, got)
		}
	}
}

func TestDeployErrorsKeepSecretsOut(t *testing.T) {
	f := newDeployFixture(t, true)
	admin, reader := f.approved(t)
	f.runner.mu.Lock()
	f.runner.handle = func(line string) (execute.Result, error) {
		if strings.HasPrefix(line, "deploy") {
			return execute.Result{Output: []byte("login failed with " + demoSecret + "\n"), ExitCode: 1}, &execute.ExitError{Code: 1}
		}
		return execute.Result{}, nil
	}
	f.runner.mu.Unlock()
	res, err := admin.DeployUpdate([]control.ProjectUpdate{appUpdate("demo-app", "1.1.0")})
	if err != nil || res.Results[0].Run == nil || res.Results[0].Run.Result != "rollback-failed" {
		t.Fatalf("update = %+v %v", res, err)
	}
	h, err := reader.DeployHistory("demo-app")
	if err != nil {
		t.Fatal(err)
	}
	if text := fmt.Sprintf("%+v %+v", res, h); strings.Contains(text, demoSecret) {
		t.Errorf("a secret is in the answer: %s", text)
	}
	logs, _ := filepath.Glob(filepath.Join(f.cfg.StateDir, "deploy", "demo-app", "runs", "*.log"))
	if len(logs) != 1 {
		t.Fatalf("run logs = %v", logs)
	}
	data, _ := os.ReadFile(logs[0])
	if strings.Contains(string(data), demoSecret) || !strings.Contains(string(data), "[redacted]") {
		t.Errorf("run log = %s", data)
	}
}

func TestSlowRunIsPendingAndNotCancelled(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	f := newDeployFixture(t, true, func(cfg *Config) { cfg.DeployWait = 100 * time.Millisecond })
	f.runner.mu.Lock()
	f.runner.handle = func(line string) (execute.Result, error) {
		if strings.HasPrefix(line, "deploy") {
			started <- struct{}{}
			<-release
		}
		return execute.Result{}, nil
	}
	f.runner.mu.Unlock()
	admin, reader := f.approved(t)

	res, err := reader.DeployRunNow("demo-app")
	if err != nil || !res.Pending || res.Run != nil {
		t.Fatalf("run-now = %+v %v", res, err)
	}
	<-started
	if st, _ := reader.DeployStatus("demo-app"); !st.Running {
		t.Errorf("status = %+v", st)
	}
	_, err = reader.DeployRunNow("demo-app")
	wantCode(t, err, control.CodeBusy)
	ur, err := admin.DeployUpdate([]control.ProjectUpdate{appUpdate("demo-app", "1.1.0")})
	if err != nil || ur.Results[0].Error != control.CodeBusy {
		t.Errorf("update during a run: %+v %v", ur, err)
	}

	// Shutting down waits for the run; it ends with its normal result.
	stopped := make(chan struct{})
	go func() { f.cancel(); <-f.done; f.done <- nil; close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("the service stopped under a running deploy")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the service did not stop after the deploy")
	}
	data, err := os.ReadFile(filepath.Join(f.cfg.StateDir, "deploy", "demo-app", "state.json"))
	if err != nil || !strings.Contains(string(data), `"result": "succeeded"`) {
		t.Errorf("state = %s %v", data, err)
	}
}

func TestLockedVaultStartsNoRunAndUnlockEnablesIt(t *testing.T) {
	f := newDeployFixture(t, false)
	// The planner runs; a trigger while the vault is locked checks nothing and deploys nothing.
	trigger := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !f.svc.engine.Trigger("demo-app") {
			if time.Now().After(deadline) {
				t.Fatal("the planner does not run")
			}
			time.Sleep(5 * time.Millisecond)
		}
		for {
			if st, err := f.svc.engine.Status("demo-app"); err != nil || !st.Running {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	trigger()
	if n := len(f.runner.calls()); n != 0 {
		t.Fatalf("a locked vault ran %v", f.runner.calls())
	}
	st, err := f.svc.engine.Status("demo-app")
	if err != nil || !st.LastCheck.IsZero() {
		t.Fatalf("a locked vault checked: %+v %v", st, err)
	}

	c := f.dial(t)
	if err := c.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	f.addCredential(t)
	if _, err := f.admin(t).ApproveAll(); err != nil {
		t.Fatal(err)
	}
	// Only auto mode deploys: the highest version is installed on the next check, without a restart.
	trigger()
	if f.runner.count("deploy demo-app '2.0.0'") != 1 {
		t.Fatalf("calls = %v", f.runner.calls())
	}
	if f.envTag(t, "demo-app") != "DEMO_APP_TAG=2.0.0" {
		t.Errorf("env = %s", f.envTag(t, "demo-app"))
	}

	// The planner ends with Serve.
	f.cancel()
	if err := <-f.done; err != nil {
		t.Fatal(err)
	}
	f.done <- nil
	if f.svc.engine.Trigger("demo-app") {
		t.Error("the planner still runs")
	}
}

func TestGitAuthRunnerGivesAGitCallItsLoginForOneCall(t *testing.T) {
	var seenEnv []string
	var seenDir string
	inner := &deployRunner{}
	var runner execute.Runner = &gitAuthRunner{
		inner: capture{inner, func(c execute.Command) {
			seenEnv = c.Env
			for _, e := range c.Env {
				if v, ok := strings.CutPrefix(e, "GIT_ASKPASS="); ok {
					seenDir = filepath.Dir(v)
					if _, err := os.Stat(v); err != nil {
						t.Errorf("the askpass script is missing during the call: %v", err)
					}
				}
			}
		}},
		use: true, url: "https://git.example.invalid/demo/app.git",
		cred: gitrepo.Credential{Kind: gitrepo.KindHTTPS, User: "demo-user", Token: demoSecret},
	}
	if _, err := runner.Run(context.Background(), execute.Command{Name: "git", Args: []string{"fetch"}, Env: []string{"A=b"}}); err != nil {
		t.Fatal(err)
	}
	if seenDir == "" || !slices.Contains(seenEnv, "A=b") {
		t.Fatalf("env = %v", seenEnv)
	}
	if _, err := os.Stat(seenDir); !os.IsNotExist(err) {
		t.Errorf("the credential files outlive the call: %v", err)
	}
	for _, e := range seenEnv {
		if strings.Contains(e, demoSecret) {
			t.Errorf("the token is in the environment: %s", e)
		}
	}

	// Other programs get nothing; a login that does not fit the address fails without the secret.
	seenEnv = nil
	_, _ = runner.Run(context.Background(), execute.Command{Name: "/bin/sh", Args: []string{"-c", "x"}})
	if len(seenEnv) != 0 {
		t.Errorf("a shell got %v", seenEnv)
	}
	bad := &gitAuthRunner{inner: inner, use: true, url: "ssh://git" + "@" + "git.example.invalid/demo/app.git",
		cred: gitrepo.Credential{Kind: gitrepo.KindHTTPS, User: "demo-user", Token: demoSecret}}
	_, err := bad.Run(context.Background(), execute.Command{Name: "git"})
	if err == nil || strings.Contains(err.Error(), demoSecret) {
		t.Errorf("err = %v", err)
	}
}

type capture struct {
	execute.Runner
	fn func(execute.Command)
}

func (c capture) Run(ctx context.Context, cmd execute.Command) (execute.Result, error) {
	c.fn(cmd)
	return c.Runner.Run(ctx, cmd)
}
