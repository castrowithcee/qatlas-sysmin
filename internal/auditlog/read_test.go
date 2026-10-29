package auditlog

import (
	"errors"
	"testing"
	"time"
)

// readFixture writes seven entries over three days: seq 1-3 on 2030-03-01, 4-5 on 03-02, 6-7 on 03-03.
func readFixture(t *testing.T) string {
	t.Helper()
	l, moveTo := newTestLogger(t, time.Date(2030, 3, 1, 8, 0, 0, 0, time.UTC), 90)
	add := func(at time.Time, f Fields) {
		moveTo(at)
		f.ClientUID = 1000
		if err := l.Append(f); err != nil {
			t.Fatal(err)
		}
	}
	day := func(d, h int) time.Time { return time.Date(2030, 3, d, h, 0, 0, 0, time.UTC) }
	add(day(1, 8), Fields{Action: OperationConfigRead, Project: "demo-a", Result: ResultSuccess})
	add(day(1, 9), Fields{Action: OperationConfigApply, Project: "demo-a", Result: ResultFailure, ErrorCode: "invalid"})
	add(day(1, 10), Fields{Action: OperationConfigRead, Project: "demo-b", Result: ResultSuccess})
	add(day(2, 8), Fields{Action: OperationDeployRun, Project: "demo-b", Result: ResultSuccess})
	add(day(2, 9), Fields{Action: OperationConfigRead, Project: "demo-a", Result: ResultSuccess})
	add(day(3, 8), Fields{Action: OperationConfigRead, Project: "demo-a", Result: ResultSuccess})
	add(day(3, 9), Fields{Action: OperationConfigApply, Project: "demo-b", Result: ResultFailure, ErrorCode: "locked"})
	return l.dir
}

func seqs(p Page) []uint64 {
	var out []uint64
	for _, e := range p.Entries {
		out = append(out, e.Seq)
	}
	return out
}

func sameSeqs(got, want []uint64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestReadFilters(t *testing.T) {
	dir := readFixture(t)
	at := func(d, h int) time.Time { return time.Date(2030, 3, d, h, 0, 0, 0, time.UTC) }
	tests := []struct {
		name string
		q    Query
		want []uint64
	}{
		{"all, newest first", Query{Limit: 100}, []uint64{7, 6, 5, 4, 3, 2, 1}},
		{"project", Query{Limit: 100, Project: "demo-b"}, []uint64{7, 4, 3}},
		{"action", Query{Limit: 100, Action: OperationConfigApply}, []uint64{7, 2}},
		{"result", Query{Limit: 100, Result: ResultFailure}, []uint64{7, 2}},
		{"from inclusive", Query{Limit: 100, From: at(2, 9)}, []uint64{7, 6, 5}},
		{"to inclusive", Query{Limit: 100, To: at(2, 8)}, []uint64{4, 3, 2, 1}},
		{"range", Query{Limit: 100, From: at(1, 9), To: at(2, 8)}, []uint64{4, 3, 2}},
		{"combined", Query{Limit: 100, Project: "demo-a", Result: ResultSuccess, From: at(2, 0)}, []uint64{6, 5}},
		{"nothing matches", Query{Limit: 100, Project: "other"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := Read(dir, tt.q)
			if err != nil || !sameSeqs(seqs(page), tt.want) || page.Next != "" {
				t.Fatalf("Read = %v, next %q, %v; want %v", seqs(page), page.Next, err, tt.want)
			}
		})
	}
}

func TestReadPagesAcrossDayBoundaries(t *testing.T) {
	dir := readFixture(t)
	var got []uint64
	after := ""
	pages := 0
	for {
		page, err := Read(dir, Query{Limit: 2, After: after})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, seqs(page)...)
		pages++
		if page.Next == "" {
			break
		}
		after = page.Next
	}
	if want := []uint64{7, 6, 5, 4, 3, 2, 1}; !sameSeqs(got, want) || pages != 4 {
		t.Fatalf("pages = %d, seqs = %v", pages, got)
	}
	// A filtered page continues with the same filter.
	page, err := Read(dir, Query{Limit: 1, Project: "demo-a"})
	if err != nil || !sameSeqs(seqs(page), []uint64{6}) || page.Next == "" {
		t.Fatalf("first = %v, %q, %v", seqs(page), page.Next, err)
	}
	page, err = Read(dir, Query{Limit: 10, Project: "demo-a", After: page.Next})
	if err != nil || !sameSeqs(seqs(page), []uint64{5, 2, 1}) || page.Next != "" {
		t.Fatalf("rest = %v, %q, %v", seqs(page), page.Next, err)
	}
}

func TestReadRejectsBadInput(t *testing.T) {
	dir := readFixture(t)
	if _, err := Read(dir, Query{}); err == nil {
		t.Fatal("limit 0 was accepted")
	}
	for _, after := range []string{"x", "2030-03-01", "2030-03-01/x", "../etc/1", "2030-03-01/-1"} {
		if _, err := Read(dir, Query{Limit: 1, After: after}); !errors.Is(err, ErrBadCursor) {
			t.Errorf("cursor %q: err = %v", after, err)
		}
	}
	page, err := Read(dir+"/missing", Query{Limit: 1})
	if err != nil || len(page.Entries) != 0 {
		t.Fatalf("missing dir = %+v, %v", page, err)
	}
}

func TestReadSkipsCutMarkers(t *testing.T) {
	l, moveTo := newTestLogger(t, time.Date(2030, 1, 1, 8, 0, 0, 0, time.UTC), 2)
	mustAppend(t, l, OperationConfigRead)
	moveTo(time.Date(2030, 1, 10, 8, 0, 0, 0, time.UTC))
	mustAppend(t, l, OperationConfigRead) // removes day one and writes a cut marker first
	page, err := Read(l.dir, Query{Limit: 10})
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Seq != 3 {
		t.Fatalf("page = %+v, %v", page, err)
	}
}
