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
	// Entries counts the action entries of the day; a retention-cut marker does not count as one.
	Entries int
	// Unverified counts the action entries of the day whose MAC is empty: written without the log key,
	// which never breaks the chain by itself.
	Unverified int
	// Unchecked counts the action entries of the day that carry a MAC nobody checked, because Verify was
	// given no Checker.
	Unchecked int
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

	var (
		report        = Report{Checked: checker != nil}
		previousRaw   []byte
		previousSeq   uint64
		sawFirstEntry bool
	)
	for _, d := range days {
		day := DayReport{Date: d.date}
		line := 0
		problem := func(code string, seq uint64, text string) {
			if day.Problem == "" {
				day.Problem, day.ProblemSeq, day.ProblemCode, day.ProblemLine = text, seq, code, line
			}
			report.Broken = true
		}
		for i, raw := range d.lines {
			line = i + 1
			var entry Entry
			if err := json.Unmarshal(raw, &entry); err != nil {
				problem(ProblemUnparsable, 0, "a line does not parse as a log entry")
				continue
			}
			if entry.Kind != KindCut {
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
			countMAC(&day, problem, checker != nil, &results, entry, raw)
			sawFirstEntry = true
			previousRaw, previousSeq = raw, entry.Seq
		}
		report.Days = append(report.Days, day)
	}
	return report, nil
}

// countMAC counts the check value of one line into its day's report. results holds the checker's answers
// for the well-formed check values not counted yet, in order; this takes the next one when the line has one
// and checked is true.
func countMAC(day *DayReport, problem func(string, uint64, string), checked bool, results *[]bool, entry Entry, raw []byte) {
	action := entry.Kind != KindCut
	switch {
	case entry.MAC == "":
		if action {
			day.Unverified++
		}
		return
	case wellFormed(raw) && !checked:
		if action {
			day.Unchecked++
		}
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
