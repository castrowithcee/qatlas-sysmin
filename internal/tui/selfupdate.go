package tui

import tea "charm.land/bubbletea/v2"

// updateAvailable is the newer stable release the last lookup found, "" for none.
func (m *Model) updateAvailable() string {
	u := m.set.upd
	if u.phase != updDone || u.info.Dev {
		return ""
	}
	return u.info.Latest
}

// selfUpdate asks before sysmin installs the newer release. On y the interface quits and Run reports the
// version; the caller runs sudo sysmin update on the terminal and starts the interface again. Without a known
// release it asks for one.
func (m *Model) selfUpdate() tea.Cmd {
	latest := m.updateAvailable()
	if latest == "" {
		m.status = tr(tUpdNone)
		return m.checkUpdate(true)
	}
	return m.guarded(func() tea.Cmd {
		m.askConfirm(question{
			title: tr(tUpdTitle, m.version, latest), consequence: tr(tUpdConsequence), warn: true,
			keys: "y update · n/{back} cancel",
		}, func() tea.Cmd {
			m.updateTo = latest
			return m.quit()
		})
		return nil
	})
}
