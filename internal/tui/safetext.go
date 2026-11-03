package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// safeText is text from outside the interface with everything removed that could act on the terminal: escape
// sequences and control characters, but not line ends. Tabs become blanks.
func safeText(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < ' ' || (r >= 0x7f && r < 0xa0):
			return -1
		}
		return r
	}, s)
}

// oneLine is text from outside the interface on a single line, with nothing in it that could act on the
// terminal.
func oneLine(s string) string { return strings.Join(strings.Fields(safeText(s)), " ") }
