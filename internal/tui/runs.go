package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The Runs page of Activity: the runs of one project, newest first, under a line with the project's deploy
// state. Everything is read when the page opens, when the project changes and on the reload key, never on a
// timer. The log of a run opens as a text of its own; it is drawn from the service's answer cleaned again
// here, and the copy key hands over only the ID of the run, never a line of a log.

// runsState is the Runs page.
type runsState struct {
	page     dataPage
	project  string
	projects []string
	status   control.DeployStatusResult
	// order lists the run IDs shown, newest first; items holds the runs.
	order   []string
	items   map[string]control.DeployRun
	err     string
	have    bool
	loading bool
	asked   bool
	gen     int
	log     runLogView
}

// runLogView is the open log of one run.
type runLogView struct {
	open, loading bool
	gen           int
	project, id   string
	text          string
	truncated     bool
	err           string
	scroll        textScroll
}

type runsMsg struct {
	gen      int
	project  string
	projects []string
	status   control.DeployStatusResult
	history  control.DeployHistoryResult
	err      error
}

type runLogMsg struct {
	gen int
	res control.DeployLogResult
	err error
}

// runsCmd reads the projects, the deploy state of project and its runs. A project the list does not hold, and
// the empty name, mean the first project of the list.
func (m *Model) runsCmd(project string) tea.Cmd {
	if m.backend == nil || !m.serviceUp() {
		return nil
	}
	r := &m.act.runs
	r.gen++
	r.loading, r.asked = true, true
	gen, b := r.gen, m.backend
	return func() tea.Msg {
		msg := runsMsg{gen: gen}
		list, err := b.Projects()
		if err != nil {
			msg.err = err
			return msg
		}
		for _, p := range list {
			msg.projects = append(msg.projects, p.Name)
		}
		slices.Sort(msg.projects)
		if !slices.Contains(msg.projects, project) {
			project = ""
			if len(msg.projects) > 0 {
				project = msg.projects[0]
			}
		}
		msg.project = project
		if project == "" {
			return msg
		}
		if msg.status, msg.err = b.DeployStatus(project); msg.err != nil {
			return msg
		}
		msg.history, msg.err = b.DeployHistory(project)
		return msg
	}
}

func (m *Model) takeRuns(msg runsMsg) {
	r := &m.act.runs
	if msg.gen != r.gen {
		return
	}
	r.loading = false
	r.projects, r.project = msg.projects, msg.project
	r.err, r.have = "", msg.err == nil
	r.order, r.items, r.status = nil, map[string]control.DeployRun{}, control.DeployStatusResult{}
	if msg.err != nil {
		r.err = errorText(msg.err)
	} else {
		r.status = msg.status
		for _, run := range slices.Backward(msg.history.Runs) {
			if _, dup := r.items[run.ID]; dup {
				continue
			}
			r.items[run.ID] = run
			r.order = append(r.order, run.ID)
		}
	}
	r.page.list.cursor, r.page.list.offset = 0, 0
	r.rebuild(m)
}

// runMark is the symbol that goes with the result of a run; the word always stands beside it.
func runMark(result string) string {
	if mark, ok := runMarks[result]; ok {
		return mark
	}
	return "?"
}

var runMarks = map[string]string{"succeeded": "✓", "failed": "✗", "rolled-back": "↩", "rollback-failed": warnMark}

func runChanges(changes []control.DeployChange) string {
	var parts []string
	for _, c := range changes {
		name := c.Name
		if name == "" {
			name = c.Kind
		}
		parts = append(parts, fmt.Sprintf("%s %s→%s", name, c.From, c.To))
	}
	return strings.Join(parts, ", ")
}

func (r *runsState) rebuild(m *Model) {
	rows := map[string][]string{}
	for _, id := range r.order {
		run := r.items[id]
		rows[id] = []string{run.Started.Local().Format("2006-01-02 15:04:05"), m.diffText(run.Action),
			runMark(run.Result) + " " + m.diffText(run.Result), durationText(run.DurationMS),
			orDash(m.diffText(runChanges(run.Changes))), orDash(m.diffText(run.FailedStep))}
	}
	r.page.cols = []column{
		{title: tr(tColTime)},
		{title: tr(tColAction)},
		{title: tr(tColResult), tone: func(cell string) lipgloss.Style {
			if strings.Contains(cell, "rollback-failed") {
				return styles.fail
			}
			if strings.Contains(cell, "succeeded") {
				return styles.ok
			}
			return styles.warning
		}},
		{title: tr(tColDuration), right: true, optional: true},
		{title: tr(tColChanges), flex: true},
		{title: tr(tColStep), optional: true},
	}
	r.page.setRows(r.order, rows)
}

// runLines are the lines of one tab of a run. Every value of the service is made safe first.
func (m *Model) runLines(id string, tab, width int) []string {
	run, ok := m.act.runs.items[id]
	if !ok {
		return nil
	}
	result := runMark(run.Result) + " " + m.diffText(run.Result)
	if run.ErrorCode != "" {
		result += " (" + m.diffText(run.ErrorCode) + ")"
	}
	switch tab {
	case tabOverview:
		pairs := [][2]string{
			{tr(tColTime), run.Started.Local().Format("2006-01-02 15:04:05")},
			{tr(tColAction), m.diffText(run.Action)},
			{tr(tColResult), result},
			{tr(tColDuration), durationText(run.DurationMS)},
		}
		for _, c := range run.Changes {
			name := c.Name
			if name == "" {
				name = c.Kind
			}
			pairs = append(pairs, [2]string{tr(tColChanges), m.diffText(fmt.Sprintf("%s %s → %s", name, c.From, c.To))})
		}
		return factLines(pairs, width)
	case tabDetails:
		return factLines([][2]string{
			{tr(tLblID), m.diffText(run.ID)},
			{tr(tColStep), orDash(m.diffText(run.FailedStep))},
			{tr(tLblError), orDash(m.diffText(run.Error))},
			{tr(tLblBackup), orDash(m.diffText(run.Backup))},
		}, width)
	}
	lines := []string{"id: " + m.diffText(run.ID), "started: " + run.Started.UTC().Format("2006-01-02T15:04:05Z"),
		"action: " + m.diffText(run.Action), "result: " + m.diffText(run.Result), "error_code: " + m.diffText(run.ErrorCode),
		fmt.Sprintf("duration_ms: %d", run.DurationMS), "failed_step: " + m.diffText(run.FailedStep),
		"error: " + m.diffText(run.Error), "backup: " + m.diffText(run.Backup)}
	for _, c := range run.Changes {
		lines = append(lines, "change: "+m.diffText(fmt.Sprintf("%s %s %s -> %s", c.Kind, c.Name, c.From, c.To)))
	}
	return lines
}

func (m *Model) runsSpec() dataSpec {
	r := &m.act.runs
	empty := tr(tRunsEmpty)
	switch {
	case r.err != "":
		empty = r.err
	case r.loading:
		empty = tr(tAccLoading)
	case r.project == "" && r.have:
		empty = tr(tRunsNoProject)
	}
	title := actTitle(actRuns)
	if r.project != "" {
		title += " · " + m.diffText(r.project)
	}
	return dataSpec{title: title, empty: empty, lines: m.runLines}
}

// runsHeadLines are the lines above the table: the deploy state of the project, and under it a fault or a
// failed check where there is one. Each state has a mark and a word; colour only repeats them.
func (m *Model) runsHeadLines(width int) []string {
	r := &m.act.runs
	if r.project == "" || r.err != "" || !r.have {
		return []string{""}
	}
	st := r.status
	var parts []string
	add := func(style lipgloss.Style, mark string, id textID) {
		parts = append(parts, style.Render(mark+" "+tr(id)))
	}
	if st.Running {
		add(styles.ok, "▶", tRunsRunning)
	}
	if st.Paused {
		add(styles.warning, "‖", tRunsPaused)
	}
	if st.Pinned {
		add(styles.warning, "◆", tRunsPinned)
	}
	if st.Locked {
		add(styles.warning, "○", tRunsLocked)
	}
	if st.Fault != nil {
		add(styles.fail, warnMark, tRunsFault)
	}
	if len(parts) == 0 {
		add(styles.ok, "●", tRunsIdle)
	}
	lines := []string{clipCells(strings.Join(parts, "  "), width)}
	switch {
	case st.Fault != nil:
		text := m.diffText(st.Fault.FailedStep + " " + st.Fault.Error)
		lines = append(lines, styles.fail.Render(truncateCells("  "+strings.TrimSpace(text), width)))
	case st.CheckError != "":
		lines = append(lines, styles.hint.Render(truncateCells("  "+m.diffText(st.CheckError), width)))
	}
	return lines
}

// runsBody is the Runs page under the strip of Activity's pages, from screen row top on.
func (m *Model) runsBody(top int) string {
	r := &m.act.runs
	if r.log.open {
		return m.runLogBody(top)
	}
	lines := m.runsHeadLines(m.usable(0))
	return strings.Join(lines, "\n") + "\n" + m.dataBody(&r.page, m.runsSpec(), top+len(lines))
}

func (m *Model) runsKeys() []string {
	switch {
	case m.act.runs.log.open:
		return []string{"up/down scroll", "ctrl+r reload", "{back} close"}
	case m.act.runs.page.open:
		return []string{"left/right tab", "up/down scroll", "o log", "{back} close"}
	}
	return []string{"up/down move", "enter details", "o log", "f project", "ctrl+r reload", "/ search"}
}

// runsKey handles the keys of Runs that the table does not.
func (m *Model) runsKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	r := &m.act.runs
	switch {
	case keyIs(key, actLogs):
		id, ok := r.page.current()
		if !ok {
			return nil, false
		}
		m.clearMessages()
		return m.runLogCmd(r.project, id), true
	case keyIs(key, actReload):
		m.clearMessages()
		return tea.Batch(m.statusCmd(), m.runsCmd(r.project)), true
	}
	return nil, false
}

// runLogKey handles a key while a run log is open; it reports false for the keys that leave it to the area.
func (m *Model) runLogKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	l := &m.act.runs.log
	switch {
	case keyIs(key, actBack):
		l.open = false
		l.gen++
	case keyIs(key, actReload):
		return m.runLogCmd(l.project, l.id), true
	case l.scroll.scroll(key, halfPage(m.height)):
	default:
		return nil, false
	}
	return nil, true
}

// runLogCmd opens the log view of a run and reads the log.
func (m *Model) runLogCmd(project, id string) tea.Cmd {
	l := &m.act.runs.log
	l.gen++
	l.open, l.loading = true, true
	l.project, l.id, l.text, l.err, l.truncated = project, id, "", "", false
	l.scroll.reset()
	if m.backend == nil || !m.serviceUp() {
		l.loading, l.err = false, errorText(nil)
		return nil
	}
	gen, b := l.gen, m.backend
	return func() tea.Msg {
		res, err := b.DeployLog(project, id)
		return runLogMsg{gen, res, err}
	}
}

func (m *Model) takeRunLog(msg runLogMsg) {
	l := &m.act.runs.log
	if msg.gen != l.gen || !l.open {
		return
	}
	l.loading = false
	if msg.err != nil {
		l.err = errorText(msg.err)
		return
	}
	l.text, l.truncated = msg.res.Text, msg.res.Truncated
}

// runLogLines are the lines of the log in width cells: every line cleaned of escape sequences, control
// characters and registered secrets, and wrapped.
func (m *Model) runLogLines(width int) []string {
	l := m.act.runs.log
	var out []string
	if l.truncated {
		out = append(out, styles.hint.Render(truncateCells(tr(tRunsLogCut), width)))
	}
	if l.text == "" {
		return out
	}
	for _, line := range strings.Split(strings.TrimSuffix(l.text, "\n"), "\n") {
		line = m.diffText(line)
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, strings.Split(ansi.Hardwrap(line, max(width, 1), true), "\n")...)
	}
	return out
}

// runLogBody is the log of a run as a screen of its own.
func (m *Model) runLogBody(top int) string {
	l := &m.act.runs.log
	width := m.usable(0)
	avail := max(m.height-(top-m.workspaceTop())-strings.Count(m.notes(), "\n"), 3)
	inner := max(width-4, 1)
	room := max(avail-1-2, 1)
	var content []string
	switch {
	case l.loading:
		content = []string{styles.hint.Render(tr(tAccLoading))}
	case l.err != "":
		content = []string{styles.hint.Render(truncateCells(l.err, inner))}
	default:
		content = m.runLogLines(inner)
		if len(content) == 0 || (l.truncated && len(content) == 1) {
			content = append(content, styles.hint.Render(tr(tRunsLogEmpty)))
		}
	}
	rows := l.scroll.window(content, room-2, false)
	note := l.scroll.position(len(content), room-2)
	title := tr(tRunsLogTitle) + " › " + truncateCells(m.diffText(l.project+" "+l.id), max(width-lipgloss.Width(tr(tRunsLogTitle))-4, 4))
	return titleLine(title, note, width) + "\n" + strings.Join(panel("", rows, inner, room, panelOpts{focused: true}), "\n")
}
