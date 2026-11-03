package tui

import (
	"maps"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// A pick row is a form row for a long multi-selection: the form shows a short summary of what is chosen, and
// enter or a click opens a list of all values with a filter, over the form. The choice only reaches the row
// when the list is left with enter; esc drops what was changed in it.

const pickSummaryNames = 2

// pickRow is a form row that chooses any number of values from a list of any length. notes, when set, has
// one entry per value: a short dimmed word behind it, such as a marker for values that need care.
func pickRow(key, label string, values, notes []string) formRow {
	return formRow{key: key, label: label, choice: true, pick: true, values: values, notes: notes, ticked: make([]bool, len(values))}
}

// pickText is how a pick row is drawn: the first chosen values and how many more there are.
func (r *formRow) pickText() string {
	if len(r.values) == 0 {
		return tr(tPickNothing)
	}
	chosen := r.picked()
	if len(chosen) == 0 {
		return tr(tPickNone)
	}
	text := strings.Join(chosen[:min(len(chosen), pickSummaryNames)], ", ")
	if more := len(chosen) - pickSummaryNames; more > 0 {
		text += " " + tr(tPickMore, more)
	}
	return text
}

// textHit is a click target found by the text of the line it stands in.
type textHit struct {
	plain string
	act   func() tea.Cmd
}

// pickDlg is the list over a form that a pick row opened.
type pickDlg struct {
	row    int
	list   filterList
	ticked map[string]bool
	notes  map[string]string
}

// openPick shows the list of the pick row i, the chosen values at the top.
func (m *Model) openPick(f *formDlg, i int) {
	r := &f.rows[i]
	pk := &pickDlg{row: i, list: newFilterList(func(v string) string { return v }), ticked: map[string]bool{}, notes: map[string]string{}}
	var first, rest []string
	for j, v := range r.values {
		if j < len(r.notes) && r.notes[j] != "" {
			pk.notes[v] = r.notes[j]
		}
		if r.ticked[j] {
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

// pickToggle ticks or unticks the value under the cursor.
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
	case keyIs(key, actBack):
		f.pick = nil
	case keyIs(key, actEnter):
		r := &f.rows[pk.row]
		for i, v := range r.values {
			r.ticked[i] = pk.ticked[v]
		}
		r.problem = ""
		f.pick = nil
		if f.changed != nil {
			f.changed(m, f, pk.row)
		}
	case keyIs(key, actSpace):
		pk.toggle()
	case keyIs(key, actUp):
		l.move(-1)
	case keyIs(key, actDown):
		l.move(1)
	case keyIs(key, actPageUp), keyIs(key, actPageDown):
		l.page(key, 0, pickerRows)
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
	f.hits = nil
	l.input.Placeholder = tr(tLogSearchPlaceholder)
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
			lines = append(lines, styles.title.Render("  > "+plain))
		} else {
			text := colourTick(plain, styles.cell)
			if note != "" {
				text = strings.TrimSuffix(text, note) + styles.hint.Render(note)
			}
			lines = append(lines, "    "+text)
		}
		cursor := i
		f.hits = append(f.hits, textHit{plain: ansi.Strip(lines[len(lines)-1]), act: func() tea.Cmd {
			if f.pick == pk {
				l.cursor = cursor
				pk.toggle()
			}
			return nil
		}})
	}
	if len(l.matches) == 0 {
		lines = append(lines, styles.hint.Render("    "+tr(tPickNoMatch)))
	} else {
		lines = append(lines, styles.hint.Render("    "+tr(tPickPosition, l.cursor+1, len(l.matches))))
	}
	return promptDialog{title: f.rows[pk.row].label, input: strings.Join(lines, "\n"),
		keys: "↑/↓ move · space tick · enter keep · {back} discard"}
}

// tickText is the plain box of a ticked or unticked entry.
func tickText(ticked bool) string {
	if ticked {
		return "[x]"
	}
	return "[ ]"
}

// hitFormLines makes the click targets of the open form or its list work: each is found among lines, which
// stand from screen row y0 on, and takes the cells [x0, x1) of its line.
func (m *Model) hitFormLines(lines []string, y0, x0, x1 int) {
	if m.screen != screenPrompt || m.form == nil {
		return
	}
	used := make([]bool, len(lines))
	for _, h := range m.form.hits {
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

// The texts of the pick row and its list.
const (
	tPickNone    textID = "PickNone"
	tPickNothing textID = "PickNothing"
	tPickMore    textID = "PickMore"
)

func init() {
	maps.Copy(textEN, map[textID]string{
		tPickNone:                  "none",
		tPickNothing:               "nothing to choose",
		tPickMore:                  "+%d",
		footerID("{back} discard"): "{back} discard",
	})
	maps.Copy(textDE, map[textID]string{
		tPickNone:                  "keine",
		tPickNothing:               "nichts zur Auswahl",
		tPickMore:                  "+%d",
		footerID("{back} discard"): "{back} verwerfen",
	})
}
