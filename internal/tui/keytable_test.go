package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// takenKeys are the keys a terminal, a multiplexer or a shell owns: no tool gives them an action of its own.
var takenKeys = []string{
	"ctrl+s", "ctrl+q", "ctrl+z", "ctrl+b", "ctrl+a", "ctrl+g", "ctrl+n", "ctrl+o", "ctrl+p", "ctrl+t",
	"ctrl+f", "ctrl+i", "ctrl+m", "ctrl+h",
	"f1", "f5", "f10", "f11", "f12",
	"alt+n", "alt+p", "alt+h", "alt+j", "alt+k", "alt+l", "alt+f", "alt+i", "alt+o", "alt+left", "alt+right",
	"alt+up", "alt+down", "alt+[", "alt+]", "alt+=", "alt+-", "alt++",
	"alt+0", "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9",
}

// secondWays are the taken keys an action may hold next to a first way without a collision.
var secondWays = map[action][]string{
	actSuggestPrev: {"ctrl+p"},
	actSuggestNext: {"ctrl+n"},
}

// takenKeyUses lists the actions of the table bound to a taken key that is no allowed second way.
func takenKeyUses() []string {
	var uses []string
	for _, d := range keyDefs {
		for _, k := range keymap[d.act].Keys() {
			if slices.Contains(takenKeys, k) && !slices.Contains(secondWays[d.act], k) {
				uses = append(uses, string(d.act)+" on "+k)
			}
		}
	}
	return uses
}

func TestNoActionTakesAKeyAnotherToolOwns(t *testing.T) {
	for _, use := range takenKeyUses() {
		t.Errorf("%s, which a terminal or multiplexer owns", use)
	}
}

// The check has teeth: an action moved onto ctrl+t is found.
func TestTakenKeyCheckFindsCtrlT(t *testing.T) {
	old := keymap[actReload]
	t.Cleanup(func() { keymap[actReload] = old })
	setKeys(actReload, "ctrl+t")
	if uses := takenKeyUses(); len(uses) != 1 || uses[0] != "reload on ctrl+t" {
		t.Errorf("uses = %v", uses)
	}
}

func TestNoTextNamesArrowsInWords(t *testing.T) {
	for lang, cat := range map[string]map[textID]string{"en": textEN, "de": textDE} {
		for id, text := range cat {
			if strings.Contains(string(id), "up/down") || strings.Contains(string(id), "left/right") ||
				strings.Contains(text, "up/down") || strings.Contains(text, "left/right") {
				t.Errorf("%s catalog: %q reads %q; arrows are ↑/↓ and ←/→", lang, id, text)
			}
		}
	}
}

func TestKeyNameSpellsArrowsAsGlyphs(t *testing.T) {
	for k, want := range map[string]string{"up": "↑", "down": "↓", "left": "←", "right": "→", "f8": "F8", "A": "A"} {
		if got := keyName(k); got != want {
			t.Errorf("keyName(%q) = %q, want %q", k, got, want)
		}
	}
}

func TestNewStartsTheFlowAndGDoesNothing(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 40)
	send(m, "g")
	if m.prj.flow != nil || m.ov != nil {
		t.Fatal("g does something in Projects")
	}
	send(m, "n", "enter")
	if m.prj.flow == nil {
		t.Fatal("n does not start the guided setup")
	}
}

func TestProjectsFooterAndHelpNameNAndNotG(t *testing.T) {
	m := projectsModel(t, projectsFake(), 160, 40)
	foot := footerOf(m)
	if !strings.Contains(foot, "n new") || strings.Contains(foot, "g/n") || strings.Contains(foot, "g new") {
		t.Errorf("footer = %q", foot)
	}
	send(m, "?")
	help := plain(m)
	if strings.Contains(help, "guided setup") {
		t.Errorf("help names the removed setup key:\n%s", help)
	}
}

func TestApproveAllNeedsCapitalAAndAsks(t *testing.T) {
	fb := accessFake()
	m := appModel(t, fb, 80, 24)
	send(m, "4", "a")
	if m.screen == screenConfirm || fb.count("approve-all") != 0 {
		t.Fatal("a acts in Access")
	}
	send(m, "A")
	if m.screen != screenConfirm {
		t.Fatalf("A does not ask first:\n%s", plain(m))
	}
	if fb.count("approve-all") != 0 {
		t.Fatal("A approved before the answer")
	}
}

func TestF8RemovesTheRowOfTheFlowAndCtrlDEditsText(t *testing.T) {
	fb := projectsFake()
	flowFakeLists(fb)
	m := flowModel(t, fb, 120, 40)
	typed(m, "demo-app")
	send(m, "f2")
	flowAtStep(t, m, flowSource)
	f := m.prj.flow
	f.draft.images = append(f.draft.images, flowImage{name: "second"})
	flowToRow(t, m, "images[1].name")
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if len(f.draft.images) != 2 {
		t.Fatalf("ctrl+d removed a row: %d images", len(f.draft.images))
	}
	if f.draft.images[1].name != "second" {
		t.Fatalf("ctrl+d at the end of the text changed it: %q", f.draft.images[1].name)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF8})
	if len(f.draft.images) != 1 {
		t.Fatalf("f8 left %d images, want 1", len(f.draft.images))
	}
}

func TestCtrlDDeletesTheCharacterRightOfTheCursor(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 40)
	send(m, "n", "enter")
	typed(m, "demo-app")
	for range 3 {
		send(m, "left")
	}
	m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	if got := m.prj.flow.in.Value(); got != "demo-pp" {
		t.Errorf("value = %q, want the character right of the cursor gone", got)
	}
}

func TestVDoesNothingInTheLibrary(t *testing.T) {
	m := appModel(t, accessFake(), 80, 24)
	send(m, "5", "v")
	if m.status != "" || m.fail != "" {
		t.Errorf("v answers: status %q fail %q", m.status, m.fail)
	}
}
