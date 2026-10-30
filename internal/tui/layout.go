package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// layoutWorkspace derives the room of the workspace from the terminal. It takes the full width; above it
// stand the header (one line), the tab line, the page strip where the area has pages, and a rule, below it the
// one-line footer.
func (m *Model) layoutWorkspace() {
	m.width = max(m.termWidth, 1)
	m.height = max(m.termHeight-len(m.headerLines())-m.barRows()-2, 1)
}

// frame puts the workspace under the header, the bars and a rule, and the footer under it. The workspace is cut or
// padded to exactly the rows layoutWorkspace gave it, so the footer is always the last line of the screen.
func (m *Model) frame(workspace string) string {
	lines, back := m.headerLayout()
	m.registerBack(back)
	lines = append(lines, m.barLines()...)
	lines = append(lines, styles.quietBorder.Render(strings.Repeat("─", m.termWidth)))
	body := strings.Split(workspace, "\n")
	if len(body) > m.height {
		body = body[:m.height]
	}
	for len(body) < m.height {
		body = append(body, "")
	}
	lines = append(lines, body...)
	lines = append(lines, m.footerLine())
	return m.boundTo(strings.Join(lines, "\n"), m.termWidth, m.termHeight)
}

// footerHits makes the key groups of the footer clickable, where the footer holds the buttons of a dialog.
func (m *Model) footerHits() {
	for _, span := range m.footerSpans {
		if span.key == "" {
			continue
		}
		name := span.key
		m.addHit(span.x0, m.termHeight-1, span.x1, m.termHeight, func() tea.Cmd { return m.pressName(name) })
	}
}

// headerSep is the gap between two parts of the header.
const headerSep = "  "

// headerLines are the header rows above the workspace: always one row.
func (m *Model) headerLines() []string {
	lines, _ := m.headerLayout()
	return lines
}

// headerLayout is headerLines and the cells of the back button in the row (empty when it is not shown). The
// back button stands at the right end where the row has room for it.
func (m *Model) headerLayout() ([]string, navSpan) {
	width := max(m.termWidth, 1)
	if m.home == screenArea {
		return m.serviceHeader(width)
	}
	line, _ := fitParts(m.headerStatus(), width)
	line, back := placeRight(line, m.headerRights(), width)
	return []string{line}, back
}

// headerRight is a block at the right end of the header: the cells of the back button in it, if it holds one.
type headerRight struct {
	text string
	back navSpan
}

// placeRight puts the widest block that fits, with at least headerSep before it, at the right end of line and
// returns the line and the cells of the back button in it.
func placeRight(line string, rights []headerRight, width int) (string, navSpan) {
	for _, r := range rights {
		w := lipgloss.Width(r.text)
		if gap := width - lipgloss.Width(line) - w; gap >= lipgloss.Width(headerSep) {
			x := width - w
			back := navSpan{}
			if r.back.x1 > r.back.x0 {
				back = navSpan{x + r.back.x0, x + r.back.x1}
			}
			return line + strings.Repeat(" ", gap) + r.text, back
		}
	}
	return line, navSpan{}
}

// headerRights are the blocks the right end of the header can show: the back button, where esc leads back.
func (m *Model) headerRights() []headerRight {
	back := m.backButton()
	if back == "" {
		return nil
	}
	return []headerRight{{back, navSpan{0, lipgloss.Width(back)}}}
}

// backButton is the clickable mark of the way back, drawn like the key hints; empty where esc leads no level back.
func (m *Model) backButton() string {
	if !m.backShown() {
		return ""
	}
	return styles.hint.Render("[" + keyLabel(actBack) + "] " + tr(tHeaderBack))
}

// fitParts lays styled parts out in one row of at most width cells, separated by headerSep, and returns the
// row and its width. The part that does not fit is cut with an ellipsis and the parts after it are dropped.
func fitParts(parts []string, width int) (string, int) {
	line, used := "", 0
	for _, part := range parts {
		if used > 0 {
			if used+lipgloss.Width(headerSep)+1 > width {
				break
			}
			line += headerSep
			used += lipgloss.Width(headerSep)
		}
		part = truncateCells(part, width-used)
		line += part
		used += lipgloss.Width(part)
	}
	return line, used
}

// headerLogo is the Nerd Font glyph in front of the name; without that preference the name stands alone.
const headerLogo = ""

// headerStatus are the left parts of the header: the name.
func (m *Model) headerStatus() []string {
	name := styles.title.Render(tr(tHeaderTitle))
	if m.prefs.Icons == uiprefs.IconsNerdFont {
		name = styles.active.Render(headerLogo) + " " + name
	}
	return []string{name}
}

// serviceHeader is the header of the service interface: at the left the name and the marks of the vault and
// the admin session, at the right the back button and the keys of help and commands. It stays one row. Where
// the row is too narrow, the parts give way in a fixed order: first the marks shrink to short words and then
// go, then the key hints, then the back button; the name stays.
func (m *Model) serviceHeader(width int) ([]string, navSpan) {
	for _, c := range m.headerCombos(m.backButton()) {
		line, _ := fitParts(m.headerMarks(c.left), width)
		if placed, span, ok := placeBlock(line, c.right, width); ok {
			return []string{placed}, span
		}
	}
	line, _ := fitParts(m.headerMarks(3), width)
	return []string{line}, navSpan{}
}

// headerCombo pairs a level of the marks with a block at the right end; a lower level shows less.
type headerCombo struct {
	left  int
	right headerRight
}

// keyHints are the keys of help and of the command line as they are bound now, dimmed like the back button.
func keyHints() string {
	var parts []string
	for _, h := range []struct {
		act  action
		text textID
	}{{actHelp, tHeaderHelp}, {actCommand, tHeaderCmd}} {
		if key := keyLabel(h.act); key != "" {
			parts = append(parts, styles.hint.Render("["+key+"] "+tr(h.text)))
		}
	}
	return strings.Join(parts, headerSep)
}

// headerCombos are the layouts to try, richest first: the marks give way before the key hints, the key hints
// before the back button.
func (m *Model) headerCombos(back string) []headerCombo {
	hints := keyHints()
	var combos []headerCombo
	if back != "" && hints != "" {
		right := headerRight{text: back + headerSep + hints, back: navSpan{0, lipgloss.Width(back)}}
		for level := 0; level <= 3; level++ {
			combos = append(combos, headerCombo{level, right})
		}
	}
	if hints != "" && back == "" {
		for level := 0; level <= 3; level++ {
			combos = append(combos, headerCombo{level, headerRight{text: hints}})
		}
	}
	if back != "" {
		right := headerRight{text: back, back: navSpan{0, lipgloss.Width(back)}}
		for level := 0; level <= 3; level++ {
			combos = append(combos, headerCombo{level, right})
		}
	}
	return combos
}

// placeBlock puts block at the right end of line, with at least headerSep before it.
func placeBlock(line string, block headerRight, width int) (string, navSpan, bool) {
	w := lipgloss.Width(block.text)
	gap := width - lipgloss.Width(line) - w
	if gap < lipgloss.Width(headerSep) {
		return "", navSpan{}, false
	}
	span := navSpan{}
	if block.back.x1 > block.back.x0 {
		span = navSpan{width - w + block.back.x0, width - w + block.back.x1}
	}
	return line + strings.Repeat(" ", gap) + block.text, span, true
}

// headerMarks are the left parts of the header at a level: 0 names the states in full, 1 in short words,
// 2 leaves out the admin mark, 3 is the name alone. The marks pair a symbol with a word, so no state rests on
// its colour.
func (m *Model) headerMarks(level int) []string {
	parts := m.stateMarks(level)
	if latest := m.updateAvailable(); latest != "" && level < 3 {
		text := tr(tUpdHeader, latest)
		if level >= 1 {
			text = tr(tUpdHeaderShort)
		}
		parts = append(parts, statusMark(statusWarn, text))
	}
	return parts
}

// stateMarks are the marks of the service, the vault and the admin session at a level of headerMarks.
func (m *Model) stateMarks(level int) []string {
	parts := m.headerStatus()
	if level >= 3 {
		return parts
	}
	short := level >= 1
	pick := func(full, brief textID) string {
		if short {
			return tr(brief)
		}
		return tr(full)
	}
	switch {
	case !m.svc.known:
		return append(parts, styles.hint.Render(tr(tHeaderChecking)))
	case !m.svc.reachable && m.svc.noGroup:
		return append(parts, statusMark(statusFail, pick(tServiceGroup, tServiceGroupShort)))
	case !m.svc.reachable:
		return append(parts, statusMark(statusFail, pick(tServiceDown, tServiceDownShort)))
	}
	switch m.svc.st.Vault {
	case control.VaultAbsent:
		return append(parts, statusMark(statusWarn, tr(tVaultNone)))
	case control.VaultLocked:
		parts = append(parts, statusMark(statusWarn, pick(tVaultLocked, tVaultLockedShort)))
	case control.VaultUnlocked:
		parts = append(parts, statusMark(statusOK, pick(tVaultOpen, tVaultOpenShort)))
	default:
		parts = append(parts, statusMark(statusWarn, tr(tErrService)))
	}
	if level >= 2 {
		return parts
	}
	switch {
	case m.adminOnce():
		return append(parts, statusMark(statusOK, tr(tAdminOnce)))
	case m.adminActive():
		return append(parts, statusMark(statusOK, tr(map[bool]textID{false: tAdminLeft, true: tAdminLeftShort}[short], m.adminMinutes())))
	}
	return append(parts, statusMark(statusDim, tr(tAdminOff)))
}
