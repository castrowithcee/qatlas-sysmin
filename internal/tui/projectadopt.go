package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// Adopting a running stack asks the service for a proposal and nothing else: the request always carries
// DryRun, so no project is stored from here. The proposal is shown, and taking it over fills in the guided
// setup, which saves only from its summary like every project.

// projectFlowStart asks how a project begins: new, or taken over from a stack that runs already.
func (m *Model) projectFlowStart() tea.Cmd {
	return m.projectFlowThen(func() tea.Cmd {
		values := []string{tr(tPrjOriginNew), tr(tPrjOriginAdopt)}
		m.openProjectPicker(tr(tPrjOriginTitle), tr(tPrjOriginNote), values, 0, func(v string) tea.Cmd {
			if v == values[1] {
				return m.projectAdoptStacks()
			}
			return m.projectFlowShow(newFlowDraft(), "")
		})
		return nil
	})
}

// projectAdoptStart lists the running stacks to adopt one of them.
func (m *Model) projectAdoptStart() tea.Cmd {
	return m.projectFlowThen(m.projectAdoptStacks)
}

func (m *Model) openAdoptPrompt() {
	m.openProjectPrompt(&projectPrompt{kind: promptAdopt, fields: []promptField{
		newPromptPath("dir", tr(tPrjAdoptDir)), newPromptText("name", tr(tPrjAdoptName))}})
}

// adoptSubmit asks the service for the proposal for the stack in the directory typed.
func (m *Model) adoptSubmit() tea.Cmd {
	p := m.prj.prompt
	dir, name := strings.TrimSpace(p.fields[0].in.Value()), strings.TrimSpace(p.fields[1].in.Value())
	switch {
	case !filepath.IsAbs(dir):
		m.fail = tr(tFlowAbsolute)
		return nil
	case name != "" && !config.ValidName(name):
		m.fail = tr(tPrjBadName)
		return nil
	}
	m.clearMessages()
	p.busy = true
	m.busy = tr(tBusyAdopt)
	seq, b := p.seq, m.backend
	params := control.AdoptParams{Dir: dir, Name: name, DryRun: true}
	return func() tea.Msg {
		res, err := b.AdoptProject(params)
		return projectAdoptMsg{seq: seq, res: res, err: err}
	}
}

// projectAdoptMsg is the answer to the request for a proposal.
type projectAdoptMsg struct {
	seq int
	// list says the request came from the list of stacks, whose seq numbers them, not from the panel.
	list bool
	res  control.AdoptResult
	err  error
}

func (projectAdoptMsg) projectsAnswer() {}

func (m *Model) takeProjectAdopt(msg projectAdoptMsg) {
	if msg.list {
		if msg.seq != m.prj.stackSeq {
			return
		}
		m.busy = ""
		switch {
		case m.screen != screenArea || m.area != areaProjects:
		case msg.err != nil:
			m.fail = projectAdoptError(msg.err)
		default:
			m.showAdoption(msg.res)
		}
		return
	}
	p := m.prj.prompt
	if p == nil || p.kind != promptAdopt || p.seq != msg.seq {
		return
	}
	m.busy, p.busy = "", false
	if msg.err != nil {
		m.fail = projectAdoptError(msg.err)
		return
	}
	m.closeProjectPrompt()
	m.showAdoption(msg.res)
}

// projectAdoptError is the fixed text of a refused request for a proposal.
func projectAdoptError(err error) string {
	switch {
	case isCode(err, control.CodeNotFound):
		return tr(tPrjNoDir)
	case isCode(err, control.CodeNoAccess):
		var ce *control.Error
		if errors.As(err, &ce) && len(ce.Paths) > 0 {
			return tr(tPrjNoAccess, pathsText(ce.Paths[:1]))
		}
		return errorText(err)
	case isCode(err, control.CodeDockerUnavailable):
		return tr(tPrjNoDocker)
	}
	return errorText(err)
}

// adoptBlocked reports whether the proposal has a problem that keeps it from being taken over.
func adoptBlocked(res control.AdoptResult) bool {
	for _, pr := range res.Problems {
		if pr.Blocking {
			return true
		}
	}
	return false
}

// showAdoption shows the proposal: what the stack is, and the problems with it, the blocking ones marked.
func (m *Model) showAdoption(res control.AdoptResult) {
	const width = 54
	blocked := adoptBlocked(res)
	pj := res.Project
	line := func(label, value string) string {
		if value == "" {
			value = "-"
		}
		return "  " + truncateCells(label+": "+oneLine(value), width)
	}
	const shown = 3
	lines := []string{
		line(tr(tFlowLblName), pj.Name),
		line(tr(tFlowLblDir), pj.Dir),
	}
	for i, f := range pj.ComposeFiles {
		if i == shown {
			lines = append(lines, styles.hint.Render("  "+tr(tPrjMore, len(pj.ComposeFiles)-shown)))
			break
		}
		lines = append(lines, line(tr(tFlowLblCompose), f))
	}
	lines = append(lines, line(tr(tFlowLblComposeProject), pj.ComposeProject), line(tr(tFlowLblEnv), pj.EnvFile))
	for i, im := range pj.Images {
		if i == shown {
			lines = append(lines, styles.hint.Render("  "+tr(tPrjMore, len(pj.Images)-shown)))
			break
		}
		lines = append(lines, line(tr(tFlowSumImage, im.Name), im.Ref+" "+versionOrUnknown(im.Current)))
	}
	if g := pj.Git; g != nil {
		lines = append(lines, line(control.SourceGit, g.URL+" "+versionOrUnknown(g.Current)))
	}
	if len(res.Problems) > 0 {
		lines = append(lines, "")
	}
	const problems = 3
	for i, pr := range res.Problems {
		if i == problems {
			lines = append(lines, styles.hint.Render("  "+tr(tPrjMore, len(res.Problems)-problems)))
			break
		}
		text := oneLine(pr.Message)
		if pr.Hint != "" {
			text += " " + oneLine(pr.Hint)
		}
		if pr.Blocking {
			lines = append(lines, styles.fail.Render("  ✗ "+truncateCells(text, width-2)))
		} else {
			lines = append(lines, styles.warning.Render("  "+warnMark+" "+truncateCells(text, width-2)))
		}
	}
	q := question{title: tr(tPrjAdoptTitle, oneLine(pj.Name)), consequence: tr(tPrjAdoptOK), details: lines,
		keys: "enter take over · {back} back"}
	if blocked {
		q.consequence, q.warn, q.keys = tr(tPrjAdoptBlock), true, "{back} close"
	}
	m.openProjectDialog(&projectDialog{kind: projDlgAdopt, name: pj.Name, adopt: &res, blocked: blocked}, q)
}

func versionOrUnknown(v string) string {
	if v == "" {
		return "(" + tr(tPrjNoCurrent) + ")"
	}
	return fmt.Sprintf("(%s)", oneLine(v))
}

// projectOfAdoption is the project the proposal describes: its file when that reads, else the fields of the
// proposal.
func projectOfAdoption(res control.AdoptResult) config.Project {
	if res.File != "" {
		if p, err := config.DecodeProject([]byte(res.File)); err == nil {
			return p
		}
	}
	pj := res.Project
	p := config.Project{Name: pj.Name, Dir: pj.Dir, Template: pj.Template, EnvFile: pj.EnvFile,
		ComposeFiles: pj.ComposeFiles, ComposeProject: pj.ComposeProject}
	for _, im := range pj.Images {
		p.Images = append(p.Images, config.Image{Name: im.Name, Ref: im.Ref, EnvVar: im.EnvVar, Services: im.Services})
	}
	if g := pj.Git; g != nil {
		p.Git = &config.GitSource{URL: g.URL}
	}
	return p
}
