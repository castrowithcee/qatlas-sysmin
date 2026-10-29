package admin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const adminPassphrase = "synthetic-admin-passphrase-5d1b"

func TestMain(m *testing.M) {
	vault.LowerWorkFactorForTests()
	os.Exit(m.Run())
}

// testVault creates an encrypted vault and returns a handle unlocked in this process and a locked one.
func testVault(t *testing.T) (unlocked, locked *vault.Vault) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "vault")
	unlocked = vault.New(dir)
	if err := unlocked.Create(adminPassphrase); err != nil {
		t.Fatal(err)
	}
	return unlocked, vault.New(dir)
}

func TestVerifyAdmin(t *testing.T) {
	unlocked, locked := testVault(t)
	for name, v := range map[string]*vault.Vault{"unlocked": unlocked, "locked": locked} {
		if err := VerifyAdmin(v, adminPassphrase); err != nil {
			t.Errorf("%s: VerifyAdmin(right) = %v", name, err)
		}
		wrong := "synthetic-wrong-passphrase"
		err := VerifyAdmin(v, wrong)
		if !errors.Is(err, ErrWrongPassphrase) || strings.Contains(err.Error(), wrong) {
			t.Errorf("%s: VerifyAdmin(wrong) = %v, want ErrWrongPassphrase without the passphrase", name, err)
		}
		if err := VerifyAdmin(v, ""); !errors.Is(err, ErrWrongPassphrase) {
			t.Errorf("%s: VerifyAdmin(empty) = %v", name, err)
		}
	}
	// Verifying does not unlock.
	if state, _ := locked.State(); state != vault.StateLocked {
		t.Errorf("VerifyAdmin unlocked the vault: %v", state)
	}
}

func TestVerifyAdminWithoutVault(t *testing.T) {
	if err := VerifyAdmin(nil, adminPassphrase); err == nil {
		t.Error("VerifyAdmin(nil) succeeded")
	}
	absent := vault.New(filepath.Join(t.TempDir(), "vault"))
	if err := VerifyAdmin(absent, adminPassphrase); !errors.Is(err, vault.ErrAbsent) {
		t.Errorf("VerifyAdmin(absent) = %v, want ErrAbsent", err)
	}
}

// clock is a controllable clock.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *clock { return &clock{t: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)} }

func TestSessionExpiresAfterTimeout(t *testing.T) {
	_, locked := testVault(t)
	c := newClock()
	s := NewSession(c.now)
	const timeout = 10 * time.Minute

	if s.Active() {
		t.Fatal("a new session is active")
	}
	if err := s.Open(locked, "wrong", timeout); !errors.Is(err, ErrWrongPassphrase) || s.Active() {
		t.Fatalf("Open(wrong) = %v, active %v", err, s.Active())
	}
	if err := s.Open(locked, adminPassphrase, timeout); err != nil || !s.Active() {
		t.Fatalf("Open(right) = %v, active %v", err, s.Active())
	}

	// Activity renews the idle deadline.
	c.advance(9 * time.Minute)
	if _, ok := s.Authorize(timeout); !ok {
		t.Fatal("an active session refused an action")
	}
	c.advance(9 * time.Minute)
	if !s.Active() || s.Remaining() != time.Minute {
		t.Fatalf("session after activity: active %v, remaining %v", s.Active(), s.Remaining())
	}

	// Idle past the timeout: the session no longer covers an action and the passphrase is asked again.
	c.advance(time.Minute)
	if s.Active() || s.Remaining() != 0 {
		t.Fatal("the session outlived its timeout")
	}
	if proof, ok := s.Authorize(timeout); ok || proof.Granted() {
		t.Fatal("an expired session authorized an action")
	}
	if err := s.Open(locked, adminPassphrase, timeout); err != nil || !s.Active() {
		t.Fatalf("Open() after expiry = %v", err)
	}
}

func TestZeroTimeoutAsksEveryTime(t *testing.T) {
	_, locked := testVault(t)
	s := NewSession(newClock().now)
	if err := s.Open(locked, adminPassphrase, 0); err != nil {
		t.Fatal(err)
	}
	if s.Active() {
		t.Fatal("a session with timeout 0 is active")
	}
	if _, ok := s.Authorize(0); ok {
		t.Fatal("a session with timeout 0 authorized an action without a passphrase")
	}

	// A timeout lowered to 0 while a session is active ends it with the next action.
	s.Grant(time.Hour)
	if _, ok := s.Authorize(0); !ok {
		t.Fatal("the active session refused the action")
	}
	if s.Active() {
		t.Fatal("the session survived an action under timeout 0")
	}
}

func TestEndAndProof(t *testing.T) {
	s := NewSession(newClock().now)
	s.Grant(time.Hour)
	s.End()
	if s.Active() {
		t.Fatal("End() left the session active")
	}
	if (Proof{}).Granted() {
		t.Fatal("the zero Proof proves something")
	}
	s.Grant(time.Hour)
	if proof, ok := s.Authorize(time.Hour); !ok || !proof.Granted() {
		t.Fatal("an active session gave no proof")
	}
}

func TestZeroValueSessionUsesRealClock(t *testing.T) {
	var s Session
	s.Grant(time.Hour)
	if !s.Active() {
		t.Fatal("zero-value session did not become active")
	}
}
