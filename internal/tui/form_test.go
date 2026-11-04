package tui

import (
	"strings"
	"testing"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// formFocus is the field of the open form that has the focus.
func formFocus(m *Model) kittui.FieldID { return m.form.kit.Focused() }

// formShows reports whether the open form shows the field id now.
func formShows(m *Model, id kittui.FieldID) bool {
	_, ok := m.form.kit.Values()[id]
	return ok
}

// formChoices are the values the choice field id of the open form offers.
func formChoices(m *Model, id kittui.FieldID) []string {
	for _, f := range m.form.fields {
		if f.ID == id {
			return choiceStrings(f.Choices)
		}
	}
	return nil
}

func choiceStrings(choices []kittui.Choice) []string {
	out := make([]string, len(choices))
	for i, c := range choices {
		out[i] = string(c.ID)
	}
	return out
}

// listValues are the values the list field id of the open form chooses from.
func listValues(m *Model, id kittui.FieldID) []string { return m.form.lists[id].values }

// setField gives the text field id of the open form the text, as typing it would.
func setField(t *testing.T, m *Model, id kittui.FieldID, text string) {
	t.Helper()
	if err := m.form.kit.SetValue(id, kittui.FormValue{Text: text}); err != nil {
		t.Fatal(err)
	}
}

// A refused save shows every problem under its own field at once and keeps every value.
func TestARefusedSaveShowsEachProblemUnderItsField(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	send(m, "n", "tab", "tab", "tab")
	typed(m, "a")
	save(m)
	view := plain(m)
	lines := strings.Split(view, "\n")
	under := func(label, problem string) bool {
		for i, line := range lines {
			if strings.Contains(line, label) {
				for _, next := range lines[i+1 : min(i+4, len(lines))] {
					if strings.Contains(next, "[!] "+problem) {
						return true
					}
				}
			}
		}
		return false
	}
	if !under("Name", "Enter a name.") || !under("Password", "Enter a password.") {
		t.Errorf("the problems do not stand under their fields:\n%s", view)
	}
	if m.form == nil || len(f.reqs) != 0 || m.status != "Not saved; problems: 2." || formFocus(m) != fieldName {
		t.Errorf("form open %v, calls %v, status %q, focus %s", m.form != nil, f.reqs, m.status, formFocus(m))
	}
	if m.form.raw(fieldAgain) != "a" {
		t.Error("a refused save dropped what was typed")
	}
	typed(m, "demo-new")
	if strings.Contains(plain(m), "Enter a name.") {
		t.Error("the problem of a field stays after the field changed")
	}
}

// esc leaves an unchanged form at once and asks first about a changed one; ctrl+c asks the same question.
func TestLeavingAChangedFormAsksFirst(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	send(m, "n", "esc")
	if m.form != nil || m.screen == screenConfirm || m.status != "Cancelled." {
		t.Fatalf("esc on an unchanged form: form %v screen %v status %q", m.form != nil, m.screen, m.status)
	}
	send(m, "n", "tab")
	typed(m, "demo-new")
	send(m, "tab")
	typed(m, inputMarker)
	send(m, "esc")
	if m.form == nil || m.screen != screenConfirm || m.leave == nil || !strings.Contains(flat(m), "d discard") {
		t.Fatalf("esc on a changed form does not ask:\n%s", plain(m))
	}
	seeNoInput(t, m, "the leave question")
	send(m, "esc")
	if m.screen != screenPrompt || m.form == nil || m.form.value(fieldName) != "demo-new" || m.form.raw(fieldPassword) != inputMarker {
		t.Fatalf("keep editing does not return to the form with its input: screen %v", m.screen)
	}
	send(m, "ctrl+c")
	if m.quitting || m.screen != screenConfirm {
		t.Fatalf("ctrl+c on a changed form quits without asking")
	}
	send(m, "esc", "esc", "d")
	if m.form != nil || m.screen != screenArea || len(f.reqs) != 0 || m.status != "Cancelled." {
		t.Errorf("discard: form %v screen %v status %q", m.form != nil, m.screen, m.status)
	}
	seeNoInput(t, m, "the page after discarding")
}

// F2 in the leave question saves: the form is checked as F2 in the form checks it, and stays open where it is
// refused.
func TestSaveInTheLeaveQuestionChecksTheForm(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	send(m, "n", "tab")
	typed(m, "demo-new")
	send(m, "esc", "f2")
	if m.form == nil || m.screen != screenPrompt || !strings.Contains(plain(m), "[!] Enter a password.") || len(f.reqs) != 0 {
		t.Errorf("save from the question:\n%s", plain(m))
	}
}

// showField moves the focus on with tab until the field is in view: a form scrolls where the height runs short.
func showField(m *Model, label string) {
	for range 10 {
		if strings.Contains(plain(m), label) {
			return
		}
		send(m, "tab")
	}
}

// A click on a field focuses it and types nothing; a click on a choice opens its values.
func TestAClickOnAFieldOfAFormFocusesIt(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}} {
		m, _, _, _ := userModel(t, size[0], size[1])
		send(m, "n")
		showField(m, "Repeat password")
		clickOn(t, m, "Repeat password")
		if formFocus(m) != fieldAgain || m.form.raw(fieldAgain) != "" {
			t.Errorf("%v: focus %s after a click on the password field", size, formFocus(m))
		}
		showField(m, "Docker")
		clickOn(t, m, "Docker")
		if formFocus(m) != fieldDocker || !m.form.kit.Open() {
			t.Errorf("%v: focus %s, open %v after a click on a choice", size, formFocus(m), m.form.kit.Open())
		}
	}
}

// The key editor adds one checked key at a time; esc drops what it added, enter on the empty line keeps it.
func TestTheKeyEditorAddsCheckedKeysAndEscDropsThem(t *testing.T) {
	m, _, _, _ := userModel(t, 120, 40)
	line, fp := demoKey(t, "demo-new@laptop")
	send(m, "n")
	for range 7 {
		send(m, "tab")
	}
	if formFocus(m) != fieldKeys {
		t.Fatalf("focus %s, want the keys", formFocus(m))
	}
	send(m, "enter")
	typed(m, "not a key")
	send(m, "enter")
	if !strings.Contains(flat(m), "Not a public key") {
		t.Errorf("a bad key has no problem:\n%s", plain(m))
	}
	m.form.keyEd.dlg.ClearInput()
	typed(m, line)
	send(m, "enter")
	m.Update(pasteKey(line))
	send(m, "enter")
	if !strings.Contains(flat(m), "already") || len(m.form.keys.keys) != 1 {
		t.Errorf("a key twice: keys %d\n%s", len(m.form.keys.keys), plain(m))
	}
	send(m, "esc")
	if m.form.keyEd != nil || len(m.form.keys.keys) != 0 || m.form.value(fieldKeys) != "" {
		t.Errorf("esc kept %d keys, summary %q", len(m.form.keys.keys), m.form.value(fieldKeys))
	}
	send(m, "enter")
	typed(m, line)
	send(m, "enter", "enter")
	if m.form.keyEd != nil || !strings.Contains(m.form.value(fieldKeys), fp) {
		t.Errorf("enter on the empty line: summary %q", m.form.value(fieldKeys))
	}
}

// openKeyEditorOf opens the key editor of the create form.
func openKeyEditorOf(t *testing.T, m *Model) {
	t.Helper()
	send(m, "n")
	for range 7 {
		send(m, "tab")
	}
	send(m, "enter")
	if m.form == nil || m.form.keyEd == nil {
		t.Fatalf("the key editor did not open:\n%s", plain(m))
	}
}

// The problem of a refused line stands in the dialog until the next key, and the fingerprint of a key added is
// one of its details.
func TestTheKeyEditorShowsTheProblemAndTheFingerprintsInItsDialog(t *testing.T) {
	m, _, _, _ := userModel(t, 120, 40)
	line, fp := demoKey(t, "demo-new@laptop")
	openKeyEditorOf(t, m)
	typed(m, "not a key")
	send(m, "enter")
	view := plain(m)
	if !strings.Contains(view, "Not a public key") || !strings.Contains(view, "not a key") || !strings.Contains(view, "[ add ]") {
		t.Errorf("a refused line is not shown with its problem:\n%s", view)
	}
	typed(m, "x")
	if strings.Contains(plain(m), "Not a public key") {
		t.Errorf("the problem stays after a key:\n%s", plain(m))
	}
	m.form.keyEd.dlg.ClearInput()
	typed(m, line)
	send(m, "enter")
	view = plain(m)
	if !strings.Contains(view, fp) || strings.Contains(view, "Not a public key") || !strings.Contains(view, "[ keep ]") {
		t.Errorf("an added key has no fingerprint, or the dialog keeps its old state:\n%s", view)
	}
}

// A click on a button of the key editor does what its key does, and a click on the line types nothing.
func TestAClickOnTheKeyEditorActsAsItsKeysDo(t *testing.T) {
	m, _, _, _ := userModel(t, 120, 40)
	line, fp := demoKey(t, "demo-new@laptop")
	openKeyEditorOf(t, m)
	typed(m, line)
	clickOn(t, m, "[ add ]")
	if m.form.keyEd == nil || len(m.form.keys.keys) != 1 || !strings.Contains(plain(m), fp) {
		t.Fatalf("a click on add did not add the key: %d keys\n%s", len(m.form.keys.keys), plain(m))
	}
	typed(m, "ab")
	x, y := cellOf(t, m, "ab_")
	clickAt(m, x, y)
	if got := m.form.keyEd.dlg.Value(); got != "ab" {
		t.Errorf("a click on the line changed it to %q", got)
	}
	m.form.keyEd.dlg.ClearInput()
	m.syncKeyDialog(m.form)
	clickOn(t, m, "[ keep ]")
	if m.form.keyEd != nil || !strings.Contains(m.form.value(fieldKeys), fp) {
		t.Errorf("a click on keep: editor %v, summary %q", m.form.keyEd != nil, m.form.value(fieldKeys))
	}
	send(m, "enter")
	typed(m, "tail")
	clickOn(t, m, "[ discard ]")
	if m.form.keyEd != nil || len(m.form.keys.keys) != 1 {
		t.Errorf("a click on discard: editor %v, %d keys", m.form.keyEd != nil, len(m.form.keys.keys))
	}
}

// Where the terminal has no room over the screen, the dialog fills the workspace and its answers stand in the
// footer, which takes the clicks.
func TestTheKeyEditorFillsTheWorkspaceOfASmallTerminal(t *testing.T) {
	m, _, _, _ := userModel(t, 40, 14)
	line, fp := demoKey(t, "demo-new@laptop")
	openKeyEditorOf(t, m)
	typed(m, line)
	view := plain(m)
	assertViewFits(t, view, 40, 14)
	if strings.Contains(view, "[ add ]") || !strings.Contains(view, "enter add") {
		t.Fatalf("the answers are not in the footer:\n%s", view)
	}
	x, y := cellOf(t, m, "enter add")
	clickAt(m, x, y)
	if len(m.form.keys.keys) != 1 || !strings.Contains(plain(m), fp[:20]) {
		t.Fatalf("a click on enter in the footer did not add the key:\n%s", plain(m))
	}
	typed(m, "ab")
	x, y = cellOf(t, m, "ab_")
	clickAt(m, x, y)
	if got := m.form.keyEd.dlg.Value(); got != "ab" {
		t.Errorf("a click on the line changed it to %q", got)
	}
	x, y = cellOf(t, m, "esc discard")
	clickAt(m, x, y)
	if m.form.keyEd != nil || len(m.form.keys.keys) != 0 {
		t.Errorf("a click on esc: editor %v, %d keys", m.form.keyEd != nil, len(m.form.keys.keys))
	}
}
