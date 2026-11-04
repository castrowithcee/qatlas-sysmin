package tui

// titleBlank reports whether a blank line stands between the title line and the panels, as under the title
// of Settings and the forms. A terminal with no height to spare keeps the rows instead.
func (m *Model) titleBlank() bool { return m.height >= tableFrameMinHeight+2 }
