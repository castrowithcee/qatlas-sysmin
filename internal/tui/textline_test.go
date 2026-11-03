package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// A text line keeps the editing keys of every text field.
func TestTheTextLineKeepsTheUsualEditingKeys(t *testing.T) {
	ctrl := func(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }
	alt := func(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModAlt} }
	for _, tc := range []struct {
		name string
		keys []tea.KeyPressMsg
		want string
	}{
		{"ctrl+w deletes the word before the cursor", []tea.KeyPressMsg{ctrl('w')}, "/srv/demo "},
		{"alt+backspace too", []tea.KeyPressMsg{{Code: tea.KeyBackspace, Mod: tea.ModAlt}}, "/srv/demo "},
		{"ctrl+u deletes up to the start", []tea.KeyPressMsg{alt('b'), ctrl('u')}, "app"},
		{"ctrl+k deletes to the end", []tea.KeyPressMsg{ctrl('a'), ctrl('f'), ctrl('k')}, "/"},
		{"alt+d deletes the word after the cursor", []tea.KeyPressMsg{ctrl('a'), alt('d')}, " app"},
		{"ctrl+d and delete take the character right", []tea.KeyPressMsg{{Code: tea.KeyHome}, ctrl('d'), {Code: tea.KeyDelete}}, "rv/demo app"},
		{"alt with another letter types it", []tea.KeyPressMsg{alt('x')}, "/srv/demo appx"},
		{"a paste lands at the cursor without its line breaks", []tea.KeyPressMsg{ctrl('e'), pasteKey("/x\ny")}, "/srv/demo app/xy"},
	} {
		var l textLine
		l.set("/srv/demo app")
		for _, k := range tc.keys {
			if !l.edit(k) {
				t.Errorf("%s: %v is not an edit", tc.name, k)
			}
		}
		if got := l.value(); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	var l textLine
	if l.edit(tea.KeyPressMsg{Code: tea.KeyUp}) || l.edit(tea.KeyPressMsg{Code: tea.KeyF2}) {
		t.Error("a key that edits nothing counts as an edit")
	}
}

// The view of a long line scrolls so the cursor stays in view and never draws more than its width.
func TestTheTextLineScrollsToKeepTheCursorInView(t *testing.T) {
	ui := testUI()
	var l textLine
	l.set(strings.Repeat("a", 30) + "END")
	view := l.view(ui, 10, true, "")
	if !strings.HasSuffix(ansi.Strip(strings.ReplaceAll(view, cursorMark, "")), "END") ||
		ansi.StringWidth(strings.ReplaceAll(view, cursorMark, "")) > 9 || !strings.HasSuffix(view, cursorMark) {
		t.Errorf("view %q", view)
	}
	l.edit(tea.KeyPressMsg{Code: tea.KeyHome})
	if view := l.view(ui, 10, true, ""); !strings.HasPrefix(view, cursorMark+"aaaa") {
		t.Errorf("after home the view is %q", view)
	}
	var empty textLine
	if got := ansi.Strip(empty.view(ui, 20, false, "Type a path")); got != "Type a path" {
		t.Errorf("empty line shows %q, want the placeholder", got)
	}
}
