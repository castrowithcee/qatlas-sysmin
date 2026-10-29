package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// A credential created on the way is typed into a panel, the secret masked, and sent with SetCredential
// through requireAdmin. The setup holds the name afterwards and never the value; the panel empties its fields
// when it is sent or left, and the value is registered with the redactor, so no text of the interface shows it.

// credentialPrefill is what a panel starts with: the fields that are no secret, kept when a save is refused.
type credentialPrefill struct {
	name, kind string
	plain      map[string]string
}

// credentialKinds are the kinds the panel can create out of those a row accepts: one with a secret that
// spans lines cannot be typed into a single line.
func credentialKinds(accepted []string) []string {
	var out []string
	for _, k := range control.CredentialKinds {
		if !k.Multiline && slices.Contains(accepted, k.Name) {
			out = append(out, k.Name)
		}
	}
	return out
}

// openCredentialPrompt shows the panel that creates a credential for the row of the setup f.
func (m *Model) openCredentialPrompt(f *projectFlow, row string, accepted []string, existing map[string]bool, pre credentialPrefill) {
	kinds := credentialKinds(accepted)
	if len(kinds) == 0 {
		m.fail = tr(tErrService)
		return
	}
	p := &projectPrompt{kind: promptCredential, flow: f, row: row, kinds: kinds, existing: existing}
	name := newPromptText("name", tr(tCredName))
	name.in.SetValue(pre.name)
	kind := newPromptChoice("kind", tr(tCredKind), kinds)
	kind.at = max(slices.Index(kinds, pre.kind), 0)
	p.fields = []promptField{name, kind}
	p.addCredentialFields(pre.plain)
	m.openProjectPrompt(p)
}

// addCredentialFields appends the fields of the kind chosen: the plain ones, then the secret.
func (p *projectPrompt) addCredentialFields(plain map[string]string) {
	k, _ := control.KindOf(p.fields[1].choices[p.fields[1].at])
	for _, id := range k.Plain {
		f := newPromptText(id, id)
		f.in.SetValue(plain[id])
		p.fields = append(p.fields, f)
	}
	p.fields = append(p.fields, newPromptSecret(k.Secret, k.Secret))
}

// rebuildCredential redraws the fields after another kind was chosen. What was typed for the old kind is
// emptied, the secret included.
func (p *projectPrompt) rebuildCredential() {
	for i := 2; i < len(p.fields); i++ {
		p.fields[i].in.Reset()
		p.fields[i].in.close()
	}
	p.fields = p.fields[:2]
	p.addCredentialFields(nil)
}

// credentialSubmit checks the panel and sends the credential. A problem of the input stays on the panel.
func (m *Model) credentialSubmit() tea.Cmd {
	p := m.prj.prompt
	name := strings.TrimSpace(p.fields[0].in.Value())
	kind := p.fields[1].choices[p.fields[1].at]
	k, _ := control.KindOf(kind)
	values := map[string]string{}
	pre := credentialPrefill{name: name, kind: kind, plain: map[string]string{}}
	filled := true
	for _, f := range p.fields[2:] {
		v := f.in.Value()
		filled = filled && v != ""
		values[f.id] = v
		if f.id != k.Secret {
			pre.plain[f.id] = v
		}
	}
	switch {
	case !config.ValidCredentialName(name):
		m.fail = tr(tCredBadName)
		return nil
	case p.existing[name]:
		m.fail = tr(tCredExists)
		return nil
	case !filled:
		m.fail = tr(tCredFill)
		return nil
	}
	m.redactor.Add(values[k.Secret])
	f, row, accepted, existing := p.flow, p.row, p.kinds, p.existing
	m.closeProjectPrompt()
	params := control.CredentialSetParams{Name: name, Kind: kind, Fields: values}
	return m.requireAdmin(change{
		busy: tr(tBusyCredSave, name),
		call: func(b Backend) error {
			_, err := b.SetCredential(params)
			return err
		},
		done: func(m *Model, err error) tea.Cmd {
			// A request that is repeated after the passphrase needs the values until here.
			clear(params.Fields)
			m.busy = ""
			current := m.flowOpen() && m.prj.flow == f && m.screen == screenArea
			if err != nil {
				text := credentialError(err)
				if current {
					m.openCredentialPrompt(f, row, accepted, existing, pre)
				}
				m.fail = text
				return nil
			}
			m.status = tr(tCredSaved, name)
			if r, ok := f.rowByID(row); ok && current {
				m.flowSet(r, name)
			}
			return nil
		},
	})
}

// credentialError is the fixed text of a refused credential.
func credentialError(err error) string {
	if isCode(err, control.CodeKindMismatch) {
		return tr(tCredExists)
	}
	return errorText(err)
}
