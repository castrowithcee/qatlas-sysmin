package vault

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// record is an entry as the document stores it. The id is random and fixed for the life of the entry.
type record struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Kind     Kind              `json:"kind"`
	Fields   map[string]string `json:"fields"`
	Created  time.Time         `json:"created"`
	Modified time.Time         `json:"modified"`
}

func (r record) entry() Entry {
	fields := make(map[string]Secret, len(r.Fields))
	for name, value := range r.Fields {
		fields[name] = NewSecret(value)
	}
	return Entry{ID: r.ID, Name: r.Name, Kind: r.Kind, Fields: fields, Created: r.Created, Modified: r.Modified}
}

func (r record) info() Info {
	return Info{ID: r.ID, Name: r.Name, Kind: r.Kind, Created: r.Created, Modified: r.Modified}
}

// document is the JSON shape held, encrypted, by secrets.age.
type document struct {
	Schema  int               `json:"schema"`
	Entries map[string]record `json:"entries"`
	// Approvals maps a project name to the fingerprint of its approved state.
	Approvals map[string]string `json:"approvals,omitempty"`
}

func newDocument() *document {
	return &document{Schema: schemaVersion, Entries: map[string]record{}}
}

func (d *document) clone() *document {
	c := &document{Schema: d.Schema, Entries: make(map[string]record, len(d.Entries))}
	for id, r := range d.Entries {
		r.Fields = cloneFields(r.Fields)
		c.Entries[id] = r
	}
	if len(d.Approvals) > 0 {
		c.Approvals = make(map[string]string, len(d.Approvals))
		for k, v := range d.Approvals {
			c.Approvals[k] = v
		}
	}
	return c
}

func cloneFields(fields map[string]string) map[string]string {
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	return out
}

// byName returns the record named name and whether it exists.
func (d *document) byName(name string) (record, bool) {
	for _, r := range d.Entries {
		if r.Name == name {
			return r, true
		}
	}
	return record{}, false
}

// validateName refuses an empty or unprintable entry name. Names are not secret, so they may appear in
// messages.
func validateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("an entry name must not be empty")
	}
	if len(name) > 128 {
		return errors.New("an entry name must not be longer than 128 bytes")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("an entry name must not contain control characters")
		}
	}
	return nil
}

// validateFields checks that fields are exactly the fields kind requires, none of them empty. The message
// names fields, never values.
func validateFields(kind Kind, fields map[string]string) error {
	want, ok := kindFields[kind]
	if !ok {
		return fmt.Errorf("unknown credential kind %q", kind)
	}
	for name, value := range fields {
		if !slices.Contains(want, name) {
			return fmt.Errorf("a %s credential has no field %q", kind, name)
		}
		if value == "" {
			return fmt.Errorf("the field %q of a %s credential must not be empty", name, kind)
		}
	}
	for _, name := range want {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("a %s credential needs the field %q", kind, name)
		}
	}
	return nil
}

// newID returns a random entry id: 16 bytes as hex, so it is safe in a pending file name.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read only fails when the OS random source is unusable; nothing safe is left to do.
		panic("vault: cannot read random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// isID reports whether s has the shape newID produces.
func isID(s string) bool {
	if len(s) != 32 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// PermissionError reports a vault file that other users can read or write; it is refused rather than read.
type PermissionError struct {
	Path string
	Mode fs.FileMode
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("%s holds vault data but its mode is %04o; it is not read until it is private again: "+
		"chmod 600 %s", e.Path, e.Mode.Perm(), e.Path)
}

// readFile reads path after checking that others cannot access it.
func readFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, &PermissionError{Path: path, Mode: perm}
	}
	return os.ReadFile(path)
}

// writeFile replaces path atomically with mode 0600, creating its directory at 0700 if needed.
func writeFile(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".sysmin-vault-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write next to %s: %w", target, err)
	}
	name := tmp.Name()
	moved := false
	defer func() {
		if !moved {
			_ = os.Remove(name)
		}
	}()

	if err := os.Chmod(name, fileMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot set the permissions of %s: %w", name, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	if err := os.Rename(name, target); err != nil {
		return fmt.Errorf("cannot replace %s: %w", target, err)
	}
	moved = true
	return nil
}

func decodeDocument(path string, data []byte) (*document, error) {
	var d document
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("cannot read %s: not a valid vault document", path)
	}
	// A schema this build does not know is refused, never read or overwritten with the current schema.
	if d.Schema != schemaVersion {
		return nil, fmt.Errorf("cannot read %s: unsupported vault schema version %d, want %d",
			path, d.Schema, schemaVersion)
	}
	if d.Entries == nil {
		d.Entries = map[string]record{}
	}
	return &d, nil
}

func encodeDocument(d *document) ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}
