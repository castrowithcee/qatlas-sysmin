package hostadmin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Access is what one class of users may do with a folder or file.
type Access string

// The three levels a class can have.
const (
	AccessNothing   Access = "nothing"
	AccessRead      Access = "read"
	AccessReadWrite Access = "read and write"
)

// Folder describes the permissions of a directory or file.
type Folder struct {
	Path      string `json:"path"`
	Resolved  string `json:"resolved"`
	Kind      string `json:"kind"`
	Owner     string `json:"owner"`
	Group     string `json:"group"`
	Mode      string `json:"mode"`
	OwnerAcc  Access `json:"owner_access"`
	GroupAcc  Access `json:"group_access"`
	OthersAcc Access `json:"others_access"`
	// ACL is nil when the file system could not say.
	ACL       *bool `json:"acl"`
	Setuid    bool  `json:"setuid"`
	Setgid    bool  `json:"setgid"`
	Sticky    bool  `json:"sticky"`
	Protected bool  `json:"protected"`
}

// level reduces three permission bits to a level. A directory needs execute (traversal) for either right.
// Write without read counts as nothing, so a class is never shown with more than it can fully use.
func level(bits uint32, dir bool) Access {
	r, w, x := bits&4 != 0, bits&2 != 0, bits&1 != 0
	if dir && !x {
		return AccessNothing
	}
	switch {
	case r && w:
		return AccessReadWrite
	case r:
		return AccessRead
	}
	return AccessNothing
}

// Folder reads the permissions of path. A link is followed for the permissions.
func (h *Host) Folder(path string) (Folder, error) {
	if path == "" {
		return Folder{}, errors.New("empty path")
	}
	raw := path
	if !filepath.IsAbs(raw) {
		wd, err := os.Getwd()
		if err != nil {
			return Folder{}, err
		}
		raw = wd + string(filepath.Separator) + raw
	}
	clean := filepath.Clean(raw)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Folder{}, fmt.Errorf("path %q: %w", path, ErrNotFound)
		}
		return Folder{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Folder{}, fmt.Errorf("path %q: %w", path, ErrNotFound)
		}
		return Folder{}, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Folder{}, errors.New("the file system gives no owner information")
	}
	d, err := h.load()
	if err != nil {
		return Folder{}, err
	}
	perm := uint32(info.Mode().Perm())
	dir := info.IsDir()
	f := Folder{
		Path: clean, Resolved: resolved, Kind: kindOf(info.Mode()),
		Owner: ownerName(d, int(st.Uid)), Group: d.groupName(int(st.Gid)),
		Mode:      fmt.Sprintf("%04o", perm),
		OwnerAcc:  level(perm>>6&7, dir),
		GroupAcc:  level(perm>>3&7, dir),
		OthersAcc: level(perm&7, dir),
		Setuid:    info.Mode()&fs.ModeSetuid != 0,
		Setgid:    info.Mode()&fs.ModeSetgid != 0,
		Sticky:    info.Mode()&fs.ModeSticky != 0,
	}
	f.ACL = h.hasACL(resolved)
	// The lexical path, the resolved given path and the path resolved without cleaning ".." first are all
	// checked, so neither ".." nor a link into a protected directory hides it.
	f.Protected = h.protected(clean) || h.protected(resolved)
	if unclean, err := filepath.EvalSymlinks(raw); err == nil && h.protected(unclean) {
		f.Protected = true
	}
	return f, nil
}

func kindOf(m fs.FileMode) string {
	switch {
	case m.IsDir():
		return "directory"
	case m.IsRegular():
		return "file"
	}
	return "other"
}

func ownerName(d *db, uid int) string {
	for _, u := range d.users {
		if u.uid == uid {
			return u.name
		}
	}
	return strconv.Itoa(uid)
}

func (h *Host) hasACL(path string) *bool {
	list := h.Xattrs
	if list == nil {
		list = listXattrs
	}
	names, err := list(path)
	if err != nil {
		if errors.Is(err, unix.ENOTSUP) {
			no := false
			return &no
		}
		return nil
	}
	has := false
	for _, n := range names {
		if n == "system.posix_acl_access" || n == "system.posix_acl_default" {
			has = true
		}
	}
	return &has
}

func listXattrs(path string) ([]string, error) {
	size, err := unix.Listxattr(path, nil)
	if err != nil || size == 0 {
		return nil, err
	}
	buf := make([]byte, size)
	size, err = unix.Listxattr(path, buf)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, n := range strings.Split(string(buf[:size]), "\x00") {
		if n != "" {
			names = append(names, n)
		}
	}
	return names, nil
}

// protected reports whether path is one of the protected roots or below one. The root directory protects
// only itself. The roots are compared both as
// written and with their links resolved.
func (h *Host) protected(path string) bool {
	for _, root := range h.ProtectedRoots {
		if within(root, path) {
			return true
		}
		if real, err := filepath.EvalSymlinks(root); err == nil && within(real, path) {
			return true
		}
	}
	return false
}

func within(root, path string) bool {
	if root == "/" {
		// Only the root directory itself; everything else is below it.
		return path == "/"
	}
	return path == root || strings.HasPrefix(path, root+"/")
}
