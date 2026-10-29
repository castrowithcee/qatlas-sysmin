package auditlog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The self-exec pattern below runs one real Append from a freshly started process, so the concurrent-writer
// test exercises the file lock across processes, not only across goroutines of one.
const (
	workerModeEnv = "SYSMIN_AUDITLOG_TEST_WORKER"
	workerDirEnv  = "SYSMIN_AUDITLOG_TEST_DIR"
)

func TestMain(m *testing.M) {
	if os.Getenv(workerModeEnv) == "1" {
		logger := New(os.Getenv(workerDirEnv), 90)
		if err := logger.Append(workerFields()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func workerFields() Fields {
	return Fields{ClientUID: 1000, Action: OperationDeployRun, Project: "demo", Result: ResultSuccess}
}

// newTestLogger returns a logger in a temp dir whose clock is fixed at t until moveTo advances it.
func newTestLogger(t *testing.T, at time.Time, retentionDays int) (*Logger, func(time.Time)) {
	t.Helper()
	current := at.UTC()
	logger := New(t.TempDir(), retentionDays)
	logger.now = func() time.Time { return current }
	return logger, func(next time.Time) { current = next.UTC() }
}

func mustAppend(t *testing.T, logger *Logger, action string) {
	t.Helper()
	err := logger.Append(Fields{ClientUID: 1000, Action: action, Project: "demo", Result: ResultSuccess,
		Duration: 5 * time.Millisecond})
	if err != nil {
		t.Fatalf("Append(%q) error = %v", action, err)
	}
}

func readLines(t *testing.T, path string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return splitLines(data)
}

func writeLines(t *testing.T, path string, lines [][]byte) {
	t.Helper()
	var buf bytes.Buffer
	for _, line := range lines {
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), fileMode); err != nil {
		t.Fatal(err)
	}
}

func verify(t *testing.T, dir string, checker Checker) Report {
	t.Helper()
	report, err := Verify(dir, checker)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	return report
}

func day(d int) time.Time { return time.Date(2026, 3, d, 10, 0, 0, 0, time.UTC) }

func TestAppendChainsAcrossDays(t *testing.T) {
	logger, moveTo := newTestLogger(t, day(1), 90)
	mustAppend(t, logger, OperationConfigApply)
	mustAppend(t, logger, OperationVaultUnlock)
	moveTo(day(3)) // a day without an action in between
	mustAppend(t, logger, OperationVaultLock)

	if _, err := os.Stat(filepath.Join(logger.dir, "2026-03-02.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("a day without an action must not get a file, stat error = %v", err)
	}
	report := verify(t, logger.dir, nil)
	if report.Broken || len(report.Days) != 2 || report.Days[0].Entries != 2 || report.Days[1].Entries != 1 {
		t.Fatalf("report = %+v, want an intact chain over two days with 2 and 1 entries", report)
	}
	for _, d := range report.Days {
		if d.Unverified != d.Entries || d.SignatureState(report.Checked) != SignatureNotChecked {
			t.Fatalf("day %+v: unsigned entries must count as unverified and not checked", d)
		}
	}
	first := readLines(t, filepath.Join(logger.dir, "2026-03-03.jsonl"))[0]
	var entry Entry
	if err := json.Unmarshal(first, &entry); err != nil || entry.Seq != 3 || entry.ClientUID == nil || *entry.ClientUID != 1000 {
		t.Fatalf("first entry of the second day = %+v (%v), want seq 3 and the client uid", entry, err)
	}
}

func TestVerifyDetectsChangedLine(t *testing.T) {
	logger, _ := newTestLogger(t, day(1), 90)
	for _, a := range []string{OperationConfigApply, OperationVaultUnlock, OperationVaultLock} {
		mustAppend(t, logger, a)
	}
	path := filepath.Join(logger.dir, "2026-03-01.jsonl")
	lines := readLines(t, path)
	lines[1] = bytes.Replace(lines[1], []byte(`"vault.unlock"`), []byte(`"vault.rekey"`), 1)
	writeLines(t, path, lines)

	report := verify(t, logger.dir, nil)
	got := report.Days[0]
	if !report.Broken || got.Date != "2026-03-01" || got.ProblemSeq != 3 {
		t.Fatalf("report = %+v, want a break on 2026-03-01 at sequence 3", report)
	}
}

func TestVerifyDetectsDeletedLine(t *testing.T) {
	logger, _ := newTestLogger(t, day(1), 90)
	for _, a := range []string{OperationConfigApply, OperationVaultUnlock, OperationVaultLock} {
		mustAppend(t, logger, a)
	}
	path := filepath.Join(logger.dir, "2026-03-01.jsonl")
	lines := readLines(t, path)
	writeLines(t, path, append(lines[:1], lines[2:]...))

	report := verify(t, logger.dir, nil)
	got := report.Days[0]
	if !report.Broken || got.ProblemSeq != 3 || got.Missing != 1 {
		t.Fatalf("report = %+v, want a break at sequence 3 with 1 missing entry", report)
	}
}

func TestVerifyDetectsSwappedLines(t *testing.T) {
	logger, _ := newTestLogger(t, day(1), 90)
	for _, a := range []string{OperationConfigApply, OperationVaultUnlock, OperationVaultLock, OperationVaultRekey} {
		mustAppend(t, logger, a)
	}
	path := filepath.Join(logger.dir, "2026-03-01.jsonl")
	lines := readLines(t, path)
	lines[1], lines[2] = lines[2], lines[1]
	writeLines(t, path, lines)

	report := verify(t, logger.dir, nil)
	got := report.Days[0]
	if !report.Broken || got.Date != "2026-03-01" || got.ProblemSeq != 3 {
		t.Fatalf("report = %+v, want a break on 2026-03-01 at sequence 3", report)
	}
}

func TestVerifyDetectsMissingDays(t *testing.T) {
	for _, removed := range []string{"2026-03-01.jsonl", "2026-03-02.jsonl"} {
		logger, moveTo := newTestLogger(t, day(1), 90)
		mustAppend(t, logger, OperationConfigApply)
		moveTo(day(2))
		mustAppend(t, logger, OperationConfigApply)
		moveTo(day(3))
		mustAppend(t, logger, OperationConfigApply)
		if err := os.Remove(filepath.Join(logger.dir, removed)); err != nil {
			t.Fatal(err)
		}
		report := verify(t, logger.dir, nil)
		if !report.Broken {
			t.Fatalf("removing %s: report = %+v, want a break", removed, report)
		}
		for _, d := range report.Days {
			if d.Problem != "" && d.Missing != 1 {
				t.Fatalf("removing %s: day %+v, want 1 missing entry", removed, d)
			}
		}
	}
}

func TestVerifyDetectsWrongGenesisHash(t *testing.T) {
	logger, _ := newTestLogger(t, day(1), 90)
	mustAppend(t, logger, OperationConfigApply)
	mustAppend(t, logger, OperationVaultLock)
	path := filepath.Join(logger.dir, "2026-03-01.jsonl")
	lines := readLines(t, path)
	lines[0] = bytes.Replace(lines[0], []byte(genesisHash), []byte(strings.Repeat("f", 64)), 1)
	writeLines(t, path, lines)

	if report := verify(t, logger.dir, nil); !report.Broken || report.Days[0].ProblemSeq != 1 {
		t.Fatalf("report = %+v, want seq 1 with a wrong genesis hash to break the chain", report)
	}
}

func TestRetentionCutIsNotAGap(t *testing.T) {
	logger, moveTo := newTestLogger(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 5)
	mustAppend(t, logger, "config.apply")
	moveTo(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	mustAppend(t, logger, "config.apply")
	moveTo(time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC))
	mustAppend(t, logger, "vault.lock")
	mustAppend(t, logger, "vault.lock")

	for _, name := range []string{"2026-01-01.jsonl", "2026-01-02.jsonl"} {
		if _, err := os.Stat(filepath.Join(logger.dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should have been removed by retention, stat error = %v", name, err)
		}
	}
	lines := readLines(t, filepath.Join(logger.dir, "2026-01-20.jsonl"))
	if len(lines) != 3 || !bytes.Contains(lines[0], []byte(`"kind":"retention-cut"`)) ||
		!bytes.Contains(lines[0], []byte(`"through_seq":2`)) {
		t.Fatalf("lines = %q, want a cut marker through seq 2 and two entries", lines)
	}
	report := verify(t, logger.dir, nil)
	if report.Broken || len(report.Days) != 1 || report.Days[0].Entries != 2 {
		t.Fatalf("report = %+v, want a documented cut, not a break, and the marker not counted", report)
	}
}

func TestRetentionCutTwiceStaysIntact(t *testing.T) {
	logger, moveTo := newTestLogger(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 5)
	mustAppend(t, logger, "config.apply")
	moveTo(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	mustAppend(t, logger, "config.apply")
	moveTo(time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC))
	mustAppend(t, logger, "config.apply") // cut (seq 3) + entry seq 4
	moveTo(time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC))
	mustAppend(t, logger, "config.apply") // second cut removes every file, marker included

	lines := readLines(t, filepath.Join(logger.dir, "2026-02-15.jsonl"))
	var marker Entry
	if err := json.Unmarshal(lines[0], &marker); err != nil || marker.Seq != 5 {
		t.Fatalf("second cut marker = %+v (%v), want seq 5: the counter must not restart", marker, err)
	}
	if report := verify(t, logger.dir, nil); report.Broken {
		t.Fatalf("report = %+v, want two cuts to stay intact", report)
	}
}

func TestVerifyDetectsGapAfterDocumentedCut(t *testing.T) {
	logger, moveTo := newTestLogger(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 5)
	mustAppend(t, logger, "config.apply")
	moveTo(time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC))
	mustAppend(t, logger, "config.apply")
	moveTo(time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC))
	mustAppend(t, logger, "config.apply") // cut through seq 1, then seq 3 and 4

	if err := os.Remove(filepath.Join(logger.dir, "2026-01-08.jsonl")); err != nil {
		t.Fatal(err)
	}
	if report := verify(t, logger.dir, nil); !report.Broken {
		t.Fatalf("report = %+v, want the undocumented gap after a cut to break the chain", report)
	}
}

func TestVerifyDetectsOldestDayRemovedByHand(t *testing.T) {
	logger, moveTo := newTestLogger(t, day(1), 90)
	mustAppend(t, logger, "config.apply")
	moveTo(day(2))
	mustAppend(t, logger, "config.apply")
	if err := os.Remove(filepath.Join(logger.dir, "2026-03-01.jsonl")); err != nil {
		t.Fatal(err)
	}
	report := verify(t, logger.dir, nil)
	if !report.Broken || len(report.Days) != 1 || report.Days[0].Problem == "" {
		t.Fatalf("report = %+v, want the surviving day to name the problem", report)
	}
}

func TestAppendAcrossDayWithEmptyCurrentFile(t *testing.T) {
	logger, moveTo := newTestLogger(t, day(1), 90)
	mustAppend(t, logger, "config.apply")
	moveTo(day(2))
	if err := os.WriteFile(filepath.Join(logger.dir, "2026-03-02.jsonl"), nil, fileMode); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, logger, "config.apply")
	if report := verify(t, logger.dir, nil); report.Broken {
		t.Fatalf("report = %+v, want an intact chain", report)
	}
}

func TestVerifyMissingDirIsEmptyChain(t *testing.T) {
	report := verify(t, filepath.Join(t.TempDir(), "absent"), nil)
	if report.Broken || len(report.Days) != 0 {
		t.Fatalf("report = %+v, want an intact, empty chain", report)
	}
}

func TestAppendConcurrentGoroutines(t *testing.T) {
	logger := New(t.TempDir(), 90)
	const n = 40
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := logger.Append(workerFields()); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("Append() error = %v", err)
	}
	assertSequence(t, logger.dir, n)
}

func TestAppendConcurrentProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns subprocesses")
	}
	dir := t.TempDir()
	const n = 6
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0])
			cmd.Env = append(os.Environ(), workerModeEnv+"=1", workerDirEnv+"="+dir)
			if out, err := cmd.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("worker process failed: %w: %s", err, out)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	assertSequence(t, dir, n)
}

// assertSequence checks that dir holds exactly want entries numbered 1..want without a gap, and that the
// chain verifies.
func assertSequence(t *testing.T, dir string, want int) {
	t.Helper()
	if report := verify(t, dir, nil); report.Broken {
		t.Fatalf("report = %+v, want an intact chain", report)
	}
	seen := map[uint64]bool{}
	files, err := dayFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		for _, line := range readLines(t, filepath.Join(dir, name)) {
			var e Entry
			if err := json.Unmarshal(line, &e); err != nil {
				t.Fatal(err)
			}
			seen[e.Seq] = true
		}
	}
	for seq := uint64(1); seq <= uint64(want); seq++ {
		if !seen[seq] {
			t.Fatalf("sequence %d is missing among %d entries", seq, len(seen))
		}
	}
	if len(seen) != want {
		t.Fatalf("got %d distinct sequence numbers, want %d", len(seen), want)
	}
}

func TestAppendRefusesBadInput(t *testing.T) {
	good := workerFields()
	cases := map[string]func(*Fields){
		"unknown action":          func(f *Fields) { f.Action = "made.up" },
		"unknown result":          func(f *Fields) { f.Result = "maybe" },
		"failure without code":    func(f *Fields) { f.Result = ResultFailure },
		"success with code":       func(f *Fields) { f.ErrorCode = "denied" },
		"negative duration":       func(f *Fields) { f.Duration = -1 },
		"control char in project": func(f *Fields) { f.Project = "a\nb" },
		"long project":            func(f *Fields) { f.Project = strings.Repeat("p", maxTextLength+1) },
	}
	for name, mutate := range cases {
		dir := t.TempDir()
		f := good
		mutate(&f)
		if err := New(dir, 90).Append(f); err == nil {
			t.Errorf("%s: Append succeeded", name)
		}
		if files, _ := dayFiles(dir); len(files) != 0 {
			t.Errorf("%s: wrote files %v", name, files)
		}
	}
	if err := New(t.TempDir(), 0).Append(good); err == nil {
		t.Error("retention of 0 days accepted")
	}
	if err := New("", 90).Append(good); err == nil {
		t.Error("empty directory accepted")
	}
	failure := good
	failure.Result, failure.ErrorCode = ResultFailure, "permission-denied"
	if err := New(t.TempDir(), 90).Append(failure); err != nil {
		t.Errorf("valid failure refused: %v", err)
	}
}

func TestEntryFieldsAndFileMode(t *testing.T) {
	logger, _ := newTestLogger(t, day(1), 90)
	if err := logger.Append(Fields{ClientUID: 0, Action: OperationApprovalDeny, Project: "demo",
		Result: ResultFailure, ErrorCode: "denied", Duration: 1500 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(logger.dir, "2026-03-01.jsonl")
	line := readLines(t, path)[0]
	var got map[string]any
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"seq": 1.0, "client_uid": 0.0, "action": "approval.deny", "project": "demo",
		"result": "failure", "error_code": "denied", "duration_ms": 1500.0, "mac": "", "prev_hash": genesisHash,
		"time": "2026-03-01T10:00:00Z"}
	if len(got) != len(want) {
		t.Fatalf("entry = %s, want fields %v", line, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("field %s = %v, want %v", k, got[k], v)
		}
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != fileMode {
		t.Errorf("day file mode = %v (%v), want %v", info.Mode().Perm(), err, os.FileMode(fileMode))
	}
}
