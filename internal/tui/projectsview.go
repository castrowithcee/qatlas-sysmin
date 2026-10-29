package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// projectsBody is the Projects area under the tab line: the list, and beside it the detail pane where the
// workspace is wide enough. A narrow workspace shows one of them, the detail once the person opened it.
func (m *Model) projectsBody() string {
	p := m.prj
	room := max(m.height-2-strings.Count(m.notes(), "\n"), 1)
	if p.flow != nil {
		return m.flowView(room)
	}
	if p.log != nil {
		return m.projectLogView(room)
	}
	if p.upd != nil {
		return m.projectUpdateView(room)
	}
	width, framed := m.usable(0), m.tableFramed()
	rows := max(room-projectChrome(m, framed), 1)
	switch {
	case width >= projectsDetailMinWidth:
		listW := width * 3 / 5
		p.gap = 1
		left := m.projectListLines(0, listW, rows, !p.focusDetail, framed)
		p.gap = 0
		right := m.projectDetailLines(listW, width-listW, rows, p.focusDetail, framed)
		out := make([]string, len(left))
		for i := range left {
			out[i] = padLine(left[i], listW) + right[i]
		}
		return strings.Join(out, "\n")
	case p.focusDetail:
		return strings.Join(m.projectDetailLines(0, width, rows, true, framed), "\n")
	}
	return strings.Join(m.projectListLines(0, width, rows, true, framed), "\n")
}

// projectChrome is the number of lines a panel adds around its body rows: the title and the frame.
func projectChrome(m *Model, framed bool) int {
	if framed {
		return m.titleRows() + 2
	}
	return 1
}

// projectFrame lays body into a panel of width cells with rows body rows under a title line, as the lists of
// the editor are drawn. Without room for a frame the title stands over the bare rows.
func (m *Model) projectFrame(title, note string, body []string, width, rows int, focused, framed bool) []string {
	out := []string{titleLine(title, note, width-m.prj.gap)}
	if !framed {
		out = append(out, body[:min(len(body), rows)]...)
		for len(out) < rows+1 {
			out = append(out, "")
		}
		return out
	}
	inner := width - 4
	if m.titleBlank() {
		out = append(out, "")
	}
	out = append(out, panelTop("", "", inner, focused))
	for i := 0; i < rows; i++ {
		text := ""
		if i < len(body) {
			text = body[i]
		}
		out = append(out, panelSide(text, inner, focused))
	}
	return append(out, panelBottom(inner, focused))
}

// projectListLines is the list panel of width cells starting at column x, rows body rows high.
func (m *Model) projectListLines(x, width, rows int, focused, framed bool) []string {
	p := m.prj
	room := width
	if framed {
		room = width - 4
	}
	total, shown := len(p.list.all), len(p.list.matches)
	pos := ""
	if filtered := p.list.editing || p.list.query() != ""; filtered {
		pos = fmt.Sprintf("%d/%d (%d total)", min(p.list.cursor+1, shown), shown, total)
	} else if total > 0 {
		pos = fmt.Sprintf("%d/%d", p.list.cursor+1, total)
	}
	m.screenPos = pos
	var body []string
	if p.list.editing || p.list.query() != "" {
		body = append(body, m.searchLineIn(room, &p.list, tr(tFormFilter)))
	}
	cells := make(map[string][]string, total)
	for _, name := range p.list.all {
		cells[name] = projectCells(p.rows[name], p.marked[name])
	}
	t := table{columns: projectColumns(), rows: cells}
	note := func(style lipgloss.Style, text string) {
		body = append(body, strings.Split(render(style, room, text), "\n")...)
	}
	switch {
	case !p.loaded:
		note(styles.hint, tr(tChecking))
	case p.err != "":
		note(styles.fail, tr(tNoteError, p.err))
	case total == 0:
		note(styles.hint, tr(tProjEmpty))
	case shown == 0:
		note(styles.hint, tr(tFormNoEntryMatchesPressEsc, p.list.query()))
	}
	if shown > 0 {
		_, w := m.fitIn(room, entryIndent)
		layout := t.layout(w)
		body = append(body, styles.hint.Render(clipCells(entryIndent+t.header(layout), room)))
		start, end := p.list.window(max(rows-len(body), 1), func(int) int { return 1 })
		before := 1
		if framed {
			before = m.titleRows() + 1
		}
		top := m.workspaceTop() + 2 + before + len(body)
		for i := start; i < end; i++ {
			name := p.list.matches[i]
			body = append(body, m.listRowIn(room, i == p.list.cursor, t.line(cells[name], layout)))
			m.addHit(x, top+i-start, x+width, top+i-start+1, func() tea.Cmd { return m.clickProject(i) })
		}
	}
	return m.projectFrame(fmt.Sprintf("%s (%d)", tr(tAreaProjects), total), pos, body, width, rows, focused, framed)
}

// clickProject marks a row; a click on the marked row does what enter does.
func (m *Model) clickProject(i int) tea.Cmd {
	p := m.prj
	if m.screen != screenArea || p.log != nil || i >= len(p.list.matches) || p.list.editing {
		return nil
	}
	return markOrAct(&p.list.cursor, i, !p.focusDetail, func() tea.Cmd { return m.press(tea.KeyPressMsg{Code: tea.KeyEnter}) })
}

// projectDetailLines is the detail panel of width cells starting at column x: the state of the project under
// the cursor, the tabs, and the page of the tab, which scrolls inside the room.
func (m *Model) projectDetailLines(x, width, rows int, focused, framed bool) []string {
	p := m.prj
	room := width
	if framed {
		room = width - 4
	}
	r, ok := p.currentRow()
	if !ok {
		return m.projectFrame(tr(tProjDetail), "", []string{styles.hint.Render(tr(tProjNoSel))}, width, rows, focused, framed)
	}
	name, _ := p.current()
	kind, word := r.state()
	words := []string{tr(tProjTabOverview), tr(tProjTabDetails), tr(tProjTabRaw)}
	strip, cells := tabStrip(words, int(p.tab), focused)
	before := 1
	if framed {
		before = m.titleRows() + 1
	}
	inset := 0
	if framed {
		inset = 2
	}
	y := m.workspaceTop() + 2 + before + 1
	for i, c := range cells {
		m.addHit(x+inset+c[0], y, x+inset+min(c[1], room), y+1, func() tea.Cmd { p.tab = projectTab(i); return nil })
	}
	body := []string{statusMark(kind, word), clipCells(strip, room), ""}
	var content []string
	switch p.tab {
	case projTabOverview:
		content = m.projectOverview(r, room)
	case projTabDetails:
		content = m.projectDetails(r, room)
	default:
		content = m.projectRaw(r, room)
	}
	shown, position := p.pane.fit(fmt.Sprintf("%s/%d", name, p.tab), content, max(rows-len(body), 1))
	body = append(body, shown...)
	if position != "" {
		body = append(body, styles.hint.Render(truncateCells(position, room)))
	}
	return m.projectFrame(name, "", body, width, rows, focused, framed)
}

// kvLines lays label and value pairs out with the labels dimmed in one column and a long value continued
// under itself.
func kvLines(room int, pairs [][2]string) []string {
	lw := 0
	for _, pair := range pairs {
		lw = max(lw, lipgloss.Width(pair[0]))
	}
	lw = min(lw, max(room/3, 4))
	width := max(room-lw-2, 1)
	var out []string
	for _, pair := range pairs {
		value := pair[1]
		if value == "" {
			value = "-"
		}
		for i, part := range strings.Split(render(lipgloss.NewStyle(), width, safeText(value)), "\n") {
			label := strings.Repeat(" ", lw)
			if i == 0 {
				label = styles.hint.Render(padLine(truncateCells(pair[0], lw), lw))
			}
			out = append(out, label+"  "+part)
		}
	}
	return out
}

// safeText is text from outside the interface with everything removed that could act on the terminal: escape
// sequences and control characters, but not line ends. Tabs become blanks.
func safeText(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < ' ' || (r >= 0x7f && r < 0xa0):
			return -1
		}
		return r
	}, s)
}

func (m *Model) projectOverview(r *projectRow, room int) []string {
	if !r.loaded {
		return []string{styles.hint.Render(tr(tProjLoading))}
	}
	s := r.status
	if s == nil {
		return []string{styles.hint.Render(tr(tValNoData))}
	}
	pairs := [][2]string{
		{tr(tProjColSource), wordText(r.sum.Source)},
		{tr(tProjColMode), r.mode()},
		{tr(tProjLblCheck), projectTime(s.LastCheck, "2006-01-02 15:04")},
	}
	if s.CheckError != "" {
		pairs[2][1] += " (" + tr(tProjCheckFailed) + ")"
	}
	if run := s.LastRun; run != nil {
		last := projectTime(run.Started, "2006-01-02 15:04") + " " + wordText(run.Result) + " (" + wordText(run.Action) + ")"
		pairs = append(pairs, [2]string{tr(tProjColLastRun), last})
		if run.FailedStep != "" {
			pairs = append(pairs, [2]string{tr(tProjLblFailedStep), run.FailedStep})
		}
	} else {
		pairs = append(pairs, [2]string{tr(tProjColLastRun), tr(tProjNoRun)})
	}
	if f := s.Fault; f != nil {
		pairs = append(pairs, [2]string{tr(tProjError), f.FailedStep + " " + projectTime(f.Time, "2006-01-02 15:04")})
	}
	for _, src := range s.Sources {
		pairs = append(pairs, [2]string{sourceLabel(src), sourceText(src)})
	}
	return kvLines(room, pairs)
}

// sourceText is the versions of a source in one phrase.
func sourceText(s control.DeploySource) string {
	if s.Error != "" {
		return tr(tProjSourceUnread)
	}
	cur := s.Current
	if cur == "" {
		cur = "-"
	}
	text := tr(tProjUpToDate, cur)
	if n := len(s.Available); n > 0 {
		text = tr(tProjNewer, cur, s.Available[n-1], n)
	}
	if s.Failed != "" {
		text += "; " + tr(tProjRolledBack, s.Failed)
	}
	return text
}

func (m *Model) projectDetails(r *projectRow, room int) []string {
	if !r.loaded {
		return []string{styles.hint.Render(tr(tProjLoading))}
	}
	cfg := r.cfg
	if cfg == nil || r.detail == nil {
		return []string{styles.hint.Render(tr(tProjNoFile))}
	}
	pairs := [][2]string{
		{tr(tProjLblDir), cfg.Dir},
		{tr(tProjLblTemplate), cfg.Template},
		{tr(tProjLblEnv), cfg.EnvFile},
		{tr(tProjLblCompose), strings.Join(cfg.ComposeFiles, ", ")},
		{tr(tProjLblPoll), pollText(cfg.PollInterval)},
		{tr(tProjLblRevision), r.detail.Revision},
	}
	for _, img := range cfg.Images {
		text := img.Ref
		if img.Credential != "" {
			text += " · " + img.Credential
		}
		pairs = append(pairs, [2]string{img.Name, text + " · " + wordText(modeOf(img.VersionRule.Mode))})
	}
	if g := cfg.Git; g != nil {
		text := g.URL
		if g.Credential != "" {
			text += " · " + g.Credential
		}
		pairs = append(pairs, [2]string{control.SourceGit, text + " · " + wordText(modeOf(g.VersionRule.Mode))})
	}
	return kvLines(room, pairs)
}

func modeOf(mode string) string {
	if mode == "" {
		return "manual"
	}
	return mode
}

func pollText(d config.Duration) string {
	if d == 0 {
		return ""
	}
	return time.Duration(d).String()
}

func (m *Model) projectRaw(r *projectRow, room int) []string {
	if !r.loaded {
		return []string{styles.hint.Render(tr(tProjLoading))}
	}
	if r.detail == nil {
		return []string{styles.hint.Render(tr(tProjNoFile))}
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(m.maskedFile(r.detail.File), "\n"), "\n") {
		out = append(out, strings.Split(wrapCells(line, room), "\n")...)
	}
	return out
}
