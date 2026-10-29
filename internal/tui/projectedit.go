package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// Editing a stored project and starting a new one from it both read the project file as it is stored now and
// open the guided setup over it. An edit carries the revision it read, so a project changed meanwhile is
// refused; a copy is a new project with a new name and carries none. A credential comes along as its name.

// projectFileMsg is the answer to the read of a stored project.
type projectFileMsg struct {
	name   string
	copied bool
	detail control.ProjectDetail
	err    error
}

func (projectFileMsg) projectsMsg() {}

// projectRead reads the project under the cursor for an edit, or with copied for a new project made from it.
func (m *Model) projectRead(copied bool) tea.Cmd {
	if m.area != areaProjects || m.backend == nil || m.prj == nil || m.prj.flow != nil {
		return nil
	}
	name, ok := m.selectedProject()
	if !ok {
		return nil
	}
	m.clearMessages()
	m.busy = tr(tBusyProjectGet, name)
	b := m.backend
	return func() tea.Msg {
		d, err := b.Project(name)
		return projectFileMsg{name: name, copied: copied, detail: d, err: err}
	}
}

func (m *Model) takeProjectFile(msg projectFileMsg) tea.Cmd {
	m.busy = ""
	if msg.err != nil {
		m.fail = projectErrorText(msg.err)
		return nil
	}
	if m.screen != screenArea || m.area != areaProjects || m.prj.flow != nil {
		return nil
	}
	p, err := config.DecodeProject([]byte(msg.detail.File))
	if err != nil {
		m.fail = tr(tProjFileBad)
		return nil
	}
	if !msg.copied {
		return m.projectFlowOpen(flowDraftOf(p), msg.detail.Revision)
	}
	p.Name = m.projectCopyName(p.Name)
	return m.projectFlowOpen(flowDraftOf(p), "")
}

// projectCopyName is a name for a copy of the project name that no listed project has.
func (m *Model) projectCopyName(name string) string {
	taken := func(n string) bool { return m.prj.rows[n] != nil }
	candidate := name + "-copy"
	for i := 2; taken(candidate); i++ {
		candidate = fmt.Sprintf("%s-copy-%d", name, i)
	}
	return candidate
}
