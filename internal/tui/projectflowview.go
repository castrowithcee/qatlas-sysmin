package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// flowListMinWidth is the narrowest workspace that carries the numbered list of the steps beside the rows.
// Below it the title names the step.
const flowListMinWidth = 100

// flowView is the guided setup, room lines high, in place of the list.
func (m *Model) flowView(room int) string {
	f := m.prj.flow
	width := m.usable(0)
	steps := f.stepOrder()
	at := f.stepIndex()
	m.screenPos = fmt.Sprintf("%d/%d", at+1, len(steps))
	title := tr(tFlowTitleNew)
	if f.revision != "" {
		title = tr(tFlowTitleEdit)
	}
	head := []string{titleLine(title+" › "+flowStepTitle(f.step), "", width)}
	if m.titleBlank() {
		head = append(head, "")
	}
	avail := max(room-len(head), 3)
	top := m.workspaceTop() + len(head)
	var out []string
	switch framed := m.tableFramed(); {
	case framed && width >= flowListMinWidth && avail >= len(steps)+2:
		list, listW := m.flowStepList(steps, at)
		inner := width - listW - 1 - 4
		right := panel("", m.flowBody(inner, avail-2, top+1, listW+1+2), inner, avail-2, panelOpts{focused: true})
		for i := 0; i < avail; i++ {
			left := ""
			if i < len(list) {
				left = list[i]
			}
			out = append(out, padLine(left, listW)+" "+right[i])
		}
	case framed:
		inner := width - 4
		out = panel("", m.flowBody(inner, avail-2, top+1, 2), inner, avail-2, panelOpts{focused: true})
	default:
		out = m.flowBody(width, avail, top, 0)
		for len(out) < avail {
			out = append(out, "")
		}
	}
	return strings.Join(append(head, out...), "\n")
}

// flowStepList draws the numbered steps in a panel and returns its lines and width; the step shown leads with
// ">" on a filled bar, and a step with a problem ends in the alert mark.
func (m *Model) flowStepList(steps []flowStepID, current int) ([]string, int) {
	f := m.prj.flow
	texts := make([]string, len(steps))
	inner := 0
	for i, s := range steps {
		texts[i] = fmt.Sprintf("%d %s", i+1, flowStepTitle(s))
		if f.stepHasProblem(s) {
			texts[i] += flowAlertMark
		}
		inner = max(inner, 2+lipgloss.Width(texts[i]))
	}
	lines := make([]string, len(steps))
	for i := range steps {
		if i == current {
			lines[i] = styles.activeRow.Render(padLine("> "+texts[i], inner))
		} else {
			lines[i] = padLine("  "+texts[i], inner)
		}
	}
	return panel("", lines, inner, len(lines), panelOpts{}), inner + 4
}

// flowAlertMark ends the entry of a step that holds a problem.
const flowAlertMark = " [!]"

// flowBody is the content of the step shown: rows lines of inner cells, scrolled so the focused row and what
// stands under it are in view. top and left are the screen cell of its first line, for the list of a choice.
func (m *Model) flowBody(inner, rows, top, left int) []string {
	f := m.prj.flow
	m.addWheel(left, top, left+inner, top+rows, m.wheelFlow)
	var lines []string
	from, to := 0, 0
	if f.step == flowSummary {
		lines = m.flowSummaryLines(inner)
		f.scroll = min(max(f.scroll, 0), max(len(lines)-rows, 0))
		return lines[f.scroll:min(len(lines), f.scroll+rows)]
	}
	shown := f.shownRows(f.step)
	lw := 0
	for _, r := range shown {
		if r.kind != flowToggle && r.kind != flowAction && r.kind != flowFold && r.kind != flowLine {
			lw = max(lw, lipgloss.Width(r.label))
		}
	}
	lw = min(lw, max(inner/3, 4))
	heading := ""
	choiceAt := -1
	type rowAt struct {
		line int
		id   string
	}
	var at []rowAt
	for _, r := range shown {
		if r.heading != "" && r.heading != heading {
			lines = append(lines, styles.title.Render(truncateCells(safeText(r.heading), inner)))
		}
		heading = r.heading
		focused := r.id == f.focus
		if focused {
			from = len(lines)
			if r.kind == flowChoice {
				choiceAt = len(lines)
			}
		}
		at = append(at, rowAt{len(lines), r.id})
		lines = append(lines, m.flowRowLine(r, focused, lw, inner))
		if focused && r.kind == flowPath {
			suggestions, mark := f.in.shown()
			for i, s := range suggestions {
				text := truncateCells("    "+s, inner)
				if f.in.marked && i == mark {
					lines = append(lines, styles.title.Render(truncateCells("  > "+s, inner)))
				} else {
					lines = append(lines, styles.hint.Render(text))
				}
			}
		}
		for _, p := range f.problems[r.id] {
			for _, l := range strings.Split(render(styles.fail, max(inner-4, 1), warnMark+" "+flowProblemText(p)), "\n") {
				lines = append(lines, "    "+l)
			}
		}
		if focused {
			to = len(lines) - 1
		}
	}
	if len(lines) <= rows {
		f.scroll = 0
	} else {
		if from < f.scroll {
			f.scroll = from
		}
		if to >= f.scroll+rows {
			f.scroll = to - rows + 1
		}
		f.scroll = min(max(f.scroll, 0), len(lines)-rows)
		lines = lines[f.scroll : f.scroll+rows]
	}
	if choiceAt >= 0 {
		m.noteField(f.focus, left+2, top+choiceAt-f.scroll)
	}
	for _, a := range at {
		if y := a.line - f.scroll; y >= 0 && y < len(lines) {
			id := a.id
			m.addHit(left, top+y, left+inner, top+y+1, func() tea.Cmd { return m.clickFlowRow(id) })
		}
	}
	return lines
}

// clickFlowRow is a click on a row of the setup: it focuses the row, and a click on the focused row does what
// enter does there.
func (m *Model) clickFlowRow(id string) tea.Cmd {
	if !m.flowOpen() || m.screen != screenArea {
		return nil
	}
	f := m.prj.flow
	if f.focus != id {
		f.focusOn(id)
		return nil
	}
	if r, ok := f.focused(); ok && r.typed() {
		return nil
	}
	return m.pressName(keyOf(actEnter))
}

// flowRowLine is one row: the label on the field surface and the value, which is the text line itself while
// the row has the focus. A choice ends in the arrow of its list.
func (m *Model) flowRowLine(r flowRow, focused bool, lw, inner int) string {
	f := m.prj.flow
	room := max(inner-2, 1)
	marker := barMarker(focused)
	style := fieldStyle(focused)
	switch r.kind {
	case flowToggle, flowFold:
		box := "[ ]"
		if r.on() {
			box = "[x]"
		}
		if r.kind == flowFold {
			box = "[+]"
			if r.on() {
				box = "[-]"
			}
		}
		return marker + style.Render(truncateCells(" "+box+" "+r.label+" ", room))
	case flowAction:
		return marker + style.Render(truncateCells(" + "+r.label+" ", room))
	case flowLine:
		return marker + styles.hint.Render(truncateCells(" "+r.label, room))
	}
	head := style.Render(" " + padLine(truncateCells(r.label, lw)+":", lw+1) + " ")
	valueRoom := max(room-lipgloss.Width(head), 1)
	if focused && r.typed() {
		return marker + head + inputView(fitInput(f.in.Model, valueRoom))
	}
	value := oneLine(r.value())
	if r.kind == flowChoice || r.kind == flowPick {
		return marker + head + truncateCells(value, max(valueRoom-2, 1)) + " " + dropdownArrow
	}
	if value == "" {
		return marker + head + styles.hint.Render("-")
	}
	return marker + head + truncateCells(value, valueRoom)
}

// flowSummaryLines is the summary: what the project will be, the steps that still hold a problem, and the
// problems no row shows.
func (m *Model) flowSummaryLines(inner int) []string {
	f := m.prj.flow
	p, _ := f.draft.project()
	scenario := p.Template
	if scenario == "" {
		scenario = wordText(flowCustom) + fmt.Sprintf(" (%d)", len(p.Steps))
	}
	pairs := [][2]string{
		{tr(tFlowLblName), p.Name},
		{tr(tFlowLblScenario), scenario},
	}
	for _, im := range p.Images {
		text := im.Ref
		if im.Credential != "" {
			text += " · " + im.Credential
		}
		pairs = append(pairs, [2]string{tr(tFlowSumImage, im.Name), text + " · " + wordText(modeOf(im.VersionRule.Mode))})
	}
	if g := p.Git; g != nil {
		text := g.URL
		if g.Credential != "" {
			text += " · " + g.Credential
		}
		pairs = append(pairs, [2]string{control.SourceGit, text + " · " + wordText(modeOf(g.VersionRule.Mode))})
	}
	pairs = append(pairs,
		[2]string{tr(tFlowLblDir), p.Dir},
		[2]string{tr(tFlowLblEnv), p.EnvFile},
		[2]string{tr(tFlowSumCompose), strings.Join(p.ComposeFiles, ", ")},
		[2]string{tr(tFlowLblBackup), flowBackupText(p.Backup)},
		[2]string{tr(tFlowStepChecks), flowChecksText(p.Checks)},
	)
	if n := p.Notify; n != nil {
		pairs = append(pairs, [2]string{tr(tFlowStepNotify), strings.Join(n.Channels, ", ") + " · " + strings.Join(n.Events, ", ")})
	}
	if w := p.Webhook; w != nil {
		pairs = append(pairs, [2]string{tr(tFlowLblHookKind), w.Kind + " · " + w.Secret})
	}
	lines := kvLines(inner, pairs)
	var marked []string
	for _, s := range f.stepOrder() {
		if s != flowSummary && f.stepHasProblem(s) {
			marked = append(marked, flowStepTitle(s))
		}
	}
	if len(marked) > 0 {
		lines = append(lines, "")
		lines = append(lines, strings.Split(render(styles.fail, inner, warnMark+" "+tr(tFlowProblemsIn, strings.Join(marked, ", "))), "\n")...)
	}
	if len(f.other) > 0 {
		lines = append(lines, "", styles.title.Render(tr(tFlowProblems)))
		for _, pr := range f.other {
			lines = append(lines, strings.Split(render(styles.fail, inner, warnMark+" "+flowProblemText(pr)), "\n")...)
		}
	}
	return lines
}

func flowBackupText(b *config.Step) string {
	switch {
	case b == nil:
		return wordText(flowNone)
	case b.Volume != "":
		return wordText(flowVolume) + " " + b.Volume
	}
	return wordText(flowCommand)
}

func flowChecksText(checks []config.Step) string {
	var parts []string
	for _, c := range checks {
		text := string(c.Type)
		if c.URL != "" {
			text += " " + c.URL
		} else if len(c.Services) > 0 {
			text += " " + strings.Join(c.Services, ", ")
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "; ")
}

// flowKeyLine is the key line of the setup: the keys of the focused row, then the way on and back.
func (m *Model) flowKeyLine() string {
	f := m.prj.flow
	var keys []string
	r, has := f.focused()
	switch {
	case f.step == flowSummary:
		keys = append(keys, "↑/↓ scroll")
	case !has:
	case r.typed():
		if r.kind == flowPath && len(f.in.matches) > 0 {
			keys = append(keys, "tab complete", "ctrl+n/ctrl+p suggestion")
		}
		keys = append(keys, "enter next")
	case r.kind == flowChoice:
		keys = append(keys, "enter choose", "←/→ switch")
	case r.kind == flowPick:
		keys = append(keys, "enter pick")
	case r.kind == flowToggle:
		keys = append(keys, "space toggle")
	case r.kind == flowAction:
		keys = append(keys, "enter add")
	case r.kind == flowFold && r.on():
		keys = append(keys, "enter close")
	case r.kind == flowFold:
		keys = append(keys, "enter open")
	}
	if has && r.remove != nil {
		keys = append(keys, "{remove} remove")
	}
	if f.step == flowSummary {
		keys = append(keys, "{save} save")
	} else {
		keys = append(keys, "{save} next")
	}
	keys = append(keys, "{back} back")
	if has {
		keys = append(keys, "↑/↓ field")
	}
	keys = append(keys, m.lockKeys()...)
	if !m.flowTyping() {
		keys = append(keys, "{info} info", "{help} help", "{command} cmds")
	}
	return strings.Join(keys, " · ")
}

// flowInfo is the title and the text of the info on the focused row of the setup.
func (m *Model) flowInfo() (string, string) {
	f := m.prj.flow
	if r, ok := f.focused(); ok && r.info != "" {
		return r.label, tr(r.info)
	}
	return flowStepTitle(f.step), tr(tInfoFlowSummary)
}
