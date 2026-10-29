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

// Error is a failed operation, carrying one of the fixed codes.
type Error struct{ Code string }

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
