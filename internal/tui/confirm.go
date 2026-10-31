package tui

import tea "charm.land/bubbletea/v2"

// confirmAsk is a yes/no question over the area screen; yes is what follows the answer y.
type confirmAsk struct {
	yes func() tea.Cmd
	// notice makes the dialog a message: enter, esc, y and n all just close it.
	notice bool
}

// askConfirm shows q as a modal over the screen. Only y runs yes; n and esc drop it.
func (m *Model) askConfirm(q question, yes func() tea.Cmd) {
	m.clearMessages()
	m.confirm, m.ask, m.screen = &confirmAsk{yes: yes}, &q, screenConfirm
}

// updateConfirm handles the answer to the question.
func (m *Model) updateConfirm(key tea.KeyPressMsg) tea.Cmd {
	if m.confirm.notice {
		if keyIs(key, actBack) || keyIs(key, actEnter) || keyIs(key, actYes) || keyIs(key, actNo) {
			m.closeDialog()
		}
		return nil
	}
	switch {
	case keyIs(key, actYes):
		yes := m.confirm.yes
		m.closeDialog()
		return yes()
	case keyIs(key, actNo), keyIs(key, actBack):
		m.closeDialog()
		m.status = tr(tCancelled)
	}
	return nil
}
