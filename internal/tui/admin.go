package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The interface opens read-only and needs no passphrase to show anything. A changing action goes through
// requireAdmin: with an admin session it runs at once, without one a masked passphrase overlay opens first
// and the action runs after the right passphrase, so one entry covers the unlocking, the session and the
// action. esc ends the overlay and drops the action.

// change is a changing action. call runs off the event loop; done gets its outcome on the event loop and may
// be nil. A nil call is a request for the session alone.
type change struct {
	call func(Backend) error
	done func(m *Model, err error) tea.Cmd
}

type entryKind int

const (
	entryAdmin entryKind = iota
	entrySetup
)

// passEntry is the masked passphrase entry over the screen: the admin passphrase, or the first passphrase of a
// new vault with its confirmation.
type passEntry struct {
	kind entryKind
	// id tells an answer that belongs to this overlay from one of an overlay that was cancelled.
	id     int
	inputs []textinput.Model
	focus  int
	// locked says the vault needs unlocking first; the same passphrase then opens the session.
	locked bool
	// change is the action that follows; retried says it already met admin-required once.
	change  *change
	retried bool
	busy    bool
}

// verifiedMsg is the outcome of the unlocking and session start of an overlay. unlocked says the vault was
// unlocked on the way, whatever else happened.
type verifiedMsg struct {
	id       int
	err      error
	unlocked bool
}

// createdMsg is the outcome of creating the vault.
type createdMsg struct {
	id  int
	err error
}

// changeDoneMsg is the outcome of a changing action.
type changeDoneMsg struct {
	change  change
	retried bool
	err     error
}

// sessionEndedMsg is the outcome of ending the admin session.
type sessionEndedMsg struct{ err error }

func maskedInput() textinput.Model {
	in := newTextInput()
	in.Prompt = ""
	in.EchoMode = textinput.EchoPassword
	return in
}

// openOverlay shows an overlay over the current screen with its first field focused.
func (m *Model) openOverlay(kind entryKind, fields int, locked bool, c *change, retried bool) {
	m.ovSeq++
	ov := &passEntry{kind: kind, id: m.ovSeq, locked: locked, change: c, retried: retried}
	for range fields {
		ov.inputs = append(ov.inputs, maskedInput())
	}
	ov.inputs[0].Focus()
	m.ov = ov
	m.screen = screenPrompt
	m.clearMessages()
}

// closeOverlay wipes the fields and leaves the overlay.
func (m *Model) closeOverlay() {
	if m.ov != nil {
		for i := range m.ov.inputs {
			m.ov.inputs[i].Reset()
		}
	}
	m.ov, m.busy = nil, ""
	m.screen = m.home
}

// requireAdmin runs c at once while an admin session is active, and otherwise asks for the passphrase first.
// A service that cannot be reached, or a vault that does not exist, ends in a message instead: there is
// nothing a passphrase could open.
func (m *Model) requireAdmin(c change) tea.Cmd {
	if m.backend == nil {
		return nil
	}
	if cmd, ok := m.serviceReady(); !ok {
		return cmd
	}
	if m.adminActive() && c.call != nil {
		return m.runChange(c, false)
	}
	m.openOverlay(entryAdmin, 1, m.vaultState() == control.VaultLocked, &c, false)
	return nil
}

// serviceReady reports whether the state allows an admin entry: the service answered and a vault exists.
func (m *Model) serviceReady() (tea.Cmd, bool) {
	switch {
	case !m.svc.known || !m.svc.reachable:
		m.fail = tr(tErrUnreachable)
		return m.statusCmd(), false
	case m.svc.st.Vault == control.VaultAbsent:
		m.fail = tr(tErrVaultAbsent)
		return nil, false
	}
	return nil, true
}

// runChange starts c as a command.
func (m *Model) runChange(c change, retried bool) tea.Cmd {
	if c.call == nil {
		return nil
	}
	b := m.backend
	return func() tea.Msg { return changeDoneMsg{change: c, retried: retried, err: c.call(b)} }
}

// takeChange handles the outcome of a changing action. admin-required means the session ended meanwhile, or
// the admin timeout is 0: the overlay opens once more and the same action follows; an unsaved input of the
// screen below is not touched.
func (m *Model) takeChange(msg changeDoneMsg) tea.Cmd {
	refresh := m.statusCmd()
	if isCode(msg.err, control.CodeAdminRequired) && !msg.retried {
		m.openOverlay(entryAdmin, 1, m.vaultState() == control.VaultLocked, &msg.change, true)
		return refresh
	}
	if msg.err != nil {
		m.fail = errorText(msg.err)
	}
	if msg.change.done == nil {
		return refresh
	}
	return tea.Batch(refresh, msg.change.done(m, msg.err))
}

// lockKey is ctrl+l: it ends the admin session while there is one, opens the setup while there is no vault,
// and otherwise asks for the passphrase, which also unlocks a locked vault.
func (m *Model) lockKey() tea.Cmd {
	if m.backend == nil {
		return nil
	}
	m.clearMessages()
	if m.vaultState() == control.VaultAbsent {
		m.openOverlay(entrySetup, 2, false, nil, false)
		return nil
	}
	if cmd, ok := m.serviceReady(); !ok {
		return cmd
	}
	if m.adminActive() {
		b := m.backend
		return func() tea.Msg { return sessionEndedMsg{err: b.EndSession()} }
	}
	return m.requireAdmin(change{})
}

// takeSessionEnded handles the end of the admin session.
func (m *Model) takeSessionEnded(msg sessionEndedMsg) tea.Cmd {
	if msg.err != nil {
		m.fail = errorText(msg.err)
	} else {
		m.status = tr(tAdminEnded)
	}
	return m.statusCmd()
}

// updateOverlay handles one key of the overlay.
func (m *Model) updateOverlay(key tea.KeyPressMsg) tea.Cmd {
	ov := m.ov
	switch {
	case keyIs(key, actBack):
		m.closeOverlay()
		m.status = tr(tCancelled)
		return nil
	case keyIs(key, actEnter):
		if ov.busy {
			return nil
		}
		if ov.focus < len(ov.inputs)-1 {
			m.focusOverlay(ov.focus + 1)
			return nil
		}
		return m.submitOverlay()
	case keyIs(key, actNext), keyIs(key, actDown):
		m.focusOverlay((ov.focus + 1) % len(ov.inputs))
		return nil
	case keyIs(key, actPrev), keyIs(key, actUp):
		m.focusOverlay((ov.focus + len(ov.inputs) - 1) % len(ov.inputs))
		return nil
	}
	if ov.busy {
		return nil
	}
	var cmd tea.Cmd
	ov.inputs[ov.focus], cmd = ov.inputs[ov.focus].Update(inputMsg(key))
	return cmd
}

func (m *Model) focusOverlay(i int) {
	for j := range m.ov.inputs {
		m.ov.inputs[j].Blur()
	}
	m.ov.focus = i
	m.ov.inputs[i].Focus()
}

// submitOverlay takes the typed passphrase out of its fields and hands it to the service as a command. The
// fields are empty from here on, whatever the outcome.
func (m *Model) submitOverlay() tea.Cmd {
	ov := m.ov
	typed := make([]string, len(ov.inputs))
	for i := range ov.inputs {
		typed[i] = ov.inputs[i].Value()
		ov.inputs[i].Reset()
	}
	m.clearMessages()
	m.focusOverlay(0)
	if typed[0] == "" {
		m.fail = tr(tPassEmpty)
		return nil
	}
	if ov.kind == entrySetup && typed[0] != typed[1] {
		m.fail = tr(tPassDiffer)
		return nil
	}
	m.redactor.Add(typed[0])
	ov.busy, m.busy = true, tr(tBusyChecking)
	b, id, locked := m.backend, ov.id, ov.locked
	pass := typed[0]
	if ov.kind == entrySetup {
		return func() tea.Msg { return createdMsg{id: id, err: b.CreateVault(pass)} }
	}
	return func() tea.Msg {
		var err error
		unlocked := false
		if locked {
			if err = b.Unlock(pass); err == nil {
				unlocked = true
			}
		}
		if err == nil {
			err = b.OpenSession(pass)
		}
		return verifiedMsg{id: id, err: err, unlocked: unlocked}
	}
}

// takeVerified handles the outcome of an admin entry. A wrong passphrase keeps the overlay open with the
// error; the action runs only after the session started, and not at all for an overlay that was cancelled.
func (m *Model) takeVerified(msg verifiedMsg) tea.Cmd {
	refresh := m.statusCmd()
	ov := m.ov
	if ov == nil || ov.id != msg.id {
		return refresh
	}
	m.busy, ov.busy = "", false
	if msg.unlocked {
		ov.locked = false
	}
	switch {
	case msg.err == nil:
		c, retried := ov.change, ov.retried
		m.closeOverlay()
		m.status = tr(tAdminOpened)
		if c == nil {
			return refresh
		}
		return tea.Batch(refresh, m.runChange(*c, retried))
	case isCode(msg.err, control.CodeWrongPassphrase):
		m.fail = errorText(msg.err)
		return refresh
	}
	m.closeOverlay()
	m.fail = errorText(msg.err)
	return refresh
}

// takeCreated handles the outcome of creating the vault. Only a rejected passphrase leaves the overlay open.
func (m *Model) takeCreated(msg createdMsg) tea.Cmd {
	refresh := m.statusCmd()
	ov := m.ov
	if ov == nil || ov.id != msg.id {
		return refresh
	}
	m.busy, ov.busy = "", false
	if msg.err != nil && isCode(msg.err, control.CodeBadRequest) {
		m.fail = errorText(msg.err)
		return refresh
	}
	m.closeOverlay()
	if msg.err != nil {
		m.fail = errorText(msg.err)
	} else {
		m.status = tr(tVaultCreated)
	}
	return refresh
}

// overlayDialog is the dialog the overlay draws, in the shape of every short input.
func (m *Model) overlayDialog() promptDialog {
	ov := m.ov
	width := m.usable(0)
	labels := []string{tr(tPassLabel)}
	d := promptDialog{field: true}
	switch ov.kind {
	case entrySetup:
		labels = []string{tr(tPassLabel), tr(tPassConfirmLabel)}
		d.title, d.hint = tr(tSetupTitle), tr(tSetupHint)
		d.keys = "enter ok · tab next · {back} cancel"
	case entryAdmin:
		d.title, d.hint = tr(tAdminTitle), tr(tAdminHint)
		d.keys = "enter ok · {back} cancel"
		if ov.locked {
			d.title, d.hint = tr(tUnlockTitle), tr(tUnlockHint)
			d.keys = "enter unlock · {back} cancel"
		}
	}
	rows, label := "", 0
	for _, l := range labels {
		label = max(label, lipgloss.Width(l))
	}
	for i, in := range ov.inputs {
		prefix := "  " + padLine(labels[i]+":", label+1) + " "
		if i > 0 {
			rows += "\n"
		}
		rows += prefix + inputView(fitInput(in, max(width-len(prefix)-1, 1)))
	}
	d.input = rows
	return d
}
