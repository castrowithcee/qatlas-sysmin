package tui

import (
	"regexp"
	"slices"
	"testing"

	"charm.land/lipgloss/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

var sgr = regexp.MustCompile("\x1b\\[[0-9;]*m")

// darkUI and plainUI draw the qsysmin dark theme in true colour and in a terminal that keeps only the
// attributes.
func darkUI() *uiContext  { return lookUI(kittui.ProfileTrueColor, themeQsysminDark) }
func plainUI() *uiContext { return lookUI(kittui.ProfileMono, themeQsysminDark) }

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
