package auditlog

import (
	"errors"
	"regexp"
	"time"
)

// The kinds of marker entry (see Entry.Kind), as opposed to the empty Kind of an action entry. KindCut
// marks a retention-cut marker, KindDamage a damaged-line marker.
const (
	KindCut    = "retention-cut"
	KindDamage = "damaged-line"
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

// Cut documents a retention deletion. Append writes it as the entry right after the deletion, naming the
// sequence number of the last entry the deletion removed, so Verify never mistakes a documented cut for a
// corrupted, undocumented gap.
type Cut struct {
	ThroughSeq uint64 `json:"through_seq"`
}

// Damage documents a line that does not parse as an entry, such as one torn by a crash while it was
// written. Append writes it as the entry right after that line, naming the SHA-256 of the line's exact
// bytes without its newline, so Verify can tell the damage it documents from a line changed afterwards. It
// never holds the bytes themselves.
type Damage struct {
	LineHash string `json:"line_hash"`
}

// Entry is one line of a log file, in the field order it is written. Kind is empty for an action, KindCut
// for a retention-cut marker, which carries only Cut beside the chain fields, and KindDamage for a
// damaged-line marker, which carries only Damage beside them. MAC is the signature of an entry that an
// earlier version wrote; it is read, never written. No field ever holds an argument, a result payload, or a
// secret. The hash covers the stored bytes, so a line with a field this struct no longer has, or an action sysmin no longer writes, still
// reads and verifies.
type Entry struct {
	Seq  uint64    `json:"seq"`
	Time time.Time `json:"time"`
	Kind string    `json:"kind,omitempty"`
	// ClientUID is the Unix user ID of the client that triggered the action; a pointer because 0 is root.
	ClientUID  *uint32 `json:"client_uid,omitempty"`
	Action     string  `json:"action,omitempty"`
	Result     string  `json:"result,omitempty"`
	ErrorCode  string  `json:"error_code,omitempty"`
	DurationMS int64   `json:"duration_ms,omitempty"`
	Cut        *Cut    `json:"cut,omitempty"`
	Damage     *Damage `json:"damage,omitempty"`
	// Host is set on the entries of OperationHostChange that name a change.
	Host     *Host  `json:"host,omitempty"`
	PrevHash string `json:"prev_hash"`
	// MAC is the signature of an entry that an earlier version wrote.
	MAC string `json:"mac,omitempty"`
}

// Fields is what a caller supplies for one completed action; Append computes Seq, Time, and PrevHash.
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

const maxTextLength = 256

// MaxDuration is the longest duration Validate accepts for one action.
const MaxDuration = 30 * 24 * time.Hour

// errorCodePattern is the form of Fields.ErrorCode: a lowercase word with dashes.
var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// Validate reports whether f is something sysmin could log: a known action, a result with a matching error
// code, a non-negative bounded duration, and a host action that fits the action. The error names the field, never its value.
func (f Fields) Validate() error {
	if !knownOperations[f.Action] {
		return errors.New("action is not a known operation")
	}
	switch {
	case f.Result == ResultSuccess && f.ErrorCode != "":
		return errors.New("a successful result has no error code")
	case f.Result == ResultFailure && !errorCodePattern.MatchString(f.ErrorCode):
		return errors.New("a failed result needs an error code of lowercase words with dashes")
	case f.Result != ResultSuccess && f.Result != ResultFailure:
		return errors.New("result must be success or failure")
	}
	switch {
	case f.Host != nil && f.Action != OperationHostChange:
		return errors.New("only a host change names a host action")
	case f.Host == nil && f.Action == OperationHostChange && f.Result == ResultSuccess:
		return errors.New("a successful host change names its action")
	case f.Host != nil:
		if err := f.Host.Validate(); err != nil {
			return err
		}
	}
	if f.Duration < 0 || f.Duration > MaxDuration {
		return errors.New("duration is out of range")
	}
	return nil
}
