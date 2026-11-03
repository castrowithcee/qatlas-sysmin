package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// filterList is the list of values of a pick row of a form: a selection over names that a typed filter can
// narrow and that scrolls inside whatever height its dialog leaves it. It never removes an entry and never changes their order: the filter
// only decides which entries are shown, and clearing it brings every one of them back where it was.
//
// The selection follows a name, not a position. When the entries or the filter change, the same name stays
// selected while it is still shown; otherwise the selection is clamped into what is left.
type filterList struct {
	all     []string
	matches []string
	// text is what the filter looks at for one name: the line the reader sees, so what can be read can be
	// found. It is never a secret, because no line of the editor ever holds one.
	text    func(name string) string
	cursor  int
	offset  int
	input   textLine
	editing bool
}

func newFilterList(text func(string) string) filterList { return filterList{text: text} }

// reset replaces the entries and forgets the filter, the selection and the scroll position.
func (l *filterList) reset(names []string) {
	l.stopFilter()
	l.input.set("")
	l.all, l.matches, l.cursor, l.offset = names, nil, 0, 0
	l.refilter()
}

func (l *filterList) query() string { return strings.TrimSpace(l.input.value()) }

// refilter recomputes the shown entries. Matching ignores case, so a filter finds what it reads like.
func (l *filterList) refilter() {
	previous, hadSelection := l.selected()
	query := strings.ToLower(l.query())
	l.matches = l.matches[:0]
	for _, name := range l.all {
		if query == "" || strings.Contains(strings.ToLower(l.text(name)), query) {
			l.matches = append(l.matches, name)
		}
	}
	if hadSelection && l.selectName(previous) {
		return
	}
	l.cursor = min(l.cursor, max(len(l.matches)-1, 0))
}

func (l *filterList) selected() (string, bool) {
	if l.cursor < 0 || l.cursor >= len(l.matches) {
		return "", false
	}
	return l.matches[l.cursor], true
}

// selectName selects the named entry if it is shown and reports whether it is.
func (l *filterList) selectName(name string) bool {
	for i, match := range l.matches {
		if match == name {
			l.cursor = i
			return true
		}
	}
	return false
}

// move steps through the shown entries and stops at both ends, like every other list of the editor.
func (l *filterList) move(by int) { l.jump(by) }

// jump moves by a page or to an end and stops at the ends.
func (l *filterList) jump(by int) {
	l.cursor = min(max(l.cursor+by, 0), max(len(l.matches)-1, 0))
}

// page handles pgup/pgdown, which move by one screen of the entries the window [start, end) shows, and
// home/end, which move to either end.
func (l *filterList) page(keys *kittui.Keymap, key tea.KeyPressMsg, start, end int) {
	page := max(end-start-1, 1)
	is := func(a action) bool { return keys.Is(key, a, kittui.InputNavigation) }
	switch {
	case is(actPageUp):
		l.jump(-page)
	case is(actPageDown):
		l.jump(page)
	case is(actHome):
		l.jump(-len(l.matches))
	case is(actEnd):
		l.jump(len(l.matches))
	}
}

func (l *filterList) startFilter() {
	l.editing = true
	l.input.cursor = len(l.input.runes)
}

// stopFilter ends typing and keeps the filter.
func (l *filterList) stopFilter() { l.editing = false }

// updateFilter hands one key to the filter being typed and applies the result at once.
func (l *filterList) updateFilter(key tea.KeyPressMsg) {
	before := l.input.value()
	l.input.edit(key)
	if l.input.value() != before {
		l.refilter()
	}
}

// window is the part of the shown entries that fits into room lines, as the range [start, end). lines
// reports how many lines one entry takes, since an entry may wrap. The selected entry is always inside
// the range. The scroll position is kept while it still shows the selection, so moving within the screen
// does not shift it; a selection outside it is brought in at the nearest edge; left-over room is filled
// from above as well as below.
func (l *filterList) window(room int, lines func(i int) int) (int, int) {
	n := len(l.matches)
	if n == 0 {
		return 0, 0
	}
	start := min(max(l.offset, 0), l.cursor)
	end, used := start, 0
	for end <= l.cursor && used <= room {
		used += lines(end)
		end++
	}
	if used > room || end <= l.cursor {
		start, end, used = l.cursor, l.cursor+1, lines(l.cursor)
		for start > 0 && used+lines(start-1) <= room {
			start--
			used += lines(start)
		}
	}
	for end < n && used+lines(end) <= room {
		used += lines(end)
		end++
	}
	for start > 0 && used+lines(start-1) <= room {
		start--
		used += lines(start)
	}
	return start, end
}
