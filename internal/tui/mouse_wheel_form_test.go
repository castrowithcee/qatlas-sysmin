package tui

import (
	tea "charm.land/bubbletea/v2"
	"fmt"

	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
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
	for _, page := range []string{"general", "vault"} {
		m, _ := settingsModel(t, newFake(control.VaultUnlocked), page)
		now := wheelClock(m)
		x, y := wheelCell(t, m, leftArea)
		focus, last := func() int { return m.set.gen.focus }, genRows-1
		if page == "vault" {
			focus, last = func() int { return m.set.vault.focus }, vaultRows-1
		}
		down, up := wheelWalk(t, m, now, x, y, focus)
		wheelEnds(t, page, down, up, 0, last)
		if len(down) != last || len(up) != last {
			t.Errorf("%s: the wheel went %v down and %v up, want one row at a time", page, down, up)
		}
	}
}

func TestTheWheelOverSettingsKeepsTheZoneAndNeverLeavesForThePageStrip(t *testing.T) {
	m, _ := settingsModel(t, newFake(control.VaultUnlocked), "general")
	now := wheelClock(m)
	send(m, "esc", "esc")
	if m.zoneNow() != zoneTabs {
		t.Fatalf("zone = %v, want the tab line", m.zoneNow())
	}
	x, y := wheelCell(t, m, leftArea)
	notch(m, now, x, y, false)
	if m.set.gen.focus != 1 || m.zoneNow() != zoneTabs {
		t.Errorf("focus = %d, zone = %v after a notch down", m.set.gen.focus, m.zoneNow())
	}
	notch(m, now, x, y, true)
	notch(m, now, x, y, true)
	if m.set.gen.focus != 0 || m.zoneNow() != zoneTabs {
		t.Errorf("focus = %d, zone = %v after the wheel up at the first row", m.set.gen.focus, m.zoneNow())
	}
	// From the content, up on the first row would lead to the page strip; the wheel holds.
	m.zone = zoneContent
	notch(m, now, x, y, true)
	if m.set.gen.focus != 0 || m.zoneNow() != zoneContent {
		t.Errorf("focus = %d, zone = %v after the wheel up from the content", m.set.gen.focus, m.zoneNow())
	}
}

func TestTheWheelRestsWhileAFieldOfSettingsIsEdited(t *testing.T) {
	m, _ := settingsModel(t, newFake(control.VaultUnlocked), "general")
	now := wheelClock(m)
	m.set.gen.focus = genKeys
	send(m, "enter")
	typed(m, "x")
	if !m.set.editing {
		t.Fatalf("no field is edited:\n%s", plain(m))
	}
	value := m.field().Value()
	x, y := wheelCell(t, m, leftArea)
	notch(m, now, x, y, false)
	notch(m, now, x, y, true)
	if !m.set.editing || m.set.gen.focus != genKeys || m.field().Value() != value {
		t.Errorf("editing = %v, focus = %d, value = %q after the wheel, want the edit untouched",
			m.set.editing, m.set.gen.focus, m.field().Value())
	}
}

func TestTheWheelMovesTheFocusOfAFormDialogOverVisibleRowsAndHoldsAtTheEnds(t *testing.T) {
	m := appModel(t, newFake(control.VaultUnlocked), 80, 24)
	now := wheelClock(m)
	hidden := textRow("hidden", "Hidden", "")
	hidden.hidden = true
	m.openForm(&formDlg{title: "Demo", saveText: "{save} save", submit: func(*Model, *formDlg) tea.Cmd { return nil },
		rows: []formRow{textRow("a", "First", ""), choiceRow("b", "Second", []string{"x", "y"}, nil, 0), hidden,
			textRow("c", "Third", "")}})
	x, y := wheelCell(t, m, func(wheelHit) bool { return true })
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
	m, _ := settingsModel(t, newFake(control.VaultUnlocked), "general")
	now := wheelClock(m)
	m.openForm(&formDlg{title: "Demo", saveText: "{save} save", rows: []formRow{textRow("a", "First", ""), textRow("b", "Second", "")}})
	m.render()
	if len(m.wheelHits) != 1 {
		t.Fatalf("%d wheel areas, want the one of the dialog:\n%s", len(m.wheelHits), plain(m))
	}
	focus := m.set.gen.focus
	for _, at := range [][2]int{{0, 3}, {m.termWidth - 1, m.termHeight - 3}} {
		notch(m, now, at[0], at[1], false)
		notch(m, now, at[0], at[1], true)
	}
	if m.form.focus != 0 || m.set.gen.focus != focus {
		t.Errorf("form focus = %d, page focus = %d after the wheel over the background", m.form.focus, m.set.gen.focus)
	}
}

func TestTheWheelMovesTheFocusOfTheCredentialForm(t *testing.T) {
	m := appModel(t, accessFake(), 80, 24)
	now := wheelClock(m)
	send(m, "5", "n")
	if m.cred == nil {
		t.Fatal("n opens no credential form")
	}
	x, y := wheelCell(t, m, func(wheelHit) bool { return true })
	down, up := wheelWalk(t, m, now, x, y, func() int { return m.cred.focus })
	wheelEnds(t, "new", down, up, 0, m.cred.rows()-1)
	if m.cred.kind != 0 || m.cred.name.Value() != "" {
		t.Errorf("the wheel changed the form: kind = %d, name = %q", m.cred.kind, m.cred.name.Value())
	}
}

func TestTheWheelOfTheCredentialFormOfAnEditedEntryStaysOnTheFields(t *testing.T) {
	m := appModel(t, accessFake(), 80, 24)
	now := wheelClock(m)
	send(m, "5")
	send(m, "e")
	if m.cred == nil || !m.cred.edit {
		t.Fatalf("e opens no edit form:\n%s", plain(m))
	}
	x, y := wheelCell(t, m, func(wheelHit) bool { return true })
	down, up := wheelWalk(t, m, now, x, y, func() int { return m.cred.focus })
	if len(down) > 0 && m.cred.focus < 2 || len(up) > 0 && up[len(up)-1] < 2 {
		t.Errorf("down %v and up %v reach the fixed rows", down, up)
	}
}

func TestTheWheelMovesTheFieldOfAProjectPromptAndHoldsAtTheEnds(t *testing.T) {
	m := projectsModel(t, projectsFake(), 80, 24)
	now := wheelClock(m)
	m.openAdoptPrompt()
	if !m.projectPromptOpen() {
		t.Fatal("no prompt is open")
	}
	typed(m, "/srv")
	x, y := wheelCell(t, m, func(wheelHit) bool { return true })
	down, up := wheelWalk(t, m, now, x, y, func() int { return m.prj.prompt.focus })
	wheelEnds(t, "prompt", down, up, 0, 1)
	if got := m.prj.prompt.fields[0].in.Value(); got != "/srv" {
		t.Errorf("the wheel changed the field to %q", got)
	}
}

func TestTheWheelMovesTheRowsOfTheGuidedSetupAndHoldsAtTheEnds(t *testing.T) {
	m := flowModel(t, projectsFake(), 120, 40)
	now := wheelClock(m)
	f := m.prj.flow
	rows := f.shownRows(f.step)
	index := func() int {
		for i, r := range f.shownRows(f.step) {
			if r.id == f.focus {
				return i
			}
		}
		return -1
	}
	x, y := wheelCell(t, m, func(wheelHit) bool { return true })
	down, up := wheelWalk(t, m, now, x, y, index)
	wheelEnds(t, "flow", down, up, 0, len(rows)-1)
	if len(down) != len(rows)-1 {
		t.Errorf("down went %v over %d rows, want one row at a time", down, len(rows))
	}
}

func TestTheWheelScrollsTheSummaryOfTheGuidedSetupAndHoldsAtTheEnds(t *testing.T) {
	m := flowModel(t, projectsFake(), 80, minimumHeight)
	now := wheelClock(m)
	flowMinimal(t, m)
	f := m.prj.flow
	for i := range 12 {
		f.draft.composeFiles = append(f.draft.composeFiles, fmt.Sprintf("/srv/demo/app/compose-%02d.yaml", i))
	}
	x, y := wheelCell(t, m, func(wheelHit) bool { return true })
	lines := m.flowSummaryLines(m.usable(0) - 4)
	if m.render(); f.scroll != 0 || strings.Contains(plain(m), "Checks") {
		t.Fatalf("the summary of %d lines does not need scrolling:\n%s", len(lines), plain(m))
	}
	notch(m, now, x, y, true)
	if m.render(); f.scroll != 0 {
		t.Errorf("the wheel up at the start moved to %d", f.scroll)
	}
	notch(m, now, x, y, false)
	if m.render(); f.scroll != 1 {
		t.Errorf("one notch moved to %d, want 1", f.scroll)
	}
	for range len(lines) {
		notch(m, now, x, y, false)
	}
	m.render()
	end := f.scroll
	notch(m, now, x, y, false)
	if m.render(); f.scroll != end || end == 0 || end >= len(lines) {
		t.Errorf("the end held at %d then %d of %d lines", end, f.scroll, len(lines))
	}
	if !strings.Contains(plain(m), "Checks") {
		t.Errorf("the end of the summary is not in view:\n%s", plain(m))
	}
}
