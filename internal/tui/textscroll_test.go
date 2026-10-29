package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func numberedLines(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %03d", i)
	}
	return lines
}

func TestTextScrollMovesByKeysAndStopsAtTheEnds(t *testing.T) {
	var s textScroll
	lines := numberedLines(50)
	rows := s.window(lines, 10, false)
	if len(rows) != 10 || rows[0] != "line 000" || rows[9] != "line 009" {
		t.Fatalf("first window = %q", rows)
	}
	for _, c := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{keyMsg("down"), 1}, {keyMsg("up"), 0}, {keyMsg("up"), 0},
		{tea.KeyPressMsg{Code: tea.KeyPgDown}, 10}, {tea.KeyPressMsg{Code: tea.KeyPgUp}, 0},
		{tea.KeyPressMsg{Code: tea.KeyEnd}, 40}, {tea.KeyPressMsg{Code: tea.KeyDown}, 40},
		{tea.KeyPressMsg{Code: tea.KeyHome}, 0},
	} {
		if !s.scroll(c.key, 10) {
			t.Fatalf("%v is not a scroll key", c.key)
		}
		s.window(lines, 10, false)
		if s.offset() != c.want {
			t.Fatalf("after %v the offset is %d, want %d", c.key, s.offset(), c.want)
		}
	}
	if s.scroll(keyMsg("x"), 10) {
		t.Error("a letter counts as a scroll key")
	}
}

func TestTextScrollKeepsRowsAsTheyWereAndClampsAfterAResize(t *testing.T) {
	var s textScroll
	lines := []string{"short", "a longer line here", "  indented", ""}
	rows := s.window(lines, 10, false)
	if strings.Join(rows, "|") != strings.Join(lines, "|") {
		t.Errorf("rows = %q, want the lines unchanged and unpadded", rows)
	}
	long := numberedLines(50)
	s.window(long, 10, false)
	s.scroll(tea.KeyPressMsg{Code: tea.KeyEnd}, 10)
	s.window(long, 10, false)
	if rows := s.window(long[:12], 10, false); s.offset() > 2 || len(rows) == 0 {
		t.Errorf("after the text shrank the offset is %d with %d rows", s.offset(), len(rows))
	}
	if rows := s.window(nil, 10, false); rows != nil {
		t.Errorf("an empty text has rows %q", rows)
	}
}

func TestTextScrollFollowsTheEndAndResetsToTheTop(t *testing.T) {
	var s textScroll
	lines := numberedLines(100)
	rows := s.window(lines, 10, true)
	if rows[len(rows)-1] != "line 099" {
		t.Fatalf("follow does not show the end: %q", rows)
	}
	lines = append(lines, "line 100")
	if rows := s.window(lines, 10, true); rows[len(rows)-1] != "line 100" {
		t.Errorf("a new line is not followed: %q", rows)
	}
	s.reset()
	if s.offset() != 0 {
		t.Errorf("reset left the offset at %d", s.offset())
	}
}

func TestTextScrollPositionIsEmptyWhileEverythingFits(t *testing.T) {
	var s textScroll
	if got := s.position(5, 5); got != "" {
		t.Errorf("position = %q, want none", got)
	}
	s.window(numberedLines(50), 10, false)
	s.scroll(tea.KeyPressMsg{Code: tea.KeyPgDown}, 10)
	s.window(numberedLines(50), 10, false)
	if got := s.position(50, 10); got != "lines 11-20 of 50" {
		t.Errorf("position = %q", got)
	}
}

func TestHalfPageIsAtLeastOneLine(t *testing.T) {
	for in, want := range map[int]int{0: 1, 1: 1, 2: 1, 9: 4, 20: 10} {
		if got := halfPage(in); got != want {
			t.Errorf("halfPage(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestPaneScrollFitsTheBodyAndTheWheelMovesThreeLines(t *testing.T) {
	var p paneScroll
	body := numberedLines(30)
	rows, position := p.fit("one", body, 10)
	if len(rows) != 9 || position == "" || !p.more || p.page != 9 {
		t.Fatalf("fit gave %d rows, position %q, more %v", len(rows), position, p.more)
	}
	p.wheel(false)
	rows, _ = p.fit("one", body, 10)
	if rows[0] != "line 003" {
		t.Errorf("one notch down starts at %q, want line 003", rows[0])
	}
	p.wheel(true)
	p.wheel(true)
	if rows, _ = p.fit("one", body, 10); rows[0] != "line 000" {
		t.Errorf("the wheel up past the top starts at %q", rows[0])
	}
	p.wheel(false)
	if rows, _ = p.fit("two", body, 10); rows[0] != "line 000" {
		t.Errorf("another subject keeps the position: %q", rows[0])
	}
	// A body that fits is shown whole and the pane takes no wheel.
	rows, position = p.fit("two", body[:5], 10)
	if len(rows) != 5 || position != "" || p.more {
		t.Errorf("a body that fits: %d rows, position %q, more %v", len(rows), position, p.more)
	}
	// A pane of a single row only cuts.
	if rows, _ = p.fit("two", body, 1); len(rows) != len(body) || p.more {
		t.Errorf("a one-row pane scrolls: %d rows, more %v", len(rows), p.more)
	}
}

func TestPaneScrollAtReportsOnlyWhereItWasDrawn(t *testing.T) {
	p := paneScroll{x0: 10, y0: 5, x1: 30, y1: 15, more: true}
	for _, c := range []struct {
		x, y int
		want bool
	}{{10, 5, true}, {29, 14, true}, {30, 14, false}, {9, 5, false}, {10, 15, false}} {
		if got := p.at(c.x, c.y); got != c.want {
			t.Errorf("at(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
	p.more = false
	if p.at(12, 8) {
		t.Error("a pane with nothing left out takes the wheel")
	}
}

func TestPageKeyNamesTheJumpKeysOnly(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyPgUp}, {Code: tea.KeyPgDown}, {Code: tea.KeyHome}, {Code: tea.KeyEnd}} {
		if !pageKey(k) {
			t.Errorf("%v is not a page key", k)
		}
	}
	if pageKey(keyMsg("down")) || pageKey(keyMsg("x")) {
		t.Error("a line key counts as a page key")
	}
}
