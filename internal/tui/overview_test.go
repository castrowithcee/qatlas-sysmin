package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// ovNow is the fixed clock of the overview tests: Wednesday, 28 October 2026, in UTC.
var ovNow = time.Date(2026, 10, 28, 12, 30, 0, 0, time.UTC)

// hostChangeEntry is a host change of the log, minutes before the fixed clock.
func hostChangeEntry(seq uint64, minutes int, action, undoOf string) control.LogEntry {
	return control.LogEntry{Seq: seq, Time: ovNow.Add(-time.Duration(minutes) * time.Minute), Action: "host.change",
		Result: control.LogResultSuccess, Host: &control.LogHost{ActionID: action, JournalID: "j-demo", UndoOf: undoOf}}
}

// overviewFake is a service whose log holds two host changes, the newer one the undo of an older action.
func overviewFake() *fakeBackend {
	fb := newFake()
	fb.audit.entries = []control.LogEntry{hostChangeEntry(2, 30, "user.lock", "user.unlock"), hostChangeEntry(1, 26*60, "user.add-key", "")}
	return fb
}

// calmFake is a service with nothing to do.
func calmFake() *fakeBackend { return newFake() }

// calmModel is an Overview whose admins are all enabled: nothing to do.
func calmModel(t *testing.T, fb *fakeBackend, w, h int) *Model {
	t.Helper()
	m := overviewModel(t, fb, w, h)
	host := m.hostSrc.(*fixtureHost)
	for i := range host.users {
		host.users[i].Enabled = true
	}
	drain(m, m.overviewCmd())
	return m
}

// overviewModel is appModel with the fixed clock, in UTC.
func overviewModel(t *testing.T, fb *fakeBackend, w, h int) *Model {
	t.Helper()
	utcClock(t)
	m := appModel(t, fb, w, h)
	m.now = func() time.Time { return ovNow }
	return m
}

// focusCard moves the focus of the overview to the card with the label.
func focusCard(t *testing.T, m *Model, label string) {
	t.Helper()
	send(m, "1")
	for i, c := range m.overviewCards() {
		if c.label == label {
			m.ovr.zone, m.ovr.cursor[ovCards] = ovCards, i
			return
		}
	}
	t.Fatalf("the overview has no %q card", label)
}

// focusStep moves the focus of the overview to the step whose text contains label.
func focusStep(t *testing.T, m *Model, label string) {
	t.Helper()
	send(m, "1")
	for i, s := range m.overviewSteps() {
		if strings.Contains(s.label, label) {
			m.ovr.zone, m.ovr.cursor[ovSteps] = ovSteps, i
			return
		}
	}
	t.Fatalf("the overview has no %q step: %v", label, m.overviewSteps())
}

func lastRow(m *Model) string {
	rows := strings.Split(plain(m), "\n")
	return rows[len(rows)-1]
}

func TestOverviewShowsHeaderCardsAndSteps(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		m := overviewModel(t, overviewFake(), size[0], size[1])
		view := plain(m)
		for _, want := range []string{"Overview", "Wed, 28 Oct 2026", "Admins", "Not enabled", "Changes 7 d", "Service",
			"qsysmin 0.0.0-test", "Next steps", "Admins to enable: 1", "Last changes", "user.add-key"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: the overview lacks %q:\n%s", size[0], size[1], want, view)
			}
		}
		assertViewFits(t, view, size[0], size[1])
	}
}

func TestOverviewCardsCountTheHost(t *testing.T) {
	m := overviewModel(t, overviewFake(), 80, 24)
	got := map[string]string{}
	for _, c := range m.overviewCards() {
		got[c.label] = c.value
	}
	want := map[string]string{"Admins": "2", "Not enabled": "1", "Changes 7 d": "2", "Service": "reachable"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("card %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestOverviewCardsWithoutDataShowNone(t *testing.T) {
	fb := overviewFake()
	fb.down = errDropped
	m := overviewModel(t, fb, 80, 24)
	m.hostSrc = nil
	drain(m, m.overviewCmd())
	for _, c := range m.overviewCards() {
		switch c.label {
		case "Service":
			if c.value != "unreachable" || c.kind != statusFail {
				t.Errorf("service card = %q (kind %v)", c.value, c.kind)
			}
		default:
			if c.value != "none" || c.kind != statusDim {
				t.Errorf("card %s = %q (kind %v), want a dimmed none", c.label, c.value, c.kind)
			}
		}
	}
	if view := plain(m); !strings.Contains(view, "Last changes") || strings.Contains(view, "user.lock") {
		t.Errorf("the panel shows data without a service:\n%s", view)
	}
	if n := len(fb.audit.calls); n != 0 {
		t.Errorf("the log was read %d times without a service", n)
	}
}

func TestOverviewReadsTheHostChangesOfTheLastWeekOnShowAndReload(t *testing.T) {
	fb := overviewFake()
	m := overviewModel(t, fb, 120, 40)
	if n := len(fb.audit.calls); n != 1 {
		t.Fatalf("the log was read %d times at the start, want once", n)
	}
	if p := fb.audit.calls[0]; p.Action != "host.change" || p.Result != control.LogResultSuccess || p.Limit != control.LogMaxLimit {
		t.Errorf("log request %+v", p)
	}
	for range 3 {
		drain(m, m.statusCmd())
		_, cmd := m.Update(tickMsg{})
		drain(m, cmd)
	}
	if n := len(fb.audit.calls); n != 1 {
		t.Errorf("the log was read %d times after polls, want once", n)
	}
	send(m, "ctrl+r")
	if n := len(fb.audit.calls); n != 2 {
		t.Fatalf("the log was read %d times after a reload, want twice", n)
	}
	if from := fb.audit.calls[1].From; from != ovNow.AddDate(0, 0, -7).Format(time.RFC3339) {
		t.Errorf("the log is read from %s, want seven days before now", from)
	}
	send(m, "3", "1")
	if n := len(fb.audit.calls); n < 4 {
		t.Errorf("showing the Overview again did not read the log: %d calls", n)
	}
}

func TestOverviewListsTheLastChangesNewestFirst(t *testing.T) {
	m := overviewModel(t, overviewFake(), 120, 40)
	view := plain(m)
	lock, key := strings.Index(view, "2026-10-28 12:00  user.lock · undo"), strings.Index(view, "2026-10-27 10:30  user.add-key")
	if lock < 0 || key < 0 || lock > key {
		t.Errorf("the panel lacks the changes or their order:\n%s", view)
	}
}

func TestOverviewStartsOnTheTabLineAndDownReachesTheFirstStep(t *testing.T) {
	m := overviewModel(t, overviewFake(), 120, 40)
	if m.zoneNow() != zoneTabs {
		t.Fatalf("the Overview starts in %v, want the tab line", m.zoneNow())
	}
	send(m, "down")
	if m.zoneNow() != zoneContent || m.ovr.zone != ovSteps || m.ovr.cursor[ovSteps] != 0 {
		t.Fatalf("down reaches %v in zone %v at %d, want the first step", m.zoneNow(), m.ovr.zone, m.ovr.cursor[ovSteps])
	}
	if got := m.overviewCurrent().label; got != "Admins to enable: 1" {
		t.Fatalf("the focused step is %q", got)
	}
	send(m, "enter")
	if m.area != areaHost || m.hst.sub != hostUsers {
		t.Errorf("enter leads to area %v page %d, want Host, Users", m.area, m.hst.sub)
	}
}

func TestOverviewCardsBecomeATextLineWhereBoxesDoNotFit(t *testing.T) {
	m := overviewModel(t, overviewFake(), 41, 24)
	view := plain(m)
	if !strings.Contains(view, "Admins 2  Not enabled 1") || strings.Contains(view, "╭─ Admins") {
		t.Errorf("the cards are not one text line:\n%s", view)
	}
	assertViewFits(t, view, 41, 24)
	m = overviewModel(t, overviewFake(), 80, 24)
	if view := plain(m); !strings.Contains(view, "╭─ Admins") {
		t.Errorf("the cards are not boxes at 80 columns:\n%s", view)
	}
}

func TestOverviewHasNoPermanentHintText(t *testing.T) {
	foreign := newFake()
	foreign.down = &client.PeerError{PID: 4321, UID: 1234}
	fbs := map[string]*fakeBackend{"usual": overviewFake(), "calm": calmFake(), "foreign": foreign}
	for name, fb := range fbs {
		for _, size := range [][2]int{{80, 24}, {120, 40}, {60, 20}} {
			m := overviewModel(t, fb, size[0], size[1])
			for _, key := range []string{"", "tab", "down", "tab", "down"} {
				if key != "" {
					send(m, key)
				}
				rows := strings.Split(plain(m), "\n")
				body := strings.Join(rows[3:len(rows)-1], "\n")
				if strings.Contains(body, "Press ") || strings.Contains(body, "enter to") {
					t.Errorf("%s %dx%d: a hint stands in the overview:\n%s", name, size[0], size[1], body)
				}
			}
		}
	}
}

func TestOverviewStepForEveryStateAndEnterTakesIt(t *testing.T) {
	type state struct {
		name  string
		build func() *fakeBackend
		step  string
		check func(t *testing.T, m *Model, fb *fakeBackend)
	}
	retried := func(t *testing.T, m *Model, _ *fakeBackend) {
		t.Helper()
		if !m.serviceUp() {
			t.Error("enter does not ask the service again")
		}
	}
	states := []state{
		{"unreachable", func() *fakeBackend {
			fb := newFake()
			fb.down = errDropped
			return fb
		}, "Retry", retried},
		{"foreign peer", func() *fakeBackend {
			fb := newFake()
			fb.down = &client.PeerError{PID: 4321, UID: 1234}
			return fb
		}, "Retry · Another process answers at the socket (pid 4321", nil},
		{"no group", func() *fakeBackend {
			fb := newFake()
			fb.down = fmt.Errorf("cannot reach the service: %w", client.ErrSessionGroup)
			return fb
		}, "Restart with group", func(t *testing.T, m *Model, _ *fakeBackend) {
			if !m.regroupAsked || !m.quitting {
				t.Errorf("enter does not quit to restart: regroup %v, quitting %v", m.regroupAsked, m.quitting)
			}
		}},
		{"admin without enablement", overviewFake, "Admins to enable: 1", func(t *testing.T, m *Model, _ *fakeBackend) {
			if m.area != areaHost || m.hst.sub != hostUsers {
				t.Errorf("enter leads to area %v page %d, want Host, Users", m.area, m.hst.sub)
			}
		}},
		{"nothing to do", calmFake, "Nothing to do", func(t *testing.T, m *Model, _ *fakeBackend) {
			if m.area != areaOverview {
				t.Errorf("enter on a step without action leaves the overview for %v", m.area)
			}
		}},
	}
	for _, s := range states {
		t.Run(s.name, func(t *testing.T) {
			fb := s.build()
			m := calmModel(t, fb, 120, 40)
			if s.name == "admin without enablement" {
				m = overviewModel(t, fb, 120, 40)
			}
			if !strings.Contains(plain(m), s.step) {
				t.Fatalf("no %q step:\n%s", s.step, plain(m))
			}
			focusStep(t, m, s.step)
			if s.name == "unreachable" || s.name == "foreign peer" {
				fb.down = nil
				send(m, "enter")
				retried(t, m, fb)
				return
			}
			send(m, "enter")
			s.check(t, m, fb)
		})
	}
}

func TestOverviewSelfUpdateIsAStep(t *testing.T) {
	m := overviewModel(t, calmFake(), 120, 40)
	m.set.upd.phase, m.set.upd.info.Latest = updDone, "9.9.9"
	if view := plain(m); !strings.Contains(view, "Install qsysmin 9.9.9") || !strings.Contains(view, "qsysmin 0.0.0-test → 9.9.9") {
		t.Fatalf("no update step or version line:\n%s", view)
	}
	focusStep(t, m, "Install qsysmin 9.9.9")
	send(m, "enter")
	if m.ask == nil {
		t.Error("enter does not ask before installing")
	}
}

func TestOverviewWithoutOpenStepsShowsNothingToDo(t *testing.T) {
	m := calmModel(t, calmFake(), 80, 24)
	if steps := m.overviewSteps(); len(steps) != 1 || steps[0].label != "Nothing to do" || steps[0].act != nil {
		t.Errorf("steps %v, want the dimmed Nothing to do alone", steps)
	}
}

func TestOverviewEnterOnCardsOpensTheirPages(t *testing.T) {
	for card, want := range map[string]area{"Admins": areaHost, "Not enabled": areaHost, "Changes 7 d": areaActivity} {
		m := overviewModel(t, overviewFake(), 120, 40)
		focusCard(t, m, card)
		send(m, "enter")
		if m.area != want {
			t.Errorf("enter on %s leads to %v, want %v", card, m.area, want)
		}
	}
}
func TestOverviewTabVisitsTheZonesAndLeavesToTheTabLine(t *testing.T) {
	m := overviewModel(t, overviewFake(), 120, 40)
	send(m, "1")
	if m.ovr.zone != ovSteps {
		t.Fatalf("the Overview starts in zone %v, want the steps", m.ovr.zone)
	}
	send(m, "tab")
	if m.zoneNow() != zoneTabs {
		t.Errorf("tab after the steps reaches %v, want the tab line", m.zoneNow())
	}
	send(m, "tab")
	if m.zoneNow() != zoneContent || m.ovr.zone != ovSteps {
		t.Errorf("tab on the tab line reaches %v in zone %v", m.zoneNow(), m.ovr.zone)
	}
	send(m, "shift+tab")
	if m.ovr.zone != ovCards || m.zoneNow() != zoneContent {
		t.Errorf("shift+tab reaches zone %v on %v, want the cards", m.ovr.zone, m.zoneNow())
	}
	send(m, "right", "left", "left")
	if m.ovr.cursor[ovCards] != 0 {
		t.Errorf("the cursor runs past the cards: %d", m.ovr.cursor[ovCards])
	}
	send(m, "shift+tab")
	if m.zoneNow() != zoneTabs {
		t.Errorf("shift+tab on the cards reaches %v, want the tab line", m.zoneNow())
	}
}
func TestOverviewFooterNamesTheKeyOfTheFocusedEntry(t *testing.T) {
	m := overviewModel(t, overviewFake(), 120, 40)
	focusCard(t, m, "Admins")
	if foot := lastRow(m); !strings.Contains(foot, "enter open") || !strings.Contains(foot, "↑/↓ move") {
		t.Errorf("footer on a card: %q", foot)
	}
	m = calmModel(t, calmFake(), 120, 40)
	send(m, "1")
	if foot := lastRow(m); strings.Contains(foot, "enter") {
		t.Errorf("footer on a step without action names enter: %q", foot)
	}
	fb := newFake()
	fb.down = errDropped
	m = overviewModel(t, fb, 120, 40)
	send(m, "1")
	if foot := lastRow(m); !strings.Contains(foot, "enter retry") {
		t.Errorf("footer on the retry step: %q", foot)
	}
}

func TestOverviewInfoDescribesTheFocusedEntry(t *testing.T) {
	m := overviewModel(t, overviewFake(), 120, 40)
	focusCard(t, m, "Not enabled")
	send(m, "i")
	if view := plain(m); !strings.Contains(view, "not in the qsysmin group") {
		t.Errorf("no info for the Not enabled card:\n%s", view)
	}
}

func TestOverviewDropsPartsInOrderAndKeepsTitleAndSteps(t *testing.T) {
	steps := []ovItem{{label: "Admins to enable: 1"}, {label: "Install qsysmin 9.9.9"}}
	rank := func(p ovPlan) int {
		switch {
		case !p.framed:
			return 4
		case !p.version:
			return 3
		case p.line:
			return 2
		case p.changes == 0:
			return 1
		}
		return 0
	}
	last, seen := 0, map[int]bool{}
	for room := 40; room >= 3; room-- {
		p := overviewPlan(room, 80, steps, 3)
		if p.steps < 1 {
			t.Fatalf("room %d: no step shown", room)
		}
		if r := rank(p); r < last {
			t.Errorf("room %d: %+v restores something dropped before", room, p)
		} else {
			last = r
		}
		seen[rank(p)] = true
	}
	for r := 0; r <= 4; r++ {
		if !seen[r] {
			t.Errorf("no room drops exactly down to level %d", r)
		}
	}
}

func TestOverviewInASmallTerminalKeepsTitleAndSteps(t *testing.T) {
	for _, h := range []int{14, 15, 16, 18} {
		m := overviewModel(t, overviewFake(), 80, h)
		view := plain(m)
		for _, want := range []string{"Overview", "Wed, 28 Oct 2026", "Admins to enable: 1"} {
			if !strings.Contains(view, want) {
				t.Errorf("height %d lacks %q:\n%s", h, want, view)
			}
		}
		assertViewFits(t, view, 80, h)
	}
}

func TestOverviewHeaderDateFollowsTheLanguage(t *testing.T) {
	m := overviewModel(t, overviewFake(), 80, 24)
	useLanguage(t, m, i18n.German)
	if view := plain(m); !strings.Contains(view, "Mi., 28. Okt. 2026") || !strings.Contains(view, "Nächste Schritte") {
		t.Errorf("the German overview:\n%s", view)
	}
}

func TestOverviewDistinguishesStatesWithoutColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	view := plain(overviewModel(t, overviewFake(), 80, 24))
	for _, mark := range []string{warnMark} {
		if !strings.Contains(view, mark) {
			t.Errorf("the mark %q is missing:\n%s", mark, view)
		}
	}
	if !strings.Contains(plain(calmModel(t, calmFake(), 80, 24)), "○") {
		t.Error("the mark of a step without action is missing")
	}
}

func TestOverviewIsClickable(t *testing.T) {
	t.Run("card", func(t *testing.T) {
		m := overviewModel(t, overviewFake(), 120, 40)
		m.render()
		clickOn(t, m, "Admins")
		if m.area != areaHost {
			t.Errorf("a click on the Admins card leads to %v, want Host", m.area)
		}
	})
	t.Run("step", func(t *testing.T) {
		m := overviewModel(t, overviewFake(), 120, 40)
		m.render()
		clickOn(t, m, "Admins to enable: 1")
		if m.area != areaHost {
			t.Errorf("a click on the enable step leads to %v, want Host", m.area)
		}
	})
}

func TestOverviewFocusMarkShowsWithoutColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := overviewModel(t, overviewFake(), 120, 40)
	send(m, "1")
	view := plain(m)
	if !slices.ContainsFunc(strings.Split(view, "\n"), func(l string) bool { return strings.Contains(l, "> "+warnMark+" Admins to enable: 1") }) {
		t.Errorf("the focused step has no mark:\n%s", view)
	}
	focusCard(t, m, "Admins")
	if view := plain(m); !strings.Contains(view, "> Admins") || !strings.Contains(view, "╔") {
		t.Errorf("the focused card has no mark:\n%s", view)
	}
}
