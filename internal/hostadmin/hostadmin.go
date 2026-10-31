// Package hostadmin is a read-only model of the local users, groups and folder permissions of the host. It
// reads the files of the local databases directly and never changes anything on the host.
package hostadmin

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Group names that carry a meaning for sysmin.
const (
	GroupAdmin    = "sudo"
	GroupDocker   = "docker"
	GroupSysmin   = "sysmin"
	systemIDBelow = 1000
)

// ErrNotFound marks a user, group or path that does not exist.
var ErrNotFound = errors.New("not found")

// SSHKey describes one entry of an authorized_keys file. It never carries the key itself.
type SSHKey struct {
	Type        string `json:"type"`
	Comment     string `json:"comment"`
	Fingerprint string `json:"fingerprint"`
}

// User is one local account. Locked, Expired and SSHKeys come from files only root can read; a nil value
// means unknown, which is different from false or an empty list.
type User struct {
	Name         string   `json:"name"`
	UID          int      `json:"uid"`
	PrimaryGroup string   `json:"primary_group"`
	Groups       []string `json:"groups"`
	Home         string   `json:"home"`
	System       bool     `json:"system"`
	Admin        bool     `json:"admin"`
	Docker       bool     `json:"docker"`
	Enabled      bool     `json:"enabled"`
	Locked       *bool    `json:"locked"`
	Expired      *bool    `json:"expired"`
	SSHKeys      []SSHKey `json:"ssh_keys"`
}

// Group is one local group. Members include the users whose primary group it is.
type Group struct {
	Name    string   `json:"name"`
	GID     int      `json:"gid"`
	Members []string `json:"members"`
	System  bool     `json:"system"`
}

// Host reads the databases of one host. The zero value is not usable; start from NewHost.
type Host struct {
	// PasswdPath, GroupPath and ShadowPath are the paths of the account databases.
	PasswdPath, GroupPath, ShadowPath string
	// Privileged says the process may read the shadow file and the users' authorized_keys files. Without
	// it those fields stay unknown.
	Privileged bool
	// Now supplies the day an account expiry is compared with.
	Now func() time.Time
	// Xattrs lists the extended attribute names of a path; nil means the real file system.
	Xattrs func(path string) ([]string, error)
	// ProtectedRoots are the directories that are protected together with everything below them.
	ProtectedRoots []string
}

// NewHost returns a Host for the real databases.
func NewHost(privileged bool) *Host {
	return &Host{
		PasswdPath: "/etc/passwd", GroupPath: "/etc/group", ShadowPath: "/etc/shadow",
		Privileged:     privileged,
		Now:            time.Now,
		ProtectedRoots: []string{"/", "/etc", "/usr", "/boot", "/proc", "/sys"},
	}
}

type passwdEntry struct {
	name     string
	uid, gid int
	home     string
}

type groupEntry struct {
	name    string
	gid     int
	members []string
}

func readLines(path string) ([][]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows [][]string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rows = append(rows, strings.Split(line, ":"))
	}
	return rows, nil
}

func (h *Host) passwd() ([]passwdEntry, error) {
	rows, err := readLines(h.PasswdPath)
	if err != nil {
		return nil, err
	}
	var out []passwdEntry
	for _, f := range rows {
		if len(f) < 7 {
			continue
		}
		uid, err1 := strconv.Atoi(f[2])
		gid, err2 := strconv.Atoi(f[3])
		if err1 != nil || err2 != nil || f[0] == "" {
			continue
		}
		out = append(out, passwdEntry{name: f[0], uid: uid, gid: gid, home: f[5]})
	}
	return out, nil
}

func (h *Host) groups() ([]groupEntry, error) {
	rows, err := readLines(h.GroupPath)
	if err != nil {
		return nil, err
	}
	var out []groupEntry
	for _, f := range rows {
		if len(f) < 4 {
			continue
		}
		gid, err := strconv.Atoi(f[2])
		if err != nil || f[0] == "" {
			continue
		}
		var members []string
		for _, m := range strings.Split(f[3], ",") {
			if m = strings.TrimSpace(m); m != "" {
				members = append(members, m)
			}
		}
		out = append(out, groupEntry{name: f[0], gid: gid, members: members})
	}
	return out, nil
}

// db is a consistent snapshot of passwd and group.
type db struct {
	users  []passwdEntry
	groups []groupEntry
}

func (h *Host) load() (*db, error) {
	users, err := h.passwd()
	if err != nil {
		return nil, err
	}
	groups, err := h.groups()
	if err != nil {
		return nil, err
	}
	return &db{users: users, groups: groups}, nil
}

func (d *db) groupName(gid int) string {
	for _, g := range d.groups {
		if g.gid == gid {
			return g.name
		}
	}
	return strconv.Itoa(gid)
}

// memberOf reports whether the user is listed in the group or has it as primary group.
func (d *db) memberOf(u passwdEntry, group string) bool {
	for _, g := range d.groups {
		if g.name != group {
			continue
		}
		if g.gid == u.gid {
			return true
		}
		for _, m := range g.members {
			if m == u.name {
				return true
			}
		}
	}
	return false
}

func (d *db) userGroups(u passwdEntry) []string {
	var names []string
	for _, g := range d.groups {
		if g.gid == u.gid {
			continue
		}
		for _, m := range g.members {
			if m == u.name {
				names = append(names, g.name)
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

func (h *Host) buildUser(d *db, p passwdEntry, shadow map[string]shadowEntry) User {
	u := User{
		Name: p.name, UID: p.uid, PrimaryGroup: d.groupName(p.gid), Groups: d.userGroups(p), Home: p.home,
		System:  p.uid < systemIDBelow,
		Admin:   d.memberOf(p, GroupAdmin),
		Docker:  d.memberOf(p, GroupDocker),
		Enabled: d.memberOf(p, GroupSysmin),
	}
	if u.Groups == nil {
		u.Groups = []string{}
	}
	if !h.Privileged {
		return u
	}
	if s, ok := shadow[p.name]; ok {
		locked := s.locked
		u.Locked = &locked
		u.Expired = s.expired(h.now())
	}
	u.SSHKeys = readAuthorizedKeys(p.home)
	return u
}

func (h *Host) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Users returns all local users in the order of the passwd file.
func (h *Host) Users() ([]User, error) {
	d, err := h.load()
	if err != nil {
		return nil, err
	}
	shadow, err := h.readShadow()
	if err != nil {
		return nil, err
	}
	users := make([]User, 0, len(d.users))
	for _, p := range d.users {
		users = append(users, h.buildUser(d, p, shadow))
	}
	return users, nil
}

// User returns one local user by name.
func (h *Host) User(name string) (User, error) {
	d, err := h.load()
	if err != nil {
		return User{}, err
	}
	shadow, err := h.readShadow()
	if err != nil {
		return User{}, err
	}
	for _, p := range d.users {
		if p.name == name {
			return h.buildUser(d, p, shadow), nil
		}
	}
	return User{}, fmt.Errorf("user %q: %w", name, ErrNotFound)
}

func (d *db) buildGroup(g groupEntry) Group {
	seen := map[string]bool{}
	members := []string{}
	for _, m := range g.members {
		if !seen[m] {
			seen[m] = true
			members = append(members, m)
		}
	}
	for _, u := range d.users {
		if u.gid == g.gid && !seen[u.name] {
			seen[u.name] = true
			members = append(members, u.name)
		}
	}
	sort.Strings(members)
	return Group{Name: g.name, GID: g.gid, Members: members, System: g.gid < systemIDBelow}
}

// Groups returns all local groups in the order of the group file.
func (h *Host) Groups() ([]Group, error) {
	d, err := h.load()
	if err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(d.groups))
	for _, g := range d.groups {
		out = append(out, d.buildGroup(g))
	}
	return out, nil
}

// Group returns one local group by name.
func (h *Host) Group(name string) (Group, error) {
	d, err := h.load()
	if err != nil {
		return Group{}, err
	}
	for _, g := range d.groups {
		if g.name == name {
			return d.buildGroup(g), nil
		}
	}
	return Group{}, fmt.Errorf("group %q: %w", name, ErrNotFound)
}

// IsAdmin reports whether the user with this UID is a member of the admin group, by the group database.
// An unknown UID is no administrator.
func (h *Host) IsAdmin(uid int) (bool, error) { return h.IsMember(uid, GroupAdmin) }

// IsMember reports whether the user with this UID is a member of the named group, by the group database:
// listed in its members or having it as primary group. An unknown UID or group is no member.
func (h *Host) IsMember(uid int, group string) (bool, error) {
	d, err := h.load()
	if err != nil {
		return false, err
	}
	for _, p := range d.users {
		if p.uid == uid && d.memberOf(p, group) {
			return true, nil
		}
	}
	return false, nil
}
