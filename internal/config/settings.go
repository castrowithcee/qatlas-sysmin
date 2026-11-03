// Package config reads and writes sysmin.yaml, the settings file of the service. Only the vault admin
// timeout is interpreted; every other key of an existing file is ignored when reading and kept as it is
// when the file is written, so installations whose file still carries keys of earlier versions keep working.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/castrowithcee/qatlas-sysmin/internal/filelock"
)

// SettingsFile is the name of the settings file in the configuration directory.
const SettingsFile = "sysmin.yaml"

// CurrentSchema is the schema version the file is written with and the only one read.
const CurrentSchema = 1

// DefaultAdminTimeout applies where admin_timeout is left out.
const DefaultAdminTimeout = 15 * time.Minute

// Permissions of what the store creates: the group sysmin may read the configuration.
const (
	dirMode  = 0o750
	fileMode = 0o640
	lockFile = ".lock"
)

// Problem codes.
const CodeInvalidDuration = "invalid-duration"

// Duration is a time.Duration written as text such as "30s" or "5m" in YAML.
type Duration time.Duration

// UnmarshalYAML reads a duration string.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: a duration must be a string such as \"30s\"", node.Line)
	}
	v, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q: use a form such as \"30s\" or \"5m\"", node.Line, node.Value)
	}
	*d = Duration(v)
	return nil
}

// MarshalYAML writes the shortest exact form; zero is written as a bare 0.
func (d Duration) MarshalYAML() (any, error) {
	v := time.Duration(d)
	switch {
	case v == 0:
		return 0, nil
	case v%time.Hour == 0:
		return fmt.Sprintf("%dh", v/time.Hour), nil
	case v%time.Minute == 0:
		return fmt.Sprintf("%dm", v/time.Minute), nil
	case v%time.Second == 0:
		return fmt.Sprintf("%ds", v/time.Second), nil
	}
	return v.String(), nil
}

// Settings is the part of sysmin.yaml the service interprets.
type Settings struct {
	Schema int           `yaml:"schema"`
	Vault  VaultSettings `yaml:"vault,omitempty"`
}

// VaultSettings configures the credential vault.
type VaultSettings struct {
	// AdminTimeout is a pointer so that an explicit 0 (every change asks for the passphrase) differs from
	// a missing field (the default).
	AdminTimeout *Duration `yaml:"admin_timeout,omitempty"`
}

// EffectiveAdminTimeout is the vault admin timeout with the built-in default applied; an explicit 0 stays 0.
func (s Settings) EffectiveAdminTimeout() time.Duration {
	if s.Vault.AdminTimeout == nil {
		return DefaultAdminTimeout
	}
	return time.Duration(*s.Vault.AdminTimeout)
}

// SchemaError reports a file whose schema version this package does not read.
type SchemaError struct {
	Got       int
	Supported int
}

func (e *SchemaError) Error() string {
	switch {
	case e.Got == 0:
		return fmt.Sprintf("missing schema version: add \"schema: %d\"", e.Supported)
	case e.Got > e.Supported:
		return fmt.Sprintf("schema version %d is newer than the supported version %d: upgrade sysmin", e.Got, e.Supported)
	}
	return fmt.Sprintf("schema version %d is not supported (supported: %d)", e.Got, e.Supported)
}

// DecodeSettings reads sysmin.yaml. The schema version is checked first; keys this package does not know
// are ignored.
func DecodeSettings(data []byte) (Settings, error) {
	var head struct {
		Schema int `yaml:"schema"`
	}
	if err := yaml.Unmarshal(data, &head); err != nil {
		return Settings{}, fmt.Errorf("invalid YAML: %w", err)
	}
	if head.Schema != CurrentSchema {
		return Settings{}, &SchemaError{Got: head.Schema, Supported: CurrentSchema}
	}
	var out Settings
	if err := yaml.Unmarshal(data, &out); err != nil {
		return Settings{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return out, nil
}

// ReadSettings reads only sysmin.yaml from the configuration directory dir. A missing file gives the
// default settings; any other read or decode error is returned.
func ReadSettings(dir string) (Settings, error) {
	data, err := os.ReadFile(filepath.Join(dir, SettingsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Settings{Schema: CurrentSchema}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("%s: %w", SettingsFile, err)
	}
	return DecodeSettings(data)
}

// Problem is one finding about a setting.
type Problem struct {
	Field   string
	Code    string
	Message string
	Hint    string
}

// ValidateAdminTimeout reports a problem when d is neither 0 nor within 1s to 24h.
func ValidateAdminTimeout(d time.Duration) []Problem {
	if d == 0 || (d >= time.Second && d <= 24*time.Hour) {
		return nil
	}
	return []Problem{{
		Field: "vault.admin_timeout", Code: CodeInvalidDuration,
		Message: fmt.Sprintf("duration %s is outside the allowed range %s to %s, or 0 for a passphrase on every change", d, time.Second, 24*time.Hour),
		Hint:    "Use a duration such as \"30s\" or \"5m\" within the allowed range.",
	}}
}

// Store writes the settings file of a configuration directory.
type Store struct {
	root string
}

// NewStore returns a store for the configuration directory at root. Nothing is created until a write.
func NewStore(root string) *Store { return &Store{root: root} }

// SetAdminTimeout sets vault.admin_timeout in sysmin.yaml under the cross-process lock and leaves every
// other key as it is. A missing file is created; an unreadable one or one of another schema is an error.
func (s *Store) SetAdminTimeout(d Duration) error {
	if err := os.MkdirAll(s.root, dirMode); err != nil {
		return fmt.Errorf("cannot create %s: %w", s.root, err)
	}
	lock, err := os.OpenFile(filepath.Join(s.root, lockFile), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, fileMode)
	if err != nil {
		return fmt.Errorf("cannot open the lock file: %w", err)
	}
	defer lock.Close()
	if err := filelock.Lock(lock); err != nil {
		return fmt.Errorf("cannot lock the configuration: %w", err)
	}
	defer func() { _ = filelock.Unlock(lock) }()

	target := filepath.Join(s.root, SettingsFile)
	data, err := os.ReadFile(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data = fmt.Appendf(nil, "schema: %d\n", CurrentSchema)
	case err != nil:
		return fmt.Errorf("cannot read %s: %w", SettingsFile, err)
	}
	if _, err := DecodeSettings(data); err != nil {
		return err
	}
	out, err := withAdminTimeout(data, d)
	if err != nil {
		return err
	}
	return replaceFile(target, out)
}

// withAdminTimeout returns data with vault.admin_timeout set, keeping all other content.
func withAdminTimeout(data []byte, d Duration) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the settings are not a YAML mapping")
	}
	var val yaml.Node
	if err := val.Encode(d); err != nil {
		return nil, err
	}
	vault := mapEntry(doc.Content[0], "vault")
	if vault.Kind != yaml.MappingNode {
		*vault = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	*mapEntry(vault, "admin_timeout") = val
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// mapEntry returns the value node of key in a mapping, appending an empty entry when it is missing.
func mapEntry(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	v := &yaml.Node{}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
	return v
}

// replaceFile atomically writes data to target: temporary file in the same directory, fsync, rename,
// directory fsync. A failure leaves no temporary file behind.
func replaceFile(target string, data []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".sysmin-tmp-*")
	if err != nil {
		return fmt.Errorf("cannot create a temporary file in %s: %w", dir, err)
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(fileMode)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, target)
	}
	if err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("cannot write %s: %w", SettingsFile, err)
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}
