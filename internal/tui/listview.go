package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// listView draws the list screen: its frame, and between them as many rows as fit.
func (m *Model) listView() string {
	header, footer := m.listFrame()
	row := m.listRows()
	var b strings.Builder
	b.WriteString(header)
	start, end := m.windowIn(&m.list, header, footer, row)
	for i := start; i < end; i++ {
		b.WriteString(row(i) + "\n")
	}
	m.hitList(header, start, end, row)
	b.WriteString(footer)
	return b.String()
}

// tableRoom is the width of the rows of the list screen: the workspace, less the frame.
func (m *Model) tableRoom() int {
	if m.tableFramed() {
		return max(m.usable(0)-4, 1)
	}
	return m.usable(0)
}

// tableFramed reports whether the list screen draws its table in a panel.
func (m *Model) tableFramed() bool {
	return m.usable(0) >= tableFrameMinWidth && m.height >= tableFrameMinHeight
}

// listFrame is everything of the list screen except its rows: above them the heading, the position of the
// selection, the filter and the column names; below them the keys. The rows get the lines that are left, so
// a list of any length fits the terminal and only the rows scroll.
func (m *Model) listFrame() (string, string) {
	var head strings.Builder
	framed, room := m.tableFramed(), m.tableRoom()
	// inside puts a line of the table into the frame, where there is one.
	inside := func(text string) string {
		if !framed {
			return text + "\n"
		}
		var b strings.Builder
		for _, line := range strings.Split(text, "\n") {
			b.WriteString(panelSide(line, room, true) + "\n")
		}
		return b.String()
	}
	note := func(style lipgloss.Style, text string) string {
		if !framed {
			return m.wrapped(style, text) + "\n"
		}
		return inside(render(style, room, text))
	}
	total := m.entryTotal()
	shown, pos := m.entryPosition()
	filtered := m.list.editing || m.list.query() != ""
	position := ""
	switch {
	case filtered:
		position = fmt.Sprintf("%d/%d (%d total)", pos, shown, total)
	case total > 0:
		position = fmt.Sprintf("%d/%d", pos, total)
	}
	m.screenPos = position
	if framed {
		head.WriteString(m.titled(fmt.Sprintf("%s (%d)", m.title, total), position, room+4))
		head.WriteString(panelTop("", "", room, true) + "\n")
	} else {
		head.WriteString(styles.title.Render(m.title) + "\n")
	}
	switch {
	case filtered:
		head.WriteString(inside(m.filterLine(&m.list)))
	case !framed:
		head.WriteString("\n")
	}
	if total > 0 {
		t := m.table
		_, width := m.fitIn(room, entryIndent)
		head.WriteString(inside(styles.hint.Render(clipCells(entryIndent+t.header(t.layout(width)), room))))
	}
	switch {
	case total == 0:
		head.WriteString(note(styles.hint, tr(tListEmpty)))
	case shown == 0:
		head.WriteString(note(styles.hint, tr(tFormNoEntryMatchesPressEsc, m.list.query())))
	}

	keys := "up/down move · / filter · q quit"
	if m.list.editing {
		keys = "type to filter · up/down move · enter keep filter · {back} clear filter"
	}
	m.keyHint(keys)
	foot := ""
	if framed {
		foot = panelBottom(room, true) + foot
	}
	return head.String(), foot + m.notes()
}

// filterLine shows the filter of l: while it is typed with its cursor, afterwards as the text it holds.
func (m *Model) filterLine(l *filterList) string { return m.searchLine(l, tr(tFormFilter)) }

// entryIndent is what an entry row is indented by behind the focus marker.
const entryIndent = "  "

// listRows draws the shown entry at index i of the list as a row of its table, with the columns laid out
// once for all rows.
func (m *Model) listRows() func(i int) string {
	t := m.table
	room := m.tableRoom()
	indent := entryIndent
	if m.groups != nil {
		indent = "    "
	}
	_, width := m.fitIn(room, indent)
	layout := t.layout(width)
	return func(i int) string {
		name := m.list.matches[i]
		active := i == m.list.cursor
		var row string
		if isGroupRow(name) {
			row = m.listRowIn(room, active, m.groups.styledRow(name, m.groups.open(name) || m.list.query() != ""))
		} else {
			row = m.listRowIn(room, active, indent[2:]+t.line(t.rows[name], layout))
		}
		if m.tableFramed() {
			return panelSide(row, room, true)
		}
		return row
	}
}

// listWindow is the range of shown entries whose rows fit between the frame of the list screen.
func (m *Model) listWindow() (int, int) {
	header, footer := m.listFrame()
	return m.windowIn(&m.list, header, footer, m.listRows())
}

// windowIn is the range of shown entries of l whose rows, drawn by row, fit between header and footer.
func (m *Model) windowIn(l *filterList, header, footer string, row func(i int) string) (int, int) {
	room := m.height - strings.Count(header, "\n") - strings.Count(footer, "\n") - 1
	heights := map[int]int{}
	return l.window(room, func(i int) int {
		if _, ok := heights[i]; !ok {
			heights[i] = strings.Count(row(i), "\n") + 1
		}
		return heights[i]
	})
}
