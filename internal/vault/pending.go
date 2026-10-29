package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// pendingFiles returns the *.age file names under pending, sorted, so a merge is deterministic.
func (v *Vault) pendingFiles() ([]string, error) {
	entries, err := os.ReadDir(v.pendingPath())
	if err != nil {
		if isNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read %s: %w", v.pendingPath(), err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".age") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// addPending writes r as a pending entry encrypted to the public recipient alone, so it needs no
// passphrase. The file is named after the entry's id.
func (v *Vault) addPending(r record) (string, error) {
	recipientPEM, err := readFile(v.recipientPath())
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", v.recipientPath(), err)
	}
	plain, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("cannot encode the pending entry: %w", err)
	}
	defer clear(plain)
	data, err := encryptToRecipient(string(plain), recipientPEM)
	if err != nil {
		return "", err
	}
	path := filepath.Join(v.pendingPath(), r.ID+".age")
	return path, writeFile(path, data)
}

// readPending decrypts the pending file name into its record.
func (v *Vault) readPending(id *identity, name string) (record, error) {
	path := filepath.Join(v.pendingPath(), name)
	data, err := readFile(path)
	if err != nil {
		return record{}, fmt.Errorf("cannot read %s: %w", path, err)
	}
	plain, err := decryptFrom(data, id.key)
	if err != nil {
		return record{}, fmt.Errorf("cannot decrypt %s: %w", path, err)
	}
	defer clear(plain)
	var r record
	if err := json.Unmarshal(plain, &r); err != nil || !isID(r.ID) || r.Fields == nil {
		return record{}, fmt.Errorf("%s does not hold a valid pending entry", path)
	}
	if validateName(r.Name) != nil || validateFields(r.Kind, r.Fields) != nil {
		return record{}, fmt.Errorf("%s does not hold a valid pending entry", path)
	}
	return r, nil
}

// mergeAndStore merges every pending file into doc and, when at least one entry was merged, writes
// secrets.age and removes the merged files. A pending entry whose name another entry already holds stays
// pending. A pending file with the id of an existing entry replaces it, which makes a repeated merge
// harmless. It returns how many entries were merged.
func (v *Vault) mergeAndStore(id *identity, doc *document) (int, error) {
	names, err := v.pendingFiles()
	if err != nil {
		return 0, err
	}
	var merged []string
	for _, name := range names {
		r, err := v.readPending(id, name)
		if err != nil {
			return 0, err
		}
		if other, taken := doc.byName(r.Name); taken && other.ID != r.ID {
			continue
		}
		doc.Entries[r.ID] = r
		merged = append(merged, name)
	}
	if len(merged) == 0 {
		return 0, nil
	}
	if err := v.writeSecrets(id, doc); err != nil {
		return 0, err
	}
	// A leftover file merges again next time as a no-op, so a failed removal loses nothing.
	for _, name := range merged {
		_ = os.Remove(filepath.Join(v.pendingPath(), name))
	}
	return len(merged), nil
}
