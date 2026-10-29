package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The short inputs of the Projects area, the directory of a stack to adopt and a new credential, are a panel
// over the dimmed screen like every short input. The panel holds its fields itself; nothing it holds reaches
// the setup before it is sent, and a field is emptied when the panel closes, whatever closed it.

type projectPromptKind int

const (
	promptAdopt projectPromptKind = iota
	promptCredential
)

// promptField is one line of the panel: typed text, a path, a masked secret, or one of a few values.
type promptField struct {
	id, label string
	in        projectPathInput
	path      bool
	// choices are the values of a choice field; at is the one chosen.
	choices []string
	at      int
}

func (f *promptField) isChoice() bool { return len(f.choices) > 0 }

func newPromptText(id, label string) promptField {
	return promptField{id: id, label: label, in: newProjectPathInput()}
}

func newPromptPath(id, label string) promptField {
	f := newPromptText(id, label)
	f.path = true
	return f
}

// newPromptSecret is a field whose text is drawn masked.
func newPromptSecret(id, label string) promptField {
	f := newPromptText(id, label)
	f.in.EchoMode = textinput.EchoPassword
	return f
}

func newPromptChoice(id, label string, choices []string) promptField {
	return promptField{id: id, label: label, choices: choices}
}

// projectPrompt is the panel being shown.
type projectPrompt struct {
	kind   projectPromptKind
	fields []promptField
	focus  int
	// seq tells an answer that belongs to this panel from one of a panel that was closed.
	seq  int
	busy bool
	// The credential the panel creates: the setup and the row that gets its name, the kinds the row accepts and
	// the names that exist.
	flow     *projectFlow
	row      string
	kinds    []string
	existing map[string]bool
}

// wipe empties every field.
func (p *projectPrompt) wipe() {
	for i := range p.fields {
		p.fields[i].in.Reset()
		p.fields[i].in.close()
	}
}

// focusOn moves the typing to field i.
func (p *projectPrompt) focusOn(i int) {
	p.fields[p.focus].in.close()
	p.focus = i
	if f := &p.fields[i]; !f.isChoice() {
		mode := projectPathPlain
		if f.path {
			mode = projectPathDir
		}
		f.in.open(f.in.Value(), mode, "/")
	}
}

func (p *projectPrompt) move(by int) {
	p.focusOn((p.focus + by + len(p.fields)) % len(p.fields))
}

// openProjectPrompt shows p over the area, with the typing in its first field.
func (m *Model) openProjectPrompt(p *projectPrompt) {
	m.prj.promptSeq++
	p.seq = m.prj.promptSeq
	m.prj.prompt = p
	m.screen = screenPrompt
	m.clearMessages()
	p.focus = 0
	p.focusOn(0)
}

// closeProjectPrompt empties the panel and leaves it.
func (m *Model) closeProjectPrompt() {
	if p := m.prj.prompt; p != nil {
		p.wipe()
	}
	m.prj.prompt = nil
	m.busy = ""
	m.closeDialog()
}

// projectPromptOpen reports whether the panel of the area is on screen.
func (m *Model) projectPromptOpen() bool {
	return m.prj != nil && m.prj.prompt != nil && m.screen == screenPrompt && m.ov == nil
}

// projectPromptKey handles a key of the panel. Letters are text in a text field; enter goes to the next field
// and, on the last one of the adoption, asks the service; the credential is saved with the save key.
func (m *Model) projectPromptKey(key tea.KeyPressMsg) tea.Cmd {
	p := m.prj.prompt
	cur := &p.fields[p.focus]
	switch {
	case keyIs(key, actBack):
		m.closeProjectPrompt()
		m.status = tr(tCancelled)
	case p.busy:
	case p.kind == promptCredential && keyIs(key, actSave):
		return m.credentialSubmit()
	case keyIs(key, actNext):
		if !(cur.path && cur.in.complete()) {
			p.move(1)
		}
	case keyIs(key, actPrev), keyIs(key, actUp):
		p.move(-1)
	case keyIs(key, actDown):
		p.move(1)
	case keyIs(key, actEnter):
		switch {
		case cur.path && cur.in.marked && cur.in.complete():
		case p.focus < len(p.fields)-1:
			p.move(1)
		case p.kind == promptAdopt:
			return m.adoptSubmit()
		}
	case cur.isChoice():
		if keyIs(key, actLeft) || keyIs(key, actRight) || keyIs(key, actSpace) {
			by := 1
			if keyIs(key, actLeft) {
				by = -1
			}
			cur.at = wrap(cur.at+by, len(cur.choices))
			if p.kind == promptCredential {
				p.rebuildCredential()
			}
		}
	case keyIs(key, actSuggestNext):
		cur.in.move(1)
	case keyIs(key, actSuggestPrev):
		cur.in.move(-1)
	default:
		cmd, _ := cur.in.edit(key)
		return cmd
	}
	return nil
}

// projectPromptDialog is the panel as the dialog the modal draws.
func (m *Model) projectPromptDialog() promptDialog {
	p := m.prj.prompt
	width := m.usable(0)
	label := 0
	for _, f := range p.fields {
		label = max(label, lipgloss.Width(f.label))
	}
	var rows []string
	for i, f := range p.fields {
		focused := i == p.focus
		prefix := barMarker(focused) + padLine(f.label+":", label+1) + " "
		room := max(width-lipgloss.Width(prefix)-1, 1)
		if f.isChoice() {
			rows = append(rows, prefix+truncateCells(f.choices[f.at], max(room-2, 1))+" "+dropdownArrow)
			continue
		}
		rows = append(rows, prefix+inputView(fitInput(f.in.Model, room)))
		if focused && f.path {
			suggestions, mark := f.in.shown()
			for j, s := range suggestions {
				line := truncateCells("    "+s, width-1)
				if f.in.marked && j == mark {
					line = styles.title.Render(truncateCells("  > "+s, width-1))
				} else {
					line = styles.hint.Render(line)
				}
				rows = append(rows, line)
			}
		}
	}
	d := promptDialog{field: true, input: strings.Join(rows, "\n")}
	switch p.kind {
	case promptAdopt:
		d.title, d.hint = tr(tPrjAdoptPrompt), tr(tPrjAdoptHint)
		d.keys = "enter ok · tab next · {back} cancel"
		if f := p.fields[p.focus]; f.path && len(f.in.matches) > 0 {
			d.keys = "enter ok · tab complete · {back} cancel"
		}
	case promptCredential:
		d.title, d.hint = tr(tCredTitle), tr(tCredHint)
		d.keys = "enter next · {save} save · {back} cancel"
		if p.fields[p.focus].isChoice() {
			d.keys = "left/right kind · {save} save · {back} cancel"
		}
	}
	return d
}
