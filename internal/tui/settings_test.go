package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// settingsModel is the interface on the page of Settings, with a preferences file in a temporary directory.
func settingsModel(t *testing.T, fb *fakeBackend, page string) (*Model, string) {
	t.Helper()
	m := appModel(t, fb, 80, 24)
	m.prefsPath = filepath.Join(t.TempDir(), "tui.yaml")
	send(m, "4")
	switch page {
	case "update":
		send(m, "esc", "right", "enter")
	}
	return m, m.prefsPath
}

// generalOrder are the fields of General in the order they stand.
func generalOrder() []kittui.FieldID {
	return []kittui.FieldID{fieldLanguage, fieldTheme, fieldIcons, fieldStart, fieldMouse, keyFieldID(actSave),
		keyFieldID(actHelp), keyFieldID(actCommand), keyFieldID(actInfo)}
}

// genFocus is the place of the focused field of General.
func genFocus(m *Model) int { return slices.Index(generalOrder(), m.set.gen.Focused()) }

// genValue is the value of a field of General as it is edited.
func genValue(m *Model, id kittui.FieldID) kittui.FormValue { return m.set.gen.Values()[id] }

// toField moves the focus of General down to the field id.
func toField(t *testing.T, m *Model, id kittui.FieldID) {
	t.Helper()
	for range len(generalOrder()) {
		if m.set.gen.Focused() == id {
			return
		}
		send(m, "down")
	}
	t.Fatalf("down never reaches %s", id)
}

// choose opens the list of the focused field and takes the value with the given number of steps down.
func choose(t *testing.T, m *Model, steps int) {
	t.Helper()
	send(m, "enter")
	if !m.set.gen.Open() {
		t.Fatal("enter opens no list")
	}
	for range steps {
		send(m, "down")
	}
	send(m, "enter")
}

func TestSettingsOpensOnGeneralWithTheThreePages(t *testing.T) {
	m, _ := settingsModel(t, newFake(), "general")
	// The framed fields scroll where the height is short, so the nine fields need a tall screen.
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	view := plain(m)
	for _, want := range []string{"[General]", "Update", "Language", "Theme", "Icons", "Start view", "Mouse", "Save key"} {
		if !strings.Contains(view, want) {
			t.Errorf("the General page lacks %q:\n%s", want, view)
		}
	}
	send(m, "esc", "right")
	if m.set.sub != subUpdate {
		t.Errorf("right leads to page %d, want Update", m.set.sub)
	}
	send(m, "2", "4")
	if m.set.sub != subGeneral {
		t.Errorf("Settings reopens on page %d, want General", m.set.sub)
	}
}

func TestGeneralSavesToTheFileWithoutTheServiceAndActsAtOnce(t *testing.T) {
	fb := newFake()
	m, path := settingsModel(t, fb, "general")
	calls := len(fb.log())
	choose(t, m, 1) // language: deutsch
	toField(t, m, fieldMouse)
	choose(t, m, 1) // mouse: off
	if m.prefs.Language != kitprefs.LanguageEnglish {
		t.Fatal("a choice acts before it is saved")
	}
	send(m, "f2")
	prefs, warnings := kitprefs.Load(path, PrefsSchema())
	if len(warnings) > 0 || prefs.Language != kitprefs.LanguageGerman || prefs.MouseEnabled {
		t.Errorf("saved %+v, warnings %v; want German with the mouse off", prefs, warnings)
	}
	if m.prefs.Language != kitprefs.LanguageGerman || m.prefs.MouseEnabled {
		t.Errorf("the saved choices do not act: %+v", m.prefs)
	}
	if view := plain(m); !strings.Contains(view, "Hilfe") || !strings.Contains(view, "Auf diesem Gerät gespeichert.") {
		t.Errorf("the language does not change at once:\n%s", view)
	}
	if m.set.gen.Focused() != fieldMouse || m.set.gen.Changed() {
		t.Errorf("after the save the form stands on %s, changed %v", m.set.gen.Focused(), m.set.gen.Changed())
	}
	for _, c := range fb.log()[calls:] {
		if c != "status" {
			t.Errorf("saving the preferences called the service: %v", fb.log()[calls:])
		}
	}
}

func TestGeneralChoicesStepWithLeftAndRightAndEnterNeverSaves(t *testing.T) {
	m, path := settingsModel(t, newFake(), "general")
	send(m, "right")
	if got := genValue(m, fieldLanguage).Choice; got != kitprefs.LanguageGerman {
		t.Errorf("right on the language = %q, want the next value", got)
	}
	send(m, "left", "left")
	if got := genValue(m, fieldLanguage).Choice; got != kitprefs.LanguageSystem {
		t.Errorf("left past the first value = %q, want it to wrap to the last", got)
	}
	send(m, "enter", "esc")
	if m.set.gen.Open() || genValue(m, fieldLanguage).Choice != kitprefs.LanguageSystem {
		t.Error("esc in the list of values does not close it unchanged")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a key other than the save key wrote the preferences")
	}
}

func TestGeneralPicksTheThemeFromAList(t *testing.T) {
	m, path := settingsModel(t, newFake(), "general")
	send(m, "down")
	choose(t, m, 1)
	want := m.ui.themes.Names()[1]
	if got := string(genValue(m, fieldTheme).Choice); got != want {
		t.Fatalf("theme = %q, want %q", got, want)
	}
	send(m, "f2")
	if prefs, _ := kitprefs.Load(path, PrefsSchema()); prefs.Theme != want || m.prefs.Theme != want || m.ui.theme != want {
		t.Errorf("saved theme %q, model %q", prefs.Theme, m.prefs.Theme)
	}
}

// The start view and the icons are saved in tui.yaml: the icons are in force at once, the start view opens
// the next start.
func TestGeneralSavesTheStartViewAndTheIconsAndBothAct(t *testing.T) {
	m, path := settingsModel(t, newFake(), "general")
	toField(t, m, fieldIcons)
	choose(t, m, 1) // icons: nerdfont
	toField(t, m, fieldStart)
	choose(t, m, 1) // start view: Host
	if m.prefs.Icons != kitprefs.IconsUnicode || m.prefs.Start != "overview" {
		t.Fatalf("the choices act before they are saved: %+v", m.prefs)
	}
	send(m, "f2")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"start: host", "icons: nerdfont"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("tui.yaml lacks %q:\n%s", want, data)
		}
	}
	if m.prefs.Icons != kitprefs.IconsNerdFont || m.area != areaSettings {
		t.Errorf("after the save: icons %q in force, area %v; want nerdfont and Settings kept", m.prefs.Icons, m.area)
	}
	prefs, warnings := kitprefs.Load(path, PrefsSchema())
	if len(warnings) > 0 || prefs.Start != "host" || prefs.Icons != kitprefs.IconsNerdFont {
		t.Fatalf("loaded %+v, warnings %v", prefs, warnings)
	}
	next := testModel(prefs, nil)
	next.enableAreas(newFake())
	if next.area != areaHost || next.zone != zoneTabs {
		t.Errorf("the next start opens on %v with the focus on %v, want Host and the tab line", next.area, next.zone)
	}
	reopened, _ := settingsModel(t, newFake(), "general")
	reopened.prefs = prefs
	reopened.set.loaded = false
	reopened.loadForms()
	if genValue(reopened, fieldStart).Choice != "host" || genValue(reopened, fieldIcons).Choice != kitprefs.IconsNerdFont {
		t.Errorf("General does not show what is saved: %+v", reopened.set.gen.Values())
	}
}

func TestGeneralRebindsAKeyAndRefusesAnInvalidOne(t *testing.T) {
	m, path := settingsModel(t, newFake(), "general")
	toField(t, m, keyFieldID(actSave))
	send(m, "enter")
	if !m.set.gen.Capturing() || m.mouseEnabled() {
		t.Fatal("enter does not wait for the new key, or the mouse stays on meanwhile")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
	send(m, "f2")
	if _, err := kitprefs.Load(path, PrefsSchema()); err != nil {
		t.Fatal(err)
	}
	if view := plain(m); !strings.Contains(view, "[!]") || !strings.Contains(view, "F1") {
		t.Errorf("no cause under the field:\n%s", view)
	}
	if m.keyLabel(actSave) != "F2" {
		t.Errorf("an invalid key was applied: %s", m.keyLabel(actSave))
	}
	send(m, "enter")
	m.Update(tea.KeyPressMsg{Code: tea.KeyF3})
	send(m, "f3") // the old key still saves; the new one is not applied yet
	if m.keyLabel(actSave) != "F2" || genValue(m, keyFieldID(actSave)).Text != "f3" {
		t.Errorf("before the save: key %q, field %q", m.keyLabel(actSave), genValue(m, keyFieldID(actSave)).Text)
	}
	send(m, "f2")
	if m.keyLabel(actSave) != "F3" {
		t.Errorf("the save key = %q, want F3", m.keyLabel(actSave))
	}
	prefs, _ := kitprefs.Load(path, PrefsSchema())
	if prefs.Keys[actSave] != "f3" {
		t.Errorf("saved key = %q", prefs.Keys[actSave])
	}
	send(m, "d")
	if got := genValue(m, keyFieldID(actSave)).Text; got != "f2" {
		t.Errorf("d on the key field = %q, want the standard key", got)
	}
}

func TestAKeyFieldTakesTheNextKeyWhateverItMeansElsewhere(t *testing.T) {
	m, _ := settingsModel(t, newFake(), "general")
	toField(t, m, keyFieldID(actInfo))
	for _, key := range []string{"?", "q", ":", "2"} {
		send(m, "enter", key)
		if m.screen != screenArea || m.pop != nil || m.quitting || m.area != areaSettings {
			t.Fatalf("%s did something else than become the key", key)
		}
		if got := genValue(m, keyFieldID(actInfo)).Text; got != key {
			t.Errorf("field = %q, want %q", got, key)
		}
	}
	send(m, "enter", "esc")
	if m.set.gen.Capturing() || genValue(m, keyFieldID(actInfo)).Text != "2" {
		t.Error("esc does not end the wait for a key without one")
	}
}

func releases(info ReleaseInfo, err error, calls *int) ReleaseCheck {
	return func(ctx context.Context) (ReleaseInfo, error) {
		*calls++
		return info, err
	}
}

func TestUpdatePageShowsTheLookupOfThisRun(t *testing.T) {
	for _, c := range []struct {
		name string
		info ReleaseInfo
		err  error
		want []string
	}{
		{"available", ReleaseInfo{Latest: "v0.0.1-test"}, nil, []string{"v0.0.1-test available", "Press U to install it."}},
		{"current", ReleaseInfo{}, nil, []string{"up to date"}},
		{"dev", ReleaseInfo{Dev: true}, nil, []string{"development build"}},
		{"failed", ReleaseInfo{}, errors.New("no network"), []string{"check failed"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var calls int
			m := appModel(t, newFake(), 80, 24)
			m.version, m.releases = "0.0.0-test", releases(c.info, c.err, &calls)
			send(m, "4", "esc", "right", "enter")
			view := plain(m)
			if calls != 1 || !strings.Contains(view, "0.0.0-test") {
				t.Errorf("lookups %d:\n%s", calls, view)
			}
			for _, want := range c.want {
				if !strings.Contains(view, want) {
					t.Errorf("the page lacks %q:\n%s", want, view)
				}
			}
			if strings.Contains(view, "no network") {
				t.Errorf("the page repeats the cause of the failure:\n%s", view)
			}
			send(m, "esc", "left", "right", "enter")
			if calls != 1 {
				t.Errorf("reopening the page looked up again (%d)", calls)
			}
			send(m, "t")
			if calls != 2 {
				t.Errorf("t did not look up again (%d)", calls)
			}
		})
	}
}

func TestUpdatePageWithoutALookupShowsNoData(t *testing.T) {
	m, _ := settingsModel(t, newFake(), "update")
	if view := plain(m); !strings.Contains(view, "no data") {
		t.Errorf("no state:\n%s", view)
	}
	send(m, "t")
}

func TestLeavingUnsavedInputAsksFirst(t *testing.T) {
	m, path := settingsModel(t, newFake(), "general")
	choose(t, m, 1)
	send(m, "1")
	if m.screen != screenConfirm || m.area != areaSettings {
		t.Fatalf("screen %v area %v: no question before leaving unsaved input", m.screen, m.area)
	}
	if view := plain(m); !strings.Contains(view, "Unsaved changes") || !strings.Contains(view, "d discard") {
		t.Errorf("the question lacks its answers:\n%s", view)
	}
	send(m, "esc")
	if m.screen != screenArea || m.area != areaSettings || genValue(m, fieldLanguage).Choice != kitprefs.LanguageGerman {
		t.Error("esc does not keep editing")
	}
	send(m, "1", "d")
	if m.area != areaOverview {
		t.Error("d does not discard and leave")
	}
	send(m, "4")
	if genValue(m, fieldLanguage).Choice != kitprefs.LanguageEnglish {
		t.Error("the discarded choice came back")
	}
	choose(t, m, 1)
	send(m, "q", "f2")
	if prefs, _ := kitprefs.Load(path, PrefsSchema()); prefs.Language != kitprefs.LanguageGerman || m.quitting {
		t.Errorf("save in the question: saved %q, quitting %v", prefs.Language, m.quitting)
	}
	if m.screen != screenArea || m.area != areaSettings {
		t.Error("saving in the question leaves the page")
	}
}

func TestSettingsPagesFitEverySizeInBothLanguages(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		for _, size := range [][2]int{{40, 14}, {60, 16}, {80, 24}, {120, 40}} {
			for _, page := range []string{"general", "update"} {
				m, _ := settingsModel(t, overviewFake(), page)
				sizedModel(m, size[0], size[1])
				assertViewFits(t, plain(m), size[0], size[1])
			}
			m := appModel(t, overviewFake(), size[0], size[1])
			assertViewFits(t, plain(m), size[0], size[1])
		}
	})
}

// Tab runs round the fields of General and never leaves the form; esc leads one level back, to the page strip.
func TestGeneralTabWrapsAroundAndEscLeadsBack(t *testing.T) {
	m, _ := settingsModel(t, newFake(), "general")
	order := generalOrder()
	for range len(order) {
		send(m, "tab")
		if m.zoneNow() != zoneContent {
			t.Fatalf("tab left the form for %v", m.zoneNow())
		}
	}
	if genFocus(m) != 0 {
		t.Errorf("tab round the form ends on field %d, want the first", genFocus(m))
	}
	send(m, "shift+tab")
	if m.zoneNow() != zoneContent || genFocus(m) != len(order)-1 {
		t.Errorf("shift+tab on the first field reaches field %d in %v, want the last", genFocus(m), m.zoneNow())
	}
	send(m, "esc")
	if m.zoneNow() != zoneSubs {
		t.Errorf("esc from the form reaches %v, want the page strip", m.zoneNow())
	}
}
