package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// Every built-in scheme mixes, and its palette keeps the contrast limits of the terminal theme.
func TestBuiltinThemesKeepTheContrast(t *testing.T) {
	for _, s := range builtinSchemes {
		t.Run(s.name, func(t *testing.T) {
			p, ok := mixPalette(s.fg, s.bg, &s.ansi)
			if !ok {
				t.Fatalf("the scheme does not mix (%.2f:1)", contrast(s.fg, s.bg))
			}
			if got := themeDefOf(t, s.name).palette(profileTrueColor); got != p {
				t.Error("the theme is not the mix of its scheme")
			}
			if p.base != s.bg.hex() || p.text != s.fg.hex() || p.accent != s.ansi[slotAccent].hex() ||
				p.ok != s.ansi[slotOK].hex() || p.fail != s.ansi[slotFail].hex() || p.warn != s.ansi[slotWarn].hex() {
				t.Errorf("roles do not follow the scheme: %+v", p)
			}
			for role, c := range map[string]colour{"ground": p.base, "surface": p.surface, "panel": p.panel, "selection": p.selection} {
				if got := contrast(s.fg, hexRGB(t, string(c))); got < textContrast {
					t.Errorf("text on the %s %s is %.2f:1", role, c, got)
				}
			}
			dim := hexRGB(t, string(p.textDim))
			for role, c := range map[string]colour{"ground": p.base, "panel": p.panel} {
				if got := contrast(dim, hexRGB(t, string(c))); got < dimContrast {
					t.Errorf("dimmed text on the %s is %.2f:1", role, got)
				}
			}
		})
	}
}

func themeDefOf(t *testing.T, name string) themeDef {
	t.Helper()
	d, ok := themeNamed(name)
	if !ok {
		t.Fatalf("no theme %q", name)
	}
	return d
}

// With 256 colours a scheme steps down like the terminal theme, or falls back to it where the steps lose their
// contrast; with 16 colours it is the terminal's fallback and paints nothing.
func TestBuiltinThemesFollowTheColourProfile(t *testing.T) {
	restoreRenderer(t)
	t.Setenv("NO_COLOR", "")
	for _, s := range builtinSchemes {
		setProfile(profile256)
		applyTheme(s.name)
		p := styles.palette
		mixed, _ := mixPalette(s.fg, s.bg, &s.ansi)
		if stepped, ok := steppedTo256(mixed, s.fg, s.bg); ok {
			if p != stepped || !styles.filled {
				t.Errorf("%s with 256 colours: not the stepped palette, filled %v", s.name, styles.filled)
			}
		} else if p != plainTerminalPalette() || styles.filled {
			t.Errorf("%s with 256 colours: want the terminal fallback without a painted ground", s.name)
		}
		setProfile(profile16)
		applyTheme(s.name)
		if styles.palette != plainTerminalPalette() || styles.filled {
			t.Errorf("%s with 16 colours: want the terminal fallback without a painted ground", s.name)
		}
		if styles.activeRow.GetReverse() == false {
			t.Errorf("%s with 16 colours: the selection is not reverse video", s.name)
		}
		setProfile(profileTrueColor)
		applyTheme(s.name)
		if !styles.filled || styles.palette.base != s.bg.hex() {
			t.Errorf("%s with truecolor: filled %v base %q", s.name, styles.filled, styles.palette.base)
		}
	}
}

func TestBuiltinThemesYieldToNoColor(t *testing.T) {
	restoreRenderer(t)
	setProfile(profileTrueColor)
	t.Setenv("NO_COLOR", "1")
	for _, s := range builtinSchemes {
		applyTheme(s.name)
		if styles.filled || styles.palette != (palette{}) {
			t.Errorf("%s under NO_COLOR keeps a palette or paints", s.name)
		}
	}
}

func TestUnknownSchemeNameFallsBackToTerminal(t *testing.T) {
	for _, name := range []string{"dracula", "solarized dark", "Nord"} {
		got, warning := knownTheme(name)
		if got != uiprefs.ThemeTerminal || !strings.Contains(warning, name) {
			t.Errorf("knownTheme(%q) = %q, %q; want terminal and a warning", name, got, warning)
		}
	}
}
