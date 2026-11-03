package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
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

// wheelAreas are the parts of the frame drawn last that the wheel scrolls, in the order they were drawn.
func wheelAreas(m *Model) []kittui.Target {
	var out []kittui.Target
	for _, t := range m.mt.targets {
		if _, ok := m.mt.wheels[t.Zone]; ok && t.Zone != "" && t.Item == "" && t.Action == "" && t.Area == "" && t.Tab == "" {
			out = append(out, t)
		}
	}
	return out
}

// wheelCell is a cell in the wheel area that pick chooses among the areas of the frame, the last one first.
func wheelCell(t *testing.T, m *Model, pick func(kittui.Target) bool) (x, y int) {
	t.Helper()
	m.render()
	areas := wheelAreas(m)
	for i := len(areas) - 1; i >= 0; i-- {
		if r := areas[i].Rect; pick(areas[i]) {
			return r.X + 2, r.Y + r.Height/2
		}
	}
	t.Fatalf("no wheel area:\n%s", plain(m))
	return 0, 0
}

// leftArea picks the wheel areas that start at the left edge.
func leftArea(a kittui.Target) bool { return a.Rect.X == 0 }

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
		{"Activity", []string{"3"}, activityFake, func(m *Model) *dataPage { return &m.act.page }, 12},
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
	m := appModel(t, activityFake(), 120, 40)
	send(m, "3")
	now := wheelClock(m)
	x, y := wheelCell(t, m, leftArea)
	for range 3 {
		notch(m, now, x, y, true)
	}
	if m.zoneNow() != zoneContent || m.act.page.list.cursor != 0 {
		t.Errorf("the wheel up on the first row left it: zone %v, row %d", m.zoneNow(), m.act.page.list.cursor)
	}
}

func TestTheWheelScrollsTheOpenDetailInSteps(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	m.hostSrc = longGroupsHost()
	send(m, "2", "esc", "right", "enter", "enter")
	if !m.hst.groups.page.open {
		t.Fatal("enter did not open the detail")
	}
	m.hst.groups.page.tab = tabDetails
	now := wheelClock(m)
	x, y := wheelCell(t, m, leftArea)
	p := &m.hst.groups.page
	notch(m, now, x, y, true)
	if p.scroll != 0 {
		t.Errorf("the wheel up at the start moved to %d", p.scroll)
	}
	notch(m, now, x, y, false)
	if p.scroll != kittui.WheelTextLines {
		t.Errorf("one notch moved to %d, want %d", p.scroll, kittui.WheelTextLines)
	}
	for range 40 {
		notch(m, now, x, y, false)
	}
	plain(m)
	end := p.scroll
	notch(m, now, x, y, false)
	plain(m)
	if p.scroll != end || end <= kittui.WheelTextLines {
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
	if offset() != kittui.WheelTextLines {
		t.Errorf("%s: one notch moved to %d, want %d", name, offset(), kittui.WheelTextLines)
	}
	for range 60 {
		notch(m, now, x, y, false)
	}
	end := offset()
	notch(m, now, x, y, false)
	if offset() != end || end <= kittui.WheelTextLines {
		t.Errorf("%s: the wheel at the end moved on from %d to %d", name, end, offset())
	}
	notch(m, now, x, y, true)
	if offset() != end-kittui.WheelTextLines {
		t.Errorf("%s: one notch up went to %d, want %d", name, offset(), end-kittui.WheelTextLines)
	}
}

// The parts of the overview share the zone of its content, the one zone the kit names for a notch: the wheel
// over either steps the mark of the part that holds the focus, and the focus stays. Each part is tried with
// the pointer over the other.
func TestTheWheelStepsTheFocusedPartOfTheOverview(t *testing.T) {
	m := overviewModel(t, overviewFake(), 120, 40)
	m.set.upd = updateState{phase: updDone, info: ReleaseInfo{Latest: "v9.9.9"}}
	m.ovr.zone = ovSteps
	zone := m.zoneNow()
	now := wheelClock(m)
	m.render()
	areas := wheelAreas(m)
	if len(areas) != 2 {
		t.Fatalf("%d wheel areas on the overview, want cards and steps", len(areas))
	}
	at := func(a kittui.Target) (int, int) { return a.Rect.X + 2, a.Rect.Y }
	x, y := at(areas[0])
	stepsOf(t, "next steps", m, now, x, y, func() int { return m.ovr.cursor[ovSteps] }, len(m.overviewSteps()))
	if m.ovr.zone != ovSteps || m.zoneNow() != zone || m.ovr.cursor[ovCards] != 0 {
		t.Errorf("the wheel over the steps moved the focus or the cards: %v, %v, %v", m.ovr.zone, m.zoneNow(), m.ovr.cursor)
	}
	m.ovr.zone, m.ovr.cursor = ovCards, [2]int{}
	x, y = at(areas[1])
	stepsOf(t, "cards", m, now, x, y, func() int { return m.ovr.cursor[ovCards] }, len(m.overviewCards()))
	if m.ovr.zone != ovCards || m.ovr.cursor[ovSteps] != 0 {
		t.Errorf("the wheel moved the focus or the steps: %v, %v", m.ovr.zone, m.ovr.cursor)
	}
}

func TestTheWheelRestsWhileAFilterIsTypedAndBehindAQuestion(t *testing.T) {
	m := appModel(t, activityFake(), 120, 40)
	send(m, "3", "/")
	now := wheelClock(m)
	x, y := wheelCell(t, m, leftArea)
	notch(m, now, x, y, false)
	if m.act.page.list.cursor != 0 {
		t.Errorf("the wheel moved the table while filtering: %d", m.act.page.list.cursor)
	}
	send(m, "esc")
	m.screen = screenConfirm
	m.ask = &question{title: "Sure?", consequence: "Nothing.", keys: keyLine(answer("yes", actYes), answer("no", actNo, actBack))}
	notch(m, now, x, y, false)
	if m.act.page.list.cursor != 0 {
		t.Errorf("the wheel moved the table behind a question: %d", m.act.page.list.cursor)
	}
}
