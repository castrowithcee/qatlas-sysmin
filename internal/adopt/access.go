package adopt

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-sysmin/internal/writecheck"
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

// Rights a missing-access finding names.
const (
	RightPass  = "pass"
	RightRead  = "read"
	RightWrite = "write"
)

// AccessNeed is one right the service lacks on a path: Right is RightPass for a directory to search,
// RightRead or RightWrite.
type AccessNeed struct {
	Path  string
	Right string
}

type accessNeed struct {
	path, right string
	dir         bool
}

func (in *inspector) need(path, right string, dir bool) {
	for _, n := range in.needs {
		if n.path == path && n.right == right {
			return
		}
	}
	in.needs = append(in.needs, accessNeed{path, right, dir})
}

// pass checks that every directory above path, and path itself when dir is set, may be passed. When one may
// not, it records that directory and returns false, since nothing below it can be checked.
func (in *inspector) pass(path string, dir bool) bool {
	d := path
	if !dir {
		d = filepath.Dir(path)
	}
	var ae *AccessError
	if err := CheckDir(d); errors.As(err, &ae) {
		in.need(ae.Path, RightPass, true)
		if !slices.Contains(in.hidden, ae.Path) {
			in.hidden = append(in.hidden, ae.Path)
		}
		return false
	}
	return true
}

// readFile records a missing right to read path. A missing file is no finding; Compose names it.
func (in *inspector) readFile(path string) {
	if in.pass(path, false) && !readable(path) {
		in.need(path, RightRead, false)
	}
}

// checkAccess records every right the stack needs before Compose runs: passing the directory, reading the
// Compose files and all env files (Compose loads the .env of the directory in any case) and, for a git working
// copy, using .git.
func (in *inspector) checkAccess(l *stackLabels) {
	dirOK := in.pass(in.dir, true)
	for _, f := range l.files {
		in.readFile(f)
	}
	for _, f := range l.envFiles {
		in.readFile(f)
	}
	in.readFile(filepath.Join(in.dir, ".env"))
	if !dirOK {
		return
	}
	git := filepath.Join(in.dir, ".git")
	if info, err := os.Lstat(git); err == nil && info.IsDir() && !passable(git) {
		in.gitDenied = true
		in.need(git, RightRead, true)
	}
}

// checkWrite records a missing right to write the env file, or the working copy when dir is set, by the rule
// of writecheck.
func (in *inspector) checkWrite(path string, dir bool) {
	if !in.pass(path, dir) {
		return
	}
	check := writecheck.File
	if dir {
		check = writecheck.Dir
	}
	if check(path) != nil {
		in.need(path, RightWrite, dir)
	}
}

// accessProblem turns the recorded needs into the one blocking CodeMissingAccess problem.
func (in *inspector) accessProblem() {
	if len(in.needs) == 0 {
		return
	}
	var parts []string
	var access []AccessNeed
	perms := map[string]string{}
	var order []string
	for _, n := range in.needs {
		access = append(access, AccessNeed{Path: n.path, Right: n.right})
		switch n.right {
		case RightPass:
			parts = append(parts, "pass "+n.path)
		case RightRead:
			parts = append(parts, "read "+n.path)
		case RightWrite:
			parts = append(parts, "write "+n.path)
		}
		if _, ok := perms[n.path]; !ok {
			order = append(order, n.path)
		}
		perms[n.path] = mergePerm(perms[n.path], n)
	}
	msg := "the service user " + serviceUser() + " may not " + strings.Join(parts, ", ")
	if len(in.hidden) > 0 {
		msg += "; below " + strings.Join(in.hidden, ", ") + " nothing could be checked, so more access may be missing"
	}

	acct := serviceUser()
	if strings.Contains(acct, " ") {
		acct = "<service user>"
	}
	groups := map[string][]string{}
	var gorder []string
	for _, p := range order {
		if _, ok := groups[perms[p]]; !ok {
			gorder = append(gorder, perms[p])
		}
		groups[perms[p]] = append(groups[perms[p]], shellQuote(p))
	}
	var cmds []string
	for _, perm := range gorder {
		cmds = append(cmds, "sudo setfacl -m u:"+acct+":"+perm+" "+strings.Join(groups[perm], " "))
	}
	hint := "Give " + serviceUser() + " the access, for example: " + strings.Join(cmds, "; ")
	in.prop.Problems = append(in.prop.Problems, Problem{Code: CodeMissingAccess, Message: msg, Hint: hint, Access: access})
}

// mergePerm adds the permission letters n needs to cur and returns them in the order r, w, x. A directory that
// is read or written must be searched as well, and what is written is read too.
func mergePerm(cur string, n accessNeed) string {
	set := cur
	add := func(c string) {
		if !strings.Contains(set, c) {
			set += c
		}
	}
	switch n.right {
	case RightPass:
		add("x")
	case RightRead:
		add("r")
		if n.dir {
			add("x")
		}
	case RightWrite:
		// An env file is read and rewritten, a working copy listed, written and searched.
		add("r")
		add("w")
		if n.dir {
			add("x")
		}
	}
	var out string
	for _, c := range "rwx" {
		if strings.ContainsRune(set, c) {
			out += string(c)
		}
	}
	return out
}

// shellQuote quotes s for a shell unless it holds only characters that need none.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+=:@%,") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
