package auditlog

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeVaultKey is an invented stand-in for the key the vault will hand over.
var fakeVaultKey = []byte("invented-vault-key-for-tests-0001")

func testKey(t *testing.T, vaultKey []byte) *Key {
	t.Helper()
	key, err := DeriveKey(vaultKey)
	if err != nil {
		t.Fatalf("DeriveKey() error = %v", err)
	}
	return key
}

func signedTestLogger(t *testing.T, key *Key, at time.Time) (*Logger, func(time.Time)) {
	t.Helper()
	logger, moveTo := newTestLogger(t, at, 90)
	return logger.WithKey(key), moveTo
}

// TestSignedEntriesFollowTheDefinition pins the check value to its documented definition: the hex
// HMAC-SHA256, under the key HKDF derives from the vault key, of the line with an empty mac, while the next
// entry's prev_hash is still the SHA-256 of the whole stored line, check value included.
func TestSignedEntriesFollowTheDefinition(t *testing.T) {
	key := testKey(t, fakeVaultKey)
	logger, _ := signedTestLogger(t, key, day(1))
	mustAppend(t, logger, OperationConfigApply)
	mustAppend(t, logger, OperationVaultLock)
	if !logger.Signs() {
		t.Fatal("a logger with a key does not sign")
	}

	derived, err := hkdf.Key(sha256.New, fakeVaultKey, nil, "sysmin audit log v1", 32)
	if err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, filepath.Join(logger.dir, "2026-03-01.jsonl"))
	var prev []byte
	for i, line := range lines {
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatal(err)
		}
		unsigned := bytes.Replace(line, []byte(`"mac":"`+e.MAC+`"`), []byte(`"mac":""`), 1)
		h := hmac.New(sha256.New, derived)
		h.Write(unsigned)
		if want := hex.EncodeToString(h.Sum(nil)); e.MAC != want {
			t.Fatalf("line %d mac = %s, want %s", i, e.MAC, want)
		}
		if i > 0 && e.PrevHash != hashHex(prev) {
			t.Fatalf("line %d prev_hash is not the hash of the whole stored line", i)
		}
		prev = line
	}
	report := verify(t, logger.dir, key)
	if report.Broken || !report.Checked || report.Days[0].SignatureState(report.Checked) != SignatureValid {
		t.Fatalf("report = %+v, want a valid signed day", report)
	}
}

func TestDeriveKey(t *testing.T) {
	a, b := testKey(t, fakeVaultKey), testKey(t, fakeVaultKey)
	other := testKey(t, []byte("another-invented-vault-key-0002"))
	if !bytes.Equal(a.key, b.key) || bytes.Equal(a.key, other.key) || bytes.Equal(a.key, fakeVaultKey) {
		t.Fatal("derivation must be stable per vault key and differ from it and from other keys")
	}
	before := append([]byte(nil), fakeVaultKey...)
	testKey(t, fakeVaultKey)
	if !bytes.Equal(before, fakeVaultKey) {
		t.Fatal("DeriveKey changed its input")
	}
	if _, err := DeriveKey(nil); err == nil {
		t.Fatal("DeriveKey(nil) succeeded")
	}
	a.Clear()
	if _, err := a.Check([][]byte{[]byte("{}")}); err == nil {
		t.Fatal("a cleared key still checks")
	}
}

func TestVerifyDetectsWrongKeyAndTamperedMAC(t *testing.T) {
	key := testKey(t, fakeVaultKey)
	logger, _ := signedTestLogger(t, key, day(1))
	mustAppend(t, logger, OperationConfigApply)
	mustAppend(t, logger, OperationVaultLock)

	wrong := testKey(t, []byte("another-invented-vault-key-0002"))
	report := verify(t, logger.dir, wrong)
	if !report.Broken || report.Days[0].Changed != 2 || report.Days[0].SignatureState(true) != SignatureInvalid {
		t.Fatalf("report under another key = %+v, want both entries invalid", report)
	}

	path := filepath.Join(logger.dir, "2026-03-01.jsonl")
	lines := readLines(t, path)
	last := lines[1]
	i := len(last) - len(`"}`) - 1
	flipped := byte('0')
	if last[i] == '0' {
		flipped = '1'
	}
	lines[1] = append(append([]byte(nil), last[:i]...), append([]byte{flipped}, last[i+1:]...)...)
	writeLines(t, path, lines)
	report = verify(t, logger.dir, key)
	if !report.Broken || report.Days[0].Changed != 1 || report.Days[0].ProblemSeq != 2 {
		t.Fatalf("report = %+v, want a changed check value at sequence 2", report)
	}
}

func TestSignedLineChangeWithoutKeyStillBreaksChain(t *testing.T) {
	key := testKey(t, fakeVaultKey)
	logger, _ := signedTestLogger(t, key, day(1))
	for i := 0; i < 3; i++ {
		mustAppend(t, logger, OperationConfigApply)
	}
	path := filepath.Join(logger.dir, "2026-03-01.jsonl")
	lines := readLines(t, path)
	lines[0] = bytes.Replace(lines[0], []byte(`"demo"`), []byte(`"demx"`), 1)
	writeLines(t, path, lines)
	if report := verify(t, logger.dir, nil); !report.Broken {
		t.Fatalf("report = %+v, want the chain to catch a change without a key", report)
	}
}

func TestUnsignedEntriesAreNotCheckedButDoNotBreak(t *testing.T) {
	key := testKey(t, fakeVaultKey)
	logger, _ := signedTestLogger(t, key, day(1))
	mustAppend(t, logger, OperationConfigApply)
	unsigned := logger.WithKey(nil)
	mustAppend(t, unsigned, OperationVaultLock)
	mustAppend(t, logger, OperationVaultUnlock)

	for _, checker := range []Checker{key, nil} {
		report := verify(t, logger.dir, checker)
		d := report.Days[0]
		if report.Broken || d.Entries != 3 || d.Unverified != 1 || d.Changed != 0 ||
			d.SignatureState(report.Checked) != SignatureNotChecked {
			t.Fatalf("checker %v: report = %+v, want an intact chain with one unverified entry", checker != nil, report)
		}
	}
	if report := verify(t, logger.dir, nil); report.Days[0].Unchecked != 2 {
		t.Fatalf("report = %+v, want 2 signed entries unchecked without a key", report)
	}
}

type failingChecker struct{}

func (failingChecker) Check([][]byte) ([]bool, error) { return nil, errors.New("synthetic failure") }

type shortChecker struct{}

func (shortChecker) Check([][]byte) ([]bool, error) { return nil, nil }

func TestVerifyFailsClosed(t *testing.T) {
	key := testKey(t, fakeVaultKey)
	logger, _ := signedTestLogger(t, key, day(1))
	mustAppend(t, logger, OperationConfigApply)
	for _, checker := range []Checker{failingChecker{}, shortChecker{}} {
		if _, err := Verify(logger.dir, checker); err == nil {
			t.Fatalf("Verify with %T succeeded", checker)
		}
	}
}

func TestSignedCutMarkerIsChecked(t *testing.T) {
	key := testKey(t, fakeVaultKey)
	logger, moveTo := signedTestLogger(t, key, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	logger.retentionDays = 5
	mustAppend(t, logger, OperationConfigApply)
	moveTo(time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC))
	mustAppend(t, logger, OperationConfigApply)

	report := verify(t, logger.dir, key)
	if report.Broken || report.Days[0].SignatureState(true) != SignatureValid {
		t.Fatalf("report = %+v, want a valid day including the signed marker", report)
	}
	path := filepath.Join(logger.dir, "2026-01-20.jsonl")
	lines := readLines(t, path)
	lines[0] = bytes.Replace(lines[0], []byte(`"through_seq":1`), []byte(`"through_seq":0`), 1)
	writeLines(t, path, lines)
	if report := verify(t, logger.dir, key); !report.Broken {
		t.Fatalf("report = %+v, want an edited marker to break", report)
	}
}

func TestSignedLogHoldsNoKeyMaterial(t *testing.T) {
	key := testKey(t, fakeVaultKey)
	logger, _ := signedTestLogger(t, key, day(1))
	mustAppend(t, logger, OperationCredentialUse)
	data, err := os.ReadFile(filepath.Join(logger.dir, "2026-03-01.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{fakeVaultKey, key.key, []byte(hex.EncodeToString(key.key))} {
		if bytes.Contains(data, secret) {
			t.Fatal("the log file holds key material")
		}
	}
	if strings.Contains(string(data), "invented-vault-key") {
		t.Fatal("the log file names the vault key")
	}
}
