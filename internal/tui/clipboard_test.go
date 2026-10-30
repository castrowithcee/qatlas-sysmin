package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// clipboardOf is the text a command hands to the terminal, and whether it is a clipboard command at all.
func clipboardOf(cmd tea.Cmd) (string, bool) {
	if cmd == nil {
		return "", false
	}
	msg := cmd()
	if v := reflect.ValueOf(msg); v.Kind() == reflect.String && reflect.TypeOf(msg).Name() == "setClipboardMsg" {
		return v.String(), true
	}
	return "", false
}

// pressCopy presses the copy key and returns what it handed to the terminal.
func pressCopy(m *Model) (string, bool) {
	_, cmd := m.Update(textKey("c"))
	return clipboardOf(cmd)
}

func noControl(t *testing.T, where, text string) {
	t.Helper()
	for _, r := range text {
		if r != '\n' && r < ' ' || r == 0x7f || r == 0x1b {
			t.Errorf("%s holds the control character %q", where, r)
			return
		}
	}
}

func TestCopyHandsOverTheNameOfTheMarkedRow(t *testing.T) {
	m := frameModel(t, 100, 30)
	m.list.selectName("charlie")
	got, ok := pressCopy(m)
	if !ok || got != "charlie" {
		t.Fatalf("c copied %q (%v), want the name of the marked row", got, ok)
	}
	if !strings.Contains(m.status, "terminal") {
		t.Errorf("status = %q, want the hand-over to the terminal", m.status)
	}
	if foot := withFooter(m, screenOf(m)); !strings.Contains(foot, "c copy") {
		t.Errorf("the footer does not name c:\n%s", foot)
	}
}

func TestCopyStatusIsAHandOverInBothLanguages(t *testing.T) {
	t.Cleanup(func() { setLanguage(uiprefs.LanguageEnglish) })
	for lang, want := range map[string]string{uiprefs.LanguageEnglish: "Handed to the terminal", uiprefs.LanguageGerman: "An das Terminal"} {
		m := frameModel(t, 100, 30)
		setLanguage(lang)
		if _, ok := pressCopy(m); !ok {
			t.Fatalf("%s: c handed nothing over", lang)
		}
		if !strings.Contains(m.status, want) {
			t.Errorf("%s: status = %q, want %q", lang, m.status, want)
		}
		low := strings.ToLower(m.status)
		if strings.Contains(low, "copied") || strings.Contains(low, "kopiert") {
			t.Errorf("%s: status %q claims the text was copied", lang, m.status)
		}
	}
}

// Where c is a letter, or there is nothing to copy, it copies nothing.
func TestCopyKeyCopiesNothingWhereItIsATextKeyOrNothingIsMarked(t *testing.T) {
	m := frameModel(t, 100, 30)
	press(t, m, "/")
	_, cmd := m.Update(textKey("c"))
	if text, ok := clipboardOf(cmd); ok {
		t.Fatalf("c on the filter line copied %q", text)
	}
	if m.list.query() != "c" {
		t.Errorf("the filter = %q, want c typed", m.list.query())
	}

	m = frameModel(t, 100, 30)
	m.setEntries(nil, nil, nil)
	if _, ok := pressCopy(m); ok || m.copyKeyShown() {
		t.Error("an empty list copies or names the copy key")
	}

	m = frameModel(t, 100, 30)
	m.screen = screenConfirm
	m.ask = &question{title: "Sure?", keys: "y yes · n/{back} no"}
	if _, ok := pressCopy(m); ok {
		t.Error("c copied while a question is open")
	}

	m = frameModel(t, 100, 30)
	m.openDropdown("field", []string{"One", "Two"}, 0, func(int) tea.Cmd { return nil })
	if m.copySource() != nil {
		t.Error("an open dropdown offers a copy source")
	}
}

func TestCopyCleanRemovesEscapesControlsAndRegisteredSecrets(t *testing.T) {
	m := frameModel(t, 100, 30)
	m.redactor.Add("canary-demo-secret")
	for name, c := range map[string]struct{ in, notWant string }{
		"registered secret": {"token canary-demo-secret end", "canary-demo-secret"},
		"colour escape":     {"a\x1b[31mred\x1b[0m", "\x1b"},
		"osc sequence":      {"x\x1b]52;c;AAAA\x07y", "\x1b"},
		"nul and bell":      {"a\x00b\x07c", "\x00"},
		"invalid utf-8":     {"a\xffb", "\xff"},
		"zero width joiner": {"a\u200db", "\u200d"},
	} {
		got := m.copyClean(c.in)
		if strings.Contains(got, c.notWant) {
			t.Errorf("%s: %q still holds %q", name, got, c.notWant)
		}
		noControl(t, name, got)
	}
}

func TestCopyCleanKeepsLineBreaksTrimsAndNeverCuts(t *testing.T) {
	m := frameModel(t, 100, 30)
	if got := m.copyClean("  one\r\ntwo\tthree  \n"); got != "one\ntwo three" {
		t.Errorf("copyClean = %q", got)
	}
	long := strings.Repeat("0123456789", 500)
	if got := m.copyClean(long); got != long {
		t.Errorf("a long text was cut to %d bytes", len(got))
	}
	var nilRedactor Model
	if got := nilRedactor.copyClean("plain"); got != "plain" {
		t.Errorf("without a redactor: %q", got)
	}
}

// The name of a row that holds a registered secret is handed over redacted, never raw.
func TestCopyRedactsASecretInTheNameOfTheRow(t *testing.T) {
	m := frameModel(t, 100, 30)
	m.setEntries([]string{"leaky-canary-demo-secret"}, nil, nil)
	m.redactor.Add("canary-demo-secret")
	got, ok := pressCopy(m)
	if !ok || strings.Contains(got, "canary-demo-secret") {
		t.Fatalf("the copy = %q (%v) holds the registered secret", got, ok)
	}
}

func TestWithCopyKeyPutsTheGroupAheadOfHelpAndCommands(t *testing.T) {
	for in, want := range map[string]string{
		"↑/↓ move · {help} help":                    "↑/↓ move · {copy} copy · {help} help",
		"enter open · {command} cmds · {help} help": "enter open · {copy} copy · {command} cmds · {help} help",
		"↑/↓ move":                      "↑/↓ move · {copy} copy",
		"x · {copy} copy · {help} help": "x · {copy} copy · {help} help",
	} {
		if got := withCopyKey(in); got != want {
			t.Errorf("withCopyKey(%q) = %q, want %q", in, got, want)
		}
	}
}
