package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A data page is a table of an area with the detail of the marked row: beside the table from wideDetailMin
// columns on, and as a screen of its own after enter below that. The detail has three tabs. The pages of
// Activity and Host share this block; a page brings its columns, its rows and the lines of each tab.

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
	list filterList
	cols []column
	rows map[string][]string
	// tab is the detail tab; open says the detail has the screen; scroll is its first line.
	tab    int
	open   bool
	scroll int
	// sec is the fold state of a page with sections; see datasections.go.
	sec sectionState
}

// dataSpec is what a page brings to the drawing: its title, its empty text, and the lines of a tab for a
// row, wrapped into width cells.
type dataSpec struct {
	title string
	empty string
	lines func(name string, tab, width int) []string
}

// setRows replaces the rows of the page and keeps the filter and, where it can, the marked row.
func (p *dataPage) setRows(names []string, rows map[string][]string) {
	if p.list.text == nil {
		p.list = newFilterList(func(name string) string { return strings.Join(p.rows[name], " ") })
	}
	p.rows = rows
	p.list.setItems(names)
}

// wheelDetail scrolls the detail by the notches of the wheel.
func (p *dataPage) wheelDetail(up bool) {
	if up {
		p.scroll = max(p.scroll-kittui.WheelTextLines, 0)
	} else {
		p.scroll += kittui.WheelTextLines
	}
}

// wheelRows is a notch of the wheel over the table: the mark takes a step, with the consequences the key has.
func (m *Model) wheelRows(p *dataPage, up bool) tea.Cmd {
	return wheelList(&p.list, up, func() tea.Cmd {
		p.scroll = 0
		return m.dataWant()
	})
}

// current is the name of the marked row; a heading of a section is no row and has none.
func (p *dataPage) current() (string, bool) {
	name, ok := p.list.selected()
	return name, ok && !isSectionRow(name)
}

// filtering reports whether the filter takes the typed keys.
func (p *dataPage) filtering() bool { return p != nil && p.list.editing }

func tabTitle(ui *uiContext, tab int) string {
	return ui.tr([...]textID{tTabOverview, tTabDetails, tTabRaw}[tab])
}

// tabStripLine is the strip of the detail tabs and the cells each takes.
func tabStripLine(ui *uiContext, p *dataPage, width int, focused bool) (string, [][2]int) {
	words := make([]string, tabCount)
	for i := range words {
		words[i] = tabTitle(ui, i)
	}
	strip, spans := tabStrip(ui, words, p.tab, focused)
	return clipCells(strip, width), spans
}

// dataBody draws the page from screen row top on.
func (m *Model) dataBody(p *dataPage, v dataSpec, top int) string {
	width := m.usable(0)
	avail := max(m.height-(top-m.workspaceTop()), 3)
	name, have := p.current()
	if !have {
		p.open = false
	}
	total := p.entries()
	at, shown := p.list.cursor+1, len(p.list.matches)
	if p.sectioned() {
		at, shown = p.sectionPosition()
	}
	position := ""
	if total > 0 {
		position = fmt.Sprintf("%d/%d", at, shown)
		if shown != total && (!p.sectioned() || p.list.query() != "") {
			position = fmt.Sprintf("%d/%d (%d total)", at, shown, total)
		}
	}
	if !m.backdrop {
		m.screenPos = position
	}
	if p.open {
		return m.detailScreen(p, v, name, avail, width, top)
	}
	// A heading has no detail, but the panel stays so the table does not jump while the mark moves over it.
	marked, hasMark := p.list.selected()
	wide := width >= wideDetailMin && hasMark && (have || isSectionRow(marked))
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
	strip, spans := tabStripLine(m.ui, p, inner, detailFocus)
	title := name
	var content []string
	if have {
		content = v.lines(name, p.tab, inner)
	} else {
		sec, _ := p.section(marked)
		title = m.tr(sec.title)
		content = wrapIndented(m.ui.st(kittui.StyleHint), "", m.tr(tSectionHint), inner)
		strip, spans = "", nil
	}
	height := min(max(len(content)+2, len(left)-2), avail-2)
	room := max(height-2, 1)
	p.scroll = min(max(p.scroll, 0), max(len(content)-room, 0))
	body := append([]string{strip, ""}, content[p.scroll:min(p.scroll+room, len(content))]...)
	if !have {
		body = content[:min(room, len(content))]
	}
	right := panel(m.ui, truncateCells(title, inner-2), body, inner, height, panelOpts{focused: detailFocus})
	left0 := tableWidth + 1
	m.addWheel(0, top, tableWidth, top+len(left), zoneContent, rowsWheel)
	m.addWheel(left0, top, width, top+len(right), zoneDetail, func(up bool) tea.Cmd { p.wheelDetail(up); return nil })
	for i, span := range spans {
		m.addHit(left0+2+span[0], top+1, left0+2+span[1], top+2, func() tea.Cmd {
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

// tableBlock is the table of the page in width cells, at most avail lines.
func (m *Model) tableBlock(p *dataPage, v dataSpec, width, avail, top int, position string, focused bool) []string {
	framed := width >= tableFrameMinWidth && m.height >= tableFrameMinHeight
	room := width
	if framed {
		room = max(width-4, 1)
	}
	inside := func(text string) string {
		if !framed {
			return text
		}
		return panelSide(m.ui, text, room, focused)
	}
	total, shown := p.entries(), len(p.list.matches)
	lines := []string{m.ui.styles.TitleLine(fmt.Sprintf("%s (%d)", v.title, total), position, width)}
	if framed {
		lines = append(lines, panelTop(m.ui, "", "", room, focused))
	}
	if p.list.editing || p.list.query() != "" {
		lines = append(lines, inside(m.searchLineIn(room, &p.list, m.tr(tFormFilter))))
	}
	t := table{columns: p.cols, rows: p.rows}
	_, cells := m.fitIn(room, entryIndent)
	layout := t.layout(cells)
	if total > 0 {
		lines = append(lines, inside(m.ui.st(kittui.StyleHint).Render(clipCells(entryIndent+t.header(layout), room))))
	}
	switch {
	case total == 0:
		lines = append(lines, inside(m.ui.st(kittui.StyleHint).Render(truncateCells(v.empty, room))))
	case shown == 0:
		lines = append(lines, inside(m.ui.st(kittui.StyleHint).Render(truncateCells(m.tr(tFormNoEntryMatchesPressEsc, p.list.query()), room))))
	}
	foot := 0
	if framed {
		foot = 1
	}
	start, end := p.list.window(max(avail-len(lines)-foot, 1), func(int) int { return 1 })
	for i := start; i < end; i++ {
		i := i
		name := p.list.matches[i]
		heading := isSectionRow(name)
		text := ""
		if heading {
			text = p.headingLine(m.ui, name)
		} else {
			text = t.line(p.rows[name], layout)
		}
		row := m.listRowIn(room, i == p.list.cursor, text)
		m.addHit(0, top+len(lines), width, top+len(lines)+1, func() tea.Cmd {
			if heading {
				// A click on a heading folds it at once, which is harmless and undone by the next click.
				m.focusRows()
				p.list.cursor = i
				p.toggleHeading()
				return nil
			}
			fromDetail := m.focusRows()
			if p.list.cursor != i {
				p.list.cursor, p.scroll = i, 0
				return m.dataWant()
			}
			if fromDetail {
				return nil
			}
			return m.contentPress(actEnter)
		})
		lines = append(lines, inside(row))
	}
	if framed {
		lines = append(lines, panelBottom(m.ui, room, focused))
	}
	return lines
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
	strip, spans := tabStripLine(m.ui, p, inner, true)
	body := append([]string{strip, ""}, content[p.scroll:min(p.scroll+room, len(content))]...)
	head := m.ui.styles.TitleLine(v.title+" › "+truncateCells(name, max(width-lipgloss.Width(v.title)-4, 4)), note, width)
	for i, span := range spans {
		m.addHit(2+span[0], top+2, 2+span[1], top+3, func() tea.Cmd { p.tab, p.scroll = i, 0; return nil })
	}
	m.addWheel(0, top, width, top+1+room+2, zoneDetail, func(up bool) tea.Cmd { p.wheelDetail(up); return nil })
	return head + "\n" + strings.Join(panel(m.ui, "", body, inner, room+2, panelOpts{focused: true}), "\n")
}

// pageKey handles the keys that move through the table, and the keys of the open detail; up on the first row
// goes to the bar above. It reports false for every other key.
func (m *Model) pageKey(p *dataPage, key tea.KeyPressMsg) (tea.Cmd, bool) {
	if p.open {
		switch {
		case m.keyIs(key, actBack):
			p.open = false
		case m.keyIs(key, actLeft), m.keyIs(key, actPrev):
			p.tab, p.scroll = (p.tab+tabCount-1)%tabCount, 0
		case m.keyIs(key, actRight), m.keyIs(key, actNext), m.keyIs(key, actEnter):
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
			return nil, false
		}
		return nil, true
	}
	if m.zoneNow() == zoneDetail {
		switch {
		case m.keyIs(key, actLeft):
			p.tab, p.scroll = (p.tab+tabCount-1)%tabCount, 0
		case m.keyIs(key, actRight):
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
			return nil, false
		}
		return nil, true
	}
	cursor := p.list.cursor
	defer func() {
		if p.list.cursor != cursor {
			p.scroll = 0
		}
	}()
	switch {
	case m.keyIs(key, actUp) && p.list.cursor <= 0:
		m.toBar()
		return nil, true
	case m.keyIs(key, actUp):
		p.list.move(-1)
	case m.keyIs(key, actDown):
		p.list.move(1)
	case m.keyIs(key, actPageUp), m.keyIs(key, actPageDown), m.keyIs(key, actHome), m.keyIs(key, actEnd):
		p.list.page(m.ui.keys, key, 0, 10)
	case p.headingKey(m.ui.keys, key):
	case m.keyIs(key, actEnter):
		if _, ok := p.current(); ok {
			p.scroll = 0
			if m.detailZoneAvailable() {
				m.zone = zoneDetail
			} else {
				p.open = true
			}
		}
	case m.keyIs(key, actSearch), m.keyIs(key, actFilter):
		p.list.startFilter()
		return nil, true
	case m.keyIs(key, actBack):
		if p.list.query() == "" {
			return nil, false
		}
		p.list.clearFilter()
	default:
		return nil, false
	}
	return m.dataWant(), true
}

// filterKey handles a key while the filter is typed.
func (m *Model) filterKey(p *dataPage, key tea.KeyPressMsg) tea.Cmd {
	switch {
	case m.keyIs(key, actEnter):
		p.list.stopFilter()
	case m.keyIs(key, actBack):
		p.list.clearFilter()
	case m.keyIs(key, actUp):
		p.list.move(-1)
	case m.keyIs(key, actDown):
		p.list.move(1)
	default:
		return tea.Batch(p.list.updateFilter(key), m.dataWant())
	}
	return m.dataWant()
}

// currentPage is the data page the area shows, nil where there is none.
func (m *Model) currentPage() *dataPage {
	switch {
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
	return p != nil && (p.open || (m.usable(0) >= wideDetailMin && len(p.list.matches) > 0))
}

// dataWant asks for what the marked row's detail needs and has not yet.
func (m *Model) dataWant() tea.Cmd {
	switch {
	case m.area == areaActivity:
		return m.activityWant()
	}
	return nil
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
