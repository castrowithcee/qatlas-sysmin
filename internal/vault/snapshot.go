package vault

import (
	"bytes"
	"fmt"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
)

// LogKey returns the audit log key of an unlocked vault, derived from the vault's private key, so the same
// vault always yields the same log key, also after a passphrase change. The key bytes never leave the
// derivation; the caller clears the returned key. It fails with ErrLocked unless the key is held.
func (v *Vault) LogKey() (*auditlog.Key, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.identity == nil {
		return nil, ErrLocked
	}
	material := []byte(v.identity.key.String())
	defer clear(material)
	return auditlog.DeriveKey(material)
}

// Identity returns the vault's private key, for the binding that opens the vault without the passphrase
// (see OpenWithKey). It is as secret as every entry; its String method prints the key, so it must never
// be logged or formatted. It fails with ErrLocked unless the key is held.
func (v *Vault) Identity() (*age.X25519Identity, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.identity == nil {
		return nil, ErrLocked
	}
	return v.identity.key, nil
}

// Recipient returns the public key the vault encrypts to, as age text. It needs no passphrase.
func (v *Vault) Recipient() (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.identity != nil {
		return v.identity.recipient.String(), nil
	}
	data, err := readFile(v.recipientPath())
	if err != nil {
		if isNotExist(err) {
			return "", ErrAbsent
		}
		return "", fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
	}
	return string(bytes.TrimSpace(data)), nil
}

// OpenWithKey returns the vault in dir, unlocked with key, its own private key, without the passphrase. It
// reads secrets.age as it is on disk now and leaves every pending entry where it is: the next unlock merges
// them, and a write through the returned vault keeps them pending.
func OpenWithKey(dir string, key *age.X25519Identity) (*Vault, error) {
	if key == nil {
		return nil, ErrLocked
	}
	v := &Vault{dir: dir}
	recipientPEM, err := readFile(v.recipientPath())
	if err != nil {
		if isNotExist(err) {
			return nil, ErrAbsent
		}
		return nil, fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
	}
	recipient, err := age.ParseX25519Recipient(string(bytes.TrimSpace(recipientPEM)))
	if err != nil {
		return nil, fmt.Errorf("cannot read the vault recipient: %w", err)
	}
	if key.Recipient().String() != recipient.String() {
		return nil, fmt.Errorf("the vault key and the vault recipient do not match")
	}
	id := &identity{key: key, recipient: recipient}
	doc, err := v.loadDocument(id)
	if err != nil {
		return nil, err
	}
	v.identity, v.doc, v.unlocked = id, doc, true
	return v, nil
}
