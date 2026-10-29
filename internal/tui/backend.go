package tui

import (
	"errors"
	"sync"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// Backend is what the interface asks of the service. Every call runs as a command, off the event loop, so an
// implementation may block; it must serialize its calls, because the connection behind it is not safe for
// concurrent use. A failure the service reports is a *control.Error.
type Backend interface {
	// Status reports the vault and the admin session of this window.
	Status() (control.StatusResult, error)
	// Unlock opens the vault; OpenSession starts the admin session of this window.
	Unlock(passphrase string) error
	OpenSession(passphrase string) error
	// EndSession ends the admin session of this window.
	EndSession() error
	// CreateVault creates the vault with its first passphrase and leaves it unlocked.
	CreateVault(passphrase string) error
	// ChangePassphrase replaces the vault passphrase; SetAdminTimeout sets vault.admin_timeout from a duration
	// text. Both need the admin session.
	ChangePassphrase(oldPassphrase, newPassphrase string) error
	SetAdminTimeout(timeout string) (control.SettingsResult, error)
	// Settings, Approvals and Projects only read.
	Settings() (control.SettingsResult, error)
	Approvals() ([]control.ApprovalSummary, error)
	Projects() ([]control.ProjectSummary, error)
	// Approval and Credentials only read; Credentials never carries a value.
	Approval(project string) (control.ApprovalDetail, error)
	Credentials() ([]control.CredentialSummary, error)
	// The calls below change the service and need the admin session. Approve approves exactly the state the
	// fingerprint names; SetCredential is the one call that carries secret values.
	Approve(project, fingerprint string) error
	ApproveAll() (control.ApprovalsAllResult, error)
	Discard(project string) error
	DiscardAll() (control.ApprovalsAllResult, error)
	SetCredential(p control.CredentialSetParams) (control.CredentialResult, error)
	RemoveCredential(name string) (control.CredentialResult, error)
}

// NewBackend is the Backend over the control socket at path. It dials on the first call and again after the
// connection broke; a new connection starts without an admin session, which is the truth after a loss.
func NewBackend(socket string) *ClientBackend { return &ClientBackend{socket: socket} }

// ClientBackend is a Backend over one connection to the service.
type ClientBackend struct {
	socket string
	mu     sync.Mutex
	conn   *client.Client
}

// Close ends the connection, if there is one.
func (b *ClientBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn == nil {
		return nil
	}
	err := b.conn.Close()
	b.conn = nil
	return err
}

// do runs fn on the connection. A failure that is no answer of the service means the connection is gone or
// not trustworthy, so it is dropped and the next call dials again. A read-only call may be repeated once on a
// fresh connection; a changing one never is, because its outcome is not known.
func (b *ClientBackend) do(repeatable bool, fn func(*client.Client) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if b.conn == nil {
			c, err := client.Dial(b.socket)
			if err != nil {
				return err
			}
			b.conn = c
		}
		err := fn(b.conn)
		var ce *control.Error
		if err == nil || errors.As(err, &ce) {
			return err
		}
		_ = b.conn.Close()
		b.conn = nil
		if !repeatable || attempt > 0 {
			return err
		}
	}
}

func (b *ClientBackend) Status() (st control.StatusResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { st, e = c.Status(); return e })
	return st, err
}

func (b *ClientBackend) Unlock(passphrase string) error {
	return b.do(false, func(c *client.Client) error { return c.Unlock(passphrase) })
}

func (b *ClientBackend) OpenSession(passphrase string) error {
	return b.do(false, func(c *client.Client) error { return c.OpenSession(passphrase) })
}

func (b *ClientBackend) EndSession() error {
	return b.do(false, func(c *client.Client) error { return c.EndSession() })
}

func (b *ClientBackend) CreateVault(passphrase string) error {
	return b.do(false, func(c *client.Client) error { return c.CreateVault(passphrase) })
}

func (b *ClientBackend) ChangePassphrase(oldPassphrase, newPassphrase string) error {
	return b.do(false, func(c *client.Client) error { return c.ChangePassphrase(oldPassphrase, newPassphrase) })
}

func (b *ClientBackend) SetAdminTimeout(timeout string) (res control.SettingsResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.SetAdminTimeout(timeout); return e })
	return res, err
}

func (b *ClientBackend) Settings() (res control.SettingsResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.Settings(); return e })
	return res, err
}

func (b *ClientBackend) Approvals() (list []control.ApprovalSummary, err error) {
	err = b.do(true, func(c *client.Client) (e error) { list, e = c.Approvals(); return e })
	return list, err
}

func (b *ClientBackend) Projects() (list []control.ProjectSummary, err error) {
	err = b.do(true, func(c *client.Client) (e error) { list, e = c.Projects(); return e })
	return list, err
}

func (b *ClientBackend) Approval(project string) (d control.ApprovalDetail, err error) {
	err = b.do(true, func(c *client.Client) (e error) { d, e = c.Approval(project); return e })
	return d, err
}

func (b *ClientBackend) Credentials() (list []control.CredentialSummary, err error) {
	err = b.do(true, func(c *client.Client) (e error) { list, e = c.Credentials(); return e })
	return list, err
}

func (b *ClientBackend) Approve(project, fingerprint string) error {
	return b.do(false, func(c *client.Client) error { return c.Approve(project, fingerprint) })
}

func (b *ClientBackend) ApproveAll() (res control.ApprovalsAllResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.ApproveAll(); return e })
	return res, err
}

func (b *ClientBackend) Discard(project string) error {
	return b.do(false, func(c *client.Client) error { return c.Discard(project) })
}

func (b *ClientBackend) DiscardAll() (res control.ApprovalsAllResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.DiscardAll(); return e })
	return res, err
}

func (b *ClientBackend) SetCredential(p control.CredentialSetParams) (res control.CredentialResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.SetCredential(p); return e })
	return res, err
}

func (b *ClientBackend) RemoveCredential(name string) (res control.CredentialResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.RemoveCredential(name); return e })
	return res, err
}
