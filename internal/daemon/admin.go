package daemon

import (
	"bufio"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

// AdminGroup is the group whose members are the administrators of the host.
const AdminGroup = "sudo"

// NoUID stands in for the user id of a peer whose credentials could not be read.
const NoUID = ^uint32(0)

// Peer is the process at the other end of a connection, as the kernel reports it.
type Peer struct {
	UID uint32
}

// systemAdmin reports whether the user with this id is, per the group database, a member of AdminGroup. It
// reads /etc/group and /etc/passwd directly. The files are the whole truth here: no NSS, so a user known
// only to LDAP or SSSD is not an administrator.
func systemAdmin(uid uint32) (bool, error) {
	group, err := os.Open("/etc/group")
	if err != nil {
		return false, err
	}
	defer group.Close()
	passwd, err := os.Open("/etc/passwd")
	if err != nil {
		return false, err
	}
	defer passwd.Close()
	return adminInDatabase(group, passwd, uid)
}

// adminInDatabase reports whether uid is listed in AdminGroup's member list or has it as primary group. A
// missing AdminGroup, an unknown uid or a malformed line yields false or an error, never access.
func adminInDatabase(group, passwd io.Reader, uid uint32) (bool, error) {
	var gid string
	var members []string
	found := false
	sc := bufio.NewScanner(group)
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		if len(f) < 4 || f[0] != AdminGroup {
			continue
		}
		gid, members, found = f[2], strings.Split(f[3], ","), true
		break
	}
	if err := sc.Err(); err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	want := strconv.FormatUint(uint64(uid), 10)
	sc = bufio.NewScanner(passwd)
	for sc.Scan() {
		f := strings.Split(sc.Text(), ":")
		if len(f) < 4 || f[2] != want {
			continue
		}
		if f[3] == gid {
			return true, nil
		}
		return slices.Contains(members, f[0]), nil // the first entry of the uid decides, as getpwuid does
	}
	return false, sc.Err()
}

// isAdmin reports whether uid is root or, per inDatabase, an administrator. Any error means no.
func isAdmin(inDatabase func(uint32) (bool, error), uid uint32) bool {
	if uid == 0 {
		return true
	}
	ok, err := inDatabase(uid)
	return err == nil && ok
}
