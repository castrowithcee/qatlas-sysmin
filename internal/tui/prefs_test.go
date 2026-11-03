package tui

import (
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// writePrefs puts a tui.yaml with the given content into a temporary folder and returns its path.
func writePrefs(t *testing.T, content string) string {
	t.Helper()
	path := kitprefs.Path(t.TempDir())
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A tui.yaml as the editor has written it so far loads without loss: theme, start, icons, language, mouse and
// every rebindable key, and the editor draws and reads keys by it.
func TestPreferencesOfTheCurrentFormatLoadWithoutLoss(t *testing.T) {
	path := writePrefs(t, `theme: qsysmin light
start: overview
icons: none
language: deutsch
mouse: false
keys:
  info: f4
  save: f3
  help: ctrl+l
  command: ctrl+x
`)
	prefs, warnings := kitprefs.Load(path, PrefsSchema())
	want := kitprefs.Prefs{Theme: themeQsysminLight, Start: "overview", Icons: kitprefs.IconsNone,
		Language: kitprefs.LanguageGerman, MouseEnabled: false,
		Keys: map[action]string{actInfo: "f4", actSave: "f3", actHelp: "ctrl+l", actCommand: "ctrl+x"}}
	if len(warnings) != 0 || !reflect.DeepEqual(prefs, want) {
		t.Fatalf("loaded %+v, warnings %v; want %+v", prefs, warnings, want)
	}
	m := New(prefs, nil)
	if m.ui.theme != themeQsysminLight || m.ui.lang != i18n.German || m.mouseMode() != tea.MouseModeNone {
		t.Errorf("theme %q, language %q, mouse mode %v", m.ui.theme, m.ui.lang, m.mouseMode())
	}
	for a, key := range map[action]tea.KeyPressMsg{
		actSave: {Code: tea.KeyF3}, actHelp: {Code: 'l', Mod: tea.ModCtrl}, actCommand: {Code: 'x', Mod: tea.ModCtrl},
		actInfo: {Code: tea.KeyF4},
	} {
		if !m.keyIs(key, a) {
			t.Errorf("%s is not on %v", a, key)
		}
	}
	// What the editor writes back loads as the same preferences.
	if err := kitprefs.Save(path, m.prefs, m.ui.prefsSchema()); err != nil {
		t.Fatal(err)
	}
	if again, warnings := kitprefs.Load(path, PrefsSchema()); len(warnings) != 0 || !reflect.DeepEqual(again, want) {
		t.Errorf("after a save: %+v, warnings %v", again, warnings)
	}
}

// ctrl+g and ctrl+o were free keys once; a multiplexer takes them, so their actions keep the standard key
// with a warning each, and the valid entries stay.
func TestPreferencesWithMultiplexerKeysFallBackToTheStandard(t *testing.T) {
	path := writePrefs(t, "theme: terminal\nlanguage: english\nmouse: true\nkeys:\n  help: ctrl+g\n  command: ctrl+o\n  save: f3\n")
	prefs, warnings := kitprefs.Load(path, PrefsSchema())
	if len(warnings) != 2 || !strings.Contains(strings.Join(warnings, "\n"), "keys.help ctrl+g") ||
		!strings.Contains(strings.Join(warnings, "\n"), "keys.command ctrl+o") {
		t.Fatalf("warnings = %v, want one each for help and command", warnings)
	}
	if !reflect.DeepEqual(prefs.Keys, map[action]string{actSave: "f3"}) {
		t.Fatalf("keys = %v, want only the valid save key", prefs.Keys)
	}
	m := New(prefs, nil)
	if m.keyLabel(actHelp) != "?" || m.keyLabel(actCommand) != ":" || m.keyLabel(actSave) != "F3" {
		t.Errorf("help %q, command %q, save %q", m.keyLabel(actHelp), m.keyLabel(actCommand), m.keyLabel(actSave))
	}
}
