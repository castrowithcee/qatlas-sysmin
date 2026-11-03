package tui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The mouse reaches the editor through the kit's MouseMap of the frame drawn last. Every place that draws
// something clickable adds a target as it goes: the frame its keys, tabs and bars, the screens their rows,
// fields and buttons as items, whose click the editor runs here, and the parts the wheel scrolls as zones. A
// click does what its key does, through the same handlers, so every check the key meets (the confirmations,
// the question before unsaved input is dropped) stays in force, and a click is never stronger than its key. A
// row is only marked by the first click, so that a click that meant to scroll or to take the focus opens
// nothing; the click on the marked row is the press. Nothing is stored beyond one frame.

// mouseTargets are the targets of the frame being drawn: what a click on each item does and what the wheel
// does over each zone.
type mouseTargets struct {
	targets []kittui.Target
	clicks  []func() tea.Cmd
	wheels  map[kittui.FocusZone]func(up bool) tea.Cmd
}

// takesTargets reports whether the drawing in progress adds targets. A screen drawn as the background of a
// dialog has none: the dialog is what a click reaches then.
func (m *Model) takesTargets(x0, y0, x1, y1 int) bool {
	return !m.backdrop && !m.tooSmall && x1 > x0 && y1 > y0
}

// addTarget adds a target of the kit, such as one the frame drew.
func (m *Model) addTarget(t kittui.Target) {
	if m.takesTargets(t.Rect.X, t.Rect.Y, t.Rect.X+t.Rect.Width, t.Rect.Y+t.Rect.Height) {
		m.mt.targets = append(m.mt.targets, t)
	}
}

// addHit adds a click target of the cells [x0, x1) by [y0, y1): an item whose click runs act.
func (m *Model) addHit(x0, y0, x1, y1 int, act func() tea.Cmd) {
	if !m.takesTargets(x0, y0, x1, y1) {
		return
	}
	m.mt.targets = append(m.mt.targets, kittui.Target{Rect: rect(x0, y0, x1, y1), Item: strconv.Itoa(len(m.mt.clicks))})
	m.mt.clicks = append(m.mt.clicks, act)
}

// addZone adds the free area of zone z. It lies beneath every other target, so a click lands on an element
// inside the zone if there is one and gives the zone the focus otherwise.
func (m *Model) addZone(x0, y0, x1, y1 int, z kittui.FocusZone) {
	if m.takesTargets(x0, y0, x1, y1) {
		m.mt.targets = append(m.mt.targets, kittui.Target{Rect: rect(x0, y0, x1, y1), Zone: z})
	}
}

// addWheel adds a part of zone z that the wheel scrolls under the pointer, without moving the focus. scroll
// returns what the move asks for, as the key it stands for would. The kit names the zone of a notch only, so
// a zone has one scroll.
func (m *Model) addWheel(x0, y0, x1, y1 int, z kittui.FocusZone, scroll func(up bool) tea.Cmd) {
	if !m.takesTargets(x0, y0, x1, y1) {
		return
	}
	m.addZone(x0, y0, x1, y1, z)
	if m.mt.wheels == nil {
		m.mt.wheels = map[kittui.FocusZone]func(up bool) tea.Cmd{}
	}
	m.mt.wheels[z] = scroll
}

func rect(x0, y0, x1, y1 int) kittui.Rect {
	return kittui.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
}

// focusZone is the click on a free place of zone z. It does what tab does and no more: an inner level of the
// content (a typed filter, a log, a guided setup, an open detail) keeps the focus, and a detail that does not
// stand beside the rows or a page strip that is not there cannot be focused.
func (m *Model) focusZone(z kittui.FocusZone) tea.Cmd {
	if m.screen != screenArea || m.innerLevel() {
		return nil
	}
	if (z == zoneDetail && !m.detailZoneAvailable()) || (z == zoneSubs && !m.hasSubs()) {
		return nil
	}
	m.zone = z
	return nil
}

// focusRows gives the rows the focus for a click on a row. It reports whether the detail had the focus, in
// which case the click only moves the focus and opens nothing.
func (m *Model) focusRows() bool {
	if m.screen != screenArea || m.innerLevel() {
		return false
	}
	fromDetail := m.zoneNow() == zoneDetail
	m.zone = zoneContent
	return fromDetail
}

// endInput ends a filter being typed, as the first level of esc does, before a click leaves for a tab.
func (m *Model) endInput() {
	if p := m.currentPage(); p.filtering() {
		p.list.stopFilter()
	}
}

// hitTop is the screen row of the first line after header, a text the workspace starts with.
func (m *Model) hitTop(header string) int { return m.workspaceTop() + strings.Count(header, "\n") }

// hitRows makes the rows [start, end) of a windowed list clickable. The rows stand one under the other from
// row top on, each as high as row draws it, in the first width cells.
func (m *Model) hitRows(top, width, start, end int, row func(i int) string, act func(i int) tea.Cmd) {
	y := top
	for i := start; i < end; i++ {
		height := strings.Count(row(i), "\n") + 1
		m.addHit(0, y, width, y+height, func() tea.Cmd { return act(i) })
		y += height
	}
}

// mouseEnabled reports whether the editor takes mouse input.
func (m *Model) mouseEnabled() bool { return m.prefs.MouseEnabled }

// mouseMode is the mouse mode the view asks the terminal for: cell motion while the editor takes the mouse.
func (m *Model) mouseMode() tea.MouseMode {
	if m.mouseEnabled() {
		return tea.MouseModeCellMotion
	}
	return tea.MouseModeNone
}

// updateMouse handles one mouse event through the map of the frame the event meets. A press of the left button
// and the wheel act; the map takes nothing while the mouse is switched off or the terminal is too small. An
// open list takes every click: one beside its entries only closes it.
func (m *Model) updateMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg, tea.MouseWheelMsg:
	default:
		// Motion and release act on nothing, so they draw nothing either.
		return nil
	}
	if m.quitting {
		return nil
	}
	m.render()
	return m.handleMouse(msg)
}

// handleMouse is updateMouse on the map of the frame drawn last.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	res := m.mouse.Handle(msg)
	if _, click := msg.(tea.MouseClickMsg); click && m.dd != nil && res.Item == "" {
		if msg.Mouse().Button == tea.MouseLeft && m.mouseEnabled() && !m.tooSmall {
			m.dd = nil
		}
		return nil
	}
	switch {
	case !res.Handled:
		return nil
	case res.WheelSteps != 0:
		up := res.WheelSteps < 0
		if scroll, ok := m.mt.wheels[res.Zone]; ok && res.Zone != "" {
			return scroll(up)
		}
		if m.dd != nil {
			return nil
		}
		return m.wheel(up)
	case res.Item != "":
		i, err := strconv.Atoi(res.Item)
		if err != nil || i < 0 || i >= len(m.mt.clicks) {
			return nil
		}
		return m.mt.clicks[i]()
	case res.Action != "":
		return m.pressAction(res.Action)
	case res.Tab != "" && m.screen == screenArea:
		m.endInput()
		if sub, ok := m.tabIndex(res.Tab); ok {
			return m.setSubPage(sub)
		}
		return nil
	case res.Area != "" && m.screen == screenArea:
		m.endInput()
		if a, ok := areaOf(res.Area); ok {
			return m.setArea(a)
		}
		return nil
	case res.Zone != "":
		return m.focusZone(res.Zone)
	}
	return nil
}

// timeNow is the clock of the editor; tests stand in for it.
func (m *Model) timeNow() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// wheel scrolls the bare list: a notch is one press of up or down over its rows.
func (m *Model) wheel(up bool) tea.Cmd {
	if m.screen != screenList || m.list.editing {
		return nil
	}
	key := rune(tea.KeyDown)
	if up {
		key = tea.KeyUp
	}
	return m.press(tea.KeyPressMsg{Code: key})
}

// wheelList is a notch of the wheel over the rows of a list: one step of the mark, as up and down take it, but
// it stops at both ends (up on the first row never leaves for the bar) and rests while a filter is typed.
// moved says what the key does after a step.
func wheelList(l *filterList, up bool, moved func() tea.Cmd) tea.Cmd {
	by := 1
	if up {
		by = -1
	}
	before := l.cursor
	if !l.editing {
		l.move(by)
	}
	if l.cursor == before {
		return nil
	}
	if moved == nil {
		return nil
	}
	return moved()
}

// wheelCursor is a notch of the wheel over a list of n entries kept as a bare cursor.
func wheelCursor(cursor *int, n int, up bool) { *cursor = wheelStep(*cursor, n, up) }

// wheelStep is the place one notch of the wheel leads to from at among n places; it stops at both ends,
// where the keys may wrap round.
func wheelStep(at, n int, up bool) int {
	steps := 1
	if up {
		steps = -1
	}
	return kittui.WheelStep(at, n, steps)
}

// press hands a key to the editor as if it had been typed. A mouse event never makes a character: a letter
// is not passed on where the screen reads text.
func (m *Model) press(key tea.KeyPressMsg) tea.Cmd {
	if key.Text != "" && m.typing() {
		return nil
	}
	_, cmd := m.Update(key)
	return cmd
}

// contentPress is pressAction for a click in the content of the area screen, which the key reaches only with
// the focus there.
func (m *Model) contentPress(a action) tea.Cmd {
	if m.screen == screenArea {
		m.zone = zoneContent
	}
	return m.pressAction(a)
}

// pressAction is press for the first key of an action, where a click can press it.
func (m *Model) pressAction(a action) tea.Cmd {
	key, ok := m.keyByName(m.firstKeyLabel(a))
	if !ok {
		return nil
	}
	return m.press(key)
}

// keyByName is the key press a key label stands for, for the keys a click can press.
func (m *Model) keyByName(name string) (tea.KeyPressMsg, bool) {
	switch name {
	case m.keyLabel(actEnter):
		return tea.KeyPressMsg{Code: tea.KeyEnter}, true
	case m.keyLabel(actBack):
		return tea.KeyPressMsg{Code: tea.KeyEscape}, true
	case m.keyLabel(actNext):
		return tea.KeyPressMsg{Code: tea.KeyTab}, true
	}
	if key, ok := functionKeyCode(strings.ToUpper(name)); ok {
		return tea.KeyPressMsg{Code: key}, true
	}
	if runes := []rune(name); len(runes) == 1 && runes[0] > ' ' && !isArrowGlyph(name) && !(runes[0] >= '0' && runes[0] <= '9') {
		return tea.KeyPressMsg{Code: runes[0], Text: name}, true
	}
	return tea.KeyPressMsg{}, false
}

// isArrowGlyph reports whether name is the footer glyph of an arrow key, which a click cannot press.
func isArrowGlyph(name string) bool {
	return len([]rune(name)) == 1 && strings.Contains("↑↓←→", name)
}

// functionKeyCode is the code of a function key a dialog may name, spelled as a footer spells it ("F3").
func functionKeyCode(name string) (rune, bool) {
	switch name {
	case "F2":
		return tea.KeyF2, true
	case "F3":
		return tea.KeyF3, true
	case "F4":
		return tea.KeyF4, true
	case "F6":
		return tea.KeyF6, true
	case "F7":
		return tea.KeyF7, true
	case "F8":
		return tea.KeyF8, true
	case "F9":
		return tea.KeyF9, true
	}
	return 0, false
}

// footerSpan is one key group of a key line: the cells it stands in and the action of its first key.
type footerSpan struct {
	x0, x1 int
	act    action
}

// footerLine is a footer line as drawn, and where its key groups stand.
type footerLine struct {
	plain string
	spans []footerSpan
}

// hitDialog makes the keys of the footer lines of a dialog clickable: they are its buttons. lines are the
// rows of the dialog text, drawn from column left+2 of row top+1 on, inside its frame.
func (m *Model) hitDialog(lines []string, log []footerLine, top, left int) {
	m.hitFooters(lines, log, top+1, left+2)
}

// hitFooters makes the key groups of the footer lines found among lines clickable. lines are drawn from screen
// row y0 and column x0 on.
func (m *Model) hitFooters(lines []string, log []footerLine, y0, x0 int) {
	used := make([]bool, len(lines))
	for _, footer := range log {
		for i, line := range lines {
			if used[i] || ansi.Strip(line) != footer.plain {
				continue
			}
			used[i] = true
			for _, span := range footer.spans {
				if span.act == "" {
					continue
				}
				act := span.act
				m.addHit(x0+span.x0, y0+i, x0+span.x1, y0+i+1, func() tea.Cmd { return m.pressAction(act) })
			}
			break
		}
	}
}

// dialogOrigin is the top row and left column of a dialog centred in a screen of width by height.
func dialogOrigin(dialog []string, width, height int) (top, left int) {
	dialogWidth := 0
	for _, line := range dialog {
		dialogWidth = max(dialogWidth, ansi.StringWidth(line))
	}
	return max((height-len(dialog))/2, 0), max((width-dialogWidth)/2, 0)
}

// backShown reports whether esc leads one level back from the screen shown now, which is where the back
// button stands: from a dialog, a question or a list with a filter, from the content or the page strip of an
// area, never from the bare list or the tab line.
func (m *Model) backShown() bool {
	if m.backdrop || m.dd != nil {
		return false
	}
	if _, ok := m.keyByName(m.keyLabel(actBack)); !ok {
		return false
	}
	if m.home == screenArea {
		return m.screen != screenArea || m.zoneNow() != zoneTabs
	}
	return m.screen != screenList || m.list.editing || m.list.query() != ""
}

// markOrAct is the click on a row of a list: it marks the row, and only a click on the row that already was
// marked, with the focus on the rows, does act. focused says the rows had the focus before the click.
func markOrAct(cursor *int, i int, focused bool, act func() tea.Cmd) tea.Cmd {
	if !focused || *cursor != i {
		*cursor = i
		return nil
	}
	return act()
}

// clickListRow is a click on a row of the list: it marks the row, and a click on the marked row does what enter
// does there, which opens an entry. A group folds at the first click, which is harmless and undone by the next.
// A filter being typed is only kept.
func (m *Model) clickListRow(i int) tea.Cmd {
	if i < 0 || i >= len(m.list.matches) || m.screen != screenList {
		return nil
	}
	if m.list.editing {
		m.list.cursor = i
		m.list.stopFilter()
		return nil
	}
	enter := func() tea.Cmd { return m.press(tea.KeyPressMsg{Code: tea.KeyEnter}) }
	if isGroupRow(m.list.matches[i]) {
		m.list.cursor = i
		return enter()
	}
	return markOrAct(&m.list.cursor, i, true, enter)
}

// hitList makes the rows of the list screen clickable.
func (m *Model) hitList(header string, start, end int, row func(i int) string) {
	m.hitRows(m.hitTop(header), m.usable(0), start, end, row, m.clickListRow)
}

// hitTable makes the rows of a windowed list of a screen clickable: a click marks a row, and a click on the
// marked row is a press of enter on it.
func (m *Model) hitTable(l *filterList, on screen, header string, start, end int, row func(i int) string) {
	m.hitRows(m.hitTop(header), m.usable(0), start, end, row, func(i int) tea.Cmd {
		if m.screen != on || i >= len(l.matches) {
			return nil
		}
		return markOrAct(&l.cursor, i, true, func() tea.Cmd { return m.press(tea.KeyPressMsg{Code: tea.KeyEnter}) })
	})
}

// hitSelect makes the rows of a windowed list of a screen clickable by choosing the row only; what enter would
// do on it is left to the key.
func (m *Model) hitSelect(l *filterList, on screen, header string, start, end int, row func(i int) string) {
	m.hitRows(m.hitTop(header), m.usable(0), start, end, row, func(i int) tea.Cmd {
		if m.screen == on && i < len(l.matches) {
			l.cursor = i
		}
		return nil
	})
}
