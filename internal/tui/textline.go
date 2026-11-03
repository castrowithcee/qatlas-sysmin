package tui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// textLine is a one-line text input of the editor where the kit offers none: the path line, the filter of a
// pick list and the line of the key editor. It keeps the usual editing keys of a text field. Its view scrolls
// sideways so the cursor stays in view; the text itself is never cut. Only printable characters enter it.
type textLine struct {
	runes  []rune
	cursor int
	offset int
}

func (t *textLine) value() string { return string(t.runes) }

// set makes s the text, with the cursor at its end.
func (t *textLine) set(s string) {
	t.runes, t.offset = nil, 0
	t.cursor = 0
	t.insert(s)
}

// insert puts the printable characters of s at the cursor and reports whether one was taken.
func (t *textLine) insert(s string) bool {
	var add []rune
	for _, r := range s {
		if unicode.IsPrint(r) {
			add = append(add, r)
		}
	}
	if len(add) == 0 {
		return false
	}
	t.runes = append(t.runes[:t.cursor], append(add, t.runes[t.cursor:]...)...)
	t.cursor += len(add)
	return true
}

// cut removes the runes [from, to) and leaves the cursor at from.
func (t *textLine) cut(from, to int) {
	from, to = max(min(from, len(t.runes)), 0), max(min(to, len(t.runes)), 0)
	if from < to {
		t.runes = append(t.runes[:from], t.runes[to:]...)
	}
	t.cursor = from
}

// wordLeft is the start of the word before the cursor, wordRight the end of the word after it.
func (t *textLine) wordLeft() int {
	i := t.cursor
	for i > 0 && unicode.IsSpace(t.runes[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(t.runes[i-1]) {
		i--
	}
	return i
}

func (t *textLine) wordRight() int {
	i := t.cursor
	for i < len(t.runes) && unicode.IsSpace(t.runes[i]) {
		i++
	}
	for i < len(t.runes) && !unicode.IsSpace(t.runes[i]) {
		i++
	}
	return i
}

// edit applies a key press that types, pastes, moves or deletes, and reports whether it was one. The keys are
// those of every text field: the arrows, home and end, ctrl+a, ctrl+e, ctrl+b and ctrl+f, the word moves on
// alt or ctrl, backspace, delete and ctrl+d, ctrl+w and alt+backspace, alt+d and alt+delete, ctrl+k and ctrl+u.
func (t *textLine) edit(key tea.KeyPressMsg) bool {
	if content, ok := pasted(key); ok {
		t.insert(content)
		return true
	}
	ctrl, alt := key.Mod == tea.ModCtrl, key.Mod == tea.ModAlt
	switch {
	case key.Code == tea.KeyLeft && key.Mod == 0, ctrl && key.Code == 'b':
		t.cursor = max(t.cursor-1, 0)
	case key.Code == tea.KeyRight && key.Mod == 0, ctrl && key.Code == 'f':
		t.cursor = min(t.cursor+1, len(t.runes))
	case (ctrl || alt) && key.Code == tea.KeyLeft, alt && key.Code == 'b':
		t.cursor = t.wordLeft()
	case (ctrl || alt) && key.Code == tea.KeyRight, alt && key.Code == 'f':
		t.cursor = t.wordRight()
	case key.Code == tea.KeyHome && key.Mod == 0, ctrl && key.Code == 'a':
		t.cursor = 0
	case key.Code == tea.KeyEnd && key.Mod == 0, ctrl && key.Code == 'e':
		t.cursor = len(t.runes)
	case key.Code == tea.KeyBackspace && key.Mod == 0:
		t.cut(t.cursor-1, t.cursor)
	case key.Code == tea.KeyDelete && key.Mod == 0, ctrl && key.Code == 'd':
		at := t.cursor
		t.cut(at, at+1)
	case ctrl && key.Code == 'w', alt && key.Code == tea.KeyBackspace:
		t.cut(t.wordLeft(), t.cursor)
	case alt && (key.Code == 'd' || key.Code == tea.KeyDelete):
		at := t.cursor
		t.cut(at, t.wordRight())
	case ctrl && key.Code == 'k':
		t.cut(t.cursor, len(t.runes))
	case ctrl && key.Code == 'u':
		t.cut(0, t.cursor)
	case key.Text != "" && key.Mod&^tea.ModShift == 0:
		t.insert(key.Text)
	case key.Text == "" && key.Mod&^tea.ModShift == tea.ModAlt && unicode.IsPrint(key.Code):
		// A terminal that sends alt with a character the line binds nothing to has always typed it.
		r := key.Code
		if key.Mod.Contains(tea.ModShift) {
			r = unicode.ToUpper(r)
		}
		t.insert(string(r))
	default:
		return false
	}
	return true
}

// view draws the line in width cells: while focused with the cursorMark where the terminal cursor belongs, and
// while empty the placeholder in the hint style.
func (t *textLine) view(ui *uiContext, width int, focused bool, placeholder string) string {
	width = max(width, 1)
	mark := ""
	if focused {
		mark = cursorMark
	}
	if len(t.runes) == 0 {
		return mark + ui.st(kittui.StyleHint).Render(truncateCells(placeholder, width))
	}
	cellWidth := func(from, to int) int { return lipgloss.Width(string(t.runes[from:to])) }
	t.offset = min(t.offset, t.cursor)
	// The cell after the text holds the cursor at the end of the line.
	for t.offset < t.cursor && cellWidth(t.offset, t.cursor)+1 > width {
		t.offset++
	}
	room := width
	if focused && t.cursor == len(t.runes) {
		room--
	}
	end := t.offset
	for end < len(t.runes) && cellWidth(t.offset, end+1) <= room {
		end++
	}
	if !focused {
		return string(t.runes[t.offset:end])
	}
	var b strings.Builder
	b.WriteString(string(t.runes[t.offset:t.cursor]))
	b.WriteString(mark)
	if t.cursor < end {
		b.WriteString(string(t.runes[t.cursor:end]))
	}
	return b.String()
}
