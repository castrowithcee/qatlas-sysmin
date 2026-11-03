package client

import "time"

// SetSessionLacksGroup replaces the check of the login session's groups until the returned function runs.
func SetSessionLacksGroup(lacks bool) (restore func()) {
	old := sessionLacksGroup
	sessionLacksGroup = func() bool { return lacks }
	return func() { sessionLacksGroup = old }
}

// SetFirstAnswerTimeout shortens the first-answer deadline until the returned function runs.
func SetFirstAnswerTimeout(d time.Duration) (restore func()) {
	old := firstAnswerTimeout
	firstAnswerTimeout = d
	return func() { firstAnswerTimeout = old }
}
