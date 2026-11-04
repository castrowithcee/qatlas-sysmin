package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// hostFormOpeners are the forms of the Host area, each opened from a fresh Users page.
func hostFormOpeners() map[string]func(t *testing.T, m *Model) {
	return map[string]func(t *testing.T, m *Model){
		"user new":    func(t *testing.T, m *Model) { send(m, "n") },
		"service new": func(t *testing.T, m *Model) { send(m, "n", "right") },
		"user edit":   func(t *testing.T, m *Model) { selectUser(m, "demo-user"); send(m, "e", "right") },
		"group new":   func(t *testing.T, m *Model) { runCommand(m, "go host groups"); send(m, "n") },
		"group rename": func(t *testing.T, m *Model) {
			runCommand(m, "go host groups")
			selectGroup(m, "demo-team")
			send(m, "r")
		},
		"group members": func(t *testing.T, m *Model) {
			runCommand(m, "go host groups")
			selectGroup(m, "demo-team")
			send(m, "e")
			tickIn(m, 1)
		},
		"folder": func(t *testing.T, m *Model) { openFolderForm(t, m, "/srv/demo-app", fixedCount(3)) },
	}
}

// focusName names the focus of the form: a field, or a button by its action.
func focusName(m *Model) string {
	if act, ok := m.form.kit.FocusedButton(); ok {
		return "button:" + string(act)
	}
	return "field:" + string(m.form.kit.Focused())
}

// Tab runs through the fields, then the save button and the cancel button, and on to the first field again;
// shift+tab goes back the same way.
func TestTabCyclesThroughFieldsAndButtonsInEveryHostForm(t *testing.T) {
	for name, open := range hostFormOpeners() {
		t.Run(name, func(t *testing.T) {
			m, _, _, _ := userModel(t, 120, 40)
			open(t, m)
			if m.form == nil {
				t.Fatalf("no form open:\n%s", plain(m))
			}
			first := focusName(m)
			if !strings.HasPrefix(first, "field:") {
				t.Fatalf("the form opens on %s", first)
			}
			var walk []string
			for range 60 {
				send(m, "tab")
				if m.form == nil {
					t.Fatal("tab closed the form")
				}
				walk = append(walk, focusName(m))
				if walk[len(walk)-1] == first {
					break
				}
			}
			n := len(walk)
			if n < 3 || walk[n-1] != first || walk[n-2] != "button:"+string(actBack) || walk[n-3] != "button:"+string(actSave) {
				t.Fatalf("tab walks %v, want fields, then save, cancel, then %s", walk, first)
			}
			send(m, "shift+tab")
			if got := focusName(m); got != "button:"+string(actBack) {
				t.Errorf("shift+tab from the first field reaches %s, want the cancel button", got)
			}
			send(m, "shift+tab")
			if got := focusName(m); got != "button:"+string(actSave) {
				t.Errorf("shift+tab from cancel reaches %s, want the save button", got)
			}
			before := first
			if n >= 4 {
				before = walk[n-4]
			}
			send(m, "shift+tab")
			if got := focusName(m); got != before {
				t.Errorf("shift+tab from save reaches %s, want %s", got, before)
			}
		})
	}
}

// The buttons are the kit's: the form draws them once, in both languages, and enter on cancel leaves the form
// through the question about unsaved input, while enter on save runs the check of the form.
func TestFormButtonsSaveThroughTheCheckAndCancelAsk(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		m, _, _, _ := userModel(t, 120, 40)
		useLanguage(t, m, lang)
		send(m, "n")
		text := plain(m)
		save, cancel := m.tr(tBtnSave), m.tr(tBtnCancel)
		if strings.Count(text, cancel) != 1 || !strings.Contains(text, save) {
			t.Errorf("%s: the buttons are not drawn once:\n%s", lang, text)
		}
		for range 20 {
			send(m, "tab")
			if _, ok := m.form.kit.FocusedButton(); ok {
				break
			}
		}
		send(m, "enter") // save: the name is empty, so the check refuses
		if m.form == nil || m.status == "" {
			t.Errorf("%s: enter on save does not run the check:\n%s", lang, plain(m))
		}
		for range 30 {
			if act, _ := m.form.kit.FocusedButton(); act == actBack {
				break
			}
			send(m, "tab")
		}
		if act, _ := m.form.kit.FocusedButton(); act != actBack {
			t.Fatalf("%s: the cancel button has no focus: %q", lang, act)
		}
		send(m, "enter")
		if m.form != nil {
			t.Errorf("%s: enter on cancel leaves the form open", lang)
		}
	}
}

// Cancel with input asks first, as esc does.
func TestCancelButtonAsksAboutUnsavedInput(t *testing.T) {
	m, _, _, _ := userModel(t, 120, 40)
	send(m, "n", "tab")
	typed(m, "demo-new")
	for range 30 {
		send(m, "tab")
		if act, _ := m.form.kit.FocusedButton(); act == actBack {
			break
		}
	}
	send(m, "enter")
	if m.form == nil || m.screen != screenConfirm || m.leave == nil {
		t.Errorf("cancel with input did not ask (screen %v)", m.screen)
	}
}

// A click on a button gives the action of its key.
func TestFormButtonClicksActLikeTheirKeys(t *testing.T) {
	m, _, _, _ := userModel(t, 120, 40)
	send(m, "n")
	clickOn(t, m, "F2 Save")
	if m.form == nil || m.status == "" {
		t.Errorf("a click on save does not run the check:\n%s", plain(m))
	}
	clickOn(t, m, "esc Cancel")
	if m.form != nil {
		t.Errorf("a click on cancel leaves the form open:\n%s", plain(m))
	}
}
