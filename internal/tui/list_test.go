package tui

import (
	"fmt"
	"testing"
)

func numberedServices(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("svc-%03d", i)
	}
	return names
}

func selectedName(t *testing.T, m *Model) string {
	t.Helper()
	name, ok := m.selected()
	if !ok {
		t.Fatalf("nothing is selected; shown %v", m.list.matches)
	}
	return name
}

// The window always holds the selection and never more lines than it has room for, however the rows wrap.
func TestTheWindowHoldsTheSelection(t *testing.T) {
	l := newFilterList(func(name string) string { return name })
	l.reset(numberedServices(50))
	height := func(i int) int { return 1 + i%3 }
	for _, room := range []int{3, 4, 7, 20} {
		for _, by := range []int{1, 1, 1, 7, -3, 25, -40, 1, -1, -1} {
			l.move(by)
			start, end := l.window(room, height)
			if l.cursor < start || l.cursor >= end {
				t.Fatalf("room %d: cursor %d outside [%d, %d)", room, l.cursor, start, end)
			}
			used := 0
			for i := start; i < end; i++ {
				used += height(i)
			}
			if used > room && end-start > 1 {
				t.Fatalf("room %d: window [%d, %d) takes %d lines", room, start, end, used)
			}
			l.offset = start
		}
	}
}
