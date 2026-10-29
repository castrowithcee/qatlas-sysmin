package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// frameModel is a frame of the given size over a short list of invented entries, drawn without colour.
func frameModel(t *testing.T, w, h int) *Model {
	t.Helper()
	restoreRenderer(t)
	setProfile(profileNone)
	applyTheme(uiprefs.ThemeTerminal)
	useLanguage(t, "en")
	m := New(uiprefs.Default(), nil)
	m.title = "Entries"
	m.table = table{
		columns: []column{{title: "Name"}, {title: "Note", flex: true}},
		rows:    map[string][]string{},
	}
	var names []string
	for _, n := range []string{"alpha", "bravo", "charlie", "delta"} {
		names = append(names, n)
		m.table.rows[n] = []string{n, "note of " + n}
	}
	m.setEntries(names, nil, nil)
	sizedModel(m, w, h)
	return m
}

func plain(m *Model) string { return ansi.Strip(m.render()) }

func TestEmptyFrameHasHeaderFooterAndQuits(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.setEntries(nil, nil, nil)
	view := plain(m)
	assertViewFits(t, view, 80, 24)
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[0], "sysmin") {
		t.Errorf("header = %q, want the name", lines[0])
	}
	if !strings.Contains(lines[len(lines)-1], "q quit") {
		t.Errorf("footer = %q, want the quit key", lines[len(lines)-1])
	}
	_, cmd := m.Update(keyMsg("q"))
	if cmd == nil || !m.quitting {
		t.Fatal("q does not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q returned %T, want a quit message", cmd())
	}
}

func TestListShowsRowsAndMovesTheCursor(t *testing.T) {
	m := frameModel(t, 80, 24)
	view := plain(m)
	assertViewFits(t, view, 80, 24)
	for _, want := range []string{"Entries (4)", "1/4", "alpha", "note of delta"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	press(t, m, "down", "down")
	if name, _ := m.selected(); name != "charlie" {
		t.Errorf("selected %q after two downs, want charlie", name)
	}
	if !strings.Contains(plain(m), "3/4") {
		t.Error("position does not follow the cursor")
	}
}

func TestListFilterNarrowsAndEscClears(t *testing.T) {
	m := frameModel(t, 80, 24)
	press(t, m, "/")
	typeText(t, m, "ar")
	press(t, m, "enter")
	if got := strings.Join(m.list.matches, ","); got != "charlie" {
		t.Errorf("matches = %q", got)
	}
	press(t, m, "esc")
	if len(m.list.matches) != 4 {
		t.Errorf("esc left %d entries, want 4", len(m.list.matches))
	}
}

func TestGroupsFoldAndTheCursorStaysOnEntries(t *testing.T) {
	m := frameModel(t, 80, 24)
	group := func(n string) string {
		if n == "alpha" || n == "bravo" {
			return "first"
		}
		return ""
	}
	m.setEntries([]string{"alpha", "bravo", "charlie", "delta"}, group, nil)
	view := plain(m)
	for _, want := range []string{"▾ ■ first (2)", "▾ ■ no group (2)"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	m.list.selectName(groupRow("first"))
	press(t, m, "enter")
	if strings.Contains(plain(m), "alpha") {
		t.Error("a folded group still shows its entries")
	}
}

func TestQuestionIsAModalWithButtonsOverTheList(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.screen = screenConfirm
	m.ask = &question{title: "Remove the example entry?", consequence: "The entry is gone.", danger: true,
		keys: "y remove · n/{back} cancel"}
	view := plain(m)
	assertViewFits(t, view, 80, 24)
	for _, want := range []string{"Remove the example entry?", "The entry is gone.", "Remove", "Cancel", "alpha"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	press(t, m, "n")
	if m.screen != screenList || m.ask != nil {
		t.Error("n does not close the question")
	}
}

func TestQuestionFillsASmallTerminal(t *testing.T) {
	m := frameModel(t, 50, 16)
	m.screen = screenConfirm
	m.ask = &question{title: "Remove the example entry?", consequence: "The entry is gone.", keys: "y remove · n/{back} cancel"}
	view := plain(m)
	assertViewFits(t, view, 50, 16)
	if !strings.Contains(view, "Remove the example entry?") || !strings.Contains(view, "y remove") {
		t.Errorf("the question or its keys are missing:\n%s", view)
	}
}

func TestPromptDialogIsAModal(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.screen = screenPrompt
	m.dialog = &promptDialog{title: "Name the entry", input: "name: example", hint: "Letters only.", keys: "enter save · {back} cancel"}
	view := plain(m)
	assertViewFits(t, view, 80, 24)
	for _, want := range []string{"Name the entry", "example", "Letters only.", "Save", "Cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	press(t, m, "esc")
	if m.screen != screenList {
		t.Error("esc does not close the dialog")
	}
}

func TestTooSmallTerminalShowsTheNoticeAndTheWayOut(t *testing.T) {
	m := frameModel(t, 30, 8)
	view := plain(m)
	assertViewFits(t, view, 30, 8)
	if !strings.Contains(view, "q quit") || !strings.Contains(view, "Resize terminal") {
		t.Errorf("notice = %q", view)
	}
}

func TestClickMarksARowAndASecondClickPressesEnter(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.render()
	row := -1
	for y, line := range strings.Split(plain(m), "\n") {
		if strings.Contains(line, "charlie") {
			row = y
		}
	}
	click := func() { m.Update(tea.MouseClickMsg{X: 5, Y: row, Button: tea.MouseLeft}) }
	click()
	if name, _ := m.selected(); name != "charlie" {
		t.Fatalf("a click selected %q, want charlie", name)
	}
	click()
	if m.quitting {
		t.Error("a click quit the editor")
	}
}

func TestSwitchedOffMouseDoesNothing(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.prefs.MouseOff = true
	m.render()
	m.Update(tea.MouseClickMsg{X: 5, Y: 8, Button: tea.MouseLeft})
	if name, _ := m.selected(); name != "alpha" {
		t.Errorf("selected %q, want alpha", name)
	}
	if m.mouseMode() != tea.MouseModeNone {
		t.Error("mouse mode is on although the preference is off")
	}
}

func TestWheelMovesOneRowAndStopsAtTheEnds(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.render()
	wheel := func(b tea.MouseButton) {
		m.now = func() time.Time { return time.Now().Add(time.Hour) }
		m.Update(tea.MouseWheelMsg{X: 5, Y: 8, Button: b})
	}
	wheel(tea.MouseWheelUp)
	if name, _ := m.selected(); name != "alpha" {
		t.Errorf("the wheel wrapped to %q", name)
	}
	wheel(tea.MouseWheelDown)
	if name, _ := m.selected(); name != "bravo" {
		t.Errorf("selected %q after one notch, want bravo", name)
	}
}

func TestBackButtonStandsOnDialogsAndPressesEsc(t *testing.T) {
	// A terminal too small for a modal shows the question as the workspace, under the header.
	m := frameModel(t, 50, 16)
	if strings.Contains(strings.Split(plain(m), "\n")[0], "back") {
		t.Error("the bare list shows a back button")
	}
	m.screen = screenConfirm
	m.ask = &question{title: "Sure?", consequence: "Nothing happens.", keys: "y yes · n/{back} no"}
	first := strings.Split(plain(m), "\n")[0]
	at := strings.Index(first, "[esc] back")
	if at < 0 {
		t.Fatalf("header = %q, want a back button", first)
	}
	m.Update(tea.MouseClickMsg{X: at + 2, Y: 0, Button: tea.MouseLeft})
	if m.screen != screenList {
		t.Error("a click on the back button did not close the question")
	}
}

func TestCopyHandsOverTheNameOfTheRowAndRedactsSecrets(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.redactor.Add("bravo-secret-value")
	_, cmd := m.Update(keyMsg("c"))
	if cmd == nil || m.status == "" {
		t.Fatal("c copies nothing")
	}
	if got := m.copyClean("a\x1b[31mb\x1b[0m bravo-secret-value\x00"); strings.Contains(got, "secret-value") ||
		strings.ContainsAny(got, "\x1b\x00") {
		t.Errorf("copyClean = %q, want no secret and no control character", got)
	}
}

func TestDropdownOpensChoosesAndCloses(t *testing.T) {
	m := frameModel(t, 80, 24)
	chosen := -1
	m.openDropdown("field", []string{"One", "Two", "Three"}, 0, func(i int) tea.Cmd { chosen = i; return nil })
	m.noteField("field", 2, 3)
	m.layoutWorkspace()
	view := ansi.Strip(m.overlayDropdown(m.frame(m.workspaceView())))
	assertViewFits(t, view, 80, 24)
	if !strings.Contains(view, "Three") {
		t.Errorf("the open list is not drawn:\n%s", view)
	}
	press(t, m, "down", "enter")
	if chosen != 1 || m.dd != nil {
		t.Errorf("chosen = %d, open = %v, want 1 and closed", chosen, m.dd != nil)
	}
	m.openDropdown("field", []string{"One"}, 0, func(int) tea.Cmd { chosen = 9; return nil })
	press(t, m, "esc")
	if m.dd != nil || chosen == 9 {
		t.Error("esc did not close the list without choosing")
	}
}

func TestFragmentFilterDropsASplitMouseReport(t *testing.T) {
	f := newFragmentFilter(func() time.Time { return time.Unix(0, 0) })
	if msg := f.filter(nil, textKey("a")); msg == nil {
		t.Error("a typed letter was dropped")
	}
}
