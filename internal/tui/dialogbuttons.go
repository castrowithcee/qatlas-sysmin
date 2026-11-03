package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A modal dialog shows the answers of its key line as buttons too. The buttons are drawn from the key line
// itself, so a button always presses the key it names and no dialog states its answers twice.

// dialogButton is one answer of a dialog.
type dialogButton struct {
	act     action
	label   string
	kind    buttonKind
	focused bool
}

// warnMark leads the heading of a question in a modal.
const warnMark = "⚠"

// buttonGap separates two buttons of a row.
const buttonGap = "  "

// dialogButtons reads the answers out of a key line: every answer whose key a click can press, by the first
// key of its group. A group that goes back is a plain button; with danger the answers of y and d are drawn
// as the dangerous kind. The answer of enter is the focused one, so the default shows without colour too; a
// question that enter does not answer has none.
func (m *Model) dialogButtons(keys []kittui.Hint, danger bool, labels map[action]string) []dialogButton {
	var buttons []dialogButton
	seen := map[action]bool{}
	for _, g := range m.hintGroups(keys, kittui.InputNavigation) {
		if g.Kind != kittui.HintAnswer || len(g.Keys) == 0 {
			continue
		}
		act := g.Keys[0].Action
		if _, ok := m.keyByName(m.firstKeyLabel(act)); !ok || seen[act] {
			continue
		}
		seen[act] = true
		label := labels[act]
		if label == "" {
			label = buttonLabel(g.Label)
		}
		if label == "" {
			label = g.Keys[0].Keys
		}
		groupHas := func(a action) bool {
			return slices.ContainsFunc(g.Keys, func(k kittui.HintKey) bool { return k.Action == a })
		}
		kind := buttonPrimary
		switch {
		case groupHas(actBack):
			kind = buttonNormal
		case danger && (act == actYes || act == actDiscard):
			kind = buttonDanger
		}
		buttons = append(buttons, dialogButton{act: act, label: label, kind: kind, focused: groupHas(actEnter)})
	}
	return buttons
}

// buttonLabel is the label of an answer as a button label: "cancel, nothing saved" gives "Cancel".
func buttonLabel(label string) string {
	label, _, _ = strings.Cut(label, ",")
	label = strings.TrimSpace(truncateCells(label, 24))
	if label == "" {
		return ""
	}
	r := []rune(label)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// buttonRows draws the buttons in rows that fit the dialog and makes each of them clickable: the rows are
// logged like footer lines, with the key of every button as its span.
func (m *Model) buttonRows(buttons []dialogButton) string {
	width := m.usable(0)
	var rows []string
	var line string
	var spans []footerSpan
	flush := func() {
		if line == "" {
			return
		}
		rows = append(rows, line)
		if m.footerLog != nil {
			*m.footerLog = append(*m.footerLog, footerLine{plain: ansi.Strip(line), spans: spans})
		}
		line, spans = "", nil
	}
	x := 0
	for _, b := range buttons {
		drawn := button(m.ui, b.kind, b.label, b.focused)
		w := lipgloss.Width(drawn)
		if x > 0 && x+len(buttonGap)+w > width {
			flush()
			x = 0
		}
		if x > 0 {
			line += buttonGap
			x += len(buttonGap)
		}
		spans = append(spans, footerSpan{x, x + w, b.act})
		line += drawn
		x += w
	}
	flush()
	return strings.Join(rows, "\n")
}

// withButtons is the text of a modal dialog up to its key line, then its buttons and the key line.
func (m *Model) withButtons(text string, keys []kittui.Hint, danger bool, labels map[action]string) string {
	rows := m.buttonRows(m.dialogButtons(keys, danger, labels))
	if rows != "" {
		text += "\n" + rows + "\n"
	}
	return text + m.keyHint(keys)
}
