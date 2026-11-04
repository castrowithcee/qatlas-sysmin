package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// pickModel is the create form of a user with the pick row for the further groups focused. The host offers
// demo-ops and demo-team, and the extra groups named.
func pickModel(t *testing.T, w, h int, extra ...string) *Model {
	t.Helper()
	m, _, host, _ := userModel(t, w, h)
	for i, name := range extra {
		host.groups = append(host.groups, hostadmin.Group{Name: name, GID: 2000 + i})
	}
	send(m, "n", "tab", "tab", "tab", "tab", "tab", "tab")
	if m.form == nil || formFocus(m) != fieldGroups {
		t.Fatalf("the groups field is not focused:\n%s", plain(m))
	}
	return m
}

func listLines(m *Model) []string {
	var out []string
	for _, line := range strings.Split(plain(m), "\n") {
		if at := strings.Index(line, "[ ] "); at >= 0 {
			out = append(out, cell(line[at:]))
		} else if at := strings.Index(line, "[x] "); at >= 0 {
			out = append(out, cell(line[at:]))
		}
	}
	return out
}

// cell is the text up to the border of the panel it stands in.
func cell(text string) string {
	if at := strings.IndexAny(text, "║│"); at >= 0 {
		text = text[:at]
	}
	return strings.TrimSpace(text)
}

// summaryShown is the summary of the further groups, and whether the form draws it.
func summaryShown(m *Model) (string, bool) {
	got := m.form.value(fieldGroups)
	return got, strings.Contains(plain(m), got)
}

func TestPickRowShowsAShortSummaryAndOpensTheListOnEnterAndClick(t *testing.T) {
	for _, w := range []struct{ w, h int }{{120, 40}, {80, 24}} {
		m := pickModel(t, w.w, w.h)
		if got, _ := summaryShown(m); got != "" || !strings.Contains(plain(m), "none") {
			t.Errorf("summary %q:\n%s", got, plain(m))
		}
		send(m, "enter")
		if m.form.pick == nil || len(listLines(m)) != 2 {
			t.Fatalf("enter did not open the list:\n%s", plain(m))
		}
		send(m, "esc")
		if m.form == nil || m.form.pick != nil {
			t.Fatalf("esc closed more than the list:\n%s", plain(m))
		}
		send(m, "tab")
		clickOn(t, m, "none")
		if m.form.pick == nil || formFocus(m) != fieldGroups {
			t.Errorf("a click on the row did not open the list at %dx%d:\n%s", w.w, w.h, plain(m))
		}
	}
}

func TestPickListFiltersTicksTakesOverAndDiscards(t *testing.T) {
	m := pickModel(t, 120, 40)
	send(m, "enter")
	typed(m, "team")
	if got := listLines(m); len(got) != 1 || got[0] != "[ ] demo-team" {
		t.Fatalf("filtered list %q", got)
	}
	send(m, "space")
	if got := listLines(m); len(got) != 1 || got[0] != "[x] demo-team" {
		t.Fatalf("ticked list %q", got)
	}
	send(m, "esc")
	if got := m.form.picked(fieldGroups); len(got) != 0 {
		t.Errorf("esc kept %q", got)
	}
	send(m, "enter", "space", "enter")
	if got := m.form.picked(fieldGroups); len(got) != 1 || got[0] != "demo-ops" {
		t.Errorf("picked %q", got)
	}
	if got, shown := summaryShown(m); got != "demo-ops" || !shown {
		t.Errorf("summary %q, shown %v", got, shown)
	}
}

func TestPickListTicksWithAClick(t *testing.T) {
	for _, w := range []struct{ w, h int }{{120, 40}, {80, 24}} {
		m := pickModel(t, w.w, w.h)
		send(m, "enter")
		clickOn(t, m, "[ ] demo-team")
		clickOn(t, m, "[x] demo-team")
		clickOn(t, m, "[ ] demo-team")
		if got := listLines(m); len(got) != 2 || got[1] != "[x] demo-team" {
			t.Errorf("list %q at %dx%d", got, w.w, w.h)
		}
		send(m, "enter")
		if got := m.form.picked(fieldGroups); len(got) != 1 || got[0] != "demo-team" {
			t.Errorf("picked %q", got)
		}
	}
}

// pickMarked is the ID of the row the open pick list has marked.
func pickMarked(m *Model) string {
	id, _ := m.form.pick.list.Selected()
	return string(id)
}

func TestPickListWheelMovesTheCursorAndStopsAtTheEnds(t *testing.T) {
	m := pickModel(t, 120, 40)
	send(m, "enter")
	now := wheelClock(m)
	x, y := cellOf(t, m, "demo-ops")
	for i := 0; i < 3; i++ {
		*now = now.Add(time.Second)
		wheel(m, x, y, tea.MouseWheelDown)
	}
	if got := pickMarked(m); got != "1" {
		t.Errorf("row %s marked after wheeling down past the end", got)
	}
	*now = now.Add(time.Second)
	wheel(m, x, y, tea.MouseWheelUp)
	if got := pickMarked(m); got != "0" {
		t.Errorf("row %s marked after wheeling up", got)
	}
}

func TestPickSummaryNamesTwoAndCountsTheRest(t *testing.T) {
	m := pickModel(t, 120, 40, "demo-a", "demo-b", "demo-c")
	send(m, "enter")
	for range 5 {
		send(m, "space", "down")
	}
	send(m, "enter")
	if got, shown := summaryShown(m); got != "demo-a, demo-b +3" || !shown {
		t.Errorf("summary %q, shown %v", got, shown)
	}
}

func TestPickListShowsTheChosenAtTheTop(t *testing.T) {
	m := pickModel(t, 120, 40, "demo-a", "demo-b")
	send(m, "enter")
	send(m, "down", "down", "down", "space", "enter", "enter")
	if got := listLines(m); len(got) != 4 || got[0] != "[x] demo-team" || got[1] != "[ ] demo-a" {
		t.Errorf("ticked value is not first: %q", got)
	}
}

func TestPickListOfTheChangeFormMarksSystemGroups(t *testing.T) {
	m, _, host, _ := userModel(t, 120, 40)
	host.groups = append(host.groups, hostadmin.Group{Name: "demo-sys", GID: 65534, System: true})
	chooseChange(t, m, "demo-user", chgAddGroups)
	send(m, "tab", "enter")
	var sys, regular string
	for _, line := range listLines(m) {
		if strings.Contains(line, "demo-sys") {
			sys = line
		}
		if strings.Contains(line, "demo-ops") {
			regular = line
		}
	}
	if !strings.HasSuffix(sys, "system") || strings.Contains(regular, "system") {
		t.Errorf("system marker: %q, regular: %q", sys, regular)
	}
	send(m, "down", "space", "enter")
	save(m)
	if m.dlg == nil || m.dlgSpec.Danger == "" {
		t.Errorf("the system group did not make the confirmation red: %+v", m.dlgSpec)
	}
}

func TestCreateFormOffersNoSystemMarkers(t *testing.T) {
	m := pickModel(t, 120, 40)
	send(m, "enter")
	if strings.Contains(strings.Join(listLines(m), "\n"), "system") {
		t.Errorf("a marker in the create list: %q", listLines(m))
	}
}

func TestListFieldIgnoresArrowsAndSpaceInTheForm(t *testing.T) {
	m := pickModel(t, 120, 40)
	called := false
	m.form.changed = func(*Model, *formDlg, kittui.FieldID) { called = true }
	send(m, "right", "left", "space")
	if called || len(m.form.picked(fieldGroups)) != 0 || m.form.pick != nil || formFocus(m) != fieldGroups {
		t.Errorf("called %v, picked %q, focus %s", called, m.form.picked(fieldGroups), formFocus(m))
	}
}

// The list opens as the kit's modal with the field's label as title, an empty search and a tick box per value.
func TestPickListOpensWithSearchAndTickBoxes(t *testing.T) {
	m := pickModel(t, 120, 40)
	send(m, "enter")
	view := plain(m)
	if !strings.Contains(view, "═ Further groups") || !strings.Contains(view, "Type to search") {
		t.Errorf("no titled modal with a search:\n%s", view)
	}
	if got := listLines(m); len(got) != 2 || got[0] != "[ ] demo-ops" || got[1] != "[ ] demo-team" {
		t.Errorf("list %q", got)
	}
}

// A tick box takes a click as space takes the key, and neither clicks nor keys put text into the search.
func TestPickClickTicksAsTheKeyDoesAndTypesNothing(t *testing.T) {
	byKey := pickModel(t, 120, 40)
	send(byKey, "enter", "down", "space")
	byClick := pickModel(t, 120, 40)
	send(byClick, "enter")
	clickOn(t, byClick, "[ ] demo-team")
	if a, b := strings.Join(listLines(byKey), "|"), strings.Join(listLines(byClick), "|"); a != b {
		t.Errorf("key %q, click %q", a, b)
	}
	clickOn(t, byClick, "demo-ops")
	clickOn(t, byClick, "Type to search")
	if q := byClick.form.pick.list.Query(); q != "" {
		t.Errorf("a click typed %q into the search", q)
	}
	if got := listLines(byClick); len(got) != 2 || got[0] != "[ ] demo-ops" || got[1] != "[x] demo-team" {
		t.Errorf("a click on a row changed ticks: %q", got)
	}
	if id := pickMarked(byClick); id != "0" {
		t.Errorf("a click on the row marked %q", id)
	}
}

// The buttons act as enter and esc do, also with a filter typed, which esc does not clear first.
func TestPickButtonsKeepAndDiscard(t *testing.T) {
	m := pickModel(t, 120, 40)
	send(m, "enter")
	typed(m, "ops")
	send(m, "space")
	clickOn(t, m, "esc discard")
	if m.form.pick != nil || len(m.form.picked(fieldGroups)) != 0 {
		t.Errorf("discard kept %q", m.form.picked(fieldGroups))
	}
	send(m, "enter")
	typed(m, "ops")
	send(m, "space")
	clickOn(t, m, "enter keep")
	if got := m.form.picked(fieldGroups); m.form.pick != nil || len(got) != 1 || got[0] != "demo-ops" {
		t.Errorf("keep took %q", got)
	}
	send(m, "enter", "esc")
	if m.form == nil || m.form.pick != nil || len(m.form.picked(fieldGroups)) != 1 {
		t.Error("esc did not restore the earlier choice")
	}
}

// Where the modal has no room the list fills the workspace and keeps keys, wheel, boxes and buttons.
func TestPickListInTheWorkspaceWhereTheModalDoesNotFit(t *testing.T) {
	m := pickModel(t, 40, 14)
	send(m, "enter")
	if view := plain(m); strings.Contains(view, "╔═ Further groups") || m.form.pick == nil {
		t.Fatalf("not the workspace fallback:\n%s", view)
	}
	clickOn(t, m, "[ ] demo-team")
	if got := listLines(m); len(got) != 2 || got[1] != "[x] demo-team" {
		t.Errorf("list %q", got)
	}
	typed(m, "ops")
	if q := m.form.pick.list.Query(); q != "ops" {
		t.Errorf("query %q", q)
	}
	clickOn(t, m, "esc discard")
	if m.form.pick != nil || len(m.form.picked(fieldGroups)) != 0 {
		t.Errorf("a click on discard kept %q", m.form.picked(fieldGroups))
	}
}
