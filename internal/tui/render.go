package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
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
	return m.indentedWith(m.ui.st(kittui.StyleHint), text)
}

func (m *Model) indentedWith(style lipgloss.Style, text string) string {
	indent, width := m.fit(hintIndent)
	lines := strings.Split(render(style, width, text), "\n")
	for i, l := range lines {
		lines[i] = indent + l
	}
	return strings.Join(lines, "\n")
}

// keyHint hands the key line of a screen to the footer of the frame. A screen drawn only as the backdrop of a
// dialog leaves the footer as it was.
func (m *Model) keyHint(keys []kittui.Hint) {
	if m.backdrop {
		return
	}
	if m.copyKeyShown() {
		keys = withCopyKey(keys)
	}
	m.screenKeys = keys
}

// keepStyle makes a finished line keep style across the resets of its own styles.
func keepStyle(text string, style lipgloss.Style) string {
	if !strings.Contains(text, "\x1b[") {
		return text
	}
	set := style.Render("x")
	set = set[:strings.Index(set, "x")]
	if set == "" {
		return text
	}
	return afterResets(text, set)
}

// afterResets puts set behind every full reset of text, in both spellings: Lip Gloss writes the short one,
// the painting of the screen the long one.
func afterResets(text, set string) string {
	return strings.NewReplacer("\x1b[0m", "\x1b[0m"+set, "\x1b[m", "\x1b[m"+set).Replace(text)
}
