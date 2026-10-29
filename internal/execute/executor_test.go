package execute

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
)

func compose(typ config.StepType, files []string, project string, services ...string) config.ResolvedStep {
	return config.ResolvedStep{Type: typ, Timeout: time.Minute, Dir: "/srv/example/shop", ComposeFiles: files,
		ComposeProject: project, Services: services}
}

func TestComposeCommands(t *testing.T) {
	cases := []struct {
		name string
		st   config.ResolvedStep
		want []string
	}{
		{"pull files and project", compose(config.StepComposePull, []string{"/a.yml", "/b.yml"}, "shop", "app", "db"),
			[]string{"compose", "-f", "/a.yml", "-f", "/b.yml", "-p", "shop", "pull", "app", "db"}},
		{"up plain", compose(config.StepComposeUp, nil, "", "app"), []string{"compose", "up", "-d", "app"}},
		{"up project only all services", compose(config.StepComposeUp, nil, "shop"), []string{"compose", "-p", "shop", "up", "-d"}},
		{"pull files only", compose(config.StepComposePull, []string{"/a.yml"}, "", "app"), []string{"compose", "-f", "/a.yml", "pull", "app"}},
	}
	for _, c := range cases {
		fr := &fakeRunner{}
		ex := &Executor{Runner: fr}
		if _, err := ex.Execute(context.Background(), config.Plan{Project: "shop", Steps: []config.ResolvedStep{c.st}}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(fr.calls) != 1 || fr.calls[0].Name != "docker" || !reflect.DeepEqual(fr.calls[0].Args, c.want) || fr.calls[0].Dir != "/srv/example/shop" {
			t.Errorf("%s: %+v", c.name, fr.calls)
		}
	}
}

func TestComposeRejectsOptionLikeService(t *testing.T) {
	fr := &fakeRunner{}
	_, err := (&Executor{Runner: fr}).Execute(context.Background(), config.Plan{Steps: []config.ResolvedStep{compose(config.StepComposeUp, nil, "", "--rm")}})
	if err == nil || len(fr.calls) != 0 {
		t.Errorf("err=%v calls=%v", err, fr.calls)
	}
}

func TestStopsAtFirstFailure(t *testing.T) {
	fr := &fakeRunner{handle: func(c Command) (Result, error) {
		if len(c.Args) > 1 && c.Args[1] == "pull" {
			return Result{Output: []byte("boom\n"), ExitCode: 3}, nil
		}
		return Result{}, nil
	}}
	var log bytes.Buffer
	ex := &Executor{Runner: fr, Log: &log}
	plan := config.Plan{Project: "shop", Steps: []config.ResolvedStep{
		compose(config.StepComposeUp, nil, "", "a"),
		compose(config.StepComposePull, nil, "", "a"),
		compose(config.StepComposeUp, nil, "", "b"),
	}}
	rep, err := ex.Execute(context.Background(), plan)
	var se *StepError
	if !errors.As(err, &se) || se.Index != 1 || se.Type != config.StepComposePull || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("err = %v", err)
	}
	if len(fr.calls) != 2 {
		t.Errorf("calls = %d", len(fr.calls))
	}
	got := []string{rep.Steps[0].Status, rep.Steps[1].Status, rep.Steps[2].Status}
	if !reflect.DeepEqual(got, []string{StatusOK, StatusFailed, StatusSkipped}) {
		t.Errorf("statuses %v", got)
	}
	if rep.Steps[1].Output != "boom\n" || rep.Steps[1].Error == "" {
		t.Errorf("%+v", rep.Steps[1])
	}
	for _, want := range []string{"step=2/3", "exit=3", "result=failed", "boom"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, log.String())
		}
	}
}

func TestOutputIsTruncated(t *testing.T) {
	big := bytes.Repeat([]byte("0123456789abcdef"), 8192) // 128 KiB
	fr := &fakeRunner{handle: func(Command) (Result, error) { return Result{Output: big}, nil }}
	var log bytes.Buffer
	ex := &Executor{Runner: fr, Log: &log}
	rep, err := ex.Execute(context.Background(), config.Plan{Steps: []config.ResolvedStep{compose(config.StepComposeUp, nil, "")}})
	if err != nil {
		t.Fatal(err)
	}
	out := rep.Steps[0].Output
	if !strings.HasPrefix(out, "0123456789abcdef") || !strings.Contains(out, "[output truncated: 65536 bytes omitted]") || len(out) > DefaultMaxOutput+100 {
		t.Errorf("len=%d tail=%q", len(out), out[len(out)-60:])
	}
	if log.Len() > DefaultMaxOutput+4096 || !strings.Contains(log.String(), "[output truncated") {
		t.Errorf("log len %d", log.Len())
	}
}

func TestRedactionOfLogAndReport(t *testing.T) {
	var r redact.Redactor
	r.Add("s3cr3t-token")
	fr := &fakeRunner{handle: func(Command) (Result, error) { return Result{Output: []byte("token s3cr3t-token\n"), ExitCode: 1}, nil }}
	var log bytes.Buffer
	rep, err := (&Executor{Runner: fr, Log: &log, Redactor: &r}).Execute(context.Background(), config.Plan{Steps: []config.ResolvedStep{compose(config.StepComposeUp, nil, "")}})
	if err == nil {
		t.Fatal("expected failure")
	}
	if strings.Contains(log.String()+rep.Steps[0].Output, "s3cr3t-token") || !strings.Contains(rep.Steps[0].Output, redact.Marker) {
		t.Errorf("log=%q out=%q", log.String(), rep.Steps[0].Output)
	}
}

func TestDryRunExecutesNothing(t *testing.T) {
	envFile := writeFile(t, "A=1\n", 0o600)
	plan := config.Plan{Project: "shop", Steps: []config.ResolvedStep{
		{Type: config.StepSetEnv, Timeout: time.Second, EnvFile: envFile, Assignments: []config.Assignment{{Key: "A", Value: "1.2.3"}}},
		compose(config.StepComposePull, []string{"/a", "/b"}, "name", "app", "db"),
		{Type: config.StepGitCheckout, Timeout: time.Second, Dir: "/srv/example/site", Tag: "v1.0.0"},
		{Type: config.StepCommand, Timeout: time.Second, Dir: "/srv/example/shop", Shell: "echo 'x y'"},
		{Type: config.StepBackup, Timeout: time.Second, Dir: "/srv/example/shop", Backup: &config.BackupParams{Command: "tar -czf \"$SYSMIN_BACKUP_DIR/a.tgz\" x", Keep: 3}},
	}}
	fr := &fakeRunner{}
	var out bytes.Buffer
	if err := (&Executor{Runner: fr}).WriteDryRun(&out, plan); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 0 || readFile(t, envFile) != "A=1\n" {
		t.Error("dry run had an effect")
	}
	entries, _ := os.ReadDir(filepath.Dir(envFile))
	if len(entries) != 1 {
		t.Errorf("files: %v", entries)
	}
	for _, want := range []string{
		"set A=1.2.3 in " + envFile,
		"docker compose -f /a -f /b -p name pull app db (in /srv/example/shop)",
		"git checkout --detach refs/tags/v1.0.0 -- (in /srv/example/site)",
		`/bin/sh -c 'echo '\''x y'\''' (in /srv/example/shop)`,
		"backup command in /var/lib/sysmin/backups/shop/<timestamp>/: /bin/sh -c ",
		"keep last 3 backups of shop",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("dry run lacks %q:\n%s", want, out.String())
		}
	}
}

func TestSetEnvStepReportsPrevious(t *testing.T) {
	p := writeFile(t, "A=1\n", 0o600)
	rep, err := (&Executor{Runner: &fakeRunner{}}).Execute(context.Background(), config.Plan{Steps: []config.ResolvedStep{
		{Type: config.StepSetEnv, Timeout: time.Second, EnvFile: p, Assignments: []config.Assignment{{Key: "A", Value: "2"}}}}})
	if err != nil || !reflect.DeepEqual(rep.Steps[0].Previous, []EnvChange{{"A", "1", true}}) || readFile(t, p) != "A=2\n" {
		t.Errorf("%v %+v", err, rep.Steps[0])
	}
}

func TestTimeoutKillsHangingStepAndChildren(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	st := config.ResolvedStep{Type: config.StepCommand, Timeout: 300 * time.Millisecond, Dir: dir,
		Shell: "sleep 30 & echo $! > pid; wait"}
	start := time.Now()
	rep, err := (&Executor{Runner: &ExecRunner{KillGrace: time.Second}}).Execute(context.Background(), config.Plan{Steps: []config.ResolvedStep{st}})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
	if rep.Steps[0].Status != StatusFailed {
		t.Errorf("%+v", rep.Steps[0])
	}
	b, rerr := os.ReadFile(pidFile)
	if rerr != nil {
		t.Fatal(rerr)
	}
	var pid int
	for _, c := range strings.TrimSpace(string(b)) {
		pid = pid*10 + int(c-'0')
	}
	deadline := time.Now().Add(2 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("child %d still running", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// processAlive treats a zombie as gone.
func processAlive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	b, err := os.ReadFile("/proc/" + itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	s := string(b)
	return !strings.HasPrefix(s[strings.LastIndexByte(s, ')')+2:], "Z")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}

func TestExecRunnerLimitsOutput(t *testing.T) {
	r := &ExecRunner{MaxOutput: 100}
	res, err := r.Run(context.Background(), Command{Name: "/bin/sh", Args: []string{"-c", "head -c 1000 /dev/zero | tr '\\0' x; echo err >&2"}})
	if err != nil || len(res.Output) != 100 || res.Omitted != 1004-100 {
		t.Errorf("err=%v len=%d omitted=%d", err, len(res.Output), res.Omitted)
	}
}

func TestExecRunnerExitCode(t *testing.T) {
	res, err := (&ExecRunner{}).Run(context.Background(), Command{Name: "/bin/sh", Args: []string{"-c", "echo hi; exit 7"}})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 7 || res.ExitCode != 7 || string(res.Output) != "hi\n" {
		t.Errorf("%v %+v", err, res)
	}
}

// Placeholder values full of shell metacharacters arrive unchanged and are never executed.
func TestCommandStepIsInjectionSafe(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "executed")
	dir := filepath.Join(root, "a b'c\"d $(touch pwned);`touch pwned`;e")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	note := `x' ; touch ` + marker + ` ; "$(touch ` + marker + `)" ` + "`touch " + marker + "`"
	m := &config.Model{
		Settings: config.Settings{Schema: 1},
		Projects: []config.Project{{
			Schema: 1, Name: "shop", Dir: dir, Values: map[string]string{"note": note},
			Steps:  []config.Step{{Type: config.StepCommand, Run: `pwd > out.txt; printf '%s\n' {note} {version} >> out.txt`}},
			Images: []config.Image{{Name: "app", Ref: "registry.example.com/example/app", EnvVar: "APP_TAG", Services: []string{"app"}}},
		}},
	}
	plan, err := m.Resolve("shop", []config.Target{{Name: "app", Version: "1.0.0", PreviousVersion: "0.9.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Executor{Runner: &ExecRunner{}}).Execute(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{marker, filepath.Join(dir, "pwned"), "pwned"} {
		if _, err := os.Stat(m); err == nil {
			t.Fatalf("a placeholder value was executed (%s)", m)
		}
	}
	if got, want := readFile(t, filepath.Join(dir, "out.txt")), dir+"\n"+note+"\n1.0.0\n"; got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGitCheckout(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	work := filepath.Join(root, "work")
	clone := filepath.Join(root, "clone")
	git(t, root, "init", "-q", "--bare", "-b", "main", origin)
	git(t, root, "init", "-q", "-b", "main", work)
	os.WriteFile(filepath.Join(work, "f.txt"), []byte("one\n"), 0o644)
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "one")
	git(t, work, "remote", "add", "origin", origin)
	git(t, work, "push", "-q", "origin", "HEAD:refs/heads/main")
	git(t, root, "clone", "-q", origin, clone)
	// The tag appears on the origin after the clone, so the step has to fetch it.
	os.WriteFile(filepath.Join(work, "f.txt"), []byte("two\n"), 0o644)
	git(t, work, "commit", "-q", "-am", "two")
	git(t, work, "tag", "v2.0.0")
	git(t, work, "push", "-q", "origin", "HEAD:refs/heads/main", "refs/tags/v2.0.0")
	want := git(t, work, "rev-parse", "HEAD")

	ex := &Executor{Runner: &ExecRunner{}, GitEnv: []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null"}}
	step := config.ResolvedStep{Type: config.StepGitCheckout, Timeout: 30 * time.Second, Dir: clone, Tag: "v2.0.0"}
	plan := config.Plan{Project: "site", Steps: []config.ResolvedStep{step}}

	// An untracked file blocks the checkout.
	untracked := filepath.Join(clone, "new.txt")
	os.WriteFile(untracked, []byte("x"), 0o644)
	before := git(t, clone, "rev-parse", "HEAD")
	if _, err := ex.Execute(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("err = %v", err)
	}
	if git(t, clone, "rev-parse", "HEAD") != before {
		t.Error("checkout happened despite local changes")
	}
	// So does a modified tracked file.
	os.Remove(untracked)
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("dirty\n"), 0o644)
	if _, err := ex.Execute(context.Background(), plan); err == nil {
		t.Fatal("modified file not detected")
	}
	git(t, clone, "checkout", "--", "f.txt")

	if _, err := ex.Execute(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if got := git(t, clone, "rev-parse", "HEAD"); got != want {
		t.Errorf("HEAD %s want %s", got, want)
	}
	if b := readFile(t, filepath.Join(clone, "f.txt")); b != "two\n" {
		t.Errorf("content %q", b)
	}
}

func TestGitCheckoutRejectsOptionLikeTag(t *testing.T) {
	fr := &fakeRunner{}
	_, err := (&Executor{Runner: fr}).Execute(context.Background(), config.Plan{Steps: []config.ResolvedStep{
		{Type: config.StepGitCheckout, Timeout: time.Second, Dir: "/srv/example/site", Tag: "--upload-pack=x"}}})
	if err == nil || len(fr.calls) != 0 {
		t.Errorf("err=%v calls=%v", err, fr.calls)
	}
}

func TestGitCheckoutCallsAndEnv(t *testing.T) {
	fr := &fakeRunner{}
	ex := &Executor{Runner: fr, GitEnv: []string{"X=1"}}
	if _, err := ex.Execute(context.Background(), config.Plan{Steps: []config.ResolvedStep{
		{Type: config.StepGitCheckout, Timeout: time.Second, Dir: "/srv/example/site", Tag: "v1"}}}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range fr.calls {
		got = append(got, strings.Join(c.Args, " "))
		if !reflect.DeepEqual(c.Env, []string{"GIT_TERMINAL_PROMPT=0", "X=1"}) {
			t.Errorf("env %v", c.Env)
		}
	}
	want := []string{"status --porcelain --untracked-files=all", "fetch --tags --force origin", "checkout --detach refs/tags/v1 --"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("calls %v", got)
	}
}
