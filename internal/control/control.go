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

// ServiceGroup is the group whose members may use the control socket, besides root.
const ServiceGroup = "sysmin"

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

	// OpHostRecord records a finished host change in the audit log. Only root may call it, and it needs
	// neither an open vault nor an admin session: the sudo call that made the change is the proof.
	OpHostRecord = "host.record"
)

// The codes an answer's error carries. They are fixed words, never text built from a request, so an answer
// can neither carry a secret nor echo what a peer sent.
const (
	CodeRefused = "refused"
	// CodeNotAdmin answers a peer that may reach the socket but is not an administrator, or no longer is.
	CodeNotAdmin = "not-admin"
	// CodeNotRoot answers a peer that may reach the socket but called an operation only root may call.
	CodeNotRoot    = "not-root"
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
	// CodeChanged answers a settings change whose file was changed by someone else meanwhile.
	CodeChanged = "changed"
	// CodeInvalid answers a change or check whose result fails the validation. The answer lists the
	// problems.
	CodeInvalid = "invalid"
	// CodeExists answers the creation of a vault that already exists.
	CodeExists = "exists"
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
	// ConfigDir is the service's configuration directory.
	ConfigDir string `json:"config_dir,omitempty"`
	// Admin reports whether the asking connection has an admin session that covers a change now, without
	// using it. AdminRemaining is the whole seconds (rounded up) it stays active while idle; it is 0 while
	// the admin timeout is 0 (the passphrase covers one change). AdminTimeout is the effective admin timeout
	// as a duration text such as "15m0s"; "0s" means every change asks for the passphrase.
	Admin          bool   `json:"admin,omitempty"`
	AdminRemaining int64  `json:"admin_remaining,omitempty"`
	AdminTimeout   string `json:"admin_timeout,omitempty"`
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

// The results of a host change that OpHostRecord takes: the change stands, the change failed, and the
// change failed and the previous state is back.
const (
	HostResultApplied  = "applied"
	HostResultFailed   = "failed"
	HostResultRestored = "restored"
)

// HostRecordParams are the parameters of OpHostRecord. SudoUID is the human behind the sudo call, 0 when
// unknown. The parameters carry no sentence, command, argument or secret.
type HostRecordParams struct {
	Action    string `json:"action"`
	Result    string `json:"result"`
	SudoUID   uint32 `json:"sudo_uid"`
	JournalID string `json:"journal_id"`
	UndoOf    string `json:"undo_of,omitempty"`
}
