package auditlog

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The files under testdata/compat are logs in the format qsysmin wrote before the kit took over the
// mechanics, with invented values only:
//
//	chain   entries over days with no file between them, one of them a host change
//	damage  torn lines, one with its marker on the same day and one with its marker on the next
//	cut     a retention cut whose window still holds older days
//	legacy  lines of an earlier version with actions qsysmin no longer writes and a signature member
//
// They must stay readable and verifiable, and new entries must continue their chains.

// copyFixture copies the day files of variant whose date is not after through (all when "") into a fresh
// directory.
func copyFixture(t *testing.T, variant, through string) string {
	t.Helper()
	src := filepath.Join("testdata", "compat", variant)
	files, err := filepath.Glob(filepath.Join(src, "*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatalf("fixture %s: %v (%v)", variant, files, err)
	}
	dir := t.TempDir()
	for _, f := range files {
		name := filepath.Base(f)
		if through != "" && strings.TrimSuffix(name, ".jsonl") > through {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, fileMode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const fileMode = 0o600

func TestCompatibleLogsVerify(t *testing.T) {
	for variant, want := range map[string][]DayReport{
		"chain": {
			{Date: "2026-03-01", Entries: 2, Unverified: 2},
			{Date: "2026-03-03", Entries: 1, Unverified: 1},
			{Date: "2026-03-05", Entries: 2, Unverified: 2},
		},
		"damage": {
			{Date: "2026-03-01", Entries: 3, Unverified: 4, Damaged: 2},
			{Date: "2026-03-02", Entries: 1, Unverified: 2},
			{Date: "2026-03-03", Entries: 1, Unverified: 1},
		},
		"cut": {
			{Date: "2026-03-15", Entries: 1, Unverified: 1},
			{Date: "2026-04-10", Entries: 1, Unverified: 2},
			{Date: "2026-04-11", Entries: 1, Unverified: 1},
		},
		"legacy": {
			{Date: "2026-03-01", Entries: 3, Unverified: 1, Unchecked: 2},
			{Date: "2026-03-02", Entries: 1, Unverified: 1},
		},
	} {
		report, err := Verify(copyFixture(t, variant, ""))
		if err != nil {
			t.Fatalf("%s: %v", variant, err)
		}
		if report.Broken || report.Checked || len(report.Days) != len(want) {
			t.Errorf("%s: report = %+v, want an intact chain of %d days", variant, report, len(want))
			continue
		}
		for i, w := range want {
			if report.Days[i] != w {
				t.Errorf("%s: day %d = %+v, want %+v", variant, i, report.Days[i], w)
			}
		}
	}
}

func TestCompatibleLogsContinue(t *testing.T) {
	for variant, at := range map[string]time.Time{
		"chain":  time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
		"damage": time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC),
		"cut":    time.Date(2026, 4, 12, 10, 0, 0, 0, time.UTC),
		"legacy": time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC),
	} {
		dir := copyFixture(t, variant, "")
		log, err := newLog(dir, func() time.Time { return at })
		if err != nil {
			t.Fatal(err)
		}
		if err := log.Append(Fields{ClientUID: 1000, Action: OperationConfigRead, Result: ResultSuccess}.entry()); err != nil {
			t.Fatalf("%s: %v", variant, err)
		}
		if report, err := Verify(dir); err != nil || report.Broken {
			t.Errorf("%s: report after Append = %+v, %v", variant, report, err)
		}
	}
}

func TestLegacyLinesRead(t *testing.T) {
	dir := copyFixture(t, "legacy", "")
	page, err := Read(dir, Query{Limit: 10})
	if err != nil || len(page.Records) != 4 {
		t.Fatalf("Read = %+v, %v; want the four entries", page.Records, err)
	}
	if got := page.Records[3]; got.Seq != 1 || got.Entry.Action != "deploy.run" || got.Entry.MAC == "" {
		t.Errorf("oldest = %+v, want the signed deploy.run entry", got)
	}
	if got := page.Records[0]; got.Seq != 4 || got.Entry.MAC != "" {
		t.Errorf("newest = %+v, want the unsigned entry of the second day", got)
	}
	page, err = Read(dir, Query{Limit: 10, Action: "credential.use"})
	if err != nil || len(page.Records) != 1 || page.Records[0].Seq != 2 {
		t.Errorf("Read(credential.use) = %+v, %v", page.Records, err)
	}
}

func TestReadSkipsMarkersAndPages(t *testing.T) {
	dir := copyFixture(t, "damage", "")
	first, err := Read(dir, Query{Limit: 3})
	if err != nil || len(first.Records) != 3 || first.Next == "" {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	rest, err := Read(dir, Query{Limit: 3, After: first.Next})
	if err != nil || len(rest.Records) != 2 || rest.Next != "" {
		t.Fatalf("second page = %+v, %v", rest, err)
	}
	for _, r := range append(first.Records, rest.Records...) {
		if r.Kind != "" {
			t.Errorf("Read returned the marker %+v", r)
		}
	}
	if _, err := Read(dir, Query{Limit: 3, After: "nonsense"}); err != ErrBadCursor {
		t.Errorf("a bad cursor gives %v, want ErrBadCursor", err)
	}
}

// macSuffix ends every line the kit writes without a key.
const macSuffix = `,"mac":""}`

var prevHashMember = regexp.MustCompile(`"prev_hash":"[0-9a-f]{64}"`)

// TestNewLinesKeepTheStoredLayout checks that new lines are the lines this program wrote before the kit took
// over, member for member and byte for byte, apart from the empty signature member the kit ends every line
// with. The fixture lines were written by the earlier implementation.
func TestNewLinesKeepTheStoredLayout(t *testing.T) {
	host := &Host{ActionID: "demo.set", JournalID: "20260305T030405.000000Z-0a1b2c"}
	for _, tc := range []struct {
		name    string
		through string // the fixture days that precede the new lines
		day     string
		at      time.Time
		fields  []Fields
	}{
		{"first entry of a day after a gap", "2026-03-01", "2026-03-03", time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC),
			[]Fields{{ClientUID: 1000, Action: OperationControlStatus, Result: ResultSuccess, Duration: 5 * time.Millisecond}}},
		{"host change and root refusal", "2026-03-03", "2026-03-05", time.Date(2026, 3, 5, 8, 0, 0, 0, time.UTC),
			[]Fields{
				{ClientUID: 1000, Action: OperationHostChange, Result: ResultSuccess, Host: host, Duration: 40 * time.Millisecond},
				{ClientUID: 1000, Action: OperationControlRefuse, Result: ResultFailure, ErrorCode: "not-root"},
			}},
	} {
		dir := copyFixture(t, "chain", tc.through)
		now := tc.at
		log, err := newLog(dir, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		for i, f := range tc.fields {
			now = tc.at.Add(time.Duration(i) * time.Hour)
			if err := log.Append(f.entry()); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
		}
		got, err := os.ReadFile(filepath.Join(dir, tc.day+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(filepath.Join("testdata", "compat", "chain", tc.day+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		gotLines, wantLines := bytes.Split(bytes.TrimSpace(got), []byte("\n")), bytes.Split(bytes.TrimSpace(want), []byte("\n"))
		if len(gotLines) != len(wantLines) {
			t.Fatalf("%s: %d lines, want %d", tc.name, len(gotLines), len(wantLines))
		}
		for i := range gotLines {
			line := strings.TrimSuffix(string(gotLines[i]), macSuffix) + "}"
			if string(gotLines[i]) == line {
				t.Errorf("%s: line %d does not end with the empty signature member", tc.name, i+1)
			}
			if i > 0 {
				// A line after the first chains from the first new line, which is longer by the signature
				// member than the stored one.
				line = prevHashMember.ReplaceAllString(line, `"prev_hash":""`)
				wantLines[i] = prevHashMember.ReplaceAll(wantLines[i], []byte(`"prev_hash":""`))
			}
			if line != string(wantLines[i]) {
				t.Errorf("%s: line %d =\n%s\nwant\n%s", tc.name, i+1, line, wantLines[i])
			}
		}
		if info, err := os.Stat(filepath.Join(dir, tc.day+".jsonl")); err != nil || info.Mode().Perm() != fileMode {
			t.Errorf("%s: day file mode = %v (%v), want %v", tc.name, info.Mode().Perm(), err, os.FileMode(fileMode))
		}
	}
}

func TestRootClientIsWritten(t *testing.T) {
	dir := t.TempDir()
	logger, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Append(Fields{ClientUID: 0, Action: OperationControlRefuse, Result: ResultFailure, ErrorCode: "denied"}); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	data, err := os.ReadFile(files[0])
	if err != nil || !strings.Contains(string(data), `"client_uid":0,"action":"control.refuse"`) {
		t.Fatalf("line = %s (%v), want client_uid 0 recorded", data, err)
	}
}
