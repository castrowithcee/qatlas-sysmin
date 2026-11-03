package tui

import (
	"fmt"
	"maps"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The editor's own texts live in a catalog of typed keys per language (text_keys.go, text_en.go,
// text_de.go and the text files of the screens that keep their own) on top of the kit. A screen that is not
// translated yet keeps its English literals; translating one means adding its keys to both catalogs and
// replacing the literals with tr.

// textID is the key of one text of the catalog.
type textID = kittui.TextID

// Keys that are built from a name instead of being declared as constants.
func wordID(english string) textID { return textID("word." + english) }

// catalog is the editor's catalog in English and German: the texts of the frame and those of each screen
// that keeps its own.
func catalog() i18n.Catalogs[textID] {
	en, de := textEN(), textDE()
	for _, part := range []func() (en, de map[textID]string){
		hintTexts, pickTexts, hostChangeTexts, hostFolderChangeTexts, hostGroupTexts, hostTexts, hostUserTexts,
	} {
		partEN, partDE := part()
		maps.Copy(en, partEN)
		maps.Copy(de, partDE)
	}
	commandTexts(en)
	commandTexts(de)
	return i18n.Catalogs[textID]{i18n.English: en, i18n.German: de}
}

// setLanguage makes lang the language of every text drawn from now on. The key placeholders of the texts
// follow the keys in force.
func (u *uiContext) setLanguage(lang i18n.Lang) {
	text, err := kittui.NewTranslator(lang, u.texts, u.keys)
	if err != nil {
		panic(fmt.Sprintf("the text catalog is invalid: %v", err))
	}
	u.lang, u.text = lang, text
}

// tr is the text of id in the current language; args are formatted into it.
func (u *uiContext) tr(id textID, args ...any) string { return u.text.Lookup(id, args...) }

// trOr is the text of id, or fallback when no catalog holds one.
func (u *uiContext) trOr(id textID, fallback string) string {
	if _, ok := i18n.Find(u.texts, u.lang, id); ok {
		return u.text.Lookup(id)
	}
	return fallback
}

// wordText is how a word or short phrase is drawn that the editor takes from its own tables, such as the
// title of a group. An unknown word is drawn as it is.
func (u *uiContext) wordText(english string) string { return u.trOr(wordID(english), english) }

// tr is the text of id in the language of the editor; args are formatted into it.
func (m *Model) tr(id textID, args ...any) string { return m.ui.tr(id, args...) }

// trOr is the text of id, or fallback when no catalog holds one.
func (m *Model) trOr(id textID, fallback string) string { return m.ui.trOr(id, fallback) }
