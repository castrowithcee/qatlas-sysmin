package tui

import (
	"charm.land/lipgloss/v2"
)

// fieldStyle is the surface of a field: the field colour behind the text, the accent colour and bold while
// the field has the focus. Without colour it names nothing; the marker carries the focus then.
func fieldStyle(ui *uiContext, focused bool) lipgloss.Style {
	p := ui.palette()
	style := lipgloss.NewStyle()
	if p.Surface != "" {
		style = style.Background(lip(p.Surface)).Foreground(lip(p.Text))
	}
	if focused {
		style = style.Bold(true)
		if p.Accent != "" {
			style = style.Foreground(lip(p.Accent))
		}
	}
	return style
}

// barMarker is what stands in front of a field: "> " while the field has the focus.
func barMarker(focused bool) string {
	if focused {
		return "> "
	}
	return "  "
}
