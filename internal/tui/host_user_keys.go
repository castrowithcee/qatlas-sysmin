package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The SSH keys of a new person are a list field of the create form. Its editor stands over the form: a line
// takes one public key at a time, enter adds it, enter on the empty line keeps the keys added, and esc drops
// what was added since the editor opened.

// fieldKeys is the list field of the SSH keys of a new person.
const fieldKeys kittui.FieldID = "keys"

// keyEditor is the editor of the SSH keys over the form.
type keyEditor struct {
	line textLine
	// before are the keys when the editor opened, which esc restores.
	before  []userKey
	problem string
}

func (m *Model) openKeyEditor(f *formDlg) {
	f.keyEd = &keyEditor{before: slices.Clone(f.keys.keys)}
}

// keyEditKey handles a key of the key editor; every other key is text of the line.
func (m *Model) keyEditKey(f *formDlg, key tea.KeyPressMsg) tea.Cmd {
	ed := f.keyEd
	switch {
	case m.keyIs(key, actBack):
		f.keys.keys = ed.before
		f.keyEd = nil
	case m.keyIs(key, actEnter) && strings.TrimSpace(ed.line.value()) == "":
		f.keyEd = nil
		m.setListValue(f, fieldKeys, f.keys.summary(m.ui))
	case m.keyIs(key, actEnter):
		if ed.problem = f.keys.add(m.ui, ed.line.value()); ed.problem == "" {
			ed.line.set("")
		}
	case m.keyIsTyped(key, actInfo):
		m.openPopup(popInfo)
	default:
		if ed.line.edit(key) {
			ed.problem = ""
		}
	}
	return nil
}

// keyEditDialog is the key editor as the dialog the modal draws: the line, its problem and the fingerprints
// of the keys added.
func (m *Model) keyEditDialog(f *formDlg) promptDialog {
	ed := f.keyEd
	width := m.usable(4)
	rows := []string{"  " + ed.line.view(m.ui, width, true, m.tr(tHostKeyPlaceholder))}
	if ed.problem != "" {
		rows = append(rows, m.indentedWith(m.ui.st(kittui.StyleFail), "[!] "+ed.problem))
	}
	for _, k := range f.keys.keys {
		rows = append(rows, m.ui.st(kittui.StyleHint).Render(truncateCells("    "+k.fingerprint, m.usable(0))))
	}
	enter := answer(tHintKeep, actEnter)
	if strings.TrimSpace(ed.line.value()) != "" {
		enter = answer(tHintAdd, actEnter)
	}
	return promptDialog{title: m.tr(tHostFormKeyList), input: strings.Join(rows, "\n"), hint: m.tr(tHostChgKeyHint),
		keys: keyLine(enter, answer(tHintDiscard, actBack)), field: true}
}
