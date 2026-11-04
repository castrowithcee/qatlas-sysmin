package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The SSH keys of a new person are a list field of the create form. Its editor is a kit dialog over the form: a
// line takes one public key at a time, enter adds it, enter on the empty line keeps the keys added, and esc
// drops what was added since the editor opened.

// fieldKeys is the list field of the SSH keys of a new person.
const fieldKeys kittui.FieldID = "keys"

// keyEditor is the editor of the SSH keys over the form.
type keyEditor struct {
	dlg *kittui.Dialog
	// before are the keys when the editor opened, which esc restores.
	before []userKey
	// adds says enter adds the line, as against keeping the keys when the line is empty.
	adds bool
}

func (m *Model) openKeyEditor(f *formDlg) {
	f.keyEd = &keyEditor{before: slices.Clone(f.keys.keys)}
	m.buildKeyDialog(f, "")
}

// buildKeyDialog builds the dialog of the key editor anew, as its answers and the fingerprints of the keys
// added are fixed when the kit builds a dialog; value is what the line holds.
func (m *Model) buildKeyDialog(f *formDlg, value string) {
	ed := f.keyEd
	ed.adds = strings.TrimSpace(value) != ""
	enter := tHintKeep
	if ed.adds {
		enter = tHintAdd
	}
	var details []kittui.DialogLine
	for _, k := range f.keys.keys {
		details = append(details, hintLine(k.fingerprint))
	}
	spec := kittui.DialogSpec{
		Title: tHostFormKeyList, Question: m.tr(tHostChgKeyHint), Details: details,
		Answers: []kittui.Answer{{Action: actEnter, Label: enter}, {Action: actBack, Label: tHintDiscard}},
		Input:   &kittui.DialogInput{Value: value},
	}
	d, err := kittui.NewDialog(spec, m.ui.keys, m.ui.text)
	if err != nil {
		panic(fmt.Sprintf("the key editor is not a dialog: %v", err))
	}
	ed.dlg = d
}

// keyEditKey handles a key of the key editor; every key it does not answer is text of the line.
func (m *Model) keyEditKey(f *formDlg, key tea.KeyPressMsg) tea.Cmd {
	ed := f.keyEd
	if content, ok := pasted(key); ok {
		ed.dlg.Paste(content)
		m.syncKeyDialog(f)
		return nil
	}
	if m.keyIsTyped(key, actInfo) {
		m.openPopup(popInfo)
		return nil
	}
	answer, handled := ed.dlg.Update(key)
	switch {
	case answer != "":
		m.keyEditAnswer(f, answer)
	case handled:
		m.syncKeyDialog(f)
	}
	return nil
}

// syncKeyDialog redraws the word of the enter answer when the line turned empty or not.
func (m *Model) syncKeyDialog(f *formDlg) {
	ed := f.keyEd
	if (strings.TrimSpace(ed.dlg.Value()) != "") != ed.adds {
		m.buildKeyDialog(f, ed.dlg.Value())
	}
}

// keyEditAnswer is the answer of the key editor, by key or by click.
func (m *Model) keyEditAnswer(f *formDlg, answer action) {
	ed := f.keyEd
	value := ed.dlg.Value()
	switch {
	case answer == actBack:
		f.keys.keys = ed.before
		f.keyEd = nil
	case strings.TrimSpace(value) == "":
		f.keyEd = nil
		m.setListValue(f, fieldKeys, f.keys.summary(m.ui))
	default:
		if problem := f.keys.add(m.ui, value); problem != "" {
			ed.dlg.SetMessage(&kittui.Message{Kind: kittui.MessageError, Text: problem})
		} else {
			m.buildKeyDialog(f, "")
		}
	}
}

// clickKeyEdit is a click on a target of the key editor: a button answers as its key does, and a click on the
// line only keeps it. No click types.
func (m *Model) clickKeyEdit(f *formDlg, res kittui.MouseResult) tea.Cmd {
	if m.form != f || m.screen != screenPrompt || f.keyEd == nil {
		return nil
	}
	if answer, ok := f.keyEd.dlg.Click(res); ok && answer != "" {
		m.keyEditAnswer(f, answer)
	}
	return nil
}

// keyEditFrame draws the key editor as the kit's dialog over the dimmed screen, or, where the workspace has no
// room for it, in the workspace with its answers in the footer. Only the dialog takes clicks.
func (m *Model) keyEditFrame() (string, bool) {
	f := m.form
	if m.screen != screenPrompt || f == nil || f.keyEd == nil {
		return "", false
	}
	d := f.keyEd.dlg
	room := m.ui.newFrame().Workspace(m.frameSpec(""))
	var background string
	m.renderAs(m.baseScreen(), 0, func() {
		m.backdrop = true
		defer func() { m.backdrop = false }()
		background = m.frame(m.workspaceView())
	})
	click := func(res kittui.MouseResult) tea.Cmd { return m.clickKeyEdit(f, res) }
	if view, targets, ok := d.Overlay(m.ui.styles, background, m.termWidth, m.termHeight, room); ok {
		m.addFormTargets(targets, 0, 0, "", click)
		return view, true
	}
	m.keyHint(d.Hints())
	room = m.ui.newFrame().Workspace(m.frameSpec(""))
	view, targets := d.View(m.ui.styles, room.Width, room.Height)
	frame := m.backFrame(view)
	m.addFormTargets(targets, room.X, room.Y, "", click)
	m.footerHits()
	return frame, true
}
