package autounlock

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const testPassphrase = "example-passphrase-1"

type call struct {
	name  string
	args  []string
	stdin []byte
}

// fakeRunner records calls; it never starts a process.
type fakeRunner struct {
	calls     []call
	tpm2      bool
	creds     map[string]string // credential path -> content written by the fake "encrypt"
	failCreds error
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	f.calls = append(f.calls, call{name, append([]string(nil), args...), append([]byte(nil), stdin...)})
	switch name {
	case "systemd-analyze":
		if f.tpm2 {
			return []byte("yes\n"), nil
		}
		return []byte("no\n"), errors.New("exit status 1")
	case "systemd-creds":
		if f.failCreds != nil {
			return nil, f.failCreds
		}
		// Stand-in for the sealed file; the fake keeps the key in a plain file for the load tests.
		return nil, os.WriteFile(args[len(args)-1], []byte("sealed"), 0o600)
	}
	return nil, errors.New("unexpected program")
}

func newVault(t *testing.T) (string, *vault.Vault) {
	t.Helper()
	vault.LowerWorkFactorForTests()
	dir := filepath.Join(t.TempDir(), "vault")
	v := vault.New(dir)
	if err := v.Create(testPassphrase); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Add("example", vault.KindSecret, map[string]string{vault.FieldValue: "example-value"}); err != nil {
		t.Fatal(err)
	}
	return dir, v
}

func binder(dir string, r Runner, root bool) *Binder {
	return &Binder{Dir: dir, Runner: r, IsRoot: func() bool { return root },
		Passphrase: func(io.Reader) (string, error) { return testPassphrase, nil }}
}

func keyOf(t *testing.T, v *vault.Vault) string {
	t.Helper()
	id, err := v.Identity()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func credDir(t *testing.T, key string) string {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "vault-key"), []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBindPassesKeyOnlyOnStdin(t *testing.T) {
	dir, v := newVault(t)
	key := keyOf(t, v)
	r := &fakeRunner{}
	mode, err := binder(dir, r, true).Bind(context.Background(), strings.NewReader(""))
	if err != nil || mode != ModeHost {
		t.Fatalf("Bind() = %q, %v", mode, err)
	}
	var creds *call
	for i, c := range r.calls {
		for _, a := range c.args {
			if strings.Contains(a, "AGE-SECRET-KEY") || strings.Contains(a, key) {
				t.Errorf("argument %q carries key material", a)
			}
		}
		if c.name == "systemd-creds" {
			creds = &r.calls[i]
		}
	}
	if creds == nil {
		t.Fatal("systemd-creds was not called")
	}
	want := []string{"encrypt", "--with-key=auto", "--name=vault-key", "-", filepath.Join(dir, "key.cred")}
	if strings.Join(creds.args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args = %v, want %v", creds.args, want)
	}
	if _, err := age.ParseX25519Identity(strings.TrimSpace(string(creds.stdin))); err != nil {
		t.Errorf("stdin is no vault identity: %v", err)
	}
	if string(creds.stdin) != key {
		t.Error("stdin is not the vault's own key")
	}
	if got := recordedMode(dir); got != ModeHost {
		t.Errorf("marker = %q", got)
	}
}

func TestBindReportsTPM2(t *testing.T) {
	dir, _ := newVault(t)
	mode, err := binder(dir, &fakeRunner{tpm2: true}, true).Bind(context.Background(), nil)
	if err != nil || mode != ModeTPM2 || recordedMode(dir) != ModeTPM2 {
		t.Fatalf("Bind() = %q, %v, marker %q", mode, err, recordedMode(dir))
	}
}

func TestBindRequiresRootBeforeAnything(t *testing.T) {
	dir, _ := newVault(t)
	r := &fakeRunner{}
	b := binder(dir, r, false)
	b.Passphrase = func(io.Reader) (string, error) { t.Error("passphrase was asked"); return "", nil }
	if _, err := b.Bind(context.Background(), nil); !errors.Is(err, ErrNotRoot) {
		t.Fatalf("err = %v", err)
	}
	if len(r.calls) != 0 {
		t.Errorf("runner called: %v", r.calls)
	}
}

func TestBindFailureLeavesNoBinding(t *testing.T) {
	dir, _ := newVault(t)
	r := &fakeRunner{failCreds: errors.New("boom")}
	if _, err := binder(dir, r, true).Bind(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "key.cred")); err == nil {
		t.Error("key.cred left behind")
	}
}

func TestBindWrongPassphrase(t *testing.T) {
	dir, _ := newVault(t)
	r := &fakeRunner{}
	b := binder(dir, r, true)
	b.Passphrase = func(io.Reader) (string, error) { return "wrong", nil }
	if _, err := b.Bind(context.Background(), nil); !errors.Is(err, vault.ErrWrongPassphrase) {
		t.Fatalf("err = %v", err)
	}
	if len(r.calls) != 0 {
		t.Errorf("runner called: %v", r.calls)
	}
}

func TestUnbind(t *testing.T) {
	dir, _ := newVault(t)
	b := binder(dir, &fakeRunner{}, true)
	if _, err := b.Bind(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	removed, err := b.Unbind()
	if err != nil || !removed {
		t.Fatalf("Unbind() = %v, %v", removed, err)
	}
	for _, f := range []string{"key.cred", "key.cred.mode"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s still exists", f)
		}
	}
	if removed, err := b.Unbind(); err != nil || removed {
		t.Errorf("second Unbind() = %v, %v", removed, err)
	}
	if _, err := binder(dir, &fakeRunner{}, false).Unbind(); !errors.Is(err, ErrNotRoot) {
		t.Errorf("non-root err = %v", err)
	}
}

func TestLoadOpensVault(t *testing.T) {
	dir, v := newVault(t)
	cd := credDir(t, keyOf(t, v))
	got, st := Load(dir, cd)
	if got == nil {
		t.Fatalf("vault locked: %+v", st)
	}
	if _, err := got.GetByName("example"); err != nil {
		t.Errorf("GetByName: %v", err)
	}
	if st.Unlock != ModeHost || st.Message != "" {
		t.Errorf("status = %+v", st)
	}
}

func TestLoadFailuresStayLocked(t *testing.T) {
	dir, v := newVault(t)
	good := keyOf(t, v)
	other, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	cases := map[string]string{
		"no env":        "",
		"missing file":  empty,
		"empty":         credDir(t, ""),
		"blank":         credDir(t, "\n"),
		"invalid":       credDir(t, "not-a-key"),
		"other key":     credDir(t, other.String()),
		"unreadable":    filepath.Join(empty, "nope"),
		"truncated key": credDir(t, good[:20]),
	}
	for name, cd := range cases {
		t.Run(name, func(t *testing.T) {
			got, st := Load(dir, cd)
			if got != nil || st.Unlock != ModeManual || st.Message == "" || st.Warning() != "" {
				t.Errorf("got %v, %+v", got, st)
			}
			if (name == "empty" || name == "blank" || name == "missing file") && st.Message != noCredentialMsg {
				t.Errorf("message = %q, want the no-credential message", st.Message)
			}
			for _, k := range []string{good, other.String(), "AGE-SECRET-KEY"} {
				if strings.Contains(st.Message, k) {
					t.Errorf("message leaks key: %q", st.Message)
				}
			}
		})
	}
	if got, _ := Load(filepath.Join(t.TempDir(), "none"), credDir(t, good)); got != nil {
		t.Error("absent vault opened")
	}
}

func TestLoadFromEnv(t *testing.T) {
	dir, v := newVault(t)
	t.Setenv("CREDENTIALS_DIRECTORY", credDir(t, keyOf(t, v)))
	if got, _ := LoadFromEnv(dir); got == nil {
		t.Error("vault locked")
	}
	t.Setenv("CREDENTIALS_DIRECTORY", "")
	if got, st := LoadFromEnv(dir); got != nil || st.Unlock != ModeManual {
		t.Errorf("got %v, %+v", got, st)
	}
}

func TestPassphraseChangeNeedsNoNewBinding(t *testing.T) {
	dir, v := newVault(t)
	if _, err := binder(dir, &fakeRunner{tpm2: true}, true).Bind(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	cd := credDir(t, keyOf(t, v)) // what the service manager would unseal
	if err := v.ChangePassphrase(testPassphrase, "example-passphrase-2"); err != nil {
		t.Fatal(err)
	}
	got, st := Load(dir, cd)
	if got == nil || st.Unlock != ModeTPM2 {
		t.Fatalf("got %v, %+v", got, st)
	}
}

func TestModeWarning(t *testing.T) {
	for m, warn := range map[Mode]bool{ModeManual: false, ModeTPM2: true, ModeHost: true} {
		if (m.Warning() != "") != warn || m.Automatic() != warn || !m.valid() {
			t.Errorf("mode %q: warning %q", m, m.Warning())
		}
	}
	if Mode("x").valid() {
		t.Error("unknown mode valid")
	}
}

func TestExecRunnerDisabledInTests(t *testing.T) {
	if _, err := ExecRunner().Run(context.Background(), "systemd-creds", nil, nil); err == nil {
		t.Fatal("real runner ran inside a test binary")
	}
}

func TestBindWritesNothingButBindingFiles(t *testing.T) {
	dir, v := newVault(t)
	// A pending entry written while the vault was locked, as the service does.
	locked := vault.New(dir)
	if _, err := locked.Add("later", vault.KindSecret, map[string]string{vault.FieldValue: "example-later"}); err != nil {
		t.Fatal(err)
	}
	_ = v
	snapshot := func() map[string]string {
		out := map[string]string{}
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, _ := os.ReadFile(p)
			info, _ := d.Info()
			out[p] = string(data) + info.ModTime().String()
			return nil
		})
		return out
	}
	before := snapshot()
	if len(before) < 4 {
		t.Fatalf("expected a pending entry, got %v", len(before))
	}
	if _, err := binder(dir, &fakeRunner{}, true).Bind(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	after := snapshot()
	for p, content := range before {
		if after[p] != content {
			t.Errorf("%s changed", p)
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok && filepath.Base(p) != "key.cred" && filepath.Base(p) != "key.cred.mode" {
			t.Errorf("unexpected new file %s", p)
		}
	}
}

func TestBindHandsFilesToDirectoryOwner(t *testing.T) {
	dir, _ := newVault(t)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	var got []string
	b := binder(dir, &fakeRunner{}, true)
	b.chown = func(p string, uid, gid int) error {
		if uid != int(st.Uid) || gid != int(st.Gid) {
			t.Errorf("chown %s to %d:%d", p, uid, gid)
		}
		got = append(got, filepath.Base(p))
		return nil
	}
	if _, err := b.Bind(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "key.cred,key.cred.mode" {
		t.Errorf("chown calls = %v", got)
	}

	// With the real chown the owner matches the directory.
	b = binder(dir, &fakeRunner{}, true)
	if _, err := b.Bind(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"key.cred", "key.cred.mode"} {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		fs := fi.Sys().(*syscall.Stat_t)
		if fs.Uid != st.Uid || fs.Gid != st.Gid {
			t.Errorf("%s owner %d:%d, want %d:%d", f, fs.Uid, fs.Gid, st.Uid, st.Gid)
		}
	}

	// A failing chown rolls the binding back.
	b.chown = func(string, int, int) error { return errors.New("denied") }
	if _, err := b.Bind(context.Background(), nil); err == nil {
		t.Fatal("expected an error")
	}
	for _, f := range []string{"key.cred", "key.cred.mode"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s left behind", f)
		}
	}
}
