package tui

import (
	"errors"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// A form is a short overlay of text fields and choices. A field with a known set of values is a choice that
// the left and right keys step through, never free text. Nothing typed into a form is a secret: secrets are
// entered in the credential form only.

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
}

// formDlg is the form over the screen, drawn as the prompt of screenPrompt.
type formDlg struct {
	title, hint string
	rows        []formRow
	focus       int
	// notice is a problem that belongs to no row.
	notice string
	// submit runs on the save key with the form already left; changed runs after a choice took another value;
	// refresh runs where a list a row offers arrived.
	submit  func(m *Model, f *formDlg) tea.Cmd
	changed func(m *Model, f *formDlg, row int)
	refresh func(m *Model, f *formDlg)
	// saveText is the footer text of the save key, such as "{save} save".
	saveText string
}

func textRow(key, label, value string) formRow {
	return formRow{key: key, label: label, in: keyInput(value)}
}

func choiceRow(key, label string, values, shown []string, sel int) formRow {
	return formRow{key: key, label: label, choice: true, values: values, shown: shown, sel: sel}
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

// setChoices replaces the values of a choice and keeps the one chosen where it is still offered.
func (r *formRow) setChoices(values, shown []string) {
	keep := r.value()
	r.values, r.shown, r.sel = values, shown, 0
	for i, v := range values {
		if v == keep {
			r.sel = i
		}
	}
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

// reopenForm shows a form again that a refusal of the service sent back, where nothing else is in the way.
func (m *Model) reopenForm(f *formDlg) {
	if m.screen != m.home || m.ov != nil || m.confirm != nil || m.pop != nil {
		return
	}
	m.fail = ""
	m.form, m.screen = f, screenPrompt
}

// updateForm handles one key of the form.
func (m *Model) updateForm(key tea.KeyPressMsg) tea.Cmd {
	f := m.form
	f.notice = ""
	r := &f.rows[f.focus]
	switch {
	case keyIs(key, actBack):
		m.closeForm()
		m.status = tr(tCancelled)
		return nil
	case keyIs(key, actSave):
		return f.submit(m, f)
	case keyIs(key, actEnter), keyIs(key, actNext), keyIs(key, actDown):
		f.step(1)
		return nil
	case keyIs(key, actPrev), keyIs(key, actUp):
		f.step(-1)
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
			text := truncateCells(r.shownValue(), max(room-6, 1))
			if f.focus == i {
				text = "< " + text + " >"
			}
			rows = append(rows, prefix+fieldStyle(f.focus == i).Render(" "+text+" "))
		} else {
			rows = append(rows, prefix+inputView(fitInput(r.in, room)))
		}
		if r.problem != "" {
			rows = append(rows, "    "+styles.fail.Render(truncateCells(warnMark+" "+r.problem, max(width-5, 1))))
		}
	}
	keys := "enter next · tab next · " + f.saveText + " · {back} cancel"
	if f.rows[f.focus].choice {
		keys = "←/→ choose · tab next · " + f.saveText + " · {back} cancel"
	}
	d := promptDialog{title: f.title, input: strings.Join(rows, "\n"), hint: f.hint, keys: keys}
	if f.notice != "" {
		d.notice = styles.fail.Render(warnMark + " " + f.notice)
	}
	return d
}

// fieldOf is the name of the field a problem of the service is about, without the place in its list: the
// last part of "channels[3].to[0]" is "to".
func fieldOf(field string) string {
	if strings.HasPrefix(field, "channels[") {
		if _, rest, ok := strings.Cut(field, "."); ok {
			field = rest
		}
	}
	if i := strings.Index(field, "["); i >= 0 {
		field = field[:i]
	}
	return field
}

// formProblems puts the problems a refused input names under their rows; one that fits no row goes to the
// notice. It reports whether the error carried any problem.
func (m *Model) formProblems(f *formDlg, err error) bool {
	var ce *control.Error
	if !errors.As(err, &ce) || len(ce.Problems) == 0 {
		return false
	}
	var rest []control.Problem
	for _, p := range ce.Problems {
		r := f.row(fieldOf(p.Field))
		if r == nil || r.hidden {
			rest = append(rest, p)
			continue
		}
		if r.problem == "" {
			r.problem = m.diffText(problemText([]control.Problem{p}))
		}
	}
	if len(rest) > 0 {
		f.notice = m.diffText(problemText(rest))
	}
	return true
}
