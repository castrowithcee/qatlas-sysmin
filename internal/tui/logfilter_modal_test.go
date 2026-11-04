package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// In a narrow terminal the bar is a button, and its modal takes every key and click until it is answered.
func TestActivityFilterModalIsOperableAt40Columns(t *testing.T) {
	utcClock(t)
	for _, height := range []int{24, 14} {
		fb := activityFake()
		m := appModel(t, fb, 40, height)
		send(m, "3")
		if !strings.Contains(plain(m), "Filter (0)") {
			t.Fatalf("height %d: the narrow bar is no button:\n%s", height, plain(m))
		}
		send(m, "f", "enter")
		if !m.barModal() {
			t.Fatalf("height %d: enter on the button opens no modal:\n%s", height, plain(m))
		}
		view := plain(m)
		for _, want := range []string{"Period:", "Action:", "Result:", "enter apply", "esc discard"} {
			if !strings.Contains(view, want) {
				t.Errorf("height %d: the modal lacks %q:\n%s", height, want, view)
			}
		}
		// Keys the screen reads elsewhere stay with the modal.
		before := fb.count("log")
		send(m, "q", "?", "down", "down", "right")
		if m.quitting || m.pop != nil || !m.barModal() || fb.count("log") != before {
			t.Fatalf("height %d: the modal does not take every key: quit %v help %v", height, m.quitting, m.pop != nil)
		}
		send(m, "enter")
		if m.barModal() || m.act.filter.result != control.LogResultSuccess || fb.count("log") != before+1 {
			t.Errorf("height %d: apply: modal %v result %q reads %d", height, m.barModal(), m.act.filter.result, fb.count("log")-before)
		}
		send(m, "enter", "down", "right", "esc")
		if m.barModal() || m.act.filter.result != control.LogResultSuccess || fb.count("log") != before+1 {
			t.Errorf("height %d: discard changed the filter: result %q", height, m.act.filter.result)
		}
	}
}

// A click on a value of the modal takes the next one, and a click on its key line answers it.
func TestActivityFilterModalTakesClicks(t *testing.T) {
	utcClock(t)
	// 40x30 holds the modal over the dimmed screen, 40x14 in the workspace.
	for _, height := range []int{30, 14} {
		fb := activityFake()
		m := appModel(t, fb, 40, height)
		send(m, "3")
		m.render()
		clickOn(t, m, "Filter (0)")
		if !m.barModal() {
			t.Fatalf("a click on the button opens no modal:\n%s", plain(m))
		}
		before := fb.count("log")
		clickOn(t, m, "All ▾") // the value of the first row, the period
		x, y := cellOf(t, m, "apply")
		_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		drain(m, cmd)
		if m.barModal() || m.act.filter.period == 0 || fb.count("log") != before+1 {
			t.Errorf("clicks did not take the value: modal %v period %d, %d reads", m.barModal(), m.act.filter.period, fb.count("log")-before)
		}
	}
}

// The keys of the footer act only where they answer a dialog, however many rows the footer takes: the upper row
// of a wrapped footer is no row of the screen to press.
func TestFooterOfTwoRowsActsOnlyAsTheButtonsOfADialog(t *testing.T) {
	m := appModel(t, activityFake(), 40, 24)
	send(m, "3")
	m.render()
	if m.footerRows() != 2 {
		t.Fatalf("the footer takes %d rows, want 2:\n%s", m.footerRows(), plain(m))
	}
	clickOn(t, m, "enter details")
	if m.act.page.open {
		t.Errorf("a click on a key of the upper footer row opened the details:\n%s", plain(m))
	}
	clickOn(t, m, "f filter")
	if m.zoneNow() == zoneFilters {
		t.Errorf("a click on the filter key of the footer reached the bar")
	}

	help := helpModels(t, 60, 14)["area 60x14"]
	help.render()
	if help.footerRows() != 2 {
		t.Fatalf("the footer of the help takes %d rows, want 2:\n%s", help.footerRows(), plain(help))
	}
	topic := help.pop.help.Topic()
	clickOn(t, help, "topic")
	if help.pop == nil || help.pop.help.Topic() == topic {
		t.Errorf("a click on a key of the upper footer row of the help did nothing:\n%s", plain(help))
	}
	clickOn(t, help, "close")
	if help.pop != nil {
		t.Errorf("a click on a key of the lower footer row of the help did not close it:\n%s", plain(help))
	}
}
