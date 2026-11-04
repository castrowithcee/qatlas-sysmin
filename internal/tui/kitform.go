package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The forms of the editor are the kit's: the editor hands over fields, values and texts, checks the values
// when the save key is pressed and hands the problems back. This file holds what every form of the editor
// shares.

// literalPrefix marks a text ID that stands for data shown as it is, such as an account or theme name: no
// catalog holds it, and it is never translated.
const literalPrefix = "="

// literal is the text ID of data a form shows as it is.
func literal(s string) textID { return textID(literalPrefix + s) }

// formText is the translator of a form: the catalog in force, and data under literal IDs as it is. Data never
// passes through the catalog, so a name that looks like a text ID or a key placeholder stays the name.
type formText struct{ base kittui.Translator }

func (t formText) Lookup(id kittui.TextID, args ...any) string {
	if s, ok := strings.CutPrefix(string(id), literalPrefix); ok {
		return s
	}
	return t.base.Lookup(id, args...)
}

// formKeys are the actions of the key table a form reads.
func formKeys() kittui.FormKeys {
	return kittui.FormKeys{Up: actUp, Down: actDown, Next: actNext, Prev: actPrev, PageUp: actPageUp,
		PageDown: actPageDown, Left: actLeft, Right: actRight, First: actHome, Last: actEnd, Enter: actEnter,
		Toggle: actSpace, Save: actSave, Back: actBack}
}

// newForm is the kit form of fields in the keys and the language in force.
func (m *Model) newForm(title, lead kittui.TextID, fields []kittui.FormField, sections []kittui.FormSection, buttons []kittui.FormButton) (*kittui.Form, error) {
	spec := kittui.FormSpec{Title: title, Lead: lead, Fields: fields, Sections: sections, Keys: formKeys(), Buttons: buttons}
	return kittui.NewForm(spec, m.ui.keys, formText{m.ui.text})
}

// literalChoices are values a form shows as they are, each its own ID.
func literalChoices(values ...string) []kittui.Choice {
	out := make([]kittui.Choice, len(values))
	for i, v := range values {
		out[i] = kittui.Choice{ID: kittui.ChoiceID(v), Label: literal(v)}
	}
	return out
}

// addFormTargets makes the targets a form drew clickable from screen cell (x0, y0) on: its free area gives
// zone the focus, where it names one, and a click on anything else reaches click.
func (m *Model) addFormTargets(targets []kittui.Target, x0, y0 int, zone kittui.FocusZone, click func(kittui.MouseResult) tea.Cmd) {
	for _, t := range targets {
		r := t.Rect
		if t.Item == "" && t.Action == "" {
			if zone != "" {
				m.addZone(x0+r.X, y0+r.Y, x0+r.X+r.Width, y0+r.Y+r.Height, zone)
			}
			continue
		}
		res := kittui.MouseResult{Handled: true, Zone: t.Zone, Action: t.Action, Item: t.Item}
		m.addHit(x0+r.X, y0+r.Y, x0+r.X+r.Width, y0+r.Y+r.Height, func() tea.Cmd { return click(res) })
	}
}
