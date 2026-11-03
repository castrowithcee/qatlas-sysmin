package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The kit checks the key table; the table holds, and a key a multiplexer takes is refused.
func TestKeyTableHoldsAndRefusesATakenKey(t *testing.T) {
	newUIContext(kittui.CharsetUnicode)
	table := keyTable()
	for i := range table {
		if table[i].ID == actReload {
			table[i].DefaultKeys = []string{"ctrl+t"}
		}
	}
	if _, err := kittui.NewKeymap(table, kittui.CharsetUnicode); err == nil || !strings.Contains(err.Error(), "ctrl+t") {
		t.Errorf("reload on ctrl+t: %v, want it refused", err)
	}
}

func TestNoTextNamesArrowsInWords(t *testing.T) {
	for lang, cat := range catalog() {
		for id, text := range cat {
			if strings.Contains(string(id), "up/down") || strings.Contains(string(id), "left/right") ||
				strings.Contains(text, "up/down") || strings.Contains(text, "left/right") {
				t.Errorf("%s catalog: %q reads %q; arrows are ↑/↓ and ←/→", lang, id, text)
			}
		}
	}
}

func TestKeyLabelsSpellArrowsAsGlyphs(t *testing.T) {
	keys := testKeys()
	for a, want := range map[action]string{actUp: "↑", actDown: "↓", actLeft: "←", actRight: "→", actSave: "F2", actSelfUpdate: "U"} {
		if got := keys.Label(a); got != want {
			t.Errorf("label of %s = %q, want %q", a, got, want)
		}
	}
}

func TestCtrlDDeletesTheCharacterRightOfTheCursor(t *testing.T) {
	m := hostModel(t, 120, 40)
	runCommand(m, "go host folders")
	send(m, "enter")
	typed(m, "/srv/demo-app")
	for range 3 {
		send(m, "left")
	}
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if got := m.hst.fold.in.Value(); got != "/srv/demo-pp" {
		t.Errorf("value = %q, want the character right of the cursor gone", got)
	}
}

func TestVDoesNothingInTheHostUsers(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	send(m, "2", "v")
	if m.status != "" || m.fail != "" {
		t.Errorf("v answers: status %q fail %q", m.status, m.fail)
	}
}

// The arrows move the suggestion of the path line, and ctrl+n and ctrl+p are their second way.
func TestArrowsAndTheirSecondWayMoveTheSuggestionOfThePathLine(t *testing.T) {
	m := hostModel(t, 120, 40)
	runCommand(m, "go host folders")
	send(m, "enter")
	in := &m.hst.fold.in
	in.matches, in.mark, in.marked = []string{"/srv/demo-a", "/srv/demo-b", "/srv/demo-c"}, 0, false
	for _, step := range []struct {
		key  tea.KeyPressMsg
		mark int
	}{
		{keyMsg("down"), 1}, {tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}, 2}, {keyMsg("up"), 1},
		{tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}, 0},
	} {
		m.Update(step.key)
		if in.mark != step.mark || !in.marked {
			t.Fatalf("after %v the mark is %d (marked %v), want %d", step.key, in.mark, in.marked, step.mark)
		}
	}
}
