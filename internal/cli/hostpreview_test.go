package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

type noDocker struct{}

func (noDocker) Run(_ context.Context, _ execute.Command) (execute.Result, error) {
	return execute.Result{}, fmt.Errorf("docker ps failed: %w", exec.ErrNotFound)
}

func previewFixture(t *testing.T) (*changeFixture, string) {
	t.Helper()
	f := newChangeFixture(t)
	dir := t.TempDir()
	root, err := filepath.EvalSymlinks(filepath.Join(dir, "stacks"))
	if err != nil {
		root = filepath.Join(dir, "stacks")
	}
	for _, p := range []string{"web/compose.yaml", "web/.env", "web/data/state.db", "plain/notes.txt"} {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("A=1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "web"), filepath.Join(root, "web-link")); err != nil {
		t.Fatal(err)
	}
	passwd, group := filepath.Join(dir, "passwd"), filepath.Join(dir, "group")
	if err := os.WriteFile(passwd, []byte("demo-user:x:4242:4343:Demo:/home/demo-user:/bin/bash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(group, []byte("sysmin:x:4344:\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	accounts := &hostadmin.Host{PasswdPath: passwd, GroupPath: group, ShadowPath: filepath.Join(dir, "shadow"), Now: time.Now}
	reg := hostchange.NewRegistry()
	if err := hostchange.RegisterFolderActions(reg, hostchange.FolderHost{
		Accounts: accounts, Stacks: hostchange.StackCheck{Docker: noDocker{}},
	}); err != nil {
		t.Fatal(err)
	}
	f.env.Registry = reg
	return f, root
}

func TestHostPreviewIsRootOnly(t *testing.T) {
	f, root := previewFixture(t)
	code, out, errOut := runChange(t, f, 1000, "", "host", "preview", "folder.setup-stacks", root)
	if code == 0 || out != "" || !strings.Contains(errOut, "must run as root") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestHostPreviewOfTheStackFolder(t *testing.T) {
	f, root := previewFixture(t)
	code, out, errOut := runChange(t, f, 0, "", "host", "preview", "folder.setup-stacks", root, "--output", "json")
	if code != 0 {
		t.Fatalf("code %d err %q", code, errOut)
	}
	m := decode(t, out)
	if m["action"] != "folder.setup-stacks" || !strings.Contains(m["sentence"].(string), "Set up the stack folder") {
		t.Fatalf("%v", m)
	}
	if cmds := m["commands"].([]any); len(cmds) != 4 || !strings.HasPrefix(cmds[0].(string), "chown -h -- root:sysmin ") {
		t.Fatalf("commands %v", cmds)
	}
	p := m["preview"].(map[string]any)
	stacks := p["stacks"].([]any)
	web := stacks[0].(map[string]any)
	changes := web["changes"].([]any)
	first := changes[0].(map[string]any)
	if p["path"] != root || p["others_lose_access"] != true || p["nothing_to_do"] != false || len(stacks) != 1 ||
		web["name"] != "web" || web["running"] != false || len(changes) != 3 || first["role"] != "stack folder" ||
		first["after"].(map[string]any)["mode"] != "0750" || first["after"].(map[string]any)["group"] != "sysmin" ||
		p["folder"].(map[string]any)["role"] != "folder" {
		t.Fatalf("%v", p)
	}
	kept := p["kept"].([]any)
	if len(kept) != 1 || !strings.HasSuffix(kept[0].(map[string]any)["path"].(string), "web-link") {
		t.Fatalf("kept %v", kept)
	}
	if len(f.w.calls) != 0 || len(f.audited) != 0 {
		t.Fatalf("ran %v, audited %v", f.w.calls, f.audited)
	}
	if _, err := os.Stat(f.dir); err == nil {
		t.Error("the preview created the journal")
	}
}

func TestHostPreviewRefusalUsesTheRefusedFormat(t *testing.T) {
	f, _ := previewFixture(t)
	code, out, errOut := runChange(t, f, 0, "", "host", "preview", "folder.setup-stacks", "/etc/demo-stacks", "--output", "json")
	m := decode(t, out)
	if code == 0 || m["result"] != "refused" || !strings.Contains(m["error"].(string), "system path") || !strings.Contains(errOut, "refused") {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
}

func TestHostPreviewOfAnActionWithoutStructuredData(t *testing.T) {
	f, root := previewFixture(t)
	code, out, errOut := runChange(t, f, 0, "", "host", "preview", "folder.set-permissions", root, "demo-user", "sysmin", "read-write", "read", "nothing", "--output", "json")
	if code != 0 {
		t.Fatalf("code %d err %q", code, errOut)
	}
	m := decode(t, out)
	cmds := m["commands"].([]any)
	if m["preview"] != nil || !strings.HasPrefix(m["sentence"].(string), "Change "+root) || len(cmds) != 2 || len(f.w.calls) != 0 {
		t.Fatalf("%v", m)
	}
}
