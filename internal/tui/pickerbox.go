package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// colourTick colours the first ticked box of a drawn row and sets the style of the cell it stands in again
// behind it.
func colourTick(ui *uiContext, row string, cell lipgloss.Style) string {
	i := strings.Index(row, "[x]")
	if i < 0 {
		return row
	}
	set := cell.Render("x")
	set = set[:strings.Index(set, "x")]
	return row[:i] + ui.st(kittui.StyleOK).Render("[x]") + set + row[i+3:]
}
