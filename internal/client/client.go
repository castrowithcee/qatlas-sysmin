// Package client is how the command line and the terminal interface talk to the service: over its control
// socket and nowhere else.
package client

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
		return nil, fmt.Errorf("cannot reach the service: %w", err)
	}
	return &Client{conn: conn, br: bufio.NewReader(conn)}, nil
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
	if op == control.OpSourceTest {
		timeout = control.SourceTestTimeout + CallTimeout
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
		return &control.Error{Code: resp.Error, Problems: resp.Problems}
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
