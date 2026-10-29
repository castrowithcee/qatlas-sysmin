package tui

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/i18n"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// useLanguage draws the editor in the given language until the test ends.
func useLanguage(t *testing.T, lang i18n.Lang) {
	t.Helper()
	before := uiLang
	uiLang = lang
	t.Cleanup(func() { uiLang = before })
}

// eachLanguage runs body once per language of the catalog.
func eachLanguage(t *testing.T, body func(t *testing.T)) {
	t.Helper()
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		t.Run(string(lang), func(t *testing.T) {
			useLanguage(t, lang)
			body(t)
		})
	}
}

var verbPattern = regexp.MustCompile(`%[-+# 0-9.*]*[a-zA-Z%]`)

func TestCatalogsHoldTheSameKeys(t *testing.T) {
	keys := func(c map[textID]string) []string {
		var out []string
		for k := range c {
			out = append(out, string(k))
		}
		sort.Strings(out)
		return out
	}
	en, de := keys(textEN), keys(textDE)
	if strings.Join(en, "\n") != strings.Join(de, "\n") {
		inEN, inDE := map[string]bool{}, map[string]bool{}
		for _, k := range en {
			inEN[k] = true
		}
		for _, k := range de {
			inDE[k] = true
			if !inEN[k] {
				t.Errorf("key %q is German only", k)
			}
		}
		for _, k := range en {
			if !inDE[k] {
				t.Errorf("key %q lacks a German text", k)
			}
		}
	}
}

func TestCatalogsUseTheSameFormatVerbs(t *testing.T) {
	for key, english := range textEN {
		german, ok := textDE[key]
		if !ok {
			continue
		}
		if en, de := verbPattern.FindAllString(english, -1), verbPattern.FindAllString(german, -1); strings.Join(en, " ") != strings.Join(de, " ") {
			t.Errorf("%q: English verbs %v, German verbs %v", key, en, de)
		}
		if strings.TrimSpace(german) == "" && !strings.HasPrefix(string(key), "footer.") {
			t.Errorf("%q has an empty German text", key)
		}
	}
}

func TestEnglishIsTheDefaultAndFallback(t *testing.T) {
	if uiLang != i18n.English {
		t.Fatalf("default language = %q, want English", uiLang)
	}
	if got := tr(tAreaSettings); got != "Settings" {
		t.Errorf("English area = %q", got)
	}
	useLanguage(t, i18n.German)
	if got := tr(tAreaSettings); got != "Einstellungen" {
		t.Errorf("German area = %q", got)
	}
	if got := wordText("a word nobody translated"); got != "a word nobody translated" {
		t.Errorf("an untranslated word = %q, want it as it is", got)
	}
	if got := footerText("x unknown · q quit"); got != "x unknown · q Ende" {
		t.Errorf("footerText = %q", got)
	}
}

func TestLanguageOfPreference(t *testing.T) {
	cases := map[string]i18n.Lang{
		uiprefs.LanguageEnglish: i18n.English, uiprefs.LanguageGerman: i18n.German, "": i18n.English, "x": i18n.English,
	}
	for pref, want := range cases {
		if got := languageOf(pref); got != want {
			t.Errorf("languageOf(%q) = %q, want %q", pref, got, want)
		}
	}
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	if got := languageOf(uiprefs.LanguageSystem); got != i18n.German {
		t.Errorf("system with LC_ALL=de_DE.UTF-8 = %q, want German", got)
	}
}
