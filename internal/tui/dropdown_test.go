package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// openedDropdown is the frame with a dropdown of three values open under a field at (x, y).
func openedDropdown(t *testing.T, w, h, x, y int) (*Model, *int) {
	t.Helper()
	m := frameModel(t, w, h)
	chosen := new(int)
	*chosen = -1
	m.openDropdown("field", []string{"One", "Two", "Three"}, 0, func(i int) tea.Cmd { *chosen = i; return nil })
	m.noteField("field", x, y)
	return m, chosen
}

// drawn renders the frame with the open list, as a terminal shows it, and registers the list's box.
func drawn(m *Model) string {
	m.layoutWorkspace()
	return ansi.Strip(m.overlayDropdown(m.frame(m.workspaceView())))
}

func TestDropdownKeysMoveWithoutWrappingChooseAndCancel(t *testing.T) {
	m, chosen := openedDropdown(t, 80, 24, 2, 3)
	press(t, m, "up")
	if m.dd.cursor != 0 {
		t.Errorf("up at the first value moved to %d", m.dd.cursor)
	}
	press(t, m, "down", "down", "down")
	if m.dd.cursor != 2 {
		t.Errorf("down past the last value moved to %d, want to stay on 2", m.dd.cursor)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if m.dd.cursor != 0 {
		t.Errorf("home moved to %d", m.dd.cursor)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	if m.dd.cursor != 2 {
		t.Errorf("end moved to %d", m.dd.cursor)
	}
	press(t, m, "up", "enter")
	if *chosen != 1 || m.dd != nil {
		t.Errorf("chosen = %d, open = %v, want 1 and closed", *chosen, m.dd != nil)
	}

	m, chosen = openedDropdown(t, 80, 24, 2, 3)
	press(t, m, "down", "esc")
	if *chosen != -1 || m.dd != nil {
		t.Errorf("esc chose %d or left the list open", *chosen)
	}
}

// While a list is open it takes every key: a letter that is a binding of the list screen does nothing.
func TestOpenDropdownTakesTheKeysOfTheScreen(t *testing.T) {
	m, _ := openedDropdown(t, 80, 24, 2, 3)
	press(t, m, "q", "/", "c")
	if m.quitting || m.list.editing || m.status != "" || m.dd == nil {
		t.Errorf("a key reached the list screen: quitting %v editing %v status %q open %v",
			m.quitting, m.list.editing, m.status, m.dd != nil)
	}
}

func TestDropdownDrawsBelowItsFieldAndMarksTheCursorWithoutColour(t *testing.T) {
	m, _ := openedDropdown(t, 80, 24, 6, 4)
	view := drawn(m)
	assertViewFits(t, view, 80, 24)
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[5], "╭") || !strings.Contains(lines[6], "> One") ||
		!strings.Contains(lines[7], "  Two") || !strings.Contains(lines[8], "  Three") {
		t.Fatalf("the list is not drawn under its field:\n%s", view)
	}
	if got := ansi.StringWidth(lines[6][:strings.Index(lines[6], "> One")]); got != 8 {
		t.Errorf("the entries start at column %d, want the field's column 6 plus border and gap", got)
	}
	press(t, m, "down")
	if view := drawn(m); !strings.Contains(view, "> Two") || strings.Contains(view, "> One") {
		t.Errorf("the marker did not follow the cursor:\n%s", view)
	}
}

func TestDropdownStaysInsideTheTerminal(t *testing.T) {
	m, _ := openedDropdown(t, 80, 24, 78, 23)
	view := drawn(m)
	assertViewFits(t, view, 80, 24)
	if !strings.Contains(view, "Three") {
		t.Errorf("a field at the right-bottom corner pushes the list out of sight:\n%s", view)
	}
	b := m.dd.box
	if b.x1 > 80 || b.y1 > 24 || b.x0 < 0 || b.y0 < 0 {
		t.Errorf("box %+v leaves the terminal", b)
	}
}

func TestDropdownClosesWhereItsFieldIsGone(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.openDropdown("field", []string{"One"}, 0, func(int) tea.Cmd { return nil })
	if drawn(m); m.dd != nil {
		t.Error("a list whose field the frame did not draw stayed open")
	}
	m, _ = openedDropdown(t, 80, 24, 2, 3)
	m.screen, m.ask = screenConfirm, &question{title: "Sure?", keys: "y yes · n/{back} no"}
	if drawn(m); m.dd != nil {
		t.Error("a list stayed open over a question")
	}
}

// The screens that own a field draw it on every frame and so note its place again; a test that clicks goes
// to the handler directly for that reason.
func TestDropdownClicksChooseOrClose(t *testing.T) {
	m, chosen := openedDropdown(t, 80, 24, 6, 4)
	drawn(m)
	b := m.dd.box
	// The entries start under the top border, inside the side borders.
	m.clickDropdown(b.x0+2, b.y0+2)
	if *chosen != 1 || m.dd != nil {
		t.Fatalf("a click on the second value chose %d, open %v", *chosen, m.dd != nil)
	}

	m, chosen = openedDropdown(t, 80, 24, 6, 4)
	drawn(m)
	m.clickDropdown(70, 20)
	if *chosen != -1 || m.dd != nil {
		t.Errorf("a click outside chose %d or kept the list open", *chosen)
	}

	m, chosen = openedDropdown(t, 80, 24, 6, 4)
	drawn(m)
	b = m.dd.box
	m.clickDropdown(b.x0, b.y0+2)
	if *chosen != -1 || m.dd != nil {
		t.Errorf("a click on the border chose %d or kept the list open", *chosen)
	}
}

func TestChooseDropdownIgnoresAValueOutOfRange(t *testing.T) {
	m, chosen := openedDropdown(t, 80, 24, 2, 3)
	if cmd := m.chooseDropdown(7); cmd != nil || m.dd != nil || *chosen != -1 {
		t.Errorf("an out-of-range value chose %d", *chosen)
	}
	if cmd := m.chooseDropdown(0); cmd != nil {
		t.Error("choosing without an open list returned a command")
	}
}

func TestDropdownFieldCutsItsValueAndKeepsLabelAndArrow(t *testing.T) {
	restoreRenderer(t)
	setProfile(profileNone)
	full := ansi.Strip(dropdownField("Group", "Provider", false, 40))
	if full != " Group: Provider ▾ " {
		t.Errorf("field = %q", full)
	}
	if dropdownWidth("Group", "Provider") != lipgloss.Width(full) {
		t.Errorf("dropdownWidth = %d, want %d", dropdownWidth("Group", "Provider"), lipgloss.Width(full))
	}
	got := ansi.Strip(dropdownField("Group", "Provider", false, 14))
	if lipgloss.Width(got) > 14 || !strings.Contains(got, "Group: ") || !strings.Contains(got, "…") || !strings.Contains(got, "▾") {
		t.Errorf("field at 14 = %q", got)
	}
	// Even a width that holds nothing keeps one cell of the value.
	if got := ansi.Strip(dropdownField("Group", "Provider", false, 1)); !strings.Contains(got, "▾") || !strings.Contains(got, "Group:") {
		t.Errorf("field at 1 = %q", got)
	}
}

// Without colour the focus reads from the marker, and a focused field carries no colour sequence.
func TestDropdownFocusReadsWithoutColour(t *testing.T) {
	m, _ := openedDropdown(t, 80, 24, 6, 4)
	if barMarker(true) != "> " || barMarker(false) != "  " {
		t.Fatalf("markers = %q, %q", barMarker(true), barMarker(false))
	}
	for _, focused := range []bool{true, false} {
		if field := dropdownField("Group", "Provider", focused, 40); colourSGR.MatchString(shown(field)) {
			t.Errorf("focused %v: the field carries a colour: %q", focused, field)
		}
	}
	if view := m.render(); colourSGR.MatchString(view) {
		t.Errorf("the frame carries a colour: %q", view)
	}
}

func TestSearchFieldShowsPlaceholderWhileIdleAndTheContentOtherwise(t *testing.T) {
	restoreRenderer(t)
	setProfile(profileNone)
	useLanguage(t, "en")
	want := tr(tSearchField)
	if !strings.Contains(want, "…") || !strings.HasPrefix(want, "/ ") {
		t.Fatalf("placeholder = %q", want)
	}
	idle := ansi.Strip(searchField("", 24))
	if lipgloss.Width(idle) != 24 || !strings.Contains(idle, want) {
		t.Errorf("idle field = %q", idle)
	}
	typed := ansi.Strip(searchField("/ wiki", 24))
	if lipgloss.Width(typed) != 24 || !strings.Contains(typed, "/ wiki") || strings.Contains(typed, want) {
		t.Errorf("typed field = %q", typed)
	}
	if cut := ansi.Strip(searchField("/ "+strings.Repeat("x", 50), 20)); lipgloss.Width(cut) != 20 {
		t.Errorf("a long content is not cut to the field: %d cells", lipgloss.Width(cut))
	}
}

func TestSearchContentFollowsTheFilterState(t *testing.T) {
	m := frameModel(t, 80, 24)
	if got := searchContent(&m.list, 30); got != "" {
		t.Errorf("idle content = %q", got)
	}
	press(t, m, "/")
	typeText(t, m, "bra")
	if got := ansi.Strip(searchContent(&m.list, 30)); !strings.HasPrefix(got, "/ bra") {
		t.Errorf("typing content = %q", got)
	}
	press(t, m, "enter")
	if got := searchContent(&m.list, 30); got != "/ bra" {
		t.Errorf("kept content = %q", got)
	}
	if got := searchContent(&m.list, 5); got != "/ bra" && !strings.HasPrefix(got, "/ b") {
		t.Errorf("content in a narrow room = %q", got)
	}
}

func TestFieldRowsWrapFieldsAndPlaceTheSearchFieldAtTheRightEdge(t *testing.T) {
	restoreRenderer(t)
	setProfile(profileNone)
	useLanguage(t, "en")
	search := func(int) string { return "" }
	fields := []string{"Group: A ▾", "Sort: Name ▾"}

	lines, plainSearch := fieldRows(fields, search, 80)
	if len(lines) != 1 || lipgloss.Width(lines[0]) != 80 || plainSearch == "" || !strings.HasSuffix(ansi.Strip(lines[0]), plainSearch) {
		t.Fatalf("one line expected, search field at the right edge:\n%q (%q)", lines, plainSearch)
	}
	if !strings.HasPrefix(ansi.Strip(lines[0]), "Group: A ▾  Sort: Name ▾") {
		t.Errorf("fields are not two cells apart: %q", lines[0])
	}

	// A width that fits the fields but not a search field beside them puts it on a line of its own.
	narrow := lipgloss.Width(fields[0]) + 2 + lipgloss.Width(fields[1]) + 2
	lines, plainSearch = fieldRows(fields, search, narrow)
	if len(lines) != 2 || plainSearch == "" {
		t.Errorf("narrow: %d lines, search %q:\n%q", len(lines), plainSearch, lines)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > narrow {
			t.Errorf("line %q is wider than %d", line, narrow)
		}
	}

	// Too narrow for a search field, the fields only wrap and it is left out.
	lines, plainSearch = fieldRows(fields, search, 5)
	if plainSearch != "" || len(lines) < 2 {
		t.Errorf("tiny: search %q, %d lines", plainSearch, len(lines))
	}

	if lines, _ := fieldRows(nil, search, 60); len(lines) != 1 {
		t.Errorf("no fields: %d lines, want the search field alone", len(lines))
	}
}
