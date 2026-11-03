package auditlog

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// invented64 is a stand-in for a signature of an earlier version; nothing checks it any more.
const invented64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// legacyDay writes one day file in the old line format: actions sysmin no longer knows, a project field and
// signatures on two of three lines. The hash covers the stored bytes, so the lines stay valid without being
// re-encoded.
func legacyDay(t *testing.T, dir string) {
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
		mac := ""
		if i != 2 {
			mac = invented64
		}
		unsigned += fmt.Sprintf(`"result":"success","duration_ms":5,"prev_hash":%q,"mac":%q}`, prev, mac)
		line := []byte(unsigned)
		lines = append(lines, line)
		prev = hashHex(line)
	}
	writeLines(t, filepath.Join(dir, "2026-03-01.jsonl"), lines)
}

func TestSignedAndUnsignedEntriesChainTogether(t *testing.T) {
	logger, _ := newTestLogger(t, day(1), 90)
	legacyDay(t, logger.dir)

	page, err := Read(logger.dir, Query{Limit: 10})
	if err != nil || len(page.Entries) != 3 {
		t.Fatalf("Read = %+v, %v; want the three old entries", page.Entries, err)
	}
	if got := page.Entries[2].Action; got != "deploy.run" {
		t.Errorf("oldest action = %q", got)
	}
	if report := verify(t, logger.dir); report.Broken || report.Days[0].Entries != 3 || report.Days[0].Signed != 2 {
		t.Fatalf("report = %+v, want an intact chain", report)
	}

	// New entries continue the old chain, and the old project never reappears in what is written or shown.
	mustAppend(t, logger, OperationConfigRead)
	if report := verify(t, logger.dir); report.Broken || report.Days[0].Entries != 4 || report.Days[0].Signed != 2 {
		t.Fatalf("report after append = %+v, want an intact chain of old signed and new unsigned entries", report)
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
	// A signature is not checked, but the chain still covers it: changing one breaks the chain.
	path := filepath.Join(logger.dir, "2026-03-01.jsonl")
	lines := readLines(t, path)
	lines[0] = []byte(strings.Replace(string(lines[0]), invented64, strings.Repeat("f", 64), 1))
	writeLines(t, path, lines)
	if report := verify(t, logger.dir); !report.Broken || report.Days[0].ProblemSeq != 2 {
		t.Fatalf("report after changing a signature = %+v, want a break at sequence 2", report)
	}
	if err := (Fields{Action: "deploy.run", Result: ResultSuccess}).Validate(); err == nil {
		t.Error("a removed action is accepted for writing")
	}
}
