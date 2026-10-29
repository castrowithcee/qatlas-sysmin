package auditlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Query selects the entries Read returns. Zero values leave a filter open; Limit must be positive.
type Query struct {
	// From and To bound the entry time, both inclusive.
	From, To time.Time
	Project  string
	Action   string
	Result   string
	Limit    int
	// After is the Next value of the previous Page; empty starts at the newest entry.
	After string
}

// Page is one slice of the matching action entries, newest first. Next is non-empty when more entries
// match after the last one returned.
type Page struct {
	Entries []Entry
	Next    string
}

// ErrBadCursor reports a Query.After that is not a value Read handed out.
var ErrBadCursor = errors.New("the cursor is not valid")

// cursor names the entry after which a page continues: the day file it is in and its sequence number.
type cursor struct {
	day string
	seq uint64
}

func (c cursor) String() string { return c.day + "/" + strconv.FormatUint(c.seq, 10) }

func parseCursor(text string) (cursor, error) {
	day, seqText, ok := strings.Cut(text, "/")
	seq, err := strconv.ParseUint(seqText, 10, 64)
	if !ok || err != nil || !dayFilePattern.MatchString(day+".jsonl") {
		return cursor{}, ErrBadCursor
	}
	return cursor{day, seq}, nil
}

// Read returns the action entries of dir that match q, newest first, at most q.Limit of them. It only
// reads: it takes no lock and writes nothing, so a line that a concurrent Append has not finished is
// skipped like any other line that does not parse. Retention-cut markers are not action entries and are
// never returned. A missing directory holds no entries.
func Read(dir string, q Query) (Page, error) {
	if q.Limit < 1 {
		return Page{}, errors.New("limit must be positive")
	}
	var after cursor
	if q.After != "" {
		var err error
		if after, err = parseCursor(q.After); err != nil {
			return Page{}, err
		}
	}
	files, err := dayFiles(dir)
	if err != nil {
		return Page{}, err
	}
	fromDay, toDay := "", ""
	if !q.From.IsZero() {
		fromDay = q.From.UTC().Format("2006-01-02")
	}
	if !q.To.IsZero() {
		toDay = q.To.UTC().Format("2006-01-02")
	}

	var page Page
	lastDay := ""
	for i := len(files) - 1; i >= 0; i-- {
		day := dayFilePattern.FindStringSubmatch(files[i])[1]
		switch {
		case toDay != "" && day > toDay, after.day != "" && day > after.day:
			continue
		case fromDay != "" && day < fromDay:
			return page, nil
		}
		data, err := os.ReadFile(filepath.Join(dir, files[i]))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Page{}, fmt.Errorf("read %s: %w", files[i], err)
		}
		lines := splitLines(data)
		for j := len(lines) - 1; j >= 0; j-- {
			var e Entry
			if json.Unmarshal(lines[j], &e) != nil || e.Kind != "" || !q.matches(e) {
				continue
			}
			if after.day == day && e.Seq >= after.seq {
				continue
			}
			if len(page.Entries) == q.Limit {
				page.Next = cursor{lastDay, page.Entries[len(page.Entries)-1].Seq}.String()
				return page, nil
			}
			page.Entries = append(page.Entries, e)
			lastDay = day
		}
	}
	return page, nil
}

func (q Query) matches(e Entry) bool {
	switch {
	case !q.From.IsZero() && e.Time.Before(q.From), !q.To.IsZero() && e.Time.After(q.To):
		return false
	case q.Project != "" && e.Project != q.Project, q.Action != "" && e.Action != q.Action:
		return false
	case q.Result != "" && e.Result != q.Result:
		return false
	}
	return true
}
