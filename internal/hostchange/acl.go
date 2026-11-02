package hostchange

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"golang.org/x/sys/unix"
)

// The access ACL of a file is the extended attribute below in the kernel's own format: a version word, then
// one entry of tag, permission bits and id per rule, all little endian. Without the attribute a file has
// only its mode bits.
const (
	aclXattr    = "system.posix_acl_access"
	aclVersion  = 2
	aclNoID     = 0xFFFFFFFF
	aclMaxBytes = 4 + 8*64
)

const (
	aclUserObj  uint16 = 0x01
	aclUser     uint16 = 0x02
	aclGroupObj uint16 = 0x04
	aclGroup    uint16 = 0x08
	aclMask     uint16 = 0x10
	aclOther    uint16 = 0x20

	aclPermX uint16 = 1
)

type aclEntry struct {
	Tag  uint16
	Perm uint16
	ID   uint32
}

// errACLUnsupported marks a file system without POSIX ACLs.
var errACLUnsupported = errors.New("the file system does not support ACLs")

func parseACL(b []byte) ([]aclEntry, error) {
	if len(b) < 4 || len(b) > aclMaxBytes || (len(b)-4)%8 != 0 || binary.LittleEndian.Uint32(b) != aclVersion {
		return nil, errors.New("the ACL is not in a known format")
	}
	var es []aclEntry
	count := map[uint16]int{}
	named := false
	for off := 4; off < len(b); off += 8 {
		e := aclEntry{binary.LittleEndian.Uint16(b[off:]), binary.LittleEndian.Uint16(b[off+2:]), binary.LittleEndian.Uint32(b[off+4:])}
		switch e.Tag {
		case aclUserObj, aclGroupObj, aclMask, aclOther:
		case aclUser, aclGroup:
			named = true
		default:
			return nil, errors.New("the ACL holds an unknown entry")
		}
		if e.Perm > 7 {
			return nil, errors.New("the ACL holds an unknown permission")
		}
		count[e.Tag]++
		es = append(es, e)
	}
	if count[aclUserObj] != 1 || count[aclGroupObj] != 1 || count[aclOther] != 1 || count[aclMask] > 1 || (named && count[aclMask] != 1) {
		return nil, errors.New("the ACL is incomplete")
	}
	return es, nil
}

func encodeACL(es []aclEntry) []byte {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Tag != es[j].Tag {
			return es[i].Tag < es[j].Tag
		}
		return es[i].ID < es[j].ID
	})
	b := binary.LittleEndian.AppendUint32(nil, aclVersion)
	for _, e := range es {
		b = binary.LittleEndian.AppendUint16(b, e.Tag)
		b = binary.LittleEndian.AppendUint16(b, e.Perm)
		b = binary.LittleEndian.AppendUint32(b, e.ID)
	}
	return b
}

// aclFromMode is the ACL a file without the attribute has implicitly.
func aclFromMode(mode uint32) []aclEntry {
	return []aclEntry{
		{aclUserObj, uint16(mode >> 6 & 7), aclNoID},
		{aclGroupObj, uint16(mode >> 3 & 7), aclNoID},
		{aclOther, uint16(mode & 7), aclNoID},
	}
}

// grantSearch returns the ACL that lets the group gid search a directory, added to cur (nil when the
// directory has no ACL attribute). The mask is raised by the search bit only when no other entry would gain
// from it; otherwise it refuses, since raising the mask would widen that entry.
func grantSearch(cur []byte, mode uint32, gid uint32) ([]byte, error) {
	es := aclFromMode(mode)
	if cur != nil {
		var err error
		if es, err = parseACL(cur); err != nil {
			return nil, err
		}
	}
	mask := -1
	for i, e := range es {
		if e.Tag == aclMask {
			mask = i
		}
	}
	if mask < 0 {
		es = append(es, aclEntry{aclMask, 0, aclNoID})
		mask = len(es) - 1
		for _, e := range es {
			if e.Tag == aclGroupObj {
				es[mask].Perm = e.Perm
			}
		}
	}
	own := -1
	for i, e := range es {
		if e.Tag == aclGroup && e.ID == gid {
			own = i
		}
	}
	if own < 0 {
		es = append(es, aclEntry{aclGroup, aclPermX, gid})
	} else {
		es[own].Perm |= aclPermX
	}
	if es[mask].Perm&aclPermX == 0 {
		for _, e := range es {
			masked := e.Tag == aclUser || e.Tag == aclGroupObj || (e.Tag == aclGroup && e.ID != gid)
			if masked && e.Perm&aclPermX != 0 {
				return nil, errors.New("another entry holds the search permission, which the mask withholds; raising the mask would widen it")
			}
		}
		es[mask].Perm |= aclPermX
	}
	return encodeACL(es), nil
}

// searchAllowed applies the POSIX access check for the search permission of a user with the given groups.
func searchAllowed(uid int, gids []int, ownerUID, ownerGID int, mode uint32, acl []byte) bool {
	has := func(gid int) bool {
		for _, g := range gids {
			if g == gid {
				return true
			}
		}
		return false
	}
	var es []aclEntry
	if acl != nil {
		var err error
		if es, err = parseACL(acl); err != nil {
			return false
		}
	} else {
		es = aclFromMode(mode)
	}
	mask := aclPermX
	for _, e := range es {
		if e.Tag == aclMask {
			mask = e.Perm
		}
	}
	if uid == ownerUID {
		for _, e := range es {
			if e.Tag == aclUserObj {
				return e.Perm&aclPermX != 0
			}
		}
	}
	for _, e := range es {
		if e.Tag == aclUser && int(e.ID) == uid {
			return e.Perm&mask&aclPermX != 0
		}
	}
	matched, granted := false, false
	for _, e := range es {
		if (e.Tag == aclGroupObj && has(ownerGID)) || (e.Tag == aclGroup && has(int(e.ID))) {
			matched = true
			granted = granted || e.Perm&mask&aclPermX != 0
		}
	}
	if matched {
		return granted
	}
	for _, e := range es {
		if e.Tag == aclOther {
			return e.Perm&aclPermX != 0
		}
	}
	return false
}

// readACL returns the access ACL attribute of path, or nil when the file has none.
func readACL(path string) ([]byte, error) {
	buf := make([]byte, aclMaxBytes)
	n, err := unix.Getxattr(path, aclXattr, buf)
	return aclRead(n, err, buf)
}

func readACLFd(fd int) ([]byte, error) {
	buf := make([]byte, aclMaxBytes)
	n, err := unix.Fgetxattr(fd, aclXattr, buf)
	return aclRead(n, err, buf)
}

func aclRead(n int, err error, buf []byte) ([]byte, error) {
	switch {
	case errors.Is(err, unix.ENODATA):
		return nil, nil
	case errors.Is(err, unix.ENOTSUP):
		return nil, errACLUnsupported
	case err != nil:
		return nil, fmt.Errorf("read the ACL: %w", err)
	}
	return buf[:n:n], nil
}
