package control

import "time"

// DeployWait is how long a request that starts a deploy waits for its end. It stays below the client's
// time limit for a call. A run that takes longer is not stopped: the answer says it is still pending, and
// status and history tell the outcome.
const DeployWait = 20 * time.Second

// MaxUpdateProjects bounds how many projects one OpDeployUpdate request names.
const MaxUpdateProjects = 20

// The deploy operations. The first group needs no admin session: reading, and the controls that only
// hold a project back (pause, pin) or let it continue (resume, unpin) under its approved rules. run-now
// deploys only what the version rules of the project allow without a human. The second group deploys
// versions a human chose and restores data, so it needs the admin session like every other change.
const (
	OpDeployStatus  = "deploy.status"
	OpDeployHistory = "deploy.history"
	OpDeployRunNow  = "deploy.run-now"
	OpDeployPause   = "deploy.pause"
	OpDeployResume  = "deploy.resume"
	OpDeployPin     = "deploy.pin"
	OpDeployUnpin   = "deploy.unpin"
	OpBackupsList   = "backups.list"

	OpDeployUpdate  = "deploy.update"
	OpDeployRestore = "deploy.restore"
)

// The codes of a deploy request, besides the general ones.
const (
	// CodePaused and CodePinned answer a deploy of a project that is paused or pinned.
	CodePaused = "paused"
	CodePinned = "pinned"
	// CodeVersionRefused answers a requested version that is not an available update of its source.
	CodeVersionRefused = "version-refused"
	// CodeBackupUnknown answers a restore from a backup that is not one of the project's.
	CodeBackupUnknown = "backup-unknown"
	// CodeNoRestore answers a restore of a project whose approved backup has no restore command.
	CodeNoRestore = "no-restore"
)

// The results of a skipped check, as OpDeployRunNow reports them.
const (
	SkipPaused = "paused"
	SkipPinned = "pinned"
	SkipLocked = "locked"
)

// DeployRef names the project of OpDeployStatus, OpDeployHistory, OpDeployRunNow, OpDeployPause,
// OpDeployResume, OpDeployPin, OpDeployUnpin and OpBackupsList.
type DeployRef struct {
	Project string `json:"project"`
}

// DeployVersion is a version of one source: an image by its name, or the git source of the project.
type DeployVersion struct {
	// Kind is SourceImage or SourceGit; Name is the image name and empty for git.
	Kind    string `json:"kind"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version"`
}

// DeploySource is the state of one source of a project.
type DeploySource struct {
	Kind      string   `json:"kind"`
	Name      string   `json:"name,omitempty"`
	Current   string   `json:"current,omitempty"`
	Previous  string   `json:"previous,omitempty"`
	Available []string `json:"available,omitempty"`
	// Failed is a version that was rolled back; it is not deployed automatically again.
	Failed string `json:"failed,omitempty"`
	// Error is why the source could not be read in the last check.
	Error string `json:"error,omitempty"`
}

// DeployFault is the error state of a project whose rollback failed.
type DeployFault struct {
	Time       time.Time `json:"time"`
	Run        string    `json:"run,omitempty"`
	FailedStep string    `json:"failed_step,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// DeployStatusResult is the answer to OpDeployStatus.
type DeployStatusResult struct {
	Project string `json:"project"`
	// Running tells that a run of the project is active now.
	Running bool `json:"running"`
	Paused  bool `json:"paused"`
	Pinned  bool `json:"pinned"`
	// Locked tells that the vault is locked, so nothing is deployed.
	Locked     bool           `json:"locked"`
	Fault      *DeployFault   `json:"fault,omitempty"`
	LastCheck  time.Time      `json:"last_check,omitzero"`
	CheckError string         `json:"check_error,omitempty"`
	Sources    []DeploySource `json:"sources"`
	LastRun    *DeployRun     `json:"last_run,omitempty"`
}

// DeployChange is the version change of one source in a run.
type DeployChange struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	From string `json:"from"`
	To   string `json:"to"`
}

// DeployRun is one finished run. Action is auto, manual or restore; Result is succeeded, failed,
// rolled-back or rollback-failed.
type DeployRun struct {
	ID         string         `json:"id"`
	Action     string         `json:"action"`
	Started    time.Time      `json:"started"`
	DurationMS int64          `json:"duration_ms"`
	Result     string         `json:"result"`
	ErrorCode  string         `json:"error_code,omitempty"`
	FailedStep string         `json:"failed_step,omitempty"`
	Error      string         `json:"error,omitempty"`
	Changes    []DeployChange `json:"changes,omitempty"`
	Backup     string         `json:"backup,omitempty"`
}

// DeployHistoryResult is the answer to OpDeployHistory, the latest runs with the oldest first.
type DeployHistoryResult struct {
	Project string      `json:"project"`
	Runs    []DeployRun `json:"runs"`
}

// DeployRunNowResult is the answer to OpDeployRunNow. Skipped names why nothing was checked; Run is empty
// when nothing was deployed. Pending means the run was still active when the answer left.
type DeployRunNowResult struct {
	Project string     `json:"project"`
	Skipped string     `json:"skipped,omitempty"`
	Run     *DeployRun `json:"run,omitempty"`
	Pending bool       `json:"pending,omitempty"`
}

// ProjectUpdate asks for versions of one project.
type ProjectUpdate struct {
	Project  string          `json:"project"`
	Versions []DeployVersion `json:"versions"`
}

// DeployUpdateParams are the parameters of OpDeployUpdate. The projects are updated one after the other.
type DeployUpdateParams struct {
	Updates []ProjectUpdate `json:"updates"`
}

// ProjectUpdateResult is the outcome for one project of OpDeployUpdate: the run, or the code why there is
// none, or Pending when the project had not finished when the answer left.
type ProjectUpdateResult struct {
	Project string     `json:"project"`
	Run     *DeployRun `json:"run,omitempty"`
	Error   string     `json:"error,omitempty"`
	Pending bool       `json:"pending,omitempty"`
}

// DeployUpdateResult is the answer to OpDeployUpdate.
type DeployUpdateResult struct {
	Results []ProjectUpdateResult `json:"results"`
}

// DeployRestoreParams are the parameters of OpDeployRestore; Backup is a name OpBackupsList reported.
type DeployRestoreParams struct {
	Project string `json:"project"`
	Backup  string `json:"backup"`
}

// DeployRestoreResult is the answer to OpDeployRestore.
type DeployRestoreResult struct {
	Project string     `json:"project"`
	Run     *DeployRun `json:"run,omitempty"`
	Pending bool       `json:"pending,omitempty"`
}

// BackupsListResult is the answer to OpBackupsList, the backups of a project with the oldest first.
type BackupsListResult struct {
	Project string   `json:"project"`
	Backups []string `json:"backups"`
}
