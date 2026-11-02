package tui

import (
	"fmt"
	"io/fs"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// fixtureHost is a host source with invented accounts, so no test reads the accounts of the machine it runs on.
type fixtureHost struct {
	users  []hostadmin.User
	groups []hostadmin.Group
	// folders are the paths Folder knows; any other is not found, so no test stats the real file system.
	folders map[string]hostadmin.Folder
	err     error
	// homeRisks are the risks of deleting a user's home, by user name.
	homeRisks map[string][]hostchange.HomeRisk
}

func (f *fixtureHost) HomeRisks(name, _ string) ([]hostchange.HomeRisk, error) {
	return f.homeRisks[name], nil
}

func (f *fixtureHost) Users() ([]hostadmin.User, error)   { return f.users, f.err }
func (f *fixtureHost) Groups() ([]hostadmin.Group, error) { return f.groups, f.err }
func (f *fixtureHost) Folder(path string) (hostadmin.Folder, error) {
	if folder, ok := f.folders[path]; ok {
		return folder, nil
	}
	if path == "/srv/locked" {
		return hostadmin.Folder{}, fmt.Errorf("stat %s: %w", path, fs.ErrPermission)
	}
	return hostadmin.Folder{}, fmt.Errorf("path %q: %w", path, hostadmin.ErrNotFound)
}

func newFixtureHost() *fixtureHost {
	unlocked, yes := false, true
	return &fixtureHost{
		folders: map[string]hostadmin.Folder{
			"/srv/demo-stack": {Path: "/srv/demo-stack", Resolved: "/srv/demo-stack", Kind: "directory", Owner: "root", Group: "sysmin",
				Mode: "2750", OwnerAcc: hostadmin.AccessReadWrite, GroupAcc: hostadmin.AccessRead, OthersAcc: hostadmin.AccessNothing,
				ACL: &yes, Setgid: true},
			"/etc": {Path: "/etc", Resolved: "/etc", Kind: "directory", Owner: "root", Group: "root", Mode: "0755",
				OwnerAcc: hostadmin.AccessReadWrite, GroupAcc: hostadmin.AccessRead, OthersAcc: hostadmin.AccessRead, ACL: &unlocked, Protected: true},
			"/srv/demo-stack/app.conf": {Path: "/srv/demo-stack/app.conf", Resolved: "/srv/demo-stack/app.conf", Kind: "file", Owner: "demo-user",
				Group: "demo-team", Mode: "0640", OwnerAcc: hostadmin.AccessReadWrite, GroupAcc: hostadmin.AccessRead, OthersAcc: hostadmin.AccessNothing},
		},
		users: []hostadmin.User{
			{Name: "demo-admin", UID: 1000, PrimaryGroup: "demo-admin", Groups: []string{"demo-admin", "sudo", "docker", "sysmin"},
				Home: "/home/demo-admin", Admin: true, Docker: true, Enabled: true},
			{Name: "demo-user", UID: 1001, PrimaryGroup: "demo-user", Groups: []string{"demo-user", "sudo", "demo-team"},
				Home: "/home/demo-user", Admin: true, Locked: &unlocked, SSHKeys: []hostadmin.SSHKey{
					{Type: "ssh-ed25519", Comment: "demo-user@laptop", Fingerprint: "SHA256:DEMOFINGERPRINT0000000000000000000000000"}}},
			{Name: "svc-demo", UID: 998, PrimaryGroup: "svc-demo", Groups: []string{"svc-demo"}, Home: "/var/lib/svc-demo", System: true},
		},
		groups: []hostadmin.Group{
			{Name: "sudo", GID: 27, Members: []string{"demo-admin", "demo-user"}, System: true},
			{Name: "docker", GID: 998, Members: []string{"demo-admin"}, System: true},
			{Name: "sysmin", GID: 997, Members: []string{"demo-admin"}, System: true},
			{Name: "demo-team", GID: 1002, Members: []string{"demo-user"}},
		},
	}
}
