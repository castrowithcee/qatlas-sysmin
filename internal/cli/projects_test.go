package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
)

// projectSetup gives the service the settings file it needs and no project.
func projectSetup(t *testing.T) func(string) {
	return func(dir string) {
		if err := os.WriteFile(filepath.Join(dir, "sysmin.yaml"), []byte("schema: 1\n"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
}

func runCLIIn(stdin io.Reader, args ...string) (int, string, string) {
	opts := &Options{Redactor: &redact.Redactor{}}
	root := newRootCommand(opts)
	root.SetIn(stdin)
	var out, errOut bytes.Buffer
	code := run(root, opts, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

var demoAddArgs = []string{
	"project", "add", "demo-app", "--dir", "/srv/demo/app", "--env-file", "/srv/demo/app/.env",
	"--image", "app=registry.example.com/demo/app", "--image-env", "app=DEMO_APP_TAG", "--image-service", "app=app",
}

func jsonObject(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not a JSON object: %q: %v", s, err)
	}
	return m
}

func jsonRows(t *testing.T, s string) []map[string]any {
	t.Helper()
	var m []map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not a JSON list: %q: %v", s, err)
	}
	return m
}

func TestProjectLifecycleThroughTheService(t *testing.T) {
	sock := startServiceWith(t, true, projectSetup(t))
	cli := func(args ...string) (int, string, string) {
		return runCLIArgs(append(args, "--socket", sock, "--output", "json")...)
	}

	code, stdout, stderr := cli(demoAddArgs...)
	obj := jsonObject(t, stdout)
	if code != exitOK || obj["project"] != "demo-app" || obj["state"] != "pending approval" ||
		!strings.Contains(obj["message"].(string), "not approved yet") {
		t.Fatalf("add: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}

	// The change is an open approval, visible to a person.
	code, stdout, _ = cli("approvals", "list")
	if code != exitOK || !strings.Contains(stdout, `"project":"demo-app"`) || !strings.Contains(stdout, `"kind":"new"`) {
		t.Fatalf("approvals list: code = %d, stdout = %q", code, stdout)
	}

	code, stdout, stderr = cli("project", "add", "demo-app", "--file", "/dev/null")
	if code == exitOK {
		t.Fatalf("add of an existing project: stdout = %q, stderr = %q", stdout, stderr)
	}

	code, stdout, stderr = cli("project", "show", "demo-app")
	obj = jsonObject(t, stdout)
	if code != exitOK || obj["state"] != "pending approval" || obj["revision"] == "" ||
		!strings.Contains(obj["file"].(string), "template: image-tag") {
		t.Fatalf("show: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	revision := obj["revision"].(string)

	code, stdout, stderr = cli("project", "list")
	rows := jsonRows(t, stdout)
	if code != exitOK || len(rows) != 1 || rows[0]["name"] != "demo-app" || rows[0]["source"] != "images" || rows[0]["state"] != "pending approval" {
		t.Fatalf("list: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}

	code, stdout, stderr = cli("project", "check", "demo-app", "--version", "2.0.1")
	rows = jsonRows(t, stdout)
	if code != exitOK || len(rows) == 0 || !strings.Contains(stdout, "assign=DEMO_APP_TAG=2.0.1") {
		t.Fatalf("check: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, _ = runCLIArgs("project", "check", "demo-app", "--socket", sock)
	if code != exitOK || !strings.Contains(stdout, "compose-up") {
		t.Fatalf("check table: code = %d, stdout = %q", code, stdout)
	}

	code, stdout, stderr = cli("project", "edit", "demo-app", "--set", "domain=app.example.com", "--poll-interval", "6h", "--revision", revision)
	obj = jsonObject(t, stdout)
	if code != exitOK || obj["action"] != "changed" || obj["state"] != "pending approval" {
		t.Fatalf("edit: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	// The stored revision moved, so the old one is refused.
	code, stdout, stderr = cli("project", "edit", "demo-app", "--dir", "/srv/demo/other", "--revision", revision)
	if code != exitRuntime || stdout != "" || !strings.Contains(stderr, "changed since it was read") {
		t.Fatalf("edit with a stale revision: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, _ = cli("project", "show", "demo-app")
	if !strings.Contains(stdout, "domain: app.example.com") || !strings.Contains(stdout, "poll_interval: 6h") || strings.Contains(stdout, "/srv/demo/other") {
		t.Fatalf("show after edit: %q", stdout)
	}

	code, stdout, stderr = cli("project", "remove", "demo-app")
	obj = jsonObject(t, stdout)
	// Nobody approved this project, so its removal leaves nothing open.
	if code != exitOK || obj["action"] != "removed" || obj["state"] != "approved" {
		t.Fatalf("remove: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, _, stderr = cli("project", "show", "demo-app")
	if code != exitRuntime || !strings.Contains(stderr, "does not exist") {
		t.Fatalf("show after remove: code = %d, stderr = %q", code, stderr)
	}
	code, stdout, _ = cli("project", "list")
	if code != exitOK || strings.Contains(stdout, "demo-app") {
		t.Fatalf("list after remove: %q", stdout)
	}
}

func TestProjectAddFromFileAndStandardInput(t *testing.T) {
	sock := startServiceWith(t, true, projectSetup(t))
	file := filepath.Join(t.TempDir(), "demo.yaml")
	body := "schema: 1\nname: demo-app\ndir: /srv/demo/app\ntemplate: git-tag\ngit:\n  url: https://git.example.com/demo/app.git\n"
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLIArgs("project", "add", "demo-app", "--file", file, "--socket", sock, "--output", "json")
	if code != exitOK || !strings.Contains(stdout, "pending approval") {
		t.Fatalf("add --file: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIIn(strings.NewReader(strings.ReplaceAll(body, "demo-app", "demo-two")), "project", "edit", "demo-two", "--file", "-", "--socket", sock)
	if code != exitRuntime || !strings.Contains(stderr, "does not exist") {
		t.Fatalf("edit of a missing project: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIIn(strings.NewReader(strings.ReplaceAll(body, "demo-app", "demo-two")), "project", "add", "demo-two", "--file", "-", "--socket", sock, "--output", "json")
	if code != exitOK || !strings.Contains(stdout, `"project":"demo-two"`) {
		t.Fatalf("add --file -: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, _, stderr = runCLIArgs("project", "add", "demo-three", "--file", file, "--dir", "/srv/x", "--socket", sock)
	if code != exitUsage || !strings.Contains(stderr, "--file excludes") {
		t.Fatalf("--file with options: code = %d, stderr = %q", code, stderr)
	}
	code, _, stderr = runCLIArgs("project", "add", "demo-three", "--set", "novalue", "--socket", sock)
	if code != exitUsage || !strings.Contains(stderr, "NAME=VALUE") {
		t.Fatalf("bad --set: code = %d, stderr = %q", code, stderr)
	}
}

func TestProjectInvalidInputListsProblems(t *testing.T) {
	sock := startServiceWith(t, true, projectSetup(t))
	args := []string{
		"project", "add", "demo-app", "--dir", "relative/dir", "--image", "app=registry.example.com/demo/app:1.0",
		"--socket", sock, "--output", "json",
	}
	code, stdout, stderr := runCLIArgs(args...)
	rows := jsonRows(t, stdout)
	if code != exitUsage || len(rows) < 2 || !strings.Contains(stderr, "problems found; nothing was changed") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	byField := map[string]map[string]any{}
	for _, r := range rows {
		byField[r["field"].(string)] = r
		if r["code"] == "" || r["message"] == "" || r["hint"] == "" {
			t.Errorf("incomplete problem %v", r)
		}
	}
	if byField["dir"]["code"] != "relative-path" || byField["images[0].ref"]["code"] != "invalid-value" {
		t.Fatalf("problems = %v", rows)
	}
	code, stdout, _ = runCLIArgs("project", "list", "--socket", sock, "--output", "json")
	if code != exitOK || strings.Contains(stdout, "demo-app") {
		t.Fatalf("an invalid project was stored: %q", stdout)
	}
	// The table shows the same list.
	code, stdout, _ = runCLIArgs(append(args[:len(args)-2:len(args)-2], "--output", "table")...)
	if code != exitUsage || !strings.Contains(stdout, "FIELD") || !strings.Contains(stdout, "relative-path") {
		t.Fatalf("table: code = %d, stdout = %q", code, stdout)
	}
	// check reports a stored invalid project the same way.
	code, stdout, _ = runCLIArgs("project", "check", "demo-none", "--socket", sock)
	if code != exitRuntime || stdout != "" {
		t.Fatalf("check of a missing project: code = %d, stdout = %q", code, stdout)
	}
}

func TestProjectChangeWithLockedVaultStoresNothing(t *testing.T) {
	var configDir string
	sock := startServiceWith(t, false, func(dir string) { configDir = dir; projectSetup(t)(dir) })
	code, stdout, stderr := runCLIArgs(append(demoAddArgs, "--socket", sock)...)
	if code != exitRuntime || stdout != "" || !strings.Contains(stderr, "vault is locked") {
		t.Fatalf("add: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, _, stderr = runCLIArgs("project", "remove", "demo-app", "--socket", sock)
	if code != exitRuntime || !strings.Contains(stderr, "vault is locked") {
		t.Fatalf("remove: code = %d, stderr = %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(configDir, "projects", "demo-app.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a project was stored while the vault was locked: %v", err)
	}
}

// The command line has no admin session of its own, and one that another connection opened does not help it.
func TestProjectChangeIgnoresSessionOfAnotherConnection(t *testing.T) {
	sock := startServiceWith(t, true, projectSetup(t))
	adm, err := client.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer adm.Close()
	if err := adm.OpenSession("example-passphrase-1"); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLIArgs(append(demoAddArgs, "--socket", sock, "--output", "json")...)
	if code != exitOK || !strings.Contains(stdout, "pending approval") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	// The same project written on the connection that holds the session is approved at once.
	res, err := adm.AddProject(control.ProjectParams{Name: "demo-web", Dir: "/srv/demo/web", Template: "git-tag", GitURL: "https://git.example.com/demo/web.git"})
	if err != nil || res.State != control.StateApproved {
		t.Fatalf("add in a session = %+v, %v", res, err)
	}
}

// The project commands never ask for the passphrase, not even when both streams are terminals: they finish
// without reading from or writing to the terminal.
func TestProjectCommandsDoNotPromptOnATerminal(t *testing.T) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminal: %v", err)
	}
	defer master.Close()
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("cannot unlock the pseudo terminal: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("cannot name the pseudo terminal: %v", err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot open the pseudo terminal: %v", err)
	}
	defer slave.Close()
	if !isTerminal(slave) {
		t.Skip("the pseudo terminal is no terminal")
	}

	sock := startServiceWith(t, true, projectSetup(t))
	opts := &Options{Redactor: &redact.Redactor{}}
	root := newRootCommand(opts)
	root.SetIn(slave)
	var errOut bytes.Buffer
	done := make(chan int, 1)
	go func() {
		// stdout is the terminal as well, so a prompt could be shown and answered there.
		root.SetOut(slave)
		root.SetErr(&errOut)
		root.SetArgs(append(demoAddArgs, "--socket", sock, "--output", "json"))
		if _, err := root.ExecuteC(); err != nil {
			done <- exitCode(err)
			return
		}
		done <- exitOK
	}()
	select {
	case code := <-done:
		if code != exitOK {
			t.Fatalf("code = %d, stderr = %q", code, errOut.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the command waits for input on the terminal")
	}
	_ = master.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 4096)
	got, _ := master.Read(buf)
	text := strings.ToLower(string(buf[:got]))
	if strings.Contains(text, "passphrase") || strings.Contains(text, "password") {
		t.Fatalf("the terminal shows a prompt: %q", text)
	}
	if !strings.Contains(text, "pending approval") {
		t.Fatalf("the result did not reach the terminal: %q", text)
	}
}

func TestProjectListShowsUpdatesAndLastRun(t *testing.T) {
	sock := startServiceWith(t, true, projectSetup(t))
	if code, _, stderr := runCLIArgs(append(demoAddArgs, "--socket", sock, "--output", "json")...); code != exitOK {
		t.Fatalf("add: %d %q", code, stderr)
	}
	code, stdout, _ := runCLIArgs("project", "list", "--socket", sock, "--output", "json")
	rows := jsonRows(t, stdout)
	if code != exitOK || len(rows) != 1 || rows[0]["updates"] != float64(0) || rows[0]["fault"] != false {
		t.Fatalf("json: %q", stdout)
	}
	order := []string{"name", "template", "source", "dir", "state", "updates", "last_run", "fault"}
	last := -1
	for _, k := range order {
		i := strings.Index(stdout, `"`+k+`"`)
		if i <= last {
			t.Fatalf("field %s out of order in %q", k, stdout)
		}
		last = i
	}
	code, stdout, _ = runCLIArgs("project", "list", "--socket", sock, "--output", "table")
	if code != exitOK || !strings.Contains(stdout, "UPDATES") || !strings.Contains(stdout, "LAST_RUN") {
		t.Fatalf("table: %q", stdout)
	}
}
