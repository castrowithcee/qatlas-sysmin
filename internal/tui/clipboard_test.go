package tui

import (
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
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
	markName(&m.base, "charlie")
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
	for lang, want := range map[string]string{kitprefs.LanguageEnglish: "Handed to the terminal", kitprefs.LanguageGerman: "An das Terminal"} {
		m := frameModel(t, 100, 30)
		m.ui.setLanguage(kitprefs.Language(lang))
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
	if q := m.base.list.Query(); q != "c" {
		t.Errorf("the filter = %q, want c typed", q)
	}

	m = frameModel(t, 100, 30)
	fillBase(m, frameColumns(), nil, nil, nil)
	if _, ok := pressCopy(m); ok || m.copyKeyShown() {
		t.Error("an empty list copies or names the copy key")
	}

	m = frameModel(t, 100, 30)
	askSure(t, m)
	if _, ok := pressCopy(m); ok {
		t.Error("c copied while a question is open")
	}

	m, _ = settingsModel(t, newFake(), "general")
	send(m, "enter")
	if !m.set.gen.Open() || m.copySource() != nil {
		t.Error("an open list of values offers a copy source")
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
	fillBase(m, frameColumns(), nil, []string{"leaky-canary-demo-secret"}, nil)
	m.redactor.Add("canary-demo-secret")
	got, ok := pressCopy(m)
	if !ok || strings.Contains(got, "canary-demo-secret") {
		t.Fatalf("the copy = %q (%v) holds the registered secret", got, ok)
	}
}

func TestWithCopyKeyPutsTheGroupAheadOfHelpAndCommands(t *testing.T) {
	m := frameModel(t, 80, 24)
	for _, c := range []struct {
		in   []kittui.Hint
		want string
	}{
		{keyLine(navHint(tHintMove, actUp, actDown), hint(tHintHelp, actHelp)), "↑/↓ move · c copy · ? help"},
		{keyLine(hint(tHintOpen, actEnter), hint(tHintCmds, actCommand), hint(tHintHelp, actHelp)), "enter open · c copy · : cmds · ? help"},
		{navHint(tHintMove, actUp, actDown), "↑/↓ move · c copy"},
		{keyLine(hint(tHintOpen, actEnter), hint(tHintCopy, actCopy), hint(tHintHelp, actHelp)), "enter open · c copy · ? help"},
	} {
		if got := m.hintText(withCopyKey(c.in)); got != c.want {
			t.Errorf("withCopyKey(%q) = %q, want %q", m.hintText(c.in), got, c.want)
		}
	}
}
