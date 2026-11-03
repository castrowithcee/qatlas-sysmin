package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// testKeys is the key table with its default keys.
func testKeys() *kittui.Keymap { return testUI().keys }

// lookUI is the context of an editor in the theme of a name, for a terminal of the colour profile.
func lookUI(profile kittui.ColorProfile, theme string) *uiContext {
	u := newUIContext(profile, kittui.CharsetUnicode)
	u.setTheme(theme)
	return u
}

// useProfile draws m for a terminal of the colour profile from now on.
func useProfile(m *Model, profile kittui.ColorProfile) {
	m.ui.profile = profile
	m.ui.setTheme(m.ui.theme)
}

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
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
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
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
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

// screenOf is the workspace with the footer under it, as the terminal shows it, without the header.
func screenOf(m *Model) string {
	if m.quitting || m.terminalTooSmall() {
		return m.render()
	}
	m.screenKeys, m.screenPos = nil, ""
	return withFooter(m, m.workspaceView())
}

// withFooter is a view drawn by a single screen function with the footer under it, which the keys of that
// screen went to.
func withFooter(m *Model, view string) string {
	rows := strings.Split(m.frame(view), "\n")
	return m.ui.shown(view + "\n" + rows[len(rows)-1])
}

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

// fillBase fills the bare list with names in their order, each with its cells or, where cells has none, its
// name alone, grouped by group where that is not nil.
func fillBase(m *Model, cols []kittui.Column, cells map[string][]string, names []string, group func(string) string) {
	rows := make([]dataRow, 0, len(names))
	for _, n := range names {
		c := cells[n]
		if c == nil {
			c = make([]string, len(cols))
			c[0] = n
		}
		g := ""
		if group != nil {
			g = group(n)
		}
		rows = append(rows, dataRow{name: n, cells: c, group: g})
	}
	m.base.fill(m.ui, listConfig{cols: cols, empty: tListEmpty, grouped: group != nil}, rows)
}

// markName marks the row name of p and reports whether it shows; a folded group of it opens.
func markName(p *dataPage, name string) bool { return p.list.Select(rowID(name)) }

// markGroup marks the group row n of p, counted from 0 in the order they show.
func markGroup(p *dataPage, n int) bool {
	p.list.Act(actHome)
	for range p.entries() + 16 {
		if _, ok := p.current(); !ok {
			if n == 0 {
				return true
			}
			n--
		}
		p.list.Act(actDown)
	}
	return false
}

// marked is the name of the marked row of p, or "group" on a group row.
func marked(p *dataPage) string {
	if name, ok := p.current(); ok {
		return name
	}
	return "group"
}

// logRow is the place of the marked entry among the entries of the log read, -1 where none is marked.
func logRow(m *Model) int {
	name, ok := m.act.page.current()
	if !ok {
		return -1
	}
	for i, id := range m.act.order {
		if id == name {
			return i
		}
	}
	return -1
}

// baseRow is the place of the marked entry of the bare list among alpha, bravo, charlie and delta.
func baseRow(m *Model) int {
	name, _ := m.selected()
	return map[string]int{"alpha": 0, "bravo": 1, "charlie": 2, "delta": 3}[name]
}
