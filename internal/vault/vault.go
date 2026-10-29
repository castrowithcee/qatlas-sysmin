// Package vault stores credentials on disk, always encrypted to a passphrase. There is no unencrypted mode
// and no way to remove the passphrase.
//
// A vault is one directory with three kinds of files: key.age, the vault's own X25519 private key,
// encrypted to the passphrase with age's scrypt recipient; recipient, the matching public key as age text;
// and secrets.age, the JSON document of all entries, encrypted to that public key. Because recipient is
// public, an entry can always be added without the passphrase: it is written to pending/<id>.age,
// encrypted to the same public key, and merged into secrets.age the next time the vault is opened with the
// key. Reading, changing, renaming and deleting entries need the private key.
//
// Every entry has a random id that never changes, whatever its name. The passphrase only wraps the private
// key, so changing it leaves the key, the recipient and every entry untouched.
//
// The vault directory is 0700 and every file in it 0600; a write replaces the target atomically through a
// temporary file in the same directory. A Vault is safe for concurrent use.
package vault

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// DefaultDir is where the service keeps its vault.
const DefaultDir = "/var/lib/sysmin/vault"

// File and directory modes. A vault holds secrets, so it is readable by its owner alone.
const (
	fileMode = 0o600
	dirMode  = 0o700
)

// schemaVersion is the version of the JSON document a vault holds.
const schemaVersion = 1

// Names of the files and the directory a vault is made of.
const (
	keyFile       = "key.age"
	recipientFile = "recipient"
	secretsFile   = "secrets.age"
	pendingDir    = "pending"
)

// State is what a vault currently is.
type State string

// The states a vault can be in.
const (
	// StateAbsent means the vault has not been created yet.
	StateAbsent State = "absent"
	// StateLocked means the vault exists and this Vault value does not hold its private key.
	StateLocked State = "locked"
	// StateUnlocked means this Vault value holds the private key and the decrypted document in memory.
	StateUnlocked State = "unlocked"
)

var (
	// ErrWrongPassphrase reports a passphrase that does not unlock the vault's key.
	ErrWrongPassphrase = errors.New("the passphrase does not unlock the vault")
	// ErrEmptyPassphrase reports a passphrase of "": a vault always has one.
	ErrEmptyPassphrase = errors.New("a passphrase must not be empty")
	// ErrExists reports an attempt to create a vault that already exists, or an entry name already taken.
	ErrExists = errors.New("already exists")
	// ErrAbsent reports an operation that needs a vault which has not been created.
	ErrAbsent = errors.New("the vault does not exist")
	// ErrLocked reports an operation that needs the vault's private key, which this value does not hold.
	ErrLocked = errors.New("the vault is locked")
	// ErrNotFound reports an entry id or name the vault does not hold.
	ErrNotFound = errors.New("no such vault entry")
	// ErrNoTerminal is returned when a passphrase cannot be asked for because no terminal is attached.
	ErrNoTerminal = errors.New("no terminal is attached to ask for the vault passphrase")
)

// Vault is one vault directory. The zero value is not usable; use New or OpenWithKey.
type Vault struct {
	dir string

	mu       sync.Mutex
	unlocked bool
	identity *identity
	doc      *document
}

// New returns the vault in dir, the vault directory itself. The vault does not have to exist.
func New(dir string) *Vault { return &Vault{dir: dir} }

// Dir returns the vault directory.
func (v *Vault) Dir() string { return v.dir }

func (v *Vault) keyPath() string       { return filepath.Join(v.dir, keyFile) }
func (v *Vault) recipientPath() string { return filepath.Join(v.dir, recipientFile) }
func (v *Vault) secretsPath() string   { return filepath.Join(v.dir, secretsFile) }
func (v *Vault) pendingPath() string   { return filepath.Join(v.dir, pendingDir) }

// State reports what the vault currently is. It reads no secret.
func (v *Vault) State() (State, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.state()
}

func (v *Vault) state() (State, error) {
	if v.unlocked {
		return StateUnlocked, nil
	}
	exists, err := fileExists(v.keyPath())
	if err != nil {
		return "", err
	}
	if exists {
		return StateLocked, nil
	}
	return StateAbsent, nil
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

func isNotExist(err error) bool { return errors.Is(err, os.ErrNotExist) }
