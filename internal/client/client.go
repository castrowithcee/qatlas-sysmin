// Package client is how the command line and the terminal interface talk to the service: over its control
// socket and nowhere else.
package client

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// DialTimeout bounds connecting to the service; CallTimeout bounds one call.
const (
	DialTimeout = 5 * time.Second
	CallTimeout = 30 * time.Second
)

// Client is one connection to the service. It is not safe for concurrent calls.
type Client struct {
	conn net.Conn
	br   *bufio.Reader
}

// Dial connects to the control socket at path; an empty path means control.DefaultSocketPath.
func Dial(path string) (*Client, error) {
	if path == "" {
		path = control.DefaultSocketPath
	}
	conn, err := net.DialTimeout("unix", path, DialTimeout)
	if err != nil {
		if errors.Is(err, syscall.EACCES) && sessionLacksGroup() {
			err = ErrSessionGroup
		}
		return nil, fmt.Errorf("cannot reach the service: %w", err)
	}
	return &Client{conn: conn, br: bufio.NewReader(conn)}, nil
}

// ErrSessionGroup is a refused connection of a user who is in the group sysmin, from a login session that
// started before the user joined it: the session does not carry the group yet.
var ErrSessionGroup = errors.New("this login session started before the user joined the group " + control.ServiceGroup)

// sessionLacksGroup reports whether the group database lists the user in ServiceGroup while this process
// does not carry the group. It is a variable so a test can stand in for the system.
var sessionLacksGroup = func() bool {
	g, err := user.LookupGroup(control.ServiceGroup)
	if err != nil {
		return false
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil || os.Getegid() == gid {
		return false
	}
	if groups, err := os.Getgroups(); err != nil || slices.Contains(groups, gid) {
		return false
	}
	u, err := user.Current()
	if err != nil {
		return false
	}
	ids, err := u.GroupIds()
	return err == nil && slices.Contains(ids, g.Gid)
}

// Close ends the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Call runs op with params (nil for none) and decodes the result into result (nil to discard it). A failure
// the service reports is a *control.Error.
func (c *Client) Call(op string, params, result any) error {
	req := control.Request{V: control.Version, Op: op}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("cannot encode the parameters: %w", err)
		}
		req.Params = data
		defer clear(data) // the parameters may carry a passphrase
	}
	timeout := CallTimeout
	switch op {
	case control.OpSourceTest:
		timeout = control.SourceTestTimeout + CallTimeout
	case control.OpProjectsChangelog:
		timeout = control.ChangelogTimeout + CallTimeout
	case control.OpProjectsAdopt:
		timeout = control.AdoptTimeout + CallTimeout
	}
	_ = c.conn.SetDeadline(time.Now().Add(timeout))
	if err := control.WriteLine(c.conn, req); err != nil {
		return fmt.Errorf("cannot send the request: %w", err)
	}
	line, err := control.ReadLine(c.br, control.MaxResponseBytes)
	if err != nil {
		return fmt.Errorf("cannot read the answer: %w", err)
	}
	var resp control.Response
	if err := json.Unmarshal(line, &resp); err != nil || resp.V != control.Version {
		return errors.New("the service answered in a form this program does not understand")
	}
	if !resp.OK {
		return &control.Error{Code: resp.Error, Problems: resp.Problems, Paths: resp.Paths}
	}
	if result != nil && resp.Result != nil {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("cannot decode the result: %w", err)
		}
	}
	return nil
}

// Status asks for the service's versions, vault state and socket.
func (c *Client) Status() (control.StatusResult, error) {
	var out control.StatusResult
	err := c.Call(control.OpStatus, nil, &out)
	return out, err
}

// VaultStatus asks for the vault's state and how it was unlocked.
func (c *Client) VaultStatus() (control.VaultStatusResult, error) {
	var out control.VaultStatusResult
	err := c.Call(control.OpVaultStatus, nil, &out)
	return out, err
}

// Unlock opens the service's vault with its passphrase. A wrong one is control.CodeWrongPassphrase.
func (c *Client) Unlock(passphrase string) error {
	return c.Call(control.OpUnlock, control.PassphraseParams{Passphrase: passphrase}, nil)
}

// OpenSession starts the admin session of this connection with the vault passphrase.
func (c *Client) OpenSession(passphrase string) error {
	return c.Call(control.OpSessionOpen, control.PassphraseParams{Passphrase: passphrase}, nil)
}

// EndSession ends the admin session of this connection.
func (c *Client) EndSession() error { return c.Call(control.OpSessionEnd, nil, nil) }

// CreateVault creates the vault with its first passphrase and leaves it unlocked. It is control.CodeExists
// once a vault exists.
func (c *Client) CreateVault(passphrase string) error {
	return c.Call(control.OpVaultCreate, control.PassphraseParams{Passphrase: passphrase}, nil)
}

// ChangePassphrase replaces the vault passphrase. It needs the admin session of this connection; a wrong
// old passphrase is control.CodeWrongPassphrase.
func (c *Client) ChangePassphrase(oldPassphrase, newPassphrase string) error {
	return c.Call(control.OpVaultPassphrase, control.PassphraseChangeParams{Old: oldPassphrase, New: newPassphrase}, nil)
}

// Settings reads the effective admin timeout.
func (c *Client) Settings() (control.SettingsResult, error) {
	var out control.SettingsResult
	err := c.Call(control.OpSettingsShow, nil, &out)
	return out, err
}

// SetAdminTimeout sets vault.admin_timeout from a duration text such as "5m" or "0". It needs the admin
// session of this connection.
func (c *Client) SetAdminTimeout(timeout string) (control.SettingsResult, error) {
	var out control.SettingsResult
	err := c.Call(control.OpSettingsAdminTimeout, control.AdminTimeoutParams{AdminTimeout: timeout}, &out)
	return out, err
}

// Approvals lists the open approvals of projects.
func (c *Client) Approvals() ([]control.ApprovalSummary, error) {
	var out control.ApprovalsListResult
	err := c.Call(control.OpApprovalsList, nil, &out)
	return out.Approvals, err
}

// Approval shows the open approval of one project with its differing fields.
func (c *Client) Approval(project string) (control.ApprovalDetail, error) {
	var out control.ApprovalDetail
	err := c.Call(control.OpApprovalsShow, control.ApprovalParams{Project: project}, &out)
	return out, err
}

// Approve approves the state of a project that a list or show reported with fingerprint. It needs the admin
// session of this connection.
func (c *Client) Approve(project, fingerprint string) error {
	return c.Call(control.OpApprovalsApprove, control.ApprovalParams{Project: project, Fingerprint: fingerprint}, nil)
}

// ApproveAll approves every open approval as it is now. It needs the admin session of this connection.
func (c *Client) ApproveAll() (control.ApprovalsAllResult, error) {
	var out control.ApprovalsAllResult
	err := c.Call(control.OpApprovalsApproveAll, nil, &out)
	return out, err
}

// Discard restores a project from its approved state. It needs the admin session of this connection.
func (c *Client) Discard(project string) error {
	return c.Call(control.OpApprovalsDiscard, control.ApprovalParams{Project: project}, nil)
}

// DiscardAll restores every project that has an open approval a restore can undo. It needs the admin
// session of this connection.
func (c *Client) DiscardAll() (control.ApprovalsAllResult, error) {
	var out control.ApprovalsAllResult
	err := c.Call(control.OpApprovalsDiscardAll, nil, &out)
	return out, err
}

// Projects lists the stored projects.
func (c *Client) Projects() ([]control.ProjectSummary, error) {
	var out control.ProjectsListResult
	err := c.Call(control.OpProjectsList, nil, &out)
	return out.Projects, err
}

// Project shows the stored file of one project.
func (c *Client) Project(name string) (control.ProjectDetail, error) {
	var out control.ProjectDetail
	err := c.Call(control.OpProjectsShow, control.ProjectRef{Name: name}, &out)
	return out, err
}

// CheckProject validates a project and resolves its steps for an example version (empty for the default).
// Invalid input is a *control.Error with control.CodeInvalid and its Problems.
func (c *Client) CheckProject(name, version string) (control.CheckResult, error) {
	var out control.CheckResult
	err := c.Call(control.OpProjectsCheck, control.ProjectRef{Name: name, Version: version}, &out)
	return out, err
}

// AddProject stores a new project. The change is an open approval unless this connection has an admin
// session; the result says which.
func (c *Client) AddProject(p control.ProjectParams) (control.ChangeResult, error) {
	var out control.ChangeResult
	err := c.Call(control.OpProjectsAdd, p, &out)
	return out, err
}

// EditProject changes a stored project, on the terms of AddProject.
func (c *Client) EditProject(p control.ProjectParams) (control.ChangeResult, error) {
	var out control.ChangeResult
	err := c.Call(control.OpProjectsEdit, p, &out)
	return out, err
}

// RemoveProject removes a project file, on the terms of AddProject. revision, when set, is the revision
// the removal is based on.
func (c *Client) RemoveProject(name, revision string) (control.ChangeResult, error) {
	var out control.ChangeResult
	err := c.Call(control.OpProjectsRemove, control.ProjectRef{Name: name, Revision: revision}, &out)
	return out, err
}

// Credentials lists the credentials of the vault with what uses them. It never returns a value.
func (c *Client) Credentials() ([]control.CredentialSummary, error) {
	var out control.CredentialsListResult
	err := c.Call(control.OpCredentialsList, nil, &out)
	return out.Credentials, err
}

// SetCredential stores a credential, or replaces the one of that name under a new id. The fields carry the
// secret. Whatever uses the credential becomes an open approval unless this connection has an admin
// session; the result says which.
func (c *Client) SetCredential(p control.CredentialSetParams) (control.CredentialResult, error) {
	var out control.CredentialResult
	err := c.Call(control.OpCredentialsSet, p, &out)
	return out, err
}

// RemoveCredential deletes a credential. One that a project or channel names needs the admin session of
// this connection, otherwise the answer is control.CodeInUse with the uses as problems.
func (c *Client) RemoveCredential(name string) (control.CredentialResult, error) {
	var out control.CredentialResult
	err := c.Call(control.OpCredentialsRemove, control.CredentialRef{Name: name}, &out)
	return out, err
}

// Channels lists the notification channels.
func (c *Client) Channels() ([]control.ChannelSummary, error) {
	var out control.ChannelsListResult
	err := c.Call(control.OpChannelsList, nil, &out)
	return out.Channels, err
}

// AddChannel stores a new channel, on the terms of AddProject; the approval is the one of the channels.
func (c *Client) AddChannel(p control.ChannelParams) (control.ChangeResult, error) {
	var out control.ChangeResult
	err := c.Call(control.OpChannelsAdd, p, &out)
	return out, err
}

// TestChannel sends a test message to the approved channel with the given name.
func (c *Client) TestChannel(name string) (control.NotifyTestResult, error) {
	var out control.NotifyTestResult
	err := c.Call(control.OpNotifyTest, control.NotifyTestParams{Channel: name}, &out)
	return out, err
}

// RemoveChannel removes a channel, on the terms of AddChannel.
func (c *Client) RemoveChannel(name string) (control.ChangeResult, error) {
	var out control.ChangeResult
	err := c.Call(control.OpChannelsRemove, control.ChannelRef{Name: name}, &out)
	return out, err
}

// SourceTest reads the versions of the sources of a project's approved state. A source that cannot be read
// is a result with an error code, not a failed call.
func (c *Client) SourceTest(project string) (control.SourceTestResult, error) {
	var out control.SourceTestResult
	err := c.Call(control.OpSourceTest, control.SourceTestParams{Project: project}, &out)
	return out, err
}

// DeployStatus reads the deploy state of a project.
func (c *Client) DeployStatus(project string) (control.DeployStatusResult, error) {
	var out control.DeployStatusResult
	err := c.Call(control.OpDeployStatus, control.DeployRef{Project: project}, &out)
	return out, err
}

// DeployHistory lists the latest runs of a project, the oldest first.
func (c *Client) DeployHistory(project string) (control.DeployHistoryResult, error) {
	var out control.DeployHistoryResult
	err := c.Call(control.OpDeployHistory, control.DeployRef{Project: project}, &out)
	return out, err
}

// DeployRunNow checks the sources of a project at once and deploys what its version rules allow without a
// human. The service waits for the run up to control.DeployWait; a longer run goes on and the result says
// it is pending.
func (c *Client) DeployRunNow(project string) (control.DeployRunNowResult, error) {
	var out control.DeployRunNowResult
	err := c.Call(control.OpDeployRunNow, control.DeployRef{Project: project}, &out)
	return out, err
}

// DeployControl pauses, resumes, pins or unpins a project; op is one of control.OpDeployPause,
// OpDeployResume, OpDeployPin and OpDeployUnpin.
func (c *Client) DeployControl(op, project string) error {
	return c.Call(op, control.DeployRef{Project: project}, nil)
}

// Backups lists the backups of a project, the oldest first.
func (c *Client) Backups(project string) ([]string, error) {
	var out control.BackupsListResult
	err := c.Call(control.OpBackupsList, control.DeployRef{Project: project}, &out)
	return out.Backups, err
}

// DeployUpdate updates projects to versions a person chose, one after the other. It needs the admin session
// of this connection. A project that has not finished when control.DeployWait is over is pending.
func (c *Client) DeployUpdate(updates []control.ProjectUpdate) (control.DeployUpdateResult, error) {
	var out control.DeployUpdateResult
	err := c.Call(control.OpDeployUpdate, control.DeployUpdateParams{Updates: updates}, &out)
	return out, err
}

// DeployRestore restores the data of a project from one of its backups. It needs the admin session of this
// connection.
func (c *Client) DeployRestore(project, backup string) (control.DeployRestoreResult, error) {
	var out control.DeployRestoreResult
	err := c.Call(control.OpDeployRestore, control.DeployRestoreParams{Project: project, Backup: backup}, &out)
	return out, err
}

// Changelog reads the releases between the current and the target version of the sources of a project's
// approved state. image limits it to one image, to replaces the target version. A source that cannot be read
// is a result with an error code, not a failed call.
func (c *Client) Changelog(project, image, to string) (control.ChangelogResult, error) {
	var out control.ChangelogResult
	err := c.Call(control.OpProjectsChangelog, control.ChangelogParams{Project: project, Image: image, To: to}, &out)
	return out, err
}

// AdoptProject asks the service to propose a project for the stack running from p.Dir and, unless p.DryRun,
// to store it. The stored project is an open approval on the terms of AddProject.
func (c *Client) AdoptProject(p control.AdoptParams) (control.AdoptResult, error) {
	var out control.AdoptResult
	err := c.Call(control.OpProjectsAdopt, p, &out)
	return out, err
}

// LogEntries reads one page of the action log, newest first. p.After continues from the Next of the
// previous page. It needs no admin session.
func (c *Client) LogEntries(p control.LogListParams) (control.LogListResult, error) {
	var out control.LogListResult
	err := c.Call(control.OpLogList, p, &out)
	return out, err
}

// VerifyLog checks the hash chain of the action log, and its check values while the vault is unlocked. A
// broken chain is a result with Intact false, not a failed call.
func (c *Client) VerifyLog() (control.LogVerifyResult, error) {
	var out control.LogVerifyResult
	err := c.Call(control.OpLogVerify, nil, &out)
	return out, err
}

// Templates lists the builtin and custom scenario templates.
func (c *Client) Templates() ([]control.TemplateSummary, error) {
	var out control.TemplatesListResult
	err := c.Call(control.OpTemplatesList, nil, &out)
	return out.Templates, err
}

// Template reads one template with its YAML text.
func (c *Client) Template(name string) (control.TemplateDetail, error) {
	var out control.TemplateDetail
	err := c.Call(control.OpTemplatesShow, control.TemplateRef{Name: name}, &out)
	return out, err
}

// DeriveTemplate stores a new custom template name with the steps of the template from. It needs the admin
// session of this connection and never replaces an existing template.
func (c *Client) DeriveTemplate(p control.TemplateDeriveParams) (control.TemplateDetail, error) {
	var out control.TemplateDetail
	err := c.Call(control.OpTemplatesDerive, p, &out)
	return out, err
}

// DeployLog reads the log of one run of a project, cut to its end where it is long.
func (c *Client) DeployLog(project, run string) (control.DeployLogResult, error) {
	var out control.DeployLogResult
	err := c.Call(control.OpDeployLog, control.DeployLogParams{Project: project, Run: run}, &out)
	return out, err
}
