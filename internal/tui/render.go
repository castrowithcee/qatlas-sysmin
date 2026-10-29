package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// boundView cuts plain text to the workspace.
func (m *Model) boundView(view string) string { return m.boundTo(view, m.width, m.height) }

// boundTo cuts view to width by height. Lines that already fit keep their styling; only a line that does
// not fit is cut, which the callers keep to plain text.
func (m *Model) boundTo(view string, width, height int) string {
	width, height = max(width, 1), max(height, 1)
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		if lipgloss.Width(lines[i]) > width {
			lines[i] = clipCells(lines[i], width)
		}
	}
	return strings.Join(lines, "\n")
}

// boundKeepingKeys is boundTo for a question: when it does not fit, the first line and the last lines stay,
// so the key line, which ends the question, is never the part that is cut.
func (m *Model) boundKeepingKeys(view string, width, height int) string {
	height = max(height, 1)
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = append(lines[:1], lines[len(lines)-height+1:]...)
	}
	return m.boundTo(strings.Join(lines, "\n"), width, height)
}

// truncateLeft keeps the end of text within width and marks the cut at its start.
func truncateLeft(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	if width <= 1 {
		return clipCells("…", max(width, 0))
	}
	runes := []rune(text)
	for i := range runes {
		if tail := string(runes[i:]); lipgloss.Width(tail) <= width-1 {
			return "…" + tail
		}
	}
	return "…"
}

// clipCells cuts text to width visible cells. Escape sequences count no cells and are never split; when
// the cut leaves a style open, a reset ends it. Text without escapes stays a true prefix of its input.
func clipCells(text string, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	parser := ansi.NewParser()
	state := byte(ansi.NormalState)
	used, styled := 0, false
	for rest := text; rest != ""; {
		seq, w, n, next := ansi.DecodeSequence(rest, state, parser)
		state = next
		if w == 0 && ansi.HasCsiPrefix(seq) && strings.HasSuffix(seq, "m") {
			// An SGR sequence: keep it whole; a bare reset leaves nothing open.
			b.WriteString(seq)
			styled = seq != "\x1b[m" && seq != "\x1b[0m"
		} else if w == 0 && strings.HasPrefix(seq, "\x1b") {
			b.WriteString(seq)
		} else {
			if used+w > width {
				if styled {
					b.WriteString("\x1b[m")
				}
				return b.String()
			}
			b.WriteString(seq)
			used += w
		}
		rest = rest[n:]
	}
	return b.String()
}

func truncateCells(text string, width int) string {
	if lipgloss.Width(text) <= width {
		return text
	}
	if width <= 0 {
		return ""
	}
	if width == 1 {
		return "…"
	}
	return clipCells(text, width-1) + "…"
}

func wrapCells(text string, width int) string {
	if width <= 0 {
		return ""
	}
	var lines []string
	for text != "" {
		line := clipCells(text, width)
		if line == "" {
			break
		}
		lines = append(lines, line)
		text = strings.TrimPrefix(text, line)
	}
	return strings.Join(lines, "\n")
}

func padLine(text string, width int) string {
	return text + strings.Repeat(" ", max(width-lipgloss.Width(text), 0))
}

// wrapped fits a message into the terminal instead of leaving it to be cut off at the right edge.
//
// The messages of the cascade carry the way out at their end: the file to chmod, the command to run, the
// variable to export. A truncated line loses exactly the part the reader needs, and it is the longest
// messages, the ones that explain the most, that get truncated. Wrapping keeps all of it on screen and
// needs nothing but the width the terminal already reports.
func (m *Model) wrapped(style lipgloss.Style, text string) string {
	return render(style, m.usable(0), text)
}

// render wraps text into width and styles it line by line. A line that fits is left exactly as it is,
// including a trailing blank that may be the cursor of an input. A line that does not is wrapped, and its
// pieces lose their trailing blanks: Lip Gloss pads them and keeps the space at a wrap point, which can make
// a piece one cell wider than asked for and push the frame out of line.
func render(style lipgloss.Style, width int, text string) string {
	width = max(width, 1)
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if lipgloss.Width(line) <= width {
			out = append(out, style.Render(line))
			continue
		}
		for _, piece := range strings.Split(lipgloss.NewStyle().Width(width).Render(line), "\n") {
			out = append(out, style.Render(strings.TrimRight(piece, " ")))
		}
	}
	return strings.Join(out, "\n")
}

// usable is the room left for text after a prefix of n cells.
//
// A terminal that has not reported its width yet is assumed to be the usual one; a terminal that reported a
// width narrower than the prefix still gets one cell to wrap into, rather than being drawn at the assumed
// width and spilling. Falling back to the assumed width because the real one is small would be the very
// truncation this is here to avoid.
func (m *Model) usable(prefix int) int {
	width := m.width
	if width <= 0 {
		width = defaultWidth
	}
	if width-prefix < 1 {
		return 1
	}
	return width - prefix
}

// fit returns the prefix to draw and the room left for the text beside it, so that the two together never
// exceed the terminal. A terminal narrower than the prefix keeps one cell for the text and loses part of
// the prefix: an indent that pushes text past the right edge is worse than a missing indent.
func (m *Model) fit(prefix string) (string, int) { return m.fitIn(m.usable(0), prefix) }

// fitIn is fit within the first room cells of the workspace.
func (m *Model) fitIn(room int, prefix string) (string, int) {
	if m.width > 0 {
		if room := room - 1; room < len(prefix) {
			prefix = prefix[:max(room, 0)]
		}
	}
	return prefix, max(room-len(prefix), 1)
}

// indented draws a hint under the row it belongs to and keeps its continuation lines there too, so a hint
// that needs two lines still reads as one hint rather than as a stray sentence at the left margin.
func (m *Model) indented(text string) string {
	return m.indentedWith(styles.hint, text)
}

func (m *Model) indentedWith(style lipgloss.Style, text string) string {
	indent, width := m.fit(hintIndent)
	lines := strings.Split(render(style, width, text), "\n")
	for i, l := range lines {
		lines[i] = indent + l
	}
	return strings.Join(lines, "\n")
}

// row draws one line of a list. A line that does not fit is continued under its own entry instead of being
// cut off at the right edge: the list is where the source of every secret role stands, and that is the part
// that falls off first.
func (m *Model) row(active bool, text string) string { return m.rowIn(m.usable(0), active, text) }

// rowIn is row within the first room cells of the workspace.
func (m *Model) rowIn(room int, active bool, text string) string {
	blank, width := m.fitIn(room, "  ")
	marker := blank
	if active && len(blank) == 2 {
		marker = "> "
	}
	style := lipgloss.NewStyle()
	if active {
		style = styles.active
	}
	lines := strings.Split(render(style, width, text), "\n")
	for i, l := range lines {
		prefix := blank
		if i == 0 {
			prefix = marker
		}
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// listRow draws one row of a table screen: a list or a picker. text is
// already cut to fit beside the marker: long cells are shortened before they ever reach here, never
// wrapped. The marked row is highlighted the full width of the row, behind a narrow accent mark, which is
// '>' where colour cannot show the bar, so NO_COLOR or an ASCII terminal still shows which one it is; every
// other row is plain.
func (m *Model) listRow(active bool, text string) string {
	return m.listRowIn(m.usable(0), active, text)
}

// listRowIn is listRow within the first room cells of the workspace.
func (m *Model) listRowIn(room int, active bool, text string) string {
	blank, width := m.fitIn(room, "  ")
	if !active {
		return blank + padLine(text, width)
	}
	body := padLine(text, width)
	if len(blank) != 2 {
		// No room for the mark: the bar alone shows the row.
		return styles.activeRow.Render(keepStyle(blank+body, styles.activeRow))
	}
	// The cells of the text may reset their own styles, so the bar is set again after each reset.
	return styles.selMark.Render(selectionMark()) + styles.activeRow.Render(keepStyle(" "+body, styles.activeRow))
}

// selectionMark leads the marked row: a narrow accent bar where colour shows it, and '>' where it does not,
// so the selection never depends on colour alone.
func selectionMark() string {
	if styles.palette.selection == "" || currentProfile() == profileNone {
		return ">"
	}
	return "▌"
}

// hint is the key line of a screen. Like every other prose line it is wrapped into the terminal instead of
// being cut off at its right edge.
func (m *Model) hint(text string) string { return "\n" + m.wrapped(styles.hint, text) }

// keyHint is the key line of a screen: groups of keys separated by " · ". It hands the line to the footer of
// the frame (see footerLine) and returns nothing for the workspace; only while a modal dialog is drawn does
// it return the line itself (see dialogKeyHint).
func (m *Model) keyHint(text string) string {
	if m.footerLog == nil {
		// A screen's keys stand in the footer of the frame, not in the workspace. A dialog under a backdrop
		// leaves the footer as it was.
		if !m.backdrop {
			if m.copyKeyShown() {
				text = withCopyKey(text)
			}
			m.screenKeys = text
		}
		return ""
	}
	return m.dialogKeyHint(text)
}

// dialogKeyHint is the key line of a modal dialog: groups never split across a wrap, only between groups,
// and within a group the key (its first word) is bold while the action after it keeps the plain hint style.
// A single group too wide for the terminal is cut with "...". It stands in the dialog itself, where its
// keys are buttons.
func (m *Model) dialogKeyHint(text string) string {
	return "\n" + strings.Join(m.keyHintLines(text), "\n")
}

// keyHintLines is the footer text broken into lines of whole groups.
func (m *Model) keyHintLines(text string) []string { return m.keyHintLinesWith(text, keyHintGroup) }

// keyHintLinesWith is keyHintLines with each group drawn by style.
func (m *Model) keyHintLinesWith(text string, style func(group string) string) []string {
	text = footerKeys(text)
	width := m.usable(0)
	var lines []string
	var line string
	lineWidth := 0
	var current []footerSpan
	var spanned [][]footerSpan
	for _, group := range strings.Split(text, " · ") {
		group = truncateCells(group, width)
		gw := lipgloss.Width(group)
		if lineWidth > 0 && lineWidth+lipgloss.Width(" · ")+gw > width {
			lines, spanned = append(lines, line), append(spanned, current)
			line, lineWidth, current = "", 0, nil
		}
		if lineWidth > 0 {
			line += styles.hint.Render(" · ")
			lineWidth += lipgloss.Width(" · ")
		}
		current = append(current, footerSpan{lineWidth, lineWidth + gw, footerKey(group)})
		line += style(group)
		lineWidth += gw
	}
	if lineWidth > 0 || len(lines) == 0 {
		lines, spanned = append(lines, line), append(spanned, current)
	}
	if m.footerLog != nil {
		for i, l := range lines {
			*m.footerLog = append(*m.footerLog, footerLine{plain: ansi.Strip(l), spans: spanned[i]})
		}
	}
	return lines
}

// keyHintGroup styles one "key action" hint: the key (its first word) bold, the action after it faint.
func keyHintGroup(group string) string {
	key, action, ok := strings.Cut(group, " ")
	if !ok {
		return styles.key.Render(key)
	}
	return styles.key.Render(key) + styles.hint.Render(" "+action)
}
