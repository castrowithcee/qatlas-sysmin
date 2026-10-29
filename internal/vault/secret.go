package vault

import (
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// Kind is the type of credential an entry holds.
type Kind string

// The credential kinds.
const (
	KindRegistry Kind = "registry"
	KindGitHTTPS Kind = "git-https"
	KindSSHKey   Kind = "ssh-key"
	KindSMTP     Kind = "smtp"
	KindSecret   Kind = "secret"
)

// Field names of the credential kinds.
const (
	FieldUser       = "user"
	FieldToken      = "token"
	FieldPassword   = "password"
	FieldPrivateKey = "private_key"
	FieldHostKey    = "host_key"
	FieldValue      = "value"
)

// kindFields lists the fields each kind holds. Every one of them is required.
var kindFields = map[Kind][]string{
	KindRegistry: {FieldUser, FieldToken},
	KindGitHTTPS: {FieldUser, FieldToken},
	KindSSHKey:   {FieldPrivateKey, FieldHostKey},
	KindSMTP:     {FieldUser, FieldPassword},
	KindSecret:   {FieldValue},
}

// Kinds returns every credential kind, sorted.
func Kinds() []Kind {
	kinds := make([]Kind, 0, len(kindFields))
	for k := range kindFields {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return kinds
}

// FieldsOf returns the field names kind holds, or nil for an unknown kind.
func FieldsOf(kind Kind) []string { return append([]string(nil), kindFields[kind]...) }

// redacted is what a Secret prints as.
const redacted = "[redacted]"

// Secret is one field value of an entry. It prints as a marker through every fmt verb, JSON, text and slog;
// only Reveal returns the value.
type Secret struct{ value string }

// NewSecret wraps value.
func NewSecret(value string) Secret { return Secret{value: value} }

// Reveal returns the value.
func (s Secret) Reveal() string { return s.value }

func (Secret) String() string               { return redacted }
func (Secret) GoString() string             { return redacted }
func (Secret) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(redacted)) }
func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
func (Secret) LogValue() slog.Value         { return slog.StringValue(redacted) }
func (*Secret) UnmarshalJSON([]byte) error  { return fmt.Errorf("a secret is never read from JSON") }

// Entry is one credential as a caller sees it. Its fields are redacting Secrets.
type Entry struct {
	ID       string
	Name     string
	Kind     Kind
	Fields   map[string]Secret
	Created  time.Time
	Modified time.Time
}

// Info is an Entry without its values.
type Info struct {
	ID       string
	Name     string
	Kind     Kind
	Created  time.Time
	Modified time.Time
}
