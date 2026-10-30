package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// overlayCell is a cell inside the wheel area of the panel or the list that is open, and a cell outside it.
func overlayCell(t *testing.T, m *Model) (x, y, outX, outY int) {
	t.Helper()
	m.render()
	if len(m.wheelHits) != 1 {
		t.Fatalf("%d wheel areas, want the one of the overlay:\n%s", len(m.wheelHits), plain(m))
	}
	h := m.wheelHits[0]
	return h.x0 + 2, h.y0 + (h.y1-h.y0)/2, h.x1 + 1, h.y1 + 1
}

// helpModels are the help over both home screens, scrolled to a topic that does not fit.
func helpModels(t *testing.T, w, h int) map[string]*Model {
	t.Helper()
	list := frameModel(t, w, h)
	area := projectsModel(t, projectsFake(), w, h)
	if list.home != screenList || area.home != screenArea {
		t.Fatalf("home screens %v and %v", list.home, area.home)
	}
	for name, m := range map[string]*Model{"list": list, "area": area} {
		// Only the areas bind the keys of the panels, so the simple list gets the panel directly.
		m.openPopup(popHelp)
		for i := range helpTopics {
			m.pop.topic = i
			if plain(m); m.pop.last > wheelText {
				break
			}
		}
		if m.pop.last <= wheelText {
			t.Fatalf("%s %dx%d: no help topic scrolls:\n%s", name, w, h, plain(m))
		}
	}
	return map[string]*Model{fmt.Sprintf("list %dx%d", w, h): list, fmt.Sprintf("area %dx%d", w, h): area}
}

// helpModelsOfBoth are the help models of the panel that takes the whole workspace and of the one standing
// over a dimmed background.
func helpModelsOfBoth(t *testing.T) map[string]*Model {
	t.Helper()
	all := helpModels(t, 60, 14)
	for name, m := range helpModels(t, 60, 20) {
		all[name] = m
	}
	return all
}

func TestTheWheelScrollsTheHelpByThreeLinesAndStopsAtBothEnds(t *testing.T) {
	for name, m := range helpModelsOfBoth(t) {
		now := wheelClock(m)
		x, y, _, _ := overlayCell(t, m)
		topic, last := m.pop.topic, m.pop.last
		notch(m, now, x, y, true)
		if m.pop.scroll != 0 {
			t.Errorf("%s: the wheel up at the start moved to %d", name, m.pop.scroll)
		}
		notch(m, now, x, y, false)
		if m.pop.scroll != wheelText {
			t.Errorf("%s: one notch moved to %d, want %d", name, m.pop.scroll, wheelText)
		}
		for range last {
			notch(m, now, x, y, false)
		}
		if m.pop.scroll != last {
			t.Errorf("%s: the wheel down ended on %d, want the end %d", name, m.pop.scroll, last)
		}
		notch(m, now, x, y, false)
		if m.pop.scroll != last {
			t.Errorf("%s: the wheel at the end moved on to %d", name, m.pop.scroll)
		}
		notch(m, now, x, y, true)
		if m.pop.scroll != max(last-wheelText, 0) {
			t.Errorf("%s: one notch up went to %d", name, m.pop.scroll)
		}
		if m.pop.topic != topic || m.pop.kind != popHelp || m.screen != screenPopup {
			t.Errorf("%s: the wheel changed the topic or left the help: topic %d, screen %v", name, m.pop.topic, m.screen)
		}
	}
}

func TestTheWheelBesideThePanelMovesNothing(t *testing.T) {
	for name, m := range helpModelsOfBoth(t) {
		now := wheelClock(m)
		_, _, ox, oy := overlayCell(t, m)
		before := plain(m)
		notch(m, now, ox, oy, false)
		notch(m, now, 0, m.termHeight-1, false)
		if after := plain(m); after != before || m.pop.scroll != 0 {
			t.Errorf("%s: the wheel over the dimmed background changed the screen:\n%s", name, after)
		}
	}
	m := appModel(t, accessFake(), 120, 40)
	send(m, "4")
	send(m, "?")
	now := wheelClock(m)
	_, _, ox, oy := overlayCell(t, m)
	row := m.acc.page.list.cursor
	notch(m, now, ox, oy, false)
	notch(m, now, 1, m.workspaceTop()+3, false)
	if m.acc.page.list.cursor != row {
		t.Errorf("the wheel behind the help moved the table to %d", m.acc.page.list.cursor)
	}
}

func TestTheWheelDoesNothingOverTheInfo(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	send(m, "i")
	now := wheelClock(m)
	before := plain(m)
	for _, up := range []bool{false, true} {
		notch(m, now, 40, 12, up)
	}
	if m.pop == nil || m.pop.kind != popInfo || plain(m) != before {
		t.Error("the wheel changed the info")
	}
}

func TestTheWheelStepsTheCommandLineWithoutWrappingOrRunning(t *testing.T) {
	for name, m := range map[string]*Model{"list": frameModel(t, 80, 24), "area": appModel(t, overviewFake(), 80, 24)} {
		m.openPopup(popCommand)
		typed(m, "s")
		n := len(matchCommands(m.commands(), m.pop.in.Value()))
		if n < 3 {
			t.Fatalf("%s: %d commands match, want at least three", name, n)
		}
		now := wheelClock(m)
		x, y, _, _ := overlayCell(t, m)
		stepsOf(t, name+" command line", m, now, x, y, func() int { return m.pop.cursor }, n)
		if m.pop == nil || m.pop.in.Value() != "s" || m.screen != screenPopup {
			t.Errorf("%s: the wheel typed, ran or closed: %v", name, m.pop)
		}
	}
}

func TestTheWheelBesideTheCommandLineMovesNothing(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	send(m, ":")
	now := wheelClock(m)
	_, _, ox, oy := overlayCell(t, m)
	notch(m, now, ox, oy, false)
	if m.pop.cursor != 0 {
		t.Errorf("the wheel beside the line moved the choice to %d", m.pop.cursor)
	}
}

// settingsDropdown opens the language list of the settings, whose field the frame draws.
func settingsDropdown(t *testing.T) *Model {
	t.Helper()
	m, _ := settingsModel(t, newFake(control.VaultUnlocked), "general")
	send(m, "enter")
	if m.dd == nil {
		t.Fatal("enter opens no list")
	}
	return m
}

func TestTheWheelStepsTheDropdownWithoutChoosingOrClosingIt(t *testing.T) {
	m := settingsDropdown(t)
	now := wheelClock(m)
	language := m.set.gen.language
	x, y, _, _ := overlayCell(t, m)
	stepsOf(t, "dropdown", m, now, x, y, func() int { return m.dd.cursor }, len(m.dd.values))
	if m.dd == nil || m.set.gen.language != language {
		t.Errorf("the wheel closed the list or chose a value: open %v, language %q", m.dd != nil, m.set.gen.language)
	}
}

func TestTheWheelBesideTheDropdownMovesNothing(t *testing.T) {
	m := settingsDropdown(t)
	now := wheelClock(m)
	_, _, ox, oy := overlayCell(t, m)
	before := plain(m)
	notch(m, now, ox+20, oy+5, false)
	notch(m, now, 1, m.workspaceTop(), false)
	if m.dd == nil || m.dd.cursor != 0 || plain(m) != before {
		t.Errorf("the wheel beside the list changed the screen:\n%s", plain(m))
	}
}

// pickerModel is the Projects area with a picker of twelve versions open.
func pickerModel(t *testing.T, w, h int) (*Model, *string) {
	t.Helper()
	m := projectsModel(t, projectsFake(), w, h)
	var values []string
	for i := range 12 {
		values = append(values, fmt.Sprintf("v1.%02d", i))
	}
	picked := new(string)
	m.openProjectPicker("Version", "note", values, 0, func(v string) tea.Cmd { *picked = v; return nil })
	if !m.projectsDialogOpen() || m.prj.dialog.picker == nil {
		t.Fatal("no picker")
	}
	return m, picked
}

func TestTheWheelStepsTheProjectPickerInTheModalAndTheSmallTerminal(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {60, 19 + 5}, {80, 24}} {
		m, picked := pickerModel(t, size[0], size[1])
		now := wheelClock(m)
		x, y, _, _ := overlayCell(t, m)
		name := fmt.Sprintf("picker %dx%d", size[0], size[1])
		stepsOf(t, name, m, now, x, y, func() int { return m.prj.dialog.picker.list.cursor }, 12)
		if *picked != "" || !m.projectsDialogOpen() {
			t.Errorf("%s: the wheel chose %q or closed the picker", name, *picked)
		}
		if !strings.Contains(plain(m), "11 of 12") && !strings.Contains(plain(m), "11/12") {
			t.Errorf("%s: the picker does not show its mark:\n%s", name, plain(m))
		}
	}
}

func TestTheWheelBesideTheProjectPickerMovesNothing(t *testing.T) {
	m, _ := pickerModel(t, 120, 40)
	now := wheelClock(m)
	_, _, ox, oy := overlayCell(t, m)
	notch(m, now, ox, oy, false)
	notch(m, now, 0, m.termHeight-1, false)
	if m.prj.dialog.picker.list.cursor != 0 {
		t.Errorf("the wheel beside the picker moved the mark to %d", m.prj.dialog.picker.list.cursor)
	}
}

func TestTheWheelLeavesTheFilterOfThePickerAlone(t *testing.T) {
	m, _ := pickerModel(t, 120, 40)
	send(m, "/")
	typed(m, "1.0")
	now := wheelClock(m)
	l := &m.prj.dialog.picker.list
	x, y, _, _ := overlayCell(t, m)
	before := l.input.Value()
	notch(m, now, x, y, false)
	if !l.editing || l.input.Value() != before || before != "1.0" || l.cursor != 1 || len(l.matches) < 3 {
		t.Errorf("the wheel changed the filter being typed or did not step: editing %v, text %q, cursor %d", l.editing, l.input.Value(), l.cursor)
	}
	for range len(l.matches) + 2 {
		notch(m, now, x, y, false)
	}
	if l.cursor != len(l.matches)-1 || !l.editing || l.input.Value() != before {
		t.Errorf("the wheel at the end of the typed filter: cursor %d of %d, editing %v, text %q", l.cursor, len(l.matches), l.editing, l.input.Value())
	}
	for range len(l.matches) + 2 {
		notch(m, now, x, y, true)
	}
	if l.cursor != 0 {
		t.Errorf("the wheel up at the start of the typed filter went to %d", l.cursor)
	}
	send(m, "enter")
	if l.editing || l.query() != "1.0" {
		t.Fatalf("the filter is not kept: editing %v, query %q", l.editing, l.query())
	}
	notch(m, now, x, y, false)
	if l.query() != "1.0" || l.cursor != 1 || !strings.Contains(ansi.Strip(m.render()), "/ 1.0") {
		t.Errorf("the wheel changed the kept filter or did not step: query %q, cursor %d", l.query(), l.cursor)
	}
}
