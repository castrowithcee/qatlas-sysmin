package tui

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// Changes to the host run as root through sudo: the interface builds the confirmation from the action's plan,
// hands the terminal to `sudo qsysmin host apply …` and reads the report that command prints. sudo asks for the
// password on the terminal; the interface never sees it.

// HostRequest is one call of the sudo hook: the arguments after "qsysmin host".
type HostRequest struct {
	Args []string
	// Validate asks for the command that makes sudo ask for the password on the terminal and remember it
	// (sudo -v); Args and Secret are empty then.
	Validate bool
	// Secret is the standard input of an action that wants a secret input. Standard input belongs to sudo's
	// password prompt, so a request with a Secret runs in two steps: a Validate request on the terminal, then
	// this request without a prompt (sudo -n) with Secret as the standard input of the command. The hook
	// returns a command whose Stdin carries it; it never goes into the arguments or the environment.
	Secret []byte
}

// HostRunner builds the command that runs a request as root. The interface sets Stdout (it reads the report
// there) and, where unset, Stdin and Stderr to the terminal itself.
type HostRunner func(HostRequest) (*exec.Cmd, error)

// ErrNoSudo is what a HostRunner returns when sudo is not installed.
var ErrNoSudo = errors.New("sudo is not installed")

// hostWrap is the width the commands of a confirmation are wrapped to: the modal cuts its lines at 60 cells.
const hostWrap = 54

// hostRanMsg delivers the end of a command run through the terminal hand-over.
type hostRanMsg struct {
	after func(out []byte, err error) tea.Cmd
	out   []byte
	err   error
}

func (m *Model) takeHostRan(msg hostRanMsg) tea.Cmd { return msg.after(msg.out, msg.err) }

// hostRegistry is the set of actions the interface can describe.
func (m *Model) hostRegistry() *hostchange.Registry {
	if m.hostReg == nil {
		return hostchange.Default()
	}
	return m.hostReg
}

// runHost hands the terminal to the command of req; after gets what it printed on standard output and the
// error of the run. A request with a secret runs through runHostSecret.
func (m *Model) runHost(req HostRequest, after func(out []byte, err error) tea.Cmd) tea.Cmd {
	if len(req.Secret) > 0 {
		return m.runHostSecret(req, nil, after)
	}
	var cmd *exec.Cmd
	err := errors.New("no way to run commands as root")
	if m.hostRun != nil {
		cmd, err = m.hostRun(req)
	}
	if err != nil {
		return func() tea.Msg { return hostRanMsg{after: after, err: err} }
	}
	// sudo asks for the password on the terminal itself, not on standard error, so capturing standard error
	// leaves the prompt visible and gives the reason of a run that printed no report.
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	if cmd.Stdin == nil && m.hostIn != nil {
		cmd.Stdin = m.hostIn
	}
	if cmd.Stderr == nil {
		cmd.Stderr = &errOut
	}
	exec := m.hostExec
	if exec == nil {
		exec = tea.ExecProcess
	}
	return exec(cmd, func(err error) tea.Msg {
		if err != nil && errOut.Len() > 0 {
			err = &hostRunError{err: err, stderr: errOut.String()}
		}
		return hostRanMsg{after: after, out: out.Bytes(), err: err}
	})
}

// hostRunError is the error of a run without a prompt: the failure, and what the command wrote to standard
// error with the secrets taken out.
type hostRunError struct {
	err    error
	stderr string
}

func (e *hostRunError) Error() string { return e.err.Error() }
func (e *hostRunError) Unwrap() error { return e.err }

// zero overwrites a buffer that held a secret.
func zero(b []byte) { clear(b) }

func zeroAll(secret []byte, mask [][]byte) {
	zero(secret)
	for _, b := range mask {
		zero(b)
	}
}

// scrub takes every secret out of text.
func scrub(text string, secrets ...[]byte) string {
	for _, s := range secrets {
		if len(s) > 0 {
			text = strings.ReplaceAll(text, string(s), "<hidden>")
		}
	}
	return text
}

// runHostSecret runs a request that carries a secret in two steps. sudo asks for the password on the real
// terminal in the first (sudo -v), which qsysmin never sees; the second runs without a prompt, the secret on
// the standard input of the command, and stops where sudo would have asked again. The secret is overwritten
// when the run is over. mask are further texts, such as a password inside the secret, that must not reach the
// screen.
func (m *Model) runHostSecret(req HostRequest, mask [][]byte, after func(out []byte, err error) tea.Cmd) tea.Cmd {
	secret := req.Secret
	return m.runHost(HostRequest{Validate: true}, func(_ []byte, err error) tea.Cmd {
		if err != nil {
			zeroAll(secret, mask)
			kind, exit := classifyRun(nil, err)
			m.hostFailNoticeAs(m.tr(tHostChangeFailed), kind, exit, err, false)
			return nil
		}
		var cmd *exec.Cmd
		if m.hostRun == nil {
			err = errors.New("no way to run commands as root")
		} else {
			cmd, err = m.hostRun(req)
		}
		if err != nil {
			zeroAll(secret, mask)
			return func() tea.Msg { return hostRanMsg{after: after, err: err} }
		}
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		run := m.hostBg
		if run == nil {
			run = (*exec.Cmd).Run
		}
		return func() tea.Msg {
			err := run(cmd)
			if err != nil {
				err = &hostRunError{err: err, stderr: scrub(errOut.String(), append([][]byte{secret}, mask...)...)}
			}
			zeroAll(secret, mask)
			return hostRanMsg{after: after, out: out.Bytes(), err: err}
		}
	})
}

// hostReport is what `host apply` and `host undo` print.
type hostReport struct {
	JournalID  string   `json:"journal_id"`
	Action     string   `json:"action"`
	Sentence   string   `json:"sentence"`
	Commands   []string `json:"commands"`
	Result     string   `json:"result"`
	Check      string   `json:"check"`
	CheckError string   `json:"check_error"`
	Restored   bool     `json:"restored"`
	Reversible bool     `json:"reversible"`
	Error      string   `json:"error"`
	Warning    string   `json:"warning"`
}

func parseReport(out []byte) (hostReport, bool) {
	var r hostReport
	if json.Unmarshal(bytes.TrimSpace(out), &r) != nil || r.Result == "" {
		return hostReport{}, false
	}
	return r, true
}

// The ways a run through sudo ends without a usable answer.
type hostFailure int

const (
	failNoSudo hostFailure = iota
	failDenied
	failAborted
	failExit
	failOutput
)

// classifyRun tells why a run gave no usable answer. exit is the exit code of failExit.
func classifyRun(out []byte, err error) (kind hostFailure, exit int) {
	var exitErr *exec.ExitError
	switch {
	case errors.Is(err, ErrNoSudo):
		return failNoSudo, 0
	case err == nil:
		return failOutput, 0
	case !errors.As(err, &exitErr):
		return failNoSudo, 0
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return failAborted, 0
	}
	switch code := exitErr.ExitCode(); {
	case code == 130 || code < 0:
		return failAborted, 0
	case code == 1 && len(bytes.TrimSpace(out)) == 0:
		return failDenied, 0
	default:
		return failExit, code
	}
}

// hostNotice shows a message over the screen that only closes; the model's data stays as it was.
func (m *Model) hostNotice(q question) {
	m.askConfirm(q, nil)
	m.confirm.notice = true
}

// hostFailNotice explains a run that gave no usable answer. change says a host change was asked for, where an
// interrupted or failed run leaves open what was done.
func (m *Model) hostFailNotice(kind hostFailure, exit int, err error, change bool) {
	title := m.tr(tHostReadFailed)
	if change {
		title = m.tr(tHostChangeFailed)
	}
	m.hostFailNoticeAs(title, kind, exit, err, change)
}

// noCredential reports the standard error of sudo -n that has no password to use. The hook runs sudo with the
// C locale, so the words are those of sudo itself.
func noCredential(stderr string) bool {
	return strings.Contains(stderr, "a password is required") || strings.Contains(stderr, "a terminal is required")
}

// stderrLines are the first lines of what a run without a prompt wrote to standard error, cut to the modal.
func stderrLines(err error) string {
	var re *hostRunError
	if !errors.As(err, &re) {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(re.stderr), "\n") {
		if l = strings.TrimSpace(l); l != "" && len(lines) < 3 {
			lines = append(lines, ansi.Truncate(safeText(l), 200, "…"))
		}
	}
	return strings.Join(lines, " ")
}

func (m *Model) hostFailNoticeAs(title string, kind hostFailure, exit int, err error, change bool) {
	var text string
	reason := stderrLines(err)
	switch {
	case noCredential(reason):
		text = m.tr(tHostErrNoCredential)
	case change && reason != "" && !strings.HasPrefix(reason, "sudo:") && (kind == failDenied || kind == failExit):
		// A line of sudo itself ("sudo: 3 incorrect password attempts") keeps the message about sudo; anything
		// else is the reason qsysmin gave.
		text = m.tr(tHostErrReported, m.diffText(reason))
	default:
		text = m.hostFailText(kind, exit, err, change)
	}
	m.hostNotice(question{title: title, consequence: text, warn: true, danger: true, keys: closeKeys()})
}

func (m *Model) hostFailText(kind hostFailure, exit int, err error, change bool) string {
	var text string
	switch kind {
	case failNoSudo:
		text = m.tr(tHostErrNoSudo)
		if err != nil && !errors.Is(err, ErrNoSudo) {
			text = m.tr(tHostErrStart, err.Error())
		}
	case failDenied:
		text = m.tr(tHostErrDenied)
	case failAborted:
		text = m.tr(tHostErrAbortedRead)
		if change {
			text = m.tr(tHostErrAbortedChange)
		}
	case failExit:
		text = m.tr(tHostErrExitRead, exit)
		if change {
			text = m.tr(tHostErrExitChange, exit)
		}
	default:
		text = m.tr(tHostErrOutput)
	}
	return text
}

// wrapWords breaks text into lines of at most width cells, at spaces only: a break inside a path or an option
// would make the command ambiguous to read. A word wider than a line is cut at the line's end.
func wrapWords(text string, width int) []string {
	var lines []string
	line := ""
	flush := func() {
		if line != "" {
			lines = append(lines, line)
		}
		line = ""
	}
	for _, word := range strings.Split(text, " ") {
		for ansi.StringWidth(word) > width {
			flush()
			lines = append(lines, ansi.Truncate(word, width, ""))
			word = ansi.TruncateLeft(word, width, "")
		}
		switch {
		case line == "":
			line = word
		case ansi.StringWidth(line)+1+ansi.StringWidth(word) <= width:
			line += " " + word
		default:
			flush()
			line = word
		}
	}
	flush()
	return lines
}

// hostLines wraps text into the width of a modal, one styled block of lines.
func hostLines(style lipgloss.Style, lead, text string) []string {
	var out []string
	for i, line := range wrapWords(text, hostWrap-len([]rune(lead))) {
		prefix := lead
		if i > 0 {
			prefix = strings.Repeat(" ", len([]rune(lead)))
		}
		out = append(out, "  "+style.Render(prefix+line))
	}
	return out
}

// planDetails are the exact commands of a plan, dimmed and wrapped.
func planDetails(ui *uiContext, commands []string) []string {
	var lines []string
	for _, c := range commands {
		lines = append(lines, hostLines(ui.st(kittui.StyleHint), "$ ", c)...)
	}
	return []string{"\n" + strings.Join(lines, "\n")}
}

// planQuestion is the confirmation of a plan: the sentence, the commands, and the answers.
func planQuestion(ui *uiContext, sentence string, commands []string, notReversible string, verb textID) question {
	q := question{title: sentence, details: planDetails(ui, commands), keys: keyLine(answer(verb, actYes),
		answer(tHintCancel, actNo, actBack))}
	if notReversible != "" {
		q.consequence, q.warn, q.danger = ui.tr(tHostIrreversible, notReversible), true, true
	} else {
		q.consequence = ui.tr(tHostReversible)
	}
	return q
}

// hostAsk is one change the person asked for. Secret is the standard input of an action that wants one; it is
// overwritten when the run is over or the question is dropped. Mask are texts inside it that no message may
// show. Power names what the change allows, when that is practically everything on the host.
type hostAsk struct {
	id     string
	args   []string
	secret []byte
	mask   [][]byte
	power  bool
	// risk is the sentence that makes the confirmation red: the change touches root, a system account or a
	// system group.
	risk string
	// final replaces the consequence of a change that cannot be undone with one that names exactly what goes.
	final string
}

// hostPlanned is a change the registry accepted, ready to be confirmed.
type hostPlanned struct {
	ask      hostAsk
	plan     hostchange.Plan
	commands []string
}

// planHost describes the change; the error is the reason it is refused, as a message. Nothing is run.
func (m *Model) planHost(a hostAsk) (hostPlanned, string) {
	reg := m.hostRegistry()
	def, ok := reg.Lookup(a.id)
	switch {
	case !ok:
		return hostPlanned{}, m.tr(tHostUnknownAction, a.id)
	case def.WantsSecret && len(a.secret) == 0:
		return hostPlanned{}, m.tr(tHostSecretUnsupported)
	}
	plan, err := reg.Describe(a.id, a.args, a.secret)
	if err != nil {
		return hostPlanned{}, m.diffText(err.Error())
	}
	commands := make([]string, len(plan.Commands))
	for i, c := range plan.Commands {
		commands[i] = c.Display()
	}
	return hostPlanned{ask: a, plan: plan, commands: commands}, ""
}

// confirmHost shows the confirmation of a planned change; y runs it.
func (m *Model) confirmHost(p hostPlanned) {
	a := p.ask
	verb := tHintRunChange
	if a.id == hostchange.ActionUserDelete || a.id == hostchange.ActionGroupDelete {
		verb = tHintDelete
	}
	q := planQuestion(m.ui, p.plan.Sentence, p.commands, p.plan.NotReversible, verb)
	if a.power {
		q.warn = true
		q.consequence = m.tr(tHostPowerWarn) + " " + q.consequence
	}
	if a.id == hostchange.ActionUserCreateService {
		q.consequence += " " + m.tr(tHostServiceKeepFiles)
	}
	if a.id == hostchange.ActionUserLock {
		q.consequence += " " + m.tr(tHostLockStays)
	}
	if a.id == hostchange.ActionUserDelete {
		q.warn, q.danger = true, true
		if p.plan.NotReversible != "" && a.final != "" {
			q.consequence = a.final
		} else if p.plan.NotReversible == "" {
			q.consequence = m.tr(tHostDeleteKeeps) + " " + q.consequence
		}
	}
	if a.risk != "" {
		q.warn, q.danger = true, true
		q.consequence = a.risk + " " + q.consequence
	}
	argv := append([]string{"apply", "--confirm", "--output", "json", "--", a.id}, a.args...)
	m.askConfirm(q, func() tea.Cmd {
		return m.runHostChange(argv, p.plan.Sentence, a.secret, a.mask)
	})
	m.confirm.cancel = func() { zeroAll(a.secret, a.mask) }
}

// askHostChange asks for the action id with args and, on y, runs it as root. Nothing is run or changed before
// the answer; a refusal of the arguments or of the action comes back as a message. An action that wants a
// secret input is asked through confirmHost by the form that collects it.
func (m *Model) askHostChange(id string, args []string) tea.Cmd {
	p, refusal := m.planHost(hostAsk{id: id, args: args})
	if refusal != "" {
		m.hostNotice(question{title: m.tr(tHostChangeRefused), consequence: refusal, warn: true, keys: closeKeys()})
		return nil
	}
	m.confirmHost(p)
	return nil
}

// runHostChange runs a host apply or undo and shows what it reported. The page shown is read again afterwards:
// the host may have changed even when the run failed and was restored.
func (m *Model) runHostChange(argv []string, sentence string, secret []byte, mask [][]byte) tea.Cmd {
	req := HostRequest{Args: argv, Secret: secret}
	run := func(after func(out []byte, err error) tea.Cmd) tea.Cmd { return m.runHost(req, after) }
	if len(secret) > 0 {
		run = func(after func(out []byte, err error) tea.Cmd) tea.Cmd { return m.runHostSecret(req, mask, after) }
	}
	return run(func(out []byte, err error) tea.Cmd {
		report, ok := parseReport(out)
		if !ok {
			kind, exit := classifyRun(out, err)
			m.hostFailNotice(kind, exit, err, true)
			return nil
		}
		m.hostResult(report, sentence)
		return m.afterChange(report)
	})
}

// hostResult shows the report of a change in a panel.
func (m *Model) hostResult(r hostReport, asked string) {
	sentence := r.Sentence
	if sentence == "" {
		sentence = asked
	}
	q := question{keys: closeKeys()}
	switch r.Result {
	case hostchange.ResultApplied:
		q.title = m.tr(tHostDone, m.diffText(sentence))
		q.consequence, q.calm = m.tr(tHostDoneUndoable), true
		if !r.Reversible {
			q.consequence = m.tr(tHostDoneFinal)
		}
	case hostchange.ResultRefused:
		q.title, q.warn = m.tr(tHostRefused, m.diffText(r.Error)), true
		q.consequence = m.tr(tHostRefusedNote)
		r.Error = ""
	case hostchange.ResultRestored:
		q.title, q.warn, q.danger = m.tr(tHostFailedRestored, m.diffText(sentence)), true, true
		q.consequence = m.tr(tHostRestoredNote)
	default:
		q.title, q.warn, q.danger = m.tr(tHostFailed, m.diffText(sentence)), true, true
		q.consequence = m.tr(tHostFailedNote)
	}
	var details []string
	if r.Error != "" {
		details = append(details, hostLines(m.ui.st(kittui.StyleFail), "", m.diffText(r.Error))...)
	}
	switch r.Check {
	case hostchange.CheckPassed:
		details = append(details, hostLines(m.ui.st(kittui.StyleHint), "", m.tr(tHostCheckPassed))...)
	case hostchange.CheckFailed:
		details = append(details, hostLines(m.ui.st(kittui.StyleWarn), "", m.tr(tHostCheckFailed, m.diffText(r.CheckError)))...)
	}
	if r.Warning != "" {
		details = append(details, hostLines(m.ui.st(kittui.StyleWarn), "", m.tr(tHostAuditWarning, m.diffText(r.Warning)))...)
	}
	if len(details) > 0 {
		q.details = []string{"\n" + strings.Join(details, "\n")}
	}
	m.hostNotice(q)
}

// afterChange reloads the page shown so it matches the host. The journal is not read again on its own: that
// would ask for the password a second time, so its list is marked as out of date.
func (m *Model) afterChange(r hostReport) tea.Cmd {
	if r.Result == hostchange.ResultRefused {
		return nil
	}
	m.hst.chg.stale = true
	if m.hst.sub == hostChanges {
		return nil
	}
	return m.hostCmd()
}

// setHostTerminal names the real terminal the commands that take the screen over run on.
func (m *Model) setHostTerminal(in, out *os.File) { m.hostIn, m.hostOut = in, out }
