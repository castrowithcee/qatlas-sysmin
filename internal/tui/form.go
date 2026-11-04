package tui

import (
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A host form is a kit form in a short overlay. The form holds every field it can show; a field that the
// current choice leaves out is hidden, and the kit form is built anew without it, keeping what was entered in
// the other fields. A masked field holds a secret: it is never kept across a new build and never drawn, and
// it is emptied when the form closes. A list field shows a summary; its editor is a pick list or the key
// editor over the form.

// formDlg is the form over the screen, drawn by the kit as a modal; an editor open over it is drawn as the
// prompt of screenPrompt.
type formDlg struct {
	title, hint string
	// fields are every field the form can show, in their order; hidden are those left out now.
	fields []kittui.FormField
	hidden map[kittui.FieldID]bool
	kit    *kittui.Form
	// kept holds the values of the fields across a new build, masked fields never.
	kept map[kittui.FieldID]kittui.FormValue
	// rebuilt says a choice changed the fields, so the form holds a change Changed of the new build misses.
	rebuilt bool
	// submit runs on the save key and adds the problems it finds; changed runs after a field took another value.
	submit  func(m *Model, f *formDlg) tea.Cmd
	changed func(m *Model, f *formDlg, id kittui.FieldID)
	// save is the hint text of the save key, tHintSave or tHintContinue.
	save     textID
	problems []kittui.FormProblem
	// lists are the pick lists of the list fields; keys the key list of the key field.
	lists map[kittui.FieldID]*pickList
	keys  *keyList
	// pick and keyEd are the editor open over the form.
	pick  *pickDlg
	keyEd *keyEditor
	// lead is the lead the kit form shows now, the hint as last handed to it.
	lead string
}

func textField(id kittui.FieldID, label textID) kittui.FormField {
	return kittui.FormField{ID: id, Kind: kittui.FieldText, Label: label}
}

func maskedField(id kittui.FieldID, label textID) kittui.FormField {
	return kittui.FormField{ID: id, Kind: kittui.FieldMasked, Label: label}
}

// choiceField is a choice among values, on the value at sel.
func choiceField(id kittui.FieldID, label textID, choices []kittui.Choice, sel int) kittui.FormField {
	f := kittui.FormField{ID: id, Kind: kittui.FieldChoice, Label: label, Choices: choices}
	if sel >= 0 && sel < len(choices) {
		f.Value.Choice = choices[sel].ID
	}
	return f
}

// shownChoices are values with the texts drawn for them.
func shownChoices(values []string, shown []textID) []kittui.Choice {
	out := make([]kittui.Choice, len(values))
	for i, v := range values {
		out[i] = kittui.Choice{ID: kittui.ChoiceID(v), Label: shown[i]}
	}
	return out
}

// yesNo are the choices no and yes, in that order.
func yesNo() []kittui.Choice {
	return shownChoices([]string{hostNo, hostYes}, []textID{tHostNo, tHostYes})
}

func listField(id kittui.FieldID, label textID) kittui.FormField {
	return kittui.FormField{ID: id, Kind: kittui.FieldList, Label: label}
}

// summaryText makes a summary fit for a list field: one line without control characters.
func summaryText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// build makes the kit form of the fields not hidden, on the values entered so far and with the focus where it
// was.
func (f *formDlg) build(m *Model) error {
	focus := kittui.FieldID("")
	if f.kit != nil {
		f.keep()
		focus = f.kit.Focused()
		f.rebuilt = true
	}
	var shown []kittui.FormField
	for _, field := range f.fields {
		if f.hidden[field.ID] {
			continue
		}
		if v, ok := f.kept[field.ID]; ok {
			field.Value = v
		}
		if l := f.lists[field.ID]; l != nil {
			field.Value = kittui.FormValue{Text: summaryText(l.summary(m.ui))}
		}
		if field.ID == fieldKeys && f.keys != nil {
			field.Value = kittui.FormValue{Text: summaryText(f.keys.summary(m.ui))}
		}
		shown = append(shown, field)
	}
	title, lead := f.head()
	kit, err := m.newForm(title, lead, shown, nil, f.buttons())
	if err != nil {
		return err
	}
	f.kit, f.lead = kit, f.hint
	if focus != "" {
		kit.Focus(focus)
	}
	return nil
}

// buttons are the buttons below the fields, which tab reaches after the last field: the save key under the
// form's own word, and cancel.
func (f *formDlg) buttons() []kittui.FormButton {
	label := tBtnSave
	if f.save == tHintContinue {
		label = tBtnContinue
	}
	return []kittui.FormButton{{Action: actSave, Label: label, Primary: true}, {Action: actBack, Label: tBtnCancel}}
}

// keep takes the values of the form into kept, masked ones excepted.
func (f *formDlg) keep() {
	if f.kept == nil {
		f.kept = map[kittui.FieldID]kittui.FormValue{}
	}
	values := f.kit.Values()
	for _, field := range f.fields {
		if v, ok := values[field.ID]; ok && field.Kind != kittui.FieldMasked {
			f.kept[field.ID] = v
		}
	}
}

// show shows or hides the field id from the next build on.
func (f *formDlg) show(id kittui.FieldID, on bool) {
	if f.hidden == nil {
		f.hidden = map[kittui.FieldID]bool{}
	}
	f.hidden[id] = !on
}

// rearrange builds the form anew where the shown fields differ from the built ones.
func (f *formDlg) rearrange(m *Model) {
	built := f.kit.Values()
	for _, field := range f.fields {
		if _, in := built[field.ID]; in == f.hidden[field.ID] {
			if err := f.build(m); err != nil {
				m.fail = err.Error()
			}
			return
		}
	}
}

// value is the trimmed text of a field or the value its choice stands for; raw is the text as typed.
func (f *formDlg) value(id kittui.FieldID) string {
	v, ok := f.kit.Values()[id]
	if !ok {
		v = f.kept[id]
	}
	if v.Choice != "" {
		return string(v.Choice)
	}
	return strings.TrimSpace(v.Text)
}

func (f *formDlg) raw(id kittui.FieldID) string { return f.kit.Values()[id].Text }

// fail adds a problem under the field id; refuse adds one above the form.
func (f *formDlg) fail(id kittui.FieldID, text string) {
	f.problems = append(f.problems, kittui.FormProblem{Field: id, Cause: text})
}

func (f *formDlg) refuse(text string) { f.fail("", text) }

func (f *formDlg) failed() bool { return len(f.problems) > 0 }

// dirty reports input the form would lose if it closed now.
func (f *formDlg) dirty() bool { return f.rebuilt || f.kit.Changed() }

// wipe empties the masked fields.
func (f *formDlg) wipe() {
	for _, field := range f.fields {
		if field.Kind == kittui.FieldMasked {
			_ = f.kit.SetValue(field.ID, kittui.FormValue{})
		}
	}
}

// hitPickWheel makes the cells of the open pick list take the wheel.
func (m *Model) hitPickWheel(x0, y0, x1, y1 int) {
	f := m.form
	if m.screen != screenPrompt || f == nil || f.pick == nil {
		return
	}
	m.addWheel(x0, y0, x1, y1, kittui.ZoneForm, func(up bool) tea.Cmd {
		f.pick.wheel(up)
		return nil
	})
}

// hitPick makes the rows of the open pick list clickable.
func (m *Model) hitPick(lines []string, y0, x0, x1 int) {
	if m.screen == screenPrompt && m.form != nil && m.form.pick != nil {
		m.hitPickLines(lines, y0, x0, x1)
	}
}

// openForm builds f and shows it over the screen with its first field focused. A form the kit refuses, which
// only data of the host can cause, is not shown and its reason is.
func (m *Model) openForm(f *formDlg) {
	m.clearMessages()
	if err := f.build(m); err != nil {
		m.fail = err.Error()
		return
	}
	f.rebuilt = false
	m.form, m.screen = f, screenPrompt
}

// closeForm leaves the form; its secrets are emptied first.
func (m *Model) closeForm() {
	if m.form != nil {
		m.form.wipe()
	}
	m.form = nil
	m.screen = m.home
}

// cancelForm leaves the form without a change, as esc does.
func (m *Model) cancelForm() tea.Cmd {
	m.closeForm()
	m.status = m.tr(tCancelled)
	return nil
}

// updateForm handles one key of the form. Every key reaches the kit form first; esc asks first where the form
// holds input.
func (m *Model) updateForm(key tea.KeyPressMsg) tea.Cmd {
	f := m.form
	switch {
	case f.pick != nil:
		return m.pickKey(f, key)
	case f.keyEd != nil:
		return m.keyEditKey(f, key)
	}
	if content, ok := pasted(key); ok {
		f.kit.Paste(content)
		return nil
	}
	before := f.kit.Values()
	act, handled := f.kit.Update(key)
	// The kit reports the back key as an act that it leaves unhandled; the guarded leave below answers it.
	if handled || (act != "" && act != actBack) {
		return m.formAct(f, act, before)
	}
	if act == actBack || m.keyIs(key, actBack) {
		return m.guarded(m.cancelForm)
	}
	return nil
}

// clickForm is a click on a target of the form: the kit focuses the field, opens a choice or takes a value.
func (m *Model) clickForm(f *formDlg, res kittui.MouseResult) tea.Cmd {
	if m.form != f || m.screen != screenPrompt || f.pick != nil || f.keyEd != nil {
		return nil
	}
	before := f.kit.Values()
	act, _ := f.kit.Click(res)
	return m.formAct(f, act, before)
}

// formAct runs what the form yields: the save key, enter on a list field, and a change of a value.
func (m *Model) formAct(f *formDlg, act kittui.ActionID, before map[kittui.FieldID]kittui.FormValue) tea.Cmd {
	switch act {
	case actSave:
		return m.submitForm(f)
	case actEnter:
		m.openListEditor(f, f.kit.Focused())
		return nil
	case actBack:
		return m.guarded(m.cancelForm)
	}
	m.noteChanges(f, before)
	return nil
}

// noteChanges runs changed for every field whose value differs from before.
func (m *Model) noteChanges(f *formDlg, before map[kittui.FieldID]kittui.FormValue) {
	if f.changed == nil {
		return
	}
	after := f.kit.Values()
	for _, field := range f.fields {
		if a, b := after[field.ID], before[field.ID]; a.Text != b.Text || a.Choice != b.Choice ||
			!slices.Equal(a.Selected, b.Selected) {
			f.changed(m, f, field.ID)
		}
	}
}

// submitForm runs the check of the form. A refused save leaves the form open with every value and shows each
// problem under its field.
func (m *Model) submitForm(f *formDlg) tea.Cmd {
	m.clearMessages()
	f.problems = nil
	cmd := f.submit(m, f)
	if f.failed() && m.form == f {
		f.kit.SetProblems(f.problems)
		m.status = m.tr(tFormProblems, len(f.problems))
	}
	f.problems = nil
	return cmd
}

// formHints are the keys of the form: those of the kit form, with the save key under the form's own word, and
// cancel.
func (m *Model) formHints(f *formDlg) []kittui.Hint {
	var keys []kittui.Hint
	for _, h := range f.kit.Hints() {
		if h.Action != actSave {
			keys = append(keys, h)
		}
	}
	if f.kit.Open() {
		return keys
	}
	return keyLine(keys, answer(f.save, actSave), answer(tHintCancel, actBack))
}

// formDialog is the editor open over the form as the dialog the modal draws.
func (m *Model) formDialog() promptDialog {
	if m.form.pick != nil {
		return m.pickDialog(m.form)
	}
	return m.keyEditDialog(m.form)
}

// editorOpen reports whether an editor stands over the form, which then is drawn as a dialog of its own.
func (f *formDlg) editorOpen() bool { return f.pick != nil || f.keyEd != nil }

// head is the title and the lead of the kit form.
func (f *formDlg) head() (title, lead kittui.TextID) {
	if f.hint != "" {
		lead = literal(f.hint)
	}
	return literal(f.title), lead
}

// formFrame draws the form as the kit's modal over the dimmed screen, or, where the workspace has no room for
// it, in the workspace with its keys in the footer. Only the form takes clicks.
func (m *Model) formFrame() (string, bool) {
	f := m.form
	if m.screen != screenPrompt || f == nil || f.editorOpen() {
		return "", false
	}
	if f.lead != f.hint {
		_, lead := f.head()
		f.kit.SetLead(lead)
		f.lead = f.hint
	}
	room := m.ui.newFrame().Workspace(m.frameSpec(""))
	var background string
	m.renderAs(m.baseScreen(), 0, func() {
		m.backdrop = true
		defer func() { m.backdrop = false }()
		background = m.frame(m.workspaceView())
	})
	click := func(res kittui.MouseResult) tea.Cmd { return m.clickForm(f, res) }
	if view, targets, ok := f.kit.Overlay(m.ui.styles, background, m.termWidth, m.termHeight, room); ok {
		m.addFormTargets(targets, 0, 0, "", click)
		m.addFormWheel(f, targets, 0, 0)
		return view, true
	}
	m.keyHint(m.formHints(f))
	// The keys decide whether the footer wraps, so the room is asked again with them in place.
	room = m.ui.newFrame().Workspace(m.frameSpec(""))
	view, targets := f.kit.View(m.ui.styles, room.Width, room.Height)
	frame := m.frame(view)
	m.addFormTargets(targets, room.X, room.Y, "", click)
	m.addFormWheel(f, targets, room.X, room.Y)
	m.footerHits()
	return frame, true
}

// addFormWheel makes the free area of the form, which the kit names, take the wheel.
func (m *Model) addFormWheel(f *formDlg, targets []kittui.Target, x0, y0 int) {
	for _, t := range targets {
		if t.Zone != kittui.ZoneForm || t.Item != "" || t.Action != "" {
			continue
		}
		r := t.Rect
		m.addWheel(x0+r.X, y0+r.Y, x0+r.X+r.Width, y0+r.Y+r.Height, kittui.ZoneForm, func(up bool) tea.Cmd {
			steps := 1
			if up {
				steps = -1
			}
			f.kit.Wheel(steps)
			return nil
		})
	}
}

// openListEditor opens the editor of the list field id: the pick list of its values, or the key editor. A
// list without values has nothing to open, and enter goes on as it does on other fields.
func (m *Model) openListEditor(f *formDlg, id kittui.FieldID) {
	if l := f.lists[id]; l != nil {
		if len(l.values) == 0 {
			f.kit.Act(actNext)
			return
		}
		m.openPick(f, id)
		return
	}
	if id == fieldKeys && f.keys != nil {
		m.openKeyEditor(f)
	}
}

// setListValue shows the summary of the list field id after its editor closed.
func (m *Model) setListValue(f *formDlg, id kittui.FieldID, summary string) {
	before := f.kit.Values()
	if err := f.kit.SetValue(id, kittui.FormValue{Text: summaryText(summary)}); err != nil {
		m.fail = err.Error()
	}
	m.noteChanges(f, before)
}
