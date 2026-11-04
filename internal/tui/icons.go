package tui

import (
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// iconSet is the logo before the product name and one icon per area for a value of the icons preference. Each
// glyph is a single code point that kittui.CheckIcon accepts; the kit leaves them out in an ASCII terminal.
type iconSet struct {
	logo  string
	areas [areaCount]string
}

// iconsFor is the icon set of the preference value of icons; the zero set for none or an unknown value.
func iconsFor(icons string) iconSet {
	switch icons {
	case kitprefs.IconsUnicode:
		return iconSet{
			logo:  "\u25c6",                                                  // black diamond
			areas: [areaCount]string{"\u25ce", "\u25a3", "\u2261", "\u2699"}, // bullseye, boxed square, identical to, gear
		}
	case kitprefs.IconsNerdFont:
		return iconSet{
			logo:  "\uf120",                                                  // nf-fa-terminal
			areas: [areaCount]string{"\uf080", "\uf233", "\uf0f6", "\uf013"}, // bar chart, server, file text, cog
		}
	}
	return iconSet{}
}

// withIcons gives areas the icons of the set.
func (s iconSet) withIcons(areas []kittui.Area) []kittui.Area {
	for i := range areas {
		areas[i].Icon = s.areas[i]
	}
	return areas
}
