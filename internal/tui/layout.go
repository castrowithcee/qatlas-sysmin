package tui

import (
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
	icons := iconsFor(m.prefs.Icons)
	spec.Icon = icons.logo
	if m.home != screenArea {
		return spec
	}
	spec.Status = m.headerMarks()
	if m.helpReachable() {
		spec.HeaderKeys = []kittui.Hint{{Action: actHelp, Label: tHeaderHelp}, {Action: actCommand, Label: tHeaderCmd}}
	}
	spec.Areas = icons.withIcons(frameAreas())
	spec.ActiveArea, spec.ActiveTab, spec.Focus = m.area.areaID(), m.activeTab(), m.zoneNow()
	return spec
}

// frameInput is the input focus the frame names keys for: that of a form, and in a pick list, which types its
// filter, navigation, so the keys of its key line are named.
func (m *Model) frameInput() kittui.InputFocus {
	if m.generalFocused() {
		return m.set.gen.Input()
	}
	if f := m.form; m.screen == screenPrompt && f != nil {
		switch {
		case f.pick != nil:
			return kittui.InputNavigation
		case f.keyEd != nil:
			return kittui.InputText
		}
		return f.kit.Input()
	}
	return m.inputFocus()
}

// product is the name at the left of the header.
func (m *Model) product() string { return m.tr(tHeaderTitle) }

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

// frameTargets adds the targets the frame drew: its tabs, the back button and the header keys, and the bars as
// zones of their own. The keys of the footer act only where they answer a dialog (see footerHits).
func (m *Model) frameTargets(targets []kittui.Target) {
	m.footerTargets = nil
	zoned := map[int]bool{}
	footerTop := m.termHeight - m.footerRows()
	for _, t := range targets {
		if t.Action != "" && t.Rect.Y >= footerTop {
			m.footerTargets = append(m.footerTargets, t)
			continue
		}
		m.addTarget(t)
		if y := t.Rect.Y; t.Zone != "" && !zoned[y] {
			zoned[y] = true
			m.addZone(0, y, m.termWidth, y+t.Rect.Height, t.Zone)
		}
	}
}

// footerRows is the number of rows the footer takes: one, or two where its keys do not fit one. The frame
// gives the rest of the screen to the workspace, so the footer is what stands under it.
func (m *Model) footerRows() int {
	spec := m.frameSpec("")
	spec.Message = nil
	room := m.ui.newFrame().Workspace(spec)
	return max(m.termHeight-room.Y-room.Height, 1)
}

// footerHits adds the keys of the footer, where the footer holds the answers of a dialog.
func (m *Model) footerHits() {
	for _, t := range m.footerTargets {
		m.addTarget(t)
	}
}
