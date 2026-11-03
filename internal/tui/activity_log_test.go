package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// utcClock makes the log times of a test read the same everywhere.
func utcClock(t *testing.T) {
	t.Helper()
	before := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = before })
}

// testLogEntries is a log of n entries, newest first, with invented values.
func testLogEntries(n int) []control.LogEntry {
	actions := []string{"config.read", "host.change", "control.status", "control.refuse", "config.apply"}
	start := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	var out []control.LogEntry
	for i := range n {
		uid := uint32(1000)
		if i%3 == 0 {
			uid = 0
		}
		e := control.LogEntry{Seq: uint64(n - i), Time: start.Add(-time.Duration(i) * time.Minute), ClientUID: &uid,
			Action: actions[i%len(actions)], Result: control.LogResultSuccess, DurationMS: int64(3 + i*7), Signed: i%4 != 0}
		if i%5 == 4 {
			e.Result, e.ErrorCode = control.LogResultFailure, "refused"
		}
		out = append(out, e)
	}
	return out
}

func intactChain() control.LogVerifyResult {
	return control.LogVerifyResult{Intact: true, Days: []control.LogDay{
		{Date: "2026-10-27", Entries: 40, Intact: true},
		{Date: "2026-10-28", Entries: 12, Intact: true}}}
}

func brokenChain() control.LogVerifyResult {
	return control.LogVerifyResult{Intact: false, Days: []control.LogDay{
		{Date: "2026-10-27", Entries: 40, Intact: false, Signatures: control.LogSignaturesNotChecked},
		{Date: "2026-10-28", Entries: 12, Intact: true, Signatures: control.LogSignaturesNotChecked}},
		Problems: []control.LogProblem{{Date: "2026-10-27", Seq: 31, Line: 31, Code: control.LogProblemChain},
			{Date: "2026-10-28", Line: 4, Code: control.LogProblemUnparsable}}}
}

// secretValue is the invented secret the tests type; it must never be drawn, copied or reported.
const secretValue = "fake-token-for-tests-only"

// f2 presses the save key, which saves a form.
func f2(m *Model) {
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyF2})
	drain(m, cmd)
}

// activityFake is a service with a short log.
func activityFake() *fakeBackend {
	fb := newFake()
	fb.audit.entries = testLogEntries(12)
	fb.audit.verify = intactChain()
	return fb
}

func TestActivityLoadsEntriesAndShowsAnIntactChain(t *testing.T) {
	utcClock(t)
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		fb := activityFake()
		m := appModel(t, fb, size[0], size[1])
		send(m, "3")
		view := plain(m)
		for _, want := range []string{"Log (12)", "● Chain intact, 52 entries", "2026-10-28 12:00:00", "Config read"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: lacks %q:\n%s", size[0], size[1], want, view)
			}
		}
		// One read of the log is the Overview's at the start.
		if fb.count("log") != 2 || fb.count("log-verify") != 1 {
			t.Errorf("calls = %v", fb.log())
		}
		if strings.Contains(view, "signatures") {
			t.Errorf("%dx%d: names signatures although no entry is signed:\n%s", size[0], size[1], view)
		}
		assertViewFits(t, view, size[0], size[1])
	}
}

func TestActivityNotesOldSignaturesAsNotChecked(t *testing.T) {
	utcClock(t)
	fb := activityFake()
	fb.audit.verify.Days[0].Signatures = control.LogSignaturesNotChecked
	m := appModel(t, fb, 120, 40)
	send(m, "3")
	if view := plain(m); !strings.Contains(view, "● Chain intact, 52 entries · signatures not checked") {
		t.Errorf("old signatures not noted:\n%s", view)
	}
}

func TestActivityShowsABrokenChainWithSymbolAndText(t *testing.T) {
	utcClock(t)
	fb := activityFake()
	fb.audit.verify = brokenChain()
	m := appModel(t, fb, 80, 24)
	send(m, "3")
	view := plain(m)
	if !strings.Contains(view, "⚠ Chain broken · 2026-10-27 entry 31 · hash mismatch") || strings.Contains(view, "Chain intact") {
		t.Errorf("no break shown:\n%s", view)
	}
	assertViewFits(t, view, 80, 24)
	fb.audit.verify = intactChain()
	send(m, "t")
	if fb.count("log-verify") != 2 || !strings.Contains(plain(m), "● Chain intact") {
		t.Errorf("calls %v:\n%s", fb.log(), plain(m))
	}
}

func TestActivityCountsDocumentedDamage(t *testing.T) {
	utcClock(t)
	fb := activityFake()
	fb.audit.verify = intactChain()
	fb.audit.verify.Days[1].Damaged = 1
	m := appModel(t, fb, 120, 40)
	send(m, "3")
	if view := plain(m); !strings.Contains(view, "● Chain intact, 52 entries · unreadable lines documented: 1") {
		t.Errorf("no documented damage shown:\n%s", view)
	}
	useLanguage(t, m, "de")
	send(m, "t")
	if view := plain(m); !strings.Contains(view, "unlesbare Zeilen dokumentiert: 1") {
		t.Errorf("no German documented damage shown:\n%s", view)
	}
}

func TestActivityDoesNotPollTheLog(t *testing.T) {
	fb := activityFake()
	m := appModel(t, fb, 80, 24)
	send(m, "3")
	logs, checks := fb.count("log"), fb.count("log-verify")
	clock := clockModel(m, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
	for range 5 {
		*clock = clock.Add(2 * pollEvery)
		_, cmd := m.Update(tickMsg{})
		drain(m, cmd)
	}
	if fb.count("log") != logs || fb.count("log-verify") != checks {
		t.Errorf("the log was read on a timer: %v", fb.log())
	}
}

func TestActivityFilterSetsTheParametersAndReloads(t *testing.T) {
	utcClock(t)
	fb := activityFake()
	m := appModel(t, fb, 80, 24)
	now := time.Date(2026, 10, 28, 15, 30, 0, 0, time.UTC)
	clockModel(m, now)
	send(m, "3", "f")
	if m.form == nil {
		t.Fatal("no filter form")
	}
	// Period: 7 days; action: the first one; result: failure.
	send(m, "right", "right", "right", "tab", "right", "tab", "right", "right")
	f2(m)
	last := fb.audit.calls[len(fb.audit.calls)-1]
	want := control.LogListParams{From: now.AddDate(0, 0, -7).Format(time.RFC3339), Action: "config.apply",
		Result: control.LogResultFailure, Limit: logPageSize}
	if last != want {
		t.Errorf("params = %+v, want %+v", last, want)
	}
	if view := plain(m); !strings.Contains(view, "Log · 7 days · Config applied · failure") {
		t.Errorf("the filters are not in the title:\n%s", view)
	}
	// The period "today" starts at midnight of the local day.
	send(m, "f", "left")
	f2(m)
	last = fb.audit.calls[len(fb.audit.calls)-1]
	if last.From != "2026-10-28T00:00:00Z" {
		t.Errorf("today = %q", last.From)
	}
}

func TestActivityLoadsFurtherPagesAtTheEnd(t *testing.T) {
	utcClock(t)
	fb := activityFake()
	fb.audit.entries = testLogEntries(250)
	m := appModel(t, fb, 80, 24)
	send(m, "3")
	if got := len(m.act.order); got != logPageSize || m.act.next == "" {
		t.Fatalf("first page: %d entries, next %q", got, m.act.next)
	}
	send(m, "end")
	if got := len(m.act.order); got != 2*logPageSize {
		t.Fatalf("second page: %d entries", got)
	}
	if last := fb.audit.calls[len(fb.audit.calls)-1]; last.After != "100" {
		t.Errorf("after = %q", last.After)
	}
	send(m, "end", "end")
	if got := len(m.act.order); got != 250 || m.act.next != "" {
		t.Errorf("end: %d entries, next %q", got, m.act.next)
	}
	calls := fb.count("log")
	send(m, "end", "end")
	if fb.count("log") != calls {
		t.Error("the log is read past its end")
	}
}

func TestActivityDrawsNoControlSequenceOfAnEntry(t *testing.T) {
	utcClock(t)
	fb := activityFake()
	fb.audit.entries[0].Action = "config.read\x1b]52;c;ZXZpbA==\x07"
	fb.audit.entries[0].ErrorCode = "x\x1b[31m"
	m := appModel(t, fb, 120, 40)
	m.redactor.Add(secretValue)
	fb.audit.entries[1].Action = secretValue
	send(m, "3")
	for range 3 {
		raw := m.render()
		for _, bad := range []string{"\x1b]52", "\x1b[2J", "\x1b[31m", "\x07", secretValue} {
			if strings.Contains(raw, bad) {
				t.Fatalf("%q is drawn:\n%q", bad, raw)
			}
		}
		send(m, "enter", "right")
		send(m, "esc")
	}
	send(m, "ctrl+r")
	send(m, "c")
	if m.status == "" {
		t.Error("nothing copied")
	}
}

func TestActivityKnowsOnlyActionsTheServiceLogs(t *testing.T) {
	for _, a := range logActions {
		if !auditlog.KnownOperation(a) {
			t.Errorf("%q is no action of the log", a)
		}
	}
}

func TestActivityShowsNoProject(t *testing.T) {
	utcClock(t)
	fb := activityFake()
	m := appModel(t, fb, 120, 40)
	send(m, "3")
	view := plain(m)
	if strings.Contains(view, "demo-app") || strings.Contains(view, "Project") {
		t.Errorf("a project is shown:\n%s", view)
	}
	send(m, "enter")
	if view := plain(m); strings.Contains(view, "demo-app") || strings.Contains(view, "Project") {
		t.Errorf("the details show a project:\n%s", view)
	}
	send(m, "esc", "f")
	if view := plain(m); strings.Contains(view, "Project") {
		t.Errorf("the filter offers a project:\n%s", view)
	}
}

func TestActivityShowsAHostChangeWithItsLabel(t *testing.T) {
	utcClock(t)
	fb := activityFake()
	uid := uint32(0)
	fb.audit.entries = []control.LogEntry{{Seq: 7, Time: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), ClientUID: &uid,
		Action: "host.change", Result: control.LogResultSuccess, Signed: true,
		Host: &control.LogHost{ActionID: "user.lock", JournalID: "demo-journal"}}}
	for _, tc := range []struct {
		lang i18n.Lang
		want string
	}{{i18n.English, "Host change"}, {i18n.German, "Host-Änderung"}} {
		t.Run(string(tc.lang), func(t *testing.T) {
			m := appModel(t, fb, 120, 40)
			useLanguage(t, m, tc.lang)
			send(m, "3")
			if view := plain(m); !strings.Contains(view, tc.want) || strings.Contains(view, "host.change") {
				t.Errorf("table lacks %q or shows the raw action:\n%s", tc.want, view)
			}
		})
	}
}

func TestEveryLogActionHasALabelInBothLanguages(t *testing.T) {
	for _, a := range logActions {
		for _, cat := range catalog() {
			if strings.TrimSpace(cat[actionID(a)]) == "" {
				t.Errorf("action %q has no label", a)
			}
		}
	}
}

func TestActivityTextsAreTranslated(t *testing.T) {
	utcClock(t)
	eachLanguage(t, func(t *testing.T) {
		fb := activityFake()
		fb.audit.verify = brokenChain()
		m := appModel(t, fb, 120, 40)
		for _, keys := range [][]string{{"2"}, {"2", "f"}} {
			m2 := appModel(t, fb, 120, 40)
			send(m2, keys...)
			assertViewFits(t, plain(m2), 120, 40)
		}
		send(m, "?", "left")
		for range helpTopics {
			send(m, "left")
		}
	})
}

func TestEveryFooterGroupOfThePagesIsTranslated(t *testing.T) {
	utcClock(t)
	m := appModel(t, activityFake(), 120, 40)
	useLanguage(t, m, "de")
	send(m, "3")
	for _, group := range strings.Split(m.areaKeys(), " · ") {
		if strings.HasPrefix(group, "{") || group == "1-4 area" {
			continue
		}
		if _, ok := catalog()[i18n.German][footerID(group)]; !ok {
			t.Errorf("footer group %q has no German text", group)
		}
	}
}
