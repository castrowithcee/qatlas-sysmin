package adopt

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// ErrNotDirectory reports a path that does not exist or is not a directory.
var ErrNotDirectory = errors.New("not an existing directory")

// AccessError reports the first part of a path that the account running sysmin may not pass.
type AccessError struct{ Path string }

func (e *AccessError) Error() string { return "no access to " + e.Path }

// CheckDir checks that dir, an absolute clean path, is a directory the service can pass: every element from
// the root to dir itself must allow the search permission. It asks the kernel without opening anything and
// returns nil, an *AccessError naming the first element that is denied (always a prefix of dir), or an error
// wrapping ErrNotDirectory.
func CheckDir(dir string) error {
	prefix := string(filepath.Separator)
	elems := strings.Split(strings.TrimPrefix(dir, prefix), prefix)
	if dir == prefix {
		elems = nil
	}
	for i := -1; i < len(elems); i++ {
		if i >= 0 {
			prefix = filepath.Join(prefix, elems[i])
		}
		err := unix.Faccessat(unix.AT_FDCWD, prefix, unix.X_OK, 0)
		switch {
		case err == nil:
		case errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM):
			// A file without the execute bit is not a closed directory.
			var st unix.Stat_t
			if unix.Stat(prefix, &st) == nil && st.Mode&unix.S_IFMT != unix.S_IFDIR {
				return ErrNotDirectory
			}
			return &AccessError{Path: prefix}
		default:
			return ErrNotDirectory
		}
	}
	var st unix.Stat_t
	if err := unix.Stat(dir, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return ErrNotDirectory
	}
	return nil
}

// readable reports whether the account may read path, asked without opening it.
func readable(path string) bool {
	return !errors.Is(unix.Faccessat(unix.AT_FDCWD, path, unix.R_OK, 0), unix.EACCES)
}

// passable reports whether the account may read and search the directory path.
func passable(path string) bool {
	return !errors.Is(unix.Faccessat(unix.AT_FDCWD, path, unix.R_OK|unix.X_OK, 0), unix.EACCES)
}

// serviceUser returns the name of the account that runs this process, or "the service user".
func serviceUser() string {
	if u, err := user.LookupId(strconv.Itoa(os.Getuid())); err == nil && u.Username != "" {
		return u.Username
	}
	return "the service user"
}

// grantHint names the next step for a path the account may not use: perm is "x" for a directory to pass, "r"
// for a file to read.
func grantHint(path, perm string) string {
	acct := serviceUser()
	if strings.Contains(acct, " ") {
		acct = "<service user>"
	}
	return "Give " + serviceUser() + " the permission, for example: sudo setfacl -m u:" + acct + ":" + perm + " " + shellQuote(path)
}

// shellQuote quotes s for a shell unless it holds only characters that need none.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+=:@%,") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
