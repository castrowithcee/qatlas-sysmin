package cli

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
)

// hostFixture writes invented databases and returns the environment of a caller with the given UID.
func hostFixture(t *testing.T, uid int) (env *hostEnv, keyBlob string) {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home", "demo-admin")
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	keyBlob = base64.StdEncoding.EncodeToString(key.Marshal())
	accounts := "root:x:0:0::/root:/bin/bash\n" +
		"demo-admin:x:1000:1000::" + home + ":/bin/bash\n" +
		"demo-user:x:1001:1001::" + filepath.Join(dir, "home", "demo-user") + ":/bin/bash\n"
	files := map[string]string{
		"passwd": accounts,
		"group":  "root:x:0:\nsudo:x:27:demo-admin\nsysmin:x:990:demo-user\ndemo-admin:x:1000:\ndemo-user:x:1001:\n",
		"shadow": "root:*:19000:::::\ndemo-admin:$6$fake:19000:::::\ndemo-user:!$6$fake:19000:::::\n",
		filepath.Join("home", "demo-admin", ".ssh", "authorized_keys"): "ssh-ed25519 " + keyBlob + " demo-admin@demo-laptop\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h := hostadmin.NewHost(uid == 0)
	h.PasswdPath, h.GroupPath, h.ShadowPath = filepath.Join(dir, "passwd"), filepath.Join(dir, "group"), filepath.Join(dir, "shadow")
	return &hostEnv{UID: uid, Host: h}, keyBlob
}

func runHost(env *hostEnv, args ...string) (int, string, string) {
	opts := &Options{Redactor: &redact.Redactor{}, host: env}
	var out, errOut bytes.Buffer
	code := run(newRootCommand(opts), opts, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestHostRefusesCallersWithoutAdminRights(t *testing.T) {
	env, _ := hostFixture(t, 1001)
	for _, args := range [][]string{
		{"host", "users"}, {"host", "user", "demo-user"}, {"host", "groups"}, {"host", "group", "sudo"},
		{"host", "folder", "/"}, {"host"}, {"host", "users", "extra"},
	} {
		code, out, errOut := runHost(env, args...)
		if code != 1 || out != "" || errOut != "sysmin is reserved for administrators (sudo)\n" {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
	// A caller the database does not know is no administrator either.
	unknown, _ := hostFixture(t, 4242)
	if code, _, errOut := runHost(unknown, "host", "users"); code != 1 || !strings.HasPrefix(errOut, "sysmin is reserved") {
		t.Errorf("unknown caller: code %d, stderr %q", code, errOut)
	}
}

func TestHostAdminWithoutRootSeesUnknown(t *testing.T) {
	env, blob := hostFixture(t, 1000)
	code, out, errOut := runHost(env, "host", "user", "demo-admin", "--output", "json")
	if code != 0 || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"locked", "expired", "ssh_keys"} {
		if v, ok := got[field]; !ok || v != nil {
			t.Errorf("%s = %v (present %v), want null", field, v, ok)
		}
	}
	if got["admin"] != true || got["primary_group"] != "demo-admin" {
		t.Errorf("user = %v", got)
	}
	if strings.Contains(out, blob) {
		t.Error("key content in output")
	}
	_, table, _ := runHost(env, "host", "user", "demo-admin")
	if !strings.Contains(table, "unknown") {
		t.Errorf("table does not say unknown:\n%s", table)
	}
	_, list, _ := runHost(env, "host", "users", "--output", "compact")
	if !strings.Contains(list, "|sudo|") || !strings.Contains(list, "|unknown|unknown") {
		t.Errorf("compact list:\n%s", list)
	}
}

func TestHostRootSeesLockedAndKeysButNeverTheKey(t *testing.T) {
	env, blob := hostFixture(t, 0)
	for _, format := range []string{"json", "table", "compact"} {
		for _, args := range [][]string{{"host", "user", "demo-admin"}, {"host", "users"}} {
			code, out, errOut := runHost(env, append(args, "--output", format)...)
			if code != 0 || errOut != "" {
				t.Fatalf("%v %s: code %d, stderr %q", args, format, code, errOut)
			}
			if strings.Contains(out, blob) {
				t.Errorf("%v %s: key content in output", args, format)
			}
			if strings.Contains(out, "unknown") {
				t.Errorf("%v %s: unknown for root:\n%s", args, format, out)
			}
		}
	}
	_, out, _ := runHost(env, "host", "user", "demo-admin", "--output", "json")
	var got struct {
		Locked  *bool              `json:"locked"`
		Expired *bool              `json:"expired"`
		Keys    []hostadmin.SSHKey `json:"ssh_keys"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Locked == nil || *got.Locked || got.Expired == nil || len(got.Keys) != 1 ||
		got.Keys[0].Type != "ssh-ed25519" || got.Keys[0].Comment != "demo-admin@demo-laptop" ||
		!strings.HasPrefix(got.Keys[0].Fingerprint, "SHA256:") {
		t.Errorf("user = %+v", got)
	}
	_, out, _ = runHost(env, "host", "user", "demo-user", "--output", "json")
	if !strings.Contains(out, `"locked":true`) {
		t.Errorf("demo-user not locked: %s", out)
	}
}

func TestHostGroupsAndFolder(t *testing.T) {
	env, _ := hostFixture(t, 1000)
	_, out, _ := runHost(env, "host", "group", "demo-user", "--output", "json")
	if out != `{"name":"demo-user","gid":1001,"members":["demo-user"],"system":false}`+"\n" {
		t.Errorf("group = %s", out)
	}
	if code, out, _ := runHost(env, "host", "groups", "--output", "json"); code != 0 || !strings.HasPrefix(out, "[{") {
		t.Errorf("groups = %d %s", code, out)
	}
	dir := t.TempDir()
	code, out, _ := runHost(env, "host", "folder", dir, "--output", "json")
	if code != 0 {
		t.Fatalf("folder code %d", code)
	}
	var f map[string]any
	if err := json.Unmarshal([]byte(out), &f); err != nil {
		t.Fatal(err)
	}
	if f["kind"] != "directory" || f["protected"] != false || f["owner_access"] != "read and write" {
		t.Errorf("folder = %v", f)
	}
	if code, _, _ := runHost(env, "host", "folder", "/etc/../etc", "--output", "json"); code != 0 {
		t.Error("folder /etc failed")
	}
	_, out, _ = runHost(env, "host", "folder", "/etc/../etc", "--output", "json")
	if !strings.Contains(out, `"protected":true`) {
		t.Errorf("/etc not protected: %s", out)
	}
}

func TestHostUnknownTargets(t *testing.T) {
	env, _ := hostFixture(t, 1000)
	for _, args := range [][]string{
		{"host", "user", "nobody-here"}, {"host", "group", "nobody-here"}, {"host", "folder", "/srv/demo-missing-path"},
	} {
		code, out, errOut := runHost(env, args...)
		if code != 1 || out != "" || !strings.HasPrefix(errOut, "sysmin: runtime: host ") || !strings.Contains(errOut, "not found") {
			t.Errorf("%v: code %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
	if code, _, _ := runHost(env, "host", "user"); code != 2 {
		t.Errorf("missing argument exits %d, want 2", code)
	}
}
