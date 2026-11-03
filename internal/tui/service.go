package tui

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The intervals of the header. The countdown of the admin session runs locally; the service is asked every
// pollEvery at most, which stays below the idle limit of its connections and keeps its log short, because
// every request is a log entry there. They are variables only so a test can change them.
var (
	tickEvery = 10 * time.Second
	pollEvery = 60 * time.Second
)

// svcState is what the interface knows of the service: the last answer of Status and when it came.
type svcState struct {
	// known is false until the first answer or failure arrived.
	known bool
	// reachable is false while the last Status failed; fail then says why, as a fixed text.
	reachable bool
	fail      string
	// noGroup says the last Status failed because this login session does not carry the group sysmin yet.
	noGroup bool
	// foreign says the last Status failed because a process other than the service answers at the socket.
	foreign bool
	st      control.StatusResult
	// at is when st arrived; checked is when the service was last asked, answered or not.
	at, checked time.Time
	// pending says a Status call is out; again says another was wanted meanwhile and follows.
	pending, again bool
}

// statusMsg is the answer of a Status call.
type statusMsg struct {
	st  control.StatusResult
	err error
}

// tickMsg drives the countdown and the periodic Status.
type tickMsg struct{}

func tickCmd() tea.Cmd { return tea.Tick(tickEvery, func(time.Time) tea.Msg { return tickMsg{} }) }

// statusCmd asks the service for its state. While a call is out, the next is deferred until it answered, so
// an answer older than an action is never the last word.
func (m *Model) statusCmd() tea.Cmd {
	if m.backend == nil {
		return nil
	}
	if m.svc.pending {
		m.svc.again = true
		return nil
	}
	m.svc.pending = true
	b := m.backend
	return func() tea.Msg {
		st, err := b.Status()
		return statusMsg{st, err}
	}
}

// takeStatus records the answer of a Status call.
func (m *Model) takeStatus(msg statusMsg) tea.Cmd {
	now := m.timeNow()
	m.svc.pending = false
	m.svc.known, m.svc.checked = true, now
	m.svc.noGroup = errors.Is(msg.err, client.ErrSessionGroup)
	var pe *client.PeerError
	m.svc.foreign = errors.As(msg.err, &pe)
	if msg.err != nil {
		m.svc.reachable, m.svc.fail = false, errorText(msg.err)
	} else {
		m.svc.reachable, m.svc.fail = true, ""
		m.svc.st, m.svc.at = msg.st, now
	}
	if !m.serviceUp() {
		m.ovr.clearLog()
	}
	if isDenied(msg.err) {
		m.denied = msg.err
		return tea.Quit
	}
	if m.svc.again {
		m.svc.again = false
		return tea.Batch(m.statusCmd(), m.overviewReads(), m.listsCmd())
	}
	return tea.Batch(m.overviewReads(), m.listsCmd())
}

// takeTick schedules the next tick and asks the service when the last question is old enough.
func (m *Model) takeTick() tea.Cmd {
	if m.backend == nil {
		return nil
	}
	if m.timeNow().Sub(m.svc.checked) >= pollEvery {
		return tea.Batch(tickCmd(), m.statusCmd())
	}
	return tickCmd()
}

// serviceUp reports whether the last Status was answered.
func (m *Model) serviceUp() bool { return m.svc.known && m.svc.reachable }

// vaultState is the vault state of the last answer, "" while the service is unknown or unreachable.
func (m *Model) vaultState() string {
	if !m.serviceUp() {
		return ""
	}
	return m.svc.st.Vault
}

// adminLeft is how long the admin session of this window still lasts, counted down locally from the last
// answer; zero for none, and for a session that covers one change only (see adminOnce).
func (m *Model) adminLeft() time.Duration {
	if !m.svc.reachable || !m.svc.st.Admin || m.svc.st.AdminRemaining <= 0 {
		return 0
	}
	left := time.Duration(m.svc.st.AdminRemaining)*time.Second - m.timeNow().Sub(m.svc.at)
	return max(left, 0)
}

// adminOnce reports a session that covers exactly one change: the admin timeout is 0.
func (m *Model) adminOnce() bool {
	return m.svc.reachable && m.svc.st.Admin && m.svc.st.AdminRemaining <= 0
}

// adminActive reports whether a change would meet an admin session now, as far as the interface knows. The
// service has the last word: a change it refuses as admin-required asks for the passphrase again.
func (m *Model) adminActive() bool { return m.adminLeft() > 0 || m.adminOnce() }

// adminMinutes is the remaining time in whole minutes, rounded up, so a session never reads 0 while it lasts.
func (m *Model) adminMinutes() int {
	left := m.adminLeft()
	return int((left + time.Minute - 1) / time.Minute)
}

// isDenied reports whether err says the caller may not use sysmin.
func isDenied(err error) bool {
	return errors.Is(err, client.ErrReserved) || errors.Is(err, client.ErrNotEnabled)
}

// errorText is the fixed, short text of a failure. It never repeats what the service or the system said, so a
// passphrase cannot reach the screen through it.
func errorText(err error) string {
	switch {
	case errors.Is(err, client.ErrReserved):
		return tr(tErrReserved)
	case errors.Is(err, client.ErrNotEnabled):
		return tr(tErrNotEnabled)
	case errors.Is(err, client.ErrSessionGroup):
		return tr(tErrSessionGroup)
	}
	var pe *client.PeerError
	if errors.As(err, &pe) {
		if pe.Silent {
			return tr(tErrPeerSilent, pe.PID, pe.UID, pe.PID)
		}
		return tr(tErrPeer, pe.PID, pe.UID, pe.PID)
	}
	var ce *control.Error
	if !errors.As(err, &ce) {
		return tr(tErrUnreachable)
	}
	switch ce.Code {
	case control.CodeWrongPassphrase:
		return tr(tErrWrongPassphrase)
	case control.CodeLocked:
		return tr(tErrLocked)
	case control.CodeAdminRequired:
		return tr(tErrAdminRequired)
	case control.CodeVaultAbsent:
		return tr(tErrVaultAbsent)
	case control.CodeExists:
		return tr(tErrExists)
	case control.CodeBusy:
		return tr(tErrBusy)
	case control.CodeTimeout:
		return tr(tErrTimeout)
	case control.CodeBadRequest, control.CodeInvalid:
		return tr(tErrInvalid)
	}
	return tr(tErrService)
}

// isCode reports whether err is a failure of the service with the given code.
func isCode(err error, code string) bool {
	var ce *control.Error
	return errors.As(err, &ce) && ce.Code == code
}

// listsCmd reads the log when its page is shown and has not been read yet.
func (m *Model) listsCmd() tea.Cmd {
	// The log is read once when the page opens, not again on a timer: reading it is an entry of the log.
	if m.area == areaActivity && m.act.sub == actLog && !m.act.asked {
		return m.activityCmd()
	}
	return nil
}
