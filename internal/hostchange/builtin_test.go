package hostchange

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

func builtinHome(t *testing.T) (hostadmin.Account, accountLookup) {
	t.Helper()
	acc := hostadmin.Account{UID: os.Getuid(), GID: os.Getgid(), Home: t.TempDir()}
	return acc, func(string) (hostadmin.Account, error) { return acc, nil }
}

func TestBuiltinDisplayAndValidation(t *testing.T) {
	k := newDemoKey(t, "")
	c := addKeyCommand("demo-user", []string{k.line}, k.fp)
	if c.Display() != "[builtin] authorized-keys-add demo-user < <ssh key "+k.fp+">" {
		t.Errorf("display %q", c.Display())
	}
	if err := validateCommand(Command{Argv: []string{"rm", "-rf", "/"}, Builtin: true}); err == nil {
		t.Error("an unknown built-in step must be refused")
	}
	if _, err := (SystemExecutor{}).Run(t.Context(), Command{Argv: []string{"cat"}, Builtin: true}); err == nil {
		t.Error("the system executor must refuse an unknown built-in step")
	}
}

func TestBuiltinAppendsAfterALastLineWithoutNewline(t *testing.T) {
	acc, lookup := builtinHome(t)
	if err := os.Mkdir(filepath.Join(acc.Home, ".ssh"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(acc.Home, ".ssh", "authorized_keys")
	old := newDemoKey(t, "old")
	if err := os.WriteFile(file, []byte(old.line), 0o644); err != nil {
		t.Fatal(err)
	}
	k := newDemoKey(t, "")
	if err := runBuiltin(addKeyCommand("demo-user", []string{k.line}, k.fp), lookup); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	fi, _ := os.Stat(file)
	di, _ := os.Stat(filepath.Join(acc.Home, ".ssh"))
	if string(b) != old.line+"\n"+k.line+"\n" || fi.Mode().Perm() != 0o644 || di.Mode().Perm() != 0o755 {
		t.Fatalf("file %q mode %v dir %v: an existing file and directory keep their modes", b, fi.Mode().Perm(), di.Mode().Perm())
	}
	if err := runBuiltin(addKeyCommand("demo-user", []string{k.line}, k.fp), lookup); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("a key that is there already: %v", err)
	}
	if err := runBuiltin(Command{Argv: []string{stepKeysAdd, "demo-user"}, Builtin: true, Secret: []byte("# no key\n")}, lookup); err == nil {
		t.Error("input without a key must be refused")
	}
	if err := runBuiltin(Command{Argv: []string{stepKeysRemove, "demo-user", newDemoKey(t, "").fp}, Builtin: true}, lookup); err == nil {
		t.Error("removing a missing key must fail")
	}
}

func TestBuiltinRefusesLinks(t *testing.T) {
	k := newDemoKey(t, "")
	add := addKeyCommand("demo-user", []string{k.line}, k.fp)
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	acc, lookup := builtinHome(t)
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(acc.Home, ".ssh")); err != nil {
		t.Fatal(err)
	}
	if err := runBuiltin(add, lookup); err == nil {
		t.Error("a linked .ssh must be refused")
	}

	acc, lookup = builtinHome(t)
	if err := os.Mkdir(filepath.Join(acc.Home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(acc.Home, ".ssh", "authorized_keys")); err != nil {
		t.Fatal(err)
	}
	if err := runBuiltin(add, lookup); err == nil {
		t.Error("a linked authorized_keys must be refused")
	}
	if err := runBuiltin(Command{Argv: []string{stepKeysRemove, "demo-user", k.fp}, Builtin: true}, lookup); err == nil {
		t.Error("a linked authorized_keys must be refused on removal")
	}

	acc, lookup = builtinHome(t)
	if err := os.Mkdir(filepath.Join(acc.Home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(acc.Home, ".ssh", "authorized_keys")); err != nil {
		t.Fatal(err)
	}
	if err := runBuiltin(add, lookup); err == nil || !strings.Contains(err.Error(), "not a plain file") {
		t.Errorf("a hard link must be refused: %v", err)
	}
	if b, _ := os.ReadFile(outside); string(b) != "keep\n" {
		t.Errorf("the outside file changed: %q", b)
	}
}
