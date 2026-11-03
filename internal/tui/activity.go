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

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// Activity shows what the service did: the action log. A page is a constant, a state in activityState, a body
// and a key function. The log is read when the page opens, when the filter changes and on the
// reload key, never on a timer, because every request to the service is an entry of this log itself.
const (
	actLog = iota
	actPages
)

// actKey is the English name of an Activity page, the one commands are given by.
func actKey(sub int) string { return [...]string{"log"}[sub] }

func actTitle(ui *uiContext, sub int) string { return ui.tr([...]textID{tSubLog}[sub]) }

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

// logActions() are the actions of the log the filter offers, in the order of the service's own list.
func logActions() []string {
	return []string{
		"config.apply", "config.read",
		"control.request", "control.refuse", "control.status",
		"host.change",
	}
}

// actionID is the key of the readable label of a log action.
func actionID(action string) textID { return textID("action." + action) }

// actionText is the readable label of a log action; an action without one is shown as it is, made safe.
func (m *Model) actionText(action string) string { return m.trOr(actionID(action), m.diffText(action)) }

// logFilter is what narrows the log. An empty action or result and the period all mean no limit.
type logFilter struct {
	period         int
	action, result string
}

func (f logFilter) active() bool {
	return f.period != periodAll || f.action != "" || f.result != ""
}

// params is the request for one page of the log under the filter, at time now.
func (f logFilter) params(now time.Time, after string) control.LogListParams {
	p := control.LogListParams{Action: f.action, Result: f.result, Limit: logPageSize, After: after}
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

func periodText(ui *uiContext, period int) string {
	return ui.tr([...]textID{tPeriodAll, tPeriodHour, tPeriodToday, tPeriod7Days, tPeriod30Days}[period])
}

// activityState is the Activity area.
type activityState struct {
	sub  int
	page dataPage
	// order lists the sequence numbers shown, newest first; items holds their entries.
	order []string
	items map[string]control.LogEntry
	// next continues the listing; loading says a request is out; stalled says the last request for more
	// failed, so reaching the end does not ask again until the log is read anew.
	next    string
	loading bool
	stalled bool
	err     string
	have    bool
	asked   bool
	gen     int
	filter  logFilter
	chain   chainState
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
			a.err, a.have = errorText(m.ui, msg.err), false
			a.order, a.items, a.next = nil, nil, ""
			a.rebuild(m)
			return nil
		}
		a.stalled = true
		m.fail = errorText(m.ui, msg.err)
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
		c.err = errorText(m.ui, msg.err)
		return
	}
	c.res = msg.res
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
		rows[id] = []string{e.Time.Local().Format("2006-01-02 15:04:05"), m.actionText(e.Action),
			m.diffText(e.Result), durationText(e.DurationMS), uid, signed}
	}
	a.page.cols = logColumns(m.ui)
	a.page.setRows(a.order, rows)
}

func durationText(ms int64) string {
	if ms >= 1000 {
		return fmt.Sprintf("%.1f s", float64(ms)/1000)
	}
	return fmt.Sprintf("%d ms", ms)
}

func logColumns(ui *uiContext) []column {
	return []column{
		{title: ui.tr(tColTime)},
		{title: ui.tr(tColAction)},
		{title: ui.tr(tColResult), flex: true, tone: func(cell string) lipgloss.Style {
			if cell == control.LogResultFailure {
				return ui.st(kittui.StyleWarn)
			}
			return ui.st(kittui.StyleText)
		}},
		{title: ui.tr(tColDuration), right: true, optional: true},
		{title: ui.tr(tColUID), right: true, optional: true},
		{title: ui.tr(tColSigned), optional: true},
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
func (a *activityState) titleText(ui *uiContext) string {
	parts := []string{actTitle(ui, a.sub)}
	f := a.filter
	if f.period != periodAll {
		parts = append(parts, periodText(ui, f.period))
	}
	if f.action != "" {
		parts = append(parts, ui.trOr(actionID(f.action), f.action))
	}
	if f.result != "" {
		parts = append(parts, f.result)
	}
	return strings.Join(parts, " · ")
}

func (m *Model) logSpec() dataSpec {
	a := &m.act
	empty := m.tr(tLogEmpty)
	switch {
	case a.err != "":
		empty = a.err
	case a.loading:
		empty = m.tr(tAccLoading)
	case a.filter.active():
		empty = m.tr(tLogEmptyFiltered)
	}
	spec := dataSpec{title: a.titleText(m.ui), empty: empty, lines: m.logLines}
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
	signed := m.tr(tLogUnsigned)
	if e.Signed {
		signed = m.tr(tLogSigned)
	}
	result := m.diffText(e.Result)
	if e.ErrorCode != "" {
		result += " (" + m.diffText(e.ErrorCode) + ")"
	}
	when := e.Time.Local().Format("2006-01-02 15:04:05")
	switch tab {
	case tabOverview:
		return factLines(m.ui, [][2]string{
			{m.tr(tColTime), when},
			{m.tr(tColAction), m.actionText(e.Action)},
			{m.tr(tColResult), result},
		}, width)
	case tabDetails:
		return factLines(m.ui, [][2]string{
			{m.tr(tLblSeq), id},
			{m.tr(tColUID), uid},
			{m.tr(tColDuration), durationText(e.DurationMS)},
			{m.tr(tColSigned), signed},
		}, width)
	}
	return []string{"seq: " + id, "time: " + e.Time.UTC().Format(time.RFC3339), "action: " + m.diffText(e.Action),
		"result: " + m.diffText(e.Result), "error_code: " + m.diffText(e.ErrorCode),
		"client_uid: " + uid, "duration_ms: " + strconv.FormatInt(e.DurationMS, 10), "signed: " + strconv.FormatBool(e.Signed)}
}

// chainLines is the result of the chain check: a dot and the word for an intact chain on one line; for a
// broken one a warning sign and the first problem, and under it the notes, if any. Unreadable
// lines that a marker documents are counted before the signatures, in either case. Colour only repeats
// what the mark and the word say.
func (m *Model) chainLines(width int) []string {
	c := m.act.chain
	switch {
	case c.err != "":
		return []string{m.ui.st(kittui.StyleHint).Render(truncateCells("○ "+m.tr(tLogChainUnknown)+": "+c.err, width))}
	case !c.have:
		return []string{m.ui.st(kittui.StyleHint).Render(truncateCells("○ "+m.tr(tLogChainChecking), width))}
	}
	r := c.res
	// notes are what follows the verdict: documented damage, and old signatures that nobody checks.
	var notes []string
	damaged, unchecked := 0, false
	for _, d := range r.Days {
		damaged += d.Damaged
		unchecked = unchecked || d.Signatures == control.LogSignaturesNotChecked
	}
	if damaged > 0 {
		notes = append(notes, m.tr(tLogDamaged, damaged))
	}
	if unchecked {
		notes = append(notes, m.tr(tLogSigUnchecked))
	}
	signatures := strings.Join(notes, " · ")
	if r.Intact {
		entries := 0
		for _, d := range r.Days {
			entries += d.Entries
		}
		text := m.tr(tLogChainIntact, entries)
		if signatures != "" {
			text += " · " + signatures
		}
		return []string{m.ui.st(kittui.StyleOK).Render("●") + " " + truncateCells(text, max(width-2, 1))}
	}
	text := m.tr(tLogChainBroken)
	if len(r.Problems) > 0 {
		p := r.Problems[0]
		position := m.tr(tLogLine, p.Line)
		if p.Seq != 0 {
			position = m.tr(tLogEntry, p.Seq)
		}
		text += " · " + m.diffText(p.Date) + " " + position + " · " + problemCodeText(m.ui, p.Code)
		if n := len(r.Problems) - 1; n > 0 || r.MoreProblems {
			more := strconv.Itoa(n)
			if r.MoreProblems {
				more += "+"
			}
			text += " · " + m.tr(tLogMoreDays, more)
		}
	}
	lines := []string{m.ui.st(kittui.StyleFail).Render(truncateCells(warnMark+" "+text, width))}
	if signatures != "" {
		lines = append(lines, m.ui.st(kittui.StyleFail).Render(truncateCells("  "+signatures, width)))
	}
	return lines
}

// problemCodeText is a problem word of the chain check in words; an unknown word is shown as it is, made safe.
func problemCodeText(ui *uiContext, code string) string {
	ids := map[string]textID{
		control.LogProblemUnparsable: tLogProbUnparsable, control.LogProblemGenesis: tLogProbGenesis,
		control.LogProblemMissing: tLogProbMissing, control.LogProblemChain: tLogProbChain,
	}
	if id, ok := ids[code]; ok {
		return ui.tr(id)
	}
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return '�'
		}
		return r
	}, code)
}

// activityBody is the page of Activity under the bars of the frame.
func (m *Model) activityBody() string {
	top := m.workspaceTop()
	lines := m.chainLines(m.usable(0))
	return strings.Join(lines, "\n") + "\n" + m.dataBody(&m.act.page, m.logSpec(), top+len(lines))
}

func (m *Model) activityKeys() []string {
	if m.detailFocus(&m.act.page) {
		return []string{"←/→ tab", "↑/↓ scroll", "{back} back"}
	}
	return []string{"↑/↓ move", "enter details", "f filter", "t verify", "ctrl+r reload", "/ search"}
}

// activityKey handles the keys of Activity that the table does not.
func (m *Model) activityKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case m.keyIs(key, actTest):
		m.clearMessages()
		return m.verifyCmd(), true
	case m.keyIs(key, actReload):
		m.clearMessages()
		return tea.Batch(m.statusCmd(), m.activityCmd()), true
	}
	return nil, false
}

// openLogFilter opens the form of the filter.
func (m *Model) openLogFilter() {
	a := &m.act
	f := &formDlg{title: m.tr(tLogFilterTitle), saveText: "{save} apply", submit: submitLogFilter}
	periods := make([]string, periodCount)
	values := make([]string, periodCount)
	for i := range periods {
		periods[i], values[i] = periodText(m.ui, i), strconv.Itoa(i)
	}
	f.rows = []formRow{
		choiceRow("period", m.tr(tColPeriod), values, periods, a.filter.period),
		choiceRow("action", m.tr(tColAction), nil, nil, 0),
		choiceRow("result", m.tr(tColResult), []string{"", control.LogResultSuccess, control.LogResultFailure},
			[]string{m.tr(tFilterAll), control.LogResultSuccess, control.LogResultFailure}, 0),
	}
	setActionChoice(m.ui, f.row("action"), a.filter.action)
	switch a.filter.result {
	case control.LogResultSuccess:
		f.row("result").sel = 1
	case control.LogResultFailure:
		f.row("result").sel = 2
	}
	m.openForm(f)
}

// setActionChoice offers all and the logged actions to the action row, each by its readable label, and selects
// the current one.
func setActionChoice(ui *uiContext, r *formRow, current string) {
	r.values, r.shown = []string{""}, []string{ui.tr(tFilterAll)}
	for _, action := range logActions() {
		r.values, r.shown = append(r.values, action), append(r.shown, ui.trOr(actionID(action), action))
	}
	r.sel = slices.Index(r.values, current)
	if r.sel < 0 {
		r.sel = 0
	}
}

// submitLogFilter takes the choices as the filter and reads the log anew.
func submitLogFilter(m *Model, f *formDlg) tea.Cmd {
	period, _ := strconv.Atoi(f.row("period").value())
	m.act.filter = logFilter{period: period, action: f.row("action").value(),
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
