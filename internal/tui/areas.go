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
}

// updateArea handles one key of the area screen.
func (m *Model) updateArea(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case keyIs(key, actQuit):
		return m.quit()
	case keyIs(key, actRight), keyIs(key, actNext):
		m.area = (m.area + 1) % areaCount
		return nil
	case keyIs(key, actLeft), keyIs(key, actPrev):
		m.area = (m.area + areaCount - 1) % areaCount
		return nil
	case keyIs(key, actReload):
		return m.statusCmd()
	}
	if len(key.Text) == 1 && key.Text[0] >= '1' && key.Text[0] < '1'+byte(areaCount) {
		m.area = area(key.Text[0] - '1')
		return nil
	}
	return m.areaKey(key)
}

// areaKey is the key handling of the current area.
func (m *Model) areaKey(key tea.KeyPressMsg) tea.Cmd {
	if m.area == areaOverview && keyIs(key, actEnter) && m.vaultState() == control.VaultAbsent {
		m.openOverlay(entrySetup, 2, false, nil, false)
	}
	return nil
}

// areaView is the workspace of the area screen: the tab line and the area under it.
func (m *Model) areaView() string {
	line, spans := m.tabLine()
	top := m.workspaceTop()
	for i, span := range spans {
		m.addHit(span.x0, top, span.x1, top+1, func() tea.Cmd { m.area = area(i); return nil })
	}
	m.keyHint(m.areaKeys())
	return line + "\n\n" + m.areaBody()
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
	keys := []string{"1-6 area", "left/right move"}
	switch state := m.vaultState(); {
	case state == control.VaultAbsent:
		keys = append(keys, "enter set up")
	case state == control.VaultLocked:
		keys = append(keys, "{lock} unlock")
	case !m.serviceUp():
		keys = append(keys, "ctrl+r retry")
	case m.adminActive():
		keys = append(keys, "{lock} end admin")
	default:
		keys = append(keys, "{lock} admin")
	}
	keys = append(keys, "q quit")
	return strings.Join(keys, " · ")
}

// areaBody is the area under the tab line. Areas without content show one line.
func (m *Model) areaBody() string {
	if m.area == areaOverview {
		return m.overviewBody()
	}
	return m.wrapped(styles.hint, tr(tListEmpty))
}

// overviewBody is the state of the service and the vault with the next step.
func (m *Model) overviewBody() string {
	var mark, next string
	switch state := m.vaultState(); {
	case !m.svc.known:
		return m.wrapped(styles.hint, tr(tChecking))
	case !m.serviceUp():
		mark, next = statusMark(statusFail, tr(tServiceDown)), tr(tNextRetry, keyName(keyOf(actReload)))
	case state == control.VaultAbsent:
		mark, next = statusMark(statusWarn, tr(tVaultNone)), tr(tNextSetup)
	case state == control.VaultLocked:
		mark, next = statusMark(statusWarn, tr(tVaultLocked)), tr(tNextUnlock, keyLabel(actLock))
	case state == control.VaultUnlocked:
		mark = statusMark(statusOK, tr(tVaultOpen))
	default:
		mark = statusMark(statusWarn, tr(tErrService))
	}
	out := m.wrapped(lipgloss.NewStyle(), mark)
	if next != "" {
		out += "\n" + m.indented(next)
	}
	return out
}
