package control

import "time"

// The operations on the action log. Both read it without an admin session and never return a file path
// or a raw log line.
const (
	// OpLogList lists entries of the action log, newest first, in pages.
	OpLogList = "log.list"
	// OpLogVerify checks the hash chain of the log, and its check values while the vault is unlocked.
	OpLogVerify = "log.verify"
)

// Limits of OpLogList: a request without a limit gets LogDefaultLimit entries.
const (
	LogDefaultLimit = 200
	LogMaxLimit     = 500
)

// The results an entry records, as LogListParams.Result filters them.
const (
	LogResultSuccess = "success"
	LogResultFailure = "failure"
)

// LogListParams are the parameters of OpLogList. Every field narrows the result; From and To are RFC 3339
// times, both inclusive. Action is a known action word such as "config.read". Limit is 1 to LogMaxLimit,
// 0 meaning LogDefaultLimit. After is the Next of the previous answer.
type LogListParams struct {
	From    string `json:"from,omitempty"`
	To      string `json:"to,omitempty"`
	Project string `json:"project,omitempty"`
	Action  string `json:"action,omitempty"`
	Result  string `json:"result,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	After   string `json:"after,omitempty"`
}

// LogEntry is one entry of the log as the parsed fields carry it. ClientUID is a pointer because 0 is
// root. Signed is false for an entry written while the log key was not available.
type LogEntry struct {
	Seq        uint64    `json:"seq"`
	Time       time.Time `json:"time"`
	ClientUID  *uint32   `json:"client_uid,omitempty"`
	Action     string    `json:"action"`
	Project    string    `json:"project,omitempty"`
	Result     string    `json:"result"`
	ErrorCode  string    `json:"error_code,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Signed     bool      `json:"signed,omitempty"`
}

// LogListResult is the answer to OpLogList. Next is set when more entries match; passing it as After
// continues right behind the last entry returned.
type LogListResult struct {
	Entries []LogEntry `json:"entries"`
	Next    string     `json:"next,omitempty"`
}

// The signature states of a day in OpLogVerify's answer.
const (
	LogSignaturesValid      = "valid"
	LogSignaturesInvalid    = "invalid"
	LogSignaturesNotChecked = "not-checked"
)

// The fixed problem words of OpLogVerify.
const (
	LogProblemUnparsable = "unparsable-line"
	LogProblemGenesis    = "bad-genesis"
	LogProblemMissing    = "missing-entries"
	LogProblemChain      = "chain-broken"
	LogProblemMAC        = "check-value-mismatch"
)

// LogVerifyMaxProblems is how many problems an answer lists at most.
const LogVerifyMaxProblems = 20

// LogDay is the state of one day file. Date is YYYY-MM-DD; Entries counts action entries. Damaged counts
// the unreadable lines of the day, such as one torn by a crash, that a marker entry right after them
// documents; such a line leaves the day intact.
type LogDay struct {
	Date       string `json:"date"`
	Entries    int    `json:"entries"`
	Intact     bool   `json:"intact"`
	Signatures string `json:"signatures"`
	Damaged    int    `json:"damaged,omitempty"`
}

// LogProblem is the first problem of one day: Seq is the sequence number of the entry concerned (0 for a
// line that does not parse) and Line the 1-based line number in the day's file.
type LogProblem struct {
	Date string `json:"date"`
	Seq  uint64 `json:"seq,omitempty"`
	Line int    `json:"line"`
	// Code is one of the LogProblem constants.
	Code string `json:"code"`
}

// LogVerifyResult is the answer to OpLogVerify. Intact is false when the chain is broken anywhere or a
// check value does not match. Checked is true when check values were checked, which takes an unlocked
// vault. Problems lists the first LogVerifyMaxProblems days with a problem, oldest first; MoreProblems
// says there were further ones.
type LogVerifyResult struct {
	Intact       bool         `json:"intact"`
	Checked      bool         `json:"checked"`
	Days         []LogDay     `json:"days"`
	Problems     []LogProblem `json:"problems,omitempty"`
	MoreProblems bool         `json:"more_problems,omitempty"`
}
