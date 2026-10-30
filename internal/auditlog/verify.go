package auditlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DayReport is one day file's part of a Report.
type DayReport struct {
	Date string
	// Entries counts the action entries of the day; a marker does not count as one.
	Entries int
	// Unverified counts the action entries and markers of the day whose MAC is empty: written without the log
	// key, which never breaks the chain by itself.
	Unverified int
	// Unchecked counts the action entries and markers of the day that carry a MAC nobody checked, because
	// Verify was given no Checker.
	Unchecked int
	// Damaged counts the lines of the day that do not parse but are documented by the damaged-line marker
	// right after them, which may be the first line of the next day file. Such a line does not break the
	// chain.
	Damaged int
	// Changed counts the lines of the day, entries and retention-cut markers alike, whose MAC is malformed
	// or does not match the line. Each one breaks the chain.
	Changed int
	// Missing counts the sequence numbers skipped right before an entry of this day that no retention cut
	// documents: entries deleted from the middle, or a whole day file.
	Missing uint64
	// Problem names the first way this day's entries fail to chain from what precedes them, or fail their
	// check value, or "" when neither happens. ProblemSeq is the sequence number of the entry it concerns,
	// 0 for a line that does not parse.
	Problem    string
	ProblemSeq uint64
	// ProblemCode is the fixed word for the kind of the first problem (one of the Problem constants, ""
	// when there is none) and ProblemLine its 1-based line number within the day file.
	ProblemCode string
	ProblemLine int
}

// The kinds of problem a DayReport names.
const (
	ProblemUnparsable = "unparsable-line"
	ProblemGenesis    = "bad-genesis"
	ProblemMissing    = "missing-entries"
	ProblemChain      = "chain-broken"
	ProblemMAC        = "check-value-mismatch"
)

// Signature states of a day, as SignatureState reports them.
const (
	SignatureValid      = "valid"
	SignatureInvalid    = "invalid"
	SignatureNotChecked = "not checked"
)

// SignatureState summarizes the check values of the day. A day is valid only if every line was checked
// against a key and matched; an unsigned entry or the absence of a key leaves it not checked, and a
// non-matching check value makes it invalid.
func (d DayReport) SignatureState(checked bool) string {
	switch {
	case d.Changed > 0:
		return SignatureInvalid
	case !checked || d.Unverified > 0 || d.Unchecked > 0:
		return SignatureNotChecked
	}
	return SignatureValid
}

// Report is the result of Verify: one row per day file that exists, in date order.
type Report struct {
	Days []DayReport
	// Checked is true when the check values were checked, which takes a Checker.
	Checked bool
	// Broken is true when the chain is broken somewhere that is not a documented retention cut: a changed,
	// swapped or deleted entry, a day file missing from the middle, or a check value that does not match.
	Broken bool
}

// Verify walks every day file under dir in date order and checks the hash chain: each entry's sequence
// number must follow the one before it by exactly one, and its prev_hash must be the SHA-256 of the exact
// previous line, across day boundaries. The very first entry Verify encounters is exempt from that rule
// only two ways: its own sequence number is 1, in which case it is checked against the genesis hash
// instead, or its sequence number is exactly one more than the greatest through_seq of every surviving
// retention-cut marker, which documents that entries up to there were removed on purpose. Any other first
// entry means entries are missing without such a marker. A missing directory is an installation that
// logged nothing yet, which is an intact, empty chain, not an error.
//
// A line that does not parse breaks the chain unless the line right after it, across a day boundary too, is
// a damaged-line marker that documents it: the marker names the SHA-256 of exactly that line, and it holds
// as an entry itself, chaining from the last line before the damaged one that parses and passing its check
// value. A marker that does not hold leaves the damaged line a break as well.
//
// Every line that carries a check value is checked with checker, when there is one: a malformed check value
// or one that does not match counts as changed and breaks the chain, while an empty one only counts as
// unverified. Without a checker a well-formed check value counts as unchecked instead. A check that cannot
// be made at all fails Verify rather than passing a line unchecked.
func Verify(dir string, checker Checker) (Report, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return Report{Checked: checker != nil}, nil
		}
		return Report{}, err
	}
	days, err := readDays(dir)
	if err != nil {
		return Report{}, err
	}

	// A surviving retention-cut marker may sit anywhere among the days that are still there, not only in
	// the very first one: a later cut can remove the day that held an earlier marker along with the day that
	// documented it. The greatest through_seq of every marker still around always matches the sequence
	// number of the first entry Verify can currently see, because retention only ever removes the oldest
	// contiguous run of days.
	var cutThroughSeq uint64
	haveCut := false
	for _, d := range days {
		for _, raw := range d.lines {
			var entry Entry
			if json.Unmarshal(raw, &entry) != nil || entry.Kind != KindCut || entry.Cut == nil {
				continue
			}
			if !haveCut || entry.Cut.ThroughSeq > cutThroughSeq {
				cutThroughSeq, haveCut = entry.Cut.ThroughSeq, true
			}
		}
	}

	// Every well-formed check value is checked at once, before the walk; the walk takes the answers in the
	// same order.
	var results []bool
	if checker != nil {
		var signed [][]byte
		for _, d := range days {
			for _, raw := range d.lines {
				var entry Entry
				if json.Unmarshal(raw, &entry) == nil && entry.MAC != "" && wellFormed(raw) {
					signed = append(signed, raw)
				}
			}
		}
		if len(signed) > 0 {
			results, err = checker.Check(signed)
			if err != nil {
				return Report{}, fmt.Errorf("cannot check the check values: %w", err)
			}
			if len(results) != len(signed) {
				return Report{}, errors.New("cannot check the check values: the checker answered for the wrong number of lines")
			}
		}
	}

	type finding struct {
		code string
		seq  uint64
		text string
	}
	type damagedLine struct {
		day  *DayReport
		line int
	}
	var (
		report        = Report{Checked: checker != nil, Days: make([]DayReport, len(days))}
		previousRaw   []byte
		previousSeq   uint64
		sawFirstEntry bool
		// pending is the damaged line that the marker on the next line documents, if it holds.
		pending *damagedLine
	)
	setProblem := func(day *DayReport, code string, seq uint64, line int, text string) {
		if day.Problem == "" {
			day.Problem, day.ProblemSeq, day.ProblemCode, day.ProblemLine = text, seq, code, line
		}
		report.Broken = true
	}
	for k, d := range days {
		day := &report.Days[k]
		day.Date = d.date
		for i, raw := range d.lines {
			line := i + 1
			entry, ok := parseLine(raw)
			if !ok {
				if documents(nextLine(days, k, i), raw) {
					pending = &damagedLine{day, line}
				} else {
					setProblem(day, ProblemUnparsable, 0, line, "a line does not parse as a log entry")
				}
				continue
			}
			// The problems of a line are collected first, so a damaged line that its marker fails to document
			// is named before the marker.
			var found []finding
			problem := func(code string, seq uint64, text string) { found = append(found, finding{code, seq, text}) }
			if entry.Kind == "" {
				day.Entries++
			}
			switch {
			case !sawFirstEntry && entry.Seq == 1:
				if entry.PrevHash != genesisHash {
					problem(ProblemGenesis, entry.Seq, "the first entry does not chain from the genesis hash")
				}
			case !sawFirstEntry && haveCut && entry.Seq == cutThroughSeq+1:
				// Documented: this is exactly where the retained window's own retention cut says it continues.
			case !sawFirstEntry:
				if haveCut && entry.Seq > cutThroughSeq+1 {
					day.Missing += entry.Seq - cutThroughSeq - 1
				} else if !haveCut && entry.Seq > 1 {
					day.Missing += entry.Seq - 1
				}
				problem(ProblemMissing, entry.Seq, fmt.Sprintf("entries before sequence %d are missing without a retention cut", entry.Seq))
			case entry.Seq != previousSeq+1 || entry.PrevHash != hashHex(previousRaw):
				if entry.Seq > previousSeq+1 {
					day.Missing += entry.Seq - previousSeq - 1
				}
				problem(ProblemChain, entry.Seq, fmt.Sprintf("entry with sequence %d does not chain from the previous entry", entry.Seq))
			}
			countMAC(day, problem, checker != nil, &results, entry, raw)
			if pending != nil {
				if len(found) == 0 {
					pending.day.Damaged++
				} else {
					setProblem(pending.day, ProblemUnparsable, 0, pending.line, "a line does not parse as a log entry, and the damaged-line marker after it does not hold")
				}
				pending = nil
			}
			for _, f := range found {
				setProblem(day, f.code, f.seq, line, f.text)
			}
			sawFirstEntry = true
			previousRaw, previousSeq = raw, entry.Seq
		}
	}
	return report, nil
}

// nextLine returns the line that follows line i of day k in the walk, which is the first line of the next
// day file that has one when line i is the last of its day, or nil when there is none.
func nextLine(days []dayFile, k, i int) []byte {
	if i+1 < len(days[k].lines) {
		return days[k].lines[i+1]
	}
	for _, d := range days[k+1:] {
		if len(d.lines) > 0 {
			return d.lines[0]
		}
	}
	return nil
}

// documents reports whether next is a damaged-line marker that names the hash of exactly damaged. Whether
// the marker holds as an entry is checked where it is walked.
func documents(next, damaged []byte) bool {
	entry, ok := parseLine(next)
	return ok && entry.Kind == KindDamage && entry.Damage != nil && entry.Damage.LineHash == hashHex(damaged)
}

// countMAC counts the check value of one line into its day's report. results holds the checker's answers
// for the well-formed check values not counted yet, in order; this takes the next one when the line has one
// and checked is true.
func countMAC(day *DayReport, problem func(string, uint64, string), checked bool, results *[]bool, entry Entry, raw []byte) {
	switch {
	case entry.MAC == "":
		day.Unverified++
		return
	case wellFormed(raw) && !checked:
		day.Unchecked++
		return
	case wellFormed(raw):
		valid := (*results)[0]
		*results = (*results)[1:]
		if valid {
			return
		}
	}
	day.Changed++
	problem(ProblemMAC, entry.Seq, fmt.Sprintf("entry with sequence %d does not match its check value", entry.Seq))
}

func wellFormed(raw []byte) bool {
	_, _, err := unsignedOf(raw)
	return err == nil
}

type dayFile struct {
	date  string
	lines [][]byte
}

// readDays reads every day file of dir under the lock, which it releases before it returns.
func readDays(dir string) ([]dayFile, error) {
	unlock, err := lockDir(dir)
	if err != nil {
		return nil, err
	}
	defer unlock()

	files, err := dayFiles(dir)
	if err != nil {
		return nil, err
	}
	days := make([]dayFile, 0, len(files))
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		days = append(days, dayFile{date: dayFilePattern.FindStringSubmatch(name)[1], lines: splitLines(data)})
	}
	return days, nil
}
