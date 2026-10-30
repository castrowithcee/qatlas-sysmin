package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApprovalsHasOnlyReadCommands(t *testing.T) {
	root := newRootCommand(&Options{})
	cmd, _, err := root.Find([]string{"approvals"})
	if err != nil || cmd.Name() != "approvals" {
		t.Fatalf("find approvals: %v", err)
	}
	var names []string
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	if strings.Join(names, ",") != "list,show" {
		t.Fatalf("subcommands = %v, want only list and show", names)
	}
	for _, name := range []string{"approve", "discard", "approve-all"} {
		code, _, stderr := runCLIArgs("approvals", name)
		if code != exitUsage || !strings.Contains(stderr, "unexpected argument") {
			t.Errorf("approvals %s: code = %d, stderr = %q", name, code, stderr)
		}
	}
}

const approvalsDemoProject = `schema: 1
name: demo-app
dir: /srv/demo/app
template: git-tag
git:
  url: https://git.example.com/demo/app.git
steps:
  - type: command
    run: echo hello
`

func TestApprovalsListAndShow(t *testing.T) {
	sock := startServiceWith(t, true, func(dir string) {
		for rel, content := range map[string]string{"sysmin.yaml": "schema: 1\n", "projects/demo-app.yaml": approvalsDemoProject} {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
				t.Fatal(err)
			}
		}
	})
	code, stdout, stderr := runCLIArgs("approvals", "list", "--socket", sock, "--output", "json")
	if code != exitOK || !strings.Contains(stdout, `"project":"demo-app"`) || !strings.Contains(stdout, `"kind":"new"`) ||
		!strings.Contains(stdout, `"free_commands":true`) {
		t.Fatalf("list: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIArgs("approvals", "list", "--socket", sock)
	if code != exitOK || !strings.Contains(stdout, "demo-app") {
		t.Fatalf("list table: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIArgs("approvals", "show", "demo-app", "--socket", sock, "--output", "json")
	if code != exitOK || !strings.Contains(stdout, `"new":"echo hello"`) || !strings.Contains(stdout, `"free":true`) {
		t.Fatalf("show: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIArgs("approvals", "show", "other-app", "--socket", sock)
	if code != exitRuntime || stdout != "" || !strings.Contains(stderr, "no open approval") {
		t.Fatalf("show unknown: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, _, stderr = runCLIArgs("approvals", "show", "--socket", sock)
	if code != exitUsage || !strings.Contains(stderr, "exactly one project name") {
		t.Fatalf("show without name: code = %d, stderr = %q", code, stderr)
	}
}

func TestApprovalsLockedAndWithoutService(t *testing.T) {
	sock := startService(t, false)
	code, _, stderr := runCLIArgs("approvals", "list", "--socket", sock)
	if code != exitRuntime || !strings.Contains(stderr, "vault is locked") {
		t.Fatalf("locked: code = %d, stderr = %q", code, stderr)
	}
	none := filepath.Join(t.TempDir(), "none.sock")
	code, _, stderr = runCLIArgs("approvals", "list", "--socket", none)
	if code != exitRuntime || !strings.Contains(stderr, "cannot reach the service") {
		t.Fatalf("no service: code = %d, stderr = %q", code, stderr)
	}
}
