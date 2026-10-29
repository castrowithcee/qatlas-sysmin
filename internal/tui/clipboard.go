package tui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// The copy key hands text to the terminal as an OSC 52 sequence, which also reaches the local clipboard over
// SSH. The terminal never confirms it, so the editor only says that it handed the text over. What is handed
// over is built from the same safe text the screen draws, never from the raw data behind it.

// copySource is where the copy key takes its text from right now, or nil when there is nothing to copy: the
// name of the marked row of the list. The text is built only when the key is pressed.
func (m *Model) copySource() func() string {
	if m.screen == screenArea && m.dd == nil && !m.typing() {
		if p := m.currentPage(); p != nil {
			// Only the name of the marked row: a credential's value is not in the interface to copy. A log entry
			// has no name, so its row is copied as drawn.
			if name, ok := p.current(); ok {
				if m.area == areaActivity && m.act.sub == actLog {
					return func() string { return strings.Join(p.rows[name], " ") }
				}
				return func() string { return name }
			}
		}
		return nil
	}
	if m.screen != screenList || m.list.editing || m.dd != nil {
		return nil
	}
	name, ok := m.selected()
	if !ok {
		return nil
	}
	return func() string { return name }
}

// copyKeyShown reports whether a footer names the copy key: where it copies something.
func (m *Model) copyKeyShown() bool { return m.helpReachable() && m.copySource() != nil }

// withCopyKey adds the copy group to a key line, ahead of the groups every screen ends with.
func withCopyKey(text string) string {
	if strings.Contains(text, "{copy}") {
		return text
	}
	at := -1
	for _, marker := range []string{"{command} cmds", "{help} help"} {
		if i := strings.Index(text, marker); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	if at < 0 {
		return text + " · {copy} copy"
	}
	return text[:at] + "{copy} copy · " + text[at:]
}

// copyCurrent hands the text of the copy source to the terminal and says so; it is nil where there is
// nothing to copy.
func (m *Model) copyCurrent() tea.Cmd {
	source := m.copySource()
	if source == nil {
		return nil
	}
	text := m.copyClean(source())
	if text == "" {
		return nil
	}
	m.clearMessages()
	m.status = tr(tCopyHandedOver)
	return tea.SetClipboard(text)
}

// copyClean makes text safe to hand over: no escape sequence, no registered secret, no invalid UTF-8 and no
// control character but the line breaks. Unlike logText it never cuts, so a long text is copied whole.
func (m *Model) copyClean(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\t", " ")
	text = ansi.Strip(text)
	if m.redactor != nil {
		text = m.redactor.Apply(text)
	}
	text = strings.ToValidUTF8(text, "�")
	text = strings.Map(func(r rune) rune {
		if r != '\n' && (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) {
			return '�'
		}
		return r
	}, text)
	return strings.TrimSpace(text)
}
