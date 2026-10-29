package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The setup offers what the service knows instead of asking for names: the notification channels as toggles
// and the credentials of the vault as a list to pick from. Neither list carries a secret.

// projectChannelsMsg is the answer to the read of the channels for a setup.
type projectChannelsMsg struct {
	flow *projectFlow
	list []control.ChannelSummary
	err  error
}

func (projectChannelsMsg) projectsAnswer() {}

// flowLoadCmd reads the channels the setup f offers.
func (m *Model) flowLoadCmd(f *projectFlow) tea.Cmd {
	b := m.backend
	return func() tea.Msg {
		list, err := b.Channels()
		return projectChannelsMsg{flow: f, list: list, err: err}
	}
}

// takeProjectChannels hands the channels to the setup that asked. A list that cannot be read leaves the
// channels typed.
func (m *Model) takeProjectChannels(msg projectChannelsMsg) {
	f := msg.flow
	if msg.err != nil || m.prj == nil || m.prj.flow != f {
		return
	}
	f.channels, f.channelsKnown = nil, true
	for _, c := range msg.list {
		f.channels = append(f.channels, safeText(c.Name))
	}
	// The rows of the notify step changed under the focus.
	if _, ok := f.focused(); !ok && f.step != flowSummary {
		if rows := f.shownRows(f.step); len(rows) > 0 {
			f.focusOn(rows[0].id)
		}
	}
}

// rowByID is the row id of any step of the setup.
func (f *projectFlow) rowByID(id string) (flowRow, bool) {
	for _, s := range f.stepOrder() {
		for _, r := range f.rows(s) {
			if r.id == id {
				return r, true
			}
		}
	}
	return flowRow{}, false
}

// projectCredsMsg is the answer to the read of the credentials for the pick row row of a setup.
type projectCredsMsg struct {
	flow *projectFlow
	row  string
	list []control.CredentialSummary
	err  error
}

func (projectCredsMsg) projectsAnswer() {}

// flowPickOpen reads the credentials to list the ones row r accepts.
func (m *Model) flowPickOpen(r flowRow) tea.Cmd {
	f, b := m.prj.flow, m.backend
	m.clearMessages()
	m.busy = tr(tBusyCreds)
	return func() tea.Msg {
		list, err := b.Credentials()
		return projectCredsMsg{flow: f, row: r.id, list: list, err: err}
	}
}

// takeProjectCreds shows the picker of the credentials: none, those of a kind the row accepts by name and
// kind, and the way to create one.
func (m *Model) takeProjectCreds(msg projectCredsMsg) {
	m.busy = ""
	f := msg.flow
	if msg.err != nil {
		m.fail = errorText(msg.err)
		return
	}
	if m.prj == nil || m.prj.flow != f || m.screen != screenArea {
		return
	}
	row, ok := f.rowByID(msg.row)
	if !ok {
		return
	}
	values, names := []string{tr(tCredNone)}, []string{""}
	existing := map[string]bool{}
	for _, c := range msg.list {
		existing[c.Name] = true
		if slices.Contains(row.kinds, c.Kind) {
			values = append(values, safeText(c.Name)+" · "+safeText(c.Kind))
			names = append(names, c.Name)
		}
	}
	add := tr(tCredNew)
	values = append(values, add)
	m.openProjectPicker(tr(tCredPickTitle), tr(tCredPickNote), values, max(slices.Index(names, row.get()), 0), func(v string) tea.Cmd {
		i := slices.Index(values, v)
		switch {
		case i < 0 || !m.flowOpen() || m.prj.flow != f:
		case v == add:
			m.openCredentialPrompt(f, row.id, row.kinds, existing, credentialPrefill{})
		default:
			if r, ok := f.rowByID(row.id); ok {
				m.flowSet(r, names[i])
			}
		}
		return nil
	})
}
