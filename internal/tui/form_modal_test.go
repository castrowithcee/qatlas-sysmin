package tui

import (
	"strings"
	"testing"
)

// leadAfterTitle reports whether the lead stands right under the title in the frame of the modal in view,
// wrapped or not. Only the cells between the borders of the modal count, not the dimmed screen beside it.
func leadAfterTitle(view, title, lead string) bool {
	rows := strings.Split(view, "\n")
	for i, row := range rows {
		head, _, found := strings.Cut(row, "═ "+title)
		if !found {
			continue
		}
		left := len([]rune(head)) - 1
		next := ""
		for _, r := range rows[i+1 : min(i+4, len(rows))] {
			cells := []rune(r)
			if len(cells) <= left+1 {
				continue
			}
			inside, _, _ := strings.Cut(string(cells[left+1:]), "║")
			next += " " + inside
		}
		words := strings.Fields(lead)
		return strings.HasPrefix(flatText(strings.TrimLeft(next, " ")), strings.Join(words[:min(6, len(words))], " "))
	}
	return false
}

// walkFocus runs tab over every field and button of the open form and checks check at each place.
func walkFocus(t *testing.T, m *Model, check func(place string)) {
	t.Helper()
	first := focusName(m)
	for range 60 {
		check(focusName(m))
		send(m, "tab")
		if focusName(m) == first {
			return
		}
	}
	t.Fatal("tab does not come back to the first field")
}

// The lead stands under the title at every focus, over every field and button, in the modal and in the
// workspace where the modal does not fit.
func TestTheLeadStaysUnderTheTitleAtEveryFocus(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}} {
		for name, open := range hostFormOpeners() {
			t.Run(name, func(t *testing.T) {
				m, _, _, _ := userModel(t, size[0], size[1])
				open(t, m)
				if m.form == nil {
					t.Fatalf("no form open:\n%s", plain(m))
				}
				lead, title := m.form.hint, m.form.title
				if lead == "" {
					t.Skip("this form has no lead")
				}
				walkFocus(t, m, func(place string) {
					view := plain(m)
					modal := strings.Contains(view, "═ "+title)
					if size[0] == 120 && !modal {
						t.Errorf("%s: no modal at %v:\n%s", place, size, view)
					}
					if modal && !leadAfterTitle(view, title, lead) {
						t.Errorf("%s: the lead is not under the title:\n%s", place, view)
					}
					if !modal && !strings.Contains(flat(m), flatText(lead)) {
						t.Errorf("%s: the lead is not in the workspace:\n%s", place, view)
					}
				})
			})
		}
	}
}

// A choice that changes the lead changes it in the open form and keeps what was typed and the focus.
func TestTheLeadFollowsTheKindWithoutLosingInput(t *testing.T) {
	m, _, _, _ := userModel(t, 120, 40)
	send(m, "n", "tab")
	typed(m, "demo-new")
	send(m, "shift+tab")
	person := m.tr(tHostNewHint)
	if !leadAfterTitle(plain(m), m.form.title, person) {
		t.Fatalf("no person lead:\n%s", plain(m))
	}
	send(m, "right")
	view := plain(m)
	if !leadAfterTitle(view, m.form.title, m.tr(tHostNewServiceHint)) {
		t.Errorf("the lead did not follow the kind:\n%s", view)
	}
	if m.form.raw(fieldName) != "demo-new" || formFocus(m) != fieldKind {
		t.Errorf("name %q, focus %s", m.form.raw(fieldName), formFocus(m))
	}
}

// A click on a field of the modal gives it the focus and types nothing; the dimmed screen behind takes none.
func TestClicksReachTheFieldsOfTheModalOnly(t *testing.T) {
	m, _, _, _ := userModel(t, 120, 40)
	send(m, "n")
	clickOn(t, m, "Repeat password")
	if formFocus(m) != fieldAgain {
		t.Errorf("focus %s after a click on the field", formFocus(m))
	}
	if m.form.raw(fieldAgain) != "" || m.form.raw(fieldName) != "" {
		t.Error("a click typed text")
	}
	clickOn(t, m, "3 Activity")
	if m.form == nil || m.screen != screenPrompt {
		t.Errorf("a click behind the modal left the form:\n%s", plain(m))
	}
}

// Where the modal does not fit, the form fills the workspace and its fields and buttons still take clicks and
// its keys stand in the footer.
func TestTheFormInTheWorkspaceTakesClicks(t *testing.T) {
	m, _, _, _ := userModel(t, 80, 24)
	send(m, "n")
	if view := plain(m); strings.Contains(view, "═ New user") || !strings.Contains(view, "F2 save") {
		t.Fatalf("not the workspace fallback:\n%s", view)
	}
	clickOn(t, m, "Name")
	if formFocus(m) != fieldName {
		t.Errorf("focus %s after a click on the field", formFocus(m))
	}
	clickOn(t, m, "F2 Save")
	if m.form == nil || m.status == "" {
		t.Errorf("a click on save does not run the check:\n%s", plain(m))
	}
	clickOn(t, m, "esc Cancel")
	if m.form != nil {
		t.Errorf("a click on cancel leaves the form open:\n%s", plain(m))
	}
}
