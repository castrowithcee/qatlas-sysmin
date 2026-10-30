package client

import (
	"errors"
	"net"
	"time"
)

// SetSessionLacksGroup replaces the check of the login session's groups until the returned function runs.
func SetSessionLacksGroup(lacks bool) (restore func()) {
	old := sessionLacksGroup
	sessionLacksGroup = func() bool { return lacks }
	return func() { sessionLacksGroup = old }
}

// SetPeer replaces who the kernel reports at the socket and who the service user is until the returned
// function runs.
func SetPeer(pid int32, uid uint32, serviceUser uint32, serviceExists bool) (restore func()) {
	oldPeer, oldSvc := peerOf, serviceUID
	peerOf = func(net.Conn) (peerCred, error) { return peerCred{PID: pid, UID: uid}, nil }
	serviceUID = func() (uint32, bool) { return serviceUser, serviceExists }
	return func() { peerOf, serviceUID = oldPeer, oldSvc }
}

// SetFirstAnswerTimeout shortens the first-answer deadline until the returned function runs.
func SetFirstAnswerTimeout(d time.Duration) (restore func()) {
	old := firstAnswerTimeout
	firstAnswerTimeout = d
	return func() { firstAnswerTimeout = old }
}

// FailPeerRead makes the peer lookup fail until the returned function runs.
func FailPeerRead() (restore func()) {
	old := peerOf
	peerOf = func(net.Conn) (peerCred, error) { return peerCred{}, errors.New("no credentials") }
	return func() { peerOf = old }
}
