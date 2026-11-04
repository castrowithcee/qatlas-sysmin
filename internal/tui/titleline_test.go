package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

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
	if !m.titleBlank() {
		t.Fatalf("a roomy terminal drops the gap: blank %v", m.titleBlank())
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: tableFrameMinHeight + 4})
	if m.titleBlank() {
		t.Fatalf("the gap is kept at height %d (workspace %d)", m.termHeight, m.height)
	}
	lines := strings.Split(plain(m), "\n")
	if !strings.HasPrefix(lines[m.workspaceTop()], "Entries (") || !strings.ContainsAny(lines[m.workspaceTop()+1], "╭╔") {
		t.Errorf("compact title:\n%s", plain(m))
	}
	m.Update(tea.WindowSizeMsg{Width: minimumWidth, Height: minimumHeight})
	view := plain(m)
	assertViewFits(t, view, minimumWidth, minimumHeight)
	if !strings.Contains(view, "Entries") || !strings.Contains(view, "q quit") {
		t.Errorf("the smallest size loses its title or its footer:\n%s", view)
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
