package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// usersWorld is a small invented host for the user actions through the CLI: passwd, group and shadow files
// that useradd, chpasswd, chage and userdel change; built-in steps are recorded only.
type usersWorld struct {
	dir   string
	argv  []string
	shown []string
	stdin []string
}

func (w *usersWorld) path(name string) string { return filepath.Join(w.dir, name) }

func (w *usersWorld) edit(t *testing.T, file string, fn func(fields []string) []string) {
	t.Helper()
	data, err := os.ReadFile(w.path(file))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if f := fn(strings.Split(line, ":")); f != nil {
			out = append(out, strings.Join(f, ":"))
		}
	}
	if err := os.WriteFile(w.path(file), []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newUsersWorld(t *testing.T) (*usersWorld, hostchange.UserHost, func(context.Context, hostchange.Command) ([]byte, error)) {
	t.Helper()
	w := &usersWorld{dir: t.TempDir()}
	for _, d := range []string{"home/demo-admin", "home/demo-user", "mail", "log"} {
		if err := os.MkdirAll(w.path(d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	accounts := "root:x:0:0::/root:/bin/bash\ndemo-admin:x:1000:1000::" + w.path("home/demo-admin") + ":/bin/bash\n" +
		"demo-user:x:1001:1001::" + w.path("home/demo-user") + ":/bin/bash\n"
	files := map[string]string{
		"passwd": accounts,
		"group":  "root:x:0:\nsudo:x:27:demo-admin\nsysmin:x:990:demo-admin\ndemo-admin:x:1000:\ndemo-user:x:1001:\n",
		"shadow": "root:*:19000::::::\ndemo-admin:$6$demo-a:19000::::::\ndemo-user:$6$demo-u:19000::::::\n",
	}
	for name, content := range files {
		if err := os.WriteFile(w.path(name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h := hostadmin.NewHost(true)
	h.PasswdPath, h.GroupPath, h.ShadowPath = w.path("passwd"), w.path("group"), w.path("shadow")
	uh := hostchange.UserHost{Accounts: h, HomeBase: w.path("home"), MailDir: w.path("mail"), Lastlog: w.path("log/lastlog")}
	run := func(_ context.Context, c hostchange.Command) ([]byte, error) {
		w.argv = append(w.argv, strings.Join(c.Argv, " "))
		w.shown = append(w.shown, c.Display())
		w.stdin = append(w.stdin, string(c.Secret))
		a := c.Argv
		switch {
		case c.Builtin:
		case a[0] == "useradd":
			name, home := a[len(a)-1], a[3]
			appendLine(t, w.path("passwd"), name+":x:1500:1500::"+home+":/bin/bash")
			appendLine(t, w.path("group"), name+":x:1500:")
			appendLine(t, w.path("shadow"), name+":!:20000::::::")
			if err := os.Mkdir(home, 0o755); err != nil {
				return nil, err
			}
		case a[0] == "chpasswd":
			name, _, _ := strings.Cut(string(c.Secret), ":")
			w.edit(t, "shadow", func(f []string) []string {
				if f[0] == name {
					f[1], f[2] = "$y$demo-new-hash", "20001"
				}
				return f
			})
		case a[0] == "chage":
			w.edit(t, "shadow", func(f []string) []string {
				if f[0] == a[3] {
					f[2] = a[2]
				}
				return f
			})
		case a[0] == "userdel":
			for _, file := range []string{"passwd", "group", "shadow"} {
				w.edit(t, file, func(f []string) []string {
					if f[0] == a[2] {
						return nil
					}
					return f
				})
			}
		}
		return nil, nil
	}
	return w, uh, run
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

type execFn func(context.Context, hostchange.Command) ([]byte, error)

func (f execFn) Run(ctx context.Context, c hostchange.Command) ([]byte, error) { return f(ctx, c) }

func TestHostApplyUserActionsKeepPasswordsAndKeysOutOfEverything(t *testing.T) {
	w, uh, run := newUsersWorld(t)
	f := newChangeFixture(t)
	reg := hostchange.NewRegistry()
	if err := hostchange.RegisterUserActions(reg, uh); err != nil {
		t.Fatal(err)
	}
	f.env.Registry, f.env.Exec = reg, execFn(run)

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	keyLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	keyBlob := strings.Fields(keyLine)[1]
	const marker = "hunter-demo-42"
	input, err := json.Marshal(hostchange.CreateInput{Password: marker, Keys: []string{keyLine}})
	if err != nil {
		t.Fatal(err)
	}

	var outputs []string
	// The key cannot be written by the fake host, so the check fails and the account is deleted again; the
	// report of that path is checked as well.
	code, out, errOut := runChange(t, f, 0, string(input)+"\n", "host", "apply", "--yes", "--output", "json", "--", "user.create", "demo-new")
	outputs = append(outputs, out, errOut)
	m := decode(t, out)
	if code != 1 || m["result"] != "restored" || !strings.Contains(m["check_error"].(string), "SSH key") {
		t.Fatalf("create: code %d %v", code, m)
	}
	if cmds := m["commands"].([]any); cmds[1] != "chpasswd < <password>" || cmds[3] != "[builtin] authorized-keys-add demo-new < <ssh key "+ssh.FingerprintSHA256(key)+">" {
		t.Errorf("commands %v", cmds)
	}

	code, out, errOut = runChange(t, f, 0, marker+"-2\n", "host", "apply", "--yes", "--output", "json", "--", "user.set-password", "demo-user", "must-change")
	outputs = append(outputs, out, errOut)
	if m := decode(t, out); code != 0 || m["result"] != "applied" || m["check"] != "passed" {
		t.Fatalf("set-password: code %d %v %q", code, m, errOut)
	}
	_, out, errOut = runChange(t, f, 0, keyLine+"\n", "host", "apply", "--yes", "--output", "json", "--", "user.add-key", "demo-user")
	outputs = append(outputs, out, errOut)

	_, out, errOut = runChange(t, f, 0, "", "host", "journal", "--output", "json")
	outputs = append(outputs, out, errOut)
	audit, _ := json.Marshal(f.audited)
	shown := strings.Join(append(append(append(outputs, w.argv...), w.shown...), string(audit)), "\n")
	for _, s := range []string{marker, keyBlob, "$6$demo-u"} {
		if strings.Contains(shown, s) {
			t.Errorf("%q leaked into argv, displayed commands, output or audit", s)
		}
	}
	if !strings.Contains(strings.Join(w.stdin, ""), "demo-new:"+marker+"\n") {
		t.Errorf("the password did not reach chpasswd on standard input: %q", w.stdin)
	}
}

func TestHostApplyUserRefusalChangesNothing(t *testing.T) {
	w, uh, run := newUsersWorld(t)
	f := newChangeFixture(t)
	reg := hostchange.NewRegistry()
	if err := hostchange.RegisterUserActions(reg, uh); err != nil {
		t.Fatal(err)
	}
	f.env.Registry, f.env.Exec = reg, execFn(run)
	f.env.SudoUID = func() int { return 1000 }
	code, out, errOut := runChange(t, f, 0, "", "host", "apply", "--yes", "--", "user.set-admin", "demo-admin", "off")
	if code == 0 || out != "" || !strings.Contains(errOut, "from yourself") || len(w.argv) != 0 {
		t.Fatalf("code %d out %q err %q argv %q", code, out, errOut, w.argv)
	}
	code, _, _ = runChange(t, f, 0, "", "host", "apply", "--yes", "--", "user.set-admin", "Demo", "off")
	if code != 2 {
		t.Errorf("an invalid name is a usage error: %d", code)
	}
}
