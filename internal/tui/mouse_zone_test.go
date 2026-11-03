package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// longGroupsHost is the fixture host with enough members in every group for the detail to scroll.
func longGroupsHost() *fixtureHost {
	h := newFixtureHost()
	for i := range h.groups {
		for n := range 60 {
			h.groups[i].Members = append(h.groups[i].Members, fmt.Sprintf("demo-member-%02d", n))
		}
	}
	return h
}

// detailCell is a place inside the detail beside the rows that carries no element of its own.
func detailCell(m *Model) (x, y int) { return m.usable(0) - 3, m.workspaceTop() + 3 }

// rowCell is the cell of the row that shows text.
func rowCell(t *testing.T, m *Model, text string) (x, y int) {
	t.Helper()
	x, y = cellOf(t, m, text)
	return x, y
}

type zoneCase struct {
	name  string
	model func() *Model
	row   string // text that stands on the row to click and nowhere above it
	mark  string // name of that row
	page  func(m *Model) *dataPage
}

func zoneCases(t *testing.T) []zoneCase {
	return []zoneCase{
		{"Groups", func() *Model {
			m := appModel(t, overviewFake(), 120, 40)
			m.hostSrc = longGroupsHost()
			send(m, "2", "esc", "right", "enter")
			return m
		}, "61", "demo-team", func(m *Model) *dataPage { return &m.hst.groups.page }},
	}
}

func TestAClickIntoTheDetailFocusesItAndAClickOnARowBringsTheFocusBack(t *testing.T) {
	for _, c := range zoneCases(t) {
		m := c.model()
		if m.zoneNow() != zoneContent {
			t.Fatalf("%s: starts on %v", c.name, m.zoneNow())
		}
		x, y := detailCell(m)
		clickAt(m, x, y)
		if m.zoneNow() != zoneDetail {
			t.Fatalf("%s: a click into the free area of the detail reaches %v", c.name, m.zoneNow())
		}
		// A click on a row marks it and brings the focus to the rows; it opens nothing.
		x, y = rowCell(t, m, c.row)
		clickAt(m, x, y)
		if m.zoneNow() != zoneContent {
			t.Errorf("%s: a click on a row leaves the focus on %v", c.name, m.zoneNow())
		}
		if name, _ := c.page(m).current(); name != c.mark || c.page(m).open {
			t.Errorf("%s: row %q, open %v after the click", c.name, name, c.page(m).open)
		}
		// A click on the marked row while the detail has the focus only moves the focus.
		m.zone = zoneDetail
		clickAt(m, x, y)
		if m.zoneNow() != zoneContent || c.page(m).open {
			t.Errorf("%s: a click on the marked row from the detail reaches %v (opened something)", c.name, m.zoneNow())
		}
	}
}

func TestAClickOnADetailTabTurnsItAndFocusesTheDetail(t *testing.T) {
	for _, c := range zoneCases(t) {
		m := c.model()
		x, y := cellOf(t, m, "Raw")
		if y <= 3 {
			x, y = lastCell(t, m, "Raw")
		}
		clickAt(m, x, y)
		tab := c.page(m).tab
		if tab != 2 || m.zoneNow() != zoneDetail {
			t.Errorf("%s: a click on the Raw tab gives tab %d on %v", c.name, tab, m.zoneNow())
		}
	}
}

// lastCell is cellOf for the last row that shows text.
func lastCell(t *testing.T, m *Model, text string) (x, y int) {
	t.Helper()
	found := false
	for row, line := range strings.Split(plain(m), "\n") {
		if at := strings.Index(line, text); at >= 0 {
			x, y, found = len([]rune(line[:at])), row, true
		}
	}
	if !found {
		t.Fatalf("no %q:\n%s", text, plain(m))
	}
	return x, y
}

func TestTheWheelOverTheDetailScrollsItWithoutTakingTheFocus(t *testing.T) {
	for _, c := range zoneCases(t) {
		m := c.model()
		clickOn(t, m, "Details")
		m.zone = zoneContent
		_ = wheelClock(m)
		x, y := detailCell(m)
		wheel(m, x, y, tea.MouseWheelDown)
		if m.zoneNow() != zoneContent {
			t.Errorf("%s: the wheel moved the focus to %v", c.name, m.zoneNow())
		}
		if c.page(m).scroll == 0 {
			t.Errorf("%s: the wheel over the detail scrolled nothing", c.name)
		}
		moved := c.page(m).scroll
		wheel(m, x, y, tea.MouseWheelUp)
		if c.page(m).scroll >= moved {
			t.Errorf("%s: the wheel up scrolled %d -> %d", c.name, moved, c.page(m).scroll)
		}
	}
}

func TestAClickOnAFreeAreaOfABarFocusesIt(t *testing.T) {
	m := appModel(t, overviewFake(), 120, 40)
	send(m, "2")
	if m.zoneNow() != zoneContent {
		t.Fatalf("starts on %v", m.zoneNow())
	}
	clickAt(m, 118, 1) // right of the last tab, the row of the tab line
	if got, want := m.zoneNow(), zoneTabs; got != want {
		t.Fatalf("a click on the free area of the tab line reaches %v", got)
	}
	_, row := cellOf(t, m, "Users")
	clickAt(m, 118, row)
	if m.zoneNow() != zoneSubs || m.area != areaHost {
		t.Errorf("a click on the free area of the page strip reaches %v in %v", m.zoneNow(), m.area)
	}
	// A free place of the rows gives them the focus.
	x, y := cellOf(t, m, "demo-")
	clickAt(m, x, y)
	m.zone = zoneTabs
	clickAt(m, 3, m.workspaceTop()+1)
	if m.zoneNow() != zoneContent {
		t.Errorf("a click on the title line of the rows reaches %v", m.zoneNow())
	}
}

func TestTheFooterIsNotClickable(t *testing.T) {
	m := appModel(t, overviewFake(), 120, 40)
	send(m, "3")
	m.zone = zoneDetail
	rows := strings.Split(plain(m), "\n")
	for x := 0; x < 120; x += 7 {
		clickAt(m, x, len(rows)-1)
	}
	if m.zoneNow() != zoneDetail || m.area != areaActivity || m.screen != screenArea {
		t.Errorf("a click on the footer changed zone %v, area %v, screen %v", m.zoneNow(), m.area, m.screen)
	}
}

func TestTheBackgroundOfADialogIsNotClickable(t *testing.T) {
	m, _, _ := hostChangeModel(t, 120, 40)
	drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
	if m.screen != screenConfirm {
		t.Fatalf("the change opens screen %v", m.screen)
	}
	x, y := detailCell(m)
	for _, at := range [][2]int{{x, y}, {3, 0}, {118, 1}, {3, m.workspaceTop() + 3}} {
		clickAt(m, at[0], at[1])
	}
	if m.screen != screenConfirm || m.zoneNow() != zoneContent || m.area != areaHost {
		t.Errorf("a click behind the dialog reached screen %v, zone %v, area %v", m.screen, m.zoneNow(), m.area)
	}
	clickOn(t, m, "Activity")
	if m.area != areaHost || m.screen != screenConfirm {
		t.Errorf("a tab click behind the dialog reached area %v, screen %v", m.area, m.screen)
	}
}

func TestATabClickFromSettingsWithInputAsksFirst(t *testing.T) {
	m, _ := settingsModel(t, newFake(), "general")
	send(m, "down", "down", "down", "enter")
	typed(m, "5")
	send(m, "esc")
	if !m.settingsDirty() {
		t.Fatal("Settings holds no unsaved input")
	}
	clickOn(t, m, "2 Host")
	if m.area != areaSettings || m.screen != screenConfirm || m.leave == nil {
		t.Fatalf("a tab click with unsaved input reaches area %v, screen %v", m.area, m.screen)
	}
	if !strings.Contains(plain(m), "d discard") {
		t.Errorf("not the usual question:\n%s", plain(m))
	}
	// The question is answered by the person first: a click on a tab behind it does nothing.
	clickOn(t, m, "3 Activity")
	if m.area != areaSettings || m.screen != screenConfirm {
		t.Errorf("a tab click behind the question reached area %v, screen %v", m.area, m.screen)
	}
	send(m, "d")
	if m.area != areaHost {
		t.Errorf("discard reaches area %v, want Host", m.area)
	}
}
func TestATabClickEndsAFilterBeingTyped(t *testing.T) {
	m := appModel(t, overviewFake(), 120, 40)
	send(m, "3", "/")
	if !m.typing() {
		t.Fatal("/ starts no filter")
	}
	clickOn(t, m, "1 Overview")
	if m.area != areaOverview || m.typing() || m.act.page.filtering() {
		t.Errorf("a tab click left area %v with the filter typing", m.area)
	}
	send(m, "3")
	if m.typing() {
		t.Error("the filter takes keys again after the way back")
	}
}
