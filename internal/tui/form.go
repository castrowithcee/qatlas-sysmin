package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// A form is a short overlay of text fields and choices. A field with a known set of values is a choice that
// the left and right keys step through, never free text. A row that holds a secret is masked and read with
// raw: only the host user forms have one, and nothing of it is ever drawn or kept past the form.

// formRow is one row of a form: a text field, or a choice among values.
type formRow struct {
	// key names the field of the service the row stands for; problems of that field are drawn under it.
	key, label string
	choice     bool
	// values are what a choice stands for, shown what is drawn for each; shown may be nil.
	values, shown []string
	sel           int
	in            textinput.Model
	problem       string
	// hidden rows are skipped by the focus and not drawn.
	hidden bool
	// multi makes the choice a set: left and right move through the values, space ticks the one under the
	// cursor sel. ticked has one entry per value.
	multi  bool
	ticked []bool
	// pick makes the choice a set chosen in a list over the form (see pickRow); ticked and values work as for
	// multi, and notes, when set, has one short word per value.
	pick  bool
	notes []string
}

// formDlg is the form over the screen, drawn as the prompt of screenPrompt.
type formDlg struct {
	title, hint string
	rows        []formRow
	focus       int
	// notice is a problem that belongs to no row.
	notice string
	// submit runs on the save key with the form already left; changed runs after a choice took another value.
	submit  func(m *Model, f *formDlg) tea.Cmd
	changed func(m *Model, f *formDlg, row int)
	// saveText is the footer text of the save key, such as "{save} save".
	saveText string
	// enter, when set, runs first on the enter key; true says it took the key.
	enter func(m *Model, f *formDlg, row int) bool
	// pick is the list a pick row opened over the form; hits are the click targets of what was drawn last.
	pick *pickDlg
	hits []textHit
}

func textRow(key, label, value string) formRow {
	return formRow{key: key, label: label, in: keyInput(value)}
}

func choiceRow(key, label string, values, shown []string, sel int) formRow {
	return formRow{key: key, label: label, choice: true, values: values, shown: shown, sel: sel}
}

func maskedInput() textinput.Model {
	in := newTextInput()
	in.Prompt = ""
	in.EchoMode = textinput.EchoPassword
	return in
}

// maskedRow is a text row whose text is drawn as dots.
func maskedRow(key, label string) formRow {
	r := formRow{key: key, label: label, in: maskedInput()}
	return r
}

// multiRow is a choice of any number of values.
func multiRow(key, label string, values []string) formRow {
	return formRow{key: key, label: label, choice: true, multi: true, values: values, ticked: make([]bool, len(values))}
}

// raw is the text of a text row as typed, blanks included.
func (r *formRow) raw() string { return r.in.Value() }

// picked are the ticked values of a multi row, in order.
func (r *formRow) picked() []string {
	var out []string
	for i, on := range r.ticked {
		if on && i < len(r.values) {
			out = append(out, r.values[i])
		}
	}
	return out
}

// multiText is how a multi row is drawn: the value under the cursor with its box, and how many are ticked.
func (r *formRow) multiText() string {
	if len(r.values) == 0 {
		return tr(tHostFormNoGroups)
	}
	return tickBox(r.ticked[r.sel]) + " " + r.values[r.sel] + "  (" + tr(tHostFormTicked, len(r.picked()), len(r.values)) + ")"
}

// value is the text of a text row, or the value a choice stands for.
func (r *formRow) value() string {
	if r.choice {
		if r.sel < 0 || r.sel >= len(r.values) {
			return ""
		}
		return r.values[r.sel]
	}
	return strings.TrimSpace(r.in.Value())
}

func (r *formRow) shownValue() string {
	if r.sel >= 0 && r.sel < len(r.shown) {
		return r.shown[r.sel]
	}
	return r.value()
}

func (f *formDlg) row(key string) *formRow {
	for i := range f.rows {
		if f.rows[i].key == key {
			return &f.rows[i]
		}
	}
	return nil
}

// focusRow moves the focus to row i.
func (f *formDlg) focusRow(i int) {
	for j := range f.rows {
		f.rows[j].in.Blur()
	}
	f.focus = i
	if !f.rows[i].choice {
		f.rows[i].in.Focus()
	}
}

// step moves the focus by one visible row, round the ends.
func (f *formDlg) step(by int) {
	n := len(f.rows)
	for i := 1; i <= n; i++ {
		j := ((f.focus+by*i)%n + n) % n
		if !f.rows[j].hidden {
			f.focusRow(j)
			return
		}
	}
}

// wheelRow moves the focus by one visible row and stops at the first and the last.
func (f *formDlg) wheelRow(up bool) {
	by := 1
	if up {
		by = -1
	}
	for j := f.focus + by; j >= 0 && j < len(f.rows); j += by {
		if !f.rows[j].hidden {
			f.focusRow(j)
			return
		}
	}
}

// hitFormWheel makes the cells of the form that is open take the wheel, with
// the same precedence prompt gives them.
func (m *Model) hitFormWheel(x0, y0, x1, y1 int) {
	var scroll func(up bool)
	switch {
	case m.screen != screenPrompt:
		return
	case m.form != nil && m.form.pick != nil:
		scroll = m.form.pick.wheel
	case m.form != nil:
		scroll = m.form.wheelRow
	default:
		return
	}
	m.addWheel(x0, y0, x1, y1, func(up bool) tea.Cmd { scroll(up); return nil })
}

// openForm shows f over the screen with its first visible row focused.
func (m *Model) openForm(f *formDlg) {
	m.clearMessages()
	m.form, m.screen = f, screenPrompt
	first := 0
	for first < len(f.rows)-1 && f.rows[first].hidden {
		first++
	}
	f.focusRow(first)
}

// closeForm leaves the form.
func (m *Model) closeForm() {
	m.form = nil
	m.screen = m.home
}

// updateForm handles one key of the form.
func (m *Model) updateForm(key tea.KeyPressMsg) tea.Cmd {
	f := m.form
	f.notice = ""
	if f.pick != nil {
		return m.pickKey(f, key)
	}
	r := &f.rows[f.focus]
	switch {
	case keyIs(key, actBack):
		m.closeForm()
		m.status = tr(tCancelled)
		return nil
	case keyIs(key, actSave):
		return f.submit(m, f)
	case keyIs(key, actEnter) && f.enter != nil && f.enter(m, f, f.focus):
		return nil
	case keyIs(key, actEnter) && r.pick && len(r.values) > 0:
		m.openPick(f, f.focus)
		return nil
	case keyIs(key, actEnter), keyIs(key, actNext), keyIs(key, actDown):
		f.step(1)
		return nil
	case keyIs(key, actPrev), keyIs(key, actUp):
		f.step(-1)
		return nil
	}
	if r.pick {
		return nil
	}
	if r.multi {
		switch {
		case keyIs(key, actSpace) && len(r.values) > 0:
			r.ticked[r.sel] = !r.ticked[r.sel]
		case keyIs(key, actRight) && len(r.values) > 0:
			r.sel = (r.sel + 1) % len(r.values)
		case keyIs(key, actLeft) && len(r.values) > 0:
			r.sel = (r.sel + len(r.values) - 1) % len(r.values)
		default:
			return nil
		}
		r.problem = ""
		if f.changed != nil {
			f.changed(m, f, f.focus)
		}
		return nil
	}
	if r.choice {
		by := 0
		switch {
		case keyIs(key, actRight), keyIs(key, actSpace):
			by = 1
		case keyIs(key, actLeft):
			by = -1
		}
		if by != 0 && len(r.values) > 0 {
			r.sel = (r.sel + by + len(r.values)) % len(r.values)
			r.problem = ""
			if f.changed != nil {
				f.changed(m, f, f.focus)
			}
		}
		return nil
	}
	r.problem = ""
	var cmd tea.Cmd
	r.in, cmd = r.in.Update(inputMsg(key))
	return cmd
}

// formDialog is the form as the dialog the modal draws.
func (m *Model) formDialog() promptDialog {
	f := m.form
	if f.pick != nil {
		return m.pickDialog(f)
	}
	f.hits = nil
	width := m.usable(0)
	var labels []string
	for _, r := range f.rows {
		if !r.hidden {
			labels = append(labels, r.label)
		}
	}
	lw := labelWidth(labels)
	var rows []string
	for i, r := range f.rows {
		if r.hidden {
			continue
		}
		prefix := barMarker(f.focus == i) + padLine(r.label+":", lw+1) + " "
		room := max(width-lipgloss.Width(prefix)-1, 1)
		if r.choice {
			shown := r.shownValue()
			if r.multi {
				shown = r.multiText()
			}
			if r.pick {
				shown = r.pickText()
			}
			text := truncateCells(shown, max(room-6, 1))
			if f.focus == i && !r.pick {
				text = "< " + text + " >"
			}
			rows = append(rows, prefix+fieldStyle(f.focus == i).Render(" "+text+" "))
			if r.pick {
				row := i
				f.hits = append(f.hits, textHit{plain: ansi.Strip(rows[len(rows)-1]), act: func() tea.Cmd {
					if m.form == f && f.pick == nil && len(f.rows[row].values) > 0 {
						f.focusRow(row)
						m.openPick(f, row)
					}
					return nil
				}})
			}
		} else {
			rows = append(rows, prefix+inputView(fitInput(r.in, room)))
		}
		if r.problem != "" {
			rows = append(rows, "    "+styles.fail.Render(truncateCells(warnMark+" "+r.problem, max(width-5, 1))))
		}
	}
	keys := "enter next · tab next · " + f.saveText + " · {back} cancel"
	if f.rows[f.focus].pick {
		keys = "enter choose · tab next · " + f.saveText + " · {back} cancel"
	} else if f.rows[f.focus].multi {
		keys = "←/→ choose · space tick · tab next · " + f.saveText + " · {back} cancel"
	} else if f.rows[f.focus].choice {
		keys = "←/→ choose · tab next · " + f.saveText + " · {back} cancel"
	}
	d := promptDialog{title: f.title, input: strings.Join(rows, "\n"), hint: f.hint, keys: keys}
	if f.notice != "" {
		d.notice = styles.fail.Render(warnMark + " " + f.notice)
	}
	return d
}
