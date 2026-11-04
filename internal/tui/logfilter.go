package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The filter of the log is the kit's filter bar above the table: the period, the action and the result. Its
// values narrow what the service is asked for, so a change reads the log anew; the table holds what came back.

// zoneFilters is the zone of the filter bar; the kit gives it its place in the order of the zones.
const zoneFilters = kittui.ZoneFilters

// The fields of the filter bar, and the value that sets no limit.
const (
	fieldPeriod kittui.FieldID  = "period"
	fieldAction kittui.FieldID  = "action"
	fieldResult kittui.FieldID  = "result"
	choiceAll   kittui.ChoiceID = "all"
)

// filterBarKeys are the actions the filter bar reads.
func filterBarKeys() kittui.FilterBarKeys {
	return kittui.FilterBarKeys{Left: actLeft, Right: actRight, Next: actNext, Prev: actPrev, Up: actUp, Down: actDown,
		Enter: actEnter, Back: actBack}
}

// choiceOf is the value of the bar for a filter value, where empty sets no limit.
func choiceOf(v string) kittui.ChoiceID {
	if v == "" {
		return choiceAll
	}
	return kittui.ChoiceID(v)
}

// valueOf is the filter value of a value of the bar.
func valueOf(c kittui.ChoiceID) string {
	if c == choiceAll {
		return ""
	}
	return string(c)
}

// logBar is the filter bar of the log, on the values of the filter in force. It is built anew in another
// language, because the kit looks the labels up with the translator it was built with. The fields are fixed in
// the code, so a bar the kit refuses is a defect of this package.
func (m *Model) logBar() *kittui.FilterBar {
	a := &m.act
	if a.bar != nil && a.barLang == m.ui.lang {
		return a.bar
	}
	periods := make([]kittui.Choice, periodCount)
	for i := range periods {
		periods[i] = kittui.Choice{ID: kittui.ChoiceID(strconv.Itoa(i)), Label: periodID(i)}
	}
	actions := []kittui.Choice{{ID: choiceAll, Label: tFilterAll}}
	for _, action := range logActions() {
		actions = append(actions, kittui.Choice{ID: kittui.ChoiceID(action), Label: actionID(action)})
	}
	results := []kittui.Choice{{ID: choiceAll, Label: tFilterAll},
		{ID: control.LogResultSuccess, Label: tLogResultSuccess}, {ID: control.LogResultFailure, Label: tLogResultFailure}}
	f := a.filter
	bar, err := kittui.NewFilterBar(kittui.FilterBarSpec{Fields: []kittui.FilterField{
		{ID: fieldPeriod, Label: tColPeriod, Options: periods, Selected: kittui.ChoiceID(strconv.Itoa(f.period))},
		{ID: fieldAction, Label: tColAction, Options: actions, Selected: choiceOf(f.action)},
		{ID: fieldResult, Label: tColResult, Options: results, Selected: choiceOf(f.result)},
	}, Keys: filterBarKeys()}, m.ui.keys, m.ui.text)
	if err != nil {
		panic("the filter bar of the log is invalid: " + err.Error())
	}
	a.bar, a.barLang = bar, m.ui.lang
	return bar
}

// barOpen reports whether the list of values of the filter bar is open; it takes every key then.
func (m *Model) barOpen() bool {
	return m.zoneNow() == zoneFilters && m.act.bar != nil && m.act.bar.Open()
}

// barModal reports whether the modal of the filter bar is open, which the bar shows in place of its fields in a
// narrow terminal.
func (m *Model) barModal() bool {
	return m.barOpen() && len(m.act.bar.Hints()) > 0
}

// filtersAvailable reports whether the screen shows the filter bar.
func (m *Model) filtersAvailable() bool { return m.area == areaActivity && m.act.sub == actLog }

// focusFilters gives the filter bar the focus: its first field, or its last where the focus comes from below
// it in the order of the zones.
func (m *Model) focusFilters(fromEnd bool) {
	m.zone = zoneFilters
	m.logBar().Focus(fromEnd)
}

// applyLogBar takes the values of the bar as the filter and reads the log anew.
func (m *Model) applyLogBar() tea.Cmd {
	b := m.logBar()
	period, _ := strconv.Atoi(string(b.Value(fieldPeriod)))
	m.act.filter = logFilter{period: period, action: valueOf(b.Value(fieldAction)), result: valueOf(b.Value(fieldResult))}
	return m.logCmd("")
}

// filterBarKey handles a key while the filter bar has the focus. What the bar leaves goes on: tab and shift+tab
// to the next zone, down and esc to the rows under it, up to the tab line above it.
func (m *Model) filterBarKey(key tea.KeyPressMsg) tea.Cmd {
	b := m.logBar()
	if !b.Focused() {
		b.Focus(false)
	}
	changed, handled := b.Update(key)
	switch {
	case len(changed) > 0:
		return m.applyLogBar()
	case handled:
	case m.keyIs(key, actNext):
		m.cycleZone(1)
	case m.keyIs(key, actPrev):
		m.cycleZone(-1)
	case m.keyIs(key, actDown), m.keyIs(key, actBack):
		b.Blur()
		m.zone = zoneContent
	case m.keyIs(key, actUp):
		b.Blur()
		m.zone = m.barZone()
	default:
		if cmd, ok := m.activityKey(key); ok {
			return cmd
		}
		return m.digitKey(key)
	}
	return nil
}

// clickFilterBar is a click on a target of the filter bar or its list of values: the kit focuses the field
// and opens its values, or takes the value clicked.
func (m *Model) clickFilterBar(t kittui.Target) tea.Cmd {
	m.endInput()
	if m.innerLevel() {
		return nil
	}
	m.zone = zoneFilters
	changed, _ := m.logBar().Click(kittui.MouseResult{Handled: true, Zone: t.Zone, Action: t.Action, Item: t.Item})
	if len(changed) > 0 {
		return m.applyLogBar()
	}
	return nil
}

// withFilterBar draws the lines above the bar, the bar from screen row top+len(above) on and under it the
// table that body draws from the row it is given. The open list of values stands under the bar at its left
// edge, since the kit names no place of its fields, and only it and the bar take clicks then.
func (m *Model) withFilterBar(above []string, top int, body func(top int) string) string {
	b := m.logBar()
	if focused := m.zoneNow() == zoneFilters; focused != b.Focused() {
		if focused {
			b.Focus(false)
		} else {
			b.Blur()
		}
	}
	width := m.usable(0)
	y := top + len(above)
	bar, targets := b.View(m.ui.styles, width)
	m.addFilterTargets(targets, 0, y)
	mark := len(m.mt.targets)
	lines := append(append(above, bar), strings.Split(body(y+1), "\n")...)
	if box, boxTargets := b.ValueList(m.ui.styles); box != "" {
		m.mt.targets = m.mt.targets[:mark]
		delete(m.mt.wheels, zoneContent)
		delete(m.mt.wheels, zoneDetail)
		row := len(above) + 1
		for i, line := range strings.Split(box, "\n") {
			for row+i >= len(lines) {
				lines = append(lines, "")
			}
			lines[row+i] = line + ansi.Cut(lines[row+i], ansi.StringWidth(line), width)
		}
		m.addFilterTargets(boxTargets, 0, y+1)
	}
	return strings.Join(lines, "\n")
}

// addFilterTargets makes the targets of the bar or its list of values clickable from screen cell (x0, y0) on;
// the free area of the bar gives it the focus.
func (m *Model) addFilterTargets(targets []kittui.Target, x0, y0 int) {
	for _, t := range targets {
		r := t.Rect
		if t.Item == "" && t.Action == "" {
			m.addZone(x0+r.X, y0+r.Y, x0+r.X+r.Width, y0+r.Y+r.Height, zoneFilters)
			continue
		}
		m.addHit(x0+r.X, y0+r.Y, x0+r.X+r.Width, y0+r.Y+r.Height, func() tea.Cmd { return m.clickFilterBar(t) })
	}
}

// filterBarHints is the key line while the filter bar has the focus. While its modal is open, the keys are
// the kit's.
func (m *Model) filterBarHints() []kittui.Hint {
	if m.barModal() {
		return m.act.bar.Hints()
	}
	if m.barOpen() {
		return keyLine(navHint(tHintMove, actUp, actDown), hint(tHintChoose, actEnter), hint(tHintClose, actBack))
	}
	return keyLine(barHint(tHintChoose, actLeft, actRight), hint(tHintOpen, actEnter), navHint(tHintNext, actNext),
		hint(tHintBack, actBack))
}

// filterModalFrame draws the modal of the filter bar over the dimmed screen, or, where the screen has no room
// for it, in the workspace with its keys in the footer, whose keys then act on it. Only the modal takes clicks.
func (m *Model) filterModalFrame() (string, bool) {
	if !m.barModal() || m.screen != screenArea {
		return "", false
	}
	b := m.act.bar
	room := m.ui.newFrame().Workspace(m.frameSpec(""))
	var background string
	m.renderAs(m.baseScreen(), 0, func() {
		m.backdrop = true
		defer func() { m.backdrop = false }()
		background = m.frame(m.workspaceView())
	})
	if view, targets, ok := b.ModalOverlay(m.ui.styles, background, m.termWidth, m.termHeight, room); ok {
		m.addFilterTargets(targets, 0, 0)
		return view, true
	}
	m.keyHint(b.Hints())
	room = m.ui.newFrame().Workspace(m.frameSpec(""))
	view, targets := b.ModalView(m.ui.styles, room.Width, room.Height)
	mark := len(m.mt.targets)
	frame := m.frame(view)
	m.mt.targets = m.mt.targets[:mark]
	m.addFilterTargets(targets, room.X, room.Y)
	m.footerHits()
	return frame, true
}
