package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// layoutWorkspace derives the room of the workspace from the terminal. It takes the full width; above it
// stand the header (one line) and a rule, below it the one-line footer.
func (m *Model) layoutWorkspace() {
	m.width = max(m.termWidth, 1)
	m.height = max(m.termHeight-len(m.headerLines())-2, 1)
}

// frame puts the workspace under the header and a rule, and the footer under it. The workspace is cut or
// padded to exactly the rows layoutWorkspace gave it, so the footer is always the last line of the screen.
func (m *Model) frame(workspace string) string {
	lines, back := m.headerLayout()
	m.registerBack(back)
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
