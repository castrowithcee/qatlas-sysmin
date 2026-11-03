package tui

import (
	"errors"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The intervals of the header. The service is asked every pollEvery at most, which stays below the idle limit
// of its connections and keeps its log short, because every request is a log entry there. They are variables
// only so a test can change them.
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
	// noGroup says the last Status failed because this login session does not carry the group qsysmin yet.
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

// tickMsg drives the periodic Status.
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
		m.svc.reachable, m.svc.fail = false, errorText(m.ui, msg.err)
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

// isDenied reports whether err says the caller may not use qsysmin.
func isDenied(err error) bool {
	return errors.Is(err, client.ErrReserved) || errors.Is(err, client.ErrNotEnabled)
}

// errorText is the fixed, short text of a failure. It never repeats what the service or the system said, so a
// secret cannot reach the screen through it.
func errorText(ui *uiContext, err error) string {
	switch {
	case errors.Is(err, client.ErrReserved):
		return ui.tr(tErrReserved)
	case errors.Is(err, client.ErrNotEnabled):
		return ui.tr(tErrNotEnabled)
	case errors.Is(err, client.ErrSessionGroup):
		return ui.tr(tErrSessionGroup)
	}
	var pe *client.PeerError
	if errors.As(err, &pe) {
		if pe.Silent {
			return ui.tr(tErrPeerSilent, pe.PID, pe.UID, pe.PID)
		}
		return ui.tr(tErrPeer, pe.PID, pe.UID, pe.PID)
	}
	var ce *control.Error
	if !errors.As(err, &ce) {
		return ui.tr(tErrUnreachable)
	}
	switch ce.Code {
	case control.CodeBusy:
		return ui.tr(tErrBusy)
	case control.CodeTimeout:
		return ui.tr(tErrTimeout)
	case control.CodeBadRequest:
		return ui.tr(tErrInvalid)
	}
	return ui.tr(tErrService)
}

// listsCmd reads the log when its page is shown and has not been read yet.
func (m *Model) listsCmd() tea.Cmd {
	// The log is read once when the page opens, not again on a timer: reading it is an entry of the log.
	if m.area == areaActivity && m.act.sub == actLog && !m.act.asked {
		return m.activityCmd()
	}
	return nil
}
