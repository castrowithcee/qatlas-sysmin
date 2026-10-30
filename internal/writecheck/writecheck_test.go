package writecheck

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// lockedDir returns a directory that cannot be written; its permissions are restored on cleanup.
func lockedDir(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root can write to any directory")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	return dir
}

func TestFileInUnwritableDirectory(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	if err := os.WriteFile(env, []byte("A=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := File(env); err != nil {
		t.Fatalf("writable file and directory: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write to any directory")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := File(env); err == nil {
		t.Error("a writable file in an unwritable directory counts as writable")
	}
}

func TestMissingFileInWritableDirectory(t *testing.T) {
	if err := File(filepath.Join(t.TempDir(), ".env")); err != nil {
		t.Errorf("missing file in a writable directory: %v", err)
	}
}

func TestReadOnlyFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to read-only files")
	}
	env := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(env, nil, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := File(env); err == nil {
		t.Error("a read-only file counts as writable")
	}
}

func TestMissingDirNeedsWritableParent(t *testing.T) {
	if err := Dir(filepath.Join(t.TempDir(), "new")); err != nil {
		t.Errorf("missing dir below a writable parent: %v", err)
	}
	if err := Dir(filepath.Join(lockedDir(t), "new")); err == nil {
		t.Error("missing dir below an unwritable parent counts as writable")
	}
}

func TestMissingPaths(t *testing.T) {
	locked := lockedDir(t)
	open := t.TempDir()
	cases := []struct {
		name string
		p    config.Project
		want []string
	}{
		{"writable", config.Project{Dir: open, EnvFile: filepath.Join(open, ".env")}, nil},
		{"env file", config.Project{Dir: locked, EnvFile: filepath.Join(locked, ".env")}, []string{filepath.Join(locked, ".env")}},
		{"git dir", config.Project{Dir: locked, Git: &config.GitSource{}}, []string{locked}},
		{"images do not write dir", config.Project{Dir: locked}, nil},
	}
	for _, c := range cases {
		if got := Missing(c.p); !slices.Equal(got, c.want) {
			t.Errorf("%s: Missing = %v, want %v", c.name, got, c.want)
		}
	}
}
