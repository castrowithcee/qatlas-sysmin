package tui

import (
	"fmt"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"strconv"

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

// groupedModel is the frame over four invented entries in three groups: data, web, and none.
func groupedModel(t *testing.T) *Model {
	t.Helper()
	m := frameModel(t, 100, 30)
	fillBase(m, frameColumns(), nil, []string{"alpha", "bravo", "charlie", "delta"}, func(name string) string {
		return map[string]string{"alpha": "web", "bravo": "web", "charlie": "data"}[name]
	})
	return m
}

func TestClickMarksARowAndAClickOnAGroupRowFoldsIt(t *testing.T) {
	m := groupedModel(t)
	m.render()
	clickOn(t, m, "bravo")
	if name, _ := m.selected(); name != "bravo" {
		t.Fatalf("a click selected %q, want bravo", name)
	}
	if view := plain(m); !strings.Contains(view, "alpha") || !strings.Contains(view, "▾ ■ web (2)") {
		t.Errorf("a first click on an entry changed the list:\n%s", view)
	}
	clickOn(t, m, "web (2)")
	if view := plain(m); marked(&m.base) != "group" || !strings.Contains(view, "▸ ■ web (2)") || strings.Contains(view, "bravo") {
		t.Errorf("a click on a group row does not mark and fold it:\n%s", view)
	}
	clickOn(t, m, "web (2)")
	if view := plain(m); !strings.Contains(view, "bravo") {
		t.Errorf("a second click did not unfold the group:\n%s", view)
	}
}

func TestClickWhileFilteringKeepsTheFilterAndTypesNothing(t *testing.T) {
	m := frameModel(t, 80, 24)
	press(t, m, "/")
	typeText(t, m, "a")
	m.render()
	clickOn(t, m, "charlie")
	if m.base.filtering() || m.base.list.Query() != "a" {
		t.Errorf("editing %v, query %q after a click", m.base.filtering(), m.base.list.Query())
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

// A notch the terminal reports as a press and a release arrives as two equal events, which the input filter
// of the program takes as one step.
func TestOneNotchIsOneStepWhateverTheTerminalReports(t *testing.T) {
	m := frameModel(t, 100, 30)
	now := wheelClock(m)
	in := kittui.NewInputFilter(func() time.Time { return *now })
	filtered := func(x, y int, b tea.MouseButton) {
		if msg := in.Filter(m, tea.MouseWheelMsg{X: x, Y: y, Button: b}); msg != nil {
			m.Update(msg)
		}
	}
	filtered(5, 10, tea.MouseWheelDown)
	filtered(5, 10, tea.MouseWheelDown)
	if baseRow(m) != 1 {
		t.Fatalf("a notch with its release moved the cursor to %d, want 1", baseRow(m))
	}
	// Four queued events are two notches.
	for range 2 {
		*now = now.Add(time.Second)
		filtered(5, 10, tea.MouseWheelDown)
		filtered(5, 10, tea.MouseWheelDown)
	}
	if baseRow(m) != 3 {
		t.Errorf("two more notches ended on %d, want 3", baseRow(m))
	}
	// Another direction or another cell is another notch, however close.
	filtered(5, 10, tea.MouseWheelUp)
	if baseRow(m) != 2 {
		t.Errorf("the wheel up right after a wheel down ended on %d, want 2", baseRow(m))
	}
	filtered(6, 10, tea.MouseWheelUp)
	if baseRow(m) != 1 {
		t.Errorf("the wheel up in another cell ended on %d, want 1", baseRow(m))
	}
}

func TestTheWheelAndTheKeysStopAtTheEnds(t *testing.T) {
	m := frameModel(t, 100, 30)
	now := wheelClock(m)
	step := func(b tea.MouseButton) {
		*now = now.Add(time.Second)
		wheel(m, 5, 10, b)
	}
	step(tea.MouseWheelUp)
	if baseRow(m) != 0 {
		t.Errorf("the wheel wrapped from the first row to %d", baseRow(m))
	}
	for range 10 {
		step(tea.MouseWheelDown)
	}
	if baseRow(m) != 3 {
		t.Errorf("the wheel ended on %d, want the last row 3", baseRow(m))
	}
	press(t, m, "down")
	if baseRow(m) != 3 {
		t.Errorf("down on the last row went to %d, want it to stay on 3", baseRow(m))
	}
}

func TestTheWheelDoesNothingOnADialogOrWhileFiltering(t *testing.T) {
	m := frameModel(t, 100, 30)
	now := wheelClock(m)
	press(t, m, "/")
	*now = now.Add(time.Second)
	wheel(m, 5, 10, tea.MouseWheelDown)
	if baseRow(m) != 0 {
		t.Errorf("the wheel moved the list while filtering: %d", baseRow(m))
	}
	press(t, m, "esc")
	askSure(t, m)
	*now = now.Add(time.Second)
	wheel(m, 5, 10, tea.MouseWheelDown)
	if baseRow(m) != 0 {
		t.Errorf("the wheel moved the list behind a question: %d", baseRow(m))
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
	if baseRow(m) != 0 || m.quitting || m.render() != view {
		t.Errorf("clicks on the size notice acted: cursor %d, quitting %v", baseRow(m), m.quitting)
	}
}

func TestSwitchedOffMouseAsksForNoMouseModeAndTakesNoWheel(t *testing.T) {
	m := frameModel(t, 80, 24)
	now := wheelClock(m)
	if mode := m.View().MouseMode; mode != tea.MouseModeCellMotion {
		t.Errorf("mouse mode = %v with the mouse on, want cell motion", mode)
	}
	m.prefs.MouseEnabled = false
	*now = now.Add(time.Second)
	wheel(m, 5, 10, tea.MouseWheelDown)
	if baseRow(m) != 0 {
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
	m.pressAction(actYes)
	m.pressAction(actDelete)
	if m.base.list.Query() != "" {
		t.Errorf("a letter button reached the filter: %q", m.base.list.Query())
	}
	if cmd := m.pressAction(actEnter); cmd != nil || m.base.filtering() {
		t.Errorf("the enter button did not end the filter: editing %v", m.base.filtering())
	}
	if cmd := m.pressAction("nonsense"); cmd != nil {
		t.Error("an unknown action returned a command")
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
	if m.base.list.Query() != "" || !strings.HasSuffix(m.base.list.Position(), "/4") || m.backShown() {
		t.Errorf("the back button did not clear the filter: query %q, position %q", m.base.list.Query(), m.base.list.Position())
	}
	m.screen = screenConfirm
	askSure(t, m)
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
	askSure(t, m, tallDetails...)
	plain(m)
	foot := strings.Split(plain(m), "\n")[m.termHeight-1]
	at := strings.Index(foot, "cancel")
	if at < 0 {
		t.Fatalf("the footer = %q, want the answer cancel", foot)
	}
	clickAt(m, ansi.StringWidth(foot[:at]), m.termHeight-1)
	if m.screen != screenList {
		t.Errorf("a click on the answer cancel left screen %v", m.screen)
	}
}

// tableKey is the key press of the first key of an action in the key table.
func tableKey(t *testing.T, m *Model, a action) tea.KeyPressMsg {
	t.Helper()
	for _, e := range m.ui.keys.Actions() {
		if e.ID != a {
			continue
		}
		key := e.DefaultKeys[0]
		if n, ok := strings.CutPrefix(key, "f"); ok && len(key) > 1 {
			code, err := strconv.Atoi(n)
			if err != nil {
				t.Fatalf("key %q", key)
			}
			return tea.KeyPressMsg{Code: tea.KeyF1 + rune(code-1)}
		}
		return keyMsg(key)
	}
	t.Fatalf("no key for %q", a)
	return tea.KeyPressMsg{}
}

// outcome is what a screen shows and holds after an input, to compare a click with its key.
func outcome(m *Model) string {
	return fmt.Sprintf("screen %v quitting %v\n%s", m.screen, m.quitting, plain(m))
}

// A click on a key hint, a button or an element of the frame does what the key of its action does in the same
// state, and where text is typed it makes none.
func TestClickOnEveryHintActsAsItsKey(t *testing.T) {
	states := map[string]func(t *testing.T, w, h int) *Model{
		"list": func(t *testing.T, w, h int) *Model { return hostModel(t, w, h) },
		"detail": func(t *testing.T, w, h int) *Model {
			m := hostModel(t, w, h)
			send(m, "enter")
			return m
		},
		"form": func(t *testing.T, w, h int) *Model {
			m := hostModel(t, w, h)
			send(m, "n")
			return m
		},
		"help": func(t *testing.T, w, h int) *Model {
			m := hostModel(t, w, h)
			send(m, "?")
			return m
		},
		"info": func(t *testing.T, w, h int) *Model {
			m := hostModel(t, w, h)
			send(m, "i")
			return m
		},
		"question": func(t *testing.T, w, h int) *Model {
			m, _, _ := groupsModel(t, w, h)
			selectGroup(m, "demo-ops")
			send(m, "d")
			return m
		},
		"filter": func(t *testing.T, w, h int) *Model {
			m := hostModel(t, w, h)
			send(m, "/")
			return m
		},
	}
	clicked := 0
	for name, setup := range states {
		for _, size := range [][2]int{{120, 40}, {80, 24}, {50, 20}} {
			probe := setup(t, size[0], size[1])
			probe.render()
			seen := map[action]kittui.Target{}
			for _, tg := range probe.mt.targets {
				if tg.Action != "" {
					seen[tg.Action] = tg
				}
			}
			for a, tg := range seen {
				click, key := setup(t, size[0], size[1]), setup(t, size[0], size[1])
				click.render()
				key.render()
				_, cmd := click.Update(tea.MouseClickMsg{X: tg.Rect.X, Y: tg.Rect.Y, Button: tea.MouseLeft})
				drain(click, cmd)
				if !key.typing() || key.actsInText(a) {
					_, cmd := key.Update(tableKey(t, key, a))
					drain(key, cmd)
				}
				if got, want := outcome(click), outcome(key); got != want {
					t.Errorf("%s %dx%d: click on %q differs from its key:\n%s\nkey:\n%s", name, size[0], size[1], a, got, want)
				}
				clicked++
			}
		}
	}
	if clicked < 40 {
		t.Errorf("only %d hints clicked", clicked)
	}
}

// A hint with a letter key never types the letter, and one with no key that types nothing does not act where
// text is typed.
func TestClickDuringTextInputMakesNoText(t *testing.T) {
	m := hostModel(t, 120, 40)
	send(m, "/")
	for _, a := range []action{actYes, actDelete, actNew, actQuit, actInfo, actHelp, actCommand} {
		m.pressAction(a)
	}
	if q := m.currentPage().list.Query(); q != "" || !m.currentPage().filtering() || m.pop != nil || m.quitting {
		t.Errorf("a click acted or typed while the filter reads text: query %q, popup %v", q, m.pop != nil)
	}
	m.pressAction(actEnter)
	if m.currentPage().filtering() {
		t.Error("the enter hint did not end the filter")
	}
}
