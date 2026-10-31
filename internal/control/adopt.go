package control

import "time"

// AdoptTimeout bounds how long the service may take to inspect one stack.
const AdoptTimeout = time.Minute

// StacksTimeout bounds how long the service may take to list the running stacks.
const StacksTimeout = 30 * time.Second

// CodeDockerUnavailable answers an adoption while Docker cannot be asked for the running containers. The
// output of Docker never leaves the service.
const CodeDockerUnavailable = "docker-unavailable"

// CodeNoAccess is no longer answered by OpProjectsAdopt: a directory or file the service may not use is the
// blocking problem "missing-access" of the result. The code stays for clients of older services.
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

// Rights of an AdoptAccess.
const (
	AccessPass  = "pass"
	AccessRead  = "read"
	AccessWrite = "write"
)

// AdoptAccess is one right the service user lacks: Right is AccessPass for a directory it may not search,
// AccessRead or AccessWrite. A path that the service cannot reach hides what lies below it; those paths are
// not listed, only said in the message of the problem.
type AdoptAccess struct {
	Path  string `json:"path"`
	Right string `json:"right"`
}

// AdoptProblem is a finding about the stack with a hint. A blocking problem keeps the proposal from being
// stored. A problem with the code "missing-access" is blocking and lists in Access every missing right that
// could be found at once; the field is absent for all other problems, and clients that do not know it show
// the message and the hint.
type AdoptProblem struct {
	Code     string        `json:"code"`
	Message  string        `json:"message"`
	Hint     string        `json:"hint,omitempty"`
	Blocking bool          `json:"blocking,omitempty"`
	Access   []AdoptAccess `json:"access,omitempty"`
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

// Stack is one running Compose stack. ManagedBy names the project that manages its directory, empty when
// none does. Containers counts the running containers without one-off ones.
type Stack struct {
	ComposeProject string   `json:"compose_project"`
	Dir            string   `json:"dir"`
	ComposeFiles   []string `json:"compose_files,omitempty"`
	Containers     int      `json:"containers"`
	ManagedBy      string   `json:"managed_by,omitempty"`
}

// StacksResult is the answer to OpProjectsStacks: stacks no project manages first, then by Compose project
// and directory. Truncated says that containers were left out, so the list may be incomplete.
type StacksResult struct {
	Stacks    []Stack `json:"stacks"`
	Truncated bool    `json:"truncated,omitempty"`
}
