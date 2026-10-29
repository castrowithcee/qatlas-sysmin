package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// cellOf finds the first place text stands in the drawn frame, as its column and row. It reads the frame, never
// the click targets, so a test clicks where a person would look.
func cellOf(t *testing.T, m *Model, text string) (x, y int) {
	t.Helper()
	for row, line := range strings.Split(plain(m), "\n") {
		if at := strings.Index(line, text); at >= 0 {
			return ansi.StringWidth(line[:at]), row
		}
	}
	t.Fatalf("the frame shows no %q:\n%s", text, plain(m))
	return 0, 0
}

func clickAt(m *Model, x, y int) { m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}) }

func clickOn(t *testing.T, m *Model, text string) {
	t.Helper()
	x, y := cellOf(t, m, text)
	clickAt(m, x, y)
}

// wheelClock gives m a clock the test moves; each wheel call is a notch of its own unless the clock stands still.
func wheelClock(m *Model) *time.Time {
	now := time.Unix(1_000_000, 0)
	m.now = func() time.Time { return now }
	return &now
}

func wheel(m *Model, x, y int, button tea.MouseButton) {
	m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: button})
}

func TestFooterKeyNamesMapToKeysOnlyForKeysAButtonCanPress(t *testing.T) {
	for group, want := range map[string]string{
		"y delete": "y", "n/esc cancel": "n", "enter save": "enter", "esc back": "esc", "tab next": "tab",
		"F2 save": "F2", "1-6 area": "", "type to filter": "", "up/down move": "", "": "",
	} {
		if got := footerKey(group); got != want {
			t.Errorf("footerKey(%q) = %q, want %q", group, got, want)
		}
	}
	for name, code := range map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "F2": tea.KeyF2, "f9": tea.KeyF9} {
		if key, ok := keyByName(name); !ok || key.Code != code {
			t.Errorf("keyByName(%q) = %v, %v", name, key, ok)
		}
	}
	if key, ok := keyByName("y"); !ok || key.Text != "y" {
		t.Errorf("a letter has no text key: %v", key)
	}
	for _, name := range []string{"7", "xy", "", " ", "F5"} {
		if _, ok := keyByName(name); ok {
			t.Errorf("keyByName(%q) names a key", name)
		}
	}
}

func TestClickMarksARowAndAClickOnAGroupRowFoldsIt(t *testing.T) {
	m := groupedModel(t)
	m.render()
	clickOn(t, m, "bravo")
	if name, _ := m.selected(); name != "bravo" {
		t.Fatalf("a click selected %q, want bravo", name)
	}
	if len(m.list.matches) != 7 {
		t.Errorf("a first click on an entry changed the list: %q", m.list.matches)
	}
	clickOn(t, m, "web (2)")
	if name, _ := m.list.selected(); name != groupRow("web") || len(m.list.matches) != 5 {
		t.Errorf("a click on a group row: cursor %q, shown %q", name, m.list.matches)
	}
	clickOn(t, m, "web (2)")
	if len(m.list.matches) != 7 {
		t.Errorf("a second click did not unfold the group: %q", m.list.matches)
	}
}

func TestClickWhileFilteringKeepsTheFilterAndTypesNothing(t *testing.T) {
	m := frameModel(t, 80, 24)
	press(t, m, "/")
	typeText(t, m, "a")
	m.render()
	clickOn(t, m, "charlie")
	if m.list.editing || m.list.query() != "a" {
		t.Errorf("editing %v, query %q after a click", m.list.editing, m.list.query())
	}
	if name, _ := m.selected(); name != "charlie" {
		t.Errorf("selected %q, want charlie", name)
	}
}

func TestClickOutsideTheRowsDoesNothing(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.render()
	for _, p := range [][2]int{{0, 0}, {79, 0}, {40, 22}, {79, 23}, {200, 200}, {-1, -1}} {
		clickAt(m, p[0], p[1])
	}
	if name, _ := m.selected(); name != "alpha" || m.quitting || m.screen != screenList {
		t.Errorf("a click outside the rows acted: selected %q, screen %v", name, m.screen)
	}
}

func TestOnlyTheLeftButtonClicks(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.render()
	x, y := cellOf(t, m, "charlie")
	for _, b := range []tea.MouseButton{tea.MouseRight, tea.MouseMiddle} {
		m.Update(tea.MouseClickMsg{X: x, Y: y, Button: b})
	}
	m.Update(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x, Y: y})
	if name, _ := m.selected(); name != "alpha" {
		t.Errorf("selected %q, want alpha", name)
	}
}

// A notch the terminal reports as a press and a release arrives as two equal events: one step.
func TestOneNotchIsOneStepWhateverTheTerminalReports(t *testing.T) {
	m := frameModel(t, 100, 30)
	now := wheelClock(m)
	wheel(m, 5, 10, tea.MouseWheelDown)
	wheel(m, 5, 10, tea.MouseWheelDown)
	if m.list.cursor != 1 {
		t.Fatalf("a notch with its release moved the cursor to %d, want 1", m.list.cursor)
	}
	// Four queued events are two notches.
	for range 2 {
		*now = now.Add(wheelMerge)
		wheel(m, 5, 10, tea.MouseWheelDown)
		wheel(m, 5, 10, tea.MouseWheelDown)
	}
	if m.list.cursor != 3 {
		t.Errorf("two more notches ended on %d, want 3", m.list.cursor)
	}
	// Another direction or another cell is another notch, however close.
	wheel(m, 5, 10, tea.MouseWheelUp)
	if m.list.cursor != 2 {
		t.Errorf("the wheel up right after a wheel down ended on %d, want 2", m.list.cursor)
	}
	wheel(m, 6, 10, tea.MouseWheelUp)
	if m.list.cursor != 1 {
		t.Errorf("the wheel up in another cell ended on %d, want 1", m.list.cursor)
	}
}

func TestTheWheelStopsAtTheEndsWhileTheKeysWrap(t *testing.T) {
	m := frameModel(t, 100, 30)
	now := wheelClock(m)
	step := func(b tea.MouseButton) {
		*now = now.Add(time.Second)
		wheel(m, 5, 10, b)
	}
	step(tea.MouseWheelUp)
	if m.list.cursor != 0 {
		t.Errorf("the wheel wrapped from the first row to %d", m.list.cursor)
	}
	for range 10 {
		step(tea.MouseWheelDown)
	}
	if m.list.cursor != 3 {
		t.Errorf("the wheel ended on %d, want the last row 3", m.list.cursor)
	}
	if wheelClamp {
		t.Error("the wheel clamp stays on after the event")
	}
	press(t, m, "down")
	if m.list.cursor != 0 {
		t.Errorf("down on the last row went to %d, want the wrap to 0", m.list.cursor)
	}
}

func TestTheWheelDoesNothingOnADialogOrWhileFiltering(t *testing.T) {
	m := frameModel(t, 100, 30)
	now := wheelClock(m)
	press(t, m, "/")
	*now = now.Add(time.Second)
	wheel(m, 5, 10, tea.MouseWheelDown)
	if m.list.cursor != 0 {
		t.Errorf("the wheel moved the list while filtering: %d", m.list.cursor)
	}
	press(t, m, "esc")
	m.screen = screenConfirm
	m.ask = &question{title: "Sure?", consequence: "Nothing.", keys: "y yes · n/{back} no"}
	*now = now.Add(time.Second)
	wheel(m, 5, 10, tea.MouseWheelDown)
	if m.list.cursor != 0 {
		t.Errorf("the wheel moved the list behind a question: %d", m.list.cursor)
	}
}

func TestATooSmallTerminalIgnoresTheMouse(t *testing.T) {
	m := frameModel(t, 80, 24)
	now := wheelClock(m)
	sizedModel(m, minimumWidth-1, minimumHeight-1)
	view := m.render()
	for row := 0; row < minimumHeight; row++ {
		for _, x := range []int{0, 3, 12, 30} {
			clickAt(m, x, row)
			*now = now.Add(time.Second)
			wheel(m, x, row, tea.MouseWheelDown)
		}
	}
	if m.list.cursor != 0 || m.quitting || m.render() != view {
		t.Errorf("clicks on the size notice acted: cursor %d, quitting %v", m.list.cursor, m.quitting)
	}
}

func TestSwitchedOffMouseAsksForNoMouseModeAndTakesNoWheel(t *testing.T) {
	m := frameModel(t, 80, 24)
	now := wheelClock(m)
	if mode := m.View().MouseMode; mode != tea.MouseModeCellMotion {
		t.Errorf("mouse mode = %v with the mouse on, want cell motion", mode)
	}
	m.prefs.MouseOff = true
	*now = now.Add(time.Second)
	wheel(m, 5, 10, tea.MouseWheelDown)
	if m.list.cursor != 0 {
		t.Error("the wheel moved the list with the mouse off")
	}
	if mode := m.View().MouseMode; mode != tea.MouseModeNone {
		t.Errorf("mouse mode = %v with the mouse off, want none", mode)
	}
}

func TestMouseEventsAfterQuitDoNothing(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.quitting = true
	if cmd := m.updateMouse(tea.MouseClickMsg{X: 5, Y: 8, Button: tea.MouseLeft}); cmd != nil {
		t.Error("a click after quit returned a command")
	}
}

// A button of a dialog is a key that makes no character: a letter button is not passed on where the screen reads text.
func TestLetterButtonsAreNotTypedIntoAFilter(t *testing.T) {
	m := frameModel(t, 80, 24)
	press(t, m, "/")
	m.pressName("y")
	m.pressName("d")
	if m.list.query() != "" {
		t.Errorf("a letter button reached the filter: %q", m.list.query())
	}
	if cmd := m.pressName("enter"); cmd != nil || m.list.editing {
		t.Errorf("the enter button did not end the filter: editing %v", m.list.editing)
	}
	if cmd := m.pressName("nonsense"); cmd != nil {
		t.Error("an unknown key name returned a command")
	}
}

func TestBackIsShownWhereEscLeadsOneLevelBack(t *testing.T) {
	m := frameModel(t, 120, 30)
	if m.backShown() || strings.Contains(plain(m), "[esc] back") {
		t.Error("the bare list shows a back button")
	}
	press(t, m, "/")
	typeText(t, m, "br")
	if !m.backShown() {
		t.Error("a filter being typed shows no back button")
	}
	press(t, m, "enter")
	if !m.backShown() || !strings.Contains(plain(m), "[esc] back") {
		t.Errorf("a kept filter shows no back button:\n%s", plain(m))
	}
	clickOn(t, m, "[esc] back")
	if m.list.query() != "" || len(m.list.matches) != 4 || m.backShown() {
		t.Errorf("the back button did not clear the filter: query %q, shown %d", m.list.query(), len(m.list.matches))
	}
	m.screen = screenConfirm
	m.ask = &question{title: "Sure?", consequence: "Nothing.", keys: "y yes · n/{back} no"}
	if !m.backShown() {
		t.Error("a question shows no back button")
	}
	m.backdrop = true
	if m.backShown() {
		t.Error("the dimmed background of a modal shows a back button")
	}
}

// Where a terminal has no room for a modal, the keys of its footer are the buttons.
func TestFooterKeysOfAFullScreenDialogAreClickable(t *testing.T) {
	m := frameModel(t, 50, 16)
	m.screen = screenConfirm
	m.ask = &question{title: "Sure?", consequence: "Nothing happens.", keys: "y yes · n/{back} no"}
	plain(m)
	foot := strings.Split(plain(m), "\n")[m.termHeight-1]
	at := strings.Index(foot, "no")
	if at < 0 {
		t.Fatalf("the footer = %q, want the answer no", foot)
	}
	clickAt(m, ansi.StringWidth(foot[:at]), m.termHeight-1)
	if m.screen != screenList {
		t.Errorf("a click on the answer no left screen %v", m.screen)
	}
}

func TestSpanItemsPlacesItemsAndCutsThemAtTheWidth(t *testing.T) {
	spans := spanItems([]string{"ab", "cde", "f"}, 2, " | ", 12)
	want := []navSpan{{2, 4}, {7, 10}, {12, 12}}
	for i, s := range spans {
		if s != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, s, want[i])
		}
	}
}

func TestDialogOriginCentresAndNeverGoesNegative(t *testing.T) {
	dialog := []string{"1234567890", "1234567890", "1234567890", "1234567890"}
	if top, left := dialogOrigin(dialog, 80, 24); top != 10 || left != 35 {
		t.Errorf("origin = %d, %d, want 10, 35", top, left)
	}
	if top, left := dialogOrigin(dialog, 4, 2); top != 0 || left != 0 {
		t.Errorf("a dialog larger than the screen starts at %d, %d", top, left)
	}
}

func TestMarkOrActMarksFirstAndActsOnTheMarkedRow(t *testing.T) {
	cursor, acted := 0, 0
	act := func() tea.Cmd { acted++; return nil }
	markOrAct(&cursor, 2, true, act)
	if cursor != 2 || acted != 0 {
		t.Fatalf("a click on another row: cursor %d, acted %d", cursor, acted)
	}
	markOrAct(&cursor, 2, true, act)
	if acted != 1 {
		t.Errorf("a click on the marked row did not act")
	}
	markOrAct(&cursor, 2, false, act)
	if acted != 1 {
		t.Error("a click without the focus on the rows acted")
	}
}
