package daemon

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
)

// ServiceGroup is the group whose members may use the control socket, besides root.
const ServiceGroup = "sysmin"

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
