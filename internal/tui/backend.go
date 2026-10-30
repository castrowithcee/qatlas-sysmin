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
	// SourceTest reads one project and asks the registries; it may take minutes. Project reads one project.
	Project(name string) (control.ProjectDetail, error)
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
	// Stacks lists the running Compose stacks and the projects that manage them; it only reads.
	Stacks() (control.StacksResult, error)
	// Approval and Credentials only read; Credentials never carries a value.
	Approval(project string) (control.ApprovalDetail, error)
	Credentials() ([]control.CredentialSummary, error)
	// The calls below change the service and need the admin session. Approve and Discard handle exactly the
	// state the fingerprint names; ApproveAll and DiscardAll handle the shown approvals and are refused as
	// changed unless exactly these are open. SetCredential is the one call that carries secret values.
	Approve(project, fingerprint string) error
	ApproveAll(shown []control.ShownApproval) (control.ApprovalsAllResult, error)
	Discard(project, fingerprint string) error
	DiscardAll(shown []control.ShownApproval) (control.ApprovalsAllResult, error)
	SetCredential(p control.CredentialSetParams) (control.CredentialResult, error)
	RemoveCredential(name string) (control.CredentialResult, error)
	// Channels, Templates, Template, LogEntries and VerifyLog only read. LogEntries and VerifyLog never need the
	// admin session; every call is an entry of the audit log itself, so none is made on a timer.
	Channels() ([]control.ChannelSummary, error)
	Templates() ([]control.TemplateSummary, error)
	Template(name string) (control.TemplateDetail, error)
	LogEntries(p control.LogListParams) (control.LogListResult, error)
	VerifyLog() (control.LogVerifyResult, error)
	// DeployStatus, DeployHistory and DeployLog only read: the state of a project, its runs with the oldest
	// first, and the log of one run. None needs the admin session.
	DeployStatus(project string) (control.DeployStatusResult, error)
	DeployHistory(project string) (control.DeployHistoryResult, error)
	DeployLog(project, run string) (control.DeployLogResult, error)
	// TestChannel sends one message to an approved channel; it needs no admin session but is never repeated.
	TestChannel(name string) (control.NotifyTestResult, error)
	// AddChannel, RemoveChannel and DeriveTemplate change the service and need the admin session.
	AddChannel(p control.ChannelParams) (control.ChangeResult, error)
	RemoveChannel(name string) (control.ChangeResult, error)
	DeriveTemplate(p control.TemplateDeriveParams) (control.TemplateDetail, error)
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

func (b *ClientBackend) ApproveAll(shown []control.ShownApproval) (res control.ApprovalsAllResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.ApproveAll(shown); return e })
	return res, err
}

func (b *ClientBackend) Discard(project, fingerprint string) error {
	return b.do(false, func(c *client.Client) error { return c.Discard(project, fingerprint) })
}

func (b *ClientBackend) DiscardAll(shown []control.ShownApproval) (res control.ApprovalsAllResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.DiscardAll(shown); return e })
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

func (b *ClientBackend) Channels() (list []control.ChannelSummary, err error) {
	err = b.do(true, func(c *client.Client) (e error) { list, e = c.Channels(); return e })
	return list, err
}

func (b *ClientBackend) Templates() (list []control.TemplateSummary, err error) {
	err = b.do(true, func(c *client.Client) (e error) { list, e = c.Templates(); return e })
	return list, err
}

func (b *ClientBackend) Template(name string) (d control.TemplateDetail, err error) {
	err = b.do(true, func(c *client.Client) (e error) { d, e = c.Template(name); return e })
	return d, err
}

func (b *ClientBackend) LogEntries(p control.LogListParams) (res control.LogListResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.LogEntries(p); return e })
	return res, err
}

func (b *ClientBackend) VerifyLog() (res control.LogVerifyResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.VerifyLog(); return e })
	return res, err
}

func (b *ClientBackend) DeployStatus(project string) (res control.DeployStatusResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.DeployStatus(project); return e })
	return res, err
}

func (b *ClientBackend) DeployHistory(project string) (res control.DeployHistoryResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.DeployHistory(project); return e })
	return res, err
}

func (b *ClientBackend) DeployLog(project, run string) (res control.DeployLogResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.DeployLog(project, run); return e })
	return res, err
}

func (b *ClientBackend) TestChannel(name string) (res control.NotifyTestResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.TestChannel(name); return e })
	return res, err
}

func (b *ClientBackend) AddChannel(p control.ChannelParams) (res control.ChangeResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.AddChannel(p); return e })
	return res, err
}

func (b *ClientBackend) RemoveChannel(name string) (res control.ChangeResult, err error) {
	err = b.do(false, func(c *client.Client) (e error) { res, e = c.RemoveChannel(name); return e })
	return res, err
}

func (b *ClientBackend) DeriveTemplate(p control.TemplateDeriveParams) (d control.TemplateDetail, err error) {
	err = b.do(false, func(c *client.Client) (e error) { d, e = c.DeriveTemplate(p); return e })
	return d, err
}

func (b *ClientBackend) Project(name string) (res control.ProjectDetail, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.Project(name); return e })
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

func (b *ClientBackend) Stacks() (res control.StacksResult, err error) {
	err = b.do(true, func(c *client.Client) (e error) { res, e = c.Stacks(); return e })
	return res, err
}
