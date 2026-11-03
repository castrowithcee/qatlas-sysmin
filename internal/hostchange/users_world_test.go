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
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// userWorld is an invented host for the user actions: the fake executor applies useradd, usermod, chpasswd,
// chage, userdel and groupadd to an in-memory model and writes passwd, group and shadow files from it, and runs
// the built-in steps for real inside a temporary home. Every account maps to the test's own UID on disk. The
// tools behave like those of Debian: userdel removes the user's own group only where nothing else uses it.
type userWorld struct {
	t       *testing.T
	dir     string
	users   []worldUser
	groups  []worldGroup
	calls   []string
	stdins  map[string]string // displayed command -> standard input
	failOn  string            // displayed command that fails and echoes its standard input
	shells  map[string]string // login shell by user; a missing one is /bin/bash
	expires map[string]string // expiry day by user; a missing one is none
	gecos   map[string]string // comment field by user; a missing one is empty
	aging   map[string][4]string
	running map[int]bool // UIDs with a process in the process table
	// gshadow holds the group password and administrators by group, as "password:administrators"; a missing
	// group has the Debian default "!:".
	gshadow map[string]string
	// keepGroups makes userdel keep the user's own group, as it does with USERGROUPS_ENAB no.
	keepGroups bool
	// useraddShell, when set, replaces the shell a useradd call asks for.
	useraddShell string
	host         UserHost
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
	w := &userWorld{t: t, dir: dir, stdins: map[string]string{}, shells: map[string]string{}, expires: map[string]string{},
		gecos: map[string]string{}, aging: map[string][4]string{}, running: map[int]bool{}, gshadow: map[string]string{}}
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
		{"qsysmin", 990, []string{"demo-admin", "demo-other"}},
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
	for _, d := range []string{"mail", "log", "proc/self"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := hostadmin.NewHost(true)
	h.PasswdPath, h.GroupPath, h.ShadowPath = filepath.Join(dir, "passwd"), filepath.Join(dir, "group"), filepath.Join(dir, "shadow")
	w.host = UserHost{
		Accounts: h, HomeBase: filepath.Join(dir, "home"), MailDir: filepath.Join(dir, "mail"),
		GShadow: filepath.Join(dir, "gshadow"),
		Lastlog: filepath.Join(dir, "log", "lastlog"), Lastlog2: filepath.Join(dir, "lastlog2.db"),
		Proc: filepath.Join(dir, "proc"),
	}
	// A classic lastlog with room for the first regular users and no login recorded.
	if err := os.WriteFile(w.host.Lastlog, make([]byte, 1001*lastlogRecord), 0o644); err != nil {
		t.Fatal(err)
	}
	w.flush()
	return w
}

func (w *userWorld) flush() {
	var pw, gr, sh, gs strings.Builder
	for _, u := range w.users {
		shell := w.shells[u.name]
		if shell == "" {
			shell = "/bin/bash"
		}
		fmt.Fprintf(&pw, "%s:x:%d:%d:%s:%s:%s\n", u.name, u.uid, u.gid, w.gecos[u.name], u.home, shell)
		ag := w.agingOf(u.name)
		fmt.Fprintf(&sh, "%s:%s:%s:%s:%s:%s:%s:%s:\n", u.name, u.hash, u.lastchg, ag[0], ag[1], ag[2], ag[3], w.expires[u.name])
	}
	for _, g := range w.groups {
		fmt.Fprintf(&gr, "%s:x:%d:%s\n", g.name, g.gid, strings.Join(g.members, ","))
		secret, ok := w.gshadow[g.name]
		if !ok {
			secret = "!:"
		}
		fmt.Fprintf(&gs, "%s:%s:%s\n", g.name, secret, strings.Join(g.members, ","))
	}
	for path, content := range map[string]string{w.host.Accounts.PasswdPath: pw.String(), w.host.Accounts.GroupPath: gr.String(), w.host.Accounts.ShadowPath: sh.String(), w.host.GShadow: gs.String()} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			w.t.Fatal(err)
		}
	}
}

// agingOf is the minimum, maximum, warning and inactivity days of a user; useradd sets those of a Debian
// login.defs.
func (w *userWorld) agingOf(name string) [4]string {
	if ag, ok := w.aging[name]; ok {
		return ag
	}
	return [4]string{"0", "99999", "7", ""}
}

// process puts a process with the real and effective UID into the process table.
func (w *userWorld) process(pid, real, effective int) {
	w.t.Helper()
	dir := filepath.Join(w.host.Proc, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		w.t.Fatal(err)
	}
	status := fmt.Sprintf("Name:\tdemo\nUmask:\t0022\nUid:\t%d\t%d\t%d\t%d\nGid:\t0\t0\t0\t0\n", real, effective, effective, effective)
	if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0o644); err != nil {
		w.t.Fatal(err)
	}
	w.running[real], w.running[effective] = true, true
}

// option is the value after the first of flags in argv, the name at the end excluded.
func option(argv []string, flags ...string) (string, bool) {
	for _, f := range flags {
		if i := slices.Index(argv, f); i > 0 && i+1 < len(argv)-1 {
			return argv[i+1], true
		}
	}
	return "", false
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

func (w *userWorld) uidTaken(id int) bool {
	return slices.ContainsFunc(w.users, func(u worldUser) bool { return u.uid == id }) ||
		slices.ContainsFunc(w.groups, func(g worldGroup) bool { return g.gid == id })
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
		if w.user(name) != nil {
			return []byte("useradd: user '" + name + "' already exists"), errors.New("exit status 9")
		}
		uid := 1000
		for _, u := range w.users {
			if hostadmin.Regular(u.uid) && u.uid >= uid {
				uid = u.uid + 1
			}
		}
		if slices.Contains(a, "--system") {
			uid = 997
			for w.uidTaken(uid) {
				uid--
			}
		}
		if v, ok := option(a, "-u"); ok {
			uid, _ = strconv.Atoi(v)
			if slices.ContainsFunc(w.users, func(u worldUser) bool { return u.uid == uid }) {
				return []byte("useradd: UID " + v + " is not unique"), errors.New("exit status 4")
			}
		}
		var further []string
		if v, ok := option(a, "-G"); ok {
			further = strings.Split(v, ",")
		}
		for _, g := range further {
			if w.group(g) == nil {
				return []byte("useradd: group '" + g + "' does not exist"), errors.New("exit status 6")
			}
		}
		gid := uid
		if v, ok := option(a, "-g"); ok {
			g := w.group(v)
			if g == nil {
				return []byte("useradd: group '" + v + "' does not exist"), errors.New("exit status 6")
			}
			gid = g.gid
		} else {
			w.groups = append(w.groups, worldGroup{name, uid, nil})
		}
		home, _ := option(a, "-d", "--home-dir")
		if v, ok := option(a, "-s", "--shell"); ok {
			w.shells[name] = v
		}
		if w.useraddShell != "" {
			w.shells[name] = w.useraddShell
		}
		if v, ok := option(a, "-c"); ok {
			w.gecos[name] = v
		}
		w.users = append(w.users, worldUser{name, uid, gid, home, "!", "20000"})
		for _, g := range further {
			w.group(g).members = append(w.group(g).members, name)
		}
		if slices.Contains(a, "-m") || slices.Contains(a, "--create-home") {
			if err := os.Mkdir(home, 0o755); err != nil {
				return nil, err
			}
		}
	case "groupadd": // [-g gid] name; without -g the next GID after the highest regular one, like Debian
		name := a[len(a)-1]
		if w.group(name) != nil {
			return []byte("groupadd: group '" + name + "' already exists"), errors.New("exit status 9")
		}
		gid := 1000
		for _, g := range w.groups {
			if hostadmin.Regular(g.gid) && g.gid >= gid {
				gid = g.gid + 1
			}
		}
		if v, ok := option(a, "-g"); ok {
			gid, _ = strconv.Atoi(v)
			if slices.ContainsFunc(w.groups, func(g worldGroup) bool { return g.gid == gid }) {
				return []byte("groupadd: GID '" + v + "' already exists"), errors.New("exit status 4")
			}
		}
		w.groups = append(w.groups, worldGroup{name, gid, nil})
	case "groupdel": // refuses the primary group of a user like the real one
		name := a[len(a)-1]
		g := w.group(name)
		if g == nil {
			return []byte("groupdel: group '" + name + "' does not exist"), errors.New("exit status 6")
		}
		for _, u := range w.users {
			if u.gid == g.gid {
				return []byte("groupdel: cannot remove the primary group of user '" + u.name + "'"), errors.New("exit status 8")
			}
		}
		w.groups = slices.DeleteFunc(w.groups, func(x worldGroup) bool { return x.name == name })
		delete(w.gshadow, name)
	case "groupmod": // -n new old, in the group and the gshadow file
		g := w.group(a[3])
		switch {
		case g == nil:
			return []byte("groupmod: group '" + a[3] + "' does not exist"), errors.New("exit status 6")
		case w.group(a[2]) != nil:
			return []byte("groupmod: group '" + a[2] + "' already exists"), errors.New("exit status 9")
		}
		if secret, ok := w.gshadow[g.name]; ok {
			w.gshadow[a[2]] = secret
			delete(w.gshadow, g.name)
		}
		g.name = a[2]
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
	case "chage": // pairs of -d, -m, -M, -W, -I and -E with a day, where -1 clears the field, then the name
		name := a[len(a)-1]
		u := w.user(name)
		if u == nil {
			return []byte("chage: user '" + name + "' does not exist"), errors.New("exit status 1")
		}
		ag := w.agingOf(name)
		for i := 1; i+1 < len(a); i += 2 {
			day := a[i+1]
			if day == "-1" {
				day = ""
			}
			switch a[i] {
			case "-d":
				u.lastchg = day
			case "-m":
				ag[0] = day
			case "-M":
				ag[1] = day
			case "-W":
				ag[2] = day
			case "-I":
				ag[3] = day
			case "-E":
				w.expires[name] = day
			}
		}
		w.aging[name] = ag
	case "usermod":
		if a[1] == "-e" { // -e day name
			a = []string{a[0], "-", a[1], a[2], a[3]}
		}
		if a[1] == "-L" || a[1] == "-U" || a[1] == "-" { // -L|-U -e day name, where -L adds a "!" only to a hash without one and -U leaves a lone "!"; -e -1 clears the expiry like the real one
			u := w.user(a[4])
			switch {
			case a[1] == "-L" && !strings.HasPrefix(u.hash, "!"):
				u.hash = "!" + u.hash
			case a[1] == "-U" && strings.HasPrefix(u.hash, "!") && len(u.hash) > 1:
				u.hash = u.hash[1:]
			}
			w.expires[u.name] = a[3]
			if a[3] == "-1" {
				delete(w.expires, u.name)
			}
			break
		}
		name := a[3]
		for _, gname := range strings.Split(a[2], ",") {
			if g := w.group(gname); !slices.Contains(g.members, name) {
				g.members = append(g.members, name)
			}
		}
	case "gpasswd": // -a or -d name group, which fail for an unknown user (-a) or a non-member (-d) like the real one, or -M a,b group
		if a[1] == "-a" {
			g := w.group(a[3])
			switch {
			case w.user(a[2]) == nil:
				return []byte("gpasswd: user '" + a[2] + "' does not exist"), errors.New("exit status 3")
			case g == nil:
				return []byte("gpasswd: group '" + a[3] + "' does not exist"), errors.New("exit status 3")
			}
			if !slices.Contains(g.members, a[2]) {
				g.members = append(g.members, a[2])
			}
			break
		}
		if a[1] == "-M" {
			var members []string
			for _, m := range strings.Split(a[2], ",") {
				if w.user(m) == nil {
					return []byte("gpasswd: user '" + m + "' does not exist"), errors.New("exit status 3")
				}
				members = append(members, m)
			}
			w.group(a[3]).members = members
			break
		}
		g := w.group(a[3])
		if !slices.Contains(g.members, a[2]) {
			return []byte("gpasswd: user is not a member"), errors.New("exit status 3")
		}
		g.members = slices.DeleteFunc(g.members, func(m string) bool { return m == a[2] })
	case "userdel":
		name := a[len(a)-1]
		u := w.user(name)
		if u == nil {
			return []byte("userdel: user '" + name + "' does not exist"), errors.New("exit status 6")
		}
		if w.running[u.uid] {
			return []byte("userdel: user " + name + " is currently used by process 4711"), errors.New("exit status 8")
		}
		if slices.Contains(a, "-r") {
			for _, path := range []string{u.home, filepath.Join(w.host.MailDir, name)} {
				// Nothing outside the test directory is ever removed; a path outside it passes only where it does
				// not exist, so there is nothing to remove.
				if !strings.HasPrefix(path, w.dir+"/") {
					if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
						return nil, fmt.Errorf("the fake host removes nothing outside %s: %s", w.dir, path)
					}
					continue
				}
				if err := os.RemoveAll(path); err != nil {
					return nil, err
				}
			}
		}
		gid := u.gid
		w.users = slices.DeleteFunc(w.users, func(x worldUser) bool { return x.name == name })
		for i := range w.groups {
			w.groups[i].members = slices.DeleteFunc(w.groups[i].members, func(m string) bool { return m == name })
		}
		if g := w.group(name); !w.keepGroups && g != nil && g.gid == gid && len(g.members) == 0 &&
			!slices.ContainsFunc(w.users, func(x worldUser) bool { return x.gid == gid }) {
			w.groups = slices.DeleteFunc(w.groups, func(g worldGroup) bool { return g.name == name })
		}
		for _, m := range []map[string]string{w.shells, w.expires, w.gecos} {
			delete(m, name)
		}
		delete(w.aging, name)
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
