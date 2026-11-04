package tui

// baseScreen is the screen the current dialog stands over; it is the current screen itself when that is no
// dialog.
func (m *Model) baseScreen() screen {
	switch m.screen {
	case screenConfirm, screenPrompt, screenPopup:
		return m.home
	}
	return m.screen
}

// renderAs draws with the given screen and, when width is positive, workspace width, and then restores
// them, so a view drawn only as a background changes no state.
func (m *Model) renderAs(s screen, width int, render func()) {
	screen, w := m.screen, m.width
	defer func() { m.screen, m.width = screen, w }()
	m.screen = s
	if width > 0 {
		m.width = width
	}
	render()
}
