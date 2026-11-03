package tui

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// The Changes page lists the latest changes to the host from the journal, which only root can read. It is
// read through `sudo qsysmin host journal` when the person asks (ctrl+r), never on its own, and enter on a
// reversible entry asks to undo it.

// hostChange is one entry of the journal as `host journal` lists it. UndoSentence and UndoCommands are the
// confirmation of its undo; both are empty when no undo can be offered.
type hostChange struct {
	ID           string   `json:"id"`
	Time         string   `json:"time"`
	Action       string   `json:"action"`
	Sentence     string   `json:"sentence"`
	Result       string   `json:"result"`
	Reversible   bool     `json:"reversible"`
	SudoUID      int      `json:"sudo_uid"`
	UndoSentence string   `json:"undo_sentence"`
	UndoCommands []string `json:"undo_commands"`
}

type hostChangesState struct {
	page  dataPage
	all   []hostChange
	items map[string]hostChange
	have  bool
	// stale says the list is older than the last change made from here.
	stale bool
}

// readChanges reads the journal as root. It is the only way the list fills.
func (m *Model) readChanges() tea.Cmd {
	return m.runHost(HostRequest{Args: []string{"journal", "--output", "json"}}, func(out []byte, err error) tea.Cmd {
		var list []hostChange
		if err != nil || json.Unmarshal(bytes.TrimSpace(out), &list) != nil {
			kind, exit := classifyRun(out, err)
			m.hostFailNotice(kind, exit, err, false)
			return nil
		}
		c := &m.hst.chg
		c.all, c.have, c.stale = list, true, false
		m.fillHostChanges()
		return nil
	})
}

func (m *Model) fillHostChanges() {
	c := &m.hst.chg
	c.items = map[string]hostChange{}
	rows := map[string][]string{}
	var ids []string
	for _, e := range c.all {
		c.items[e.ID] = e
		ids = append(ids, e.ID)
		rows[e.ID] = []string{hostTime(e.Time), m.diffText(e.Sentence), e.Result}
	}
	c.page.cols = m.hostChangeColumns()
	c.page.setRows(ids, rows)
}

func hostTime(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func shownResult(v string) string {
	switch v {
	case hostchange.ResultApplied:
		return tr(tHostResApplied)
	case hostchange.ResultFailed:
		return tr(tHostResFailed)
	case hostchange.ResultRestored:
		return tr(tHostResRestored)
	case hostchange.ResultUndone:
		return tr(tHostResUndone)
	}
	return v
}

func (m *Model) hostChangeColumns() []column {
	tone := func(cell string) lipgloss.Style {
		if cell == tr(tHostResFailed) || cell == tr(tHostResRestored) {
			return styles.warning
		}
		return styles.cell
	}
	return []column{
		{title: tr(tColTime)},
		{title: tr(tHostColChange), flex: true},
		{title: tr(tHostColResult), shown: shownResult, tone: tone},
	}
}

func (m *Model) hostChangesSpec() dataSpec {
	empty := tr(tHostChangesHint, "ctrl+r")
	if m.hst.chg.have {
		empty = tr(tHostChangesNone)
	}
	return dataSpec{title: tr(tSubChanges), empty: empty, lines: m.hostChangeLines}
}

func (m *Model) hostChangeLines(id string, tab, width int) []string {
	e, ok := m.hst.chg.items[id]
	if !ok {
		return nil
	}
	undo := tr(tHostNoUndo)
	if e.UndoSentence != "" {
		undo = m.diffText(e.UndoSentence)
	}
	switch tab {
	case tabOverview:
		return factLines([][2]string{
			{tr(tColTime), hostTime(e.Time)},
			{tr(tHostColChange), m.diffText(e.Sentence)},
			{tr(tHostColResult), shownResult(e.Result)},
			{tr(tHostLblUndo), undo},
		}, width)
	case tabDetails:
		if len(e.UndoCommands) == 0 {
			return []string{styles.hint.Render(tr(tHostNoUndo))}
		}
		var lines []string
		for _, c := range e.UndoCommands {
			lines = append(lines, wrapIndented(styles.hint, "$ ", m.diffText(c), width)...)
		}
		return lines
	}
	return []string{
		"id: " + m.diffText(e.ID), "time: " + m.diffText(e.Time), "action: " + m.diffText(e.Action),
		"sentence: " + m.diffText(e.Sentence), "result: " + m.diffText(e.Result),
		"reversible: " + boolText(e.Reversible), "undo: " + undo,
	}
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// changesKey handles the keys of the Changes page that the table does not.
func (m *Model) changesKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case m.keyIs(key, actReload):
		return m.readChanges(), true
	case m.keyIs(key, actEnter):
		return m.askUndo(), true
	}
	return nil, false
}

// askUndo asks to undo the marked entry, with the sentence and the commands of the inverse.
func (m *Model) askUndo() tea.Cmd {
	id, ok := m.hst.chg.page.current()
	if !ok {
		return nil
	}
	e := m.hst.chg.items[id]
	if !e.Reversible || e.UndoSentence == "" {
		m.hostNotice(question{title: tr(tHostUndoRefused), consequence: tr(tHostUndoRefusedNote, m.diffText(e.Sentence)),
			warn: true, keys: "{back} close"})
		return nil
	}
	q := planQuestion(e.UndoSentence, e.UndoCommands, "", "undo")
	q.title = m.diffText(e.UndoSentence)
	q.consequence = tr(tHostUndoNote, m.diffText(e.Sentence))
	argv := []string{"undo", "--yes", "--output", "json", "--", id}
	m.askConfirm(q, func() tea.Cmd {
		return m.runUndo(argv, id, e.UndoSentence)
	})
	return nil
}

func (m *Model) runUndo(argv []string, id, sentence string) tea.Cmd {
	return m.runHost(HostRequest{Args: argv}, func(out []byte, err error) tea.Cmd {
		report, ok := parseReport(out)
		if !ok {
			kind, exit := classifyRun(out, err)
			m.hostFailNotice(kind, exit, err, true)
			return nil
		}
		m.hostResult(report, sentence)
		if report.Result == hostchange.ResultApplied {
			m.markUndone(id)
		}
		return m.afterChange(report)
	})
}

// markUndone takes the undone entry out of the offers, so it is not asked for twice before the list is read
// again.
func (m *Model) markUndone(id string) {
	c := &m.hst.chg
	for i := range c.all {
		if c.all[i].ID == id {
			c.all[i].Reversible, c.all[i].Result = false, hostchange.ResultUndone
		}
	}
	m.fillHostChanges()
}

// rootFields are the fields of a user that only root can read, as `host user` prints them for root.
type rootFields struct {
	Name    string             `json:"name"`
	Locked  *bool              `json:"locked"`
	Expired *bool              `json:"expired"`
	SSHKeys []hostadmin.SSHKey `json:"ssh_keys"`
}

// askReadUser asks to read the locked state and the SSH keys of the marked user as root. Nothing is read
// without the answer.
func (m *Model) askReadUser() tea.Cmd {
	name, ok := m.hst.users.page.current()
	if !ok {
		return nil
	}
	m.askConfirm(question{
		title: tr(tHostReadTitle, m.diffText(name)), consequence: tr(tHostReadNote),
		details: planDetails([]string{"qsysmin host user " + shellWord(name)}), keys: "y read as root · n/{back} cancel",
	}, func() tea.Cmd { return m.readUser(name) })
	return nil
}

func shellWord(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.-@", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (m *Model) readUser(name string) tea.Cmd {
	return m.runHost(HostRequest{Args: []string{"user", "--output", "json", "--", name}}, func(out []byte, err error) tea.Cmd {
		var f rootFields
		if err != nil || json.Unmarshal(bytes.TrimSpace(out), &f) != nil || f.Name != name {
			kind, exit := classifyRun(out, err)
			m.hostFailNotice(kind, exit, err, false)
			return nil
		}
		u := &m.hst.users
		for i := range u.all {
			if u.all[i].Name == name {
				u.all[i].Locked, u.all[i].Expired, u.all[i].SSHKeys = f.Locked, f.Expired, f.SSHKeys
			}
		}
		m.fillHostUsers()
		m.status = tr(tHostReadDone, m.diffText(name))
		return nil
	})
}
