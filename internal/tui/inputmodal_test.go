package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// inModal reports whether text stands on a row of the modal's frame.
func inModal(view, text string) bool {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, text) && (strings.Contains(line, "│") || strings.Contains(line, "╭")) {
			return true
		}
	}
	return false
}

func promptModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := frameModel(t, w, h)
	m.screen = screenPrompt
	m.dialog = &promptDialog{title: "Name the entry", notice: "Names are shown in the list.", input: "name: example",
		hint: "Letters only.", keys: "enter save · {back} cancel", field: true}
	return m
}

func TestPromptIsAModalWithTitleNoticeInputHintAndButtons(t *testing.T) {
	m := promptModel(t, 80, 24)
	view := plain(m)
	assertViewFits(t, view, 80, 24)
	for _, want := range []string{"Name the entry", "Names are shown in the list.", "name: example", "Letters only.", "> Save <", "[ Cancel ]"} {
		if !inModal(view, want) {
			t.Errorf("%q is not on a row of the modal:\n%s", want, view)
		}
	}
	// The list stays visible behind the modal.
	if !strings.Contains(view, "Entries (4)") {
		t.Errorf("the list behind the modal is gone:\n%s", view)
	}
}

func TestPromptFillsTheWorkspaceWhereTheTerminalIsTooSmallForAModal(t *testing.T) {
	m := promptModel(t, minimumWidth, minimumHeight)
	view := plain(m)
	assertViewFits(t, view, minimumWidth, minimumHeight)
	if strings.Contains(view, "╭") || strings.Contains(view, "[ Cancel ]") {
		t.Errorf("a modal on a terminal too small for one:\n%s", view)
	}
	for _, want := range []string{"Name the entry", "name: example", "Letters only.", "enter save"} {
		if !strings.Contains(view, want) {
			t.Errorf("the full-screen dialog lacks %q:\n%s", want, view)
		}
	}
}

func TestPromptFitsBothSizesWithAnErrorAndALongInput(t *testing.T) {
	for _, size := range []struct{ w, h int }{{minimumWidth, minimumHeight}, {modalMinWidth, modalMinHeight}, {120, 40}} {
		m := promptModel(t, size.w, size.h)
		m.dialog.input = "name: " + strings.Repeat("x", 90)
		m.fail = "name already taken"
		view := plain(m)
		assertViewFits(t, view, size.w, size.h)
		if !strings.Contains(view, "name already taken") || !strings.Contains(view, "cancel") && !strings.Contains(view, "Cancel") {
			t.Errorf("%dx%d lacks the error or the way out:\n%s", size.w, size.h, view)
		}
		if modal := strings.Contains(view, "╭"); modal != (size.w >= modalMinWidth && size.h >= modalMinHeight) {
			t.Errorf("%dx%d modal = %v:\n%s", size.w, size.h, modal, view)
		}
	}
}

// What is still running and the last error stand inside the modal, and nothing else of the notes does.
func TestPromptNotesStandInsideTheModal(t *testing.T) {
	m := promptModel(t, 100, 30)
	m.busy = "checking the name"
	m.fail = "name already taken"
	m.status = "a status that belongs to the list"
	view := plain(m)
	if !inModal(view, "checking the name ...") || !inModal(view, "error: name already taken") {
		t.Errorf("the notes are not inside the modal:\n%s", view)
	}
	if strings.Contains(view, "a status that belongs to the list") {
		t.Errorf("a status of the list shows in the dialog:\n%s", view)
	}
	if got := m.promptNotes(); strings.Count(got, "\n") != 2 {
		t.Errorf("promptNotes = %q", got)
	}
}

func TestPromptKeepsTheFirstInputRowAsAFieldAndTheRowsBelowAsTheyAre(t *testing.T) {
	m := promptModel(t, 100, 30)
	m.dialog.input = "path: /srv/demo\n  suggestion-one\n  suggestion-two"
	view := plain(m)
	for _, want := range []string{"path: /srv/demo", "suggestion-one", "suggestion-two"} {
		if !inModal(view, want) {
			t.Errorf("%q is not in the modal:\n%s", want, view)
		}
	}
	got := ansi.Strip(m.surfaced("  one\n  two"))
	first, rest, _ := strings.Cut(got, "\n")
	if !strings.HasPrefix(first, "  one") || rest != "  two" {
		t.Errorf("surfaced = %q", got)
	}
}

func TestPromptDialogHasNoPromptOutsideItsScreen(t *testing.T) {
	m := promptModel(t, 80, 24)
	if !m.promptScreen() {
		t.Fatal("the prompt screen is not one")
	}
	m.dialog = nil
	if _, ok := m.prompt(); ok || m.promptScreen() {
		t.Error("a prompt screen without a dialog is one")
	}
	m = promptModel(t, 80, 24)
	m.screen = screenList
	if m.promptScreen() {
		t.Error("the list is a prompt screen")
	}
}

func TestPromptClosesWithEscAndWithTheNoKey(t *testing.T) {
	for _, key := range []string{"esc", "n"} {
		m := promptModel(t, 80, 24)
		press(t, m, key)
		if m.screen != screenList || m.dialog != nil {
			t.Errorf("%s left screen %v", key, m.screen)
		}
	}
}
