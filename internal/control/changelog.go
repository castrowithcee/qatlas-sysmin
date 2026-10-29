package control

import "time"

// ChangelogTimeout bounds one OpProjectsChangelog request as a whole; each source gets a share of it.
const ChangelogTimeout = 2 * time.Minute

// ChangelogRateLimited is the source code for GitHub refusing more requests while no cached release list
// exists. With a cached list the result carries the releases and a notice instead.
const ChangelogRateLimited = "rate-limited"

// The limits of a changelog result, which keep it within MaxResponseBytes.
const (
	MaxChangelogReleases = 25
	// MaxChangelogBytes bounds the release texts of all sources together.
	MaxChangelogBytes = 640 << 10
)

// ChangelogParams are the parameters of OpProjectsChangelog.
type ChangelogParams struct {
	Project string `json:"project"`
	// Image limits the result to one image and leaves out the git source.
	Image string `json:"image,omitempty"`
	// To replaces the target version the version rule selects.
	To string `json:"to,omitempty"`
}

// ChangelogRelease is one release text, already reduced to plain text by the service.
type ChangelogRelease struct {
	Version   string    `json:"version"`
	Tag       string    `json:"tag"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	URL       string    `json:"url,omitempty"`
	Published time.Time `json:"published,omitzero"`
}

// ChangelogSource is the outcome for one image or git source. Error is empty when the releases could be
// read; Link is then the repository address to show when Releases is empty.
type ChangelogSource struct {
	// Kind is SourceImage or SourceGit.
	Kind string `json:"kind"`
	// Name is the image name, or git for the git source.
	Name string `json:"name"`
	// Ref is the image repository or the repository URL without any user information.
	Ref     string `json:"ref"`
	Current string `json:"current,omitempty"`
	Target  string `json:"target,omitempty"`
	// Repo is the GitHub repository as owner/repo, when there is one.
	Repo string `json:"repo,omitempty"`
	Link string `json:"link,omitempty"`
	// Releases are the releases above Current up to Target, newest first, at most MaxChangelogReleases of the
	// newest; ReleasesTotal is the number found.
	Releases      []ChangelogRelease `json:"releases"`
	ReleasesTotal int                `json:"releases_total"`
	// Stale reports that the releases come from an expired cache; Notice says why.
	Stale    bool      `json:"stale,omitempty"`
	Notice   string    `json:"notice,omitempty"`
	Problems []Problem `json:"problems,omitempty"`
	// Error is one of the Source codes or ChangelogRateLimited; Message says what happened.
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// ChangelogResult is the answer to OpProjectsChangelog.
type ChangelogResult struct {
	Project string            `json:"project"`
	Sources []ChangelogSource `json:"sources"`
}
