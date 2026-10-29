package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// keyMsg is the key press a test names the way the footers do.
func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+l":
		return tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	default:
		return textKey(k)
	}
}

// press sends key events the way a terminal would.
func press(t *testing.T, m *Model, keys ...string) {
	t.Helper()
	for _, k := range keys {
		m.Update(keyMsg(k))
	}
}

// typeText enters a value into the focused field, one rune at a time.
func typeText(t *testing.T, m *Model, text string) {
	t.Helper()
	for _, r := range text {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// sizedModel gives m the size of a terminal.
func sizedModel(m *Model, w, h int) *Model {
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// restoreRenderer puts the theme, the colour profile and the terminal colours back when the test ends.
func restoreRenderer(t *testing.T) {
	t.Helper()
	fg, bg, profile, saved := termFg, termBg, currentProfile(), styles
	t.Cleanup(func() {
		termFg, termBg = fg, bg
		setProfile(profile)
		setTheme(saved)
	})
}

// screenOf is the workspace with the footer under it, as the terminal shows it, without the header.
func screenOf(m *Model) string {
	if m.quitting || m.terminalTooSmall() {
		return m.render()
	}
	m.screenKeys, m.screenPos = "", ""
	return withFooter(m, m.workspaceView())
}

// withFooter is a view drawn by a single screen function with the footer under it, which the keys of that
// screen went to.
func withFooter(m *Model, view string) string { return shown(view + "\n" + m.footerLine()) }

// flatText is a view as one line of words, where the spacing between groups of keys does not matter.
func flatText(view string) string { return strings.Join(strings.Fields(view), " ") }

// assertViewFits fails the test when view is taller or wider than the terminal.
func assertViewFits(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		t.Errorf("view has %d lines, want at most %d:\n%s", len(lines), height, view)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Errorf("view line has %d cells, want at most %d: %q", got, width, line)
		}
	}
}
