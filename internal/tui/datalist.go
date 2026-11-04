package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A data page is a table of an area with the detail of the marked row: beside the table from wideDetailMin
// columns on, and as a screen of its own after enter below that. The detail has three tabs. The pages of
// Activity and Host share this block; a page brings its columns, its rows and the lines of each tab. The
// table itself is the kit's list, which holds the selection, the filter, the groups and the scroll position.

// wideDetailMin is the width from which the detail stands beside the table.
const wideDetailMin = 120

// The tabs of the detail.
const (
	tabOverview = iota
	tabDetails
	tabRaw
	tabCount
)

// dataPage is the state of one table page. The rows are keyed by a name that is unique in the page.
type dataPage struct {
	list *kittui.List
	// lang, empty and cols are what the list was built with; the column titles and the empty sentence follow
	// them.
	lang  i18n.Lang
	empty textID
	cols  int
	// ids finds the name of a row by its item ID, groups its group; rows holds the cells of each row by name.
	ids    map[kittui.ItemID]string
	groups map[kittui.ItemID]string
	rows   map[string][]string
	// folded are the groups that were given their first fold state.
	folded map[string]bool
	// tab is the detail tab; open says the detail has the screen; scroll is its first line.
	tab    int
	open   bool
	scroll int
}

// dataRow is one row a page hands its table: the name it is known by, its cells and its group.
type dataRow struct {
	name  string
	cells []string
	group string
}

// listConfig is what the table of a page is built from. A grouped table titles the group without key with
// noGroup; the groups in folded start folded the first time they show.
type listConfig struct {
	cols    []kittui.Column
	empty   textID
	grouped bool
	noGroup textID
	folded  []string
}

// dataSpec is what a page brings to the drawing: its title, and whether a gap stands under it where the
// height allows, as under the titles of the other framed screens; a note that stands in place of the table
// while it has no rows, such as an error or a load in progress; and the lines of a tab for a row, wrapped into
// width cells.
type dataSpec struct {
	title string
	gap   bool
	note  string
	lines func(name string, tab, width int) []string
}

// listKeys are the actions the tables read.
func listKeys() kittui.ListKeys {
	return kittui.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actHome,
		Last: actEnd, Left: actLeft, Right: actRight, Enter: actEnter, Search: actSearch, Back: actBack}
}

// rowID is the item ID of a row name. Quoting keeps every name, a control character included, apart from
// every other, and the ID itself free of control characters.
func rowID(name string) kittui.ItemID { return kittui.ItemID(strconv.Quote(name)) }

// stale reports whether the table was built in another language than the one in force, or not at all.
func (p *dataPage) stale(ui *uiContext) bool { return p.list == nil || p.lang != ui.lang }

// fill replaces the rows of the page and keeps the filter and, where it still shows, the marked row. A name
// listed twice keeps its first row. The columns are fixed in the code, so a table the kit refuses is a defect
// of this package.
func (p *dataPage) fill(ui *uiContext, cfg listConfig, rows []dataRow) {
	old := p.list
	if p.stale(ui) || p.empty != cfg.empty || p.cols != len(cfg.cols) {
		l, err := kittui.NewList(kittui.ListSpec{Columns: cfg.cols, Keys: listKeys(), Empty: cfg.empty, NoGroup: cfg.noGroup},
			ui.keys, ui.text)
		if err != nil {
			panic(fmt.Sprintf("the table is invalid: %v", err))
		}
		p.list, p.lang, p.empty, p.cols, p.folded = l, ui.lang, cfg.empty, len(cfg.cols), map[string]bool{}
	}
	p.list.SetGrouped(cfg.grouped)
	p.ids, p.groups, p.rows = map[kittui.ItemID]string{}, map[kittui.ItemID]string{}, map[string][]string{}
	items := make([]kittui.ListItem, 0, len(rows))
	firstOf := map[string]kittui.ItemID{}
	for _, r := range rows {
		id := rowID(r.name)
		if _, dup := p.ids[id]; dup {
			continue
		}
		p.ids[id], p.groups[id], p.rows[r.name] = r.name, r.group, r.cells
		items = append(items, kittui.ListItem{ID: id, Cells: r.cells, Group: r.group})
		if _, ok := firstOf[r.group]; !ok {
			firstOf[r.group] = id
		}
	}
	if err := p.list.SetItems(items); err != nil {
		panic(fmt.Sprintf("the rows do not fit the table: %v", err))
	}
	for _, g := range cfg.folded {
		if first, ok := firstOf[g]; ok && !p.folded[g] {
			p.folded[g] = true
			p.foldGroupOf(first)
		}
	}
	if old != nil && old != p.list {
		p.list.SetFilter(old.Query())
		if old.Searching() {
			p.list.Act(actSearch)
		}
		if id, ok := old.Selected(); ok {
			p.list.Select(id)
		}
	}
}

// foldGroupOf folds the group of the entry first, as left on it does, and keeps the mark where it was unless
// it stood in that group.
func (p *dataPage) foldGroupOf(first kittui.ItemID) {
	marked, had := p.list.Selected()
	p.list.Select(first)
	p.list.Act(actLeft)
	if had && p.groups[marked] != p.groups[first] {
		p.list.Select(marked)
	}
}

// toFirst marks the first row shown and scrolls to it; the filter stays.
func (p *dataPage) toFirst() {
	query, searching := p.list.Query(), p.list.Searching()
	p.list.Reset()
	p.list.SetFilter(query)
	if searching {
		p.list.Act(actSearch)
	}
}

// entries is the number of rows of the page.
func (p *dataPage) entries() int { return len(p.ids) }

// wheelDetail scrolls the detail by the notches of the wheel.
func (p *dataPage) wheelDetail(up bool) {
	if up {
		p.scroll = max(p.scroll-kittui.WheelTextLines, 0)
	} else {
		p.scroll += kittui.WheelTextLines
	}
}

// wheelRows is a notch of the wheel over the table: the mark takes a step, with the consequences the key has.
// It rests while a filter is typed.
func (m *Model) wheelRows(p *dataPage, up bool) tea.Cmd {
	if p.list == nil || p.list.Searching() {
		return nil
	}
	steps := 1
	if up {
		steps = -1
	}
	return m.movedBy(p, func() { p.list.Wheel(steps) })
}

// movedBy runs move on the table of p; where the mark moved, the detail starts at its top and the page asks
// for what the new row needs.
func (m *Model) movedBy(p *dataPage, move func()) tea.Cmd {
	before, _ := p.list.Selected()
	move()
	if after, _ := p.list.Selected(); after == before {
		return nil
	}
	p.scroll = 0
	return m.dataWant()
}

// current is the name of the marked row; a group row is no row and has none.
func (p *dataPage) current() (string, bool) {
	if p.list == nil {
		return "", false
	}
	id, ok := p.list.Selected()
	if !ok {
		return "", false
	}
	name, ok := p.ids[id]
	return name, ok
}

// onGroup reports whether the mark stands on a group row: the table has rows, none is marked and no filter
// could have hidden them all.
func (p *dataPage) onGroup() bool {
	_, marked := p.current()
	return p.list != nil && !marked && p.entries() > 0 && p.list.Query() == "" && !p.list.Searching()
}

// filtering reports whether the filter takes the typed keys.
func (p *dataPage) filtering() bool { return p != nil && p.list != nil && p.list.Searching() }

// atLastRow reports whether the mark stands on the last row shown. It steps down and back, which moves
// nothing that is drawn.
func (p *dataPage) atLastRow() bool {
	before, ok := p.list.Selected()
	if !ok {
		return false
	}
	p.list.Act(actDown)
	after, _ := p.list.Selected()
	if after == before {
		return true
	}
	p.list.Act(actUp)
	return false
}

func tabTitle(ui *uiContext, tab int) string {
	return ui.tr([...]textID{tTabOverview, tTabDetails, tTabRaw}[tab])
}

// tabStripLine is the strip of the detail tabs and the cells each takes.
func tabStripLine(ui *uiContext, p *dataPage, focused bool) (string, []kittui.Span) {
	words := make([]string, tabCount)
	for i := range words {
		words[i] = tabTitle(ui, i)
	}
	return ui.styles.TabStrip(words, p.tab, focused)
}

// dataBody draws the page from screen row top on.
func (m *Model) dataBody(p *dataPage, v dataSpec, top int) string {
	width := m.usable(0)
	avail := max(m.height-(top-m.workspaceTop()), 3)
	name, have := p.current()
	if !have {
		p.open = false
	}
	position := ""
	if p.list != nil {
		position = p.list.Position()
	}
	if !m.backdrop {
		m.screenPos = position
	}
	if p.open {
		return m.detailScreen(p, v, name, avail, width, top)
	}
	// A group row has no detail, but the panel stays so the table does not jump while the mark moves over it.
	wide := width >= wideDetailMin && p.entries() > 0
	tableWidth := width
	detailWidth := 0
	if wide {
		detailWidth = width * 5 / 12
		tableWidth = width - detailWidth - 1
	}
	detailFocus := m.zoneNow() == zoneDetail
	left := m.tableBlock(p, v, tableWidth, avail, top, position, !detailFocus)
	rowsWheel := func(up bool) tea.Cmd { return m.wheelRows(p, up) }
	if !wide {
		m.addWheel(0, top, width, top+len(left), zoneContent, rowsWheel)
		return strings.Join(left, "\n")
	}
	inner := detailWidth - 4
	strip, spans := tabStripLine(m.ui, p, detailFocus)
	title := m.diffText(name)
	var content []string
	switch {
	case have:
		content = v.lines(name, p.tab, inner)
	case p.onGroup():
		content = wrapIndented(m.ui.st(kittui.StyleHint), "", m.tr(tGroupRowHint), inner)
	}
	height := min(max(len(content)+2, len(left)-2), avail-2)
	room := max(height-2, 1)
	p.scroll = min(max(p.scroll, 0), max(len(content)-room, 0))
	body := append([]string{strip, ""}, content[p.scroll:min(p.scroll+room, len(content))]...)
	if !have {
		body, spans = content[:min(room, len(content))], nil
	}
	right := m.ui.styles.Panel(title, "", body, detailWidth, height+2, detailFocus)
	left0 := tableWidth + 1
	m.addWheel(0, top, tableWidth, top+len(left), zoneContent, rowsWheel)
	m.addWheel(left0, top, width, top+len(right), zoneDetail, func(up bool) tea.Cmd { p.wheelDetail(up); return nil })
	for i, span := range spans {
		m.addHit(left0+2+span.Start, top+1, left0+2+span.End, top+2, func() tea.Cmd {
			p.tab, p.scroll = i, 0
			return m.focusZone(zoneDetail)
		})
	}
	var out []string
	for i := range max(len(left), len(right)) {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, padLine(l, tableWidth)+" "+r)
	}
	return strings.Join(out, "\n")
}

// tableBlock is the table of the page in width cells, at most avail lines: the title with the position, and
// in a panel where there is room the list of the kit, or the note of the page while it has no rows.
func (m *Model) tableBlock(p *dataPage, v dataSpec, width, avail, top int, position string, focused bool) []string {
	framed := width >= tableFrameMinWidth && m.height >= tableFrameMinHeight
	room, x0 := width, 0
	if framed {
		room, x0 = max(width-4, 1), 2
	}
	lines := []string{m.ui.styles.TitleLine(fmt.Sprintf("%s (%d)", v.title, p.entries()), position, width)}
	if v.gap && m.titleBlank() {
		lines = append(lines, "")
	}
	foot := 0
	if framed {
		foot = 1
	}
	var body []string
	if p.list == nil || (p.entries() == 0 && v.note != "") {
		body = []string{m.ui.st(kittui.StyleHint).Render(truncateCells("  "+v.note, room))}
	} else {
		view, targets := p.list.View(m.ui.styles, room, max(avail-len(lines)-foot, 1))
		body = strings.Split(view, "\n")
		for len(body) > 1 && strings.TrimSpace(ansi.Strip(body[len(body)-1])) == "" {
			body = body[:len(body)-1]
		}
		m.addRowTargets(p, targets, x0, top+len(lines)+foot, len(body))
	}
	if !framed {
		return append(lines, body...)
	}
	return append(lines, m.ui.styles.Panel("", "", body, width, len(body)+2, focused)...)
}

// addRowTargets makes the rows and the search line the list drew clickable, from screen cell (x0, y0) on;
// rows is how many of its lines were kept.
func (m *Model) addRowTargets(p *dataPage, targets []kittui.Target, x0, y0, rows int) {
	for _, t := range targets {
		if (t.Item == "" && t.Action == "") || t.Rect.Y >= rows {
			continue
		}
		r := t.Rect
		m.addHit(x0+r.X, y0+r.Y, x0+r.X+r.Width, y0+r.Y+r.Height, func() tea.Cmd { return m.clickRow(p, t) })
	}
}

// clickRow is a click on a target of a table. The kit marks the row, folds a group or starts the search; only
// a click on the marked row while the rows had the focus opens it, which is the press of enter there, so a
// click that meant to take the focus from the detail opens nothing.
func (m *Model) clickRow(p *dataPage, t kittui.Target) tea.Cmd {
	p.list.SetFocused(m.screen != screenArea || m.zoneNow() == zoneContent)
	m.focusRows()
	var open kittui.ItemID
	cmd := m.movedBy(p, func() {
		open, _ = p.list.Click(kittui.MouseResult{Handled: true, Zone: t.Zone, Action: t.Action, Item: t.Item})
	})
	if open != "" {
		return m.contentPress(actEnter)
	}
	return cmd
}

// detailScreen is the detail of the marked row as a screen of its own, scrolled by the person.
func (m *Model) detailScreen(p *dataPage, v dataSpec, name string, avail, width, top int) string {
	inner := max(width-4, 1)
	content := v.lines(name, p.tab, inner)
	room := max(avail-1-2-2, 1)
	p.scroll = min(max(p.scroll, 0), max(len(content)-room, 0))
	note := ""
	if len(content) > room {
		note = m.tr(tScrollPosition, p.scroll+1, min(p.scroll+room, len(content)), len(content))
	}
	strip, spans := tabStripLine(m.ui, p, true)
	body := append([]string{strip, ""}, content[p.scroll:min(p.scroll+room, len(content))]...)
	head := m.ui.styles.TitleLine(v.title+" › "+truncateCells(m.diffText(name), max(width-lipgloss.Width(v.title)-4, 4)), note, width)
	for i, span := range spans {
		m.addHit(2+span.Start, top+2, 2+span.End, top+3, func() tea.Cmd { p.tab, p.scroll = i, 0; return nil })
	}
	m.addWheel(0, top, width, top+1+room+2, zoneDetail, func(up bool) tea.Cmd { p.wheelDetail(up); return nil })
	return head + "\n" + strings.Join(m.ui.styles.Panel("", "", body, width, room+4, true), "\n")
}

// pageKey handles the keys of the table, and the keys of the open detail; up on the first row goes to the bar
// above. It reports false for every other key.
func (m *Model) pageKey(p *dataPage, key tea.KeyPressMsg) (tea.Cmd, bool) {
	if p.open || m.zoneNow() == zoneDetail {
		return nil, m.detailKey(p, key)
	}
	if p.list == nil {
		return nil, false
	}
	if m.keyIs(key, actFilter) {
		p.list.Act(actSearch)
		return nil, true
	}
	var open kittui.ItemID
	var handled bool
	cmd := m.movedBy(p, func() { open, handled = p.list.Update(key) })
	switch {
	case open != "" && m.screen != screenArea:
		// The bare list has no detail to open.
	case open != "":
		p.scroll = 0
		if m.detailZoneAvailable() {
			m.zone = zoneDetail
		} else {
			p.open = true
		}
	case handled:
	case m.keyIs(key, actUp) && m.screen == screenArea:
		m.toBar()
	default:
		return nil, false
	}
	return cmd, true
}

// detailKey handles a key of the detail of p: the tabs and the scroll position, and esc where it has the
// screen.
func (m *Model) detailKey(p *dataPage, key tea.KeyPressMsg) bool {
	switch {
	case p.open && m.keyIs(key, actBack):
		p.open = false
	case m.keyIs(key, actLeft), p.open && m.keyIs(key, actPrev):
		p.tab, p.scroll = (p.tab+tabCount-1)%tabCount, 0
	case m.keyIs(key, actRight), p.open && (m.keyIs(key, actNext) || m.keyIs(key, actEnter)):
		p.tab, p.scroll = (p.tab+1)%tabCount, 0
	case m.keyIs(key, actUp):
		p.scroll = max(p.scroll-1, 0)
	case m.keyIs(key, actDown):
		p.scroll++
	case m.keyIs(key, actPageUp):
		p.scroll = max(p.scroll-5, 0)
	case m.keyIs(key, actPageDown):
		p.scroll += 5
	case m.keyIs(key, actHome):
		p.scroll = 0
	case m.keyIs(key, actEnd):
		p.scroll = 1 << 30
	default:
		return false
	}
	return true
}

// filterKey handles a key while the filter is typed. It reaches the filter as it reaches every text field: a
// paste as its text, and alt with a character as that character.
func (m *Model) filterKey(p *dataPage, key tea.KeyPressMsg) tea.Cmd {
	key = typedKey(key)
	return m.movedBy(p, func() { p.list.Update(key) })
}

// currentPage is the data page the screen shows, nil where there is none.
func (m *Model) currentPage() *dataPage {
	switch {
	case m.screen == screenList:
		return &m.base
	case m.area == areaActivity:
		return &m.act.page
	case m.area == areaHost && m.hst.sub == hostUsers:
		return &m.hst.users.page
	case m.area == areaHost && m.hst.sub == hostGroups:
		return &m.hst.groups.page
	case m.area == areaHost && m.hst.sub == hostChanges:
		return &m.hst.chg.page
	}
	return nil
}

// detailSeen reports whether the detail of the marked row is on the screen now.
func (m *Model) detailSeen() bool {
	p := m.currentPage()
	return p != nil && (p.open || (m.usable(0) >= wideDetailMin && p.entries() > 0))
}

// dataWant asks for what the marked row's detail needs and has not yet.
func (m *Model) dataWant() tea.Cmd {
	switch {
	case m.area == areaActivity:
		return m.activityWant()
	}
	return nil
}

// cellText makes a value of the service fit for a cell of a table: invalid UTF-8 replaced and every
// registered secret hidden. The table escapes control characters itself, so they show instead of acting.
func (m *Model) cellText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if m.redactor != nil {
		s = m.redactor.Apply(s)
	}
	return s
}

// diffText makes a value of the service safe to draw: no escape sequence, no registered secret, no control
// character; a line break is shown as a mark.
func (m *Model) diffText(s string) string {
	s = strings.ToValidUTF8(ansi.Strip(strings.ReplaceAll(s, "\r\n", "\n")), "�")
	if m.redactor != nil {
		s = m.redactor.Apply(s)
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return '⏎'
		case r == '\t':
			return ' '
		case r < ' ' || r == 0x7f:
			return '�'
		}
		return r
	}, s)
}

// wrapIndented wraps text into width cells behind the lead of its first line and an equal blank on the others.
func wrapIndented(style lipgloss.Style, lead, text string, width int) []string {
	lines := strings.Split(render(style, max(width-lipgloss.Width(lead), 1), text), "\n")
	for i, l := range lines {
		if i == 0 {
			lines[i] = lead + l
		} else {
			lines[i] = strings.Repeat(" ", lipgloss.Width(lead)) + l
		}
	}
	return lines
}

// factLines draws label/value pairs with aligned values.
func factLines(ui *uiContext, pairs [][2]string, width int) []string {
	lw := 0
	for _, p := range pairs {
		lw = max(lw, lipgloss.Width(p[0]))
	}
	var out []string
	for _, p := range pairs {
		out = append(out, wrapIndented(lipgloss.NewStyle(), ui.st(kittui.StyleHint).Render(padLine(p[0], lw))+"  ", p[1], width)...)
	}
	return out
}

// detailFocus reports whether the keys of the detail apply to page p: it has the screen, or it is the focused
// zone beside the rows.
func (m *Model) detailFocus(p *dataPage) bool { return p.open || m.zoneNow() == zoneDetail }
