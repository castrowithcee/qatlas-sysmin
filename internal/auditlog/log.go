package auditlog

import (
	"fmt"
	"time"

	kit "github.com/castrowithcee/qatlas-kit/auditlog"
)

const (
	// DefaultDir is where the service keeps its log files.
	DefaultDir = "/var/lib/qsysmin/logs"
	// RetentionDays is how long a day file is kept.
	RetentionDays = 90

	lockName = ".lock"
)

// The kit types qsysmin's callers work with.
type (
	// Record is one parsed line: its sequence number, time and Entry.
	Record = kit.Record[Entry]
	// Report is the result of Verify.
	Report = kit.Report
	// DayReport is one day file's part of a Report.
	DayReport = kit.DayReport
)

// ErrBadCursor reports a Query.After that is not a value Read handed out.
var ErrBadCursor = kit.ErrBadCursor

// Logger appends entries to the day files of one directory.
type Logger struct{ log *kit.Log[Entry] }

// Open returns a logger for dir. It touches no file: the directory and its files are created on the first
// Append. A line that does not parse is documented by a marker instead of refusing the next entry.
func Open(dir string) (*Logger, error) {
	log, err := newLog(dir, nil)
	if err != nil {
		return nil, err
	}
	return &Logger{log: log}, nil
}

// newLog is the kit log with qsysmin's options; now is its clock, nil for the system clock.
func newLog(dir string, now func() time.Time) (*kit.Log[Entry], error) {
	log, err := kit.New(kit.Options[Entry]{
		Dir: dir, LockName: lockName, Validate: Entry.Validate,
		RetentionDays: RetentionDays, TornLine: kit.TornLineDocument, Now: now,
	})
	if err != nil {
		return nil, fmt.Errorf("open the action log: %w", err)
	}
	return log, nil
}

// Append writes one entry for a completed action. A returned error means the entry might not be durable; the
// caller reports it and never changes the result of the action.
func (l *Logger) Append(f Fields) error { return l.log.Append(f.entry()) }

// Query selects the action entries Read returns. Zero values leave a filter open; Limit must be positive.
type Query struct {
	// From and To bound the entry time, both inclusive.
	From, To time.Time
	Action   string
	Result   string
	Limit    int
	// After is the Next value of the previous Page; empty starts at the newest entry.
	After string
}

// Page is one slice of the matching action entries, newest first. Next is non-empty when more match.
type Page struct {
	Records []Record
	Next    string
}

// Read returns the action entries of dir that match q, newest first.
func Read(dir string, q Query) (Page, error) {
	log, err := newLog(dir, nil)
	if err != nil {
		return Page{}, err
	}
	page, err := log.Read(kit.Query[Entry]{
		From: q.From, To: q.To, Limit: q.Limit, After: q.After,
		Match: func(r Record) bool {
			return (q.Action == "" || r.Entry.Action == q.Action) && (q.Result == "" || r.Entry.Result == q.Result)
		},
	})
	return Page{Records: page.Records, Next: page.Next}, err
}

// Verify checks the hash chain of the day files of dir. No key is held, so the signature of a line that an
// earlier version wrote is not checked and the day counts it as unchecked.
func Verify(dir string) (Report, error) {
	log, err := newLog(dir, nil)
	if err != nil {
		return Report{}, err
	}
	return log.Verify(nil)
}

// ProblemDetail is the sentence for the first problem of a day, or "" when it has none.
func ProblemDetail(d DayReport) string {
	switch d.Problem {
	case kit.ProblemNone:
		return ""
	case kit.ProblemUnparsable:
		return "a line does not parse as a log entry"
	case kit.ProblemGenesis:
		return "the first entry does not chain from the genesis hash"
	case kit.ProblemMissing:
		return fmt.Sprintf("entries before sequence %d are missing without a retention cut", d.ProblemSeq)
	case kit.ProblemChain:
		return fmt.Sprintf("entry with sequence %d does not chain from the previous entry", d.ProblemSeq)
	}
	return string(d.Problem)
}
