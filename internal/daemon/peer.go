package daemon

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// ServiceGroup is the group whose members may use the control socket, besides root.
const ServiceGroup = control.ServiceGroup

// AdminGroup is the group whose members are the administrators of the host.
const AdminGroup = "sudo"

// NoUID stands in for the user id of a peer whose credentials could not be read.
const NoUID = ^uint32(0)

// Peer is the process at the other end of a connection, as the kernel reports it.
type Peer struct {
	PID int32
	UID uint32
	GID uint32
}

// Resolver answers the group questions of the peer check, so a test can stand in for the system's groups.
type Resolver interface {
	// GroupID returns the id of the named group.
	GroupID(name string) (uint32, error)
	// PeerGroups returns the supplementary groups of the peer's process.
	PeerGroups(p Peer) ([]uint32, error)
	// IsAdmin reports whether the user with this id is, per the group database, a member of AdminGroup.
	IsAdmin(uid uint32) (bool, error)
}

// SystemResolver reads the group database and /proc.
type SystemResolver struct{}

// GroupID implements Resolver.
func (SystemResolver) GroupID(name string) (uint32, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseUint(g.Gid, 10, 32)
	return uint32(id), err
}

// PeerGroups implements Resolver with the Groups line of /proc/<pid>/status.
func (SystemResolver) PeerGroups(p Peer) ([]uint32, error) {
	f, err := os.Open("/proc/" + strconv.Itoa(int(p.PID)) + "/status")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseGroups(f)
}

// IsAdmin implements Resolver by reading /etc/group and /etc/passwd directly. The files are the whole
// truth here: no NSS, so a user known only to LDAP or SSSD is not an administrator.
func (SystemResolver) IsAdmin(uid uint32) (bool, error) {
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

func parseGroups(r io.Reader) ([]uint32, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), "Groups:")
		if !ok {
			continue
		}
		var out []uint32
		for _, field := range strings.Fields(rest) {
			id, err := strconv.ParseUint(field, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("malformed group list")
			}
			out = append(out, uint32(id))
		}
		return out, nil
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no group list")
}

// peerCred asks the kernel who connected to conn.
func peerCred(conn net.Conn) (Peer, error) {
	unix, ok := conn.(*net.UnixConn)
	if !ok {
		return Peer{}, fmt.Errorf("not a Unix socket connection")
	}
	raw, err := unix.SyscallConn()
	if err != nil {
		return Peer{}, err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return Peer{}, err
	}
	if credErr != nil {
		return Peer{}, credErr
	}
	return Peer{PID: cred.Pid, UID: cred.Uid, GID: cred.Gid}, nil
}

// isAdmin reports whether p is root or a member of AdminGroup in the group database. Any error means no.
func isAdmin(res Resolver, p Peer) bool {
	if p.UID == 0 {
		return true
	}
	ok, err := res.IsAdmin(p.UID)
	return err == nil && ok
}

// admit returns "" for a peer that may use the socket, CodeRefused for one outside ServiceGroup and
// CodeNotAdmin for one inside it that is no administrator.
func admit(res Resolver, p Peer) string {
	switch {
	case !allowed(res, p):
		return control.CodeRefused
	case !isAdmin(res, p):
		return control.CodeNotAdmin
	}
	return ""
}

// allowed reports whether p may use the socket: root, or a member of ServiceGroup by its primary or one of
// its supplementary groups. Whatever cannot be established refuses; a missing group lets root alone in.
func allowed(res Resolver, p Peer) bool {
	if p.UID == 0 {
		return true
	}
	gid, err := res.GroupID(ServiceGroup)
	if err != nil {
		return false
	}
	if p.GID == gid {
		return true
	}
	groups, err := res.PeerGroups(p)
	if err != nil {
		return false
	}
	for _, g := range groups {
		if g == gid {
			return true
		}
	}
	return false
}
