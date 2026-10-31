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
)

// Changes to the host run as root through sudo: the interface builds the confirmation from the action's plan,
// hands the terminal to `sudo sysmin host apply …` and reads the report that command prints. sudo asks for the
// password on the terminal; the interface never sees it.

// HostRequest is one call of the sudo hook: the arguments after "sysmin host".
type HostRequest struct {
	Args []string
	// Secret is the extension point for an action that wants a secret input. Standard input belongs to sudo's
	// password prompt, so the secret needs a channel of its own (an inherited extra file descriptor, or a
	// sudo-validated two-step); until one exists the hook must refuse a non-empty Secret and askHostChange
	// refuses an action that wants one before it asks anything.
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
// error of the run.
func (m *Model) runHost(req HostRequest, after func(out []byte, err error) tea.Cmd) tea.Cmd {
	var cmd *exec.Cmd
	err := errors.New("no way to run commands as root")
	if m.hostRun != nil {
		cmd, err = m.hostRun(req)
	}
	if err != nil {
		return func() tea.Msg { return hostRanMsg{after: after, err: err} }
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if cmd.Stdin == nil && m.hostIn != nil {
		cmd.Stdin = m.hostIn
	}
	if cmd.Stderr == nil && m.hostOut != nil {
		cmd.Stderr = m.hostOut
	}
	exec := m.hostExec
	if exec == nil {
		exec = tea.ExecProcess
	}
	return exec(cmd, func(err error) tea.Msg { return hostRanMsg{after: after, out: out.Bytes(), err: err} })
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
	title := tr(tHostReadFailed)
	if change {
		title = tr(tHostChangeFailed)
	}
	var text string
	switch kind {
	case failNoSudo:
		text = tr(tHostErrNoSudo)
		if err != nil && !errors.Is(err, ErrNoSudo) {
			text = tr(tHostErrStart, err.Error())
		}
	case failDenied:
		text = tr(tHostErrDenied)
	case failAborted:
		text = tr(tHostErrAbortedRead)
		if change {
			text = tr(tHostErrAbortedChange)
		}
	case failExit:
		text = tr(tHostErrExitRead, exit)
		if change {
			text = tr(tHostErrExitChange, exit)
		}
	default:
		text = tr(tHostErrOutput)
	}
	m.hostNotice(question{title: title, consequence: text, warn: true, danger: true, keys: "{back} close"})
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
func planDetails(commands []string) []string {
	var lines []string
	for _, c := range commands {
		lines = append(lines, hostLines(styles.hint, "$ ", c)...)
	}
	return []string{"\n" + strings.Join(lines, "\n")}
}

// planQuestion is the confirmation of a plan: the sentence, the commands, and the answers.
func planQuestion(sentence string, commands []string, notReversible, verb string) question {
	q := question{title: sentence, details: planDetails(commands), keys: "y " + verb + " · n/{back} cancel"}
	if notReversible != "" {
		q.consequence, q.warn, q.danger = tr(tHostIrreversible, notReversible), true, true
	} else {
		q.consequence = tr(tHostReversible)
	}
	return q
}

// askHostChange asks for the action id with args and, on y, runs it as root. Nothing is run or changed before
// the answer; a refusal of the arguments or of the action comes back as a message.
func (m *Model) askHostChange(id string, args []string) tea.Cmd {
	reg := m.hostRegistry()
	def, ok := reg.Lookup(id)
	switch {
	case !ok:
		m.hostNotice(question{title: tr(tHostChangeRefused), consequence: tr(tHostUnknownAction, id), warn: true, keys: "{back} close"})
		return nil
	case def.WantsSecret:
		m.hostNotice(question{title: tr(tHostChangeRefused), consequence: tr(tHostSecretUnsupported), warn: true, keys: "{back} close"})
		return nil
	}
	plan, err := reg.Describe(id, args, nil)
	if err != nil {
		m.hostNotice(question{title: tr(tHostChangeRefused), consequence: m.diffText(err.Error()), warn: true, keys: "{back} close"})
		return nil
	}
	commands := make([]string, len(plan.Commands))
	for i, c := range plan.Commands {
		commands[i] = c.Display()
	}
	argv := append([]string{"apply", "--yes", "--output", "json", "--", id}, args...)
	m.askConfirm(planQuestion(plan.Sentence, commands, plan.NotReversible, "apply"), func() tea.Cmd {
		return m.runHostChange(argv, plan.Sentence)
	})
	return nil
}

// runHostChange runs a host apply or undo and shows what it reported. The page shown is read again afterwards:
// the host may have changed even when the run failed and was restored.
func (m *Model) runHostChange(argv []string, sentence string) tea.Cmd {
	return m.runHost(HostRequest{Args: argv}, func(out []byte, err error) tea.Cmd {
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
	q := question{keys: "{back} close"}
	switch r.Result {
	case hostchange.ResultApplied:
		q.title = tr(tHostDone, m.diffText(sentence))
		q.consequence, q.calm = tr(tHostDoneUndoable), true
		if !r.Reversible {
			q.consequence = tr(tHostDoneFinal)
		}
	case hostchange.ResultRestored:
		q.title, q.warn, q.danger = tr(tHostFailedRestored, m.diffText(sentence)), true, true
		q.consequence = tr(tHostRestoredNote)
	default:
		q.title, q.warn, q.danger = tr(tHostFailed, m.diffText(sentence)), true, true
		q.consequence = tr(tHostFailedNote)
	}
	var details []string
	if r.Error != "" {
		details = append(details, hostLines(styles.fail, "", m.diffText(r.Error))...)
	}
	switch r.Check {
	case hostchange.CheckPassed:
		details = append(details, hostLines(styles.hint, "", tr(tHostCheckPassed))...)
	case hostchange.CheckFailed:
		details = append(details, hostLines(styles.warning, "", tr(tHostCheckFailed, m.diffText(r.CheckError)))...)
	}
	if r.Warning != "" {
		details = append(details, hostLines(styles.warning, "", tr(tHostAuditWarning, m.diffText(r.Warning)))...)
	}
	if len(details) > 0 {
		q.details = []string{"\n" + strings.Join(details, "\n")}
	}
	m.hostNotice(q)
}

// afterChange reloads the page shown so it matches the host. The journal is not read again on its own: that
// would ask for the password a second time, so its list is marked as out of date.
func (m *Model) afterChange(r hostReport) tea.Cmd {
	m.hst.chg.stale = true
	if m.hst.sub == hostChanges {
		return nil
	}
	return m.hostCmd()
}

// setHostTerminal names the real terminal the commands that take the screen over run on.
func (m *Model) setHostTerminal(in, out *os.File) { m.hostIn, m.hostOut = in, out }
