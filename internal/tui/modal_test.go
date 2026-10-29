package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// dialogRows returns the first and last row of the modal frame of a question, and the column of its left
// edge; the warning mark in the row under its top border tells it from the frame of the workspace behind it.
func dialogRows(view string) (top, bottom, left int, ok bool) {
	top = -1
	rows := strings.Split(view, "\n")
	for i, line := range rows {
		if at := strings.Index(line, "╭─"); at >= 0 && i+1 < len(rows) && strings.Contains(rows[i+1], "│ "+warnMark+" ") {
			top, left = i, lipgloss.Width(line[:at])
		} else if top >= 0 && i > top && closesAt(line, left) {
			return top, i, left, true
		}
	}
	return 0, 0, 0, false
}

// closesAt reports whether a "╰" stands in the line at the given column; a field or panel behind the dialog may
// have its own earlier on the same row.
func closesAt(line string, column int) bool {
	for at := 0; at < len(line); {
		i := strings.Index(line[at:], "╰")
		if i < 0 {
			return false
		}
		if lipgloss.Width(line[:at+i]) == column {
			return true
		}
		at += i + len("╰")
	}
	return false
}

func hasQuestionModal(view string) bool {
	_, _, _, ok := dialogRows(view)
	return ok
}

func TestOverlayKeepsTheBackgroundReadableWithoutColour(t *testing.T) {
	out := overlay("aaaaaaaaaa\nbbbbbbbbbb\ncccccccccc", []string{"[X]"}, 10, 3)
	rows := strings.Split(ansi.Strip(out), "\n")
	if rows[0] != "aaaaaaaaaa" || rows[1] != "bbb[X]bbbb" || rows[2] != "cccccccccc" {
		t.Errorf("overlay = %q", rows)
	}
}
