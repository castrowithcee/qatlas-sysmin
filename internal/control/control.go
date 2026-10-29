// Package control defines the wire protocol of the service's control socket: one JSON object per line, a
// request from the client and an answer from the service. It holds the frame, the fixed error codes and the
// size and time limits, and nothing that serves or calls.
package control

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Version is the protocol version every request and answer carries.
const Version = 1

// DefaultSocketPath is where the service listens unless told otherwise.
const DefaultSocketPath = "/run/sysmin/sysmin.sock"

// The size and time limits of a connection. A request is small; an answer may carry a listing.
const (
	MaxRequestBytes  = 64 << 10
	MaxResponseBytes = 1 << 20

	// ReadTimeout bounds how long one request may take from its first byte to its end of line.
	ReadTimeout = 10 * time.Second
	// IdleTimeout bounds how long a connection may stay silent between requests.
	IdleTimeout = 5 * time.Minute
	// WriteTimeout bounds how long one answer may take to leave.
	WriteTimeout = 10 * time.Second
)

// The operations of the service.
const (
	// OpStatus asks for the service's versions, vault state and socket.
	OpStatus = "status"
	// OpVaultStatus asks for the vault's state and how it was unlocked.
	OpVaultStatus = "vault.status"
	// OpUnlock opens a locked vault with its passphrase.
	OpUnlock = "unlock"
	// OpSessionOpen and OpSessionEnd start and end the admin session of the connection.
	OpSessionOpen = "session.open"
	OpSessionEnd  = "session.end"

	// OpApprovalsList and OpApprovalsShow read the open approvals of projects. The others change the
	// approved state and need the admin session.
	OpApprovalsList       = "approvals.list"
	OpApprovalsShow       = "approvals.show"
	OpApprovalsApprove    = "approvals.approve"
	OpApprovalsApproveAll = "approvals.approve-all"
	OpApprovalsDiscard    = "approvals.discard"
	OpApprovalsDiscardAll = "approvals.discard-all"

	// The project operations. The first three read the files and need no admin session. The others write
	// a project file without one: the change is stored and stays an open approval, unless the connection
	// has an admin session, which the change then uses up and which approves it at once.
	OpProjectsList   = "projects.list"
	OpProjectsShow   = "projects.show"
	OpProjectsCheck  = "projects.check"
	OpProjectsAdd    = "projects.add"
	OpProjectsEdit   = "projects.edit"
	OpProjectsRemove = "projects.remove"

	// The credential operations. list reads without an admin session. set and remove change the vault
	// without one as well: a credential that nothing uses is stored or removed at once, and what uses one
	// stays an open approval, unless the connection has an admin session, which the change uses up.
	OpCredentialsList   = "credentials.list"
	OpCredentialsSet    = "credentials.set"
	OpCredentialsRemove = "credentials.remove"

	// The channel operations follow the rules of the project operations; the channels are approved as one
	// unit.
	OpChannelsList   = "channels.list"
	OpChannelsAdd    = "channels.add"
	OpChannelsRemove = "channels.remove"

	// OpSourceTest reads the versions of the sources of a project's approved state from the registries and
	// repositories it names. It changes nothing and needs no admin session.
	OpSourceTest = "source.test"

	// OpProjectsChangelog reads the GitHub releases between the current and the target version of the sources
	// of a project's approved state. It changes nothing and needs no admin session.
	OpProjectsChangelog = "projects.changelog"

	// OpNotifyTest sends a test message to one approved channel. It reads and changes nothing, so it needs
	// no admin session.
	OpNotifyTest = "notify.test"
)

// The codes an answer's error carries. They are fixed words, never text built from a request, so an answer
// can neither carry a secret nor echo what a peer sent.
const (
	CodeRefused    = "refused"
	CodeBadRequest = "bad-request"
	CodeVersion    = "version"
	CodeUnknownOp  = "unknown-operation"
	CodeTooLarge   = "too-large"
	CodeTimeout    = "timeout"
	CodeBusy       = "busy"
	// CodeUnavailable answers an operation the service cannot serve now, such as a changing one while no
	// check for it is installed.
	CodeUnavailable = "unavailable"
	CodeInternal    = "internal"
	// CodeLocked answers a changing request while the vault is locked or does not exist.
	CodeLocked = "locked"
	// CodeAdminRequired answers a changing request of a connection without an active admin session.
	CodeAdminRequired   = "admin-required"
	CodeWrongPassphrase = "wrong-passphrase"
	CodeVaultAbsent     = "vault-absent"
	// CodeNothingOpen answers an approval request for a project that has no open approval.
	CodeNothingOpen = "nothing-open"
	// CodeChanged answers an approval request whose project differs from what the client was shown.
	CodeChanged = "changed"
	// CodeCannotDiscard answers a discard of an open approval that restoring the project file cannot undo.
	CodeCannotDiscard = "cannot-discard"
	// CodeInvalid answers a change or check whose result fails the validation. The answer lists the
	// problems.
	CodeInvalid = "invalid"
	// CodeNotFound and CodeExists answer a project operation for a project that is missing or already there.
	CodeNotFound = "not-found"
	CodeExists   = "exists"
	// CodeInUse answers the removal of a credential that a project or channel names, without an admin
	// session. The answer lists the uses as problems.
	CodeInUse = "in-use"
	// CodeNotApproved answers a request that needs the approved state of a project that has none.
	CodeNotApproved = "not-approved"
	// CodeKindMismatch answers a credential set whose kind differs from the stored credential of that name.
	CodeKindMismatch = "kind-mismatch"
	// CodeSendFailed answers a test message that the channel did not take. The reason is in the audit log
	// of the service, not in the answer.
	CodeSendFailed = "send-failed"
)

// Request is one line a client sends. Params is the operation's own object, if it takes one.
type Request struct {
	V      int             `json:"v"`
	Op     string          `json:"op"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is one line the service answers with: a result, or a fixed error code and nothing else.
type Response struct {
	V      int             `json:"v"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	// Problems accompanies the error CodeInvalid.
	Problems []Problem `json:"problems,omitempty"`
}

// The vault states and unlock modes an answer reports.
const (
	VaultAbsent   = "absent"
	VaultLocked   = "locked"
	VaultUnlocked = "unlocked"

	UnlockManual = "manual"
	UnlockTPM2   = "tpm2"
	UnlockHost   = "host"
)

// StatusResult is the answer to OpStatus. The fields after Version are empty for a server without a vault.
type StatusResult struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	Vault    string `json:"vault,omitempty"`
	Unlock   string `json:"unlock,omitempty"`
	Warning  string `json:"warning,omitempty"`
	Socket   string `json:"socket,omitempty"`
}

// VaultStatusResult is the answer to OpVaultStatus. It never carries key material or entry values.
type VaultStatusResult struct {
	Vault   string `json:"vault"`
	Unlock  string `json:"unlock"`
	Warning string `json:"warning,omitempty"`
}

// PassphraseParams are the parameters of OpUnlock and OpSessionOpen. The passphrase travels only in the
// request; no answer, log entry or error repeats it.
type PassphraseParams struct {
	Passphrase string `json:"passphrase"`
}

// ApprovalParams are the parameters of OpApprovalsShow, OpApprovalsApprove and OpApprovalsDiscard.
// Fingerprint is the one an earlier list or show reported; approving it approves exactly that state. It may
// be empty only for a project whose file is gone.
type ApprovalParams struct {
	Project     string `json:"project"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// ApprovalSummary is one open approval as OpApprovalsList reports it.
type ApprovalSummary struct {
	Project string `json:"project"`
	// Kind is new, changed, deleted or credential.
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Changes     int    `json:"changes"`
	// FreeCommands is set when a free command line was added, changed or removed.
	FreeCommands bool `json:"free_commands,omitempty"`
	// Unverified means the approved state could not be checked, so the changes list the whole project.
	Unverified bool `json:"unverified,omitempty"`
}

// ApprovalsListResult is the answer to OpApprovalsList.
type ApprovalsListResult struct {
	Approvals []ApprovalSummary `json:"approvals"`
}

// ApprovalChange is one differing field of an open approval. Old and New are empty when the field is absent
// on that side; Free marks a free command line.
type ApprovalChange struct {
	Path string `json:"path"`
	Old  string `json:"old,omitempty"`
	New  string `json:"new,omitempty"`
	Free bool   `json:"free,omitempty"`
}

// ApprovalDetail is the answer to OpApprovalsShow.
type ApprovalDetail struct {
	ApprovalSummary
	Details []ApprovalChange `json:"details"`
}

// ApprovalFailure is a project an all-at-once operation could not handle, with the fixed code why.
type ApprovalFailure struct {
	Project string `json:"project"`
	Code    string `json:"code"`
}

// ApprovalsAllResult is the answer to OpApprovalsApproveAll and OpApprovalsDiscardAll.
type ApprovalsAllResult struct {
	Done   []string          `json:"done"`
	Failed []ApprovalFailure `json:"failed,omitempty"`
}

// Error is a failed operation, carrying one of the fixed codes. Problems is set only with CodeInvalid.
type Error struct {
	Code     string
	Problems []Problem
}

func (e *Error) Error() string { return "control: " + e.Code }

// ErrLineTooLong reports a line that exceeds the limit given to ReadLine.
var ErrLineTooLong = errors.New("line exceeds the size limit")

// ReadLine reads one line without its end of line, at most max bytes long. It returns io.EOF when the stream
// ends before any byte and io.ErrUnexpectedEOF when it ends inside a line. After ErrLineTooLong the stream
// is not usable.
func ReadLine(r *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if n := len(line); (err == nil && n-1 > max) || (err != nil && n > max) {
			return nil, ErrLineTooLong
		}
		switch {
		case err == nil:
			return line[:len(line)-1], nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) > 0:
			return nil, io.ErrUnexpectedEOF
		default:
			return nil, err
		}
	}
}

// WriteLine writes v as one JSON line in a single write.
func WriteLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("cannot encode the message: %w", err)
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
