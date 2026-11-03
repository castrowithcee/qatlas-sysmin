package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// notch is one notch of the wheel at a place, with the commands it asks for run; the clock moves on first so
// that no notch is taken for the release half of the one before.
func notch(m *Model, now *time.Time, x, y int, up bool) {
	*now = now.Add(time.Second)
	button := tea.MouseWheelDown
	if up {
		button = tea.MouseWheelUp
	}
	_, cmd := m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: button})
	drain(m, cmd)
}

// wheelCell is a cell in the wheel area that pick chooses among the areas of the frame, the last one first.
func wheelCell(t *testing.T, m *Model, pick func(h wheelHit) bool) (x, y int) {
	t.Helper()
	m.render()
	for i := len(m.wheelHits) - 1; i >= 0; i-- {
		if h := m.wheelHits[i]; pick(h) {
			return h.x0 + 2, h.y0 + (h.y1-h.y0)/2
		}
	}
	t.Fatalf("no wheel area:\n%s", plain(m))
	return 0, 0
}

// leftArea picks the wheel areas that start at the left edge.
func leftArea(h wheelHit) bool { return h.x0 == 0 }

// stepsOf checks the wheel over a surface with a mark: a notch up at the first entry and a notch down at the
// last move nothing, and a notch moves it by one.
func stepsOf(t *testing.T, name string, m *Model, now *time.Time, x, y int, cursor func() int, n int) {
	t.Helper()
	notch(m, now, x, y, true)
	if cursor() != 0 {
		t.Errorf("%s: the wheel up on the first entry went to %d", name, cursor())
	}
	notch(m, now, x, y, false)
	if cursor() != 1 {
		t.Errorf("%s: one notch down went to %d, want 1", name, cursor())
	}
	for range n + 2 {
		notch(m, now, x, y, false)
	}
	if cursor() != n-1 {
		t.Errorf("%s: the wheel down ended on %d, want the last entry %d", name, cursor(), n-1)
	}
	notch(m, now, x, y, true)
	if cursor() != n-2 {
		t.Errorf("%s: one notch up went to %d, want %d", name, cursor(), n-2)
	}
}

func TestTheWheelStepsTheTableOfEveryDataPageWhateverHasTheFocus(t *testing.T) {
	utcClock(t)
	cases := []struct {
		name string
		keys []string
		fake func() *fakeBackend
		page func(m *Model) *dataPage
		rows int
	}{
		{"Access", []string{"3"}, accessFake, func(m *Model) *dataPage { return &m.acc.page }, 2},
		{"Activity", []string{"2"}, libFake, func(m *Model) *dataPage { return &m.act.page }, 12},
		{"Library", []string{"4"}, libFake, func(m *Model) *dataPage { return &m.lib.cred.page }, 4},
	}
	for _, c := range cases {
		for _, size := range [][2]int{{80, 24}, {120, 40}} {
			m := appModel(t, c.fake(), size[0], size[1])
			send(m, c.keys...)
			now := wheelClock(m)
			p := c.page(m)
			m.toBar()
			zone := m.zoneNow()
			p.scroll = 4
			x, y := wheelCell(t, m, leftArea)
			name := fmt.Sprintf("%s %dx%d", c.name, size[0], size[1])
			stepsOf(t, name, m, now, x, y, func() int { return p.list.cursor }, c.rows)
			if m.zoneNow() != zone {
				t.Errorf("%s: the wheel moved the focus from %v to %v", name, zone, m.zoneNow())
			}
			if p.scroll != 0 {
				t.Errorf("%s: a new row kept the detail scrolled to %d", name, p.scroll)
			}
		}
	}
}

func TestTheWheelNeverLeavesTheTableForTheBar(t *testing.T) {
	m := appModel(t, accessFake(), 120, 40)
	send(m, "3")
	now := wheelClock(m)
	x, y := wheelCell(t, m, leftArea)
	for range 3 {
		notch(m, now, x, y, true)
	}
	if m.zoneNow() != zoneContent || m.acc.page.list.cursor != 0 {
		t.Errorf("the wheel up on the first row left it: zone %v, row %d", m.zoneNow(), m.acc.page.list.cursor)
	}
}

func TestTheWheelScrollsTheOpenDetailInSteps(t *testing.T) {
	m := appModel(t, longAccessFake(), 80, 24)
	send(m, "3", "down", "enter")
	if !m.acc.page.open {
		t.Fatal("enter did not open the detail")
	}
	m.acc.page.tab = tabDetails
	now := wheelClock(m)
	x, y := wheelCell(t, m, leftArea)
	p := &m.acc.page
	notch(m, now, x, y, true)
	if p.scroll != 0 {
		t.Errorf("the wheel up at the start moved to %d", p.scroll)
	}
	notch(m, now, x, y, false)
	if p.scroll != wheelText {
		t.Errorf("one notch moved to %d, want %d", p.scroll, wheelText)
	}
	for range 40 {
		notch(m, now, x, y, false)
	}
	plain(m)
	end := p.scroll
	notch(m, now, x, y, false)
	plain(m)
	if p.scroll != end || end <= wheelText {
		t.Errorf("the wheel at the end moved on from %d to %d", end, p.scroll)
	}
	if !p.open {
		t.Error("the wheel closed the detail")
	}
}

func longLog(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	return b.String()
}

// textSteps checks the wheel over a text area: three lines a notch, stopping at both ends.
func textSteps(t *testing.T, name string, m *Model, now *time.Time, x, y int, offset func() int) {
	t.Helper()
	plain(m)
	notch(m, now, x, y, true)
	if offset() != 0 {
		t.Errorf("%s: the wheel up at the start moved to %d", name, offset())
	}
	notch(m, now, x, y, false)
	if offset() != wheelText {
		t.Errorf("%s: one notch moved to %d, want %d", name, offset(), wheelText)
	}
	for range 60 {
		notch(m, now, x, y, false)
	}
	end := offset()
	notch(m, now, x, y, false)
	if offset() != end || end <= wheelText {
		t.Errorf("%s: the wheel at the end moved on from %d to %d", name, end, offset())
	}
	notch(m, now, x, y, true)
	if offset() != end-wheelText {
		t.Errorf("%s: one notch up went to %d, want %d", name, offset(), end-wheelText)
	}
}

func TestTheWheelStepsEachZoneOfTheOverviewForItself(t *testing.T) {
	fb := overviewFake()
	fb.unlockMode = control.UnlockHost
	m := overviewModel(t, fb, 120, 40)
	m.ovr.zone = ovSteps
	zone := m.zoneNow()
	now := wheelClock(m)
	m.render()
	if len(m.wheelHits) != 2 {
		t.Fatalf("%d wheel areas on the overview, want cards and steps", len(m.wheelHits))
	}
	cards, steps := m.wheelHits[0], m.wheelHits[1]
	at := func(h wheelHit) (int, int) { return h.x0 + 2, h.y0 }
	x, y := at(steps)
	stepsOf(t, "next steps", m, now, x, y, func() int { return m.ovr.cursor[ovSteps] }, len(m.overviewSteps()))
	if m.ovr.zone != ovSteps || m.zoneNow() != zone {
		t.Errorf("the wheel over the steps moved the focus: %v, %v", m.ovr.zone, m.zoneNow())
	}
	if m.ovr.cursor[ovCards] != 0 {
		t.Errorf("the wheel over the steps moved another zone: %v", m.ovr.cursor)
	}
	x, y = at(cards)
	stepsOf(t, "cards", m, now, x, y, func() int { return m.ovr.cursor[ovCards] }, len(m.overviewCards()))
	if m.ovr.zone != ovSteps {
		t.Errorf("the overview zone is %v, want the steps", m.ovr.zone)
	}
}

func TestTheWheelRestsWhileAFilterIsTypedAndBehindAQuestion(t *testing.T) {
	m := appModel(t, accessFake(), 120, 40)
	send(m, "3", "/")
	now := wheelClock(m)
	x, y := wheelCell(t, m, leftArea)
	notch(m, now, x, y, false)
	if m.acc.page.list.cursor != 0 {
		t.Errorf("the wheel moved the table while filtering: %d", m.acc.page.list.cursor)
	}
	send(m, "esc")
	m.screen = screenConfirm
	m.ask = &question{title: "Sure?", consequence: "Nothing.", keys: "y yes · n/{back} no"}
	notch(m, now, x, y, false)
	if m.acc.page.list.cursor != 0 {
		t.Errorf("the wheel moved the table behind a question: %d", m.acc.page.list.cursor)
	}
}
