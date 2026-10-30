package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

var cursorProfiles = map[string]colorProfile{"truecolor": profileTrueColor, "16": profile16, "none": profileNone}

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

func TestTheCursorStandsInTheFocusedFormField(t *testing.T) {
	for name, p := range cursorProfiles {
		m := flowModel(t, projectsFake(), 120, 40)
		setProfile(p)
		typed(m, "demo-app")
		if before := cursorBefore(t, m); !strings.HasSuffix(before, "demo-app") {
			t.Errorf("%s: text before the cursor = %q, want it to end with the value", name, before)
		}
	}
}

func TestTheCursorStandsInThePassphraseDialog(t *testing.T) {
	for name, p := range cursorProfiles {
		fb := newFake(control.VaultUnlocked)
		m := appModel(t, fb, 100, 30)
		setProfile(p)
		var done []error
		drain(m, m.requireAdmin(gated(fb, &done)))
		if m.ov == nil {
			t.Fatalf("%s: no passphrase asked", name)
		}
		typed(m, "abc")
		before := cursorBefore(t, m)
		mask := string(m.ov.inputs[0].EchoCharacter)
		if mask == "" || !strings.HasSuffix(before, strings.Repeat(mask, 3)) {
			t.Errorf("%s: text before the cursor = %q, want three mask cells at its end", name, before)
		}
	}
}

func TestTheCursorStandsInTheAdoptPathInput(t *testing.T) {
	for name, p := range cursorProfiles {
		m := projectsModel(t, adoptFake(), 120, 40)
		setProfile(p)
		send(m, "n", "down", "enter", "enter")
		typed(m, "/srv/demo-stack")
		if before := cursorBefore(t, m); !strings.HasSuffix(before, "/srv/demo-stack") {
			t.Errorf("%s: text before the cursor = %q, want it to end with the path", name, before)
		}
	}
}

func TestNoCursorWithoutAFocusedTextField(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 40)
	if c := m.View().Cursor; c != nil {
		t.Errorf("project list: cursor at %v, want none", *c)
	}
}
