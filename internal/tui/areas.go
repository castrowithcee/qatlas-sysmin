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

// zone is the part of the area screen that takes the keys: the tab line, the page strip of an area with
// pages, or the content under them.
type zone int

const (
	zoneTabs zone = iota
	zoneSubs
	zoneContent
)

// enableAreas makes the area screen the base of the interface, served by backend.
func (m *Model) enableAreas(backend Backend) {
	m.backend = backend
	m.home, m.screen, m.area, m.zone = screenArea, screenArea, areaOverview, zoneTabs
	m.prj = newProjectsState()
}

// setArea shows an area, on its first page, with the focus in its content. Unsaved input of Settings is asked
// about first.
func (m *Model) setArea(a area) tea.Cmd { return m.show(a, 0) }

// show makes page sub of area a the screen, with the focus in its content; only Settings, Activity and
// Library have pages.
func (m *Model) show(a area, sub int) tea.Cmd { return m.showIn(a, sub, zoneContent) }

// showIn is show with the focus on zone z.
func (m *Model) showIn(a area, sub int, z zone) tea.Cmd {
	return m.guarded(func() tea.Cmd {
		m.area, m.zone = a, z
		m.resetForms()
		m.clearMessages()
		m.ovr.cursor = 0
		if a == areaSettings {
			m.set.sub = sub
		}
		if a == areaLibrary {
			m.lib.sub = sub
		}
		if a == areaActivity {
			m.act.sub = sub
		}
		m.closeDetails()
		return m.enterPage()
	})
}

// showActivity makes page sub of Activity the screen.
func (m *Model) showActivity(sub int) tea.Cmd { return m.show(areaActivity, sub) }

// enterPage asks for what the page just shown needs and has not yet.
func (m *Model) enterPage() tea.Cmd {
	switch {
	case m.area == areaOverview:
		return m.overviewCmd()
	case m.area == areaProjects:
		m.prj.log, m.prj.upd, m.prj.focusDetail, m.prj.flow = nil, nil, false, nil
		return m.projectListCmd()
	case m.area == areaAccess:
		return m.accessCmd()
	case m.area == areaActivity && m.act.sub == actRuns:
		return m.runsCmd(m.act.runs.project)
	case m.area == areaActivity:
		return m.activityCmd()
	case m.area == areaLibrary:
		switch m.lib.sub {
		case libCredentials:
			return m.credentialsCmd()
		case libChannels:
			return m.channelsCmd()
		case libTemplates:
			return m.templatesCmd()
		}
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
		switch {
		case m.flowOpen():
			return m.flowKey(key)
		case m.area == areaProjects:
			return m.projectsEditKey(key)
		}
		if p := m.currentPage(); p.filtering() {
			return m.filterKey(p, key)
		}
		return m.editKey(key)
	}
	// The guided setup is a form: only help, the command line and the info leave it to the frame.
	if m.flowOpen() && !keyIs(key, actHelp) && !keyIs(key, actCommand) && !m.infoKey(key) {
		return m.flowKey(key)
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
	case keyIs(key, actSelfUpdate):
		return m.selfUpdate()
	case keyIs(key, actCopy) && m.copySource() != nil:
		return m.copyCurrent()
	}
	if m.zoneNow() != zoneContent {
		return m.barKey(key)
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
	if cmd, ok := m.dataKey(key); ok {
		return cmd
	}
	if m.area == areaOverview {
		if cmd, ok := m.overviewKey(key); ok {
			return cmd
		}
	}
	// A key an inner level (a log, an update, a detail) leaves unhandled does not leave it: only esc does.
	inner := m.innerLevel()
	switch {
	case keyIs(key, actNext) && !inner:
		m.zone = zoneTabs
	case (keyIs(key, actPrev) || keyIs(key, actBack)) && !inner:
		m.toBar()
	case keyIs(key, actReload):
		return m.statusCmd()
	}
	return m.digitKey(key)
}

// digitKey opens the area of a digit, with the focus in its content.
func (m *Model) digitKey(key tea.KeyPressMsg) tea.Cmd {
	if len(key.Text) == 1 && key.Text[0] >= '1' && key.Text[0] < '1'+byte(areaCount) {
		return m.setArea(area(key.Text[0] - '1'))
	}
	return nil
}

// hasSubs reports whether the area shows a strip of its pages under the tab line.
func (m *Model) hasSubs() bool {
	switch m.area {
	case areaSettings, areaLibrary:
		return true
	case areaActivity:
		return actPages > 1
	}
	return false
}

// barZone is the bar one level above the content: the page strip where the area has one, else the tab line.
func (m *Model) barZone() zone {
	if m.hasSubs() {
		return zoneSubs
	}
	return zoneTabs
}

// toBar hands the focus from the content to the bar above it.
func (m *Model) toBar() { m.zone = m.barZone() }

// zoneNow is the zone that takes the keys. An inner level of the content holds the focus whatever the zone
// was, and a page strip falls back to the tab line in an area without one.
func (m *Model) zoneNow() zone {
	switch {
	case m.innerLevel():
		return zoneContent
	case m.zone == zoneSubs && !m.hasSubs():
		return zoneTabs
	}
	return m.zone
}

// innerLevel reports whether the content shows a level that esc closes before it leaves the content: a
// filter being typed, a field being edited, the guided setup, a log, an update or the detail of a row.
func (m *Model) innerLevel() bool {
	if m.typing() || m.flowOpen() {
		return true
	}
	switch m.area {
	case areaProjects:
		p := m.prj
		return p != nil && (p.log != nil || p.upd != nil || p.focusDetail)
	case areaActivity:
		if m.act.runs.log.open {
			return true
		}
	}
	p := m.currentPage()
	return p != nil && p.open
}

// barKey handles a key while the tab line or the page strip has the focus. The bars wrap round; down, enter
// and tab go one zone further in, esc and up one zone back.
func (m *Model) barKey(key tea.KeyPressMsg) tea.Cmd {
	subs := m.zoneNow() == zoneSubs
	step := 0
	switch {
	case keyIs(key, actLeft):
		step = -1
	case keyIs(key, actRight):
		step = 1
	case keyIs(key, actHome) && !subs:
		return m.showIn(0, 0, zoneTabs)
	case keyIs(key, actEnd) && !subs:
		return m.showIn(areaCount-1, 0, zoneTabs)
	case keyIs(key, actUp), keyIs(key, actBack), keyIs(key, actPrev) && subs:
		m.zone = zoneTabs
	case keyIs(key, actPrev):
		m.zone = zoneContent
	case keyIs(key, actDown), keyIs(key, actEnter), keyIs(key, actNext):
		m.zone = zoneContent
		if !subs && m.hasSubs() {
			m.zone = zoneSubs
		}
	case keyIs(key, actReload):
		return m.statusCmd()
	default:
		return m.digitKey(key)
	}
	if step == 0 {
		return nil
	}
	if !subs {
		return m.showIn(area(wrap(int(m.area)+step, int(areaCount))), 0, zoneTabs)
	}
	switch m.area {
	case areaSettings:
		return m.setSub(m.set.sub + step)
	case areaLibrary:
		return m.setLibSub(m.lib.sub + step)
	case areaActivity:
		return m.setActSub(m.act.sub + step)
	}
	return nil
}

// closeDetails closes the detail of every table page and drops the notes of a refused removal.
func (m *Model) closeDetails() {
	m.acc.page.open, m.lib.cred.page.open, m.lib.chans.page.open = false, false, false
	m.lib.tmpls.page.open, m.act.page.open, m.lib.cred.problems = false, false, nil
	m.act.runs.page.open = false
	if m.act.runs.log.open {
		m.act.runs.log.open = false
		m.act.runs.log.gen++
	}
}

// dataKey handles a key of the pages with a table: the table and its detail first, then the page's own keys.
func (m *Model) dataKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.area == areaActivity && m.act.runs.log.open {
		if cmd, ok := m.runLogKey(key); ok {
			return cmd, true
		}
	}
	p := m.currentPage()
	if p == nil {
		return nil, false
	}
	if m.area == areaActivity && !p.open && keyIs(key, actFilter) {
		// f is the filter of the log and the project of the runs here; the table is searched with /.
		if m.act.sub == actRuns {
			return m.openRunsProject(), true
		}
		return m.openLogFilter(), true
	}
	if cmd, ok := m.pageKey(p, key); ok {
		return cmd, true
	}
	switch m.area {
	case areaAccess:
		return m.accessKey(key)
	case areaLibrary:
		return m.libraryKey(key)
	case areaActivity:
		return m.activityKey(key)
	}
	return nil, false
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
	focused := m.zoneNow() == zoneTabs
	line, cells := tabStrip(words, int(m.area), focused)
	if width := m.usable(0); lipgloss.Width(line) > width {
		for a := range words {
			if a != int(m.area) {
				words[a] = string(rune('1' + a))
			}
		}
		line, cells = tabStrip(words, int(m.area), focused)
	}
	spans := make([]navSpan, len(cells))
	for i, c := range cells {
		spans[i] = navSpan{min(c[0], m.usable(0)), min(c[1], m.usable(0))}
	}
	return clipCells(line, m.usable(0)), spans
}

// areaKeys is the key line of the area screen.
func (m *Model) areaKeys() string {
	if m.flowOpen() {
		return m.flowKeyLine()
	}
	if m.typing() {
		if m.area == areaProjects || m.currentPage().filtering() {
			return "type to filter · up/down move · enter keep filter · {back} clear filter"
		}
		return "enter next · {back} stop · {save} save"
	}
	var keys []string
	switch zone := m.zoneNow(); {
	case zone == zoneTabs:
		keys = []string{"left/right area", "down open"}
	case zone == zoneSubs:
		keys = []string{"left/right page", "down open", "{back} back"}
	case m.area == areaOverview:
		keys = append(keys, "up/down move")
		if row := m.overviewCurrent(); row.key != "" {
			keys = append(keys, row.key)
		}
	case m.area == areaSettings:
		keys = append(keys, m.settingsKeys()...)
	case m.area == areaProjects:
		keys = append(keys, m.projectsKeys()...)
	case m.area == areaAccess:
		keys = append(keys, m.accessKeys()...)
	case m.area == areaLibrary:
		keys = append(keys, m.libraryKeys()...)
	case m.area == areaActivity:
		keys = append(keys, m.activityKeys()...)
	}
	if m.zoneNow() == zoneContent && !m.innerLevel() {
		keys = append(keys, "{back} back")
	}
	keys = append(keys, m.lockKeys()...)
	keys = append(keys, "{info} info", "{help} help", "{command} cmds", "q quit")
	return strings.Join(keys, " · ")
}

// lockKeys is what the lock key does now, as a key group, or what stands in for it.
func (m *Model) lockKeys() []string {
	switch state := m.vaultState(); {
	case state == control.VaultAbsent:
		return nil
	case state == control.VaultLocked:
		return []string{"{lock} unlock"}
	case !m.serviceUp():
		return []string{"ctrl+r retry"}
	case m.adminActive():
		return []string{"{lock} end admin"}
	}
	return []string{"{lock} admin"}
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
	case areaAccess:
		return m.accessBody()
	case areaLibrary:
		return m.libraryBody()
	case areaActivity:
		return m.activityBody()
	}
	return m.wrapped(styles.hint, tr(tListEmpty))
}
