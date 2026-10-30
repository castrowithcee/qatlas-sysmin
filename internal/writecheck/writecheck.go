// Package writecheck decides whether the account that runs sysmin may write the paths a project's deploy
// writes. It asks the kernel from inside the service, so a read-only mount or a restriction of the unit shows
// up as it would in a deploy.
package writecheck

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// File reports why path cannot be replaced by a write: set-env writes a temporary file next to it and renames
// it over the file, so the directory must be writable whether or not the file exists, and an existing file
// must be writable as well.
func File(path string) error {
	if err := access(filepath.Dir(path)); err != nil {
		return err
	}
	if err := access(path); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	return nil
}

// Dir reports why files below dir cannot be written. A missing dir counts as writable when its parent is,
// since a checkout can create it.
func Dir(dir string) error {
	err := access(dir)
	if errors.Is(err, unix.ENOENT) {
		return access(filepath.Dir(dir))
	}
	return err
}

func access(path string) error {
	// Flags 0 is the plain syscall, which answers EROFS for a read-only mount; any flag may fall back to a
	// mode-bit emulation that cannot see one.
	return unix.Faccessat(unix.AT_FDCWD, path, unix.W_OK, 0)
}

// Missing returns the paths the deploy of p writes that the service may not write: the env file with its
// directory, and the working copy of a git project.
func Missing(p config.Project) []string {
	var out []string
	if p.EnvFile != "" && File(p.EnvFile) != nil {
		out = append(out, p.EnvFile)
	}
	if p.Git != nil && p.Dir != "" && Dir(p.Dir) != nil {
		out = append(out, p.Dir)
	}
	return out
}
