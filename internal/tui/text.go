package tui

import (
	"fmt"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/i18n"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// The editor's own texts live in a catalog of typed keys per language (text_keys.go, text_en.go,
// text_de.go) on top of internal/i18n. A screen that is not translated yet keeps its English literals;
// translating one means adding its keys to both catalogs and replacing the literals with tr.

// textID is the key of one text of the catalog.
type textID string

// Keys that are built from a name instead of being declared as constants.
func footerID(group string) textID { return textID("footer." + group) }
func wordID(english string) textID { return textID("word." + english) }

var catalogs = i18n.Catalogs[textID]{i18n.English: textEN, i18n.German: textDE}

// uiLang is the language the editor draws in. Like the theme it is process-wide: one editor runs per
// process, and the text helpers are called from places that hold no model.
var uiLang = i18n.English

// languageOf resolves the language preference to a language; system asks the device.
func languageOf(pref string) i18n.Lang {
	switch pref {
	case uiprefs.LanguageGerman:
		return i18n.German
	case uiprefs.LanguageSystem:
		return i18n.Detect()
	}
	return i18n.English
}

// setLanguage makes the preference the language of every text drawn from now on.
func setLanguage(pref string) {
	uiLang = languageOf(pref)
	refreshKeyHelp()
}

// tr is the text of id in the current language; args are formatted into it.
func tr(id textID, args ...any) string {
	text := i18n.Lookup(catalogs, uiLang, id)
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// trOr is the text of id, or fallback when no catalog holds one.
func trOr(id textID, fallback string) string {
	if text, ok := i18n.Find(catalogs, uiLang, id); ok {
		return text
	}
	return fallback
}

// footerText translates the groups of a key line, separated by " · ". A group without an entry stays as it
// is; the keys inside a translated group stay the keys.
func footerText(text string) string {
	if uiLang == i18n.English {
		return text
	}
	var out []string
	for _, group := range strings.Split(text, " · ") {
		// A group whose German text is empty is folded into a neighbour's and left out: the German words are
		// longer, and the key line of a 40-column terminal must still fit the lines English needs.
		if translated := trOr(footerID(group), group); translated != "" {
			out = append(out, translated)
		}
	}
	return strings.Join(out, " · ")
}

// wordText is how a word or short phrase is drawn that the editor takes from its own tables, such as the
// title of a group. An unknown word is drawn as it is.
func wordText(english string) string { return trOr(wordID(english), english) }
