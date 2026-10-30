package tui

import (
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// The mouse reaches the editor through a list of click targets that the drawing of a frame fills as it goes:
// every place that draws something clickable says where on the screen it stands and which key press the click
// stands for. A click then does what that key does, through the same handlers, so every check the key meets
// (the admin session, the confirmations, the question before unsaved input is dropped) stays in force, and a
// click is never stronger than its key. A row is only marked by the first click, so that a click that meant to
// scroll or to take the focus opens nothing; the click on the marked row is the press. Nothing is drawn from the
// list and nothing is stored beyond one frame.

// hit is one click target: the cells [x0, x1) by [y0, y1) of the screen and what a click on them does.
type hit struct {
	x0, y0, x1, y1 int
	act            func() tea.Cmd
}

// addHit registers a click target of the workspace. A screen drawn as the background of a dialog has none: the
// dialog is what a click reaches then.
func (m *Model) addHit(x0, y0, x1, y1 int, act func() tea.Cmd) {
	if m.backdrop || m.tooSmall || x1 <= x0 || y1 <= y0 {
		return
	}
	m.hits = append(m.hits, hit{x0, y0, x1, y1, act})
}

// wheelHit is an area of the screen the wheel moves under the pointer, without moving the focus. scroll
// returns what the move asks for, as the key it stands for would.
type wheelHit struct {
	x0, y0, x1, y1 int
	scroll         func(up bool) tea.Cmd
}

// addZone registers the click target of a zone. It ranks beneath every other target, so a click lands on an
// element inside the zone if there is one and gives the zone the focus otherwise.
func (m *Model) addZone(x0, y0, x1, y1 int, z zone) {
	if m.backdrop || m.tooSmall || x1 <= x0 || y1 <= y0 {
		return
	}
	m.zoneHits = append(m.zoneHits, hit{x0, y0, x1, y1, func() tea.Cmd { return m.focusZone(z) }})
}

// addWheel registers an area that the wheel scrolls.
func (m *Model) addWheel(x0, y0, x1, y1 int, scroll func(up bool) tea.Cmd) {
	if m.backdrop || m.tooSmall || x1 <= x0 || y1 <= y0 {
		return
	}
	m.wheelHits = append(m.wheelHits, wheelHit{x0, y0, x1, y1, scroll})
}

// focusZone is the click on a free place of zone z. It does what tab does and no more: an inner level of the
// content (a typed filter, a log, a guided setup, an open detail) keeps the focus, and a detail that does not
// stand beside the rows or a page strip that is not there cannot be focused.
func (m *Model) focusZone(z zone) tea.Cmd {
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
	if m.area == areaProjects && m.prj != nil && m.prj.list.editing {
		m.prj.list.stopFilter()
	}
}

// addFrameHit registers a click target of the header.
func (m *Model) addFrameHit(x0, y0, x1, y1 int, act func() tea.Cmd) {
	if m.backdrop || m.tooSmall || x1 <= x0 || y1 <= y0 {
		return
	}
	m.frameHits = append(m.frameHits, hit{x0, y0, x1, y1, act})
}

// workspaceTop is the screen row of the first line of the workspace: below the header, the bars and the rule.
// Every click target of the workspace counts its rows from here.
func (m *Model) workspaceTop() int { return len(m.headerLines()) + m.barRows() + 1 }

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
func (m *Model) mouseEnabled() bool { return m.prefs.MouseEnabled() }

// mouseMode is the mouse mode the view asks the terminal for: cell motion while the editor takes the mouse.
func (m *Model) mouseMode() tea.MouseMode {
	if m.mouseEnabled() {
		return tea.MouseModeCellMotion
	}
	return tea.MouseModeNone
}

// wheelText is how many rows one notch of the wheel moves in a text area; elsewhere it is one step.
const wheelText = 3

// wheelMerge is the longest gap between a wheel event and the event after it that can be its release half. A
// terminal that reports a notch as a press and a release delivers both as presses, back to back.
const wheelMerge = 15 * time.Millisecond

// wheelSeen is the last wheel event that moved something.
type wheelSeen struct {
	button tea.MouseButton
	x, y   int
	at     time.Time
	ok     bool
}

// repeats reports whether the event is the release half of the notch seen last. It claims at most one event
// per notch: after it dropped one, the next equal event is a notch of its own however close it is.
func (w *wheelSeen) repeats(event tea.Mouse, now time.Time) bool {
	if w.ok && w.button == event.Button && w.x == event.X && w.y == event.Y && now.Sub(w.at) < wheelMerge {
		w.ok = false
		return true
	}
	return false
}

// updateMouse handles one mouse event. It acts only on a press of the left button and on the wheel; motion and
// release do nothing. A terminal too small for the screen and an editor with the mouse switched off take no
// mouse at all.
func (m *Model) updateMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg, tea.MouseWheelMsg:
	default:
		return nil
	}
	event := msg.Mouse()
	if m.quitting || !m.mouseEnabled() || m.terminalTooSmall() {
		return nil
	}
	wheel := event.Button == tea.MouseWheelUp || event.Button == tea.MouseWheelDown
	if !wheel && event.Button != tea.MouseLeft {
		return nil
	}
	if wheel && m.wheelSeen.repeats(event, m.timeNow()) {
		return nil
	}
	// The targets are those of the state the event meets, so the frame is drawn first.
	m.render()
	if m.tooSmall {
		return nil
	}
	if wheel {
		if m.dd != nil {
			return nil
		}
		if m.screen == screenArea {
			for i := len(m.wheelHits) - 1; i >= 0; i-- {
				if h := m.wheelHits[i]; event.X >= h.x0 && event.X < h.x1 && event.Y >= h.y0 && event.Y < h.y1 {
					cmd := h.scroll(event.Button == tea.MouseWheelUp)
					m.wheelSeen = wheelSeen{button: event.Button, x: event.X, y: event.Y, at: m.timeNow(), ok: true}
					return cmd
				}
			}
		}
		cmd := m.wheel(event)
		m.wheelSeen = wheelSeen{button: event.Button, x: event.X, y: event.Y, at: m.timeNow(), ok: true}
		return cmd
	}
	if m.dd != nil {
		return m.clickDropdown(event.X, event.Y)
	}
	hits := append(append(append([]hit{}, m.zoneHits...), m.frameHits...), m.hits...)
	for i := len(hits) - 1; i >= 0; i-- {
		h := hits[i]
		if event.X >= h.x0 && event.X < h.x1 && event.Y >= h.y0 && event.Y < h.y1 {
			return h.act()
		}
	}
	return nil
}

// timeNow is the clock of the wheel filter.
func (m *Model) timeNow() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// wheel scrolls: a notch is one press of up or down over the rows of the list.
func (m *Model) wheel(event tea.Mouse) tea.Cmd {
	if m.screen != screenList || m.list.editing {
		return nil
	}
	key := rune(tea.KeyDown)
	if event.Button == tea.MouseWheelUp {
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
func wheelCursor(cursor *int, n int, up bool) {
	by := 1
	if up {
		by = -1
	}
	*cursor = max(min(*cursor+by, n-1), 0)
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

// contentPress is pressName for a click in the content of the area screen, which the key reaches only with
// the focus there.
func (m *Model) contentPress(name string) tea.Cmd {
	if m.screen == screenArea {
		m.zone = zoneContent
	}
	return m.pressName(name)
}

// pressName is press for a key named as a footer names it.
func (m *Model) pressName(name string) tea.Cmd {
	key, ok := keyByName(name)
	if !ok {
		return nil
	}
	return m.press(key)
}

// keyByName is the key press a footer's key name stands for, for the keys a dialog's buttons use.
func keyByName(name string) (tea.KeyPressMsg, bool) {
	switch name {
	case keyOf(actEnter):
		return tea.KeyPressMsg{Code: tea.KeyEnter}, true
	case keyOf(actBack):
		return tea.KeyPressMsg{Code: tea.KeyEscape}, true
	case keyOf(actNext):
		return tea.KeyPressMsg{Code: tea.KeyTab}, true
	}
	if key, ok := functionKeys[strings.ToUpper(name)]; ok {
		return tea.KeyPressMsg{Code: key}, true
	}
	if runes := []rune(name); len(runes) == 1 && runes[0] > ' ' && !isArrowGlyph(name) && !(runes[0] >= '0' && runes[0] <= '9') {
		return tea.KeyPressMsg{Code: runes[0], Text: name}, true
	}
	return tea.KeyPressMsg{}, false
}

// isArrowGlyph reports whether name is the footer glyph of an arrow key, which a click cannot press.
func isArrowGlyph(name string) bool {
	for _, g := range arrowNames {
		if g == name {
			return true
		}
	}
	return false
}

// functionKeys are the function keys a dialog may name.
var functionKeys = map[string]rune{
	"F2": tea.KeyF2, "F3": tea.KeyF3, "F4": tea.KeyF4, "F6": tea.KeyF6, "F7": tea.KeyF7, "F8": tea.KeyF8,
	"F9": tea.KeyF9,
}

// footerSpan is one key group of a footer line: the cells it stands in and the key it names.
type footerSpan struct {
	x0, x1 int
	key    string
}

// footerLine is a footer line as drawn, and where its key groups stand.
type footerLine struct {
	plain string
	spans []footerSpan
}

// footerKey is the key a footer group opens with ("y delete" names y). Of alternatives ("n/esc") the first
// stands for the group. A group that opens with anything else, such as "1-6" or "type to filter", has none.
func footerKey(group string) string {
	word, _, _ := strings.Cut(group, " ")
	word, _, _ = strings.Cut(word, "/")
	if _, ok := keyByName(word); ok {
		return word
	}
	return ""
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
				if span.key == "" {
					continue
				}
				name := span.key
				m.addHit(x0+span.x0, y0+i, x0+span.x1, y0+i+1, func() tea.Cmd { return m.pressName(name) })
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

// navSpan is the cells one entry of the tab line or the option bar stands in.
type navSpan struct{ x0, x1 int }

// spanItems places the items of a bar one after the other from column start on, with sep between them, and
// cuts the places to width cells.
func spanItems(items []string, start int, sep string, width int) []navSpan {
	spans := make([]navSpan, len(items))
	x := start
	for i, item := range items {
		w := ansi.StringWidth(item)
		spans[i] = navSpan{min(x, width), min(x+w, width)}
		x += w + ansi.StringWidth(sep)
	}
	return spans
}

// backShown reports whether esc leads one level back from the screen shown now, which is where the back
// button stands: from a dialog, a question or a list with a filter, from the content or the page strip of an
// area, never from the bare list or the tab line.
func (m *Model) backShown() bool {
	if m.backdrop || m.dd != nil {
		return false
	}
	if _, ok := keyByName(keyOf(actBack)); !ok {
		return false
	}
	if m.home == screenArea {
		return m.screen != screenArea || m.zoneNow() != zoneTabs
	}
	return m.screen != screenList || m.list.editing || m.list.query() != ""
}

// pressBack is a press of the key bound to esc; the click on the back button uses it so it meets every rule
// the key meets.
func (m *Model) pressBack() tea.Cmd {
	key, ok := keyByName(keyOf(actBack))
	if !ok {
		return nil
	}
	return m.press(key)
}

// registerBack makes the back button of the header clickable.
func (m *Model) registerBack(span navSpan) {
	m.addFrameHit(span.x0, 0, span.x1, 1, m.pressBack)
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

// fragmentWindow is the longest gap between the pieces of one split mouse report. Typing is slower than this.
const fragmentWindow = 200 * time.Millisecond

// bracketHold is how long a "[" that may open the rest of a report, alt+[ or a "[" after an escape, waits
// for it before it counts as the key it is. The text field types it, so it cannot be let through first and
// taken back. It is as long as fragmentWindow: a head the filter would still take as the rest must find the
// "[" still held, or the "[" is typed and the rest dropped. The input reader reports the "[" only after it
// waited for the rest itself, so the rest is later here than at the terminal.
const bracketHold = fragmentWindow

// fragmentTail matches what a mouse report of the SGR form can still lack: the rest of its parameters and the
// final M or m.
var fragmentTail = regexp.MustCompile(`^[0-9;]*[Mm]?`)

// fragmentHead matches the start of such a report that is left once its escape and "[" went into keys of their own.
var fragmentHead = regexp.MustCompile(`^<[0-9;]*[Mm]?`)

// reportStart matches the start of such a report the input reader gave up on, its rest still to come.
var reportStart = regexp.MustCompile(`^\x1b\[<[0-9;]*$`)

// keysMsg delivers keys the filter held back, in order, as one message.
type keysMsg []tea.KeyPressMsg

// heldKeyMsg is the end of the hold of the "[" number id.
type heldKeyMsg struct{ id int }

// fragmentEsc is the filter state after a lone escape; it names that state, not a key to dispatch on.
const fragmentEsc = "esc"

// fragmentFilter drops the pieces of a mouse report that the terminal delivered in more than one read with a
// pause between them longer than the input reader waits: the reader then reports the start it has as an
// unknown sequence, or as an escape or alt+[, and the rest as typed characters like "<65;12;5M", which a text
// field would take. The input reader joins pieces that follow each other within its escape wait, so the filter
// only meets those after a longer pause. A piece counts only when it follows the one before it within
// fragmentWindow and fits the report form, so the same characters typed by hand, pasted, or standing alone pass
// unchanged. Only a "[" that may open a piece is held back, briefly; a lone escape is a real key and passes at
// once, so an escape byte the reader already gave up on still reaches the model as esc.
// On the way it gives keys the names stored key bindings know (see legacyKey and linuxFunctionKey).
type fragmentFilter struct {
	now  func() time.Time
	send func(tea.Msg) // delivers heldKeyMsg to the event loop; without it nothing is held back

	state string // fragmentEsc or "[" when that key came last, "tail" while a report lacks its end
	at    time.Time
	held  *tea.KeyPressMsg
	id    int
}

func newFragmentFilter(now func() time.Time) *fragmentFilter {
	if now == nil {
		now = time.Now
	}
	return &fragmentFilter{now: now}
}

// unknownSequence returns the bytes of an input sequence the input reader could not name.
func unknownSequence(msg tea.Msg) (string, bool) {
	switch u := msg.(type) {
	case uv.UnknownEvent:
		return string(u), true
	case uv.UnknownCsiEvent:
		return string(u), true
	}
	return "", false
}

// filter is the tea.WithFilter function. It runs in the event loop, one message at a time.
func (f *fragmentFilter) filter(_ tea.Model, msg tea.Msg) tea.Msg {
	if h, ok := msg.(heldKeyMsg); ok {
		if f.held == nil || h.id != f.id {
			return nil
		}
		key := *f.held
		f.held = nil
		// The hold is over: a head arriving now is text, whatever the window says, so no race remains
		// between this timer and the window comparison.
		if f.state == "[" {
			f.state = ""
		}
		return key
	}
	if unknown, ok := unknownSequence(msg); ok && (reportStart.MatchString(unknown) || unknown == "\x1b[[") {
		f.state, f.at = "tail", f.now()
		if unknown == "\x1b[[" {
			f.state = "[["
		}
		if held := f.held; held != nil {
			f.held = nil
			return keysMsg{*held}
		}
		return nil
	}
	if paste, ok := msg.(tea.PasteMsg); ok {
		msg = pasteKey(paste.Content)
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return msg
	}
	key = legacyKey(key)
	at, last := f.now(), f.state
	f.state = ""
	if last != "" && at.Sub(f.at) > fragmentWindow {
		last = ""
	}
	if last == "[[" {
		if fn, ok := linuxFunctionKey(key); ok {
			return fn
		}
	}
	held := f.held
	f.held = nil
	_, isPaste := pasted(key)
	text, plain := "", key.Text != "" && !key.Mod.Contains(tea.ModAlt) && !isPaste
	if plain {
		text = key.Text
	}
	var n int
	switch last {
	case "[":
		if plain && strings.HasPrefix(text, "<") {
			n = len(fragmentHead.FindString(text))
		}
	case "tail":
		if plain {
			n = len(fragmentTail.FindString(text))
		}
	}
	if n > 0 {
		if piece := text[:n]; !strings.HasSuffix(piece, "M") && !strings.HasSuffix(piece, "m") {
			f.state, f.at = "tail", at
		}
		if n == len(text) {
			return nil
		}
		return textKey(text[n:])
	}
	// Not a piece: a held "[" was a key after all and goes first.
	switch {
	case isPaste:
	case key.Code == tea.KeyEscape && key.Mod == 0:
		f.state, f.at = fragmentEsc, at
	case f.send != nil && (key.Code == '[' && key.Mod == tea.ModAlt || last == fragmentEsc && text == "["):
		f.state, f.at = "[", at
		f.held = &key
		f.id++
		id, send := f.id, f.send
		time.AfterFunc(bracketHold, func() { send(heldKeyMsg{id}) })
		if held != nil {
			return keysMsg{*held}
		}
		return nil
	}
	if held != nil {
		return keysMsg{*held, key}
	}
	return key
}
