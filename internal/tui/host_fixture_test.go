package tui

import "github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"

// fixtureHost is a host source with invented accounts, so no test reads the accounts of the machine it runs on.
type fixtureHost struct {
	users  []hostadmin.User
	groups []hostadmin.Group
	err    error
}

func (f *fixtureHost) Users() ([]hostadmin.User, error)   { return f.users, f.err }
func (f *fixtureHost) Groups() ([]hostadmin.Group, error) { return f.groups, f.err }
func (f *fixtureHost) Folder(string) (hostadmin.Folder, error) {
	return hostadmin.Folder{}, hostadmin.ErrNotFound
}

func newFixtureHost() *fixtureHost {
	unlocked := false
	return &fixtureHost{
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
