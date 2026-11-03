package tui

// titleBlank reports whether a blank line stands between the title line and the panels, as under the title
// of Settings and the forms. A terminal with no height to spare keeps the rows instead.
func (m *Model) titleBlank() bool { return m.height >= tableFrameMinHeight+2 }

// titleRows is the number of lines titled adds above a panel.
func (m *Model) titleRows() int {
	if m.titleBlank() {
		return 2
	}
	return 1
}

// titled is the title line of a framed screen of width cells with the gap under it, each ended by a line break.
func (m *Model) titled(title, note string, width int) string {
	out := m.ui.styles.TitleLine(title, note, width) + "\n"
	if m.titleBlank() {
		out += "\n"
	}
	return out
}
