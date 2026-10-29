package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	// modalMinWidth and modalMinHeight are the smallest terminal that carries a modal over its background;
	// below them a question takes the whole workspace as before.
	modalMinWidth  = 60
	modalMinHeight = 20
	// modalMaxInner is the widest text of a modal.
	modalMaxInner = 60
)

// questionScreen reports whether the current screen is a question the modal shows.
func (m *Model) questionScreen() bool { return m.screen == screenConfirm && m.ask != nil }

// questionView is the text of the current question, without the notes that follow a full screen.
func (m *Model) questionView() string { return m.questionText(*m.ask) }

// screenBeneath is the screen a dialog stands over, and false for a screen that is none.
func (m *Model) screenBeneath(s screen) (screen, bool) {
	switch s {
	case screenConfirm, screenPrompt:
		return m.home, true
	}
	return s, false
}

// baseScreen is the screen the current dialog stands over, however many dialogs are stacked; it is the
// current screen itself when that is no dialog.
func (m *Model) baseScreen() screen {
	s := m.screen
	for range 4 {
		below, ok := m.screenBeneath(s)
		if !ok || below == s {
			return s
		}
		s = below
	}
	return s
}

// renderAs draws with the given screen and, when width is positive, workspace width, and then restores
// them, so a view drawn only as a background changes no state.
func (m *Model) renderAs(s screen, width int, render func()) {
	screen, w := m.screen, m.width
	defer func() { m.screen, m.width = screen, w }()
	m.screen = s
	if width > 0 {
		m.width = width
	}
	render()
}

// modalView is the current question or input dialog centred over its dimmed background, or false when the
// terminal is too small for that and the dialog takes the whole workspace.
func (m *Model) modalView() (string, bool) {
	if m.termWidth < modalMinWidth || m.termHeight < modalMinHeight {
		return "", false
	}
	inner := min(modalMaxInner, m.termWidth-8)
	var text string
	var footers []footerLine
	m.footerLog, m.inModal = &footers, true
	defer func() { m.footerLog, m.inModal = nil, false }()
	title := ""
	if m.questionScreen() {
		m.renderAs(m.screen, inner, func() { text = m.questionView() })
	} else {
		ok := false
		m.renderAs(m.screen, inner, func() {
			var d promptDialog
			if d, ok = m.prompt(); ok {
				text = m.wrapped(styles.title, d.title) + "\n\n" + m.promptText(d, false) + m.promptNotes()
			}
		})
		if !ok {
			return "", false
		}
	}
	m.footerLog, m.inModal = nil, false
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	room := m.termHeight - 4
	if m.questionScreen() {
		// A question leaves the top line in view, so it never hides where it was asked.
		room = m.termHeight - 5
	}
	if len(lines) > room {
		return "", false
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, inner, "")
	}
	dialog := panel(title, lines, inner, len(lines), panelOpts{focused: true})

	var background string
	m.renderAs(m.baseScreen(), 0, func() {
		m.backdrop = true
		defer func() { m.backdrop = false }()
		background = m.frame(m.workspaceView())
	})
	top, left := dialogOrigin(dialog, m.termWidth, m.termHeight)
	m.hitDialog(lines, footers, top, left)
	return overlay(background, dialog, m.termWidth, m.termHeight), true
}

// overlay draws dialog centred over background in a screen of width by height. The background is dimmed:
// stripped of its colours and drawn faint, so it stays readable and only the dialog stands out, and still
// does without colour, where its border marks the dialog.
func overlay(background string, dialog []string, width, height int) string {
	rows := strings.Split(background, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	rows = rows[:height]
	dialogWidth := 0
	for _, line := range dialog {
		dialogWidth = max(dialogWidth, ansi.StringWidth(line))
	}
	top, left := dialogOrigin(dialog, width, height)
	for i, row := range rows {
		plain := ansi.Strip(row)
		plain += strings.Repeat(" ", max(width-ansi.StringWidth(plain), 0))
		if i < top || i >= top+len(dialog) {
			rows[i] = dim(ansi.Cut(plain, 0, width))
			continue
		}
		rows[i] = dim(ansi.Cut(plain, 0, left)) + dialog[i-top] + dim(ansi.Cut(plain, left+dialogWidth, width))
	}
	return strings.Join(rows, "\n")
}

func dim(text string) string {
	if text == "" {
		return ""
	}
	return styles.hint.Render(text)
}

// question is the one shape of every yes/no and leave question: the question itself, one sentence on what
// follows from it, the detail lines the answer needs, and the key line as the last line. The modal and the
// full-screen fallback draw the same text.
type question struct {
	title       string
	consequence string
	// warn draws the consequence as a warning.
	warn bool
	// danger draws the answer that destroys something (y or d) as the dangerous button.
	danger bool
	// labels name buttons by their key where the key line's words do not fit a button.
	labels map[string]string
	// details are finished lines (they may span several rows) between the consequence and the keys.
	details []string
	keys    string
}

// questionHeading is the question of a modal with the warning mark at its left.
func (m *Model) questionHeading(title string) string {
	lines := strings.Split(render(styles.title, m.usable(2), title), "\n")
	for i, line := range lines {
		lead := "  "
		if i == 0 {
			lead = styles.warning.Render(warnMark) + " "
		}
		lines[i] = lead + line
	}
	return strings.Join(lines, "\n")
}

func (m *Model) questionText(q question) string {
	style := styles.hint
	if q.warn {
		style = styles.warning
	}
	var b strings.Builder
	if m.inModal || m.inPanel {
		b.WriteString(m.questionHeading(q.title) + "\n\n")
	} else {
		b.WriteString(m.wrapped(styles.title, q.title) + "\n\n")
	}
	b.WriteString(m.indentedWith(style, q.consequence) + "\n")
	for _, d := range q.details {
		b.WriteString(strings.TrimRight(d, "\n") + "\n")
	}
	if m.inModal {
		return m.withButtons(b.String(), q.keys, q.danger, q.labels)
	}
	b.WriteString(m.keyHint(q.keys))
	return b.String()
}
