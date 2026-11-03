package tui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

var sgr = regexp.MustCompile("\x1b\\[[0-9;]*m")

// darkUI and plainUI draw the qsysmin dark theme in true colour and in a terminal that keeps only the
// attributes.
func darkUI() *uiContext  { return lookUI(kittui.ProfileTrueColor, themeQsysminDark) }
func plainUI() *uiContext { return lookUI(kittui.ProfileMono, themeQsysminDark) }

func TestWidgetsCloseTheirFrameAtEveryWidth(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		ui := darkUI()
		if noColor {
			ui = plainUI()
		}
		for w := 10; w <= 200; w++ {
			long := strings.Repeat("wide ", 80)
			blocks := map[string][]string{
				"panel":   panel(ui, "a long title "+long, []string{long, "", "x"}, w-4, 4, panelOpts{}),
				"focused": panel(ui, "t", []string{long}, w-4, 2, panelOpts{focused: true}),
				"card":    card(ui, "Services "+long, "10"+long, w),
			}
			for name, lines := range blocks {
				for i, l := range lines {
					if got := lipgloss.Width(l); got != w {
						t.Fatalf("%s w=%d line %d: width %d", name, w, i, got)
					}
				}
				plain := sgr.ReplaceAllString(lines[0], "")
				last := sgr.ReplaceAllString(lines[len(lines)-1], "")
				if !strings.ContainsAny(plain[:3], "╭╔") || !strings.HasSuffix(plain, "╮") && !strings.HasSuffix(plain, "╗") ||
					!strings.ContainsAny(last[:3], "╰╚") || !strings.HasSuffix(last, "╯") && !strings.HasSuffix(last, "╝") {
					t.Fatalf("%s w=%d: open corners %q / %q", name, w, plain, last)
				}
			}
		}
	}
}

func TestStatusMarkAndButtonWithoutColour(t *testing.T) {
	ui := plainUI()
	for _, s := range []string{statusMark(ui, statusOK, "active"), statusMark(ui, statusFail, "failed"),
		button(ui, buttonDanger, "Delete", true), button(ui, buttonPrimary, "Save", false), button(ui, buttonNormal, "Cancel", false)} {
		if strings.Contains(s, "\x1b[3") || strings.Contains(s, "\x1b[4") || strings.Contains(s, "38;") || strings.Contains(s, "48;") {
			t.Fatalf("colour sequence in %q", s)
		}
	}
	if !strings.Contains(statusMark(ui, statusOK, "active"), "active") || !strings.Contains(button(ui, buttonDanger, "Delete", true), "> Delete <") {
		t.Fatal("word or focus marker missing")
	}
}

func TestGroupMarkIsOneCellInABrandColour(t *testing.T) {
	ui := darkUI()
	for _, name := range []string{"github", "gitlab", "mattermost", "", "x"} {
		c := ui.styles.CategoryColor(name)
		if brand := ui.palette().Brand; !slices.Contains(brand[:], c) {
			t.Fatalf("%q colour %q is no brand colour", name, c)
		}
		if lipgloss.Width(ui.styles.CategoryMark(name)) != 1 {
			t.Fatal("mark width")
		}
	}
}

func TestPanelAndFieldCarrySurfaces(t *testing.T) {
	ui := darkUI()
	p := ui.palette()
	bgOf := func(c kittui.Color) string {
		out := lipgloss.NewStyle().Background(lip(c)).Render("x")
		return out[:strings.Index(out, "x")]
	}
	panelBG, fieldBG := bgOf(p.Panel), bgOf(p.Surface)
	if panelBG == fieldBG || bgOf(p.Base) == panelBG || bgOf(p.Base) == fieldBG {
		t.Fatalf("base, panel and surface must differ: %q %q %q", bgOf(p.Base), panelBG, fieldBG)
	}
	// A styled text inside a panel keeps the panel colour behind it, also after its own reset.
	lines := panel(ui, "t", []string{ui.st(kittui.StyleHint).Render("dim") + " tail"}, 20, 1, panelOpts{})
	if !strings.HasPrefix(strings.TrimPrefix(lines[1], panelStyle(ui, false).Render("│")), panelBG) {
		t.Fatalf("panel content does not open with the panel colour: %q", lines[1])
	}
	if !strings.Contains(lines[1], "\x1b[m"+panelBG+" tail") {
		t.Fatalf("panel colour lost after a styled run: %q", lines[1])
	}
	if f := surfaceRow(ui, "name", 10); !strings.Contains(f, fieldBG) || strings.Contains(f, panelBG) {
		t.Fatalf("field row not on the field surface: %q", f)
	}

	ui = plainUI()
	for _, l := range append(panel(ui, "t", []string{"x"}, 10, 1, panelOpts{}), surfaceRow(ui, "x", 10)) {
		if strings.Contains(l, "48;") || strings.Contains(l, "\x1b[4") {
			t.Fatalf("background colour without colour: %q", l)
		}
	}
}
