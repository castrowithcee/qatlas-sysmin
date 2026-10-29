package vault

import (
	"fmt"
	"os"
	"sort"
	"time"
)

// Create makes a new vault in the vault directory: a fresh key pair, the private key wrapped with
// passphrase, and an empty document. The vault is unlocked afterwards. An empty passphrase is refused and
// a vault that exists is never replaced.
func (v *Vault) Create(passphrase string) error {
	if passphrase == "" {
		return ErrEmptyPassphrase
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	state, err := v.state()
	if err != nil {
		return err
	}
	if state != StateAbsent {
		return fmt.Errorf("the vault %w", ErrExists)
	}

	id, err := generateIdentity()
	if err != nil {
		return err
	}
	keyData, err := encryptKey(id, passphrase)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(v.dir, dirMode); err != nil {
		return fmt.Errorf("cannot create %s: %w", v.dir, err)
	}
	if err := os.Chmod(v.dir, dirMode); err != nil {
		return fmt.Errorf("cannot set the permissions of %s: %w", v.dir, err)
	}
	doc := newDocument()
	if err := writeFile(v.recipientPath(), []byte(id.recipient.String()+"\n")); err != nil {
		return err
	}
	if err := v.writeSecrets(id, doc); err != nil {
		return err
	}
	// key.age is written last: it is what makes the vault exist, so a failed Create leaves an absent vault.
	if err := writeFile(v.keyPath(), keyData); err != nil {
		return err
	}
	v.identity, v.doc, v.unlocked = id, doc, true
	return nil
}

// Unlock opens a locked vault with passphrase, merges every pending entry into it, and holds the key and
// the document in memory until Forget. It reports how many pending entries were merged. A vault that is
// already unlocked is left as it is.
func (v *Vault) Unlock(passphrase string) (merged int, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	state, err := v.state()
	if err != nil {
		return 0, err
	}
	switch state {
	case StateAbsent:
		return 0, ErrAbsent
	case StateUnlocked:
		return 0, nil
	}
	keyData, err := readFile(v.keyPath())
	if err != nil {
		return 0, fmt.Errorf("cannot read %s: %w", v.keyPath(), err)
	}
	recipientPEM, err := readFile(v.recipientPath())
	if err != nil {
		return 0, fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
	}
	id, err := decryptKey(keyData, passphrase, recipientPEM)
	if err != nil {
		return 0, err
	}
	doc, err := v.loadDocument(id)
	if err != nil {
		return 0, err
	}
	merged, err = v.mergeAndStore(id, doc)
	if err != nil {
		return 0, err
	}
	v.identity, v.doc, v.unlocked = id, doc, true
	return merged, nil
}

// loadDocument decrypts secrets.age; a missing file is an empty document.
func (v *Vault) loadDocument(id *identity) (*document, error) {
	data, err := readFile(v.secretsPath())
	if err != nil {
		if isNotExist(err) {
			return newDocument(), nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", v.secretsPath(), err)
	}
	plain, err := decryptFrom(data, id.key)
	if err != nil {
		return nil, fmt.Errorf("cannot decrypt %s: %w", v.secretsPath(), err)
	}
	defer clear(plain)
	return decodeDocument(v.secretsPath(), plain)
}

// Forget drops the key and document this value holds in memory, so the vault is locked again. It touches
// no file.
func (v *Vault) Forget() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.unlocked, v.identity, v.doc = false, nil, nil
}

// requireUnlocked returns ErrAbsent or ErrLocked unless the key is held. The caller holds the mutex.
func (v *Vault) requireUnlocked() error {
	state, err := v.state()
	if err != nil {
		return err
	}
	switch state {
	case StateAbsent:
		return ErrAbsent
	case StateLocked:
		return ErrLocked
	}
	return nil
}

// mutate applies change to a copy of the document and keeps the copy only once it was written.
func (v *Vault) mutate(change func(*document) error) error {
	next := v.doc.clone()
	if err := change(next); err != nil {
		return err
	}
	if err := v.writeSecrets(v.identity, next); err != nil {
		return err
	}
	v.doc = next
	return nil
}

func (v *Vault) writeSecrets(id *identity, doc *document) error {
	plain, err := encodeDocument(doc)
	if err != nil {
		return fmt.Errorf("cannot encode %s: %w", v.secretsPath(), err)
	}
	defer clear(plain)
	data, err := encryptTo(string(plain), id.recipient)
	if err != nil {
		return err
	}
	return writeFile(v.secretsPath(), data)
}

// Add stores a new entry and returns its id, which never changes afterwards. fields must be exactly the
// fields kind requires. An unlocked vault stores the entry in secrets.age and refuses a name that is
// taken. A locked vault needs no passphrase: the entry is queued as a pending file encrypted to the public
// recipient and becomes readable once the vault is unlocked and the entry merged; a name that is taken by
// then keeps the entry pending.
func (v *Vault) Add(name string, kind Kind, fields map[string]string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	if err := validateFields(kind, fields); err != nil {
		return "", err
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	state, err := v.state()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	r := record{ID: newID(), Name: name, Kind: kind, Fields: cloneFields(fields), Created: now, Modified: now}
	switch state {
	case StateAbsent:
		return "", ErrAbsent
	case StateLocked:
		if _, err := v.addPending(r); err != nil {
			return "", err
		}
		return r.ID, nil
	}
	err = v.mutate(func(d *document) error {
		if _, taken := d.byName(name); taken {
			return fmt.Errorf("an entry with this name %w", ErrExists)
		}
		d.Entries[r.ID] = r
		return nil
	})
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

// Get returns the entry with the given id.
func (v *Vault) Get(id string) (Entry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return Entry{}, err
	}
	r, ok := v.doc.Entries[id]
	if !ok {
		return Entry{}, ErrNotFound
	}
	return r.entry(), nil
}

// GetByName returns the entry with the given name.
func (v *Vault) GetByName(name string) (Entry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return Entry{}, err
	}
	r, ok := v.doc.byName(name)
	if !ok {
		return Entry{}, ErrNotFound
	}
	return r.entry(), nil
}

// List returns every entry without its values, sorted by name.
func (v *Vault) List() ([]Info, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return nil, err
	}
	infos := make([]Info, 0, len(v.doc.Entries))
	for _, r := range v.doc.Entries {
		infos = append(infos, r.info())
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// Set replaces the fields of the entry with the given id. fields must be exactly the fields the entry's
// kind requires; kind and id do not change.
func (v *Vault) Set(id string, fields map[string]string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return err
	}
	return v.mutate(func(d *document) error {
		r, ok := d.Entries[id]
		if !ok {
			return ErrNotFound
		}
		if err := validateFields(r.Kind, fields); err != nil {
			return err
		}
		r.Fields = cloneFields(fields)
		r.Modified = time.Now().UTC()
		d.Entries[id] = r
		return nil
	})
}

// Rename gives the entry with the given id a new name. The id stays the same.
func (v *Vault) Rename(id, newName string) error {
	if err := validateName(newName); err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return err
	}
	return v.mutate(func(d *document) error {
		r, ok := d.Entries[id]
		if !ok {
			return ErrNotFound
		}
		if other, taken := d.byName(newName); taken && other.ID != id {
			return fmt.Errorf("an entry with this name %w", ErrExists)
		}
		r.Name = newName
		r.Modified = time.Now().UTC()
		d.Entries[id] = r
		return nil
	})
}

// Delete removes the entry with the given id.
func (v *Vault) Delete(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if err := v.requireUnlocked(); err != nil {
		return err
	}
	return v.mutate(func(d *document) error {
		if _, ok := d.Entries[id]; !ok {
			return ErrNotFound
		}
		delete(d.Entries, id)
		return nil
	})
}
