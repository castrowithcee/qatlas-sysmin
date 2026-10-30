package auditlog

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// tornLine is the start of an entry, as a crash while it was written leaves it.
const tornLine = `{"seq":3,"time":"2026-03-01T10:00:00Z","client_u`

// appendRaw appends data to path as it is.
func appendRaw(t *testing.T, path, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, fileMode)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(data); err != nil {
		t.Fatal(err)
	}
}

func parse(t *testing.T, raw []byte) Entry {
	t.Helper()
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("line %q does not parse: %v", raw, err)
	}
	return e
}

// assertMarker checks that marker documents damaged and chains from prev with sequence seq.
func assertMarker(t *testing.T, marker Entry, damaged string, prev []byte, seq uint64) {
	t.Helper()
	if marker.Kind != KindDamage || marker.Damage == nil || marker.Damage.LineHash != hashHex([]byte(damaged)) ||
		marker.PrevHash != hashHex(prev) || marker.Seq != seq || marker.ClientUID != nil || marker.Action != "" {
		t.Fatalf("marker = %+v, want a damaged-line marker with seq %d for the damaged line", marker, seq)
	}
}

func TestAppendBridgesADamagedLastLine(t *testing.T) {
	for _, terminated := range []bool{false, true} {
		key := testKey(t, fakeVaultKey)
		logger, _ := signedTestLogger(t, key, day(1))
		mustAppend(t, logger, OperationConfigApply)
		mustAppend(t, logger, OperationVaultUnlock)
		path := filepath.Join(logger.dir, "2026-03-01.jsonl")
		valid := readLines(t, path)
		damaged := tornLine
		if terminated {
			appendRaw(t, path, damaged+"\n")
		} else {
			appendRaw(t, path, damaged)
		}

		mustAppend(t, logger, OperationVaultLock)
		mustAppend(t, logger, OperationConfigRead)

		lines := readLines(t, path)
		if len(lines) != 6 || string(lines[2]) != damaged {
			t.Fatalf("terminated=%v: lines = %q, want the damaged line kept and a marker and two entries after it", terminated, lines)
		}
		marker := parse(t, lines[3])
		assertMarker(t, marker, damaged, valid[1], 3)
		if marker.MAC == "" {
			t.Fatalf("terminated=%v: the marker of a signing logger is not signed", terminated)
		}
		if e := parse(t, lines[4]); e.Seq != 4 || e.PrevHash != hashHex(lines[3]) || e.Action != OperationVaultLock {
			t.Fatalf("terminated=%v: entry after the marker = %+v", terminated, e)
		}

		report := verify(t, logger.dir, key)
		d := report.Days[0]
		if report.Broken || d.Problem != "" || d.Damaged != 1 || d.Entries != 4 || d.Changed != 0 ||
			d.SignatureState(report.Checked) != SignatureValid {
			t.Fatalf("terminated=%v: report = %+v, want an intact day with one documented damage and valid signatures", terminated, report)
		}
		page, err := Read(logger.dir, Query{Limit: 10})
		if err != nil || len(page.Entries) != 4 {
			t.Fatalf("terminated=%v: Read = %+v, %v, want the four action entries only", terminated, page, err)
		}
	}
}

func TestAppendBridgesADamagedLastLineWithoutKey(t *testing.T) {
	logger, _ := newTestLogger(t, day(1), 90)
	mustAppend(t, logger, OperationConfigApply)
	path := filepath.Join(logger.dir, "2026-03-01.jsonl")
	appendRaw(t, path, tornLine)
	mustAppend(t, logger, OperationVaultLock)

	lines := readLines(t, path)
	if marker := parse(t, lines[2]); marker.Kind != KindDamage || marker.MAC != "" {
		t.Fatalf("marker = %+v, want an unsigned damaged-line marker", marker)
	}
	report := verify(t, logger.dir, nil)
	if report.Broken || report.Days[0].Damaged != 1 || report.Days[0].Unverified != 3 {
		t.Fatalf("report = %+v, want one documented damage and the marker counted as unverified", report)
	}
}

func TestAppendBridgesADamagedLastLineAcrossDays(t *testing.T) {
	key := testKey(t, fakeVaultKey)
	logger, moveTo := signedTestLogger(t, key, day(1))
	mustAppend(t, logger, OperationConfigApply)
	first := filepath.Join(logger.dir, "2026-03-01.jsonl")
	valid := readLines(t, first)
	appendRaw(t, first, tornLine)

	moveTo(day(3))
	mustAppend(t, logger, OperationVaultLock)

	if data, err := os.ReadFile(first); err != nil || !bytes.HasSuffix(data, []byte(tornLine)) {
		t.Fatalf("the earlier day file must stay as it was: %q, %v", data, err)
	}
	lines := readLines(t, filepath.Join(logger.dir, "2026-03-03.jsonl"))
	if len(lines) != 2 {
		t.Fatalf("lines = %q, want a marker and an entry", lines)
	}
	assertMarker(t, parse(t, lines[0]), tornLine, valid[0], 2)
	report := verify(t, logger.dir, key)
	if report.Broken || report.Days[0].Damaged != 1 || report.Days[1].Damaged != 0 || report.Days[1].Entries != 1 {
		t.Fatalf("report = %+v, want the damage documented on the day that holds it", report)
	}
}

func TestRetentionRemovesADamagedFile(t *testing.T) {
	logger, moveTo := newTestLogger(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 5)
	mustAppend(t, logger, OperationConfigApply)
	mustAppend(t, logger, OperationConfigApply)
	appendRaw(t, filepath.Join(logger.dir, "2026-01-01.jsonl"), tornLine)

	// Every file expires, the damaged one included: the cut runs through the last entry that parses.
	moveTo(time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC))
	mustAppend(t, logger, OperationVaultLock)
	lines := readLines(t, filepath.Join(logger.dir, "2026-01-20.jsonl"))
	if len(lines) != 2 {
		t.Fatalf("lines = %q, want a cut and an entry", lines)
	}
	if cut := parse(t, lines[0]); cut.Kind != KindCut || cut.Cut.ThroughSeq != 2 || cut.Seq != 3 {
		t.Fatalf("cut = %+v, want a cut through seq 2", cut)
	}
	if report := verify(t, logger.dir, nil); report.Broken {
		t.Fatalf("report = %+v, want an intact chain", report)
	}
}

func TestRetentionRemovesADamagedFileWhoseMarkerSurvives(t *testing.T) {
	logger, moveTo := newTestLogger(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 5)
	mustAppend(t, logger, OperationConfigApply)
	appendRaw(t, filepath.Join(logger.dir, "2026-01-01.jsonl"), tornLine+"\n")
	moveTo(time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC))
	mustAppend(t, logger, OperationConfigApply) // marker seq 2, entry seq 3

	moveTo(time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC))
	mustAppend(t, logger, OperationVaultLock) // removes the damaged day, cut through seq 1
	lines := readLines(t, filepath.Join(logger.dir, "2026-01-08.jsonl"))
	if cut := parse(t, lines[0]); cut.Kind != KindCut || cut.Cut.ThroughSeq != 1 {
		t.Fatalf("cut = %+v, want a cut through seq 1", cut)
	}
	if report := verify(t, logger.dir, nil); report.Broken || len(report.Days) != 2 {
		t.Fatalf("report = %+v, want the surviving marker to continue the documented cut", report)
	}
}

// bridgedLog returns a signed log whose second line was damaged and bridged, its key, and its lines.
func bridgedLog(t *testing.T) (string, *Key, [][]byte) {
	t.Helper()
	key := testKey(t, fakeVaultKey)
	logger, _ := signedTestLogger(t, key, day(1))
	mustAppend(t, logger, OperationConfigApply)
	path := filepath.Join(logger.dir, "2026-03-01.jsonl")
	appendRaw(t, path, tornLine)
	mustAppend(t, logger, OperationVaultLock)
	mustAppend(t, logger, OperationConfigRead)
	return path, key, readLines(t, path)
}

func TestVerifyRejectsDamageThatNoMarkerDocuments(t *testing.T) {
	path, key, lines := bridgedLog(t)
	// lines: entry 1, damaged line, marker 2, entry 3, entry 4
	resign := func(e Entry) []byte {
		raw, err := New("", 1).WithKey(key).encode(e)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	marker := parse(t, lines[2])
	wrongHash := marker
	wrongHash.Damage = &Damage{LineHash: hashHex([]byte("something else"))}
	unlinked := marker
	unlinked.PrevHash = genesisHash
	badMAC := bytes.Replace(lines[2], []byte(`"mac":"`), []byte(`"mac":"0`), 1)
	badMAC = append(badMAC[:len(badMAC)-3], `"}`...)

	cases := map[string]struct {
		lines [][]byte
		code  string
		line  int
	}{
		"middle line without a marker": {[][]byte{lines[0], []byte("not json"), lines[2], lines[3]}, ProblemUnparsable, 2},
		"marker removed":               {[][]byte{lines[0], lines[1], lines[3], lines[4]}, ProblemUnparsable, 2},
		"marker with a wrong hash":     {[][]byte{lines[0], lines[1], resign(wrongHash), lines[3]}, ProblemUnparsable, 2},
		"marker that does not chain":   {[][]byte{lines[0], lines[1], resign(unlinked), lines[3]}, ProblemUnparsable, 2},
		"marker with a bad check":      {[][]byte{lines[0], lines[1], badMAC, lines[3]}, ProblemUnparsable, 2},
		"damage at the very end":       {[][]byte{lines[0], lines[1]}, ProblemUnparsable, 2},
	}
	for name, c := range cases {
		writeLines(t, path, c.lines)
		report := verify(t, filepath.Dir(path), key)
		d := report.Days[0]
		if !report.Broken || d.Damaged != 0 || d.ProblemCode != c.code || d.ProblemLine != c.line {
			t.Errorf("%s: report = %+v, want a break at line %d", name, report, c.line)
		}
	}

	writeLines(t, path, lines)
	if report := verify(t, filepath.Dir(path), key); report.Broken || report.Days[0].Damaged != 1 {
		t.Fatalf("restored: report = %+v, want an intact day with one documented damage", report)
	}
}
