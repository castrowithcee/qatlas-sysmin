package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTitleLinePlacesTheNoteAtTheRightEdgeOnlyWhereItFits(t *testing.T) {
	restoreRenderer(t)
	setProfile(profileNone)
	got := ansi.Strip(titleLine("Entries (4)", "2/4", 30))
	if len(got) != 30 || !strings.HasPrefix(got, "Entries (4)") || !strings.HasSuffix(got, "2/4") {
		t.Errorf("line = %q", got)
	}
	if got := ansi.Strip(titleLine("Entries (4)", "2/4", 13)); got != "Entries (4)" {
		t.Errorf("a note without two cells of gap must be left out, got %q", got)
	}
	if got := ansi.Strip(titleLine("A very long title indeed", "", 8)); len([]rune(got)) > 8 {
		t.Errorf("the title is not cut to the width: %q", got)
	}
	if got := ansi.Strip(titleLine("T", "n", 0)); got == "" {
		t.Error("a zero width must still keep one cell of the title")
	}
}

// A list screen carries its title line above the frame, with the count and the position; the title is plain
// text without colour as well.
func TestListTitleStandsAboveThePanelWithItsCountAndPosition(t *testing.T) {
	m := frameModel(t, 80, 24)
	lines := strings.Split(plain(m), "\n")
	title := lines[m.workspaceTop()]
	if !strings.HasPrefix(title, "Entries (4)") || !strings.HasSuffix(strings.TrimRight(title, " "), "1/4") {
		t.Fatalf("title line = %q", title)
	}
	if blank, frame := lines[m.workspaceTop()+1], lines[m.workspaceTop()+2]; strings.TrimSpace(blank) != "" ||
		!strings.ContainsAny(frame, "╭╔") || strings.Contains(frame, "Entries") {
		t.Errorf("the title has no gap above the frame, or the frame carries the title:\n%s", plain(m))
	}
}

// With no height to spare the gap under the title goes, not a row of the table; the smallest size keeps the
// title and the footer whole without a frame.
func TestListTitleIsCompactWhereHeightIsShort(t *testing.T) {
	m := frameModel(t, 80, 24)
	if !m.titleBlank() || m.titleRows() != 2 {
		t.Fatalf("a roomy terminal drops the gap: blank %v rows %d", m.titleBlank(), m.titleRows())
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: tableFrameMinHeight + 4})
	if m.titleBlank() || m.titleRows() != 1 {
		t.Fatalf("the gap is kept at height %d (workspace %d)", m.termHeight, m.height)
	}
	lines := strings.Split(plain(m), "\n")
	if !strings.HasPrefix(lines[m.workspaceTop()], "Entries (") || !strings.ContainsAny(lines[m.workspaceTop()+1], "╭╔") {
		t.Errorf("compact title:\n%s", plain(m))
	}
	m.Update(tea.WindowSizeMsg{Width: minimumWidth, Height: minimumHeight})
	view := plain(m)
	assertViewFits(t, view, minimumWidth, minimumHeight)
	if m.tableFramed() || !strings.Contains(view, "Entries") || !strings.Contains(view, "q quit") {
		t.Errorf("the smallest size loses its title or its footer:\n%s", view)
	}
}

func TestTitledAndDialogPanelStandUnderTheirTitleLine(t *testing.T) {
	m := frameModel(t, 80, 24)
	got := strings.Split(ansi.Strip(m.dialogPanelOf().top("Name the entry", "1/3")), "\n")
	if !strings.HasPrefix(got[0], "Name the entry") || !strings.HasSuffix(strings.TrimRight(got[0], " "), "1/3") ||
		!strings.ContainsAny(got[m.titleRows()], "╭╔") || strings.Contains(got[m.titleRows()], "Name") {
		t.Errorf("dialog title:\n%s", strings.Join(got, "\n"))
	}
}

// A click on a row follows the layout the title line shifts.
func TestClickFollowsTheTitleLine(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.render()
	row := -1
	for y, line := range strings.Split(plain(m), "\n") {
		if strings.Contains(line, "delta") {
			row = y
		}
	}
	m.Update(tea.MouseClickMsg{X: 5, Y: row, Button: tea.MouseLeft})
	if name, _ := m.selected(); name != "delta" {
		t.Errorf("a click on a row selected %q, want delta", name)
	}
}
