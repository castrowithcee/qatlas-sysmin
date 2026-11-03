package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

var cursorProfiles = map[string]kittui.ColorProfile{
	"truecolor": kittui.ProfileTrueColor, "16": kittui.ProfileANSI16, "none": kittui.ProfileNone,
}

// cursorBefore draws the frame and returns the text left of the terminal cursor, read from the rendered image.
// It fails when the view sets no cursor or no blinking one.
func cursorBefore(t *testing.T, m *Model) string {
	t.Helper()
	frame := m.render()
	if strings.Contains(frame, cursorMark) {
		t.Fatal("the frame still carries the cursor mark")
	}
	c := m.View().Cursor
	if c == nil {
		t.Fatalf("no cursor set:\n%s", ansi.Strip(frame))
	}
	if !c.Blink {
		t.Error("the cursor does not blink")
	}
	lines := strings.Split(ansi.Strip(frame), "\n")
	if c.Y >= len(lines) {
		t.Fatalf("cursor row %d outside the %d rows drawn", c.Y, len(lines))
	}
	return strings.TrimRight(ansi.Cut(lines[c.Y], 0, c.X), " ")
}

// A kit form draws the cursor of its text field itself, so the terminal shows no second one.
func TestTheFormDrawsTheCursorOfItsTextFieldItself(t *testing.T) {
	for name, p := range cursorProfiles {
		m, _, _, _ := userModel(t, 120, 40)
		useProfile(m, p)
		send(m, "n", "tab")
		typed(m, "demo-app")
		if c := m.View().Cursor; c != nil {
			t.Errorf("%s: terminal cursor at %v beside the cursor the form draws", name, *c)
		}
		if p == kittui.ProfileNone && !strings.Contains(plain(m), "demo-app_") {
			t.Errorf("%s: no cursor after the value:\n%s", name, plain(m))
		}
	}
}

// The key editor of a new person is a line of the editor, with the terminal cursor in it.
func TestTheCursorStandsInTheLineOfTheKeyEditor(t *testing.T) {
	for name, p := range cursorProfiles {
		m, _, _, _ := userModel(t, 120, 40)
		useProfile(m, p)
		send(m, "n")
		for range 7 {
			send(m, "tab")
		}
		send(m, "enter")
		typed(m, "ssh-demo")
		if before := cursorBefore(t, m); !strings.HasSuffix(before, "ssh-demo") {
			t.Errorf("%s: text before the cursor = %q, want it to end with the line", name, before)
		}
	}
}

func TestTheCursorStandsInTheFolderPathInput(t *testing.T) {
	for name, p := range cursorProfiles {
		m, _, _, _ := userModel(t, 120, 40)
		useProfile(m, p)
		runCommand(m, "go host folders")
		send(m, "enter")
		typed(m, "/srv/demo-app")
		if before := cursorBefore(t, m); !strings.HasSuffix(before, "/srv/demo-app") {
			t.Errorf("%s: text before the cursor = %q, want it to end with the path", name, before)
		}
	}
}

func TestNoCursorWithoutAFocusedTextField(t *testing.T) {
	m := appModel(t, overviewFake(), 120, 40)
	if c := m.View().Cursor; c != nil {
		t.Errorf("overview: cursor at %v, want none", *c)
	}
}
