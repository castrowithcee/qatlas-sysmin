package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The Projects area lists the projects of the host with their deploy state, shows the marked one in a detail
// pane and steers it. Reading needs no passphrase; deleting goes through requireAdmin.

// projectZone is the zone times are shown in.
var projectZone = time.Local

// projectRow is what is known of one project: its summary from the list, and the deploy state and the stored
// file, which arrive one project at a time.
type projectRow struct {
	sum    control.ProjectSummary
	status *control.DeployStatusResult
	detail *control.ProjectDetail
	// cfg is the decoded file, nil while it is unknown or does not decode.
	cfg *config.Project
	// loaded says the answer of the two reads arrived, whatever it was.
	loaded bool
}

// projectTab is a tab of the detail pane.
type projectTab int

const (
	projTabOverview projectTab = iota
	projTabDetails
	projTabRaw
	projTabCount
)

// projectsDetailMinWidth is the narrowest workspace that carries the detail pane beside the list.
const projectsDetailMinWidth = 120

// projectsState is the state of the Projects area.
type projectsState struct {
	// gen numbers the loads, so an answer of an older one is dropped.
	gen int
	// gap is the cells the title line of the panel being drawn leaves free at its right, where another panel
	// follows.
	gap int
	// loaded says the list was read; err is the fixed text of a failed read.
	loaded bool
	err    string
	rows   map[string]*projectRow
	list   filterList
	// marked holds the projects the person marked with space; the update of several projects reads it.
	marked map[string]bool
	// focusDetail puts the focus on the detail pane, which fills the workspace where it does not fit beside
	// the list.
	focusDetail bool
	tab         projectTab
	pane        paneScroll
	log         *projectLog
	dialog      *projectDialog
}

func newProjectsState() *projectsState {
	p := &projectsState{rows: map[string]*projectRow{}, marked: map[string]bool{}}
	p.list = newFilterList(p.searchText)
	return p
}

// searchText is what the filter looks at for a project: the cells of its row.
func (p *projectsState) searchText(name string) string {
	r := p.rows[name]
	if r == nil {
		return name
	}
	return strings.Join(projectCells(r, false), " ")
}

func (p *projectsState) current() (string, bool) { return p.list.selected() }

// currentRow is the row under the cursor.
func (p *projectsState) currentRow() (*projectRow, bool) {
	name, ok := p.current()
	if !ok {
		return nil, false
	}
	r, ok := p.rows[name]
	return r, ok
}

// markedProjects are the marked projects in the order of the list.
func (p *projectsState) markedProjects() []string {
	var out []string
	for _, name := range p.list.all {
		if p.marked[name] {
			out = append(out, name)
		}
	}
	return out
}

// projectsMsg is implemented by every answer of the Projects area.
type projectsMsg interface{ projectsMsg() }

// projectsListMsg is the answer of the list read.
type projectsListMsg struct {
	gen  int
	list []control.ProjectSummary
	err  error
}

// projectDataMsg is the answer of the two reads of one project.
type projectDataMsg struct {
	gen       int
	name      string
	status    control.DeployStatusResult
	statusErr error
	detail    control.ProjectDetail
	detailErr error
}

// projectActionMsg is the outcome of run-now, pause, resume, pin or unpin.
type projectActionMsg struct {
	name, kind string
	run        control.DeployRunNowResult
	err        error
}

// projectSourceMsg is the outcome of a source test.
type projectSourceMsg struct {
	name   string
	result control.SourceTestResult
	err    error
}

// projectLogMsg is the outcome of a log read.
type projectLogMsg struct {
	name   string
	result control.DeployLogResult
	err    error
}

func (projectsListMsg) projectsMsg()  {}
func (projectDataMsg) projectsMsg()   {}
func (projectActionMsg) projectsMsg() {}
func (projectSourceMsg) projectsMsg() {}
func (projectLogMsg) projectsMsg()    {}

// projectsCmd reads the list of projects; the state of each follows with the answer.
func (m *Model) projectsCmd() tea.Cmd {
	if m.backend == nil || m.area != areaProjects {
		return nil
	}
	p := m.prj
	p.gen++
	gen, b := p.gen, m.backend
	return func() tea.Msg {
		list, err := b.Projects()
		return projectsListMsg{gen: gen, list: list, err: err}
	}
}

// projectDataCmd reads the deploy state and the stored file of one project.
func (m *Model) projectDataCmd(name string) tea.Cmd {
	gen, b := m.prj.gen, m.backend
	return func() tea.Msg {
		msg := projectDataMsg{gen: gen, name: name}
		msg.status, msg.statusErr = b.DeployStatus(name)
		msg.detail, msg.detailErr = b.Project(name)
		return msg
	}
}

// takeProjects handles an answer of the area.
func (m *Model) takeProjects(msg projectsMsg) tea.Cmd {
	switch msg := msg.(type) {
	case projectsListMsg:
		return m.takeProjectsList(msg)
	case projectDataMsg:
		m.takeProjectData(msg)
	case projectActionMsg:
		return m.takeProjectAction(msg)
	case projectSourceMsg:
		m.takeProjectSource(msg)
	case projectLogMsg:
		m.takeProjectLog(msg)
	}
	return nil
}

func (m *Model) takeProjectsList(msg projectsListMsg) tea.Cmd {
	p := m.prj
	if msg.gen != p.gen {
		return nil
	}
	p.loaded = true
	if msg.err != nil {
		p.err, p.rows = errorText(msg.err), map[string]*projectRow{}
		p.list.setItems(nil)
		return nil
	}
	p.err = ""
	rows := make(map[string]*projectRow, len(msg.list))
	names := make([]string, 0, len(msg.list))
	for _, s := range msg.list {
		r := p.rows[s.Name]
		if r == nil {
			r = &projectRow{}
		}
		r.sum = s
		rows[s.Name] = r
		names = append(names, s.Name)
	}
	p.rows = rows
	for name := range p.marked {
		if rows[name] == nil {
			delete(p.marked, name)
		}
	}
	p.list.setItems(names)
	cmds := make([]tea.Cmd, 0, len(names))
	for _, name := range names {
		cmds = append(cmds, m.projectDataCmd(name))
	}
	return tea.Batch(cmds...)
}

func (m *Model) takeProjectData(msg projectDataMsg) {
	p := m.prj
	r := p.rows[msg.name]
	if r == nil || msg.gen != p.gen {
		return
	}
	r.loaded = true
	r.status, r.detail, r.cfg = nil, nil, nil
	if msg.statusErr == nil {
		r.status = &msg.status
	}
	if msg.detailErr == nil {
		r.detail = &msg.detail
		if cfg, err := config.DecodeProject([]byte(msg.detail.File)); err == nil {
			r.cfg = &cfg
		}
	}
}

// projectErrorText is errorText with the words a project action adds.
func projectErrorText(err error) string {
	switch {
	case isCode(err, control.CodeChanged):
		return tr(tProjChanged)
	case isCode(err, control.CodeNotFound):
		return tr(tProjGone)
	case isCode(err, control.CodeNotApproved):
		return tr(tProjNotApproved)
	}
	return errorText(err)
}

// The cells of the list.
const (
	projColName = iota
	projColSource
	projColCurrent
	projColAvailable
	projColMode
	projColState
	projColLastRun
)

// projectColumns are the columns of the list. Where the width runs short the last run goes first, then the
// source, the mode and the available version.
func projectColumns() []column {
	return []column{
		{title: tr(tProjColName)},
		{title: tr(tProjColSource), optional: true, drop: 3},
		{title: tr(tProjColCurrent), optional: true},
		{title: tr(tProjColAvailable), optional: true, drop: 1},
		{title: tr(tProjColMode), optional: true, drop: 2},
		{title: tr(tProjColState), tone: projectStateTone},
		{title: tr(tProjColLastRun), optional: true, drop: 4, tone: toneDim},
	}
}

// projectStateTone styles a state cell by its mark, so the word carries the meaning without colour.
func projectStateTone(cell string) lipgloss.Style {
	switch {
	case strings.HasPrefix(cell, "●"):
		return styles.ok
	case strings.HasPrefix(cell, warnMark):
		return styles.warning
	case strings.HasPrefix(cell, "✗"):
		return styles.fail
	}
	return styles.hint
}

// projectCells are the cells of a row. marked leads the name with the mark of a marked project.
func projectCells(r *projectRow, marked bool) []string {
	lead := "  "
	if marked {
		lead = "✓ "
	}
	kind, word := r.state()
	last := "-"
	if r.status != nil && r.status.LastRun != nil {
		last = projectTime(r.status.LastRun.Started, "01-02 15:04") + " " + wordText(r.status.LastRun.Result)
	}
	return []string{
		lead + r.sum.Name,
		wordText(r.sum.Source),
		r.versions(func(s control.DeploySource) string { return s.Current }),
		r.versions(func(s control.DeploySource) string {
			if s.Error != "" {
				return "?"
			}
			if n := len(s.Available); n > 0 {
				return s.Available[n-1]
			}
			return ""
		}),
		r.mode(),
		statusGlyph(kind) + " " + word,
		last,
	}
}

// statusGlyph is the mark of a state kind, the one statusRow draws.
func statusGlyph(kind statusKind) string {
	switch kind {
	case statusWarn:
		return warnMark
	case statusFail:
		return "✗"
	case statusDim:
		return "○"
	}
	return "●"
}

// projectTime is t in the zone of the list, or - while there is none.
func projectTime(t time.Time, layout string) string {
	if t.IsZero() {
		return "-"
	}
	return t.In(projectZone).Format(layout)
}

// state is the state of the project as one word with its kind: an error or a run first, then the approval.
func (r *projectRow) state() (statusKind, string) {
	switch {
	case !r.loaded:
		return statusDim, tr(tProjLoading)
	case r.status == nil:
		return statusDim, tr(tValNoData)
	case r.status.Fault != nil:
		return statusFail, tr(tProjError)
	case r.status.Running:
		return statusOK, tr(tProjRunning)
	}
	switch r.sum.State {
	case control.StateApproved:
		return statusOK, tr(tProjApproved)
	case control.StatePending:
		return statusWarn, tr(tProjPending)
	}
	return statusWarn, tr(tValUnknown)
}

// versions lists one version per source; a project with several sources names each.
func (r *projectRow) versions(pick func(control.DeploySource) string) string {
	if r.status == nil {
		return "-"
	}
	var parts []string
	for _, s := range r.status.Sources {
		v := pick(s)
		if v == "" {
			v = "-"
		}
		if len(r.status.Sources) > 1 {
			v = sourceLabel(s) + " " + v
		}
		parts = append(parts, v)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

// sourceLabel names a source: the image, or git.
func sourceLabel(s control.DeploySource) string {
	if s.Name == "" {
		return control.SourceGit
	}
	return s.Name
}

// mode is how the project updates: the version rule of its sources, then what holds it back.
func (r *projectRow) mode() string {
	var parts []string
	if r.cfg != nil {
		parts = append(parts, wordText(projectRuleMode(r.cfg)))
	}
	if r.status != nil {
		if r.status.Paused {
			parts = append(parts, wordText("paused"))
		}
		if r.status.Pinned {
			parts = append(parts, wordText("pinned"))
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

// projectRuleMode is the version rule mode all sources of the project share, or mixed. A source without a
// mode follows the default, manual.
func projectRuleMode(cfg *config.Project) string {
	var modes []string
	for _, img := range cfg.Images {
		modes = append(modes, img.VersionRule.Mode)
	}
	if cfg.Git != nil {
		modes = append(modes, cfg.Git.VersionRule.Mode)
	}
	mode := ""
	for i, v := range modes {
		if v == "" {
			v = config.ModeManual
		}
		switch {
		case i == 0:
			mode = v
		case v != mode:
			return "mixed"
		}
	}
	if mode == "" {
		return config.ModeManual
	}
	return mode
}
