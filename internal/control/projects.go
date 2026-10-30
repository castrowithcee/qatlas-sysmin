package control

// Problem is one finding of the validation: where it is, which rule it breaks, why, and what to do. It
// never carries a file system path of the service.
type Problem struct {
	// Field is the location inside the project, such as images[1].ref; empty for the project as a whole.
	Field   string `json:"field,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// The states of a change or a project in the answers.
const (
	// StateApproved means the stored project is the state an administrator approved.
	StateApproved = "approved"
	// StatePending means the stored project differs from its approved state; it does not run before a
	// person approves it in the terminal interface.
	StatePending = "pending approval"
	// StateUnknown means the vault is locked, so the approved state cannot be compared.
	StateUnknown = "unknown"
)

// ProjectImage is one image of a project in a change. Empty fields stay as they are on an edit; Services,
// when set, replaces the services of the image.
type ProjectImage struct {
	Name       string   `json:"name"`
	Ref        string   `json:"ref,omitempty"`
	EnvVar     string   `json:"env_var,omitempty"`
	Services   []string `json:"services,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// ProjectParams are the parameters of OpProjectsAdd and OpProjectsEdit. File, the text of a complete project
// file, replaces the project and excludes every other field. Otherwise the fields change a project that
// starts empty on an add and is the stored one on an edit; an empty field leaves its value alone.
type ProjectParams struct {
	Name string `json:"name"`
	// Revision, when set on an edit, is the revision of the stored file the change is based on, as show
	// reported it; another stored revision refuses the change with CodeChanged.
	Revision string `json:"revision,omitempty"`
	File     string `json:"file,omitempty"`

	Template       string            `json:"template,omitempty"`
	Dir            string            `json:"dir,omitempty"`
	EnvFile        string            `json:"env_file,omitempty"`
	ComposeFiles   []string          `json:"compose_files,omitempty"`
	ComposeProject string            `json:"compose_project,omitempty"`
	PollInterval   string            `json:"poll_interval,omitempty"`
	Values         map[string]string `json:"values,omitempty"`
	UnsetValues    []string          `json:"unset_values,omitempty"`
	Images         []ProjectImage    `json:"images,omitempty"`
	RemoveImages   []string          `json:"remove_images,omitempty"`
	GitURL         string            `json:"git_url,omitempty"`
	GitCredential  string            `json:"git_credential,omitempty"`
}

// ProjectRef names one project. Revision serves OpProjectsRemove like ProjectParams.Revision; Version is the
// example version of OpProjectsCheck.
type ProjectRef struct {
	Name     string `json:"name"`
	Revision string `json:"revision,omitempty"`
	Version  string `json:"version,omitempty"`
}

// ChangeResult is the answer to every operation that changes the configuration. State says whether the
// change is approved already or waits for a person; Revision is the revision of the file written, empty
// after a removal.
type ChangeResult struct {
	Project  string `json:"project"`
	State    string `json:"state"`
	Revision string `json:"revision,omitempty"`
	// NotWritable lists the paths a deploy of the project writes that the service may not write. The change
	// is stored; the project cannot be approved until they are writable.
	NotWritable []string `json:"not_writable,omitempty"`
}

// ProjectSummary is one project as OpProjectsList reports it.
type ProjectSummary struct {
	Name     string `json:"name"`
	Template string `json:"template,omitempty"`
	// Source is images or git.
	Source string `json:"source"`
	Dir    string `json:"dir"`
	State  string `json:"state"`
}

// ProjectsListResult is the answer to OpProjectsList.
type ProjectsListResult struct {
	Projects []ProjectSummary `json:"projects"`
}

// ProjectDetail is the answer to OpProjectsShow: the stored file, which may differ from the approved state
// as State says.
type ProjectDetail struct {
	ProjectSummary
	Revision string `json:"revision"`
	// File is the project file as stored.
	File string `json:"file"`
}

// CheckStep is one resolved step of OpProjectsCheck. Detail lists the parameters the step would run with.
type CheckStep struct {
	Type    string `json:"type"`
	Timeout string `json:"timeout"`
	// Free marks a free command line.
	Free   bool   `json:"free,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// CheckResult is the answer to OpProjectsCheck for a project without problems.
type CheckResult struct {
	Project string      `json:"project"`
	Version string      `json:"version"`
	Steps   []CheckStep `json:"steps"`
}

// HasFields reports whether any field besides Name, Revision and File is set.
func (p ProjectParams) HasFields() bool {
	return p.Template != "" || p.Dir != "" || p.EnvFile != "" || len(p.ComposeFiles) > 0 || p.ComposeProject != "" ||
		p.PollInterval != "" || len(p.Values) > 0 || len(p.UnsetValues) > 0 || len(p.Images) > 0 ||
		len(p.RemoveImages) > 0 || p.GitURL != "" || p.GitCredential != ""
}
