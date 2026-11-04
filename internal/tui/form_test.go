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
	if !strings.Contains(flat(m), "[!] Not a public key") {
		t.Errorf("a bad key has no problem:\n%s", plain(m))
	}
	m.form.keyEd.line.set("")
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
