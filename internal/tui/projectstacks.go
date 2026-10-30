package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// Adopting starts from the stacks that run on the host. The list is a picker over the answer of the
// service; the last row leads to the panel for a directory, whatever the list holds.

// stackRowWidth is the cells a row of the list may take.
const stackRowWidth = 56

// projectStacksMsg is the answer to the request for the running stacks.
type projectStacksMsg struct {
	res control.StacksResult
	err error
}

func (projectStacksMsg) projectsAnswer() {}

// projectAdoptStacks asks the service for the running stacks and shows them to choose one from.
func (m *Model) projectAdoptStacks() tea.Cmd {
	m.clearMessages()
	m.busy = tr(tBusyStacks)
	b := m.backend
	return func() tea.Msg {
		res, err := b.Stacks()
		return projectStacksMsg{res: res, err: err}
	}
}

func (m *Model) takeProjectStacks(msg projectStacksMsg) {
	m.busy = ""
	if m.screen != screenArea || m.area != areaProjects {
		return
	}
	stacks := msg.res.Stacks
	if msg.err != nil {
		stacks = nil
	}
	values := make([]string, 0, len(stacks)+1)
	for i := range stacks {
		values = append(values, strconv.Itoa(i))
	}
	other := strconv.Itoa(len(stacks))
	values = append(values, other)

	pk := &projectPicker{title: tr(tStackTitle), note: tr(tStackNote), width: stackRowWidth,
		list: newFilterList(func(v string) string { return v })}
	pk.display = func(v string) string {
		i, err := strconv.Atoi(v)
		if err != nil || i < 0 || i >= len(stacks) {
			return tr(tStackOther)
		}
		return stackRow(stacks[i])
	}
	pk.list.text = pk.display
	// The way to a typed directory stays in view whatever the filter says.
	pk.list.match = func(v, query string) bool {
		return v == other || strings.Contains(strings.ToLower(pk.display(v)), query)
	}
	pk.pick = func(v string) tea.Cmd {
		i, err := strconv.Atoi(v)
		if err != nil || i < 0 || i >= len(stacks) {
			m.openAdoptPrompt()
			return nil
		}
		return m.adoptStack(stacks[i])
	}
	switch {
	case msg.err != nil:
		pk.lead = stackNotice(styles.warning, stackFailure(msg.err))
	case len(stacks) == 0:
		pk.lead = stackNotice(styles.hint, tr(tStackEmpty))
	case msg.res.Truncated:
		pk.lead = stackNotice(styles.warning, tr(tStackTruncated))
	}
	m.showProjectPicker(pk, values, 0)
}

// stackNotice is a message in front of the rows, with the room the rows have.
func stackNotice(style lipgloss.Style, text string) []string {
	return wrapIndented(style, "    ", text, stackRowWidth)
}

// stackFailure is the text of a refused request for the stacks, with the way on.
func stackFailure(err error) string {
	switch {
	case isCode(err, control.CodeDockerUnavailable):
		return tr(tStackFailed, tr(tPrjNoDocker))
	case isCode(err, control.CodeTimeout):
		return tr(tStackFailed, tr(tStackTimeout))
	}
	return tr(tStackFailed, errorText(err))
}

// stackRow is the line of a stack: the Compose project, the directory with its end kept, the number of
// containers and the project that manages it. What the host reports is not trusted.
func stackRow(s control.Stack) string {
	name := truncateCells(oneLine(s.ComposeProject), 12)
	tail := tr(tStackContainers, s.Containers)
	if s.ManagedBy != "" {
		tail += "  " + tr(tStackManaged, truncateCells(oneLine(s.ManagedBy), 12))
	}
	room := stackRowWidth - lipgloss.Width(name) - lipgloss.Width(tail) - 4
	return name + "  " + truncateLeft(oneLine(s.Dir), max(room, 4)) + "  " + tail
}

// adoptStack asks the service for the proposal for the chosen stack.
func (m *Model) adoptStack(s control.Stack) tea.Cmd {
	params := control.AdoptParams{Dir: s.Dir, DryRun: true}
	if config.ValidName(s.ComposeProject) {
		params.Name = s.ComposeProject
	}
	m.clearMessages()
	m.prj.stackSeq++
	m.busy = tr(tBusyAdopt)
	seq, b := m.prj.stackSeq, m.backend
	return func() tea.Msg {
		res, err := b.AdoptProject(params)
		return projectAdoptMsg{seq: seq, list: true, res: res, err: err}
	}
}
