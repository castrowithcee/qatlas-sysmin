package hostchange

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// userWorld is an invented host for the user actions: the fake executor applies useradd, usermod, chpasswd,
// chage and userdel to an in-memory model and writes passwd, group and shadow files from it, and runs the
// built-in steps for real inside a temporary home. Every account maps to the test's own UID on disk.
type userWorld struct {
	t      *testing.T
	dir    string
	users  []worldUser
	groups []worldGroup
	calls  []string
	stdins map[string]string // displayed command -> standard input
	failOn string            // displayed command that fails and echoes its standard input
	host   UserHost
}

type worldUser struct {
	name          string
	uid, gid      int
	home          string
	hash, lastchg string
}

type worldGroup struct {
	name    string
	gid     int
	members []string
}

func newUserWorld(t *testing.T) *userWorld {
	t.Helper()
	dir := t.TempDir()
	w := &userWorld{t: t, dir: dir, stdins: map[string]string{}}
	home := func(n string) string { return filepath.Join(dir, "home", n) }
	w.users = []worldUser{
		{"root", 0, 0, "/root", "$6$demo-root", "19000"},
		{"svc-demo", 998, 998, "/nonexistent", "*", "19000"},
		{"demo-admin", 1000, 1000, home("demo-admin"), "$6$demo-a", "19000"},
		{"demo-user", 1001, 1001, home("demo-user"), "$6$demo-u", "19000"},
		{"demo-other", 1002, 1002, home("demo-other"), "$6$demo-o", "19000"},
		{"demo-boss", 1003, 1003, home("demo-boss"), "$6$demo-b", "19000"},
		{"nobody", 65534, 65534, "/nonexistent", "*", "19000"},
	}
	w.groups = []worldGroup{
		{"root", 0, nil},
		{"adm", 4, []string{"demo-user"}},
		{"sudo", 27, []string{"demo-admin", "demo-other", "demo-boss"}},
		{"sysmin", 990, []string{"demo-admin", "demo-other"}},
		{"docker", 999, []string{"demo-user"}},
		{"svc-demo", 998, nil},
		{"demo-admin", 1000, nil}, {"demo-user", 1001, nil}, {"demo-other", 1002, nil}, {"demo-boss", 1003, nil},
		{"projects", 2000, nil},
		{"media", 2001, []string{"demo-user"}},
		{"nogroup", 65534, nil},
	}
	for _, u := range w.users[2:6] {
		if err := os.MkdirAll(u.home, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"mail", "log"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := hostadmin.NewHost(true)
	h.PasswdPath, h.GroupPath, h.ShadowPath = filepath.Join(dir, "passwd"), filepath.Join(dir, "group"), filepath.Join(dir, "shadow")
	w.host = UserHost{
		Accounts: h, HomeBase: filepath.Join(dir, "home"), MailDir: filepath.Join(dir, "mail"),
		Lastlog: filepath.Join(dir, "log", "lastlog"), Lastlog2: filepath.Join(dir, "lastlog2.db"),
	}
	// A classic lastlog with room for the first regular users and no login recorded.
	if err := os.WriteFile(w.host.Lastlog, make([]byte, 1001*lastlogRecord), 0o644); err != nil {
		t.Fatal(err)
	}
	w.flush()
	return w
}

func (w *userWorld) flush() {
	var pw, gr, sh strings.Builder
	for _, u := range w.users {
		fmt.Fprintf(&pw, "%s:x:%d:%d::%s:/bin/bash\n", u.name, u.uid, u.gid, u.home)
		fmt.Fprintf(&sh, "%s:%s:%s:0:99999:7:::\n", u.name, u.hash, u.lastchg)
	}
	for _, g := range w.groups {
		fmt.Fprintf(&gr, "%s:x:%d:%s\n", g.name, g.gid, strings.Join(g.members, ","))
	}
	for path, content := range map[string]string{w.host.Accounts.PasswdPath: pw.String(), w.host.Accounts.GroupPath: gr.String(), w.host.Accounts.ShadowPath: sh.String()} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			w.t.Fatal(err)
		}
	}
}

func (w *userWorld) user(name string) *worldUser {
	for i := range w.users {
		if w.users[i].name == name {
			return &w.users[i]
		}
	}
	return nil
}

func (w *userWorld) group(name string) *worldGroup {
	for i := range w.groups {
		if w.groups[i].name == name {
			return &w.groups[i]
		}
	}
	return nil
}

func (w *userWorld) member(user, group string) bool {
	g := w.group(group)
	return g != nil && slices.Contains(g.members, user)
}

func (w *userWorld) lookup(name string) (hostadmin.Account, error) {
	u := w.user(name)
	if u == nil {
		return hostadmin.Account{}, hostadmin.ErrNotFound
	}
	return hostadmin.Account{UID: os.Getuid(), GID: os.Getgid(), Home: u.home}, nil
}

func (w *userWorld) Run(_ context.Context, c Command) ([]byte, error) {
	if err := validateCommand(c); err != nil {
		return nil, err
	}
	d := c.Display()
	w.calls = append(w.calls, d)
	w.stdins[d] = string(c.Secret)
	if d == w.failOn {
		return append([]byte("simulated failure: "), c.Secret...), errors.New("exit status 1")
	}
	if c.Builtin {
		return nil, runBuiltin(c, w.lookup)
	}
	a := c.Argv
	switch a[0] {
	case "useradd":
		name := a[len(a)-1]
		uid := 1000
		for _, u := range w.users {
			if hostadmin.Regular(u.uid) && u.uid >= uid {
				uid = u.uid + 1
			}
		}
		home := a[slices.Index(a, "-d")+1]
		w.users = append(w.users, worldUser{name, uid, uid, home, "!", "20000"})
		w.groups = append(w.groups, worldGroup{name, uid, nil})
		if i := slices.Index(a, "-G"); i > 0 {
			for _, g := range strings.Split(a[i+1], ",") {
				w.group(g).members = append(w.group(g).members, name)
			}
		}
		if err := os.Mkdir(home, 0o755); err != nil {
			return nil, err
		}
	case "chpasswd":
		name, value, _ := strings.Cut(strings.TrimSuffix(string(c.Secret), "\n"), ":")
		u := w.user(name)
		if len(a) == 2 && a[1] == "-e" {
			u.hash = value
		} else {
			sum := sha256.Sum256([]byte(value))
			u.hash = "$y$demo$" + hex.EncodeToString(sum[:8])
		}
		u.lastchg = "20001"
	case "chage":
		day := a[2]
		if day == "-1" {
			day = ""
		}
		w.user(a[3]).lastchg = day
	case "usermod": // -aG list name
		name := a[3]
		for _, gname := range strings.Split(a[2], ",") {
			if g := w.group(gname); !slices.Contains(g.members, name) {
				g.members = append(g.members, name)
			}
		}
	case "gpasswd": // -d name group, which fails for a non-member like the real one
		g := w.group(a[3])
		if !slices.Contains(g.members, a[2]) {
			return []byte("gpasswd: user is not a member"), errors.New("exit status 3")
		}
		g.members = slices.DeleteFunc(g.members, func(m string) bool { return m == a[2] })
	case "userdel":
		name := a[2]
		u := w.user(name)
		if err := os.RemoveAll(u.home); err != nil {
			return nil, err
		}
		w.users = slices.DeleteFunc(w.users, func(x worldUser) bool { return x.name == name })
		w.groups = slices.DeleteFunc(w.groups, func(g worldGroup) bool { return g.name == name })
		for i := range w.groups {
			w.groups[i].members = slices.DeleteFunc(w.groups[i].members, func(m string) bool { return m == name })
		}
	default:
		return nil, fmt.Errorf("the fake host does not know %s", a[0])
	}
	w.flush()
	return nil, nil
}

type userFixture struct {
	*userWorld
	runner  *Runner
	journal *Journal
	jdir    string
	audited []AuditEntry
}

// newUserFixture runs the user actions as the human with the given UID behind sudo.
func newUserFixture(t *testing.T, actor int) *userFixture {
	t.Helper()
	w := newUserWorld(t)
	reg := NewRegistry()
	w.host.lookup = w.lookup
	if err := RegisterUserActions(reg, w.host); err != nil {
		t.Fatal(err)
	}
	f := &userFixture{userWorld: w, jdir: filepath.Join(t.TempDir(), "host", "journal")}
	j, err := OpenJournal(f.jdir, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	f.journal = j
	clock := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	f.runner = &Runner{
		Registry: reg, Journal: j, Exec: w, SudoUID: actor,
		Now:    func() time.Time { clock = clock.Add(time.Second); return clock },
		Record: func(a AuditEntry) error { f.audited = append(f.audited, a); return nil },
	}
	return f
}

func (f *userFixture) apply(t *testing.T, id string, secret string, args ...string) Result {
	t.Helper()
	var in []byte
	if secret != "" {
		in = []byte(secret)
	}
	res, err := f.runner.Apply(context.Background(), id, args, in)
	if err != nil {
		t.Fatalf("%s %v: %v", id, args, err)
	}
	if res.Entry.Result != ResultApplied {
		t.Fatalf("%s %v: %+v", id, args, res.Entry)
	}
	return res
}

func (f *userFixture) undo(t *testing.T, id string) Result {
	t.Helper()
	res, err := f.runner.Undo(context.Background(), id)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if res.Entry.Result != ResultApplied {
		t.Fatalf("undo: %+v", res.Entry)
	}
	return res
}

// journalText is everything the journal directory holds.
func (f *userFixture) journalText(t *testing.T) string {
	t.Helper()
	files, err := os.ReadDir(f.jdir)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, de := range files {
		b, _ := os.ReadFile(filepath.Join(f.jdir, de.Name()))
		all.Write(b)
	}
	return all.String()
}

// demoKey is a freshly generated public key: its authorized_keys line, base64 body and fingerprint.
type demoKey struct{ line, blob, fp string }

func newDemoKey(t *testing.T, comment string) demoKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(k)), "\n")
	blob := strings.Fields(line)[1]
	if comment != "" {
		line += " " + comment
	}
	return demoKey{line: line, blob: blob, fp: ssh.FingerprintSHA256(k)}
}
