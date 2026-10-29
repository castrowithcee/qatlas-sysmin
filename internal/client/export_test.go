package client

// SetSessionLacksGroup replaces the check of the login session's groups until the returned function runs.
func SetSessionLacksGroup(lacks bool) (restore func()) {
	old := sessionLacksGroup
	sessionLacksGroup = func() bool { return lacks }
	return func() { sessionLacksGroup = old }
}
