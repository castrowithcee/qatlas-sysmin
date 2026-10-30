package tui

import (
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
)

// The kinds of a dialog of the area.
type projectDialogKind int

const (
	projDlgResult projectDialogKind = iota
	projDlgDelete
	projDlgRun
	projDlgRestore
	projDlgPick
	projDlgAdopt
)

// projectDialog is the question or result over the area; it is shown through m.ask.
type projectDialog struct {
	kind           projectDialogKind
	name, revision string
	// backup is the backup a restore asks about; picker is the list of a projDlgPick.
	backup string
	picker *projectPicker
	// adopt is the proposal of a projDlgAdopt; blocked says it cannot be taken over.
	adopt   *control.AdoptResult
	blocked bool
}

// projectControlOps maps the commands that steer a project to the operations of the service.
var projectControlOps = map[string]string{
	"pause":  control.OpDeployPause,
	"resume": control.OpDeployResume,
	"pin":    control.OpDeployPin,
	"unpin":  control.OpDeployUnpin,
}

// projectCommands are the commands of Projects. The controls have no key of their own: the letters of the key
// table are all taken by other meanings, so these commands are the way to run now, pause, resume, pin, unpin and
// restore. All of them act on the project under the cursor.
func (m *Model) projectCommands() []command {
	var list []command
	idle := every(inArea(areaProjects), noFlow, projectsIdle)
	onProject := every(idle, projectSelected)
	controls := []struct {
		name    string
		aliases []string
		kind    string
		desc    textID
	}{
		{"run now", []string{"run"}, "run", tCmdRunNow}, {"pause", nil, "pause", tCmdPause},
		{"resume", nil, "resume", tCmdResume}, {"pin", nil, "pin", tCmdPin}, {"unpin", nil, "unpin", tCmdUnpin},
	}
	for _, c := range controls {
		list = append(list, command{name: c.name, desc: tr(c.desc), aliases: c.aliases, unavailable: onProject,
			run: func(m *Model) tea.Cmd { return m.projectAction(c.kind) }})
	}
	// n, e and p act in the content of the list only.
	keyed := every(idle, inContent)
	keyedOnProject := every(keyed, projectSelected)
	key := func(a action) func() string { return func() string { return keyLabel(a) } }
	return append(list,
		command{name: "restore backup", desc: tr(tCmdRestore), aliases: []string{"restore"}, unavailable: onProject,
			run: func(m *Model) tea.Cmd { return m.projectRestore() }},
		command{name: "new project", desc: tr(tCmdNewProject), aliases: []string{"new"}, shortcut: key(actNew),
			unavailable: keyed, run: func(m *Model) tea.Cmd { return m.projectFlowStart() }},
		command{name: "adopt stack", desc: tr(tCmdAdopt), aliases: []string{"adopt"}, unavailable: idle,
			run: func(m *Model) tea.Cmd { return m.projectAdoptStart() }},
		command{name: "edit project", desc: tr(tCmdEdit), shortcut: key(actEdit), unavailable: keyedOnProject,
			run: func(m *Model) tea.Cmd { return m.projectRead(false) }},
		command{name: "duplicate project", desc: tr(tCmdDuplicate), aliases: []string{"new from project"},
			shortcut: key(actDuplicate), unavailable: keyedOnProject,
			run: func(m *Model) tea.Cmd { return m.projectRead(true) }})
}

// openProject shows the dialog d with the question q over the area.
func (m *Model) openProjectDialog(d *projectDialog, q question) {
	m.prj.dialog, m.ask, m.screen = d, &q, screenConfirm
	m.clearMessages()
}

func (m *Model) closeProjectDialog() {
	m.prj.dialog = nil
	m.closeDialog()
}

// projectsDialogOpen reports whether the question of the area is on screen.
func (m *Model) projectsDialogOpen() bool {
	return m.prj != nil && m.prj.dialog != nil && m.screen == screenConfirm && m.ask != nil
}

// updateProjectDialog handles a key while a dialog of the area is open.
func (m *Model) updateProjectDialog(key tea.KeyPressMsg) tea.Cmd {
	d := m.prj.dialog
	switch d.kind {
	case projDlgPick:
		return m.pickerKey(key)
	case projDlgAdopt:
		switch {
		case keyIs(key, actEnter) && !d.blocked:
			res := *d.adopt
			m.closeProjectDialog()
			return m.projectFlowShow(flowDraftOf(projectOfAdoption(res)), "")
		case keyIs(key, actBack):
			m.closeProjectDialog()
		}
	case projDlgRestore:
		switch {
		case keyIs(key, actYes):
			m.closeProjectDialog()
			return m.restoreProject(d.name, d.backup)
		case keyIs(key, actBack), keyIs(key, actNo):
			m.closeProjectDialog()
		}
	case projDlgResult:
		if keyIs(key, actBack) || keyIs(key, actEnter) {
			m.closeProjectDialog()
		}
	case projDlgDelete:
		switch {
		case keyIs(key, actYes):
			m.closeProjectDialog()
			return m.removeProject(d.name, d.revision)
		case keyIs(key, actBack), keyIs(key, actNo):
			m.closeProjectDialog()
		}
	case projDlgRun:
		switch {
		case keyIs(key, actYes):
			m.closeProjectDialog()
			return m.projectControl("run", d.name)
		case keyIs(key, actBack), keyIs(key, actNo):
			m.closeProjectDialog()
		}
	}
	return nil
}

// projectsEditKey handles a key of the filter being typed.
func (m *Model) projectsEditKey(key tea.KeyPressMsg) tea.Cmd {
	l := &m.prj.list
	switch {
	case keyIs(key, actEnter):
		l.stopFilter()
	case keyIs(key, actBack):
		l.clearFilter()
	case keyIs(key, actUp):
		l.move(-1)
	case keyIs(key, actDown):
		l.move(1)
	default:
		return l.updateFilter(key)
	}
	return nil
}

// projectsKey handles a key of the area. ok is false for a key it leaves to the area screen.
func (m *Model) projectsKey(key tea.KeyPressMsg) (cmd tea.Cmd, ok bool) {
	p := m.prj
	if p.log != nil {
		return m.projectLogKey(key)
	}
	if p.upd != nil {
		return m.projectUpdateKey(key)
	}
	wide := m.usable(0) >= projectsDetailMinWidth
	step := func(by int) { p.tab = projectTab((int(p.tab) + by + int(projTabCount)) % int(projTabCount)) }
	switch {
	case p.focusDetail:
		switch {
		case keyIs(key, actBack):
			p.focusDetail = false
			return nil, true
		case keyIs(key, actLeft):
			step(-1)
			return nil, true
		case keyIs(key, actRight):
			step(1)
			return nil, true
		case keyIs(key, actUp), keyIs(key, actDown), pageKey(key):
			p.pane.view.scroll(key, p.pane.page)
			return nil, true
		}
	case wide && keyIs(key, actLeft):
		step(-1)
		return nil, true
	case wide && keyIs(key, actRight):
		step(1)
		return nil, true
	case keyIs(key, actUp) && p.list.cursor <= 0:
		m.toBar()
		return nil, true
	case keyIs(key, actUp):
		p.list.move(-1)
		return nil, true
	case keyIs(key, actDown):
		p.list.move(1)
		return nil, true
	case keyIs(key, actPageUp), keyIs(key, actPageDown), keyIs(key, actHome), keyIs(key, actEnd):
		p.list.page(key, 0, max(m.height-projectChrome(m, m.tableFramed())-2, 1))
		return nil, true
	case keyIs(key, actEnter):
		if _, has := p.current(); has {
			p.focusDetail = true
		}
		return nil, true
	case keyIs(key, actSpace):
		if name, has := p.current(); has {
			p.marked[name] = !p.marked[name]
			if !p.marked[name] {
				delete(p.marked, name)
			}
			p.list.move(1)
		}
		return nil, true
	case keyIs(key, actSearch), keyIs(key, actFilter):
		p.list.startFilter()
		return nil, true
	case keyIs(key, actBack):
		if p.list.query() != "" {
			p.list.clearFilter()
			return nil, true
		}
	}
	switch {
	case keyIs(key, actNew):
		return m.projectFlowStart(), true
	case keyIs(key, actTest):
		return m.projectSourceTest(), true
	case keyIs(key, actEdit):
		return m.projectRead(false), true
	case keyIs(key, actDuplicate):
		return m.projectRead(true), true
	case keyIs(key, actDelete):
		return m.projectDelete(), true
	case keyIs(key, actLogs):
		return m.projectOpenLog(), true
	case keyIs(key, actUpdate):
		return m.projectUpdateOpen(), true
	case keyIs(key, actReload):
		return tea.Batch(m.statusCmd(), m.projectListCmd()), true
	}
	return nil, false
}

// selectedProject is the project under the cursor; without one it says so.
func (m *Model) selectedProject() (string, bool) {
	name, ok := m.prj.current()
	if !ok {
		m.fail = tr(tProjNoSel)
	}
	return name, ok
}

// projectAction runs a command of the area on the project under the cursor.
func (m *Model) projectAction(kind string) tea.Cmd {
	if m.area != areaProjects || m.backend == nil {
		return nil
	}
	name, ok := m.selectedProject()
	if !ok {
		return nil
	}
	if kind == "run" {
		m.openProjectDialog(&projectDialog{kind: projDlgRun, name: name}, question{
			title: tr(tProjRunTitle, name), consequence: tr(tProjRunNote), keys: "y run · n/{back} cancel"})
		return nil
	}
	return m.projectControl(kind, name)
}

// projectControl runs run-now, pause, resume, pin or unpin as a command.
func (m *Model) projectControl(kind, name string) tea.Cmd {
	m.clearMessages()
	if kind == "run" {
		m.busy = tr(tBusyRun, name)
	}
	b := m.backend
	return func() tea.Msg {
		msg := projectActionMsg{name: name, kind: kind}
		if kind == "run" {
			msg.run, msg.err = b.DeployRunNow(name)
		} else {
			msg.err = b.DeployControl(projectControlOps[kind], name)
		}
		return msg
	}
}

func (m *Model) takeProjectAction(msg projectActionMsg) tea.Cmd {
	m.busy = ""
	switch {
	case msg.err != nil:
		m.fail = projectActionError(msg.err)
	case msg.kind != "run":
		m.status = tr(map[string]textID{"pause": tProjPaused, "resume": tProjResumed, "pin": tProjPinned, "unpin": tProjUnpinned}[msg.kind], msg.name)
	case msg.run.Pending:
		m.status = tr(tProjRunPending, msg.name)
	case msg.run.Skipped != "":
		m.status = tr(tProjRunSkipped, msg.name, wordText(msg.run.Skipped))
	case msg.run.Run != nil:
		m.status = tr(tProjRunResult, msg.name, wordText(msg.run.Run.Result))
	default:
		m.status = tr(tProjRunNothing, msg.name)
	}
	if m.prj.rows[msg.name] == nil {
		return nil
	}
	return m.projectDataCmd(msg.name)
}

// projectActionError is the fixed text of a failed action.
func projectActionError(err error) string {
	switch {
	case isCode(err, control.CodePaused):
		return tr(tProjIsPaused)
	case isCode(err, control.CodePinned):
		return tr(tProjIsPinned)
	}
	return projectErrorText(err)
}

// projectSourceTest reads the versions of the sources of the project under the cursor.
func (m *Model) projectSourceTest() tea.Cmd {
	name, ok := m.selectedProject()
	if !ok || m.backend == nil {
		return nil
	}
	m.clearMessages()
	m.busy = tr(tBusySource, name)
	b := m.backend
	return func() tea.Msg {
		res, err := b.SourceTest(name)
		return projectSourceMsg{name: name, result: res, err: err}
	}
}

func (m *Model) takeProjectSource(msg projectSourceMsg) {
	m.busy = ""
	switch {
	case msg.err != nil:
		m.fail = projectErrorText(msg.err)
	case m.screen != screenArea || m.area != areaProjects:
		m.status = tr(tProjSourceDone, msg.name)
	default:
		var lines []string
		for _, s := range msg.result.Sources {
			lines = append(lines, projectSourceLines(s)...)
		}
		m.openProjectDialog(&projectDialog{kind: projDlgResult, name: msg.name}, question{
			title: tr(tProjSourceTitle, msg.name), consequence: tr(tProjSourceNote), details: lines, keys: "{back} close"})
	}
}

// projectSourceLines are the lines of one source of a source test, short enough for a dialog.
func projectSourceLines(s control.SourceResult) []string {
	const width = 56
	head := "  " + s.Name
	if s.Ref != "" {
		head += "  " + s.Ref
	}
	lines := []string{styles.title.Render(truncateCells(safeText(head), width))}
	detail := ""
	switch {
	case s.Error != "":
		detail = sourceErrorText(s.Error)
	case len(s.Available) > 0:
		detail = tr(tProjNewer, orDash(s.Current), s.Available[len(s.Available)-1], max(s.AvailableTotal, len(s.Available)))
	default:
		detail = tr(tProjUpToDate, orDash(s.Current))
	}
	if n := len(s.Problems); n > 0 && s.Error == "" {
		detail += " · " + tr(tProjProblems, n)
	}
	return append(lines, "    "+truncateCells(safeText(detail), width-2))
}

// sourceErrorText is the fixed text of the code a source could not be read with.
func sourceErrorText(code string) string {
	ids := map[string]textID{
		control.SourceDenied: tSrcDenied, control.SourceNotFound: tSrcNotFound, control.SourceUnreachable: tSrcUnreachable,
		control.SourceTimeout: tSrcTimeout, control.SourceCredential: tSrcCredential, control.SourceInvalid: tSrcInvalid,
	}
	if id, ok := ids[code]; ok {
		return tr(id)
	}
	return tr(tErrService)
}

// projectDelete asks before the project under the cursor is deleted.
func (m *Model) projectDelete() tea.Cmd {
	name, ok := m.selectedProject()
	if !ok {
		return nil
	}
	r, _ := m.prj.currentRow()
	if r == nil || r.detail == nil {
		m.fail = tr(tProjNeedFile)
		return nil
	}
	m.openProjectDialog(&projectDialog{kind: projDlgDelete, name: name, revision: r.detail.Revision}, question{
		title: tr(tProjDeleteTitle, name), consequence: tr(tProjDeleteNote), warn: true, danger: true,
		keys: "y delete · n/{back} cancel"})
	return nil
}

// removeProject deletes the project file of the revision the person saw; it needs the admin session.
func (m *Model) removeProject(name, revision string) tea.Cmd {
	return m.requireAdmin(change{
		call: func(b Backend) error { _, err := b.RemoveProject(name, revision); return err },
		done: func(m *Model, err error) tea.Cmd {
			if err != nil {
				m.fail = projectErrorText(err)
				return nil
			}
			m.status = tr(tProjRemoved, name)
			return m.projectListCmd()
		},
	})
}

// projectLog is the log of a run, shown in place of the list.
type projectLog struct {
	name, run string
	cut       bool
	lines     []string
	wrapped   []string
	width     int
	page      int
	scroll    textScroll
}

// projectOpenLog reads the log of the last run of the project under the cursor.
func (m *Model) projectOpenLog() tea.Cmd {
	name, ok := m.selectedProject()
	if !ok || m.backend == nil {
		return nil
	}
	run := ""
	if r, _ := m.prj.currentRow(); r != nil && r.status != nil {
		if r.status.LastRun == nil {
			m.fail = tr(tProjLogNone)
			return nil
		}
		run = r.status.LastRun.ID
	}
	m.clearMessages()
	m.busy = tr(tBusyLog, name)
	b := m.backend
	return func() tea.Msg {
		// The state of the project is not read yet: the history names its last run.
		if run == "" {
			h, err := b.DeployHistory(name)
			if err != nil {
				return projectLogMsg{name: name, err: err}
			}
			if len(h.Runs) == 0 {
				return projectLogMsg{name: name, none: true}
			}
			run = h.Runs[len(h.Runs)-1].ID
		}
		res, err := b.DeployLog(name, run)
		return projectLogMsg{name: name, result: res, err: err}
	}
}

func (m *Model) takeProjectLog(msg projectLogMsg) {
	m.busy = ""
	switch {
	case msg.none:
		m.fail = tr(tProjLogNone)
	case isCode(msg.err, control.CodeNotFound):
		m.fail = tr(tProjLogGone)
	case msg.err != nil:
		m.fail = projectErrorText(msg.err)
	case m.screen != screenArea || m.area != areaProjects:
	default:
		text := strings.TrimRight(safeText(m.redactor.Apply(msg.result.Text)), "\n")
		m.prj.log = &projectLog{name: msg.name, run: msg.result.Run, cut: msg.result.Truncated, lines: strings.Split(text, "\n")}
	}
}

func (m *Model) projectLogKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	l := m.prj.log
	switch {
	case keyIs(key, actBack):
		m.prj.log = nil
		return nil, true
	case l.scroll.scroll(key, l.page):
		return nil, true
	}
	return nil, false
}

// projectLogView is the log panel, room lines high.
func (m *Model) projectLogView(room int) string {
	l := m.prj.log
	width, framed := m.usable(0), m.tableFramed()
	inner := width
	if framed {
		inner -= 4
	}
	rows := max(room-projectChrome(m, framed), 1)
	if l.width != inner {
		l.width, l.wrapped = inner, nil
		for _, line := range l.lines {
			l.wrapped = append(l.wrapped, strings.Split(wrapCells(line, inner), "\n")...)
		}
	}
	var body []string
	if l.cut {
		body = append(body, styles.hint.Render(truncateCells(tr(tProjLogCut), inner)))
	}
	l.page = max(rows-len(body), 1)
	body = append(body, l.scroll.window(l.wrapped, l.page, false)...)
	title := truncateCells(tr(tProjLogTitle, l.name, l.run), max(inner-24, 8))
	return strings.Join(m.projectFrame(title, l.scroll.position(len(l.wrapped), l.page), body, width, rows, true, framed), "\n")
}

// projectsKeys is the key line of the area.
func (m *Model) projectsKeys() []string {
	p := m.prj
	switch {
	case p.log != nil:
		return []string{"↑/↓ scroll", "pgup/pgdown page", "home/end ends", "{back} back"}
	case p.upd != nil && p.upd.results != nil:
		return []string{"↑/↓ scroll", "{back} close"}
	case p.upd != nil:
		return []string{"↑/↓ item", "enter version", "pgup/pgdown notes", "y update", "n/{back} cancel"}
	case p.focusDetail:
		return []string{"↑/↓ scroll", "←/→ tab", "t test", "u update", "e edit", "p duplicate", "d delete", "o log", "{back} list"}
	}
	keys := []string{"↑/↓ move", "t test", "o log", "u update", "space mark", "enter detail", "n new", "e edit", "p duplicate", "/ filter", "d delete"}
	if m.usable(0) >= projectsDetailMinWidth {
		keys = append(keys, "←/→ tab")
	}
	return keys
}

// Secret-looking keys of a project file. The file holds credential names only, but a value in it is free
// text, so what looks like a secret is hidden all the same.
var (
	secretKeyRE = regexp.MustCompile(`(?i)^(\s*(?:-\s*)?[\w.-]*(?:password|passwd|secret|token|api_?key|private)[\w.-]*\s*:\s*)\S.*$`)
	urlUserRE   = regexp.MustCompile(`(://)[^/\s@]+@`)
)

// maskedFile is a project file as the Raw tab shows it: with the secrets the interface knows, the user part of
// addresses and the values of secret-looking keys replaced by the marker, and without control characters.
func (m *Model) maskedFile(file string) string {
	lines := strings.Split(safeText(m.redactor.Apply(file)), "\n")
	hideBelow := -1
	for i, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if hideBelow >= 0 && indent > hideBelow && strings.TrimSpace(line) != "" {
			lines[i] = strings.Repeat(" ", indent) + redact.Marker
			continue
		}
		hideBelow = -1
		line = urlUserRE.ReplaceAllString(line, "${1}"+redact.Marker+"@")
		if secretKeyRE.MatchString(line) {
			// A block value continues on the lines below it.
			if _, v, _ := strings.Cut(line, ":"); strings.HasPrefix(strings.TrimSpace(v), "|") || strings.HasPrefix(strings.TrimSpace(v), ">") {
				hideBelow = indent
			}
			line = secretKeyRE.ReplaceAllString(line, "${1}"+redact.Marker)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}
