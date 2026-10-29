package autounlock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// Files next to the vault. The marker only names the mode; it holds no secret.
const (
	credentialFile = "key.cred"
	markerFile     = "key.cred.mode"
	credentialName = "vault-key"
)

// ErrNotRoot reports a binding step that needs root.
var ErrNotRoot = errors.New("this command must run as root, for example with sudo")

// Binder binds and unbinds the vault key. Runner, IsRoot and Passphrase are replaceable so that tests never
// reach the host's systemd tools or a terminal.
type Binder struct {
	// Dir is the vault directory.
	Dir    string
	Runner Runner
	// IsRoot reports whether the process may bind; checked before anything is asked or run.
	IsRoot func() bool
	// Passphrase asks for the vault passphrase; in is the process's standard input.
	Passphrase func(in io.Reader) (string, error)

	// chown replaces os.Lchown in tests.
	chown func(path string, uid, gid int) error
}

// DefaultVaultDir is the vault directory a binder works on unless told otherwise.
const DefaultVaultDir = vault.DefaultDir

// NewBinder returns a Binder for the vault in dir with the real runner, the real root check and the real
// passphrase prompt.
func NewBinder(dir string) *Binder {
	return &Binder{Dir: dir, Runner: ExecRunner(), IsRoot: func() bool { return os.Geteuid() == 0 },
		Passphrase: PromptPassphrase}
}

// PromptPassphrase asks for the passphrase on the terminal without echo, or reads one line from in when no
// terminal is attached. It is never taken from arguments or the environment.
func PromptPassphrase(in io.Reader) (string, error) {
	if vault.Interactive() {
		return vault.ReadPassphrase("Vault passphrase: ")
	}
	return vault.ReadPassphraseFrom(in)
}

// CredentialPath is where the sealed key copy lives.
func CredentialPath(dir string) string { return filepath.Join(dir, credentialFile) }

func markerPath(dir string) string { return filepath.Join(dir, markerFile) }

// Bind unlocks the vault with the passphrase from in, seals a copy of its private key to this machine and
// records which mode bound it. Binding again replaces the earlier binding.
func (b *Binder) Bind(ctx context.Context, in io.Reader) (Mode, error) {
	if !b.IsRoot() {
		return "", ErrNotRoot
	}
	passphrase, err := b.Passphrase(in)
	if err != nil {
		return "", err
	}
	// Binding runs as root, so it must not write anything into the vault but the two files below: a file
	// root rewrote would no longer be readable by the service user.
	id, err := vault.New(b.Dir).UnwrapIdentity(passphrase)
	if err != nil {
		return "", fmt.Errorf("unlock the vault: %w", err)
	}
	// The string copy of the key cannot be cleared; the byte copy is.
	key := []byte(id.String())
	defer clear(key)

	mode := ModeHost
	// has-tpm2 exits non-zero unless the TPM2 is fully usable, so an error just means "not TPM2".
	if out, err := b.Runner.Run(ctx, analyzeProgram, []string{"has-tpm2"}, nil); err == nil &&
		strings.TrimSpace(string(out)) == "yes" {
		mode = ModeTPM2
	}

	path := CredentialPath(b.Dir)
	if err := removeIfExists(markerPath(b.Dir)); err != nil {
		return "", err
	}
	if err := removeIfExists(path); err != nil {
		return "", err
	}
	args := []string{"encrypt", "--with-key=auto", "--name=" + credentialName, "-", path}
	if _, err := b.Runner.Run(ctx, credsProgram, args, key); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("seal the vault key: %w", err)
	}
	undo := func() {
		_ = os.Remove(path)
		_ = os.Remove(markerPath(b.Dir))
	}
	if err := os.Chmod(path, 0o600); err != nil {
		undo()
		return "", fmt.Errorf("seal the vault key: %w", err)
	}
	if err := os.WriteFile(markerPath(b.Dir), []byte(mode+"\n"), 0o600); err != nil {
		undo()
		return "", fmt.Errorf("record the unlock mode: %w", err)
	}
	// The service user owns the vault directory and has to read both files.
	if err := b.handOver(path, markerPath(b.Dir)); err != nil {
		undo()
		return "", fmt.Errorf("hand the binding to the vault owner: %w", err)
	}
	return mode, nil
}

// handOver gives paths the owner and group of the vault directory.
func (b *Binder) handOver(paths ...string) error {
	info, err := os.Stat(b.Dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("cannot read the vault directory owner")
	}
	chown := b.chown
	if chown == nil {
		chown = os.Lchown
	}
	for _, p := range paths {
		if err := chown(p, int(st.Uid), int(st.Gid)); err != nil {
			return err
		}
	}
	return nil
}

// Unbind removes the sealed key copy and the marker and reports whether a binding existed.
func (b *Binder) Unbind() (removed bool, err error) {
	if !b.IsRoot() {
		return false, ErrNotRoot
	}
	for _, p := range []string{CredentialPath(b.Dir), markerPath(b.Dir)} {
		if _, statErr := os.Lstat(p); statErr == nil {
			removed = true
		}
		if err := removeIfExists(p); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// recordedMode reads the marker; a missing or unreadable marker counts as the weaker host mode, because
// a credential exists whose origin is unknown.
func recordedMode(dir string) Mode {
	data, err := os.ReadFile(markerPath(dir))
	if m := Mode(strings.TrimSpace(string(data))); err == nil && m.Automatic() {
		return m
	}
	return ModeHost
}
