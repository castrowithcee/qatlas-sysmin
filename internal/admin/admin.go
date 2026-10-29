// Package admin proves that a person who knows the vault passphrase stands behind a changing action. A
// passphrase is verified against the vault; a verified session then covers further actions until it has
// been idle for the configured timeout.
package admin

import (
	"errors"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// ErrWrongPassphrase is the error VerifyAdmin returns for a passphrase that does not match the vault's. It
// is the vault's own error, so callers can test it without importing the vault package.
var ErrWrongPassphrase = vault.ErrWrongPassphrase

var errNoVault = errors.New("no vault is available to approve with")

// VerifyAdmin checks passphrase against the vault's key without any side effect: the vault stays locked or
// unlocked as it was and no pending entry is merged. A missing or not yet created vault has no passphrase
// to approve and is refused. The error never contains the passphrase.
func VerifyAdmin(v *vault.Vault, passphrase string) error {
	if v == nil {
		return errNoVault
	}
	return v.VerifyPassphrase(passphrase)
}

// Session is the proof, kept after a passphrase was verified, that a surface may keep acting. Its zero
// value is an inactive session on the real clock. It is not safe for concurrent use; a surface that is must
// serialize access itself.
//
// The vault.admin_timeout is passed to each call rather than fixed, because a surface may re-read it. A
// timeout above zero keeps the session open until that long has passed without activity; a timeout of zero
// keeps no session at all, so every action asks for the passphrase again.
type Session struct {
	now   func() time.Time
	until time.Time
}

// NewSession returns an inactive session. now is the clock, or nil for time.Now.
func NewSession(now func() time.Time) Session { return Session{now: now} }

func (s *Session) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// Active reports whether the session covers an action right now.
func (s *Session) Active() bool { return !s.until.IsZero() && s.clock().Before(s.until) }

// Grant starts, or renews, the session after a passphrase just proved it. A timeout of zero or less ends
// it instead.
func (s *Session) Grant(timeout time.Duration) {
	if timeout <= 0 {
		s.until = time.Time{}
		return
	}
	s.until = s.clock().Add(timeout)
}

// Open verifies passphrase against v and, only when it is right, starts or renews the session.
func (s *Session) Open(v *vault.Vault, passphrase string, timeout time.Duration) error {
	if err := VerifyAdmin(v, passphrase); err != nil {
		return err
	}
	s.Grant(timeout)
	return nil
}

// Consume uses the session for one action and reports whether it covered it. An active session has its
// idle deadline renewed; with a timeout of zero or less it ends instead, and anything else is refused.
func (s *Session) Consume(timeout time.Duration) bool {
	if !s.Active() {
		return false
	}
	if timeout <= 0 {
		s.until = time.Time{}
		return true
	}
	s.until = s.clock().Add(timeout)
	return true
}

// End discards the session.
func (s *Session) End() { s.until = time.Time{} }

// Remaining reports how long the session stays active, or zero when it is not.
func (s *Session) Remaining() time.Duration {
	if !s.Active() {
		return 0
	}
	return s.until.Sub(s.clock())
}

// Proof is what a changing action demands from its caller: it exists only as the result of
// Session.Authorize, so a surface cannot make one up. Its zero value proves nothing.
type Proof struct{ granted bool }

// Granted reports whether the proof was issued by a session.
func (p Proof) Granted() bool { return p.granted }

// Authorize uses the session for one action like Consume and, when it covered the action, returns the
// proof. A refused session yields the zero proof.
func (s *Session) Authorize(timeout time.Duration) (Proof, bool) {
	if !s.Consume(timeout) {
		return Proof{}, false
	}
	return Proof{granted: true}, true
}
