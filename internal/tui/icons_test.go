package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// Every glyph of both sets passes the kit's checks, and the sets differ.
func TestIconSetsPassTheKitChecks(t *testing.T) {
	seen := map[string]bool{}
	for _, icons := range []string{kitprefs.IconsUnicode, kitprefs.IconsNerdFont} {
		m := appModel(t, overviewFake(), 80, 24)
		m.prefs.Icons = icons
		spec := m.frameSpec("")
		if err := kittui.CheckFrame(spec); err != nil {
			t.Errorf("%s: %v", icons, err)
		}
		if err := kittui.CheckAreas(spec.Areas); err != nil {
			t.Errorf("%s: %v", icons, err)
		}
		glyphs := []string{spec.Icon}
		for _, a := range spec.Areas {
			glyphs = append(glyphs, a.Icon)
		}
		for _, g := range glyphs {
			if g == "" || seen[g] {
				t.Errorf("%s: glyph %q is empty or used twice", icons, g)
			}
			seen[g] = true
		}
	}
	none := appModel(t, overviewFake(), 80, 24).frameSpec("")
	if none.Icon != "" || none.Areas[0].Icon != "" {
		t.Errorf("icons none still has glyphs: %q %q", none.Icon, none.Areas[0].Icon)
	}
}

// The header and the tab line show the glyphs of the set, and none shows none; an ASCII terminal shows none
// of any set.
func TestIconsShowPerSetAndNotInNoneOrASCII(t *testing.T) {
	for _, icons := range []string{kitprefs.IconsUnicode, kitprefs.IconsNerdFont, kitprefs.IconsNone} {
		m := appModel(t, overviewFake(), 120, 40)
		m.prefs.Icons = icons
		rows := rowsOf(m)
		set := iconsFor(icons)
		for _, want := range append([]string{set.logo}, set.areas[:]...) {
			if want != "" && !strings.Contains(rows[0]+rows[1], want) {
				t.Errorf("%s: glyph %q is not in the header or the tab line:\n%s\n%s", icons, want, rows[0], rows[1])
			}
		}
		if icons == kitprefs.IconsNone && (!strings.HasPrefix(rows[0], "qsysmin") || !strings.Contains(rows[1], "1 Overview")) {
			t.Errorf("none: header %q, tab line %q", rows[0], rows[1])
		}
	}
	prefs := defaultPrefs()
	prefs.Icons = kitprefs.IconsNerdFont
	m := newModel(prefs, nil, kittui.ProfileNone, kittui.CharsetASCII)
	m.bindKeys()
	m.ui.setLanguage(testLang)
	m.enableAreas(overviewFake())
	sizedModel(m, 120, 40)
	drain(m, m.statusCmd())
	set := iconsFor(kitprefs.IconsNerdFont)
	rows := rowsOf(m)
	for _, g := range append([]string{set.logo}, set.areas[:]...) {
		if strings.Contains(rows[0]+rows[1], g) {
			t.Errorf("an ASCII terminal shows %q:\n%s\n%s", g, rows[0], rows[1])
		}
	}
}

// The icons and the start view of General act and are saved in tui.yaml.
func TestIconsChosenInGeneralActAfterTheSave(t *testing.T) {
	m, path := settingsModel(t, newFake(), "general")
	toField(t, m, fieldIcons)
	send(m, "enter", "up", "up", "enter") // none to unicode
	send(m, "f2")
	if m.prefs.Icons != kitprefs.IconsUnicode || !strings.Contains(rowsOf(m)[1], iconsFor(kitprefs.IconsUnicode).areas[areaSettings]) {
		t.Errorf("the icons do not act: %q\n%s", m.prefs.Icons, rowsOf(m)[1])
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "icons: unicode") {
		t.Errorf("tui.yaml: %v\n%s", err, data)
	}
	if filepath.Base(path) != "tui.yaml" {
		t.Errorf("path %q", path)
	}
}
