package tui

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The guided setup of a project leads through its steps in place of the list: the name and the scenario, the
// source, the target on the host, the project's own steps for the custom scenario, the version rules, the
// backup and the checks, the notifications, and a summary. Nothing is saved before the summary: F2 goes on
// from step to step and saves there, through requireAdmin, as one complete project file, the request
// `sysmin project add NAME --file FILE` sends. enter goes to the next row and never saves; esc goes back a
// step and, from the first one, leaves the setup, asking first about unsaved input. The service validates;
// each problem it names stands under the row of its field, and those no row shows stand in the summary.

// projectFlow is the state of the guided setup.
type projectFlow struct {
	draft flowDraft
	// initial is the signature of the draft the setup started with.
	initial string
	// revision is set when the setup changes a stored project; it is the revision the change is based on.
	revision string
	step     flowStepID
	// focus is the id of the focused row; in holds the text of a focused text or path row.
	focus string
	in    projectPathInput
	// open says which steps show their advanced rows because the person opened them.
	open map[flowStepID]bool
	// problems are the findings of the last check by the row they belong to; other are the findings no row
	// shows.
	problems map[string][]control.Problem
	other    []control.Problem
	// scroll is the first line of the step drawn last.
	scroll int
	// channels are the notification channels of the service; channelsKnown says the list was read. Until it
	// is, and when it cannot be, the channels are typed.
	channels      []string
	channelsKnown bool
}

// newProjectFlow is the setup over d, on its first step. A setup that changes a stored project names its
// revision.
func newProjectFlow(d flowDraft, revision string) *projectFlow {
	f := &projectFlow{draft: d, revision: revision, in: newProjectPathInput(), open: map[flowStepID]bool{},
		problems: map[string][]control.Problem{}}
	f.initial = f.draft.signature()
	f.show(flowStart)
	return f
}

// flowOpen reports whether the guided setup is on screen.
func (m *Model) flowOpen() bool {
	return m.area == areaProjects && m.prj != nil && m.prj.flow != nil
}

// flowTyping reports whether the focused row of the setup takes typed text.
func (m *Model) flowTyping() bool {
	if !m.flowOpen() {
		return false
	}
	r, ok := m.prj.flow.focused()
	return ok && r.typed()
}

// flowDirty reports whether the setup holds input that was not saved.
func (m *Model) flowDirty() bool {
	return m.flowOpen() && m.prj.flow.draft.signature() != m.prj.flow.initial
}

// projectFlowOpen opens the guided setup over d; revision names the stored project it changes, empty for a
// new one. From another area it goes to Projects first, which asks about unsaved input there.
func (m *Model) projectFlowOpen(d flowDraft, revision string) tea.Cmd {
	return m.projectFlowThen(func() tea.Cmd { return m.projectFlowShow(d, revision) })
}

// projectFlowShow puts a setup over d on screen in place of what the area shows, and asks for the lists it
// offers.
func (m *Model) projectFlowShow(d flowDraft, revision string) tea.Cmd {
	m.clearMessages()
	f := newProjectFlow(d, revision)
	m.prj.flow = f
	return m.flowLoadCmd(f)
}

// projectFlowThen runs open in the Projects area. From another area it goes there first, and an open setup
// that holds unsaved input is asked about before open drops it.
func (m *Model) projectFlowThen(open func() tea.Cmd) tea.Cmd {
	if m.backend == nil || m.prj == nil {
		return nil
	}
	if m.area != areaProjects {
		cmd := m.setArea(areaProjects)
		if m.area != areaProjects {
			return cmd
		}
		return tea.Batch(cmd, open())
	}
	// A setup that is open already holds input that a new one would drop.
	return m.guarded(open)
}

// closeFlow leaves the setup and drops what it holds.
func (m *Model) closeFlow() tea.Cmd {
	if m.prj != nil {
		m.prj.flow = nil
	}
	return nil
}

// show opens step s with the focus on its first row.
func (f *projectFlow) show(s flowStepID) {
	f.tidy()
	f.step, f.scroll = s, 0
	f.focus = ""
	if rows := f.shownRows(s); len(rows) > 0 {
		f.focusOn(rows[0].id)
		return
	}
	f.in.close()
}

// focused is the focused row of the step shown.
func (f *projectFlow) focused() (flowRow, bool) {
	for _, r := range f.shownRows(f.step) {
		if r.id == f.focus {
			return r, true
		}
	}
	return flowRow{}, false
}

// focusOn moves the focus to the row id; a text row takes the typing from there.
func (f *projectFlow) focusOn(id string) {
	f.focus = id
	r, ok := f.focused()
	if !ok || !r.typed() {
		f.in.close()
		return
	}
	start := ""
	if r.start != nil {
		start = r.start()
	}
	mode := projectPathPlain
	if r.kind == flowPath {
		mode = r.mode
	}
	f.in.open(r.get(), mode, start)
}

// tidy drops the compose files left empty, so the rows count as the file lists them. It runs when a step
// opens and before a check, never between two rows, where it would move the rows under the focus.
func (f *projectFlow) tidy() {
	d := &f.draft
	d.composeFiles = slices.DeleteFunc(d.composeFiles, func(s string) bool { return strings.TrimSpace(s) == "" })
}

// moveFocus moves the focus by rows within the step; it stays at either end.
func (f *projectFlow) moveFocus(by int) {
	rows := f.shownRows(f.step)
	if len(rows) == 0 {
		return
	}
	at := slices.IndexFunc(rows, func(r flowRow) bool { return r.id == f.focus })
	next := min(max(at+by, 0), len(rows)-1)
	if at < 0 {
		next = 0
	}
	f.focusOn(rows[next].id)
}

// stepIndex is the position of the step shown among the steps.
func (f *projectFlow) stepIndex() int {
	return max(slices.Index(f.stepOrder(), f.step), 0)
}

// flowKey handles a key of the setup: every key but help, the command line and the info reaches it.
func (m *Model) flowKey(key tea.KeyPressMsg) tea.Cmd {
	f := m.prj.flow
	r, has := f.focused()
	switch {
	case keyIs(key, actSave):
		return m.flowNext()
	case keyIs(key, actBack):
		if i := f.stepIndex(); i > 0 {
			m.clearMessages()
			f.show(f.stepOrder()[i-1])
			return nil
		}
		return m.guarded(m.closeFlow)
	case f.step == flowSummary && (keyIs(key, actUp) || keyIs(key, actDown)):
		if keyIs(key, actUp) {
			f.scroll--
		} else {
			f.scroll++
		}
		return nil
	case keyIs(key, actRemoveField) && has && r.remove != nil:
		f.removeRow(r)
		return nil
	case keyIs(key, actUp), keyIs(key, actPrev):
		f.moveFocus(-1)
		return nil
	case keyIs(key, actDown):
		f.moveFocus(1)
		return nil
	case keyIs(key, actPageUp), keyIs(key, actPageDown),
		!(has && r.typed()) && (keyIs(key, actHome) || keyIs(key, actEnd)):
		// Home and end move the cursor of a text row; elsewhere they go to the first or the last row.
		rows := f.shownRows(f.step)
		if len(rows) > 0 && (keyIs(key, actHome) || keyIs(key, actPageUp)) {
			f.focusOn(rows[0].id)
		} else if len(rows) > 0 {
			f.focusOn(rows[len(rows)-1].id)
		}
		return nil
	}
	if !has {
		return nil
	}
	if r.typed() {
		return m.flowTypedKey(r, key)
	}
	switch {
	case keyIs(key, actNext):
		f.moveFocus(1)
	case keyIs(key, actEnter), keyIs(key, actSpace) && r.kind != flowChoice:
		switch r.kind {
		case flowChoice:
			m.flowChoose(r)
		case flowPick:
			return m.flowPickOpen(r)
		case flowLine:
		case flowAction:
			before := f.shownRows(f.step)
			r.act()
			// The focus goes to the first row the action added.
			for _, n := range f.shownRows(f.step) {
				if !slices.ContainsFunc(before, func(o flowRow) bool { return o.id == n.id }) && n.kind != flowFold {
					f.focusOn(n.id)
					break
				}
			}
		default:
			if r.act != nil {
				r.act()
			}
		}
	case keyIs(key, actSpace), keyIs(key, actRight):
		if r.kind == flowChoice {
			m.flowCycle(r, 1)
		}
	case keyIs(key, actLeft):
		if r.kind == flowChoice {
			m.flowCycle(r, -1)
		}
	}
	return nil
}

// removeRow deletes the entry of row r, such as an image or a compose file; the focus keeps its place.
func (f *projectFlow) removeRow(r flowRow) {
	at := slices.IndexFunc(f.shownRows(f.step), func(o flowRow) bool { return o.id == r.id })
	r.remove()
	// The entries after it move up, so the problems found for them no longer match their rows.
	f.problems = map[string][]control.Problem{}
	f.focus = ""
	if rows := f.shownRows(f.step); len(rows) > 0 {
		f.focusOn(rows[min(max(at, 0), len(rows)-1)].id)
	}
}

// flowTypedKey handles a key of a text or path row. tab completes a path before it goes on; enter takes a
// suggestion marked with ctrl+n or ctrl+p before it goes on.
func (m *Model) flowTypedKey(r flowRow, key tea.KeyPressMsg) tea.Cmd {
	f := m.prj.flow
	in := &f.in
	switch {
	case keyIs(key, actNext):
		if !in.complete() {
			f.moveFocus(1)
			return nil
		}
	case keyIs(key, actEnter):
		if !in.marked || !in.complete() {
			f.moveFocus(1)
			return nil
		}
	case keyIs(key, actSuggestNext):
		in.move(1)
		return nil
	case keyIs(key, actSuggestPrev):
		in.move(-1)
		return nil
	default:
		cmd, changed := in.edit(key)
		if changed {
			m.flowSet(r, in.Value())
		}
		return cmd
	}
	m.flowSet(r, in.Value())
	return nil
}

// flowSet gives row r a new value; a problem reported for the row is answered by the change.
func (m *Model) flowSet(r flowRow, v string) {
	f := m.prj.flow
	r.set(v)
	delete(f.problems, r.id)
}

// flowChoose opens the list of a choice row.
func (m *Model) flowChoose(r flowRow) {
	values := r.choices
	current := r.get()
	if !slices.Contains(values, current) {
		values = append([]string{current}, values...)
	}
	shown := make([]string, len(values))
	for i, v := range values {
		shown[i] = r.show(v)
	}
	m.openDropdown(r.id, shown, slices.Index(values, current), func(i int) tea.Cmd {
		if m.flowOpen() {
			m.flowSet(r, values[i])
		}
		return nil
	})
}

// flowCycle steps a choice row to its next or previous value.
func (m *Model) flowCycle(r flowRow, by int) {
	at := slices.Index(r.choices, r.get())
	m.flowSet(r, r.choices[wrap(at+by, len(r.choices))])
}

// flowNext is F2: on a step it goes on to the next one once the plain problems of the step are dealt with; on
// the summary it saves.
func (m *Model) flowNext() tea.Cmd {
	f := m.prj.flow
	m.clearMessages()
	if f.step == flowSummary {
		if m.busy != "" {
			// A save is under way; a second one would only meet the first.
			return nil
		}
		return m.flowSave()
	}
	f.tidy()
	_, notes := f.draft.project()
	// Only the rows of this step are checked here; what the service found on other steps stays.
	here := map[string]bool{}
	for _, r := range f.rows(f.step) {
		here[r.id] = true
		delete(f.problems, r.id)
	}
	f.place(notes, here)
	if id := f.firstProblem(f.step); id != "" {
		f.focusOn(id)
		m.fail = tr(tFlowFix)
		return nil
	}
	steps := f.stepOrder()
	f.show(steps[min(f.stepIndex()+1, len(steps)-1)])
	return nil
}

// flowSave sends the project to the service, as one file. The plain problems are found first, here; the
// service finds the rest.
func (m *Model) flowSave() tea.Cmd {
	f := m.prj.flow
	f.tidy()
	p, notes := f.draft.project()
	f.problems, f.other = map[string][]control.Problem{}, nil
	if len(notes) > 0 {
		f.place(notes, nil)
		m.flowShowProblems()
		m.fail = tr(tFlowFix)
		return nil
	}
	data, err := config.EncodeProject(p)
	if err != nil {
		m.fail = tr(tErrService)
		return nil
	}
	params := control.ProjectParams{Name: p.Name, Revision: f.revision, File: string(data)}
	var res control.ChangeResult
	return m.requireAdmin(change{
		busy: tr(tBusyFlowSave, p.Name),
		call: func(b Backend) (err error) {
			if params.Revision != "" {
				res, err = b.EditProject(params)
			} else {
				res, err = b.AddProject(params)
			}
			return err
		},
		done: func(m *Model, err error) tea.Cmd {
			m.busy = ""
			current := m.flowOpen() && m.prj.flow == f
			var ce *control.Error
			switch {
			case err == nil:
				if current {
					m.prj.flow = nil
				}
				m.status = tr(tFlowSavedPending, p.Name)
				if res.State == control.StateApproved {
					m.status = tr(tFlowSaved, p.Name)
				}
				if len(res.NotWritable) > 0 {
					m.status = tr(tFlowNotWritable, p.Name, pathsText(res.NotWritable))
				}
				return m.projectListCmd()
			case !current:
				m.fail = projectErrorText(err)
			case errors.As(err, &ce) && ce.Code == control.CodeInvalid && len(ce.Problems) > 0:
				f.place(ce.Problems, nil)
				m.flowShowProblems()
				m.fail = tr(tFlowRefused, len(ce.Problems))
			case isCode(err, control.CodeExists):
				f.place([]control.Problem{flowNote("name", tFlowExists)}, nil)
				m.flowShowProblems()
				m.fail = tr(tFlowExists)
			default:
				m.fail = projectErrorText(err)
			}
			return nil
		},
	})
}

// flowShowProblems goes to the first row with a problem; with problems no row shows, the summary stays.
func (m *Model) flowShowProblems() {
	f := m.prj.flow
	for _, s := range f.stepOrder() {
		if id := f.firstProblem(s); id != "" {
			f.show(s)
			f.focusOn(id)
			return
		}
	}
	f.show(flowSummary)
}

// firstProblem is the first row of step s that has a problem, empty when there is none. The advanced rows
// open for it.
func (f *projectFlow) firstProblem(s flowStepID) string {
	for _, r := range f.shownRows(s) {
		if len(f.problems[r.id]) > 0 {
			return r.id
		}
	}
	return ""
}

// stepHasProblem reports whether a row of step s, or for the summary the setup at all, has a problem.
func (f *projectFlow) stepHasProblem(s flowStepID) bool {
	if s == flowSummary {
		return len(f.other) > 0
	}
	for _, r := range f.rows(s) {
		if len(f.problems[r.id]) > 0 {
			return true
		}
	}
	return false
}

// place files each problem under the row of its field. The field of a problem is the path of a value in the
// project file, such as images[0].services[1]; a row stands for its own path and its aliases, and a path no
// row names is cut back to the field it lies in until one does. A problem of the steps a template brings
// names the template's step, so it is placed by the field it is about instead. With only set, a problem of a
// row outside it is dropped.
func (f *projectFlow) place(problems []control.Problem, only map[string]bool) {
	var all []flowRow
	for _, s := range f.stepOrder() {
		all = append(all, f.rows(s)...)
	}
	find := func(field string) string {
		for field != "" {
			for _, r := range all {
				if r.id == field || slices.Contains(r.aliases, field) {
					return r.id
				}
			}
			switch i := strings.LastIndexAny(field, ".["); {
			case i < 0:
				field = ""
			default:
				field = field[:i]
			}
		}
		return ""
	}
	for _, p := range problems {
		field := p.Field
		switch p.Code {
		case config.CodeMissingEnvFile:
			field = "env_file"
		case config.CodeMissingEnvVar:
			field = "images[0].env_var"
			for i, im := range f.draft.images {
				if strings.TrimSpace(im.envVar) == "" {
					field = "images[" + strconv.Itoa(i) + "].env_var"
					break
				}
			}
		}
		id := find(field)
		switch {
		case only != nil && !only[id]:
		case id != "":
			f.problems[id] = append(f.problems[id], p)
		default:
			f.other = append(f.other, p)
		}
	}
}

// flowProblemText is a problem as the line under its row says it: the cause and the way out. Both come from
// the validation and name values the person typed, never a secret.
func flowProblemText(p control.Problem) string {
	text := oneLine(p.Message)
	if p.Hint != "" {
		text += " " + oneLine(p.Hint)
	}
	return text
}
