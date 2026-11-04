package tui

import (
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// frameModel is a frame of the given size over a short list of invented entries, drawn without colour.
func frameModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := testModel(defaultPrefs(), nil)
	m.title = "Entries"
	cells := map[string][]string{}
	names := []string{"alpha", "bravo", "charlie", "delta"}
	for _, n := range names {
		cells[n] = []string{n, "note of " + n}
	}
	fillBase(m, frameColumns(), cells, names, nil)
	sizedModel(m, w, h)
	return m
}

func frameColumns() []kittui.Column {
	return []kittui.Column{{Title: "Name"}, {Title: "Note", Flex: true}}
}

func plain(m *Model) string { return ansi.Strip(m.render()) }

func TestEmptyFrameHasHeaderFooterAndQuits(t *testing.T) {
	m := frameModel(t, 80, 24)
	fillBase(m, frameColumns(), nil, nil, nil)
	view := plain(m)
	assertViewFits(t, view, 80, 24)
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[0], "qsysmin") {
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
	if view := plain(m); !strings.Contains(view, "1/1 (4 total)") || strings.Contains(view, "alpha") {
		t.Errorf("the filter does not narrow to charlie:\n%s", view)
	}
	press(t, m, "esc")
	if view := plain(m); !strings.Contains(view, "3/4") || !strings.Contains(view, "alpha") {
		t.Errorf("esc does not bring every entry back:\n%s", view)
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
	fillBase(m, frameColumns(), nil, []string{"alpha", "bravo", "charlie", "delta"}, group)
	view := plain(m)
	for _, want := range []string{"▾ ■ first (2)", "▾ ■ no group (2)"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	markGroup(&m.base, 0)
	press(t, m, "enter")
	if strings.Contains(plain(m), "alpha") {
		t.Error("a folded group still shows its entries")
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
	m.prefs.MouseEnabled = false
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

func TestBackButtonStandsOnDialogsThatFillTheWorkspace(t *testing.T) {
	// A question with no room over the screen is the workspace, under the header.
	m := frameModel(t, 50, 16)
	if strings.Contains(strings.Split(plain(m), "\n")[0], "back") {
		t.Error("the bare list shows a back button")
	}
	askSure(t, m, tallDetails...)
	first := strings.Split(plain(m), "\n")[0]
	if !strings.Contains(first, "[esc] back") {
		t.Fatalf("header = %q, want a back button", first)
	}
	clickAt(m, strings.Index(first, "[esc] back")+2, 0)
	if m.screen != screenList {
		t.Error("a click on the back button did not close the question")
	}
}

func TestBackFrameKeepsOnlyTheBackButtonClickable(t *testing.T) {
	m := frameModel(t, 50, 16)
	askSure(t, m, tallDetails...)
	plain(m)
	for _, tg := range m.mt.targets {
		if tg.Rect.Y == 0 && tg.Action != actBack {
			t.Errorf("header target %+v survives in the fallback", tg)
		}
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

func TestTheInputFilterLetsATypedLetterPass(t *testing.T) {
	f := kittui.NewInputFilter(func() time.Time { return time.Unix(0, 0) })
	if msg := f.Filter(nil, textKey("a")); msg == nil {
		t.Error("a typed letter was dropped")
	}
}

func TestViewUsesTheAlternateScreen(t *testing.T) {
	if !frameModel(t, 80, 24).View().AltScreen {
		t.Error("the interface draws inline instead of on the alternate screen")
	}
}
