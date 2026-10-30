package control

import "time"

// AdoptTimeout bounds how long the service may take to inspect one stack.
const AdoptTimeout = time.Minute

// CodeDockerUnavailable answers an adoption while Docker cannot be asked for the running containers. The
// output of Docker never leaves the service.
const CodeDockerUnavailable = "docker-unavailable"

// CodeNoAccess answers an adoption of a directory that exists but that the service cannot reach: the one entry
// of Paths is the first part of the requested path that the service user may not pass. Nothing is opened to
// find it, and the entry is always a prefix of the requested path.
const CodeNoAccess = "no-access"

// AdoptParams are the parameters of OpProjectsAdopt. Dir is the absolute directory of the running stack;
// an empty Name proposes the Compose project name. With DryRun nothing is stored.
type AdoptParams struct {
	Dir    string `json:"dir"`
	Name   string `json:"name,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
}

// AdoptImage is one tracked image of a proposal. Current is the version the stack runs now, empty when it
// is not known.
type AdoptImage struct {
	Name     string   `json:"name"`
	Ref      string   `json:"ref"`
	EnvVar   string   `json:"env_var"`
	Services []string `json:"services,omitempty"`
	Current  string   `json:"current,omitempty"`
}

// AdoptGit is the git source of a proposal; URL carries no user information.
type AdoptGit struct {
	URL     string `json:"url,omitempty"`
	Current string `json:"current,omitempty"`
}

// AdoptProject is the project the service proposes.
type AdoptProject struct {
	Name           string       `json:"name"`
	Dir            string       `json:"dir"`
	ComposeProject string       `json:"compose_project,omitempty"`
	ComposeFiles   []string     `json:"compose_files,omitempty"`
	EnvFile        string       `json:"env_file,omitempty"`
	Template       string       `json:"template,omitempty"`
	Images         []AdoptImage `json:"images,omitempty"`
	Git            *AdoptGit    `json:"git,omitempty"`
}

// AdoptProblem is a finding about the stack with a hint. A blocking problem keeps the proposal from being
// stored.
type AdoptProblem struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
	Blocking bool   `json:"blocking,omitempty"`
}

// AdoptResult is the answer to OpProjectsAdopt. File is the project file as it would be or was stored.
// State and Revision are those of a ChangeResult and set only when Saved.
type AdoptResult struct {
	Project  AdoptProject   `json:"project"`
	File     string         `json:"file,omitempty"`
	Problems []AdoptProblem `json:"problems"`
	Saved    bool           `json:"saved"`
	State    string         `json:"state,omitempty"`
	Revision string         `json:"revision,omitempty"`
}
