package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// An area is a task the person came to do; the areas are the tab line. An area draws into the workspace under
// the tab line through areaBody, and handles its own keys through areaKey, after the keys of the frame.
type area int

const (
	areaOverview area = iota
	areaHost
	areaActivity
	areaSettings
	areaCount
)

// key is the English name of the area, whatever language is drawn: the name commands are given by.
func (a area) key() string {
	return [...]string{"Overview", "Host", "Activity", "Settings"}[a]
}

// id is the name of the area in the preferences.
func (a area) id() string { return strings.ToLower(a.key()) }

// areaIDs are the names of every area in the preferences, in the order of the tab line.
func areaIDs() []string {
	ids := make([]string, areaCount)
	for a := area(0); a < areaCount; a++ {
		ids[a] = a.id()
	}
	return ids
}

// title is the name of the area as drawn.
func (a area) title(ui *uiContext) string { return ui.tr(a.titleID()) }

// titleID is the catalog key of the name of the area.
func (a area) titleID() textID {
	return [...]textID{tAreaOverview, tAreaHost, tAreaActivity, tAreaSettings}[a]
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

// The zones of the area screen are the kit's: the tab line, the sub-tabs of an area with more than one, the
// rows of the content under them and the detail beside the rows. Tab walks them in the kit's order.
const (
	zoneTabs    = kittui.ZoneTabs
	zoneSubs    = kittui.ZoneSubtabs
	zoneContent = kittui.ZoneRows
	zoneDetail  = kittui.ZoneDetail
)

// frameAreas are the areas of the tab line, in its order, with their sub-tabs. The IDs are the names of the
// areas and pages commands and the preferences use.
func frameAreas() []kittui.Area {
	tabs := func(n int, id func(int) string, title func(int) textID) []kittui.Tab {
		out := make([]kittui.Tab, n)
		for i := range out {
			out[i] = kittui.Tab{ID: kittui.TabID(id(i)), Title: title(i)}
		}
		return out
	}
	areas := make([]kittui.Area, areaCount)
	for a := area(0); a < areaCount; a++ {
		areas[a] = kittui.Area{ID: a.areaID(), Title: a.titleID()}
	}
	areas[areaHost].Tabs = tabs(hostCount, hostKey, hostTitleID)
	areas[areaActivity].Tabs = tabs(actPages, actKey, actTitleID)
	areas[areaSettings].Tabs = tabs(subCount, subKey, subTitleID)
	return areas
}

// areaID is the ID of the area on the tab line.
func (a area) areaID() kittui.AreaID { return kittui.AreaID(a.id()) }

// areaOf is the area of an ID of the tab line.
func areaOf(id kittui.AreaID) (area, bool) {
	for a := area(0); a < areaCount; a++ {
		if a.areaID() == id {
			return a, true
		}
	}
	return areaOverview, false
}

// sub is the page of the current area shown.
func (m *Model) sub() int {
	switch m.area {
	case areaSettings:
		return m.set.sub
	case areaActivity:
		return m.act.sub
	case areaHost:
		return m.hst.sub
	}
	return 0
}

// activeTab is the ID of the page of the current area shown, empty in an area without pages.
func (m *Model) activeTab() kittui.TabID {
	tabs := frameAreas()[m.area].Tabs
	if sub := m.sub(); sub < len(tabs) {
		return tabs[sub].ID
	}
	return ""
}

// tabIndex is the page of the current area with the ID id.
func (m *Model) tabIndex(id kittui.TabID) (int, bool) {
	for i, t := range frameAreas()[m.area].Tabs {
		if t.ID == id {
			return i, true
		}
	}
	return 0, false
}

// zones are the zones the area screen has now, for tab and shift+tab.
func (m *Model) zones() []kittui.FocusZone {
	zones := []kittui.FocusZone{zoneTabs}
	if m.hasSubs() {
		zones = append(zones, zoneSubs)
	}
	zones = append(zones, zoneContent)
	if m.detailZoneAvailable() {
		zones = append(zones, zoneDetail)
	}
	return zones
}

// cycleZone moves the focus step zones on, with wrap-around.
func (m *Model) cycleZone(step int) {
	m.zone = kittui.CycleZone(m.zones(), m.zoneNow(), step)
	if m.zone == zoneTabs || m.zone == zoneSubs {
		m.hst.fold.editing = false
	}
}

// enableAreas makes the area screen the base of the interface, served by backend.
func (m *Model) enableAreas(backend Backend) {
	m.backend = backend
	m.home, m.screen, m.area, m.zone = screenArea, screenArea, areaOverview, zoneTabs
}

// setArea shows an area, on its first page, with the focus in its content. Unsaved input of Settings is asked
// about first.
func (m *Model) setArea(a area) tea.Cmd { return m.show(a, 0) }

// show makes page sub of area a the screen, with the focus in its content; only Settings, Activity
// and Host have pages.
func (m *Model) show(a area, sub int) tea.Cmd { return m.showIn(a, sub, zoneContent) }

// showIn is show with the focus on zone z.
func (m *Model) showIn(a area, sub int, z kittui.FocusZone) tea.Cmd {
	return m.guarded(func() tea.Cmd {
		m.area, m.zone = a, z
		m.resetForms()
		m.clearMessages()
		m.ovr.reset()
		if a == areaSettings {
			m.set.sub = sub
		}
		if a == areaActivity {
			m.act.sub = sub
		}
		if a == areaHost {
			m.hst.sub = sub
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
	case m.area == areaActivity:
		return m.activityCmd()
	case m.area == areaHost:
		return m.hostCmd()
	case m.area == areaSettings:
		m.loadForms()
		if m.set.sub == subUpdate {
			return m.checkUpdate(false)
		}
	}
	return nil
}

// updateArea handles one key of the area screen.
func (m *Model) updateArea(key tea.KeyPressMsg) tea.Cmd {
	if m.typing() {
		if p := m.currentPage(); p.filtering() {
			return m.filterKey(p, key)
		}
		if m.hostTyping() {
			return m.foldersKey(key)
		}
		return m.editKey(key)
	}
	switch {
	case m.keyIs(key, actHelp):
		m.openPopup(popHelp)
		return nil
	case m.keyIs(key, actCommand):
		m.openPopup(popCommand)
		return nil
	case m.infoKey(key):
		m.openPopup(popInfo)
		return nil
	case m.keyIs(key, actQuit):
		return m.guarded(m.quit)
	case m.keyIs(key, actSelfUpdate):
		return m.selfUpdate()
	case m.keyIs(key, actCopy) && m.copySource() != nil:
		return m.copyCurrent()
	}
	if z := m.zoneNow(); z != zoneContent && z != zoneDetail {
		return m.barKey(key)
	}
	if m.zoneNow() == zoneDetail {
		switch {
		case m.keyIs(key, actNext):
			m.cycleZone(1)
			return nil
		case m.keyIs(key, actPrev):
			m.cycleZone(-1)
			return nil
		case m.keyIs(key, actBack):
			m.zone = zoneContent
			return nil
		}
	}
	if m.area == areaSettings {
		if cmd, ok := m.settingsKey(key); ok {
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
	case m.keyIs(key, actNext) && !inner:
		m.cycleZone(1)
	case m.keyIs(key, actPrev) && !inner:
		m.cycleZone(-1)
	case m.keyIs(key, actBack) && !inner:
		m.toBar()
	case m.keyIs(key, actReload):
		return tea.Batch(m.statusCmd(), m.overviewCmd())
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
func (m *Model) hasSubs() bool { return len(frameAreas()[m.area].Tabs) > 1 }

// barZone is the bar one level above the content: the page strip where the area has one, else the tab line.
func (m *Model) barZone() kittui.FocusZone {
	if m.hasSubs() {
		return zoneSubs
	}
	return zoneTabs
}

// toBar hands the focus from the content to the bar above it.
func (m *Model) toBar() {
	m.zone = m.barZone()
	m.hst.fold.editing = false
}

// detailZoneAvailable reports whether the detail of the marked row stands beside the rows, which makes it a
// zone of its own. Where it has the screen to itself it is an inner level of the content instead.
func (m *Model) detailZoneAvailable() bool {
	p := m.currentPage()
	return p != nil && m.screen == screenArea && !p.open && m.usable(0) >= wideDetailMin && p.entries() > 0
}

// zoneNow is the zone that takes the keys. An inner level of the content holds the focus whatever the zone
// was, a page strip falls back to the tab line in an area without one, and a detail that is not beside the
// rows falls back to them.
func (m *Model) zoneNow() kittui.FocusZone {
	switch {
	case m.innerLevel():
		return zoneContent
	case m.zone == zoneDetail && !m.detailZoneAvailable():
		return zoneContent
	case m.zone == zoneSubs && !m.hasSubs():
		return zoneTabs
	}
	return m.zone
}

// innerLevel reports whether the content shows a level that esc closes before it leaves the content: a
// filter being typed, a field being edited or the detail of a row.
func (m *Model) innerLevel() bool {
	if m.typing() {
		return true
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
	case m.keyIs(key, actLeft):
		step = -1
	case m.keyIs(key, actRight):
		step = 1
	case m.keyIs(key, actHome) && !subs:
		return m.showIn(0, 0, zoneTabs)
	case m.keyIs(key, actEnd) && !subs:
		return m.showIn(areaCount-1, 0, zoneTabs)
	case m.keyIs(key, actUp), m.keyIs(key, actBack):
		m.zone = zoneTabs
	case m.keyIs(key, actPrev):
		m.cycleZone(-1)
	case m.keyIs(key, actDown), m.keyIs(key, actEnter), m.keyIs(key, actNext):
		m.cycleZone(1)
	case m.keyIs(key, actReload):
		return tea.Batch(m.statusCmd(), m.overviewCmd())
	default:
		return m.digitKey(key)
	}
	if step == 0 {
		return nil
	}
	areas := frameAreas()
	if !subs {
		next, _ := areaOf(kittui.StepArea(areas, m.area.areaID(), step))
		return m.showIn(next, 0, zoneTabs)
	}
	sub, _ := m.tabIndex(kittui.StepTab(areas[m.area], m.activeTab(), step))
	return m.setSubPage(sub)
}

// closeDetails closes the detail of every table page and drops the notes of a refused removal.
func (m *Model) closeDetails() {
	m.act.page.open = false
	m.hst.users.page.open, m.hst.groups.page.open = false, false
}

// dataKey handles a key of the pages with a table: the table and its detail first, then the page's own keys.
func (m *Model) dataKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.currentPage()
	if p == nil {
		// Folders has no table, only its path line.
		if m.area == areaHost {
			return m.hostKey(key)
		}
		return nil, false
	}
	if m.area == areaHost && m.hst.sub == hostChanges && m.zoneNow() != zoneDetail && !p.open {
		// Enter asks to undo here instead of opening the detail.
		if cmd, ok := m.changesKey(key); ok {
			return cmd, true
		}
	}
	if m.area == areaActivity && !p.open && m.keyIs(key, actFilter) {
		// f is the filter of the log here; the table is searched with /.
		m.openLogFilter()
		return nil, true
	}
	if cmd, ok := m.pageKey(p, key); ok {
		return cmd, true
	}
	switch m.area {
	case areaActivity:
		return m.activityKey(key)
	case areaHost:
		return m.hostKey(key)
	}
	return nil, false
}

// infoKey reports whether key asks for the info: the letter, or the extra key of the preferences, which also
// works where text is typed.
func (m *Model) infoKey(key tea.KeyPressMsg) bool { return m.keyIs(key, actInfo) }

// areaView is the workspace of the area screen: the area under the bars of the frame.
func (m *Model) areaView() string {
	m.keyHint(m.areaKeys())
	return m.areaBody()
}

// setSubPage shows page i of the current area.
func (m *Model) setSubPage(i int) tea.Cmd {
	switch m.area {
	case areaSettings:
		return m.setSub(i)
	case areaActivity:
		return m.setActSub(i)
	case areaHost:
		return m.setHostSub(i)
	}
	return nil
}

// areaKeys is the key line of the area screen.
func (m *Model) areaKeys() []kittui.Hint {
	if m.typing() {
		if m.currentPage().filtering() {
			return keyLine(hint(tHintTypeToFilter), navHint(tHintMove, actUp, actDown), hint(tHintKeepFilter, actEnter),
				hint(tHintClearFilter, actBack))
		}
		if m.hostTyping() {
			return m.hostFoldersKeys()
		}
		return keyLine(hint(tHintNext, actEnter), hint(tHintStop, actBack), hint(tHintSave, actSave))
	}
	var keys []kittui.Hint
	switch zone := m.zoneNow(); {
	case zone == zoneTabs:
		keys = keyLine(barHint(tHintArea, actLeft, actRight), hint(tHintOpen, actDown))
	case zone == zoneSubs:
		keys = keyLine(barHint(tHintPage, actLeft, actRight), hint(tHintOpen, actDown), hint(tHintBack, actBack))
	case m.area == areaOverview:
		keys = keyLine(navHint(tHintMove, actUp, actDown), navHint(tHintNext, actNext))
		if item := m.overviewCurrent(); item.key != "" {
			keys = append(keys, hint(item.key, actEnter)...)
		}
	case m.area == areaSettings:
		keys = m.settingsKeys()
	case m.area == areaActivity:
		keys = m.activityKeys()
	case m.area == areaHost:
		keys = m.hostKeys()
	}
	if m.zoneNow() == zoneContent && !m.innerLevel() {
		keys = append(keys, hint(tHintBack, actBack)...)
	}
	if !m.serviceUp() {
		keys = append(keys, hint(tHintRetry, actReload)...)
	}
	return keyLine(keys, hint(tHintInfo, actInfo), hint(tHintHelp, actHelp), hint(tHintCmds, actCommand),
		hint(tHintQuit, actQuit))
}

// areaBody is the area under the tab line. Areas without content show one line.
func (m *Model) areaBody() string {
	switch m.area {
	case areaOverview:
		return m.overviewBody()
	case areaSettings:
		return m.settingsBody()
	case areaActivity:
		return m.activityBody()
	case areaHost:
		return m.hostBody()
	}
	return m.wrapped(m.ui.st(kittui.StyleHint), m.tr(tListEmpty))
}
