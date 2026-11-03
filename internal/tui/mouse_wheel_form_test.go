package tui

import (
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	tea "charm.land/bubbletea/v2"

	"testing"
	"time"
)

// wheelWalk turns the wheel down until the focus stops, then up until it stops, and returns the places the
// focus took on the way down and on the way up. An end holds when one more notch moves nothing.
func wheelWalk(t *testing.T, m *Model, now *time.Time, x, y int, focus func() int) (down, up []int) {
	t.Helper()
	for range 40 {
		before := focus()
		notch(m, now, x, y, false)
		if focus() == before {
			break
		}
		down = append(down, focus())
	}
	if len(down) == 0 {
		t.Fatalf("the wheel down moves nothing:\n%s", plain(m))
	}
	for range 40 {
		before := focus()
		notch(m, now, x, y, true)
		if focus() == before {
			break
		}
		up = append(up, focus())
	}
	return down, up
}

// wheelEnds checks that a form walked by the wheel visits the places in order, one at a time, and holds at
// both ends: down ends on last and up ends on first.
func wheelEnds(t *testing.T, name string, down, up []int, first, last int) {
	t.Helper()
	if len(down) == 0 || down[len(down)-1] != last || len(up) == 0 || up[len(up)-1] != first {
		t.Errorf("%s: the wheel went %v down and %v up, want the ends %d and %d", name, down, up, last, first)
	}
}

func TestTheWheelMovesTheFocusOfSettingsFormsAndHoldsAtTheEnds(t *testing.T) {
	m, _ := settingsModel(t, newFake(), "general")
	now := wheelClock(m)
	x, y := wheelCell(t, m, leftArea)
	last := len(generalOrder()) - 1
	down, up := wheelWalk(t, m, now, x, y, func() int { return genFocus(m) })
	wheelEnds(t, "general", down, up, 0, last)
	if len(down) != last || len(up) != last {
		t.Errorf("the wheel went %v down and %v up, want one field at a time", down, up)
	}
}

func TestTheWheelOverSettingsKeepsTheZoneAndNeverLeavesForThePageStrip(t *testing.T) {
	m, _ := settingsModel(t, newFake(), "general")
	now := wheelClock(m)
	send(m, "esc", "esc")
	if m.zoneNow() != zoneTabs {
		t.Fatalf("zone = %v, want the tab line", m.zoneNow())
	}
	x, y := wheelCell(t, m, leftArea)
	notch(m, now, x, y, false)
	if genFocus(m) != 1 || m.zoneNow() != zoneTabs {
		t.Errorf("focus = %d, zone = %v after a notch down", genFocus(m), m.zoneNow())
	}
	notch(m, now, x, y, true)
	notch(m, now, x, y, true)
	if genFocus(m) != 0 || m.zoneNow() != zoneTabs {
		t.Errorf("focus = %d, zone = %v after the wheel up at the first field", genFocus(m), m.zoneNow())
	}
	// From the content, up on the first field would lead to the page strip; the wheel holds.
	m.zone = zoneContent
	notch(m, now, x, y, true)
	if genFocus(m) != 0 || m.zoneNow() != zoneContent {
		t.Errorf("focus = %d, zone = %v after the wheel up from the content", genFocus(m), m.zoneNow())
	}
}

func TestTheMouseRestsWhileAKeyFieldOfSettingsWaitsForItsKey(t *testing.T) {
	m, _ := settingsModel(t, newFake(), "general")
	now := wheelClock(m)
	x, y := wheelCell(t, m, leftArea)
	toField(t, m, keyFieldID(actSave))
	send(m, "enter")
	if !m.set.gen.Capturing() {
		t.Fatalf("the key field does not wait:\n%s", plain(m))
	}
	notch(m, now, x, y, false)
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !m.set.gen.Capturing() || m.set.gen.Focused() != keyFieldID(actSave) || m.mouseMode() != tea.MouseModeNone {
		t.Errorf("capturing = %v, focus = %s, mouse mode %v after the mouse, want the wait untouched",
			m.set.gen.Capturing(), m.set.gen.Focused(), m.mouseMode())
	}
}

func TestTheWheelMovesTheFocusOfAFormDialogOverVisibleRowsAndHoldsAtTheEnds(t *testing.T) {
	m := appModel(t, newFake(), 80, 24)
	now := wheelClock(m)
	hidden := textRow("hidden", "Hidden", "")
	hidden.hidden = true
	m.openForm(&formDlg{title: "Demo", saveText: answer(tHintSave, actSave), submit: func(*Model, *formDlg) tea.Cmd { return nil },
		rows: []formRow{textRow("a", "First", ""), choiceRow("b", "Second", []string{"x", "y"}, nil, 0), hidden,
			textRow("c", "Third", "")}})
	x, y := wheelCell(t, m, func(kittui.Target) bool { return true })
	down, up := wheelWalk(t, m, now, x, y, func() int { return m.form.focus })
	if len(down) != 2 || down[0] != 1 || down[1] != 3 {
		t.Errorf("down went %v, want 1 and 3 over the hidden row", down)
	}
	wheelEnds(t, "form", down, up, 0, 3)
	if m.screen != screenPrompt || m.form == nil || m.form.rows[1].sel != 0 {
		t.Errorf("the wheel changed the form: screen = %v", m.screen)
	}
}

func TestTheWheelOverTheDimmedBackgroundOfAFormDialogMovesNothing(t *testing.T) {
	m, _ := settingsModel(t, newFake(), "general")
	now := wheelClock(m)
	m.openForm(&formDlg{title: "Demo", saveText: answer(tHintSave, actSave), rows: []formRow{textRow("a", "First", ""), textRow("b", "Second", "")}})
	m.render()
	if n := len(wheelAreas(m)); n != 1 {
		t.Fatalf("%d wheel areas, want the one of the dialog:\n%s", n, plain(m))
	}
	focus := genFocus(m)
	for _, at := range [][2]int{{0, 3}, {m.termWidth - 1, m.termHeight - 3}} {
		notch(m, now, at[0], at[1], false)
		notch(m, now, at[0], at[1], true)
	}
	if m.form.focus != 0 || genFocus(m) != focus {
		t.Errorf("form focus = %d, page focus = %d after the wheel over the background", m.form.focus, genFocus(m))
	}
}
