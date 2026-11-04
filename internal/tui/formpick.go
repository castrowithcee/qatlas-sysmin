package tui

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A pick list is the editor of a list field for a long multi-selection: the form shows a short summary of what
// is chosen, and enter or a click opens a list of all values with a filter, over the form. The choice only
// reaches the field when the list is left with enter; esc drops what was changed in it.

const pickSummaryNames = 2

// pickList is the values of a list field and which of them are ticked. notes, when set, has one entry per
// value: a short dimmed word behind it, such as a marker for values that need care.
type pickList struct {
	values, notes []string
	ticked        []bool
}

func newPickList(values, notes []string) *pickList {
	return &pickList{values: values, notes: notes, ticked: make([]bool, len(values))}
}

// picked are the ticked values, in order.
func (l *pickList) picked() []string {
	var out []string
	for i, on := range l.ticked {
		if on {
			out = append(out, l.values[i])
		}
	}
	return out
}

// summary is what the field shows: the first chosen values and how many more there are, empty for none.
func (l *pickList) summary(ui *uiContext) string {
	if len(l.values) == 0 {
		return ui.tr(tPickNothing)
	}
	chosen := l.picked()
	text := strings.Join(chosen[:min(len(chosen), pickSummaryNames)], ", ")
	if more := len(chosen) - pickSummaryNames; more > 0 {
		text += " " + ui.tr(tPickMore, more)
	}
	return text
}

// pickField adds a list field with its pick list to the form.
func (f *formDlg) pickField(id kittui.FieldID, label textID, values, notes []string) kittui.FormField {
	if f.lists == nil {
		f.lists = map[kittui.FieldID]*pickList{}
	}
	f.lists[id] = newPickList(values, notes)
	return listField(id, label)
}

// picked are the ticked values of the list field id.
func (f *formDlg) picked(id kittui.FieldID) []string {
	if l := f.lists[id]; l != nil {
		return l.picked()
	}
	return nil
}

// pickDlg is the list over a form that a list field opened: a kit list with tick boxes, whose entry IDs are
// the positions of the values in the pick list, so no value is ever read back by its text.
type pickDlg struct {
	field kittui.FieldID
	list  *kittui.List
}

func pickItemID(i int) kittui.ItemID { return kittui.ItemID(strconv.Itoa(i)) }

// openPick shows the list of the list field id, the chosen values at the top. The label of the field titles
// the list; a column of notes only stands where some value has one.
func (m *Model) openPick(f *formDlg, id kittui.FieldID) {
	l := f.lists[id]
	label := ""
	for _, field := range f.fields {
		if field.ID == id {
			label = m.tr(field.Label)
		}
	}
	notes := slices.ContainsFunc(l.notes, func(n string) bool { return n != "" })
	cols := []kittui.Column{{Title: literal(label)}}
	if notes {
		cols = append(cols, kittui.Column{Title: tPickNote, Dimmed: true, Optional: true})
	}
	spec := kittui.ListSpec{
		Columns: cols, Keys: pickKeys(), Search: kittui.SearchAsTyped, Empty: tPickNothing, Ticks: true,
		Title: literal(label),
		Buttons: []kittui.FormButton{{Action: actEnter, Label: tHintKeep, Primary: true},
			{Action: actBack, Label: tHintDiscard}},
	}
	list, err := kittui.NewList(spec, m.ui.keys, formText{m.ui.text})
	if err != nil {
		// The columns, keys and buttons are fixed here, so a refusal is a defect of this package.
		panic(fmt.Sprintf("the pick list is invalid: %v", err))
	}
	var first, rest []int
	for j := range l.values {
		if l.ticked[j] {
			first = append(first, j)
		} else {
			rest = append(rest, j)
		}
	}
	var items []kittui.ListItem
	var ticked []kittui.ItemID
	for _, j := range append(first, rest...) {
		cells := []string{l.values[j]}
		if notes {
			note := ""
			if j < len(l.notes) {
				note = l.notes[j]
			}
			cells = append(cells, note)
		}
		items = append(items, kittui.ListItem{ID: pickItemID(j), Cells: cells})
		if l.ticked[j] {
			ticked = append(ticked, pickItemID(j))
		}
	}
	if err := errors.Join(list.SetItems(items), list.SetTicked(ticked)); err != nil {
		panic(fmt.Sprintf("the pick list does not take its values: %v", err))
	}
	f.pick = &pickDlg{field: id, list: list}
}

// pickKeys are the actions of the key table the pick list reads.
func pickKeys() kittui.ListKeys {
	return kittui.ListKeys{Up: actUp, Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actHome,
		Last: actEnd, Left: actLeft, Right: actRight, Enter: actEnter, Back: actBack, Toggle: actSpace}
}

// pickKey handles a key of the open list. Typing filters; space ticks, which the list leaves to the filter
// because a space types text there, and no value holds one. Enter takes the ticks over and esc drops them, even
// with a filter typed, which the list's own esc would clear first.
func (m *Model) pickKey(f *formDlg, key tea.KeyPressMsg) tea.Cmd {
	pk := f.pick
	switch {
	case m.keyIs(key, actBack):
		return m.pickDiscard(f)
	case m.keyIs(key, actEnter):
		return m.pickApply(f)
	case m.keyIs(key, actSpace):
		pk.list.Act(actSpace)
	default:
		pk.list.Update(key)
	}
	return nil
}

// pickDiscard closes the list and leaves the field as it was.
func (m *Model) pickDiscard(f *formDlg) tea.Cmd {
	f.pick = nil
	return nil
}

// pickApply takes the ticks over into the field and closes the list.
func (m *Model) pickApply(f *formDlg) tea.Cmd {
	pk := f.pick
	list := f.lists[pk.field]
	for i := range list.ticked {
		list.ticked[i] = false
	}
	for _, id := range pk.list.Ticked() {
		if i, err := strconv.Atoi(string(id)); err == nil && i >= 0 && i < len(list.ticked) {
			list.ticked[i] = true
		}
	}
	f.pick = nil
	m.setListValue(f, pk.field, list.summary(m.ui))
	return nil
}

// clickPick is a click on a target of the open list. A row is only marked and a tick box does what space
// does, so a click never takes more than its key; the buttons act as enter and esc do. No click types.
func (m *Model) clickPick(f *formDlg, res kittui.MouseResult) tea.Cmd {
	pk := f.pick
	if m.form != f || m.screen != screenPrompt || pk == nil {
		return nil
	}
	id, err := strconv.Atoi(res.Item)
	listed := err == nil && id >= 0 && id < len(f.lists[pk.field].values)
	switch {
	case res.Action == actSpace, listed && res.Action == actEnter:
		pk.list.Click(res)
	case res.Action == actEnter:
		return m.pickApply(f)
	case res.Action == actBack:
		return m.pickDiscard(f)
	}
	return nil
}

// pickFrame draws the open list as the kit's modal over the dimmed screen, or, where the workspace has no room
// for it, in the workspace with its keys in the footer. Only the list takes clicks and the wheel.
func (m *Model) pickFrame() (string, bool) {
	f := m.form
	if m.screen != screenPrompt || f == nil || f.pick == nil {
		return "", false
	}
	pk := f.pick
	room := m.ui.newFrame().Workspace(m.frameSpec(""))
	var background string
	m.renderAs(m.baseScreen(), 0, func() {
		m.backdrop = true
		defer func() { m.backdrop = false }()
		background = m.frame(m.workspaceView())
	})
	click := func(res kittui.MouseResult) tea.Cmd { return m.clickPick(f, res) }
	if view, targets, ok := pk.list.Overlay(m.ui.styles, background, m.termWidth, m.termHeight, room); ok {
		m.addFormTargets(targets, 0, 0, "", click)
		m.addPickWheel(pk, targets, 0, 0)
		return view, true
	}
	m.keyHint(pk.list.Hints())
	room = m.ui.newFrame().Workspace(m.frameSpec(""))
	view, targets := pk.list.View(m.ui.styles, room.Width, room.Height)
	mark := len(m.mt.targets)
	frame := m.frame(view)
	m.mt.targets = m.mt.targets[:mark]
	m.addFormTargets(targets, room.X, room.Y, "", click)
	m.addPickWheel(pk, targets, room.X, room.Y)
	m.footerHits()
	return frame, true
}

// addPickWheel makes the free area of the rows, which the kit names, take the wheel.
func (m *Model) addPickWheel(pk *pickDlg, targets []kittui.Target, x0, y0 int) {
	for _, t := range targets {
		if t.Zone != kittui.ZoneRows || t.Item != "" || t.Action != "" {
			continue
		}
		r := t.Rect
		m.addWheel(x0+r.X, y0+r.Y, x0+r.X+r.Width, y0+r.Y+r.Height, kittui.ZoneRows, func(up bool) tea.Cmd {
			steps := 1
			if up {
				steps = -1
			}
			pk.list.Wheel(steps)
			return nil
		})
	}
}

// The texts of the pick list.
const (
	tPickNothing textID = "PickNothing"
	tPickMore    textID = "PickMore"
	tPickNote    textID = "PickNote"
)

// pickTexts are the texts of the pick list, in English and German.
func pickTexts() (en, de map[textID]string) {
	en = map[textID]string{
		tPickNothing: "nothing to choose",
		tPickMore:    "+%d",
		tPickNote:    "Note",
	}
	de = map[textID]string{
		tPickNothing: "nichts zur Auswahl",
		tPickMore:    "+%d",
		tPickNote:    "Hinweis",
	}
	return en, de
}
