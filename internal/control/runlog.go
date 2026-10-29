package control

// OpDeployLog reads the log of one run of a project. It needs no admin session.
const OpDeployLog = "deploy.log"

// RunLogMaxBytes bounds the text one OpDeployLog answer carries. The answer stays below MaxResponseBytes even
// when every byte of the text needs the longest JSON escape.
const RunLogMaxBytes = 128 << 10

// DeployLogParams are the parameters of OpDeployLog; Run is the ID of a DeployRun.
type DeployLogParams struct {
	Project string `json:"project"`
	Run     string `json:"run"`
}

// DeployLogResult is the answer to OpDeployLog. Text is the end of the run log, cut at a line start, with
// control characters removed; Truncated says that an earlier part was left out.
type DeployLogResult struct {
	Project   string `json:"project"`
	Run       string `json:"run"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}
