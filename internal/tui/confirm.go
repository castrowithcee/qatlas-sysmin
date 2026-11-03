package tui

import tea "charm.land/bubbletea/v2"

// confirmAsk is a yes/no question over the area screen; yes is what follows the answer y.
type confirmAsk struct {
	yes func() tea.Cmd
	// notice makes the dialog a message: enter, esc, y and n all just close it.
	notice bool
	// cancel runs when the question is dropped unanswered, such as to overwrite a secret it holds.
	cancel func()
}

// askConfirm shows q as a modal over the screen. Only y runs yes; n and esc drop it.
func (m *Model) askConfirm(q question, yes func() tea.Cmd) {
	m.clearMessages()
	m.confirm, m.ask, m.screen = &confirmAsk{yes: yes}, &q, screenConfirm
}

// updateConfirm handles the answer to the question.
func (m *Model) updateConfirm(key tea.KeyPressMsg) tea.Cmd {
	if m.confirm.notice {
		if m.keyIs(key, actBack) || m.keyIs(key, actEnter) || m.keyIs(key, actYes) || m.keyIs(key, actNo) {
			m.closeDialog()
		}
		return nil
	}
	switch {
	case m.keyIs(key, actYes):
		yes := m.confirm.yes
		m.closeDialog()
		return yes()
	case m.keyIs(key, actNo), m.keyIs(key, actBack):
		if cancel := m.confirm.cancel; cancel != nil {
			cancel()
		}
		m.closeDialog()
		m.status = m.tr(tCancelled)
	}
	return nil
}
