package auditlog

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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
// damaged-line marker, which carries only Damage beside them. MAC is the check
// value of a signed entry and always the last field; it stays empty for an entry written without the log
// key, which marks it unverified. No field ever holds an argument, a result payload, or a secret.
type Entry struct {
	Seq  uint64    `json:"seq"`
	Time time.Time `json:"time"`
	Kind string    `json:"kind,omitempty"`
	// ClientUID is the Unix user ID of the client that triggered the action; a pointer because 0 is root.
	ClientUID  *uint32 `json:"client_uid,omitempty"`
	Action     string  `json:"action,omitempty"`
	Project    string  `json:"project,omitempty"`
	Result     string  `json:"result,omitempty"`
	ErrorCode  string  `json:"error_code,omitempty"`
	DurationMS int64   `json:"duration_ms,omitempty"`
	Cut        *Cut    `json:"cut,omitempty"`
	Damage     *Damage `json:"damage,omitempty"`
	PrevHash   string  `json:"prev_hash"`
	// MAC is the check value of a signed entry, empty for an unsigned one; see Key.mac.
	MAC string `json:"mac"`
}

// Fields is what a caller supplies for one completed action; Append computes Seq, Time, PrevHash, and MAC.
type Fields struct {
	ClientUID uint32
	// Action is one of the Operation constants.
	Action  string
	Project string
	// Result is ResultSuccess or ResultFailure.
	Result string
	// ErrorCode names the failure and is set exactly when Result is ResultFailure.
	ErrorCode string
	Duration  time.Duration
}

const maxTextLength = 256

// MaxDuration is the longest duration Validate accepts for one action.
const MaxDuration = 30 * 24 * time.Hour

// errorCodePattern is the form of Fields.ErrorCode: a lowercase word with dashes.
var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// Validate reports whether f is something sysmin could log: a known action, a result with a matching error
// code, a non-negative bounded duration, and a project of bounded length that is valid UTF-8 without a
// control character. The error names the field, never its value.
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
	if f.Duration < 0 || f.Duration > MaxDuration {
		return errors.New("duration is out of range")
	}
	if len(f.Project) > maxTextLength || !utf8.ValidString(f.Project) || strings.IndexFunc(f.Project, unicode.IsControl) >= 0 {
		return fmt.Errorf("project must be at most %d bytes of text without control characters", maxTextLength)
	}
	return nil
}
