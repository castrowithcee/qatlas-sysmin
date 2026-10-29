package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// Access lists the open approvals. A person approves what the detail showed: the fingerprint of the shown
// state goes with the approval, so a project that changed meanwhile is refused and shown again.

// accessState is the Access page: the list as the service reported it and the detail of the rows looked at.
type accessState struct {
	page  dataPage
	items map[string]control.ApprovalSummary
	err   string
	// have says the list was read successfully.
	have   bool
	detail map[string]*accDetail
	// discardAfter asks the discard-all question as soon as the list arrived; the command line sets it.
	discardAfter bool
}

// accDetail is the detail of one approval as it was asked for.
type accDetail struct {
	d       control.ApprovalDetail
	loading bool
	err     string
}

type accessMsg struct {
	list []control.ApprovalSummary
	err  error
}

type accDetailMsg struct {
	project string
	d       control.ApprovalDetail
	err     error
}

// kindTexts are the kinds of an approval the service reports.
var kindTexts = map[string]textID{"new": tKindNew, "changed": tKindChanged, "deleted": tKindDeleted, "credential": tKindCredential}

func kindText(kind string) string {
	if id, ok := kindTexts[kind]; ok {
		return tr(id)
	}
	return kind
}

// accessFlags names what needs a second look in an approval, as a mark and a word.
func accessFlags(a control.ApprovalSummary) string {
	var flags []string
	if a.FreeCommands {
		flags = append(flags, tr(tFlagFree))
	}
	if a.Unverified {
		flags = append(flags, tr(tFlagUnverified))
	}
	return strings.Join(flags, ", ")
}

func accessColumns() []column {
	return []column{
		{title: tr(tColProject)},
		{title: tr(tColKind), shown: kindText},
		{title: tr(tColChanges), right: true},
		{title: tr(tColNotes), flex: true, tone: func(cell string) lipgloss.Style { return styles.warning }},
	}
}

// accessCmd reads the open approvals.
func (m *Model) accessCmd() tea.Cmd {
	if m.backend == nil || !m.serviceUp() {
		return nil
	}
	b := m.backend
	return func() tea.Msg {
		list, err := b.Approvals()
		return accessMsg{list, err}
	}
}

// takeAccess shows the list the service reported and drops the details that no longer match their approval.
func (m *Model) takeAccess(msg accessMsg) {
	a := &m.acc
	a.err, a.have = "", msg.err == nil
	m.ovr.approvals, m.ovr.haveApprovals = msg.list, msg.err == nil
	if msg.err != nil {
		a.err = errorText(msg.err)
		msg.list = nil
	}
	a.items = map[string]control.ApprovalSummary{}
	rows := map[string][]string{}
	var names []string
	for _, s := range msg.list {
		a.items[s.Project] = s
		names = append(names, s.Project)
		rows[s.Project] = []string{s.Project, s.Kind, strconv.Itoa(s.Changes), accessFlags(s)}
		if d := a.detail[s.Project]; d != nil && !d.loading && d.d.Fingerprint != s.Fingerprint {
			delete(a.detail, s.Project)
		}
	}
	for project := range a.detail {
		if _, ok := a.items[project]; !ok {
			delete(a.detail, project)
		}
	}
	a.page.cols = accessColumns()
	a.page.setRows(names, rows)
	if a.discardAfter {
		a.discardAfter = false
		m.discardAll()
	}
}

// accessWant asks for the detail of the marked approval where it is on the screen and not there yet.
func (m *Model) accessWant() tea.Cmd {
	name, ok := m.acc.page.current()
	if !ok || !m.detailSeen() || m.backend == nil {
		return nil
	}
	if m.acc.detail == nil {
		m.acc.detail = map[string]*accDetail{}
	}
	if m.acc.detail[name] != nil {
		return nil
	}
	m.acc.detail[name] = &accDetail{loading: true}
	b := m.backend
	return func() tea.Msg {
		d, err := b.Approval(name)
		return accDetailMsg{name, d, err}
	}
}

func (m *Model) takeAccDetail(msg accDetailMsg) {
	d := m.acc.detail[msg.project]
	if d == nil {
		return
	}
	d.loading = false
	if msg.err != nil {
		d.err = errorText(msg.err)
		return
	}
	d.d = msg.d
}

// accessSpec is what the page draws.
func (m *Model) accessSpec() dataSpec {
	empty := tr(tAccEmpty)
	if m.acc.err != "" {
		empty = m.acc.err
	}
	if m.vaultState() == control.VaultLocked {
		empty = tr(tErrLocked)
	}
	return dataSpec{title: tr(tAccTitle), empty: empty, lines: m.accessLines}
}

// accessBody is the Access page under the tab line.
func (m *Model) accessBody() string {
	return m.dataBody(&m.acc.page, m.accessSpec(), m.workspaceTop()+2)
}

// accessLines are the lines of one tab of an approval.
func (m *Model) accessLines(name string, tab, width int) []string {
	item, ok := m.acc.items[name]
	if !ok {
		return nil
	}
	det := m.acc.detail[name]
	switch tab {
	case tabOverview:
		pairs := [][2]string{
			{tr(tColProject), m.diffText(item.Project)},
			{tr(tColKind), kindText(item.Kind)},
			{tr(tColChanges), strconv.Itoa(item.Changes)},
			{tr(tLblFingerprint), shortFingerprint(item.Fingerprint)},
		}
		lines := factLines(pairs, width)
		if item.FreeCommands {
			lines = append(lines, "", styles.warning.Render(tr(tFlagFree)))
			lines = append(lines, wrapIndented(styles.hint, "  ", tr(tInfoAccFree), width)...)
		}
		if item.Unverified {
			lines = append(lines, "", styles.warning.Render(tr(tFlagUnverified)))
			lines = append(lines, wrapIndented(styles.hint, "  ", tr(tAccUnverified), width)...)
		}
		return lines
	case tabDetails:
		if note := detailNote(det); note != "" {
			return []string{styles.hint.Render(note)}
		}
		var lines []string
		if item.Unverified {
			lines = append(lines, wrapIndented(styles.warning, warnMark+" ", tr(tAccUnverified), width)...)
		}
		if len(det.d.Details) == 0 {
			return append(lines, styles.hint.Render(tr(tAccNoChanges)))
		}
		for _, c := range det.d.Details {
			path := m.diffText(c.Path)
			if c.Free {
				lines = append(lines, styles.warning.Render(truncateCells(warnMark+" "+path+"  ("+tr(tFlagFreeShort)+")", width)))
			} else {
				lines = append(lines, styles.title.Render(truncateCells(path, width)))
			}
			lines = append(lines, m.diffSide("- ", c.Old, width)...)
			lines = append(lines, m.diffSide("+ ", c.New, width)...)
		}
		return lines
	}
	if note := detailNote(det); note != "" {
		return []string{styles.hint.Render(note)}
	}
	return m.accessRaw(item, det.d)
}

// diffSide draws one side of a change: its sign and the value, wrapped; an absent side reads as none.
func (m *Model) diffSide(sign, value string, width int) []string {
	if value == "" {
		return []string{styles.hint.Render("  " + sign + tr(tAccNone))}
	}
	return wrapIndented(lipgloss.NewStyle(), "  "+sign, m.diffText(value), width)
}

// detailNote is the text that stands in place of a detail that is not there.
func detailNote(d *accDetail) string {
	switch {
	case d == nil, d.loading:
		return tr(tAccLoading)
	case d.err != "":
		return d.err
	}
	return ""
}

func shortFingerprint(fp string) string {
	if len(fp) > 12 {
		return fp[:12]
	}
	if fp == "" {
		return "-"
	}
	return fp
}

// accessRaw is the approval as plain lines, every value made safe.
func (m *Model) accessRaw(item control.ApprovalSummary, d control.ApprovalDetail) []string {
	lines := []string{
		"project: " + m.diffText(item.Project),
		"kind: " + item.Kind,
		"fingerprint: " + m.diffText(item.Fingerprint),
		"changes: " + strconv.Itoa(item.Changes),
		"free_commands: " + strconv.FormatBool(item.FreeCommands),
		"unverified: " + strconv.FormatBool(item.Unverified),
		"details:",
	}
	for _, c := range d.Details {
		lines = append(lines, "  - path: "+m.diffText(c.Path), "    old: "+m.diffText(c.Old), "    new: "+m.diffText(c.New),
			"    free: "+strconv.FormatBool(c.Free))
	}
	return lines
}

// accessKeys is the key line of Access.
func (m *Model) accessKeys() []string {
	if m.acc.page.open {
		return []string{"left/right tab", "up/down scroll", "{save} approve", "d discard", "{back} close"}
	}
	return []string{"up/down move", "enter details", "{save} approve", "d discard", "a approve all", "/ filter"}
}

// accessKey handles the letters and the save key of Access.
func (m *Model) accessKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case keyIs(key, actSave):
		return m.accessApprove(), true
	case keyIs(key, actDelete):
		return m.accessDiscard(), true
	case keyIs(key, actApproveAll):
		return m.accessApproveAll(), true
	}
	return nil, false
}

// reloadAccess reads the list again after an action; the overview counter follows it.
func (m *Model) reloadAccess() tea.Cmd { return tea.Batch(m.statusCmd(), m.accessCmd()) }

// accessApprove approves the marked approval as the person saw it. Where the detail is not on the screen yet,
// it is shown first and nothing is approved.
func (m *Model) accessApprove() tea.Cmd {
	m.clearMessages()
	name, ok := m.acc.page.current()
	if !ok {
		return nil
	}
	det := m.acc.detail[name]
	if !m.detailSeen() {
		m.acc.page.open, m.acc.page.scroll = true, 0
		m.status = tr(tAccReview)
		return m.accessWant()
	}
	if det == nil || det.loading || det.err != "" {
		m.status = tr(tAccReview)
		return m.accessWant()
	}
	project, fingerprint := name, det.d.Fingerprint
	return m.requireAdmin(change{
		call: func(b Backend) error { return b.Approve(project, fingerprint) },
		done: func(m *Model, err error) tea.Cmd {
			switch {
			case err == nil:
				m.status = tr(tAccApproved, m.diffText(project))
			case isCode(err, control.CodeChanged):
				delete(m.acc.detail, project)
			}
			return tea.Batch(m.reloadAccess(), m.dataWant())
		},
	})
}

// accessDiscard asks, then discards the marked approval.
func (m *Model) accessDiscard() tea.Cmd {
	name, ok := m.acc.page.current()
	if !ok {
		return nil
	}
	project := name
	m.askConfirm(question{
		title: tr(tAccDiscardTitle, m.diffText(project)), consequence: tr(tAccDiscardConsequence), warn: true, danger: true,
		keys: "y discard · n/{back} cancel",
	}, func() tea.Cmd {
		return m.requireAdmin(change{
			call: func(b Backend) error { return b.Discard(project) },
			done: func(m *Model, err error) tea.Cmd {
				if err == nil {
					m.status = tr(tAccDiscarded, m.diffText(project))
				}
				return m.reloadAccess()
			},
		})
	})
	return nil
}

// allListMax is the most approvals an all-at-once question lists. Beyond it the question is not asked: what is
// approved or discarded in one call is exactly what the person saw, and a longer list does not fit the dialog.
const allListMax = 6

// approvalKey names an approval by what the question shows and the service binds: project, kind, fingerprint.
func approvalKey(a control.ApprovalSummary) string {
	return a.Project + "\x00" + a.Kind + "\x00" + a.Fingerprint
}

// shownApprovals are the approvals the list holds now, as the question will show them.
func (m *Model) shownApprovals() []string {
	var keys []string
	for _, name := range m.acc.page.list.all {
		keys = append(keys, approvalKey(m.acc.items[name]))
	}
	return keys
}

// sameApprovals reports whether the service lists exactly the approvals that were shown, in any order.
func sameApprovals(shown []string, now []control.ApprovalSummary) bool {
	if len(shown) != len(now) {
		return false
	}
	have := map[string]bool{}
	for _, k := range shown {
		have[k] = true
	}
	for _, a := range now {
		if !have[approvalKey(a)] {
			return false
		}
	}
	return true
}

// guardedAll runs the all-at-once call only when the service still lists what the question showed; otherwise
// it answers as changed and calls nothing, because the service binds each approval to its own current state.
func guardedAll(b Backend, shown []string, call func() (control.ApprovalsAllResult, error)) (control.ApprovalsAllResult, error) {
	now, err := b.Approvals()
	if err != nil {
		return control.ApprovalsAllResult{}, err
	}
	if !sameApprovals(shown, now) {
		return control.ApprovalsAllResult{}, &control.Error{Code: control.CodeChanged}
	}
	return call()
}

// approvalLines name the open approvals in a question, a few of them.
func (m *Model) approvalLines() []string {
	var lines []string
	for _, name := range m.acc.page.list.all {
		a := m.acc.items[name]
		line := "    " + m.diffText(a.Project) + "  " + kindText(a.Kind) + "  " + strconv.Itoa(a.Changes)
		if flags := accessFlags(a); flags != "" {
			line += "  " + styles.warning.Render(flags)
		}
		lines = append(lines, line)
	}
	return lines
}

// accessApproveAll asks, then approves every open approval in one call.
func (m *Model) accessApproveAll() tea.Cmd {
	n := len(m.acc.page.list.all)
	m.clearMessages()
	if n == 0 {
		m.status = tr(tAccEmpty)
		return nil
	}
	if n > allListMax {
		m.status = tr(tAccTooMany, n, allListMax)
		return nil
	}
	shown := m.shownApprovals()
	free := false
	for _, a := range m.acc.items {
		free = free || a.FreeCommands
	}
	m.askConfirm(question{
		title: tr(tAccAllTitle, n), consequence: tr(tAccAllConsequence), warn: free,
		details: m.approvalLines(), keys: "y approve all · n/{back} cancel",
	}, func() tea.Cmd {
		var res control.ApprovalsAllResult
		return m.requireAdmin(change{
			call: func(b Backend) (err error) {
				res, err = guardedAll(b, shown, b.ApproveAll)
				return err
			},
			done: func(m *Model, err error) tea.Cmd {
				if err == nil {
					m.allDone(res, tAccApprovedN)
				}
				return m.reloadAccess()
			},
		})
	})
	return nil
}

// discardAll asks, then discards every open approval in one call. It is reached from the command line.
func (m *Model) discardAll() tea.Cmd {
	m.clearMessages()
	if len(m.acc.page.list.all) == 0 {
		m.status = tr(tAccEmpty)
		return nil
	}
	if n := len(m.acc.page.list.all); n > allListMax {
		m.status = tr(tAccTooMany, n, allListMax)
		return nil
	}
	shown := m.shownApprovals()
	m.askConfirm(question{
		title: tr(tAccDiscardAllTitle), consequence: tr(tAccDiscardAllConsequence), warn: true, danger: true,
		details: m.approvalLines(), keys: "y discard all · n/{back} cancel",
	}, func() tea.Cmd {
		var res control.ApprovalsAllResult
		return m.requireAdmin(change{
			call: func(b Backend) (err error) {
				res, err = guardedAll(b, shown, b.DiscardAll)
				return err
			},
			done: func(m *Model, err error) tea.Cmd {
				if err == nil {
					m.allDone(res, tAccDiscardedN)
				}
				return m.reloadAccess()
			},
		})
	})
	return nil
}

// allDone reports the outcome of an all-at-once call: how many were handled and, for each project that was
// not, the fixed reason.
func (m *Model) allDone(res control.ApprovalsAllResult, done textID) {
	m.status = tr(done, len(res.Done))
	if len(res.Failed) == 0 {
		return
	}
	var parts []string
	for _, f := range res.Failed {
		parts = append(parts, m.diffText(f.Project)+" ("+errorText(&control.Error{Code: f.Code})+")")
	}
	m.fail = tr(tAccFailed, len(res.Failed), strings.Join(parts, "; "))
}
