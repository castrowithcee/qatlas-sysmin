package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// An area is a task the person came to do; the areas are the tab line. An area draws into the workspace under
// the tab line through areaBody, and handles its own keys through areaKey, after the keys of the frame.
type area int

const (
	areaOverview area = iota
	areaProjects
	areaActivity
	areaAccess
	areaLibrary
	areaSettings
	areaCount
)

// key is the English name of the area, whatever language is drawn: the name commands are given by.
func (a area) key() string {
	return [...]string{"Overview", "Projects", "Activity", "Access", "Library", "Settings"}[a]
}

// title is the name of the area as drawn.
func (a area) title() string {
	return tr([...]textID{tAreaOverview, tAreaProjects, tAreaActivity, tAreaAccess, tAreaLibrary, tAreaSettings}[a])
}

// areaByName finds the area whose English name is name, ignoring case.
func areaByName(name string) (area, bool) {
	for a := area(0); a < areaCount; a++ {
		if strings.EqualFold(a.key(), name) {
			return a, true
		}
	}
	return areaOverview, false
}

// enableAreas makes the area screen the base of the interface, served by backend.
func (m *Model) enableAreas(backend Backend) {
	m.backend = backend
	m.home, m.screen, m.area = screenArea, screenArea, areaOverview
	m.prj = newProjectsState()
}

// setArea shows an area, on its first page. Unsaved input of Settings is asked about first.
func (m *Model) setArea(a area) tea.Cmd { return m.show(a, 0) }

// show makes page sub of area a the screen; only Settings has pages.
func (m *Model) show(a area, sub int) tea.Cmd {
	return m.guarded(func() tea.Cmd {
		m.area = a
		m.resetForms()
		m.clearMessages()
		m.ovr.cursor = 0
		if a == areaSettings {
			m.set.sub = sub
		}
		return m.enterPage()
	})
}

// enterPage asks for what the page just shown needs and has not yet.
func (m *Model) enterPage() tea.Cmd {
	switch {
	case m.area == areaOverview:
		return m.overviewCmd()
	case m.area == areaProjects:
		m.prj.log, m.prj.upd, m.prj.focusDetail = nil, nil, false
		return m.projectsCmd()
	case m.area == areaSettings:
		m.loadForms()
		switch m.set.sub {
		case subVault:
			return m.settingsCmd()
		case subUpdate:
			return m.checkUpdate(false)
		}
	}
	return nil
}

// updateArea handles one key of the area screen.
func (m *Model) updateArea(key tea.KeyPressMsg) tea.Cmd {
	if m.typing() {
		if m.area == areaProjects {
			return m.projectsEditKey(key)
		}
		return m.editKey(key)
	}
	switch {
	case keyIs(key, actHelp):
		m.openPopup(popHelp)
		return nil
	case keyIs(key, actCommand):
		m.openPopup(popCommand)
		return nil
	case m.infoKey(key):
		m.openPopup(popInfo)
		return nil
	case keyIs(key, actQuit):
		return m.guarded(m.quit)
	}
	if m.area == areaSettings {
		if cmd, ok := m.settingsKey(key); ok {
			return cmd
		}
	}
	if m.area == areaProjects {
		if cmd, ok := m.projectsKey(key); ok {
			return cmd
		}
	}
	switch {
	case keyIs(key, actRight), keyIs(key, actNext):
		return m.setArea((m.area + 1) % areaCount)
	case keyIs(key, actLeft), keyIs(key, actPrev):
		return m.setArea((m.area + areaCount - 1) % areaCount)
	case keyIs(key, actReload):
		return m.statusCmd()
	}
	if len(key.Text) == 1 && key.Text[0] >= '1' && key.Text[0] < '1'+byte(areaCount) {
		return m.setArea(area(key.Text[0] - '1'))
	}
	if m.area == areaOverview {
		return m.overviewKey(key)
	}
	return nil
}

// infoKey reports whether key asks for the info: the letter, or the second key of the preferences, which also
// works where text is typed.
func (m *Model) infoKey(key tea.KeyPressMsg) bool {
	return keyIs(key, actInfo) || (infoExtra != "" && key.String() == infoExtra)
}

// areaView is the workspace of the area screen: the tab line and the area under it.
func (m *Model) areaView() string {
	line, spans := m.tabLine()
	top := m.workspaceTop()
	for i, span := range spans {
		m.addHit(span.x0, top, span.x1, top+1, func() tea.Cmd { return m.setArea(area(i)) })
	}
	m.keyHint(m.areaKeys())
	return line + "\n\n" + m.areaBody() + m.notes()
}

// tabLine draws the tab line and the cells of each tab. Where the titles do not fit the width, the tabs that
// are not active shrink to their digit.
func (m *Model) tabLine() (string, []navSpan) {
	words := make([]string, areaCount)
	for a := range words {
		words[a] = area(a).title()
	}
	line, cells := tabStrip(words, int(m.area), true)
	if width := m.usable(0); lipgloss.Width(line) > width {
		for a := range words {
			if a != int(m.area) {
				words[a] = string(rune('1' + a))
			}
		}
		line, cells = tabStrip(words, int(m.area), true)
	}
	spans := make([]navSpan, len(cells))
	for i, c := range cells {
		spans[i] = navSpan{min(c[0], m.usable(0)), min(c[1], m.usable(0))}
	}
	return clipCells(line, m.usable(0)), spans
}

// areaKeys is the key line of the area screen.
func (m *Model) areaKeys() string {
	if m.typing() {
		if m.area == areaProjects {
			return "type to filter · up/down move · enter keep filter · {back} clear filter"
		}
		return "enter next · {back} stop · {save} save"
	}
	keys := []string{"1-6 area"}
	switch m.area {
	case areaOverview:
		keys = append(keys, "up/down move")
		if row := m.overviewCurrent(); row.key != "" {
			keys = append(keys, row.key)
		}
	case areaSettings:
		keys = append(keys, "left/right page")
		keys = append(keys, m.settingsKeys()...)
	case areaProjects:
		keys = append(keys, m.projectsKeys()...)
	default:
		keys = append(keys, "left/right move")
	}
	switch state := m.vaultState(); {
	case state == control.VaultAbsent:
	case state == control.VaultLocked:
		keys = append(keys, "{lock} unlock")
	case !m.serviceUp():
		keys = append(keys, "ctrl+r retry")
	case m.adminActive():
		keys = append(keys, "{lock} end admin")
	default:
		keys = append(keys, "{lock} admin")
	}
	keys = append(keys, "{info} info", "{help} help", "{command} cmds", "q quit")
	return strings.Join(keys, " · ")
}

// areaBody is the area under the tab line. Areas without content show one line.
func (m *Model) areaBody() string {
	switch m.area {
	case areaOverview:
		return m.overviewBody()
	case areaSettings:
		return m.settingsBody()
	case areaProjects:
		return m.projectsBody()
	}
	return m.wrapped(styles.hint, tr(tListEmpty))
}
