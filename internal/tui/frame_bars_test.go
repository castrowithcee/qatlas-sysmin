package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

func rowsOf(m *Model) []string { return strings.Split(plain(m), "\n") }

// The areas are what the tab line can draw, in the order of the area constants, and every title has its text.
func TestAreasAreWhatTheTabLineDraws(t *testing.T) {
	areas := frameAreas()
	if err := kittui.CheckAreas(areas); err != nil {
		t.Fatal(err)
	}
	for a := area(0); a < areaCount; a++ {
		if got, ok := areaOf(areas[a].ID); !ok || got != a {
			t.Errorf("area %d has the ID %q, which names %d", a, areas[a].ID, got)
		}
	}
	for _, a := range areas {
		titles := []textID{a.Title}
		for _, tab := range a.Tabs {
			titles = append(titles, tab.Title)
		}
		for _, id := range titles {
			for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
				if _, ok := catalog()[lang][id]; !ok {
					t.Errorf("area %q: %s has no %s text", a.ID, id, lang)
				}
			}
		}
	}
}

// The frame reads from the top: header, tab line, page strip where the area has pages, rule, workspace.
func TestFrameOrderIsHeaderTabsPageStripRule(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	rows := rowsOf(m)
	if !strings.Contains(rows[0], "[?] help  [:] cmd") || !strings.Contains(rows[1], "1 Overview") || !strings.Contains(rows[1], "4 Settings") {
		t.Fatalf("header and tab line:\n%s\n%s", rows[0], rows[1])
	}
	if !strings.HasPrefix(rows[2], "──") {
		t.Errorf("the rule does not follow the tab line in an area without pages: %q", rows[2])
	}
	send(m, "4")
	rows = rowsOf(m)
	if !strings.Contains(rows[1], "4 Settings") || !strings.Contains(rows[2], "General") || !strings.HasPrefix(rows[3], "──") {
		t.Errorf("Settings is not header, tabs, page strip, rule:\n%s", strings.Join(rows[:4], "\n"))
	}
	if m.workspaceTop() != 4 {
		t.Errorf("workspaceTop = %d, want 4", m.workspaceTop())
	}
}

// A bar with the focus has "> " in front of it; the bar without it has blanks, so nothing moves.
func TestFocusedBarCarriesTheMarker(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	send(m, "4")
	m.zone = zoneTabs
	rows := rowsOf(m)
	if !strings.HasPrefix(rows[1], "> ") || !strings.HasPrefix(rows[2], "  ") {
		t.Errorf("tab line focused:\n%s\n%s", rows[1], rows[2])
	}
	m.zone = zoneSubs
	rows = rowsOf(m)
	if !strings.HasPrefix(rows[1], "  ") || !strings.HasPrefix(rows[2], "> ") {
		t.Errorf("page strip focused:\n%s\n%s", rows[1], rows[2])
	}
}

// Where the tabs do not fit, the gaps close first, then the inactive tabs shrink to their digit; the active one
// stays whole.
func TestNarrowTabLineClosesGapsThenShrinksInactiveTabs(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	send(m, "2")
	for w, check := range map[int]func(string) bool{
		79: func(l string) bool { return strings.Contains(l, "1 Overview") && strings.Contains(l, "4 Settings") },
		40: func(l string) bool {
			return strings.Contains(l, "2 Host") && !strings.Contains(l, "Overview") && strings.Contains(l, " 1 ") && strings.Contains(l, " 4")
		},
	} {
		sizedModel(m, w, 24)
		line := rowsOf(m)[1]
		if ansi.StringWidth(line) > w || !check(line) {
			t.Errorf("%d columns: %q", w, line)
		}
	}
}

// Under lack of space the key hints drop before the back button; the German words are the longer ones.
func TestHeaderDropsKeyHintsBeforeTheBackButton(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	useLanguage(t, m, i18n.German)
	send(m, "2", "down")
	if m.zoneNow() == zoneTabs {
		t.Fatal("expected focus below the tab line")
	}
	if row := headerOf(m); !strings.Contains(row, "[esc] Zurück") || !strings.Contains(row, "[?] Hilfe  [:] Befehl") {
		t.Fatalf("wide header = %q", row)
	}
	seen := false
	for w := 80; w >= minimumWidth; w-- {
		sizedModel(m, w, 24)
		row := headerOf(m)
		hints, back := strings.Contains(row, "[?]"), strings.Contains(row, "[esc] Zurück")
		if back && !hints {
			seen = true
		}
		if hints && !back {
			t.Fatalf("%d columns keep the key hints but lose the back button: %q", w, row)
		}
	}
	if !seen {
		t.Error("no width shows the back button without the key hints")
	}
}

// A click on the back button acts like esc, and the clicks on the tab line and the page strip land on their
// rows below the header.
func TestBackAndBarClicksUseTheirRows(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	clickOn(t, m, "4 Settings")
	if m.area != areaSettings {
		t.Fatalf("tab click reached area %v", m.area)
	}
	x, y := cellOf(t, m, "Update")
	if y != 2 {
		t.Fatalf("the page strip stands in row %d, want 2", y)
	}
	clickAt(m, x, y)
	if m.set.sub != subUpdate {
		t.Errorf("page strip click reached page %d", m.set.sub)
	}
	send(m, "down", "down")
	if m.zoneNow() != zoneContent {
		t.Fatalf("zone %v, want the content", m.zoneNow())
	}
	clickOn(t, m, "[esc] back")
	if m.zoneNow() != zoneSubs {
		t.Errorf("the back button leaves zone %v, want the page strip as esc does", m.zoneNow())
	}
}
