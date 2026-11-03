package tui

import (
	tea "charm.land/bubbletea/v2"

	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The kit draws the frame every screen stands in: the header, the tab line and the sub-tabs of the area, a
// rule, the workspace with the message under it, and the footer. A screen hands it its workspace, its key
// line through keyHint and its position.

// newFrame is the frame drawn in the keys and the language in force.
func (u *uiContext) newFrame() *kittui.Frame { return kittui.NewFrame(u.keys, u.text) }

// layoutWorkspace derives the room of the workspace from the terminal and the frame around it.
func (m *Model) layoutWorkspace() {
	m.width = max(m.termWidth, 1)
	m.height = max(m.ui.newFrame().Workspace(m.frameSpec("")).Height, 1)
}

// workspaceTop is the screen row of the first line of the workspace. Every click target of the workspace
// counts its rows from here.
func (m *Model) workspaceTop() int { return m.ui.newFrame().Workspace(m.frameSpec("")).Y }

// frame draws workspace in the frame and registers the click targets the frame drew.
func (m *Model) frame(workspace string) string {
	view, targets := m.ui.newFrame().Render(m.ui.styles, m.frameSpec(workspace))
	m.frameTargets(targets)
	return view
}

// frameSpec is the frame of the screen shown around workspace.
func (m *Model) frameSpec(workspace string) kittui.FrameSpec {
	spec := kittui.FrameSpec{
		Width:     m.termWidth,
		Height:    m.termHeight,
		Product:   m.product(),
		Input:     m.frameInput(),
		Workspace: workspace,
		Message:   m.frameMessage(),
		Footer:    m.screenKeys,
		Position:  m.screenPos,
	}
	if len(spec.Footer) == 0 {
		spec.Footer = hint(tHintHelp, actHelp)
	}
	if m.backShown() {
		spec.Back = &kittui.Hint{Action: actBack, Label: tHeaderBack}
	}
	if m.home != screenArea {
		return spec
	}
	spec.Status = m.headerMarks()
	if m.helpReachable() {
		spec.HeaderKeys = []kittui.Hint{{Action: actHelp, Label: tHeaderHelp}, {Action: actCommand, Label: tHeaderCmd}}
	}
	spec.Areas = frameAreas()
	spec.ActiveArea, spec.ActiveTab, spec.Focus = m.area.areaID(), m.activeTab(), m.zoneNow()
	return spec
}

// frameInput is the input focus the frame names keys for. A form reads typed text as a whole, but on a
// choice or in its list the keys of its key line act, so they are named there.
func (m *Model) frameInput() kittui.InputFocus {
	if f := m.form; m.screen == screenPrompt && f != nil && f.focus < len(f.rows) && (f.pick != nil || f.rows[f.focus].choice) {
		return kittui.InputNavigation
	}
	return m.inputFocus()
}

// headerLogo is the Nerd Font glyph in front of the name; without that preference the name stands alone.
const headerLogo = ""

// product is the name at the left of the header.
func (m *Model) product() string {
	if m.prefs.Icons == kitprefs.IconsNerdFont {
		return headerLogo + " " + m.tr(tHeaderTitle)
	}
	return m.tr(tHeaderTitle)
}

// headerMarks are the marks of the service after the name: its state where it is not up, and a release to
// update to. Each pairs a symbol with a word, so no state rests on its colour.
func (m *Model) headerMarks() []kittui.StatusMark {
	var marks []kittui.StatusMark
	switch {
	case !m.svc.known:
		marks = append(marks, kittui.StatusMark{Text: m.tr(tHeaderChecking)})
	case !m.svc.reachable && m.svc.noGroup:
		marks = append(marks, kittui.StatusMark{Kind: kittui.StatusFail, Text: m.tr(tServiceGroup), Short: m.tr(tServiceGroupShort)})
	case !m.svc.reachable:
		marks = append(marks, kittui.StatusMark{Kind: kittui.StatusFail, Text: m.tr(tServiceDown), Short: m.tr(tServiceDownShort)})
	}
	if latest := m.updateAvailable(); latest != "" {
		marks = append(marks, kittui.StatusMark{Kind: kittui.StatusWarn, Text: m.tr(tUpdHeader, latest), Short: m.tr(tUpdHeaderShort)})
	}
	return marks
}

// frameMessage is the message under the workspace: the error of the last action, else what is running,
// else what the last action did.
func (m *Model) frameMessage() *kittui.Message {
	switch {
	case m.fail != "":
		return &kittui.Message{Kind: kittui.MessageError, Text: m.tr(tNoteError, m.fail)}
	case m.busy != "":
		return &kittui.Message{Kind: kittui.MessageBusy, Text: m.tr(tNoteRunning, m.busy)}
	case m.status != "":
		return &kittui.Message{Kind: kittui.MessageStatus, Text: m.status}
	}
	return nil
}

// frameTargets makes the tabs, the back button and the header keys of the frame clickable, and the bars
// zones of their own. The keys of the footer are clickable only where they answer a dialog (see footerHits).
func (m *Model) frameTargets(targets []kittui.Target) {
	m.footerTargets = nil
	live := m.screen == screenArea
	zoned := map[int]bool{}
	for _, t := range targets {
		x0, y0, x1, y1 := t.Rect.X, t.Rect.Y, t.Rect.X+t.Rect.Width, t.Rect.Y+t.Rect.Height
		switch {
		case t.Action != "" && y0 == m.termHeight-1:
			m.footerTargets = append(m.footerTargets, t)
		case t.Action != "":
			act := t.Action
			m.addFrameHit(x0, y0, x1, y1, func() tea.Cmd { return m.pressAction(act) })
		case live && t.Tab != "":
			id := t.Tab
			m.addHit(x0, y0, x1, y1, func() tea.Cmd {
				m.endInput()
				sub, ok := m.tabIndex(id)
				if !ok {
					return nil
				}
				return m.setSubPage(sub)
			})
		case live && t.Area != "":
			if a, ok := areaOf(t.Area); ok {
				m.addHit(x0, y0, x1, y1, func() tea.Cmd { m.endInput(); return m.setArea(a) })
			}
		}
		if live && t.Zone != "" && !zoned[y0] {
			zoned[y0] = true
			m.addZone(0, y0, m.termWidth, y1, t.Zone)
		}
	}
}

// footerHits makes the keys of the footer clickable, where the footer holds the answers of a dialog.
func (m *Model) footerHits() {
	for _, t := range m.footerTargets {
		act := t.Action
		m.addHit(t.Rect.X, t.Rect.Y, t.Rect.X+t.Rect.Width, t.Rect.Y+t.Rect.Height, func() tea.Cmd { return m.pressAction(act) })
	}
}
