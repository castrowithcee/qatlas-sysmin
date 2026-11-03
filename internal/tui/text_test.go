package tui

import (
	"testing"

	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/redact"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// testLang is the language test models are drawn in; eachLanguage sets it.
var testLang = i18n.English

// defaultPrefs are the preferences of a device that has none.
func defaultPrefs() kitprefs.Prefs { return kitprefs.Default(PrefsSchema()) }

// testModel is New in the language of the test.
func testModel(prefs kitprefs.Prefs, redactor *redact.Redactor) *Model {
	m := New(prefs, redactor)
	m.ui.setLanguage(testLang)
	return m
}

// testUI is the context of an editor with the default keys, in English.
func testUI() *uiContext { return newUIContext(kittui.ProfileNone, kittui.CharsetUnicode) }

// useLanguage draws m in the given language from now on.
func useLanguage(t *testing.T, m *Model, lang i18n.Lang) {
	t.Helper()
	m.ui.setLanguage(lang)
}

// eachLanguage runs body once per language of the catalog, with the test models drawn in it.
func eachLanguage(t *testing.T, body func(t *testing.T)) {
	t.Helper()
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		t.Run(string(lang), func(t *testing.T) {
			before := testLang
			testLang = lang
			t.Cleanup(func() { testLang = before })
			body(t)
		})
	}
}

// The catalog parts of the screens never take a key of another part.
func TestCatalogPartsDoNotOverlap(t *testing.T) {
	seen := map[textID]bool{}
	for lang, texts := range map[string]map[textID]string{"en": textEN(), "de": textDE()} {
		for id := range texts {
			seen[id+textID(":"+lang)] = true
		}
	}
	for i, part := range []func() (en, de map[textID]string){
		pickTexts, hostChangeTexts, hostFolderChangeTexts, hostGroupTexts, hostTexts, hostUserTexts,
	} {
		en, de := part()
		for lang, texts := range map[string]map[textID]string{"en": en, "de": de} {
			for id := range texts {
				if key := id + textID(":"+lang); seen[key] {
					t.Errorf("part %d: %s text %q is defined twice", i, lang, id)
				} else {
					seen[key] = true
				}
			}
		}
	}
}

func TestEnglishIsTheDefaultAndFallback(t *testing.T) {
	m := New(defaultPrefs(), nil)
	if m.ui.lang != i18n.English {
		t.Fatalf("default language = %q, want English", m.ui.lang)
	}
	if got := m.tr(tAreaSettings); got != "Settings" {
		t.Errorf("English area = %q", got)
	}
	useLanguage(t, m, i18n.German)
	if got := m.tr(tAreaSettings); got != "Einstellungen" {
		t.Errorf("German area = %q", got)
	}
	if got := m.ui.wordText("a word nobody translated"); got != "a word nobody translated" {
		t.Errorf("an untranslated word = %q, want it as it is", got)
	}
	if got := m.hintText(keyLine(hint(tHintQuit, actQuit), hint(tHintBack, actBack))); got != "q Ende · esc zurück" {
		t.Errorf("hintText = %q", got)
	}
	if got := m.hintText(keyLine(hint("keep", actSave), hint("maybe", actYes))); got != "F2 keep · y maybe" {
		t.Errorf("an untranslated group keeps its words and names its key: %q", got)
	}
}

// A German preference draws the editor in German.
func TestLanguagePreferenceDrawsTheEditor(t *testing.T) {
	prefs := defaultPrefs()
	prefs.Language = kitprefs.LanguageGerman
	if m := New(prefs, nil); m.ui.lang != i18n.German || m.tr(tAreaSettings) != "Einstellungen" {
		t.Errorf("a German preference draws in %q", m.ui.lang)
	}
}
