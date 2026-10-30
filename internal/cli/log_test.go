package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
)

// seedLog writes entries to dir; the logger clock is not injectable from here, so every entry lands on the
// current UTC day and the tests use a single day file.
func seedLog(t *testing.T, dir string, signed bool, actions ...string) string {
	t.Helper()
	logger := auditlog.New(dir, auditlog.DefaultRetentionDays)
	if signed {
		key, err := auditlog.DeriveKey([]byte("invented-vault-key-for-tests-0001"))
		if err != nil {
			t.Fatal(err)
		}
		logger = logger.WithKey(key)
	}
	for _, action := range actions {
		err := logger.Append(auditlog.Fields{ClientUID: 1000, Action: action, Project: "demo",
			Result: auditlog.ResultSuccess})
		if err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("day files = %v (%v), want exactly one", files, err)
	}
	return files[0]
}

type verifyRow struct {
	Day        string `json:"day"`
	Entries    int    `json:"entries"`
	Chain      string `json:"chain"`
	Damaged    int    `json:"damaged"`
	Signatures string `json:"signatures"`
	Detail     string `json:"detail"`
}

func verifyJSON(t *testing.T, args ...string) (int, []verifyRow, string) {
	t.Helper()
	code, stdout, stderr := runCLI(t, append([]string{"log", "verify", "--format", "json"}, args...)...)
	var rows []verifyRow
	if stdout != "" {
		if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
			t.Fatalf("stdout %q is no JSON list: %v", stdout, err)
		}
	}
	return code, rows, stderr
}

func TestLogVerifyIntactSignedLogReportsSignaturesNotChecked(t *testing.T) {
	dir := t.TempDir()
	seedLog(t, dir, true, auditlog.OperationConfigApply, auditlog.OperationVaultLock)

	code, rows, stderr := verifyJSON(t, "--dir", dir)
	if code != exitOK || stderr != "" || len(rows) != 1 {
		t.Fatalf("code = %d, rows = %v, stderr = %q", code, rows, stderr)
	}
	if rows[0].Entries != 2 || rows[0].Chain != "intact" || rows[0].Signatures != "not checked" {
		t.Errorf("row = %+v, want 2 entries, intact chain, signatures not checked", rows[0])
	}
}

func TestLogVerifyUnsignedLogIsIntact(t *testing.T) {
	dir := t.TempDir()
	seedLog(t, dir, false, auditlog.OperationConfigApply, auditlog.OperationVaultLock)
	code, rows, _ := verifyJSON(t, "--dir", dir)
	if code != exitOK || len(rows) != 1 || rows[0].Chain != "intact" || rows[0].Signatures != "not checked" {
		t.Errorf("code = %d, rows = %v", code, rows)
	}
}

func TestLogVerifyBrokenChain(t *testing.T) {
	dir := t.TempDir()
	path := seedLog(t, dir, true, auditlog.OperationConfigApply, auditlog.OperationVaultUnlock, auditlog.OperationVaultLock)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(data), "vault.unlock", "vault.rekey", 1)
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}

	code, rows, stderr := verifyJSON(t, "--dir", dir)
	if code != exitRuntime || !strings.Contains(stderr, "chain is broken") {
		t.Fatalf("code = %d, stderr = %q, want a runtime error", code, stderr)
	}
	if len(rows) != 1 || rows[0].Chain != "broken" || !strings.Contains(rows[0].Detail, "sequence 3") {
		t.Errorf("rows = %+v, want a broken day naming sequence 3", rows)
	}
}

func TestLogVerifyCountsDocumentedDamage(t *testing.T) {
	dir := t.TempDir()
	path := seedLog(t, dir, false, auditlog.OperationConfigApply)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"seq":2,"time":"20`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	seedLog(t, dir, false, auditlog.OperationVaultLock)

	code, rows, stderr := verifyJSON(t, "--dir", dir)
	if code != exitOK || stderr != "" || len(rows) != 1 || rows[0].Chain != "intact" || rows[0].Damaged != 1 || rows[0].Entries != 2 {
		t.Fatalf("code = %d, rows = %+v, stderr = %q, want an intact day with one documented damage", code, rows, stderr)
	}
	if _, stdout, _ := runCLI(t, "log", "verify", "--dir", dir); !strings.Contains(stdout, "DAMAGED") {
		t.Errorf("the table has no damaged column:\n%s", stdout)
	}
}

func TestLogVerifyTableAndRange(t *testing.T) {
	dir := t.TempDir()
	seedLog(t, dir, false, auditlog.OperationConfigApply)

	code, stdout, stderr := runCLI(t, "log", "verify", "--dir", dir)
	if code != exitOK || stderr != "" || !strings.Contains(stdout, "intact") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if code, rows, _ := verifyJSON(t, "--dir", dir, "--from", "2999-01-01"); code != exitOK || len(rows) != 0 {
		t.Errorf("--from in the future: code = %d, rows = %v, want none", code, rows)
	}
	if code, rows, _ := verifyJSON(t, "--dir", dir, "--to", "2000-01-01"); code != exitOK || len(rows) != 0 {
		t.Errorf("--to in the past: code = %d, rows = %v, want none", code, rows)
	}
	if code, rows, _ := verifyJSON(t, "--dir", dir, "--from", "2000-01-01", "--to", "2999-01-01"); code != exitOK || len(rows) != 1 {
		t.Errorf("wide range: code = %d, rows = %v, want one", code, rows)
	}
}

func TestLogVerifyMissingDirIsEmptyAndIntact(t *testing.T) {
	code, rows, stderr := verifyJSON(t, "--dir", filepath.Join(t.TempDir(), "absent"))
	if code != exitOK || stderr != "" || len(rows) != 0 {
		t.Errorf("code = %d, rows = %v, stderr = %q", code, rows, stderr)
	}
}

func TestLogVerifyUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--from", "yesterday"},
		{"--to", "2026-13-40"},
		{"--from", "2026-02-01", "--to", "2026-01-01"},
		{"extra"},
		{"--key", "x"},
	} {
		code, stdout, stderr := runCLI(t, append([]string{"log", "verify"}, args...)...)
		if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "sysmin: usage: ") {
			t.Errorf("args %v: code = %d, stdout = %q, stderr = %q", args, code, stdout, stderr)
		}
	}
}
