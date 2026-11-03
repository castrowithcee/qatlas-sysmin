package auditlog

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// legacyDay writes one day file in the old line format: actions sysmin no longer knows and a project field.
// The hash and the check value cover the stored bytes, so the lines stay valid without being re-encoded.
func legacyDay(t *testing.T, dir string, key *Key) {
	t.Helper()
	prev := genesisHash
	var lines [][]byte
	for i, e := range []struct{ action, project string }{
		{"deploy.run", "demo-app"}, {"credential.use", "demo-app"}, {"config.read", ""},
	} {
		unsigned := fmt.Sprintf(`{"seq":%d,"time":"2026-03-01T10:00:00Z","client_uid":1000,"action":%q,`, i+1, e.action)
		if e.project != "" {
			unsigned += fmt.Sprintf(`"project":%q,`, e.project)
		}
		unsigned += fmt.Sprintf(`"result":"success","duration_ms":5,"prev_hash":%q,"mac":""}`, prev)
		line := []byte(unsigned)
		if i == 1 {
			var err error
			if line, err = key.sign(line); err != nil {
				t.Fatal(err)
			}
		}
		lines = append(lines, line)
		prev = hashHex(line)
	}
	writeLines(t, filepath.Join(dir, "2026-03-01.jsonl"), lines)
}

func TestLegacyEntriesStayReadableAndVerifiable(t *testing.T) {
	key := testKey(t, fakeVaultKey)
	logger, _ := signedTestLogger(t, key, day(1))
	legacyDay(t, logger.dir, key)

	page, err := Read(logger.dir, Query{Limit: 10})
	if err != nil || len(page.Entries) != 3 {
		t.Fatalf("Read = %+v, %v; want the three old entries", page.Entries, err)
	}
	if got := page.Entries[2].Action; got != "deploy.run" {
		t.Errorf("oldest action = %q", got)
	}
	if report := verify(t, logger.dir, key); report.Broken || report.Days[0].Entries != 3 || report.Days[0].Unverified != 2 {
		t.Fatalf("report = %+v, want an intact chain", report)
	}

	// New entries continue the old chain, and the old project never reappears in what is written or shown.
	mustAppend(t, logger, OperationConfigRead)
	if report := verify(t, logger.dir, key); report.Broken || report.Days[0].Entries != 4 {
		t.Fatalf("report after append = %+v", report)
	}
	for _, line := range readLines(t, filepath.Join(logger.dir, "2026-03-01.jsonl"))[3:] {
		if strings.Contains(string(line), "project") {
			t.Errorf("new line carries a project: %s", line)
		}
	}

	for _, action := range []string{"deploy.run", "credential.use"} {
		page, err := Read(logger.dir, Query{Limit: 10, Action: action})
		if err != nil || len(page.Entries) != 1 || page.Entries[0].Action != action {
			t.Errorf("Read(%s) = %+v, %v", action, page.Entries, err)
		}
	}
	if err := (Fields{Action: "deploy.run", Result: ResultSuccess}).Validate(); err == nil {
		t.Error("a removed action is accepted for writing")
	}
}
