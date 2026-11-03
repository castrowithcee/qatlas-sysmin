package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A dropdown field is one line, "Label: Value ▾", on the field surface; the focused one is drawn in the accent
// colour and, without colour, carries the "> " marker of barMarker. Its choices open as a small list directly
// under the field. Forms and bars share this block.

// dropdownArrow ends every dropdown field.
const dropdownArrow = "▾"

// fieldStyle is the surface of a field: the field colour behind the text, the accent colour and bold while
// the field has the focus. Without colour it names nothing; the marker carries the focus then.
func fieldStyle(ui *uiContext, focused bool) lipgloss.Style {
	p := ui.palette()
	style := lipgloss.NewStyle()
	if p.Surface != "" {
		style = style.Background(lip(p.Surface)).Foreground(lip(p.Text))
	}
	if focused {
		style = style.Bold(true)
		if p.Accent != "" {
			style = style.Foreground(lip(p.Accent))
		}
	}
	return style
}

// dropdownText is the plain text of a field, at most width cells wide: where the value does not fit it is
// cut with "…", and label and arrow stay.
func dropdownText(label, value string, width int) string {
	head := " " + label + ": "
	tail := " " + dropdownArrow + " "
	room := width - lipgloss.Width(head) - lipgloss.Width(tail)
	if room < 1 {
		room = 1
	}
	return head + truncateCells(value, room) + tail
}

// dropdownField draws a field of at most width cells (padded by one cell on each side).
func dropdownField(ui *uiContext, label, value string, focused bool, width int) string {
	return fieldStyle(ui, focused).Render(dropdownText(label, value, width))
}

// dropdownWidth is the width of a field with its whole value.
func dropdownWidth(label, value string) int {
	return lipgloss.Width(dropdownText(label, value, 1<<20))
}

// barMarker is what stands in front of a field: "> " while the field has the focus.
func barMarker(focused bool) string {
	if focused {
		return "> "
	}
	return "  "
}

// dropdown is the open list of a field.
type dropdown struct {
	// id names the field the list belongs to; the drawing of the field says where it stands.
	id     string
	values []string
	cursor int
	active int
	choose func(i int) tea.Cmd
	// x and y are the screen cell of the field's left edge and its row; placed says the frame set them.
	x, y   int
	placed bool
}

// openDropdown opens the list of a field, on the active value.
func (m *Model) openDropdown(id string, values []string, active int, choose func(i int) tea.Cmd) {
	m.dd = &dropdown{id: id, values: values, cursor: active, active: active, choose: choose}
}

// noteField tells an open list where its field stands.
func (m *Model) noteField(id string, x, y int) {
	if d := m.dd; d != nil && d.id == id {
		d.x, d.y, d.placed = x, y, true
	}
}

// updateDropdown is a key while a list is open: up and down move, enter takes the value, esc closes it.
func (m *Model) updateDropdown(key tea.KeyPressMsg) tea.Cmd {
	d := m.dd
	switch {
	case m.keyIs(key, actUp):
		d.cursor = max(d.cursor-1, 0)
	case m.keyIs(key, actDown):
		d.cursor = min(d.cursor+1, len(d.values)-1)
	case m.keyIs(key, actHome):
		d.cursor = 0
	case m.keyIs(key, actEnd):
		d.cursor = len(d.values) - 1
	case m.keyIs(key, actEnter):
		return m.chooseDropdown(d.cursor)
	case m.keyIs(key, actBack):
		m.dd = nil
	}
	return nil
}

// chooseDropdown closes the list and gives the field its i-th value.
func (m *Model) chooseDropdown(i int) tea.Cmd {
	d := m.dd
	m.dd = nil
	if d == nil || i < 0 || i >= len(d.values) {
		return nil
	}
	return d.choose(i)
}

// lines draws the list as a framed block.
func (d *dropdown) lines(ui *uiContext) []string {
	inner := 0
	for _, v := range d.values {
		inner = max(inner, lipgloss.Width(v))
	}
	inner += 2
	rows := make([]string, len(d.values))
	for i, v := range d.values {
		line := padLine(barMarker(i == d.cursor)+v, inner)
		if i == d.cursor {
			line = ui.st(kittui.StyleActiveRow).Render(line)
		}
		rows[i] = line
	}
	return panel(ui, "", rows, inner, len(rows), panelOpts{focused: true})
}

// overlayDropdown puts the open list over view, under its field, and registers its click targets. A list whose
// field the frame did not draw closes.
func (m *Model) overlayDropdown(view string) string {
	d := m.dd
	if d == nil {
		return view
	}
	if !d.placed || m.screen != m.home || m.tooSmall {
		m.dd = nil
		return view
	}
	box := d.lines(m.ui)
	w := ansi.StringWidth(box[0])
	x := max(min(d.x, m.termWidth-w), 0)
	y := min(d.y+1, max(m.termHeight-1-len(box), 0))
	rows := strings.Split(view, "\n")
	for i, line := range box {
		r := y + i
		if r >= len(rows) {
			break
		}
		// A row the screen leaves short is padded, so the list stands at its column.
		left := ansi.Cut(rows[r], 0, x)
		left += strings.Repeat(" ", max(x-ansi.StringWidth(left), 0))
		rows[r] = left + line + ansi.Cut(rows[r], x+w, m.termWidth)
	}
	// The list is modal: what stands beneath it takes no click and no wheel while it is open.
	m.mt = mouseTargets{}
	m.addWheel(x, y, x+w, y+len(box), zoneContent, func(up bool) tea.Cmd { wheelCursor(&d.cursor, len(d.values), up); return nil })
	for i := range len(box) - 2 {
		m.addHit(x+1, y+1+i, x+w-1, y+2+i, func() tea.Cmd { return m.chooseDropdown(i) })
	}
	return strings.Join(rows, "\n")
}
