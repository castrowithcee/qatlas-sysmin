package hostadmin

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

const demoGroups = `root:x:0:
sudo:x:27:demo-admin
docker:x:999:demo-user
qsysmin:x:990:demo-admin,demo-user
staff:x:50:demo-user
demo-admin:x:1000:
demo-user:x:1001:
demo-boss-grp:x:1002:
`

func demoPasswd(adminHome, userHome string) string {
	return fmt.Sprintf(`root:x:0:0:root:/root:/bin/bash
svc-demo:x:998:998::/nonexistent:/usr/sbin/nologin
demo-admin:x:1000:1000:Demo Admin:%s:/bin/bash
demo-user:x:1001:1001::%s:/bin/bash
demo-boss:x:1002:999::/home/demo-boss:/bin/bash
# a comment
broken-line
`, adminHome, userHome)
}

// demoShadow expires demo-boss long ago and demo-user far in the future.
const demoShadow = `root:$6$fake:19000:0:99999:7:::
svc-demo:*:19000:0:99999:7:::
demo-admin:$6$fake:19000:0:99999:7:::
demo-user:!$6$fake:19000:0:99999:7::40000:
demo-boss:$6$fake:19000:0:99999:7::20000:
`

type fixture struct {
	dir  string
	host *Host
	pub  string // authorized_keys line of demo-admin
	blob string // base64 body of that key
}

func newFixture(t *testing.T, privileged bool) *fixture {
	t.Helper()
	dir := t.TempDir()
	adminHome := filepath.Join(dir, "home", "demo-admin")
	userHome := filepath.Join(dir, "home", "demo-user")
	if err := os.MkdirAll(filepath.Join(adminHome, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	pubKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		t.Fatal(err)
	}
	blob := base64.StdEncoding.EncodeToString(sshKey.Marshal())
	line := "from=\"192.0.2.1\" ssh-ed25519 " + blob + " demo-admin@demo-laptop\n"
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if err := os.WriteFile(filepath.Join(adminHome, ".ssh", "authorized_keys"), []byte("# demo\n"+line+"not a key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewHost(privileged)
	h.PasswdPath = write("passwd", demoPasswd(adminHome, userHome))
	h.GroupPath = write("group", demoGroups)
	h.ShadowPath = write("shadow", demoShadow)
	h.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) } // day 20454
	return &fixture{dir: dir, host: h, pub: line, blob: blob}
}

func TestUsersAndFlags(t *testing.T) {
	f := newFixture(t, false)
	users, err := f.host.Users()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]User{}
	for _, u := range users {
		byName[u.Name] = u
	}
	if len(users) != 5 {
		t.Fatalf("got %d users, want 5 (broken and comment lines skipped)", len(users))
	}
	admin := byName["demo-admin"]
	if admin.UID != 1000 || admin.PrimaryGroup != "demo-admin" || !reflect.DeepEqual(admin.Groups, []string{"qsysmin", "sudo"}) {
		t.Errorf("demo-admin = %+v", admin)
	}
	if !admin.Admin || admin.Docker || !admin.Enabled || admin.System {
		t.Errorf("demo-admin flags = %+v", admin)
	}
	user := byName["demo-user"]
	if user.Admin || !user.Docker || !user.Enabled || !reflect.DeepEqual(user.Groups, []string{"docker", "qsysmin", "staff"}) {
		t.Errorf("demo-user = %+v", user)
	}
	// The primary group counts although the member list does not name the user.
	boss := byName["demo-boss"]
	if !boss.Docker || boss.PrimaryGroup != "docker" || len(boss.Groups) != 0 || boss.Groups == nil {
		t.Errorf("demo-boss = %+v", boss)
	}
	if !byName["svc-demo"].System || !byName["root"].System || byName["demo-admin"].System {
		t.Error("system flag is wrong")
	}
	if _, err := f.host.User("nobody-here"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown user error = %v", err)
	}
}

func TestGroupsIncludePrimaryMembers(t *testing.T) {
	f := newFixture(t, false)
	g, err := f.host.Group("docker")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Members, []string{"demo-boss", "demo-user"}) || g.GID != 999 || g.System != true {
		t.Errorf("docker = %+v", g)
	}
	g, _ = f.host.Group("demo-user")
	if !reflect.DeepEqual(g.Members, []string{"demo-user"}) || g.System {
		t.Errorf("demo-user group = %+v", g)
	}
	all, err := f.host.Groups()
	if err != nil || len(all) != 8 {
		t.Errorf("Groups = %d, %v", len(all), err)
	}
	if _, err := f.host.Group("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown group error = %v", err)
	}
}

func TestIsAdmin(t *testing.T) {
	f := newFixture(t, false)
	for uid, want := range map[int]bool{1000: true, 1001: false, 1002: false, 4242: false} {
		if got, err := f.host.IsAdmin(uid); err != nil || got != want {
			t.Errorf("IsAdmin(%d) = %v, %v; want %v", uid, got, err, want)
		}
	}
}

func TestIsMember(t *testing.T) {
	f := newFixture(t, false)
	for _, tc := range []struct {
		uid   int
		group string
		want  bool
	}{
		{1001, "docker", true}, {1001, "qsysmin", true}, {1000, "qsysmin", true}, {1001, "sudo", false}, {1000, "no-such-group", false},
		{4242, "docker", false},
	} {
		if got, err := f.host.IsMember(tc.uid, tc.group); err != nil || got != tc.want {
			t.Errorf("IsMember(%d, %q) = %v, %v; want %v", tc.uid, tc.group, got, err, tc.want)
		}
	}
}

func TestUnprivilegedFieldsAreUnknown(t *testing.T) {
	f := newFixture(t, false)
	u, err := f.host.User("demo-admin")
	if err != nil {
		t.Fatal(err)
	}
	if u.Locked != nil || u.Expired != nil || u.SSHKeys != nil {
		t.Errorf("privileged fields known without privilege: %+v", u)
	}
}

func TestPrivilegedFields(t *testing.T) {
	f := newFixture(t, true)
	users, err := f.host.Users()
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]User{}
	for _, u := range users {
		by[u.Name] = u
	}
	check := func(name string, locked, expired bool) {
		t.Helper()
		u := by[name]
		if u.Locked == nil || *u.Locked != locked || u.Expired == nil || *u.Expired != expired {
			t.Errorf("%s locked/expired = %v/%v, want %v/%v", name, u.Locked, u.Expired, locked, expired)
		}
	}
	check("demo-admin", false, false)
	check("demo-user", true, false)
	check("svc-demo", true, false)
	check("demo-boss", false, true)

	keys := by["demo-admin"].SSHKeys
	raw, _ := base64.StdEncoding.DecodeString(f.blob)
	sum := sha256.Sum256(raw)
	want := SSHKey{Type: "ssh-ed25519", Comment: "demo-admin@demo-laptop",
		Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])}
	if len(keys) != 1 || keys[0] != want {
		t.Errorf("keys = %+v, want [%+v]", keys, want)
	}
	// No file is an empty list, not unknown.
	if k := by["demo-user"].SSHKeys; k == nil || len(k) != 0 {
		t.Errorf("demo-user keys = %#v, want empty list", k)
	}
}

func TestAuthorizedKeysThroughLinkIsUnknown(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ".ssh", "authorized_keys")); err != nil {
		t.Fatal(err)
	}
	if keys := readAuthorizedKeys(dir); keys != nil {
		t.Errorf("keys through a link = %v, want unknown", keys)
	}
}

func TestExpiry(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) // day 20454
	for expire, want := range map[string]bool{"": false, "-1": false, "20453": true, "20454": true, "20455": false} {
		got := shadowEntry{expire: expire}.expired(now)
		if got == nil || *got != want {
			t.Errorf("expire %q = %v, want %v", expire, got, want)
		}
	}
	if (shadowEntry{expire: "abc"}).expired(now) != nil {
		t.Error("an unreadable expiry must be unknown")
	}
}

func TestLevels(t *testing.T) {
	for _, tt := range []struct {
		bits uint32
		dir  bool
		want Access
	}{
		{7, false, AccessReadWrite}, {6, false, AccessReadWrite}, {4, false, AccessRead}, {5, false, AccessRead},
		{2, false, AccessNothing}, {0, false, AccessNothing},
		{7, true, AccessReadWrite}, {5, true, AccessRead}, {4, true, AccessNothing}, {6, true, AccessNothing},
		{3, true, AccessNothing}, {1, true, AccessNothing}, {0, true, AccessNothing},
	} {
		if got := level(tt.bits, tt.dir); got != tt.want {
			t.Errorf("level(%o, dir=%v) = %q, want %q", tt.bits, tt.dir, got, tt.want)
		}
	}
}

func folderHost(t *testing.T) (*Host, string) {
	t.Helper()
	f := newFixture(t, false)
	// The owner of the files is the test process, so the fixture must know it.
	line := fmt.Sprintf("demo-owner:x:%d:%d::/home/demo-owner:/bin/bash\n", os.Getuid(), os.Getgid())
	grp := fmt.Sprintf("demo-owner-grp:x:%d:\n", os.Getgid())
	// The owner comes first because the first match wins.
	prepend := func(p, add string) {
		old, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, append([]byte(add), old...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prepend(f.host.PasswdPath, line)
	prepend(f.host.GroupPath, grp)
	f.host.Xattrs = func(string) ([]string, error) { return nil, nil }
	return f.host, f.dir
}

func TestFolderDirectoryAndFile(t *testing.T) {
	h, dir := folderHost(t)
	stack := filepath.Join(dir, "srv", "demo-stack")
	if err := os.MkdirAll(stack, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stack, 0o750); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(stack, "app.env")
	if err := os.WriteFile(file, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o640); err != nil {
		t.Fatal(err)
	}

	d, err := h.Folder(stack)
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "directory" || d.Owner != "demo-owner" || d.Group != "demo-owner-grp" || d.Mode != "0750" {
		t.Errorf("directory = %+v", d)
	}
	if d.OwnerAcc != AccessReadWrite || d.GroupAcc != AccessRead || d.OthersAcc != AccessNothing {
		t.Errorf("directory levels = %q/%q/%q", d.OwnerAcc, d.GroupAcc, d.OthersAcc)
	}
	if d.ACL == nil || *d.ACL || d.Protected || d.Setuid || d.Setgid || d.Sticky {
		t.Errorf("directory extras = %+v", d)
	}

	fl, err := h.Folder(file)
	if err != nil {
		t.Fatal(err)
	}
	if fl.Kind != "file" || fl.OwnerAcc != AccessReadWrite || fl.GroupAcc != AccessRead || fl.OthersAcc != AccessNothing {
		t.Errorf("file = %+v", fl)
	}
	if _, err := h.Folder(filepath.Join(stack, "missing")); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing path error = %v", err)
	}
}

func TestFolderSpecialBitsAndACL(t *testing.T) {
	h, dir := folderHost(t)
	shared := filepath.Join(dir, "shared")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, os.ModeSetgid|os.ModeSticky|0o770); err != nil {
		t.Fatal(err)
	}
	prog := filepath.Join(dir, "prog")
	if err := os.WriteFile(prog, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(prog, os.ModeSetuid|0o755); err != nil {
		t.Fatal(err)
	}
	s, err := h.Folder(shared)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Setgid || !s.Sticky || s.Setuid || s.Mode != "0770" {
		t.Errorf("shared = %+v", s)
	}
	p, _ := h.Folder(prog)
	if !p.Setuid || p.Setgid || p.Sticky {
		t.Errorf("prog = %+v", p)
	}

	var asked string
	h.Xattrs = func(path string) ([]string, error) {
		asked = path
		return []string{"user.note", "system.posix_acl_default"}, nil
	}
	if s, _ = h.Folder(shared); s.ACL == nil || !*s.ACL || asked == "" {
		t.Errorf("ACL hint missing: %+v", s.ACL)
	}
	h.Xattrs = func(string) ([]string, error) { return nil, errors.New("denied") }
	if s, _ = h.Folder(shared); s.ACL != nil {
		t.Errorf("a failed listing must be unknown, got %v", *s.ACL)
	}
}

func TestProtectedPaths(t *testing.T) {
	h, dir := folderHost(t)
	prot := filepath.Join(dir, "etc-like")
	safe := filepath.Join(dir, "safe")
	for _, d := range []string{filepath.Join(prot, "sub"), filepath.Join(safe, "etc-like"), filepath.Join(safe, "plain")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(prot, filepath.Join(safe, "link")); err != nil {
		t.Fatal(err)
	}
	h.ProtectedRoots = []string{prot}
	// The strings are joined by hand because filepath.Join would resolve ".." and hide the case.
	for path, want := range map[string]bool{
		prot:                        true,
		prot + "/sub":               true,
		safe + "/../etc-like":       true,
		safe + "/link":              true,
		safe + "/link/sub":          true,
		safe + "/link/../etc-like":  true, // lexically safe/etc-like, physically the protected directory
		safe + "/plain":             false,
		safe + "/plain/../etc-like": false,
		dir:                         false,
	} {
		f, err := h.Folder(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if f.Protected != want {
			t.Errorf("Protected(%s) = %v, want %v (resolved %s)", path, f.Protected, want, f.Resolved)
		}
	}
}

func TestDefaultProtectedRoots(t *testing.T) {
	h := NewHost(false)
	h.Xattrs = func(string) ([]string, error) { return nil, nil }
	for path, want := range map[string]bool{
		"/": true, "/etc": true, "/etc/../etc": true, "/usr/bin/../bin": true, "/proc": true, "/sys": true,
		"/tmp/../etc": true, "/tmp": false,
	} {
		f, err := h.Folder(path)
		if err != nil {
			t.Logf("%s: %v (skipped)", path, err)
			continue
		}
		if f.Protected != want {
			t.Errorf("Protected(%s) = %v, want %v", path, f.Protected, want)
		}
	}
}

func TestRegularRangeBoundaries(t *testing.T) {
	for id, want := range map[int]bool{0: false, 999: false, 1000: true, 60000: true, 60001: false, 65534: false} {
		if got := Regular(id); got != want {
			t.Errorf("Regular(%d) = %v, want %v", id, got, want)
		}
	}
}
