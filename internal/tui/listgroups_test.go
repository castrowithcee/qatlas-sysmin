package tui

import (
	"strings"
	"testing"
)

// groupedModel is the frame over four invented entries in three groups: data, web, and none.
func groupedModel(t *testing.T) *Model {
	t.Helper()
	m := frameModel(t, 100, 30)
	m.setEntries([]string{"alpha", "bravo", "charlie", "delta"}, func(name string) string {
		switch name {
		case "alpha", "bravo":
			return "web"
		case "charlie":
			return "data"
		}
		return ""
	}, nil)
	return m
}

func shownNames(m *Model) []string { return append([]string(nil), m.list.matches...) }

func joined(names ...string) string { return strings.Join(names, ",") }

func TestGroupsAreOrderedAlphabeticallyWithTheGroupWithoutKeyLast(t *testing.T) {
	m := groupedModel(t)
	want := joined(groupRow("data"), "charlie", groupRow("web"), "alpha", "bravo", groupRow(""), "delta")
	if got := joined(shownNames(m)...); got != want {
		t.Fatalf("shows %q, want %q", got, want)
	}
	if m.groups.of["delta"] != "" || m.groups.of["alpha"] != "web" {
		t.Errorf("groups = %v", m.groups.of)
	}
	if got := m.groups.rowText(groupRow(""), true); got != "▾ ■ no group (1)" {
		t.Errorf("row of the group without key = %q", got)
	}
	if got := m.groups.rowText(groupRow("web"), false); got != "▸ ■ web (2)" {
		t.Errorf("folded row = %q", got)
	}
	if shown, total := len(m.list.matches), m.entryTotal(); shown != 7 || total != 4 {
		t.Errorf("shown %d, total %d: group rows must not count as entries", shown, total)
	}
}

func TestGroupsFoldAndUnfoldByKeyAndKeepTheirStateOnRefill(t *testing.T) {
	m := groupedModel(t)
	m.list.selectName(groupRow("web"))
	press(t, m, "left")
	if got := joined(shownNames(m)...); got != joined(groupRow("data"), "charlie", groupRow("web"), groupRow(""), "delta") {
		t.Fatalf("after left: %q", got)
	}
	if view := plain(m); !strings.Contains(view, "▸ ■ web (2)") || strings.Contains(view, "bravo") {
		t.Errorf("a folded group must read as folded and hide its entries:\n%s", view)
	}
	press(t, m, "right")
	if len(m.list.matches) != 7 {
		t.Errorf("after right: %q", m.list.matches)
	}
	press(t, m, "enter")
	if len(m.list.matches) != 5 || m.screen != screenList {
		t.Errorf("enter on a group row folds it: %q", m.list.matches)
	}
	press(t, m, "enter")
	if len(m.list.matches) != 7 {
		t.Errorf("enter on a folded group row unfolds it: %q", m.list.matches)
	}
	// Left on an entry folds its group and the cursor follows to the group row.
	press(t, m, "down", "left")
	if name, _ := m.list.selected(); name != groupRow("web") || len(m.list.matches) != 5 {
		t.Errorf("left on an entry: cursor %q, shown %q", name, m.list.matches)
	}
	// Right on an entry does nothing: an entry only folds.
	m.list.selectName("charlie")
	press(t, m, "right")
	if name, _ := m.list.selected(); name != "charlie" || len(m.list.matches) != 5 {
		t.Errorf("right on an entry moved to %q or changed %q", name, m.list.matches)
	}
	// The fold survives a new fill of the list.
	m.setEntries([]string{"alpha", "bravo", "charlie", "delta"}, func(name string) string {
		return map[string]string{"alpha": "web", "bravo": "web", "charlie": "data"}[name]
	}, nil)
	if len(m.list.matches) != 5 {
		t.Errorf("the fold is lost: %q", m.list.matches)
	}
}

func TestFilterOpensFoldedGroupsWithHitsAndClearingRestoresTheFolds(t *testing.T) {
	m := groupedModel(t)
	m.list.selectName(groupRow("web"))
	press(t, m, "left")
	press(t, m, "/")
	typeText(t, m, "bra")
	if got := joined(shownNames(m)...); got != joined(groupRow("web"), "bravo") {
		t.Errorf("filter shows %q, want the folded group open with its hit", got)
	}
	if view := plain(m); !strings.Contains(view, "▾ ■ web (2)") || !strings.Contains(view, "1/1 (4 total)") {
		t.Errorf("filtered view:\n%s", view)
	}
	// Group names are not searched: only the text of the entries is.
	press(t, m, "esc")
	press(t, m, "/")
	typeText(t, m, "data")
	if len(m.list.matches) != 0 {
		t.Errorf("a group name matched an entry: %q", m.list.matches)
	}
	press(t, m, "esc")
	if got := joined(shownNames(m)...); got != joined(groupRow("data"), "charlie", groupRow("web"), groupRow(""), "delta") {
		t.Errorf("after clearing: %q", got)
	}
}

func TestPositionCountsEntriesOnly(t *testing.T) {
	m := groupedModel(t)
	m.list.selectName("charlie")
	if shown, pos := m.entryPosition(); shown != 4 || pos != 1 {
		t.Errorf("first entry: %d/%d", pos, shown)
	}
	m.list.selectName("bravo")
	if _, pos := m.entryPosition(); pos != 3 {
		t.Errorf("third entry at %d", pos)
	}
	m.list.selectName(groupRow("web"))
	if _, pos := m.entryPosition(); pos != 1 {
		t.Errorf("on a group row the position is that of the entry above, got %d", pos)
	}
	m.list.selectName(groupRow("data"))
	if _, pos := m.entryPosition(); pos != 1 {
		t.Errorf("on the first group row the position is the first entry, got %d", pos)
	}
}

func TestSelectedIgnoresGroupRowsAndCopyDoesNothingThere(t *testing.T) {
	m := groupedModel(t)
	m.list.selectName(groupRow("data"))
	if _, ok := m.selected(); ok {
		t.Fatal("a group row is selected as an entry")
	}
	if _, cmd := m.Update(keyMsg("c")); cmd != nil || m.status != "" {
		t.Error("copy acted on a group row")
	}
}

func TestSelectEntryUnfoldsItsGroup(t *testing.T) {
	m := groupedModel(t)
	m.list.selectName(groupRow("web"))
	press(t, m, "left")
	m.selectEntry("bravo")
	if name, ok := m.selected(); !ok || name != "bravo" {
		t.Errorf("selected %q (%v), want bravo with its group unfolded", name, ok)
	}
}

func TestGroupRowsDrawWithoutColourAsText(t *testing.T) {
	m := groupedModel(t)
	view := plain(m)
	for _, want := range []string{"▾ ■ data (1)", "▾ ■ web (2)", "▾ ■ no group (1)", "alpha"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	assertViewFits(t, view, 100, 30)
}
