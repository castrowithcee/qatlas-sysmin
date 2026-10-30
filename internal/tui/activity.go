package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// Activity shows what the service did: the action log here and the runs of the projects in runs.go. A page
// is a constant, a state in activityState, a body and a key function, as in Library. The log is read when the page opens, when the filter changes and on the reload key, never on a
// timer, because every request to the service is an entry of this log itself.
const (
	actLog = iota
	actRuns
	actPages
)

// actKey is the English name of an Activity page, the one commands are given by.
func actKey(sub int) string { return [...]string{"log", "runs"}[sub] }

func actTitle(sub int) string { return tr([...]textID{tSubLog, tSubRuns}[sub]) }

// logPageSize is how many entries one request asks for.
const logPageSize = 100

// The periods of the filter; the zero value is no limit.
const (
	periodAll = iota
	periodHour
	periodToday
	period7Days
	period30Days
	periodCount
)

// logActions are the actions of the log the filter offers, in the order of the service's own list.
var logActions = []string{
	"config.apply", "config.migrate", "config.read",
	"credential.create", "credential.change", "credential.delete", "credential.use",
	"approval.request", "approval.grant", "approval.deny", "approval.list", "approval.show",
	"deploy.plan", "deploy.run", "deploy.rollback",
	"vault.unlock", "vault.lock", "vault.rekey", "vault.create", "vault.status",
	"webhook.receive", "webhook.reject", "notify.send",
	"control.request", "control.refuse", "control.status", "session.open", "session.end",
}

// logFilter is what narrows the log. An empty project, action or result and the period all mean no limit.
type logFilter struct {
	period                  int
	project, action, result string
}

func (f logFilter) active() bool {
	return f.period != periodAll || f.project != "" || f.action != "" || f.result != ""
}

// params is the request for one page of the log under the filter, at time now.
func (f logFilter) params(now time.Time, after string) control.LogListParams {
	p := control.LogListParams{Project: f.project, Action: f.action, Result: f.result, Limit: logPageSize, After: after}
	switch f.period {
	case periodHour:
		p.From = now.Add(-time.Hour).Format(time.RFC3339)
	case periodToday:
		y, mo, d := now.Date()
		p.From = time.Date(y, mo, d, 0, 0, 0, 0, now.Location()).Format(time.RFC3339)
	case period7Days:
		p.From = now.AddDate(0, 0, -7).Format(time.RFC3339)
	case period30Days:
		p.From = now.AddDate(0, 0, -30).Format(time.RFC3339)
	}
	return p
}

func periodText(period int) string {
	return tr([...]textID{tPeriodAll, tPeriodHour, tPeriodToday, tPeriod7Days, tPeriod30Days}[period])
}

// activityState is the Activity area.
type activityState struct {
	sub  int
	runs runsState
	page dataPage
	// order lists the sequence numbers shown, newest first; items holds their entries.
	order []string
	items map[string]control.LogEntry
	// next continues the listing; loading says a request is out; stalled says the last request for more
	// failed, so reaching the end does not ask again until the log is read anew.
	next         string
	loading      bool
	stalled      bool
	err          string
	have         bool
	asked        bool
	gen          int
	filter       logFilter
	chain        chainState
	projects     []string
	haveProjects bool
}

// chainState is the result of the last check of the hash chain.
type chainState struct {
	res      control.LogVerifyResult
	have     bool
	checking bool
	err      string
}

type logMsg struct {
	gen   int
	after string
	res   control.LogListResult
	err   error
}

type verifyMsg struct {
	res control.LogVerifyResult
	err error
}

type projectsMsg struct {
	list []control.ProjectSummary
	err  error
}

// logCmd reads the first page of the log under the filter, or, with after, the page behind it.
func (m *Model) logCmd(after string) tea.Cmd {
	if m.backend == nil || !m.serviceUp() {
		return nil
	}
	a := &m.act
	if after == "" {
		a.gen++
		a.stalled = false
	}
	a.loading, a.asked = true, true
	params, gen, b := a.filter.params(m.timeNow(), after), a.gen, m.backend
	return func() tea.Msg {
		res, err := b.LogEntries(params)
		return logMsg{gen, after, res, err}
	}
}

// verifyCmd checks the hash chain of the log.
func (m *Model) verifyCmd() tea.Cmd {
	if m.backend == nil || !m.serviceUp() {
		return nil
	}
	m.act.chain.checking = true
	b := m.backend
	return func() tea.Msg {
		res, err := b.VerifyLog()
		return verifyMsg{res, err}
	}
}

// activityCmd reads the log and checks the chain: what opening Activity or reloading it does.
func (m *Model) activityCmd() tea.Cmd {
	return tea.Batch(m.logCmd(""), m.verifyCmd())
}

func (m *Model) takeLog(msg logMsg) tea.Cmd {
	a := &m.act
	if msg.gen != a.gen {
		return nil
	}
	a.loading = false
	if msg.err != nil {
		if msg.after == "" {
			a.err, a.have = errorText(msg.err), false
			a.order, a.items, a.next = nil, nil, ""
			a.rebuild(m)
			return nil
		}
		a.stalled = true
		m.fail = errorText(msg.err)
		return nil
	}
	a.err, a.have = "", true
	if msg.after == "" {
		a.order, a.items = nil, map[string]control.LogEntry{}
		a.page.list.cursor, a.page.list.offset = 0, 0
	}
	for _, e := range msg.res.Entries {
		id := strconv.FormatUint(e.Seq, 10)
		if _, dup := a.items[id]; dup {
			continue
		}
		a.items[id] = e
		a.order = append(a.order, id)
	}
	a.next = msg.res.Next
	a.rebuild(m)
	return m.dataWant()
}

func (m *Model) takeVerify(msg verifyMsg) {
	c := &m.act.chain
	c.checking = false
	c.err, c.have = "", msg.err == nil
	if msg.err != nil {
		c.err = errorText(msg.err)
		return
	}
	c.res = msg.res
}

func (m *Model) takeProjects(msg projectsMsg) {
	a := &m.act
	a.haveProjects = msg.err == nil
	if msg.err != nil {
		return
	}
	a.projects = nil
	for _, p := range msg.list {
		a.projects = append(a.projects, p.Name)
	}
	slices.Sort(a.projects)
	if f := m.form; f != nil && f.refresh != nil {
		f.refresh(m, f)
	}
}

// rebuild makes the rows of the table from the entries.
func (a *activityState) rebuild(m *Model) {
	rows := map[string][]string{}
	for _, id := range a.order {
		e := a.items[id]
		uid := "-"
		if e.ClientUID != nil {
			uid = strconv.FormatUint(uint64(*e.ClientUID), 10)
		}
		signed := "-"
		if e.Signed {
			signed = "✓"
		}
		project := m.diffText(e.Project)
		if project == "" {
			project = "-"
		}
		rows[id] = []string{e.Time.Local().Format("2006-01-02 15:04:05"), m.diffText(e.Action), project,
			m.diffText(e.Result), durationText(e.DurationMS), uid, signed}
	}
	a.page.cols = logColumns()
	a.page.setRows(a.order, rows)
}

func durationText(ms int64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.1f s", float64(ms)/1000)
	}
	return fmt.Sprintf("%d ms", ms)
}

func logColumns() []column {
	return []column{
		{title: tr(tColTime)},
		{title: tr(tColAction)},
		{title: tr(tColProject), flex: true},
		{title: tr(tColResult), tone: func(cell string) lipgloss.Style {
			if cell == control.LogResultFailure {
				return styles.warning
			}
			return styles.cell
		}},
		{title: tr(tColDuration), right: true, optional: true},
		{title: tr(tColUID), right: true, optional: true},
		{title: tr(tColSigned), optional: true},
	}
}

// activityWant asks for the next page where the marked entry is the last one shown.
func (m *Model) activityWant() tea.Cmd {
	a := &m.act
	if a.sub != actLog || a.next == "" || a.loading || a.stalled || len(a.page.list.matches) == 0 || a.page.list.cursor < len(a.page.list.matches)-1 {
		return nil
	}
	return m.logCmd(a.next)
}

// titleText is the title of the table: the page and, behind it, every filter that is on.
func (a *activityState) titleText() string {
	parts := []string{actTitle(a.sub)}
	f := a.filter
	if f.period != periodAll {
		parts = append(parts, periodText(f.period))
	}
	for _, v := range []string{f.project, f.action, f.result} {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " · ")
}

func (m *Model) logSpec() dataSpec {
	a := &m.act
	empty := tr(tLogEmpty)
	switch {
	case a.err != "":
		empty = a.err
	case a.loading:
		empty = tr(tAccLoading)
	case a.filter.active():
		empty = tr(tLogEmptyFiltered)
	}
	spec := dataSpec{title: a.titleText(), empty: empty, lines: m.logLines}
	return spec
}

// logLines are the lines of one tab of a log entry.
func (m *Model) logLines(id string, tab, width int) []string {
	e, ok := m.act.items[id]
	if !ok {
		return nil
	}
	uid := "-"
	if e.ClientUID != nil {
		uid = strconv.FormatUint(uint64(*e.ClientUID), 10)
	}
	signed := tr(tLogUnsigned)
	if e.Signed {
		signed = tr(tLogSigned)
	}
	result := m.diffText(e.Result)
	if e.ErrorCode != "" {
		result += " (" + m.diffText(e.ErrorCode) + ")"
	}
	when := e.Time.Local().Format("2006-01-02 15:04:05")
	switch tab {
	case tabOverview:
		return factLines([][2]string{
			{tr(tColTime), when},
			{tr(tColAction), m.diffText(e.Action)},
			{tr(tColProject), orDash(m.diffText(e.Project))},
			{tr(tColResult), result},
		}, width)
	case tabDetails:
		return factLines([][2]string{
			{tr(tLblSeq), id},
			{tr(tColUID), uid},
			{tr(tColDuration), durationText(e.DurationMS)},
			{tr(tColSigned), signed},
		}, width)
	}
	return []string{"seq: " + id, "time: " + e.Time.UTC().Format(time.RFC3339), "action: " + m.diffText(e.Action),
		"project: " + m.diffText(e.Project), "result: " + m.diffText(e.Result), "error_code: " + m.diffText(e.ErrorCode),
		"client_uid: " + uid, "duration_ms: " + strconv.FormatInt(e.DurationMS, 10), "signed: " + strconv.FormatBool(e.Signed)}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// chainLines is the result of the chain check: a dot and the word for an intact chain on one line; for a
// broken one a warning sign and the first problem, and under it the state of the signatures. Unreadable
// lines that a marker documents are counted before the signatures, in either case. Colour only repeats
// what the mark and the word say.
func (m *Model) chainLines(width int) []string {
	c := m.act.chain
	switch {
	case c.err != "":
		return []string{styles.hint.Render(truncateCells("○ "+tr(tLogChainUnknown)+": "+c.err, width))}
	case !c.have:
		return []string{styles.hint.Render(truncateCells("○ "+tr(tLogChainChecking), width))}
	}
	r := c.res
	signatures := tr(tLogSigUnchecked)
	if r.Checked {
		signatures = tr(tLogSigChecked)
	}
	damaged := 0
	for _, d := range r.Days {
		damaged += d.Damaged
	}
	if damaged > 0 {
		signatures = tr(tLogDamaged, damaged) + " · " + signatures
	}
	if r.Intact {
		entries := 0
		for _, d := range r.Days {
			entries += d.Entries
		}
		return []string{styles.ok.Render("●") + " " + truncateCells(tr(tLogChainIntact, entries)+" · "+signatures, max(width-2, 1))}
	}
	text := tr(tLogChainBroken)
	if len(r.Problems) > 0 {
		p := r.Problems[0]
		position := tr(tLogLine, p.Line)
		if p.Seq != 0 {
			position = tr(tLogEntry, p.Seq)
		}
		text += " · " + m.diffText(p.Date) + " " + position + " · " + problemCodeText(p.Code)
		if n := len(r.Problems) - 1; n > 0 || r.MoreProblems {
			more := strconv.Itoa(n)
			if r.MoreProblems {
				more += "+"
			}
			text += " · " + tr(tLogMoreDays, more)
		}
	}
	return []string{styles.fail.Render(truncateCells(warnMark+" "+text, width)),
		styles.fail.Render(truncateCells("  "+signatures, width))}
}

// problemCodeText is a problem word of the chain check in words; an unknown word is shown as it is, made safe.
func problemCodeText(code string) string {
	ids := map[string]textID{
		control.LogProblemUnparsable: tLogProbUnparsable, control.LogProblemGenesis: tLogProbGenesis,
		control.LogProblemMissing: tLogProbMissing, control.LogProblemChain: tLogProbChain, control.LogProblemMAC: tLogProbMAC,
	}
	if id, ok := ids[code]; ok {
		return tr(id)
	}
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return '�'
		}
		return r
	}, code)
}

// activityBody is Activity under the tab line. The pages of Activity get a strip of their names as soon as
// there is more than one.
func (m *Model) activityBody() string {
	top := m.workspaceTop() + 2
	head := ""
	if actPages > 1 {
		words := make([]string, actPages)
		for i := range words {
			words[i] = actTitle(i)
		}
		strip, cells := tabStrip(words, m.act.sub, false)
		for i, c := range cells {
			m.addHit(c[0], top, c[1], top+1, func() tea.Cmd { return m.setActSub(i) })
		}
		head = strip + "\n\n"
		top += 2
	}
	if m.act.sub == actRuns {
		return head + m.runsBody(top)
	}
	lines := m.chainLines(m.usable(0))
	return head + strings.Join(lines, "\n") + "\n" + m.dataBody(&m.act.page, m.logSpec(), top+len(lines))
}

func (m *Model) activityKeys() []string {
	if m.act.sub == actRuns {
		return m.runsKeys()
	}
	if m.act.page.open {
		return []string{"left/right tab", "up/down scroll", "{back} close"}
	}
	return []string{"left/right page", "up/down move", "enter details", "f filter", "t verify", "ctrl+r reload", "/ search"}
}

// activityKey handles the keys of Activity that the table does not.
func (m *Model) activityKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.act.sub == actRuns {
		return m.runsKey(key)
	}
	switch {
	case keyIs(key, actTest):
		m.clearMessages()
		return m.verifyCmd(), true
	case keyIs(key, actReload):
		m.clearMessages()
		return tea.Batch(m.statusCmd(), m.activityCmd()), true
	}
	return nil, false
}

// projectsCmd reads the project names the filter offers.
func (m *Model) projectsCmd() tea.Cmd {
	if m.backend == nil || !m.serviceUp() {
		return nil
	}
	b := m.backend
	return func() tea.Msg {
		list, err := b.Projects()
		return projectsMsg{list, err}
	}
}

// choices are the values of a filter row with "all" in front, the current value kept in even where the list
// no longer holds it.
func allChoices(values []string, current string) (vals, shown []string) {
	vals = append([]string{""}, values...)
	if current != "" && !slices.Contains(values, current) {
		vals = append(vals, current)
	}
	shown = []string{tr(tFilterAll)}
	return vals, append(shown, vals[1:]...)
}

// openLogFilter opens the form of the filter. The names of the projects are read when the form opens; the
// list arrives into it.
func (m *Model) openLogFilter() tea.Cmd {
	a := &m.act
	f := &formDlg{title: tr(tLogFilterTitle), saveText: "{save} apply", submit: submitLogFilter}
	periods := make([]string, periodCount)
	values := make([]string, periodCount)
	for i := range periods {
		periods[i], values[i] = periodText(i), strconv.Itoa(i)
	}
	f.rows = []formRow{
		choiceRow("period", tr(tColPeriod), values, periods, a.filter.period),
		choiceRow("project", tr(tColProject), nil, nil, 0),
		choiceRow("action", tr(tColAction), nil, nil, 0),
		choiceRow("result", tr(tColResult), []string{"", control.LogResultSuccess, control.LogResultFailure},
			[]string{tr(tFilterAll), control.LogResultSuccess, control.LogResultFailure}, 0),
	}
	f.refresh = func(m *Model, f *formDlg) {
		v, s := allChoices(m.act.projects, f.row("project").value())
		f.row("project").setChoices(v, s)
	}
	setChoice(f.row("project"), a.projects, a.filter.project)
	setChoice(f.row("action"), logActions, a.filter.action)
	switch a.filter.result {
	case control.LogResultSuccess:
		f.row("result").sel = 1
	case control.LogResultFailure:
		f.row("result").sel = 2
	}
	m.openForm(f)
	if !a.haveProjects {
		return m.projectsCmd()
	}
	return nil
}

// setChoice offers all and the values to a choice row and selects the current one.
func setChoice(r *formRow, values []string, current string) {
	r.values, r.shown = allChoices(values, current)
	r.sel = slices.Index(r.values, current)
	if r.sel < 0 {
		r.sel = 0
	}
}

// submitLogFilter takes the choices as the filter and reads the log anew.
func submitLogFilter(m *Model, f *formDlg) tea.Cmd {
	period, _ := strconv.Atoi(f.row("period").value())
	m.act.filter = logFilter{period: period, project: f.row("project").value(), action: f.row("action").value(),
		result: f.row("result").value()}
	m.closeForm()
	return m.logCmd("")
}

// setActSub shows page sub of Activity.
func (m *Model) setActSub(sub int) tea.Cmd {
	m.act.sub = (sub + actPages) % actPages
	m.closeDetails()
	m.clearMessages()
	return m.enterPage()
}

// openRunsProject opens the form that picks the project whose runs are shown.
func (m *Model) openRunsProject() tea.Cmd {
	r := &m.act.runs
	if len(r.projects) == 0 {
		return nil
	}
	f := &formDlg{title: tr(tRunsProjectTitle), saveText: "{save} apply", submit: func(m *Model, f *formDlg) tea.Cmd {
		m.closeForm()
		return m.runsCmd(f.row("project").value())
	}}
	row := choiceRow("project", tr(tColProject), r.projects, r.projects, max(slices.Index(r.projects, r.project), 0))
	f.rows = []formRow{row}
	m.openForm(f)
	return nil
}
