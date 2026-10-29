package autounlock

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// credentialsEnv names the variable through which the service manager passes the credentials directory.
const credentialsEnv = "CREDENTIALS_DIRECTORY"

// maxKeySize bounds how much of the credential is read; an age secret key is under 100 bytes.
const maxKeySize = 4096

const noCredentialMsg = "no vault key credential is present; the vault stays locked until the passphrase is entered"

// Status is what the service knows about how its vault was unlocked. It never carries key material.
type Status struct {
	// Unlock is the mode that opened the vault, or ModeManual when it is still locked.
	Unlock Mode
	// Message says in one line why the vault stays locked; it is empty when it was opened automatically.
	Message string
}

// Warning returns the caution that belongs to the status, or "" for manual unlocking.
func (s Status) Warning() string { return s.Unlock.Warning() }

// LoadFromEnv is the thin shell around Load: it reads CREDENTIALS_DIRECTORY from the environment.
func LoadFromEnv(vaultDir string) (*vault.Vault, Status) {
	return Load(vaultDir, os.Getenv(credentialsEnv))
}

// Load opens the vault in vaultDir with the credential vault-key from credentialsDir. A missing or empty
// credential means no binding; it, an invalid credential, or one that does not belong to the vault leaves the
// vault locked: the returned vault is then nil and the status says why, without any key material.
func Load(vaultDir, credentialsDir string) (*vault.Vault, Status) {
	locked := func(msg string) (*vault.Vault, Status) {
		return nil, Status{Unlock: ModeManual, Message: msg}
	}
	if credentialsDir == "" {
		return locked("no credentials directory is set; the vault stays locked until the passphrase is entered")
	}
	data, err := readKey(filepath.Join(credentialsDir, credentialName))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return locked(noCredentialMsg)
	case err != nil:
		return locked("the vault key credential cannot be read; the vault stays locked")
	}
	defer clear(data)
	key := string(bytes.TrimSpace(data))
	if key == "" {
		// Without a binding the unit hands over an empty default credential.
		return locked(noCredentialMsg)
	}
	id, err := age.ParseX25519Identity(key)
	if err != nil {
		return locked("the vault key credential is not a valid key; the vault stays locked")
	}
	v, err := vault.OpenWithKey(vaultDir, id)
	switch {
	case errors.Is(err, vault.ErrAbsent):
		return locked("the vault does not exist; nothing to unlock")
	case err != nil:
		return locked("the vault key credential does not open this vault; the vault stays locked")
	}
	return v, Status{Unlock: recordedMode(vaultDir)}
}

func readKey(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxKeySize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxKeySize {
		clear(data)
		return nil, fmt.Errorf("credential too large")
	}
	return data, nil
}
