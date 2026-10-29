package control

import "time"

// SourceTestTimeout bounds one OpSourceTest request as a whole; each source gets a share of it.
const SourceTestTimeout = 2 * time.Minute

// The kinds of a source.
const (
	SourceImage = "image"
	SourceGit   = "git"
)

// The codes of a source that could not be read. They are fixed words; the message beside them never
// carries a secret.
const (
	SourceDenied      = "denied"
	SourceNotFound    = "not-found"
	SourceUnreachable = "unreachable"
	SourceTimeout     = "timeout"
	// SourceCredential means the credential of the source is missing from the approved state, is of the
	// wrong kind or does not fit the address.
	SourceCredential = "credential"
	SourceInvalid    = "invalid"
)

// CodeCurrentUnknown is the problem code for a source whose current version could not be determined.
const CodeCurrentUnknown = "current-unknown"

// MaxAvailable is how many of the available versions a source result lists, the highest ones.
const MaxAvailable = 100

// SourceTestParams are the parameters of OpSourceTest.
type SourceTestParams struct {
	Project string `json:"project"`
}

// SourceResult is the outcome for one image or git source. Error is empty when the source was read; the
// version fields are then valid.
type SourceResult struct {
	// Kind is SourceImage or SourceGit.
	Kind string `json:"kind"`
	// Name is the image name, or git for the git source.
	Name string `json:"name"`
	// Ref is the image repository or the repository URL without any user information.
	Ref     string `json:"ref"`
	Current string `json:"current,omitempty"`
	// Available lists the versions above Current, lowest first, at most MaxAvailable of the highest;
	// AvailableTotal is the number found.
	Available      []string  `json:"available,omitempty"`
	AvailableTotal int       `json:"available_total"`
	Target         string    `json:"target,omitempty"`
	Digest         string    `json:"digest,omitempty"`
	SourceRepo     string    `json:"source_repo,omitempty"`
	Problems       []Problem `json:"problems,omitempty"`
	// Error is one of the Source codes; Message says what happened.
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// SourceTestResult is the answer to OpSourceTest.
type SourceTestResult struct {
	Project string         `json:"project"`
	Sources []SourceResult `json:"sources"`
}
