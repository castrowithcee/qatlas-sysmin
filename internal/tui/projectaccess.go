package tui

import (
	"bytes"
	"encoding/json"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/adopt"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// A proposal blocked by missing access offers to set it up: y reads what the setup would change as root
// (`sudo sysmin host preview stack.setup-access …`, which changes nothing), the question names it, and y on
// that question applies it as any host change. The proposal is then requested again with the same input, so
// the adoption goes on where it stopped. The person never types a command.

// adoptNote is a line under the problems of the proposal. noOffer keeps the proposal from offering the setup
// again: after a setup, the access that is still missing is outside what the action covers.
type adoptNote struct {
	text    string
	bad     bool
	noOffer bool
}

// accessOffer is the blocked proposal a setup of access belongs to.
type accessOffer struct {
	res    control.AdoptResult
	params control.AdoptParams
	// argv and sentence are set once the question is on screen.
	argv     []string
	sentence string
}

// accessPreview is the answer of `host preview stack.setup-access`.
type accessPreview struct {
	Sentence string   `json:"sentence"`
	Commands []string `json:"commands"`
	Preview  *struct {
		Path  string `json:"path"`
		Stack struct {
			Changes []json.RawMessage `json:"changes"`
		} `json:"stack"`
		Parents []struct {
			Path string `json:"path"`
		} `json:"parents"`
		OthersLoseAccess bool `json:"others_lose_access"`
		NothingToDo      bool `json:"nothing_to_do"`
	} `json:"preview"`
}

// missingAccess reports whether the proposal is blocked by missing access.
func missingAccess(res control.AdoptResult) bool {
	for _, pr := range res.Problems {
		if pr.Code == adopt.CodeMissingAccess && pr.Blocking {
			return true
		}
	}
	return false
}

// canOfferAccess reports whether the proposal offers to set up the access of its stack folder.
func canOfferAccess(res control.AdoptResult, note adoptNote) bool {
	return !note.noOffer && res.Project.Dir != "" && missingAccess(res)
}

// askAccessSetup reads the preview of the stack folder as root and, from its answer, asks to set it up.
func (m *Model) askAccessSetup(o *accessOffer) tea.Cmd {
	dir := o.res.Project.Dir
	args := append([]string{"preview", "--output", "json", "--", hostchange.ActionStackSetupAccess},
		hostchange.StackAccessArgs(dir)...)
	return m.runHost(HostRequest{Args: args}, func(out []byte, err error) tea.Cmd {
		var p accessPreview
		if json.Unmarshal(bytes.TrimSpace(out), &p) == nil && p.Preview != nil {
			if p.Preview.NothingToDo {
				m.showAdoptionAs(o.res, o.params, adoptNote{text: tr(tPrjAccessNothing), noOffer: true})
				return nil
			}
			m.confirmAccess(o, p)
			return nil
		}
		// A refusal comes as the report of a refused change; the proposal stays, with the reason.
		if report, ok := parseReport(out); ok {
			m.showAdoptionAs(o.res, o.params, adoptNote{text: tr(tPrjAccessRefused, m.diffText(report.Error)), bad: true, noOffer: true})
			return nil
		}
		kind, exit := classifyRun(out, err)
		m.hostFailNotice(kind, exit, err, false)
		return nil
	})
}

// confirmAccess asks whether to set up the access the preview describes.
func (m *Model) confirmAccess(o *accessOffer, p accessPreview) {
	v := p.Preview
	var lines []string
	if len(v.Stack.Changes) > 0 {
		lines = append(lines, hostLines(styles.cell, "", tr(tPrjAccessStack, m.diffText(v.Path)))...)
	}
	if len(v.Parents) > 0 {
		lines = append(lines, hostLines(styles.cell, "", tr(tPrjAccessParents))...)
		for _, par := range v.Parents {
			lines = append(lines, hostLines(styles.cell, "  ", m.diffText(par.Path))...)
		}
	}
	lines = append(lines, m.commandLines(p.Commands, stackDetailRoom(m.termHeight)-len(lines))...)
	q := question{title: tr(tPrjAccessTitle), consequence: tr(tHostReversible),
		details: []string{"\n" + strings.Join(lines, "\n")}, keys: "y apply · n/{back} cancel"}
	if v.OthersLoseAccess {
		q.consequence, q.warn = tr(tPrjAccessOthers)+" "+q.consequence, true
	}
	o.argv = append([]string{"apply", "--yes", "--output", "json", "--", hostchange.ActionStackSetupAccess},
		hostchange.StackAccessArgs(v.Path)...)
	o.sentence = p.Sentence
	m.openProjectDialog(&projectDialog{kind: projDlgAccess, access: o}, q)
}

// projectAccessMsg is the proposal asked for again after the access was set up.
type projectAccessMsg struct {
	offer *accessOffer
	res   control.AdoptResult
	err   error
}

func (projectAccessMsg) projectsAnswer() {}

// applyAccess sets the access up as root. When it is set up, the proposal is requested again; otherwise the
// blocked proposal is shown again with what went wrong.
func (m *Model) applyAccess(o *accessOffer) tea.Cmd {
	return m.runHost(HostRequest{Args: o.argv}, func(out []byte, err error) tea.Cmd {
		report, ok := parseReport(out)
		if !ok {
			kind, exit := classifyRun(out, err)
			m.hostFailNotice(kind, exit, err, true)
			return nil
		}
		if report.Result == hostchange.ResultRefused {
			m.showAdoptionAs(o.res, o.params, adoptNote{text: tr(tPrjAccessRefused, m.diffText(report.Error)), bad: true, noOffer: true})
			return nil
		}
		// The host may have changed even when the run was restored.
		m.hst.chg.stale = true
		if report.Result != hostchange.ResultApplied {
			m.hostResult(report, o.sentence)
			return nil
		}
		m.clearMessages()
		m.busy = tr(tBusyAdopt)
		b, params := m.backend, o.params
		return func() tea.Msg {
			res, err := b.AdoptProject(params)
			return projectAccessMsg{offer: o, res: res, err: err}
		}
	})
}

// takeProjectAccess shows the proposal asked for after the setup. Access that is still missing is shown, and
// the setup is not offered again: the action covers the stack folder and the folders above it only.
func (m *Model) takeProjectAccess(msg projectAccessMsg) {
	m.busy = ""
	if m.screen != screenArea || m.area != areaProjects {
		return
	}
	o := msg.offer
	if msg.err != nil {
		m.showAdoptionAs(o.res, o.params, adoptNote{text: tr(tPrjAccessFailed, projectAdoptError(msg.err)), bad: true, noOffer: true})
		return
	}
	note := adoptNote{text: tr(tPrjAccessDone), noOffer: true}
	if missingAccess(msg.res) {
		note.text, note.bad = tr(tPrjAccessLeft), true
	}
	m.showAdoptionAs(msg.res, o.params, note)
}
