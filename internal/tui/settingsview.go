package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// settingsBody is the page of Settings under the bars of the frame.
func (m *Model) settingsBody() string {
	m.loadForms()
	pg := &pageDraw{m: m, y: m.workspaceTop()}
	switch m.set.sub {
	case subGeneral:
		m.generalPage(pg)
	default:
		m.updatePage(pg)
	}
	if m.set.sub == subGeneral {
		m.addWheel(0, m.workspaceTop(), m.usable(0), pg.y, m.wheelSettings)
	}
	return strings.Join(pg.lines, "\n")
}

// wheelSettings is a notch of the wheel over a form of Settings: the focus of the field moves by one and stops
// at both ends, never leaving for the page strip, and rests while a field takes the text. The zone stays.
func (m *Model) wheelSettings(up bool) tea.Cmd {
	if m.set.editing {
		return nil
	}
	if m.set.sub == subGeneral {
		m.set.gen.focus = wheelStep(m.set.gen.focus, 0, genRows, up)
	}
	return nil
}

// pageDraw collects the lines of a page and keeps the screen row of the next one, for click targets and for
// the place of an open list.
type pageDraw struct {
	m     *Model
	y     int
	lines []string
}

// add appends a block of lines.
func (p *pageDraw) add(block string) {
	p.lines = append(p.lines, block)
	p.y += strings.Count(block, "\n") + 1
}

// row appends the line of a focusable row; a click marks it, a click on the marked row presses enter.
func (p *pageDraw) row(line string, i int, focus *int) {
	m := p.m
	m.addHit(0, p.y, m.usable(0), p.y+1, func() tea.Cmd {
		if *focus != i {
			*focus = i
			return nil
		}
		return m.contentPress(actEnter)
	})
	p.add(line)
}

// problem appends the finding of a field under it.
func (p *pageDraw) problem(text string) {
	if text != "" {
		p.add(p.m.indentedWith(p.m.ui.st(kittui.StyleFail), warnMark+" "+text))
	}
}

// labelWidth is the widest of the labels.
func labelWidth(labels []string) int {
	w := 0
	for _, l := range labels {
		w = max(w, lipgloss.Width(l))
	}
	return w
}

func (m *Model) generalPage(p *pageDraw) {
	g := &m.set.gen
	labels := []string{m.tr(tLblLanguage), m.tr(tLblTheme), m.tr(tLblMouse), m.tr(tLblKeySave), m.tr(tLblKeyHelp), m.tr(tLblKeyCommand),
		m.tr(tLblKeyInfo)}
	// The actions come in the order of rebindable: save, help, command, info.
	lw := labelWidth(labels)
	values := []string{g.language, g.theme, g.mouse}
	ids := []string{"language", "theme", "mouse"}
	room := m.usable(0) - 2
	for i, label := range labels {
		focused := g.focus == i
		var line string
		if i < genKeys {
			m.noteField(ids[i], 2, p.y)
			line = barMarker(focused) + fieldStyle(m.ui, focused).Render(truncateCells(
				" "+padLine(label+":", lw+1)+" "+values[i]+" "+dropdownArrow+" ", room))
		} else {
			line = barMarker(focused) + m.keyField(label, lw, &g.keys[i-genKeys], focused, room)
		}
		p.row(line, i, &g.focus)
		if i >= genKeys {
			p.problem(g.problems[i-genKeys])
		}
	}
}

// keyField draws a text line of Settings: the label on the field surface, then the value, which is the field
// itself while it takes the text.
func (m *Model) keyField(label string, lw int, in *textinput.Model, focused bool, room int) string {
	head := fieldStyle(m.ui, focused).Render(" " + padLine(label+":", lw+1) + " ")
	value := strings.TrimSpace(in.Value())
	if focused && m.set.editing {
		return head + inputView(fitInput(*in, max(room-lipgloss.Width(head), 1)))
	}
	if value == "" {
		value = "-"
	}
	return head + truncateCells(value, max(room-lipgloss.Width(head), 1))
}

// textRow is a read-only line of a page: the label on the field surface of a focused row, then the value.
func (m *Model) textRow(label string, lw int, value string, focused bool, room int) string {
	head := fieldStyle(m.ui, focused).Render(" " + padLine(label+":", lw+1) + " ")
	return head + truncateCells(value, max(room-lipgloss.Width(head), 1))
}

func (m *Model) updatePage(p *pageDraw) {
	u := &m.set.upd
	running := m.version
	if running == "" {
		running = m.svc.st.Version
	}
	labels := []string{m.tr(tLblRunning), m.tr(tLblLatest)}
	lw := labelWidth(labels)
	p.add(statusRow(m.ui, statusOK, padLine(labels[0], lw)+":  "+running))
	kind, state := statusDim, m.tr(tValNoData)
	switch u.phase {
	case updChecking:
		state = m.tr(tUpdChecking)
	case updFailed:
		kind, state = statusWarn, m.tr(tUpdFailed)
	case updDone:
		switch {
		case u.info.Dev:
			state = m.tr(tUpdDev)
		case u.info.Latest != "":
			kind, state = statusWarn, m.tr(tUpdAvailable, u.info.Latest)
		default:
			kind, state = statusOK, m.tr(tUpdCurrent)
		}
	}
	p.add(statusRow(m.ui, kind, padLine(labels[1], lw)+":  "+state))
	if u.phase == updDone && u.info.Latest != "" && !u.info.Dev {
		p.add(m.indented(m.tr(tUpdHint, m.keyLabel(actSelfUpdate))))
	}
}
