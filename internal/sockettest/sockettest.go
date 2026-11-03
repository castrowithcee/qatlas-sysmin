// Package sockettest prepares, for tests, a socket directory that the service channel accepts: it belongs to
// the test process and its group and is closed to others.
package sockettest

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
)

// Path returns the path of a socket in a fresh directory and the name of the test process's group, for the
// channel to admit besides root. The test is skipped where that group has no name.
func Path(t testing.TB) (path, group string) {
	t.Helper()
	g, err := user.LookupGroupId(strconv.Itoa(os.Getegid()))
	if err != nil {
		t.Skipf("the group of the test process has no name: %v", err)
	}
	// A short directory: a socket path holds about a hundred bytes.
	dir, err := os.MkdirTemp("", "qsysmin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chown(dir, -1, os.Getegid()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "s.sock"), g.Name
}
