package tui

import (
	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The questions and messages of the editor are the kit's dialog: the text and the answers come from here, the
// kit draws them over the dimmed screen or, where that has no room, in the workspace with the answers in the
// footer.

// confirmAnswers are the answers of a question: the confirming one with its own word, and no, which esc gives
// too. None of them is the default, so enter never answers a question.
func confirmAnswers(yes textID) []kittui.Answer {
	return []kittui.Answer{{Action: actYes, Label: yes}, {Action: actNo, Label: tHintCancel}, {Action: actBack, Label: tHintCancel}}
}

// planLines are the commands of a plan as detail lines, dimmed and behind a prompt sign, set apart from the
// sentence above by an empty line.
func planLines(commands []string) []kittui.DialogLine {
	if len(commands) == 0 {
		return nil
	}
	lines := []kittui.DialogLine{{}}
	for _, c := range commands {
		lines = append(lines, kittui.DialogLine{Prefix: "$ ", Text: c, Style: kittui.StyleHint})
	}
	return lines
}

// warnLine and hintLine are detail lines of a message.
func warnLine(text string) kittui.DialogLine {
	return kittui.DialogLine{Text: text, Style: kittui.StyleWarn}
}

func hintLine(text string) kittui.DialogLine {
	return kittui.DialogLine{Text: text, Style: kittui.StyleHint}
}

// openDialog opens spec as the dialog of screenConfirm and reports whether it did. A spec the kit refuses is a
// defect of this package; nothing is asked then, so nothing runs, and the message line says why.
func (m *Model) openDialog(spec kittui.DialogSpec) bool {
	m.clearMessages()
	d, err := kittui.NewDialog(spec, m.ui.keys, m.ui.text)
	if err != nil {
		m.fail = err.Error()
		return false
	}
	m.dlg, m.dlgSpec, m.screen = d, spec, screenConfirm
	return true
}

// askDialog asks spec. Only y runs yes; cancel runs when the question is dropped unanswered or cannot be asked,
// such as to overwrite a secret it holds.
func (m *Model) askDialog(spec kittui.DialogSpec, yes func() tea.Cmd, cancel func()) {
	if !m.openDialog(spec) {
		if cancel != nil {
			cancel()
		}
		return
	}
	m.confirm = &confirmAsk{yes: yes, cancel: cancel}
}

// hostNotice shows a message over the screen that only closes; the model's data stays as it was.
func (m *Model) hostNotice(title string, details ...kittui.DialogLine) {
	spec := kittui.DialogSpec{
		Title: tDlgNotice, Question: title, Notice: true, Details: details,
		Answers: []kittui.Answer{{Action: actBack, Label: tHintClose}}, Default: actBack, Keys: kittui.DialogKeys{Enter: actEnter},
	}
	if m.openDialog(spec) {
		m.confirm = &confirmAsk{notice: true}
	}
}

// dialogAnswer is the answer a key gives the open dialog. The kit leaves the keys it does not read; esc and
// no then drop the dialog, and a message closes on y as well.
func (m *Model) dialogAnswer(key tea.KeyPressMsg) (action, bool) {
	if a, ok := m.dialogKey(m.dlg, key); ok {
		return a, true
	}
	switch {
	case m.keyIs(key, actBack), m.keyIs(key, actNo):
		return actBack, true
	case m.dlgSpec.Notice && m.keyIs(key, actYes):
		return actBack, true
	}
	return "", false
}

// updateDialog handles the answer to a question or message of the dialog.
func (m *Model) updateDialog(key tea.KeyPressMsg) tea.Cmd {
	a, ok := m.dialogAnswer(key)
	if !ok {
		return nil
	}
	if m.confirm.notice {
		m.closeDialog()
		return nil
	}
	if a == actYes {
		yes := m.confirm.yes
		m.closeDialog()
		return yes()
	}
	if cancel := m.confirm.cancel; cancel != nil {
		cancel()
	}
	m.closeDialog()
	m.status = m.tr(tCancelled)
	return nil
}

// dialogFrame draws the open dialog over the dimmed screen, or, where the workspace has no room for it, in the
// workspace with its answers in the footer, whose keys then act on it. Only the dialog takes clicks.
func (m *Model) dialogFrame() (string, bool) {
	if m.screen != screenConfirm || m.dlg == nil {
		return "", false
	}
	d := m.dlg
	room := m.ui.newFrame().Workspace(m.frameSpec(""))
	var background string
	m.renderAs(m.baseScreen(), 0, func() {
		m.backdrop = true
		defer func() { m.backdrop = false }()
		background = m.frame(m.workspaceView())
	})
	if view, targets, ok := d.Overlay(m.ui.styles, background, m.termWidth, m.termHeight, room); ok {
		m.addTargets(targets)
		return view, true
	}
	m.keyHint(d.Hints())
	room = m.ui.newFrame().Workspace(m.frameSpec(""))
	view, targets := d.View(m.ui.styles, room.Width, room.Height)
	frame := m.backFrame(view)
	m.addTargets(kittui.OffsetTargets(targets, room.X, room.Y))
	m.footerHits()
	return frame, true
}

// confirmAsk is what follows the answers of the open dialog.
type confirmAsk struct {
	yes func() tea.Cmd
	// notice makes the dialog a message: enter, esc, y and n all just close it.
	notice bool
	// cancel runs when the question is dropped unanswered, such as to overwrite a secret it holds.
	cancel func()
}
