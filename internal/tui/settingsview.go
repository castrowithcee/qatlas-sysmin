package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// settingsBody is the page of Settings under the bars of the frame.
func (m *Model) settingsBody() string {
	m.loadForms()
	if m.set.sub == subGeneral {
		return m.generalPage()
	}
	pg := &pageDraw{m: m, y: m.workspaceTop()}
	m.updatePage(pg)
	return strings.Join(pg.lines, "\n")
}

// generalPage draws the form of General in the workspace. The wheel over it moves the focus of the field and
// stops at both ends, never leaving for the page strip.
func (m *Model) generalPage() string {
	g := m.set.gen
	top, width := m.workspaceTop(), m.usable(0)
	view, targets := g.View(m.ui.styles, width, m.height)
	m.addWheel(0, top, width, top+m.height, zoneContent, func(up bool) tea.Cmd {
		steps := 1
		if up {
			steps = -1
		}
		g.Wheel(steps)
		return nil
	})
	m.addFormTargets(targets, 0, top, zoneContent, m.clickGeneral)
	return view
}

// pageDraw collects the lines of a page and keeps the screen row of the next one.
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

// labelWidth is the widest of the labels.
func labelWidth(labels []string) int {
	w := 0
	for _, l := range labels {
		w = max(w, lipgloss.Width(l))
	}
	return w
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
