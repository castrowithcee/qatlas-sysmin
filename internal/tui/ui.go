package tui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/castrowithcee/qatlas-kit/i18n"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// uiContext is what the screens of one editor read keys with and draw their texts and colours in. The model
// holds it, so no state of the interface lives in the package.
type uiContext struct {
	keys *kittui.Keymap
	// texts is the catalog; text draws it in lang.
	texts i18n.Catalogs[textID]
	lang  i18n.Lang
	text  kittui.Translator
	// themes are the themes on offer; styles draws theme under the colour profile and character set of the
	// terminal and the colours the terminal reported, which stay empty until it has.
	themes   *kittui.ThemeSet
	theme    string
	profile  kittui.ColorProfile
	charset  kittui.CharsetMode
	terminal kittui.TerminalColors
	styles   kittui.Styles
}

// newUIContext builds the context with the default keys, in English, in the terminal theme, for a terminal of
// the given colour profile and character set. The key table, the catalog and the themes are fixed in the code,
// so one the kit refuses is a defect of this package, never of the input.
func newUIContext(profile kittui.ColorProfile, charset kittui.CharsetMode) *uiContext {
	keys, err := kittui.NewKeymap(keyTable(), charset)
	if err != nil {
		panic(fmt.Sprintf("the key table is invalid: %v", err))
	}
	themes, err := kittui.NewThemeSet(ownThemes())
	if err != nil {
		panic(fmt.Sprintf("the themes are invalid: %v", err))
	}
	u := &uiContext{keys: keys, texts: catalog(), themes: themes, profile: profile, charset: charset}
	u.setLanguage(i18n.English)
	u.setTheme(kittui.ThemeTerminal)
	return u
}

// The names of the themes of qsysmin, as tui.yaml stores them.
const (
	themeQsysminDark  = "qsysmin dark"
	themeQsysminLight = "qsysmin light"
)

// ownThemes are the themes of qsysmin beside the terminal theme and the built-in schemes.
func ownThemes() []kittui.Theme {
	return []kittui.Theme{kittui.DarkTheme(themeQsysminDark), kittui.LightTheme(themeQsysminLight)}
}

// PrefsSchema is what the preferences of the editor may hold: its themes, its areas and its key table.
func PrefsSchema() kitprefs.Schema {
	return newUIContext(kittui.ProfileNone, kittui.CharsetUnicode).prefsSchema()
}

func (u *uiContext) prefsSchema() kitprefs.Schema {
	return kitprefs.Schema{Themes: u.themes.Names(), Areas: areaIDs(), DefaultArea: areaOverview.id(), Keys: u.keys}
}

// setTheme draws the theme of a name from now on and reports whether the set holds it; a name it does not
// hold draws the terminal theme.
func (u *uiContext) setTheme(name string) bool {
	styles, known := u.themes.Resolve(name, u.profile, u.charset, &u.terminal)
	u.theme, u.styles = name, styles
	if !known {
		u.theme = kittui.ThemeTerminal
	}
	return known
}

// knownTheme is the theme a preference names, or the terminal theme and a warning where this version has no
// theme of that name.
func (u *uiContext) knownTheme(name string) (string, string) {
	if _, known := u.themes.Resolve(name, u.profile, u.charset, nil); known {
		return name, ""
	}
	return kittui.ThemeTerminal, u.tr(kittui.TextThemeUnknown, name, kittui.ThemeTerminal)
}

// terminalColorQuery asks the terminal for its foreground and background where the theme mixes from them.
// Nothing is asked without colour or of a terminal whose colours the theme could not use.
func (u *uiContext) terminalColorQuery() tea.Cmd {
	if !u.themes.AsksTerminalColors(u.theme, u.profile) {
		return nil
	}
	return tea.Batch(tea.RequestForegroundColor, tea.RequestBackgroundColor)
}

// takeTerminalColor keeps a colour the terminal answered and draws the theme anew with it.
func (u *uiContext) takeTerminalColor(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.ForegroundColorMsg:
		if msg.Color == nil {
			return
		}
		u.terminal.Foreground = kittui.ColorOf(msg.Color)
	case tea.BackgroundColorMsg:
		if msg.Color == nil {
			return
		}
		u.terminal.Background = kittui.ColorOf(msg.Color)
	}
	u.setTheme(u.theme)
}

// st is the style of a role of the theme drawn now.
func (u *uiContext) st(role kittui.TextStyle) lipgloss.Style { return u.styles.Style(role) }

// palette is the palette of the theme drawn now, the zero value without colour.
func (u *uiContext) palette() kittui.Palette { return u.styles.Palette() }

// colorless reports whether the terminal shows no colour, so marks and frames carry what colour would.
func (u *uiContext) colorless() bool { return u.styles.Profile().Colorless() }

// lip is a palette colour as Lip Gloss takes it.
func lip(c kittui.Color) color.Color { return lipgloss.Color(string(c)) }

// shown is a frame as the terminal of the colour profile is sent it, so what render returns is what the
// terminal shows. Bubble Tea steps the frame down to the same profile, which leaves it unchanged.
func (u *uiContext) shown(frame string) string {
	frame = strings.ReplaceAll(frame, cursorMark, "")
	if u.profile == kittui.ProfileTrueColor {
		return frame
	}
	var b strings.Builder
	w := colorprofile.Writer{Forward: &b, Profile: toColorprofile(u.profile)}
	_, _ = w.WriteString(frame)
	return b.String()
}

// toColorprofile is the profile of the colour library for a colour profile of the kit. Without a terminal
// nothing but plain text is sent; under NO_COLOR the attributes stay.
func toColorprofile(p kittui.ColorProfile) colorprofile.Profile {
	switch p {
	case kittui.ProfileTrueColor:
		return colorprofile.TrueColor
	case kittui.ProfileANSI256:
		return colorprofile.ANSI256
	case kittui.ProfileANSI16:
		return colorprofile.ANSI
	case kittui.ProfileMono:
		return colorprofile.ASCII
	}
	return colorprofile.NoTTY
}
