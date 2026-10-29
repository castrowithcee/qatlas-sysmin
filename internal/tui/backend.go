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
	// Project, DeployStatus, DeployLog and SourceTest read one project; SourceTest asks the registries and may
	// take minutes.
	Project(name string) (control.ProjectDetail, error)
	DeployStatus(project string) (control.DeployStatusResult, error)
	DeployLog(project, run string) (control.DeployLogResult, error)
	SourceTest(project string) (control.SourceTestResult, error)
	// DeployRunNow deploys what the version rules of the project allow; DeployControl pauses, resumes, pins or
	// unpins it, op being one of the control.OpDeploy names. Neither needs the admin session.
	DeployRunNow(project string) (control.DeployRunNowResult, error)
	DeployControl(op, project string) error
	// RemoveProject deletes the project file the revision names; it needs the admin session to take effect at
	// once.
	RemoveProject(name, revision string) (control.ChangeResult, error)
	// AddProject stores a new project and EditProject changes a stored one, both from the fields or the whole
	// file of p; they need the admin session to take effect at once.
	AddProject(p control.ProjectParams) (control.ChangeResult, error)
	EditProject(p control.ProjectParams) (control.ChangeResult, error)
	// Backups and Changelog only read; Changelog may take minutes. DeployUpdate (versions a person chose, one
	// run per project) and DeployRestore need the admin session.
	Backups(project string) ([]string, error)
	Changelog(project, image, to string) (control.ChangelogResult, error)
	DeployUpdate(updates []control.ProjectUpdate) (control.DeployUpdateResult, error)
	DeployRestore(project, backup string) (control.DeployRestoreResult, error)
	// AdoptProject proposes a project for the running stack in a directory. The interface asks with DryRun
	// only: nothing is stored.
	AdoptProject(p control.AdoptParams) (control.AdoptResult, error)
	// Credentials and Channels only read; neither carries a secret. SetCredential stores one and needs the
	// admin session to take effect at once.
	Credentials() ([]control.CredentialSummary, error)
	SetCredential(p control.CredentialSetParams) (control.CredentialResult, error)
	Channels() ([]control.ChannelSummary, error)
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

func (b *ClientBackend) Project(name string) (res control.ProjectDetail, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.Project(name); return e })
	return res, err
}

func (b *ClientBackend) DeployStatus(project string) (res control.DeployStatusResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.DeployStatus(project); return e })
	return res, err
}

func (b *ClientBackend) DeployLog(project, run string) (res control.DeployLogResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.DeployLog(project, run); return e })
	return res, err
}

func (b *ClientBackend) SourceTest(project string) (res control.SourceTestResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.SourceTest(project); return e })
	return res, err
}

func (b *ClientBackend) DeployRunNow(project string) (res control.DeployRunNowResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.DeployRunNow(project); return e })
	return res, err
}

func (b *ClientBackend) DeployControl(op, project string) error {
	return b.do(false, func(c *client.Client) error { return c.DeployControl(op, project) })
}

func (b *ClientBackend) RemoveProject(name, revision string) (res control.ChangeResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.RemoveProject(name, revision); return e })
	return res, err
}

func (b *ClientBackend) AddProject(p control.ProjectParams) (res control.ChangeResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.AddProject(p); return e })
	return res, err
}

func (b *ClientBackend) EditProject(p control.ProjectParams) (res control.ChangeResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.EditProject(p); return e })
	return res, err
}

func (b *ClientBackend) Backups(project string) (list []string, err error) {
	err = b.do(true, func(c *client.Client) (e error) { list, e = c.Backups(project); return e })
	return list, err
}

func (b *ClientBackend) Changelog(project, image, to string) (res control.ChangelogResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.Changelog(project, image, to); return e })
	return res, err
}

func (b *ClientBackend) DeployUpdate(updates []control.ProjectUpdate) (res control.DeployUpdateResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.DeployUpdate(updates); return e })
	return res, err
}

func (b *ClientBackend) DeployRestore(project, backup string) (res control.DeployRestoreResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.DeployRestore(project, backup); return e })
	return res, err
}

func (b *ClientBackend) AdoptProject(p control.AdoptParams) (res control.AdoptResult, err error) {
	// Only a request that stores nothing may be repeated on a fresh connection.
	err = b.do(p.DryRun, func(c *client.Client) (e error) { res, e = c.AdoptProject(p); return e })
	return res, err
}

func (b *ClientBackend) Credentials() (list []control.CredentialSummary, err error) {
	err = b.do(true, func(c *client.Client) (e error) { list, e = c.Credentials(); return e })
	return list, err
}

func (b *ClientBackend) SetCredential(p control.CredentialSetParams) (res control.CredentialResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.SetCredential(p); return e })
	return res, err
}

func (b *ClientBackend) Channels() (list []control.ChannelSummary, err error) {
	err = b.do(true, func(c *client.Client) (e error) { list, e = c.Channels(); return e })
	return list, err
}
