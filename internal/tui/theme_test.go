package tui

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// The General page offers the terminal theme first, then the qsysmin themes, then the built-in schemes, and
// every name it offers is one a preference may hold.
func TestThemesOnOffer(t *testing.T) {
	ui := testUI()
	names := ui.themes.Names()
	if len(names) != 13 || strings.Join(names[:3], ",") != "terminal,qsysmin dark,qsysmin light" {
		t.Fatalf("themes = %v", names)
	}
	for _, name := range append(names, "catppuccin mocha", "tomorrow night") {
		if got, warning := ui.knownTheme(name); got != name || warning != "" {
			t.Errorf("knownTheme(%q) = %q, %q", name, got, warning)
		}
	}
	got, warning := ui.knownTheme("purple")
	if got != uiprefs.ThemeTerminal || !strings.Contains(warning, `theme "purple"`) {
		t.Errorf("knownTheme(purple) = %q, %q; want terminal and a warning", got, warning)
	}
}

// A preference that names no theme of this version draws the terminal theme.
func TestUnknownThemeDrawsTheTerminalTheme(t *testing.T) {
	prefs := uiprefs.Default()
	prefs.Theme = "purple"
	if m := New(prefs, nil); m.ui.theme != uiprefs.ThemeTerminal {
		t.Errorf("theme = %q, want the terminal theme", m.ui.theme)
	}
}

// Each qsysmin theme paints the screen in its palette, the terminal theme never does.
func TestThemesPaintOnlyWhereTheyOwnTheBackground(t *testing.T) {
	for name, want := range map[string]kittui.Theme{
		uiprefs.ThemeQsysminDark:  kittui.DarkTheme(uiprefs.ThemeQsysminDark),
		uiprefs.ThemeQsysminLight: kittui.LightTheme(uiprefs.ThemeQsysminLight),
	} {
		ui := lookUI(kittui.ProfileTrueColor, name)
		if !ui.styles.Paints() || ui.palette() != want.Profiles[kittui.ProfileTrueColor] {
			t.Errorf("%s: paints %v, base %q", name, ui.styles.Paints(), ui.palette().Base)
		}
		if painted := ui.styles.Paint("x", 3, 2); !strings.Contains(painted, "48;2;") {
			t.Errorf("%s: the screen is not painted: %q", name, painted)
		}
	}
	if ui := lookUI(kittui.ProfileTrueColor, uiprefs.ThemeTerminal); ui.styles.Paints() {
		t.Error("the terminal theme paints the screen")
	}
}

// The terminal theme asks the terminal for its colours where it can mix from them, and draws anew with the
// answer; no other theme and no terminal without colour is asked.
func TestTerminalThemeAsksForTheColoursAndTakesThem(t *testing.T) {
	for _, tc := range []struct {
		profile kittui.ColorProfile
		theme   string
		asks    bool
	}{
		{kittui.ProfileTrueColor, uiprefs.ThemeTerminal, true},
		{kittui.ProfileANSI256, uiprefs.ThemeTerminal, true},
		{kittui.ProfileANSI16, uiprefs.ThemeTerminal, false},
		{kittui.ProfileMono, uiprefs.ThemeTerminal, false},
		{kittui.ProfileTrueColor, uiprefs.ThemeQsysminDark, false},
	} {
		if got := lookUI(tc.profile, tc.theme).terminalColorQuery() != nil; got != tc.asks {
			t.Errorf("profile %d, theme %s: asks %v, want %v", tc.profile, tc.theme, got, tc.asks)
		}
	}
	ui := lookUI(kittui.ProfileTrueColor, uiprefs.ThemeTerminal)
	if ui.palette().Panel != "" {
		t.Fatal("the terminal theme has fills before the terminal answered")
	}
	ui.takeTerminalColor(tea.ForegroundColorMsg{Color: color.RGBA{0xcd, 0xd6, 0xf4, 0xff}})
	ui.takeTerminalColor(tea.BackgroundColorMsg{Color: color.RGBA{0x1e, 0x1e, 0x2e, 0xff}})
	if ui.palette().Panel == "" || ui.styles.Paints() {
		t.Errorf("after the answer: panel %q, paints %v; want fills and no ground", ui.palette().Panel, ui.styles.Paints())
	}
}

// Without colour every theme draws without colour, and the focus and the selection keep their marks.
func TestColourlessTerminalKeepsTheMarks(t *testing.T) {
	for _, profile := range []kittui.ColorProfile{kittui.ProfileNone, kittui.ProfileMono} {
		ui := lookUI(profile, uiprefs.ThemeQsysminDark)
		if ui.palette() != (kittui.Palette{}) || ui.styles.Paints() {
			t.Errorf("profile %d: the theme still names colours", profile)
		}
		if h, _, _ := frameChars(ui, true); h != "═" || ui.styles.SelectionMark() != ">" {
			t.Errorf("profile %d: focus frame %q, selection mark %q", profile, h, ui.styles.SelectionMark())
		}
	}
}

// What render returns is what a terminal of the profile is sent: plain text without a terminal, the
// attributes but no colour under NO_COLOR.
func TestShownFollowsTheColourProfile(t *testing.T) {
	styled := lookUI(kittui.ProfileTrueColor, uiprefs.ThemeQsysminDark).st(kittui.StyleOK).Render("ok")
	if got := lookUI(kittui.ProfileNone, uiprefs.ThemeTerminal).shown(styled); got != "ok" {
		t.Errorf("without a terminal: %q", got)
	}
	if got := lookUI(kittui.ProfileMono, uiprefs.ThemeTerminal).shown(styled); colourSGR.MatchString(got) || !strings.Contains(got, "\x1b[1") {
		t.Errorf("under NO_COLOR: %q, want bold without colour", got)
	}
}
