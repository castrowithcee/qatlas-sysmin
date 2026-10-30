package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func rowsOf(m *Model) []string { return strings.Split(plain(m), "\n") }

// The frame reads from the top: header, tab line, page strip where the area has pages, rule, workspace.
func TestFrameOrderIsHeaderTabsPageStripRule(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	rows := rowsOf(m)
	if !strings.Contains(rows[0], "[?] help  [:] cmd") || !strings.Contains(rows[1], "1 Overview") || !strings.Contains(rows[1], "6 Settings") {
		t.Fatalf("header and tab line:\n%s\n%s", rows[0], rows[1])
	}
	if !strings.HasPrefix(rows[2], "──") {
		t.Errorf("the rule does not follow the tab line in an area without pages: %q", rows[2])
	}
	send(m, "6")
	rows = rowsOf(m)
	if !strings.Contains(rows[1], "6 Settings") || !strings.Contains(rows[2], "General") || !strings.HasPrefix(rows[3], "──") {
		t.Errorf("Settings is not header, tabs, page strip, rule:\n%s", strings.Join(rows[:4], "\n"))
	}
	if m.workspaceTop() != 4 {
		t.Errorf("workspaceTop = %d, want 4", m.workspaceTop())
	}
}

// A bar with the focus has "> " in front of it; the bar without it has blanks, so nothing moves.
func TestFocusedBarCarriesTheMarker(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	send(m, "6")
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
	send(m, "5")
	for w, check := range map[int]func(string) bool{
		72: func(l string) bool { return strings.Contains(l, "1 Overview") && strings.Contains(l, "6 Settings") },
		40: func(l string) bool {
			return strings.Contains(l, "5 Library") && !strings.Contains(l, "Overview") && strings.Contains(l, " 1 ") && strings.Contains(l, " 6")
		},
	} {
		sizedModel(m, w, 24)
		line := rowsOf(m)[1]
		if ansi.StringWidth(line) > w || !check(line) {
			t.Errorf("%d columns: %q", w, line)
		}
	}
}

// Under lack of space the key hints drop before the back button.
func TestHeaderDropsKeyHintsBeforeTheBackButton(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	send(m, "5", "down")
	if m.zoneNow() == zoneTabs {
		t.Fatal("expected focus below the tab line")
	}
	if row := ansi.Strip(m.headerLines()[0]); !strings.Contains(row, "[esc] back") || !strings.Contains(row, "[?] help  [:] cmd") {
		t.Fatalf("wide header = %q", row)
	}
	seen := false
	for w := 80; w >= 20; w-- {
		sizedModel(m, w, 24)
		row := ansi.Strip(m.headerLines()[0])
		hints, back := strings.Contains(row, "[?]"), strings.Contains(row, "[esc] back")
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
	clickOn(t, m, "6 Settings")
	if m.area != areaSettings {
		t.Fatalf("tab click reached area %v", m.area)
	}
	x, y := cellOf(t, m, "Vault")
	if y != 2 {
		t.Fatalf("the page strip stands in row %d, want 2", y)
	}
	clickAt(m, x, y)
	if m.set.sub != subVault {
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
