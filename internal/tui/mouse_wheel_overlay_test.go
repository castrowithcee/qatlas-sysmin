package tui

import (
	"fmt"
	"strings"
	"testing"
)

// overlayCell is a cell inside the wheel area of the panel or the list that is open, and a cell outside it.
func overlayCell(t *testing.T, m *Model) (x, y, outX, outY int) {
	t.Helper()
	m.render()
	areas := wheelAreas(m)
	if len(areas) != 1 {
		t.Fatalf("%d wheel areas, want the one of the overlay:\n%s", len(areas), plain(m))
	}
	r := areas[0].Rect
	return r.X + 2, r.Y + r.Height/2, r.X + r.Width + 1, r.Y + r.Height + 1
}

// helpModels are the help over both home screens on its first topic, which ends with the keys and scrolls.
func helpModels(t *testing.T, w, h int) map[string]*Model {
	t.Helper()
	list := frameModel(t, w, h)
	area := appModel(t, overviewFake(), w, h)
	if list.home != screenList || area.home != screenArea {
		t.Fatalf("home screens %v and %v", list.home, area.home)
	}
	for _, m := range []*Model{list, area} {
		// Only the areas bind the keys of the parts, so the simple list gets the help directly.
		m.openPopup(popHelp)
	}
	return map[string]*Model{fmt.Sprintf("list %dx%d", w, h): list, fmt.Sprintf("area %dx%d", w, h): area}
}

// helpModelsOfBoth are the help models of the help that fills the workspace and of the one standing over a
// dimmed background.
func helpModelsOfBoth(t *testing.T) map[string]*Model {
	t.Helper()
	all := helpModels(t, 60, 14)
	for name, m := range helpModels(t, 80, 24) {
		all[name] = m
	}
	return all
}

func TestTheWheelScrollsTheHelpAndStopsAtBothEnds(t *testing.T) {
	for name, m := range helpModelsOfBoth(t) {
		now := wheelClock(m)
		x, y := m.termWidth/2, m.termHeight/2
		start := plain(m)
		if !strings.Contains(start, "lines 1-") {
			t.Fatalf("%s: the first topic does not scroll:\n%s", name, start)
		}
		notch(m, now, x, y, true)
		if plain(m) != start {
			t.Errorf("%s: the wheel up at the start moved the text:\n%s", name, plain(m))
		}
		notch(m, now, x, y, false)
		if view := plain(m); !strings.Contains(view, "lines 4-") {
			t.Errorf("%s: one notch did not scroll three lines:\n%s", name, view)
		}
		for range 100 {
			notch(m, now, x, y, false)
		}
		end := plain(m)
		notch(m, now, x, y, false)
		if plain(m) != end {
			t.Errorf("%s: the wheel at the end moved on", name)
		}
		if m.pop == nil || m.pop.help == nil || m.pop.help.Topic() != "qsysmin" || m.screen != screenPopup {
			t.Errorf("%s: the wheel changed the topic or left the help", name)
		}
	}
}

func TestTheWheelBehindTheHelpMovesNothing(t *testing.T) {
	m := appModel(t, activityFake(), 120, 40)
	send(m, "3")
	send(m, "?")
	now := wheelClock(m)
	row := m.act.page.list.cursor
	notch(m, now, 1, m.workspaceTop()+3, false)
	notch(m, now, 0, m.termHeight-1, false)
	if m.act.page.list.cursor != row || m.pop == nil {
		t.Errorf("the wheel behind the help moved the table to %d", m.act.page.list.cursor)
	}
}

func TestTheWheelDoesNothingOverAShortInfo(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	send(m, "i")
	now := wheelClock(m)
	before := plain(m)
	for _, up := range []bool{false, true} {
		notch(m, now, 40, 12, up)
	}
	if m.pop == nil || m.pop.kind() != popInfo || plain(m) != before {
		t.Error("the wheel changed the info")
	}
}

func TestTheWheelNeitherTypesNorRunsInTheCommandLine(t *testing.T) {
	for name, m := range map[string]*Model{"list": frameModel(t, 80, 24), "area": appModel(t, overviewFake(), 80, 24)} {
		m.openPopup(popCommand)
		typed(m, "s")
		now := wheelClock(m)
		before := plain(m)
		for _, up := range []bool{false, true, false} {
			notch(m, now, 10, 8, up)
		}
		if m.pop == nil || m.pop.cmd == nil || m.pop.cmd.Query() != "s" || m.screen != screenPopup || plain(m) != before {
			t.Errorf("%s: the wheel typed, ran or closed:\n%s", name, plain(m))
		}
	}
}

// settingsDropdown opens the language list of the settings, whose field the frame draws.
func settingsDropdown(t *testing.T) *Model {
	t.Helper()
	m, _ := settingsModel(t, newFake(), "general")
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
