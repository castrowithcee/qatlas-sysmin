package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A pick list is the editor of a list field for a long multi-selection: the form shows a short summary of what
// is chosen, and enter or a click opens a list of all values with a filter, over the form. The choice only
// reaches the field when the list is left with enter; esc drops what was changed in it.

const pickSummaryNames = 2

// pickList is the values of a list field and which of them are ticked. notes, when set, has one entry per
// value: a short dimmed word behind it, such as a marker for values that need care.
type pickList struct {
	values, notes []string
	ticked        []bool
}

func newPickList(values, notes []string) *pickList {
	return &pickList{values: values, notes: notes, ticked: make([]bool, len(values))}
}

// picked are the ticked values, in order.
func (l *pickList) picked() []string {
	var out []string
	for i, on := range l.ticked {
		if on {
			out = append(out, l.values[i])
		}
	}
	return out
}

// summary is what the field shows: the first chosen values and how many more there are, empty for none.
func (l *pickList) summary(ui *uiContext) string {
	if len(l.values) == 0 {
		return ui.tr(tPickNothing)
	}
	chosen := l.picked()
	text := strings.Join(chosen[:min(len(chosen), pickSummaryNames)], ", ")
	if more := len(chosen) - pickSummaryNames; more > 0 {
		text += " " + ui.tr(tPickMore, more)
	}
	return text
}

// pickField adds a list field with its pick list to the form.
func (f *formDlg) pickField(id kittui.FieldID, label textID, values, notes []string) kittui.FormField {
	if f.lists == nil {
		f.lists = map[kittui.FieldID]*pickList{}
	}
	f.lists[id] = newPickList(values, notes)
	return listField(id, label)
}

// picked are the ticked values of the list field id.
func (f *formDlg) picked(id kittui.FieldID) []string {
	if l := f.lists[id]; l != nil {
		return l.picked()
	}
	return nil
}

// textHit is a click target found by the text of the line it stands in.
type textHit struct {
	plain string
	act   func() tea.Cmd
}

// pickDlg is the list over a form that a list field opened.
type pickDlg struct {
	field  kittui.FieldID
	list   filterList
	ticked map[string]bool
	notes  map[string]string
	hits   []textHit
}

// openPick shows the list of the list field id, the chosen values at the top.
func (m *Model) openPick(f *formDlg, id kittui.FieldID) {
	l := f.lists[id]
	pk := &pickDlg{field: id, list: newFilterList(func(v string) string { return v }), ticked: map[string]bool{}, notes: map[string]string{}}
	var first, rest []string
	for j, v := range l.values {
		if j < len(l.notes) && l.notes[j] != "" {
			pk.notes[v] = l.notes[j]
		}
		if l.ticked[j] {
			pk.ticked[v] = true
			first = append(first, v)
		} else {
			rest = append(rest, v)
		}
	}
	pk.list.reset(append(first, rest...))
	pk.list.startFilter()
	f.pick = pk
}

// toggle ticks or unticks the value under the cursor.
func (pk *pickDlg) toggle() {
	if name, ok := pk.list.selected(); ok {
		pk.ticked[name] = !pk.ticked[name]
	}
}

// wheel is a notch of the wheel over the list: one step of the cursor, stopping at both ends.
func (pk *pickDlg) wheel(up bool) {
	by := 1
	if up {
		by = -1
	}
	pk.list.move(by)
}

// pickKey handles a key of the open list. Typing filters; space ticks.
func (m *Model) pickKey(f *formDlg, key tea.KeyPressMsg) tea.Cmd {
	pk := f.pick
	l := &pk.list
	switch {
	case m.keyIs(key, actBack):
		f.pick = nil
	case m.keyIs(key, actEnter):
		list := f.lists[pk.field]
		for i, v := range list.values {
			list.ticked[i] = pk.ticked[v]
		}
		f.pick = nil
		m.setListValue(f, pk.field, list.summary(m.ui))
	case m.keyIs(key, actSpace):
		pk.toggle()
	case m.keyIs(key, actUp):
		l.move(-1)
	case m.keyIs(key, actDown):
		l.move(1)
	case m.keyIs(key, actPageUp), m.keyIs(key, actPageDown):
		l.page(m.ui.keys, key, 0, pickerRows)
	default:
		return l.updateFilter(key)
	}
	return nil
}

const (
	// pickerRows is how many rows of a pick list show at once.
	pickerRows = 8
	// pickerField is the width of the search field and the base of the row width.
	pickerField = 40
)

// pickDialog is the list as the dialog the modal draws, and registers its rows as click targets.
func (m *Model) pickDialog(f *formDlg) promptDialog {
	pk := f.pick
	l := &pk.list
	pk.hits = nil
	l.input.Placeholder = m.tr(tLogSearchPlaceholder)
	lines := []string{"  / " + inputView(fitInput(l.input, pickerField))}
	start, end := l.window(pickerRows, func(int) int { return 1 })
	for i := start; i < end; i++ {
		name := l.matches[i]
		note := ""
		if n := pk.notes[name]; n != "" {
			note = "  " + n
		}
		plain := truncateCells(tickText(pk.ticked[name])+" "+safeText(name)+note, pickerField+8)
		if i == l.cursor {
			lines = append(lines, m.ui.st(kittui.StyleTitle).Render("  > "+plain))
		} else {
			text := colourTick(m.ui, plain, m.ui.st(kittui.StyleText))
			if note != "" {
				text = strings.TrimSuffix(text, note) + m.ui.st(kittui.StyleHint).Render(note)
			}
			lines = append(lines, "    "+text)
		}
		cursor := i
		pk.hits = append(pk.hits, textHit{plain: ansi.Strip(lines[len(lines)-1]), act: func() tea.Cmd {
			if f.pick == pk {
				l.cursor = cursor
				pk.toggle()
			}
			return nil
		}})
	}
	if len(l.matches) == 0 {
		lines = append(lines, m.ui.st(kittui.StyleHint).Render("    "+m.tr(tPickNoMatch)))
	} else {
		lines = append(lines, m.ui.st(kittui.StyleHint).Render("    "+m.tr(tPickPosition, l.cursor+1, len(l.matches))))
	}
	label := ""
	for _, field := range f.fields {
		if field.ID == pk.field {
			label = m.tr(field.Label)
		}
	}
	return promptDialog{title: label, input: strings.Join(lines, "\n"),
		keys: keyLine(navHint(tHintMove, actUp, actDown), hint(tHintTick, actSpace), answer(tHintKeep, actEnter),
			answer(tHintDiscard, actBack))}
}

// tickText is the plain box of a ticked or unticked entry.
func tickText(ticked bool) string {
	if ticked {
		return "[x]"
	}
	return "[ ]"
}

// hitPickLines makes the rows of the open pick list clickable: each is found among lines, which stand from
// screen row y0 on, and takes the cells [x0, x1) of its line.
func (m *Model) hitPickLines(lines []string, y0, x0, x1 int) {
	used := make([]bool, len(lines))
	for _, h := range m.form.pick.hits {
		for i, line := range lines {
			if used[i] || !strings.Contains(ansi.Strip(line), h.plain) {
				continue
			}
			used[i] = true
			m.addHit(x0, y0+i, x1, y0+i+1, h.act)
			break
		}
	}
}

// The texts of the pick list.
const (
	tPickNothing textID = "PickNothing"
	tPickMore    textID = "PickMore"
)

// pickTexts are the texts of the pick list, in English and German.
func pickTexts() (en, de map[textID]string) {
	en = map[textID]string{
		tPickNothing: "nothing to choose",
		tPickMore:    "+%d",
	}
	de = map[textID]string{
		tPickNothing: "nichts zur Auswahl",
		tPickMore:    "+%d",
	}
	return en, de
}
