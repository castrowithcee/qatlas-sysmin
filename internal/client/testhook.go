package client

// SetStandingForTests replaces the caller's standing in the group database until the returned function runs.
// It exists so tests of the command line can name an administrator or not without touching real accounts.
func SetStandingForTests(admin, enabled bool) (restore func()) {
	old := standing
	standing = func() (bool, bool) { return admin, enabled }
	return func() { standing = old }
}
