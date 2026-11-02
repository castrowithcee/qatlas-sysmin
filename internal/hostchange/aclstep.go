package hostchange

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// Built-in steps that change the access ACL of one directory. Not every host has setfacl, so sysmin writes
// the attribute itself; the step is labelled with the setfacl call it equals.
const (
	// stepACLGrant is "setfacl -m g:sysmin:x <dir>": Argv is exactly that call.
	stepACLGrant = "setfacl"
	// stepACLRestore puts back the state recorded before the grant: Argv is the step, the directory, the
	// permission bits in octal and the previous ACL attribute in hex, or "none" when there was none.
	stepACLRestore = "acl-restore"

	aclNone = "none"
)

// systemGroupGID resolves the GID of a group of the local group file.
func systemGroupGID(name string) (int, error) {
	g, err := hostadmin.NewHost(false).Group(name)
	if err != nil {
		return 0, fmt.Errorf("the group %s is unknown: %w", name, err)
	}
	return g.GID, nil
}

func aclGrantCommand(dir string) Command {
	return Command{Argv: []string{stepACLGrant, "-m", "g:" + control.ServiceGroup + ":x", dir}, Builtin: true}
}

func aclRestoreCommand(dir string, mode uint32, acl []byte) Command {
	state := aclNone
	if acl != nil {
		state = hex.EncodeToString(acl)
	}
	return Command{Argv: []string{stepACLRestore, dir, fmt.Sprintf("%04o", mode), state}, Builtin: true}
}

func runACLStep(c Command) error {
	switch {
	case c.Argv[0] == stepACLGrant && len(c.Argv) == 4 && c.Argv[1] == "-m" && c.Argv[2] == "g:"+control.ServiceGroup+":x":
		gid, err := systemGroupGID(control.ServiceGroup)
		if err != nil {
			return err
		}
		return grantSearchAt(c.Argv[3], gid)
	case c.Argv[0] == stepACLRestore && len(c.Argv) == 4:
		mode, err := strconv.ParseUint(c.Argv[2], 8, 32)
		if err != nil || mode > 0o7777 {
			return fmt.Errorf("%s has an invalid mode", stepACLRestore)
		}
		var acl []byte
		if c.Argv[3] != aclNone {
			if acl, err = hex.DecodeString(c.Argv[3]); err != nil {
				return fmt.Errorf("%s has an invalid ACL", stepACLRestore)
			}
			if _, err := parseACL(acl); err != nil {
				return err
			}
		}
		return restoreACLAt(c.Argv[1], uint32(mode), acl)
	}
	return fmt.Errorf("built-in step %s has the wrong arguments", c.Argv[0])
}

// openACLDir opens dir, a clean absolute path outside the system paths, without following a link anywhere
// on its way.
func openACLDir(dir string) (int, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || IsSystemPath(dir) {
		return -1, fmt.Errorf("%s is not a directory this step changes", dir)
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, dir, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if errors.Is(err, unix.ENOSYS) {
		fd, err = unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		return -1, fmt.Errorf("open %s (a link is refused): %w", dir, err)
	}
	return fd, nil
}

// grantSearchAt adds the search entry for the group gid to the ACL of dir; owner and group stay.
func grantSearchAt(dir string, gid int) error {
	fd, err := openACLDir(dir)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	cur, err := readACLFd(fd)
	if err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	next, err := grantSearch(cur, st.Mode&0o7777, uint32(gid))
	if err != nil {
		return fmt.Errorf("%s: %w", dir, err)
	}
	if bytes.Equal(cur, next) {
		return nil
	}
	if err := unix.Fsetxattr(fd, aclXattr, next, 0); err != nil {
		return fmt.Errorf("set the ACL of %s: %w", dir, err)
	}
	return nil
}

// restoreACLAt sets the ACL of dir to acl, or removes it when acl is nil, and then the permission bits,
// which the mask of an ACL changes along with it.
func restoreACLAt(dir string, mode uint32, acl []byte) error {
	fd, err := openACLDir(dir)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if acl == nil {
		if err := unix.Fremovexattr(fd, aclXattr); err != nil && !errors.Is(err, unix.ENODATA) {
			return fmt.Errorf("remove the ACL of %s: %w", dir, err)
		}
	} else if err := unix.Fsetxattr(fd, aclXattr, acl, 0); err != nil {
		return fmt.Errorf("restore the ACL of %s: %w", dir, err)
	}
	if err := unix.Fchmod(fd, mode); err != nil {
		return fmt.Errorf("restore the permissions of %s: %w", dir, err)
	}
	return nil
}
