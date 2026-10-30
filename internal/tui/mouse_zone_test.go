package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// longAccessFake is the Access fake with enough changes on sample-app for the detail to scroll.
func longAccessFake() *fakeBackend {
	fb := accessFake()
	var changes []control.ApprovalChange
	for i := range 40 {
		changes = append(changes, control.ApprovalChange{Path: fmt.Sprintf("images[%d].ref", i), Old: "registry.example.test/web:1", New: "registry.example.test/web:2"})
	}
	fb.details["sample-app"] = changes
	return fb
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
	row   string // text of the second row
	scrl  func(m *Model) int
}

func zoneCases(t *testing.T) []zoneCase {
	return []zoneCase{
		{"Projects", func() *Model { return projectsModel(t, projectsFake(), 120, 40) }, "demo-db", func(m *Model) int { return m.prj.pane.view.offset() }},
		{"Access", func() *Model {
			m := appModel(t, longAccessFake(), 120, 40)
			send(m, "4")
			return m
		}, "sample-app", func(m *Model) int { return m.acc.page.scroll }},
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
		if c.name == "Projects" && (m.prj.list.cursor != 1 || m.prj.focusDetail) {
			t.Errorf("%s: cursor %d, focusDetail %v after the click", c.name, m.prj.list.cursor, m.prj.focusDetail)
		}
		if c.name == "Access" && (m.acc.page.list.cursor != 1 || m.acc.page.open) {
			t.Errorf("%s: cursor %d, open %v after the click", c.name, m.acc.page.list.cursor, m.acc.page.open)
		}
		// A click on the marked row while the detail has the focus only moves the focus.
		m.zone = zoneDetail
		clickAt(m, x, y)
		if m.zoneNow() != zoneContent || (m.prj != nil && m.prj.focusDetail) || m.acc.page.open {
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
		tab := m.acc.page.tab
		if c.name == "Projects" {
			tab = int(m.prj.tab)
		}
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
		if c.name == "Projects" {
			// The file is short: a low screen makes the pane scroll.
			m = projectsModel(t, projectsFake(), 120, 24)
			m.prj.tab = projTabRaw
		} else {
			clickOn(t, m, "Details")
			m.zone = zoneContent
		}
		_ = wheelClock(m)
		x, y := detailCell(m)
		wheel(m, x, y, tea.MouseWheelDown)
		if m.zoneNow() != zoneContent {
			t.Errorf("%s: the wheel moved the focus to %v", c.name, m.zoneNow())
		}
		if c.scrl(m) == 0 {
			t.Errorf("%s: the wheel over the detail scrolled nothing", c.name)
		}
		moved := c.scrl(m)
		wheel(m, x, y, tea.MouseWheelUp)
		if c.scrl(m) >= moved {
			t.Errorf("%s: the wheel up scrolled %d -> %d", c.name, moved, c.scrl(m))
		}
	}
}

func TestAClickOnAFreeAreaOfABarFocusesIt(t *testing.T) {
	m := appModel(t, libFake(), 120, 40)
	send(m, "5")
	if m.zoneNow() != zoneContent {
		t.Fatalf("starts on %v", m.zoneNow())
	}
	clickAt(m, 118, 1) // right of the last tab, the row of the tab line
	if got, want := m.zoneNow(), zoneTabs; got != want {
		t.Fatalf("a click on the free area of the tab line reaches %v", got)
	}
	_, row := cellOf(t, m, "Credentials")
	clickAt(m, 118, row)
	if m.zoneNow() != zoneSubs || m.area != areaLibrary {
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
	m := appModel(t, accessFake(), 120, 40)
	send(m, "4")
	m.zone = zoneDetail
	rows := strings.Split(plain(m), "\n")
	for x := 0; x < 120; x += 7 {
		clickAt(m, x, len(rows)-1)
	}
	if m.zoneNow() != zoneDetail || m.area != areaAccess || m.screen != screenArea {
		t.Errorf("a click on the footer changed zone %v, area %v, screen %v", m.zoneNow(), m.area, m.screen)
	}
}

func TestTheBackgroundOfADialogIsNotClickable(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 40)
	send(m, "d")
	if m.screen != screenConfirm {
		t.Fatalf("d opens screen %v", m.screen)
	}
	x, y := detailCell(m)
	for _, at := range [][2]int{{x, y}, {3, 0}, {118, 1}, {3, m.workspaceTop() + 3}} {
		clickAt(m, at[0], at[1])
	}
	if m.screen != screenConfirm || m.zoneNow() != zoneContent || m.area != areaProjects {
		t.Errorf("a click behind the dialog reached screen %v, zone %v, area %v", m.screen, m.zoneNow(), m.area)
	}
	clickOn(t, m, "Access")
	if m.area != areaProjects || m.screen != screenConfirm {
		t.Errorf("a tab click behind the dialog reached area %v, screen %v", m.area, m.screen)
	}
}

func TestATabClickFromTheLogAndFromTheDetailLeavesLikeEsc(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 40)
	send(m, "o")
	if m.prj.log == nil {
		t.Skip("no log in the fake")
	}
	clickOn(t, m, "4 Access")
	if m.area != areaAccess || m.screen != screenArea || m.zoneNow() != zoneContent {
		t.Errorf("a tab click from the log reaches area %v, screen %v, zone %v", m.area, m.screen, m.zoneNow())
	}
	send(m, "2")
	if m.prj.log != nil {
		t.Error("the log stayed open after the way back")
	}
	m.zone = zoneDetail
	clickOn(t, m, "3 Activity")
	if m.area != areaActivity || m.zoneNow() != zoneContent {
		t.Errorf("a tab click from the detail reaches area %v, zone %v", m.area, m.zoneNow())
	}
}

func TestATabClickFromTheGuidedSetupWithInputAsksFirst(t *testing.T) {
	m := flowModel(t, projectsFake(), 120, 40)
	flowFill(t, m, "name", "demo-new")
	if !m.flowDirty() {
		t.Fatal("the setup holds no unsaved input")
	}
	clickOn(t, m, "4 Access")
	if m.area != areaProjects || m.screen != screenConfirm || m.leave == nil {
		t.Fatalf("a tab click with unsaved input reaches area %v, screen %v", m.area, m.screen)
	}
	if !strings.Contains(plain(m), "d discard") {
		t.Errorf("not the usual question:\n%s", plain(m))
	}
	// The question is answered by the person first: a click on a tab behind it does nothing.
	clickOn(t, m, "3 Activity")
	if m.area != areaProjects || m.screen != screenConfirm {
		t.Errorf("a tab click behind the question reached area %v, screen %v", m.area, m.screen)
	}
	send(m, "d")
	if m.area != areaAccess || m.flowOpen() {
		t.Errorf("discard reaches area %v (flow open %v), want Access", m.area, m.flowOpen())
	}
}

func TestATabClickEndsAFilterBeingTyped(t *testing.T) {
	m := appModel(t, accessFake(), 120, 40)
	send(m, "4", "/")
	if !m.typing() {
		t.Fatal("/ starts no filter")
	}
	clickOn(t, m, "1 Overview")
	if m.area != areaOverview || m.typing() || m.acc.page.filtering() {
		t.Errorf("a tab click left area %v with the filter typing", m.area)
	}
	send(m, "4")
	if m.typing() {
		t.Error("the filter takes keys again after the way back")
	}
}
