package tui

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The manual update of the Projects area: a panel in place of the list shows, for each source of the chosen
// projects that has a newer version, the current and the target version and the release notes between them.
// Nothing starts before the person confirms with y; the start goes through requireAdmin.

// updateItem is one source with newer versions and the version chosen as its target.
type updateItem struct {
	project string
	src     control.DeploySource
	target  string
}

func (it *updateItem) label() string { return it.project + " · " + sourceLabel(it.src) }

// key names the release notes of the item: they depend on the target.
func (it *updateItem) key() string {
	return strings.Join([]string{it.project, it.src.Kind, it.src.Name, it.target}, "\x00")
}

// updateNotes are the release notes of one item as they load.
type updateNotes struct {
	loading bool
	// err is the fixed text of a failed read.
	err string
	src *control.ChangelogSource
	// lines are the notes laid out for width cells.
	lines []string
	width int
}

// projectUpdate is the state of the update panel.
type projectUpdate struct {
	items []*updateItem
	// skipped are the chosen projects with nothing newer.
	skipped []string
	cursor  int
	notes   map[string]*updateNotes
	scroll  textScroll
	page    int
	// results is set once the update ran, whether or not a run came of it.
	results []control.ProjectUpdateResult
}

// projectNotesMsg is the outcome of a release notes read.
type projectNotesMsg struct {
	project, key, kind, name string
	res                      control.ChangelogResult
	err                      error
}

func (projectNotesMsg) projectsAnswer() {}

// projectUpdateOpen opens the update panel for the marked projects, or the one under the cursor.
func (m *Model) projectUpdateOpen() tea.Cmd {
	if m.backend == nil {
		return nil
	}
	names := m.prj.markedProjects()
	if len(names) == 0 {
		name, ok := m.selectedProject()
		if !ok {
			return nil
		}
		names = []string{name}
	}
	if len(names) > control.MaxUpdateProjects {
		m.fail = tr(tProjUpdTooMany, control.MaxUpdateProjects)
		return nil
	}
	u := &projectUpdate{notes: map[string]*updateNotes{}}
	for _, name := range names {
		r := m.prj.rows[name]
		if r == nil || r.status == nil {
			m.fail = tr(tProjNeedFile)
			return nil
		}
		found := false
		for _, s := range r.status.Sources {
			if s.Error != "" || len(s.Available) == 0 {
				continue
			}
			found = true
			u.items = append(u.items, &updateItem{project: name, src: s, target: s.Available[len(s.Available)-1]})
		}
		if !found {
			u.skipped = append(u.skipped, name)
		}
	}
	m.clearMessages()
	if len(u.items) == 0 {
		m.status = tr(tProjUpdNone)
		return nil
	}
	m.prj.upd = u
	return m.updateNotesCmd()
}

// updateNotesCmd reads the release notes of the item under the cursor, once for each target.
func (m *Model) updateNotesCmd() tea.Cmd {
	u := m.prj.upd
	if u == nil || len(u.items) == 0 {
		return nil
	}
	it := u.items[u.cursor]
	key := it.key()
	if u.notes[key] != nil {
		return nil
	}
	u.notes[key] = &updateNotes{loading: true}
	b := m.backend
	project, image, to, kind, name := it.project, it.src.Name, it.target, it.src.Kind, it.src.Name
	return func() tea.Msg {
		res, err := b.Changelog(project, image, to)
		return projectNotesMsg{project: project, key: key, kind: kind, name: name, res: res, err: err}
	}
}

func (m *Model) takeProjectNotes(msg projectNotesMsg) {
	u := m.prj.upd
	if u == nil || u.notes[msg.key] == nil {
		return
	}
	e := u.notes[msg.key]
	e.loading, e.lines = false, nil
	if msg.err != nil {
		e.err = projectErrorText(msg.err)
		return
	}
	for i := range msg.res.Sources {
		s := &msg.res.Sources[i]
		if s.Kind == msg.kind && (s.Kind == control.SourceGit || s.Name == msg.name) {
			e.src = s
			return
		}
	}
	e.err = tr(tChgNone)
}

// projectUpdateKey handles a key while the update panel is open.
func (m *Model) projectUpdateKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	u := m.prj.upd
	if u.results != nil {
		switch {
		case keyIs(key, actBack), keyIs(key, actEnter):
			m.prj.upd = nil
			return nil, true
		case keyIs(key, actUp), keyIs(key, actDown), pageKey(key):
			u.scroll.scroll(key, u.page)
			return nil, true
		}
		return nil, false
	}
	move := func(by int) (tea.Cmd, bool) {
		u.cursor = max(min(u.cursor+by, len(u.items)-1), 0)
		u.scroll.reset()
		return m.updateNotesCmd(), true
	}
	switch {
	case keyIs(key, actBack), keyIs(key, actNo):
		m.prj.upd = nil
		return nil, true
	case keyIs(key, actUp):
		return move(-1)
	case keyIs(key, actDown):
		return move(1)
	case pageKey(key):
		u.scroll.scroll(key, u.page)
		return nil, true
	case keyIs(key, actEnter):
		m.pickUpdateVersion()
		return nil, true
	case keyIs(key, actYes):
		return m.projectUpdateStart(), true
	}
	return nil, false
}

// pickUpdateVersion lets the person choose the target of the item under the cursor from its newer versions.
func (m *Model) pickUpdateVersion() {
	u := m.prj.upd
	it := u.items[u.cursor]
	values := newestFirst(it.src.Available)
	m.openProjectPicker(tr(tProjUpdPick, it.label()), tr(tProjUpdPickNote, orDash(it.src.Current)), values,
		slices.Index(values, it.target), func(v string) tea.Cmd {
			if m.prj.upd != u || v == it.target {
				return nil
			}
			it.target = v
			u.scroll.reset()
			return m.updateNotesCmd()
		})
}

// projectUpdateStart starts the update of every item in one request; it needs the admin session.
func (m *Model) projectUpdateStart() tea.Cmd {
	u := m.prj.upd
	var updates []control.ProjectUpdate
	for _, it := range u.items {
		v := control.DeployVersion{Kind: it.src.Kind, Name: it.src.Name, Version: it.target}
		if n := len(updates); n > 0 && updates[n-1].Project == it.project {
			updates[n-1].Versions = append(updates[n-1].Versions, v)
			continue
		}
		updates = append(updates, control.ProjectUpdate{Project: it.project, Versions: []control.DeployVersion{v}})
	}
	var res control.DeployUpdateResult
	return m.requireAdmin(change{
		busy: tr(tBusyUpdate, len(updates)),
		call: func(b Backend) error {
			var err error
			res, err = b.DeployUpdate(updates)
			return err
		},
		done: func(m *Model, err error) tea.Cmd {
			m.busy = ""
			if err != nil {
				m.fail = projectActionError(err)
				return nil
			}
			if m.prj.upd != u {
				m.status = tr(tProjUpdDone, len(res.Results))
			} else {
				u.results = append([]control.ProjectUpdateResult{}, res.Results...)
				u.scroll.reset()
			}
			return m.projectListCmd()
		},
	})
}

// updateResultLine is the outcome of one project of the update, as a line.
func updateResultLine(r control.ProjectUpdateResult) string {
	kind, text := statusFail, ""
	switch {
	case r.Pending:
		kind, text = statusWarn, tr(tProjUpdPending)
	case r.Error != "":
		text = projectActionError(&control.Error{Code: r.Error})
	case r.Run != nil:
		if k, ok := map[string]statusKind{"succeeded": statusOK, "rolled-back": statusWarn}[r.Run.Result]; ok {
			kind = k
		}
		text = tr(tProjUpdRun, wordText(r.Run.Result))
		var changes []string
		for _, c := range r.Run.Changes {
			changes = append(changes, oneLine(sourceLabelOf(c))+" "+oneLine(c.From)+" → "+oneLine(c.To))
		}
		if len(changes) > 0 {
			text += " · " + strings.Join(changes, ", ")
		}
		if r.Run.FailedStep != "" {
			text += " · " + oneLine(r.Run.FailedStep)
		}
	default:
		text = tr(tProjUpdNothing)
	}
	return statusMark(kind, oneLine(r.Project)+": "+text)
}

func sourceLabelOf(c control.DeployChange) string {
	if c.Name == "" {
		return control.SourceGit
	}
	return c.Name
}

// oneLine is text from outside the interface on a single line, with nothing in it that could act on the
// terminal.
func oneLine(s string) string { return strings.Join(strings.Fields(safeText(s)), " ") }

// notesLines lays the release notes out for room cells.
func (m *Model) notesLines(e *updateNotes, room int) []string {
	switch {
	case e == nil || e.loading:
		return []string{styles.hint.Render(tr(tProjUpdLoading))}
	case e.err != "":
		return strings.Split(render(styles.warning, room, e.err), "\n")
	case e.width == room && e.lines != nil:
		return e.lines
	}
	src := e.src
	var out []string
	add := func(style lipgloss.Style, text string) {
		out = append(out, strings.Split(render(style, room, text), "\n")...)
	}
	switch {
	case src.Error == control.ChangelogRateLimited:
		add(styles.warning, tr(tChgRateLimited))
	case src.Error != "":
		add(styles.warning, tr(tChgFailed, sourceErrorText(src.Error)))
	case len(src.Releases) == 0:
		add(styles.hint, tr(tChgNone))
		if src.Link != "" {
			add(styles.hint, oneLine(src.Link))
		}
	default:
		if src.Stale {
			add(styles.hint, tr(tChgStale))
		}
		for i, rel := range src.Releases {
			if i > 0 || src.Stale {
				out = append(out, "")
			}
			head := oneLine(rel.Version)
			if name := oneLine(rel.Name); name != "" && name != head {
				head += "  " + name
			}
			if !rel.Published.IsZero() {
				head += "  " + projectTime(rel.Published, "2006-01-02")
			}
			add(styles.title, head)
			body := strings.TrimSpace(safeText(m.redactor.Apply(rel.Body)))
			if body == "" {
				continue
			}
			for _, line := range strings.Split(body, "\n") {
				out = append(out, strings.Split(wrapCells("  "+strings.TrimRight(line, " "), room), "\n")...)
			}
		}
		if n := src.ReleasesTotal - len(src.Releases); n > 0 {
			out = append(out, "")
			add(styles.hint, tr(tChgMore, n))
		}
	}
	e.lines, e.width = out, room
	return out
}

// projectUpdateView is the update panel, room lines high.
func (m *Model) projectUpdateView(room int) string {
	u := m.prj.upd
	width, framed := m.usable(0), m.tableFramed()
	inner := width
	if framed {
		inner -= 4
	}
	rows := max(room-projectChrome(m, framed), 1)
	projects := map[string]bool{}
	for _, it := range u.items {
		projects[it.project] = true
	}
	title := tr(tProjUpdTitle, len(projects))
	if u.results != nil {
		lines := make([]string, len(u.results))
		for i, r := range u.results {
			lines[i] = truncateCells(updateResultLine(r), inner)
		}
		u.page = rows
		return strings.Join(m.projectFrame(tr(tProjUpdResults), u.scroll.position(len(lines), u.page),
			u.scroll.window(lines, u.page, false), width, rows, true, framed), "\n")
	}
	var body []string
	itemRows := min(len(u.items), max((rows-3)/2, 1))
	start := min(max(u.cursor-itemRows/2, 0), len(u.items)-itemRows)
	before := 1
	if framed {
		before = m.titleRows() + 1
	}
	labelWidth := 0
	for _, it := range u.items {
		labelWidth = max(labelWidth, lipgloss.Width(safeText(it.label())))
	}
	labelWidth = min(labelWidth, max(inner/2, 1))
	for i := start; i < start+itemRows; i++ {
		it := u.items[i]
		label := padLine(truncateCells(safeText(it.label()), labelWidth), labelWidth)
		text := truncateCells(fmt.Sprintf("%s  %s → %s ▾", label, safeText(orDash(it.src.Current)), safeText(it.target)), max(inner-2, 1))
		top := m.workspaceTop() + 2 + before + len(body)
		body = append(body, m.listRowIn(inner, i == u.cursor, text))
		m.addHit(0, top, width, top+1, func() tea.Cmd {
			if m.prj.upd == u && i < len(u.items) && u.results == nil {
				u.cursor = i
				u.scroll.reset()
				return m.updateNotesCmd()
			}
			return nil
		})
	}
	if len(u.skipped) > 0 {
		body = append(body, styles.hint.Render(truncateCells(tr(tProjUpdSkipped, strings.Join(u.skipped, ", ")), inner)))
	}
	it := u.items[u.cursor]
	body = append(body, styles.title.Render(truncateCells(tr(tProjUpdNotes, it.label(), orDash(it.src.Current), it.target), inner)))
	lines := m.notesLines(u.notes[it.key()], inner)
	u.page = max(rows-len(body), 1)
	body = append(body, u.scroll.window(lines, u.page, false)...)
	return strings.Join(m.projectFrame(title, u.scroll.position(len(lines), u.page), body, width, rows, true, framed), "\n")
}
