package control

import "fmt"

// CredentialKind describes one kind of vault credential for the clients: which fields are plain options
// and which one field carries the secret. A test keeps the table equal to the vault's own.
type CredentialKind struct {
	Name string
	// Plain are the fields that are not secret and may be given as options.
	Plain []string
	// Secret is the one field that is never given as an argument or an environment variable.
	Secret string
	// Multiline marks a secret that spans lines, so it cannot be typed at a prompt.
	Multiline bool
}

// CredentialKinds lists every kind, sorted by name.
var CredentialKinds = []CredentialKind{
	{Name: "git-https", Plain: []string{"user"}, Secret: "token"},
	{Name: "registry", Plain: []string{"user"}, Secret: "token"},
	{Name: "secret", Secret: "value"},
	{Name: "smtp", Plain: []string{"user"}, Secret: "password"},
	{Name: "ssh-key", Plain: []string{"host_key"}, Secret: "private_key", Multiline: true},
}

// KindOf returns the kind with the given name.
func KindOf(name string) (CredentialKind, bool) {
	for _, k := range CredentialKinds {
		if k.Name == name {
			return k, true
		}
	}
	return CredentialKind{}, false
}

// CredentialSetParams are the parameters of OpCredentialsSet. Fields holds every field the kind requires,
// the secret one included; it is the only place a secret travels, and only toward the service.
type CredentialSetParams struct {
	Name   string            `json:"name"`
	Kind   string            `json:"kind"`
	Fields map[string]string `json:"fields"`
}

// Format keeps the secret out of any formatted output of the parameters.
func (p CredentialSetParams) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "credential %s (%s)", p.Name, p.Kind)
}

// CredentialRef names one credential for OpCredentialsRemove.
type CredentialRef struct {
	Name string `json:"name"`
}

// CredentialUse is one project or channel that names a credential. Current is set when the configuration
// as it is now names it, Approved when the approved state does.
type CredentialUse struct {
	// Kind is project or channel.
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Current  bool   `json:"current,omitempty"`
	Approved bool   `json:"approved,omitempty"`
}

// CredentialSummary is one credential as OpCredentialsList reports it. It never carries a value.
type CredentialSummary struct {
	Name string          `json:"name"`
	Kind string          `json:"kind"`
	ID   string          `json:"id"`
	Uses []CredentialUse `json:"uses,omitempty"`
}

// CredentialsListResult is the answer to OpCredentialsList.
type CredentialsListResult struct {
	Credentials []CredentialSummary `json:"credentials"`
}

// CredentialResult is the answer to OpCredentialsSet and OpCredentialsRemove.
type CredentialResult struct {
	Name string `json:"name"`
	// Action is created, replaced or removed.
	Action string `json:"action"`
	// ID is the vault id of the stored credential, empty after a removal.
	ID string `json:"id,omitempty"`
	// State is StatePending when an approval is open because of the change, StateApproved otherwise.
	State string `json:"state"`
	// Pending names the open approvals the change caused or left open: project names, and the name of the
	// channels as one unit.
	Pending []string `json:"pending,omitempty"`
	// Approved names the approvals an admin session closed with the change.
	Approved []string `json:"approved,omitempty"`
}
