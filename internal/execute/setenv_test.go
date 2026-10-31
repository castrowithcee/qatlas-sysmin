package execute

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

func writeFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// testBackup returns a backup file path in a fresh directory.
func testBackup(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "backups", "demo-stack", "env.bak")
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != ".env" {
			t.Errorf("leftover file %s", e.Name())
		}
	}
}

func TestSetEnvKeepsOtherBytes(t *testing.T) {
	orig := "# header\n\n  export  APP_TAG = 1.0.0   # pinned\r\nOTHER='a b'\nDB_TAG=\"x\"\nTRAILING=1\nLAST=z"
	p := writeFile(t, orig, 0o600)
	prev, err := SetEnv(context.Background(), p, []config.Assignment{{Key: "APP_TAG", Value: "1.2.3"}, {Key: "NEW_ONE", Value: "v"}}, testBackup(t))
	if err != nil {
		t.Fatal(err)
	}
	want := "# header\n\n  export  APP_TAG = 1.2.3   # pinned\r\nOTHER='a b'\nDB_TAG=\"x\"\nTRAILING=1\nLAST=z\nNEW_ONE=v\n"
	if got := readFile(t, p); got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
	wantPrev := []EnvChange{{Key: "APP_TAG", Previous: "1.0.0", WasSet: true}, {Key: "NEW_ONE"}}
	if !reflect.DeepEqual(prev, wantPrev) {
		t.Errorf("prev = %+v", prev)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	noTempFiles(t, filepath.Dir(p))
}

func TestSetEnvPreviousValues(t *testing.T) {
	p := writeFile(t, "A=\nB='it''s'\nC=\"a\\\"b # c\" # note\nD=\"line1\nline2\"\nE=plain # c\nexport F='x'\n", 0o644)
	prev, err := SetEnv(context.Background(), p, []config.Assignment{
		{Key: "A", Value: "1"}, {Key: "C", Value: "2"}, {Key: "D", Value: "3"}, {Key: "E", Value: "4"}, {Key: "F", Value: "5"}, {Key: "Z", Value: ""}}, testBackup(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []EnvChange{{"A", "", true}, {"C", `a"b # c`, true}, {"D", "line1\nline2", true}, {"E", "plain", true}, {"F", "x", true}, {"Z", "", false}}
	if !reflect.DeepEqual(prev, want) {
		t.Errorf("prev = %+v", prev)
	}
	wantFile := "A=1\nB='it''s'\nC=2 # note\nD=3\nE=4 # c\nexport F=5\nZ=\n"
	if got := readFile(t, p); got != wantFile {
		t.Errorf("got %q want %q", got, wantFile)
	}
}

func TestSetEnvCreatesFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	prev, err := SetEnv(context.Background(), p, []config.Assignment{{Key: "A", Value: "1"}}, testBackup(t))
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, p) != "A=1\n" || prev[0].WasSet {
		t.Errorf("%q %+v", readFile(t, p), prev)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", fi.Mode())
	}
}

func TestSetEnvDuplicateKeyReplacedEverywhere(t *testing.T) {
	p := writeFile(t, "A=1\nB=2\nA=3\n", 0o600)
	prev, err := SetEnv(context.Background(), p, []config.Assignment{{Key: "A", Value: "9"}}, testBackup(t))
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, p) != "A=9\nB=2\nA=9\n" || prev[0].Previous != "3" {
		t.Errorf("%q %+v", readFile(t, p), prev)
	}
}

func TestSetEnvRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	os.WriteFile(target, []byte("A=1\n"), 0o600)
	link := filepath.Join(dir, ".env")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := SetEnv(context.Background(), link, []config.Assignment{{Key: "A", Value: "2"}}, testBackup(t)); err == nil {
		t.Fatal("expected an error")
	}
	if readFile(t, target) != "A=1\n" {
		t.Error("target changed")
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("link replaced")
	}
}

func TestSetEnvInPlaceKeepsModeInReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to any directory")
	}
	p := writeFile(t, "A=1\n", 0o604)
	dir := filepath.Dir(p)
	before, _ := os.Stat(p)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, err := SetEnv(context.Background(), p, []config.Assignment{{Key: "A", Value: "22"}}, testBackup(t)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "A=22\n" {
		t.Errorf("got %q", got)
	}
	after, _ := os.Stat(p)
	if after.Mode().Perm() != 0o604 || !os.SameFile(before, after) {
		t.Errorf("mode %v, same file %v", after.Mode().Perm(), os.SameFile(before, after))
	}
}

func TestSetEnvShrinksFile(t *testing.T) {
	p := writeFile(t, "A=a-long-demo-value\nB=2\n", 0o600)
	if _, err := SetEnv(context.Background(), p, []config.Assignment{{Key: "A", Value: "1"}}, testBackup(t)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "A=1\nB=2\n" {
		t.Errorf("got %q", got)
	}
}

func TestSetEnvRestoresOnWriteFailure(t *testing.T) {
	orig := "# keep\nA=1\nDEMO_SECRET=not-a-real-secret\n"
	p := writeFile(t, orig, 0o600)
	old := writeInPlace
	t.Cleanup(func() { writeInPlace = old })
	writeInPlace = func(f *os.File, data []byte) error {
		f.WriteAt(data[:3], 0)
		f.Truncate(3)
		return errors.New("disk full")
	}
	_, err := SetEnv(context.Background(), p, []config.Assignment{{Key: "A", Value: "a-much-longer-demo-value"}}, testBackup(t))
	if err == nil || err.Error() != "disk full" {
		t.Fatalf("got %v", err)
	}
	if readFile(t, p) != orig {
		t.Errorf("original not restored: %q", readFile(t, p))
	}
	noTempFiles(t, filepath.Dir(p))
}

func TestSetEnvCanceledContextLeavesFile(t *testing.T) {
	orig := "A=1\n"
	p := writeFile(t, orig, 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SetEnv(ctx, p, []config.Assignment{{Key: "A", Value: "2"}}, testBackup(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if readFile(t, p) != orig {
		t.Error("original changed")
	}
}

func TestSetEnvCreateFailureLeavesNoTempFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	old := writeTemp
	t.Cleanup(func() { writeTemp = old })
	writeTemp = func(f *os.File, data []byte) error { return errors.New("disk full") }
	if _, err := SetEnv(context.Background(), p, []config.Assignment{{Key: "A", Value: "2"}}, testBackup(t)); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(p); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("file exists: %v", err)
	}
	noTempFiles(t, filepath.Dir(p))
}

func TestSetEnvSavesBackup(t *testing.T) {
	orig := "DEMO_SECRET=not-a-real-secret\nA=1\n"
	p := writeFile(t, orig, 0o600)
	bk := testBackup(t)
	if _, err := SetEnv(context.Background(), p, []config.Assignment{{Key: "A", Value: "2"}}, bk); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(bk)
	if err != nil || fi.Mode().Perm() != 0o600 || readFile(t, bk) != orig {
		t.Errorf("backup: %v, %v", fi, err)
	}
	if di, _ := os.Stat(filepath.Dir(bk)); di.Mode().Perm() != 0o700 {
		t.Errorf("backup dir mode %v", di.Mode().Perm())
	}
}

func TestRunSetEnvBackupStaysOutOfLogs(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	os.WriteFile(env, []byte("DEMO_SECRET=not-a-real-secret\nA=1\n"), 0o600)
	root := filepath.Join(t.TempDir(), "env-backups")
	var log bytes.Buffer
	s := &stepRun{e: &Executor{EnvBackupRoot: root, Log: &log}, project: "demo-stack"}
	st := config.ResolvedStep{EnvFile: env, Assignments: []config.Assignment{{Key: "A", Value: "2"}}}
	if err := runSetEnv(context.Background(), s, st); err != nil {
		t.Fatal(err)
	}
	bk, _ := envBackupPath(root, "demo-stack", env)
	if readFile(t, bk) != "DEMO_SECRET=not-a-real-secret\nA=1\n" {
		t.Error("backup content")
	}
	if strings.Contains(log.String(), "not-a-real-secret") || strings.Contains(log.String(), bk) {
		t.Errorf("log leaks backup: %q", log.String())
	}
}

func TestFormatEnvValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.2.3", "1.2.3"},
		{"registry/app:v1+b", "registry/app:v1+b"},
		{"", ""},
		{"a b", "'a b'"},
		{"a#b", "'a#b'"},
		{`a$b"c`, `'a$b"c'`},
		{"it's", `"it's"`},
		{"it's #x", `"it's #x"`},
	}
	for _, c := range cases {
		got, err := FormatEnvValue(c.in)
		if err != nil || got != c.want {
			t.Errorf("%q: got %q, %v want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"a\nb", "a\x00b", `it's $x`, `it's \ x`, `it's "x"`} {
		if _, err := FormatEnvValue(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// Whatever FormatEnvValue writes is read back as the same value.
func TestFormatRoundTrip(t *testing.T) {
	for _, v := range []string{"1.0", "", "a b", "a#b", "$HOME", `q"q`, "it's", "it's #x", " lead", "trail ", "k=v", `back\slash`} {
		p := writeFile(t, "X=old\n", 0o600)
		if _, err := SetEnv(context.Background(), p, []config.Assignment{{Key: "X", Value: v}}, testBackup(t)); err != nil {
			t.Fatalf("%q: %v", v, err)
		}
		prev, err := SetEnv(context.Background(), p, []config.Assignment{{Key: "X", Value: "next"}}, testBackup(t))
		if err != nil || prev[0].Previous != v {
			t.Errorf("%q read back as %q (%v)", v, prev[0].Previous, err)
		}
	}
}

func TestReadEnvValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("# note\nA=1\nexport B='two words'\nA=3 # last wins\nC=\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"A": "3", "B": "two words", "C": ""} {
		if v, found, err := ReadEnvValue(path, key); err != nil || !found || v != want {
			t.Errorf("%s = %q, %v, %v; want %q", key, v, found, err, want)
		}
	}
	if _, found, err := ReadEnvValue(path, "D"); err != nil || found {
		t.Errorf("D: found = %v, err = %v", found, err)
	}
	if _, _, err := ReadEnvValue(filepath.Join(dir, "missing"), "A"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadEnvValue(link, "A"); err == nil {
		t.Error("a symbolic link was read")
	}
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, make([]byte, maxReadEnvBytes+1), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadEnvValue(big, "A"); err == nil {
		t.Error("an oversized file was read")
	}
}
