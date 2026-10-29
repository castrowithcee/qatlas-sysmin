package vault

// ChangePassphrase re-encrypts the vault's key with a new passphrase, verifying the old one first. Only
// key.age changes: the key pair, the recipient and every entry stay exactly as they were. old is verified
// fresh against key.age on every call, never against a cached passphrase.
func (v *Vault) ChangePassphrase(oldPassphrase, newPassphrase string) error {
	if newPassphrase == "" {
		return ErrEmptyPassphrase
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	if err := v.requireExists(); err != nil {
		return err
	}
	id, err := v.decryptIdentity(oldPassphrase)
	if err != nil {
		return err
	}
	keyData, err := encryptKey(id, newPassphrase)
	if err != nil {
		return err
	}
	return writeFile(v.keyPath(), keyData)
}

// VerifyPassphrase checks passphrase against the vault's key.age and recipient without any side effect:
// no document is read, no pending entry is merged, and a key held in memory is neither consulted nor
// replaced.
func (v *Vault) VerifyPassphrase(passphrase string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if err := v.requireExists(); err != nil {
		return err
	}
	_, err := v.decryptIdentity(passphrase)
	return err
}

// requireExists returns ErrAbsent unless the vault exists. The caller holds the mutex.
func (v *Vault) requireExists() error {
	state, err := v.state()
	if err != nil {
		return err
	}
	if state == StateAbsent {
		return ErrAbsent
	}
	return nil
}

// decryptIdentity reads key.age and recipient fresh from disk and verifies passphrase against them.
func (v *Vault) decryptIdentity(passphrase string) (*identity, error) {
	keyData, err := readFile(v.keyPath())
	if err != nil {
		return nil, err
	}
	recipientPEM, err := readFile(v.recipientPath())
	if err != nil {
		return nil, err
	}
	return decryptKey(keyData, passphrase, recipientPEM)
}
