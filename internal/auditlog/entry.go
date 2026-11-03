package auditlog

import (
	"errors"
	"regexp"
	"time"
)

// The two results an entry records.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
)

// Host names a change of the host: the registered action by its id, the journal entry that holds the
// details, and the journal entry an undo reverts. It never holds an argument or a command.
type Host struct {
	ActionID  string `json:"action_id"`
	JournalID string `json:"journal_id"`
	UndoOf    string `json:"undo_of,omitempty"`
}

var (
	hostActionPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$`)
	hostJournalPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{6}Z-[0-9a-f]{6}$`)
)

// maxHostActionLength bounds an action id.
const maxHostActionLength = 64

// Validate reports whether h has the form of an action id and journal ids. The error names the field, never
// its value.
func (h Host) Validate() error {
	switch {
	case len(h.ActionID) > maxHostActionLength || !hostActionPattern.MatchString(h.ActionID):
		return errors.New("host action id has an invalid form")
	case !hostJournalPattern.MatchString(h.JournalID):
		return errors.New("host journal id has an invalid form")
	case h.UndoOf != "" && !hostJournalPattern.MatchString(h.UndoOf):
		return errors.New("host undo id has an invalid form")
	}
	return nil
}

// Entry is the part of a log line that qsysmin owns, in the order it is written. No field ever holds an
// argument, a result payload, or a secret. A stored line may carry members this struct no longer has or an
// action qsysmin no longer writes; it still reads and verifies, since the hash covers the stored bytes.
type Entry struct {
	// ClientUID is the Unix user ID of the client that triggered the action; a pointer because 0 is root.
	ClientUID  *uint32 `json:"client_uid,omitempty"`
	Action     string  `json:"action,omitempty"`
	Result     string  `json:"result,omitempty"`
	ErrorCode  string  `json:"error_code,omitempty"`
	DurationMS int64   `json:"duration_ms,omitempty"`
	// Host is set on the entries of OperationHostChange that name a change.
	Host *Host `json:"host,omitempty"`
	// MAC is the signature of a line that an earlier version wrote. It is read, never written: Validate
	// refuses an entry that sets it.
	MAC string `json:"mac,omitempty"`
}

// Fields is what a caller supplies for one completed action; the log adds the sequence number, the time and
// the chain.
type Fields struct {
	ClientUID uint32
	// Action is one of the Operation constants.
	Action string
	// Result is ResultSuccess or ResultFailure.
	Result string
	// ErrorCode names the failure and is set exactly when Result is ResultFailure.
	ErrorCode string
	Duration  time.Duration
	// Host is required for a successful OperationHostChange and allowed for no other action.
	Host *Host
}

func (f Fields) entry() Entry {
	uid := f.ClientUID
	return Entry{
		ClientUID: &uid, Action: f.Action, Result: f.Result, ErrorCode: f.ErrorCode,
		DurationMS: f.Duration.Milliseconds(), Host: f.Host,
	}
}

// MaxDuration is the longest duration Validate accepts for one action.
const MaxDuration = 30 * 24 * time.Hour

// errorCodePattern is the form of Entry.ErrorCode: a lowercase word with dashes.
var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// Validate reports whether f is something qsysmin could log; see Entry.Validate.
func (f Fields) Validate() error {
	if f.Duration < 0 || f.Duration > MaxDuration {
		return errors.New("duration is out of range")
	}
	return f.entry().Validate()
}

// Validate reports whether e is something qsysmin could log: a known action, a result with a matching error
// code, a non-negative bounded duration, a host action that fits the action, and no signature. The error
// names the field, never its value.
func (e Entry) Validate() error {
	if !knownOperations[e.Action] {
		return errors.New("action is not a known operation")
	}
	switch {
	case e.Result == ResultSuccess && e.ErrorCode != "":
		return errors.New("a successful result has no error code")
	case e.Result == ResultFailure && !errorCodePattern.MatchString(e.ErrorCode):
		return errors.New("a failed result needs an error code of lowercase words with dashes")
	case e.Result != ResultSuccess && e.Result != ResultFailure:
		return errors.New("result must be success or failure")
	}
	switch {
	case e.Host != nil && e.Action != OperationHostChange:
		return errors.New("only a host change names a host action")
	case e.Host == nil && e.Action == OperationHostChange && e.Result == ResultSuccess:
		return errors.New("a successful host change names its action")
	case e.Host != nil:
		if err := e.Host.Validate(); err != nil {
			return err
		}
	}
	if e.DurationMS < 0 || e.DurationMS > MaxDuration.Milliseconds() {
		return errors.New("duration is out of range")
	}
	if e.MAC != "" {
		return errors.New("an entry is not signed")
	}
	return nil
}
