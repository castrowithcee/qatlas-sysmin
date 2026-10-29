package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The steps of the guided setup are pages of rows. Every row is rebuilt from the draft whenever it is needed,
// so a row never holds a value of its own; its id is the path of its field in the project file, the path the
// problems of the service name, which is how a problem finds its row.

// flowStepID names a step of the guided setup.
type flowStepID int

const (
	flowStart flowStepID = iota
	flowSource
	flowTarget
	flowSteps
	flowRules
	flowChecks
	flowNotify
	flowSummary
)

// flowRowKind is what a row is and how it takes keys.
type flowRowKind int

const (
	// flowText and flowPath take typed text; a path completes on the file system.
	flowText flowRowKind = iota
	flowPath
	// flowChoice is one of a finite list of values.
	flowChoice
	// flowToggle is on or off.
	flowToggle
	// flowAction adds an entry.
	flowAction
	// flowFold opens or closes the advanced rows of the step.
	flowFold
	// flowPick holds the name of a vault credential; enter opens the list of those that fit.
	flowPick
	// flowLine is a line of text that takes no input.
	flowLine
)

// The kinds of vault credential each use accepts.
var (
	flowKindsImage  = []string{"registry"}
	flowKindsGit    = []string{"git-https", "ssh-key"}
	flowKindsSecret = []string{"secret"}
)

// flowRow is one row of a step.
type flowRow struct {
	id    string
	kind  flowRowKind
	label string
	// heading stands above the first row of a group, such as the rows of one image.
	heading string
	get     func() string
	set     func(string)
	// choices are the values of a choice row; show is how a value is drawn.
	choices []string
	show    func(string) string
	// mode and start serve a path row; kinds are the credential kinds a pick row accepts.
	mode  projectPathMode
	start func() string
	kinds []string
	// locked shows a text row as a value that cannot be typed into.
	locked bool
	// act runs a toggle, an action or a fold; remove deletes the entry the row belongs to.
	act    func()
	remove func()
	// on is the state of a toggle.
	on       func() bool
	advanced bool
	// aliases are further problem fields the row stands for, such as the field of a whole list.
	aliases []string
	info    textID
}

func (r *flowRow) typed() bool { return (r.kind == flowText || r.kind == flowPath) && !r.locked }

// value is the text of the row as drawn while it is not being typed into.
func (r *flowRow) value() string {
	if r.get == nil {
		return ""
	}
	v := r.get()
	if r.show != nil {
		return r.show(v)
	}
	return v
}

// stepOrder is the steps of the setup in order; the step of the project's own steps is there only for the
// custom scenario.
func (f *projectFlow) stepOrder() []flowStepID {
	out := []flowStepID{flowStart, flowSource, flowTarget}
	if f.draft.scenario == flowCustom {
		out = append(out, flowSteps)
	}
	return append(out, flowRules, flowChecks, flowNotify, flowSummary)
}

// flowStepTitle is the name of a step as drawn.
func flowStepTitle(s flowStepID) string {
	return tr([...]textID{tFlowStepStart, tFlowStepSource, tFlowStepTarget, tFlowStepSteps, tFlowStepRules,
		tFlowStepChecks, tFlowStepNotify, tFlowStepSummary}[s])
}

// text, path and choice build the common rows.
func flowTextRow(id string, label textID, value *string, info textID) flowRow {
	return flowRow{id: id, kind: flowText, label: tr(label), get: func() string { return *value },
		set: func(v string) { *value = v }, info: info}
}

func flowPathRow(id string, label textID, value *string, mode projectPathMode, start func() string, info textID) flowRow {
	r := flowTextRow(id, label, value, info)
	r.kind, r.mode, r.start = flowPath, mode, start
	return r
}

// flowPickRow is a text row for the name of a credential, picked from the vault instead of typed.
func flowPickRow(id string, label textID, value *string, kinds []string, info textID) flowRow {
	r := flowTextRow(id, label, value, info)
	r.kind, r.kinds, r.show = flowPick, kinds, flowNoneOr
	return r
}

func flowChoiceRow(id string, label textID, value *string, choices []string, show func(string) string, info textID) flowRow {
	if show == nil {
		show = wordText
	}
	return flowRow{id: id, kind: flowChoice, label: tr(label), get: func() string { return *value },
		set: func(v string) { *value = v }, choices: choices, show: show, info: info}
}

// flowOrDefault draws the empty value of a choice as the default it stands for.
func flowOrDefault(def string) func(string) string {
	return func(v string) string {
		if v == "" {
			return tr(tFlowDefault, wordText(def))
		}
		return wordText(v)
	}
}

// rows are every row of step s, the advanced ones included, in the order they are drawn.
func (f *projectFlow) rows(s flowStepID) []flowRow {
	d := &f.draft
	var out []flowRow
	switch s {
	case flowStart:
		var names []string
		for _, t := range config.BuiltinTemplates() {
			names = append(names, t.Name)
		}
		choices := append(names, flowCustom)
		if d.ownTemplate != "" {
			choices = append([]string{d.ownTemplate}, choices...)
		}
		scenario := flowChoiceRow("template", tFlowLblScenario, &d.scenario, choices, nil, tInfoFlowScenario)
		scenario.set = f.setScenario
		scenario.aliases = []string{"steps"}
		name := flowTextRow("name", tFlowLblName, &d.name, tInfoFlowName)
		// The service refuses a change of the name of a stored project.
		name.locked = f.revision != ""
		out = append(out, name, scenario)
	case flowSource:
		out = f.sourceRows()
	case flowTarget:
		dir := func() string { return strings.TrimSpace(d.dir) }
		out = append(out,
			flowPathRow("dir", tFlowLblDir, &d.dir, projectPathDir, func() string { return "/" }, tInfoFlowDir),
			flowPathRow("env_file", tFlowLblEnv, &d.envFile, projectPathFile, dir, tInfoFlowEnv))
		for i := range d.composeFiles {
			r := flowPathRow(fmt.Sprintf("compose_files[%d]", i), tFlowLblCompose, &d.composeFiles[i], projectPathFile, dir, tInfoFlowCompose)
			r.remove = func() { d.composeFiles = slices.Delete(d.composeFiles, i, i+1) }
			out = append(out, r)
		}
		// The last compose row is empty and takes a further file; typing into it adds one.
		var next string
		add := flowPathRow(fmt.Sprintf("compose_files[%d]", len(d.composeFiles)), tFlowLblCompose, &next, projectPathFile, dir, tInfoFlowCompose)
		add.set = func(v string) { d.composeFiles = append(d.composeFiles, v) }
		add.aliases = []string{"compose_files"}
		out = append(out, add)
		project := flowTextRow("compose_project", tFlowLblComposeProject, &d.composeProject, tInfoFlowComposeProject)
		poll := flowTextRow("poll_interval", tFlowLblPoll, &d.poll, tInfoFlowPoll)
		project.advanced, poll.advanced = true, true
		out = append(out, project, poll)
	case flowSteps:
		out = f.stepRows()
	case flowRules:
		out = f.ruleRows()
	case flowChecks:
		out = f.checkRows()
	case flowNotify:
		out = append(out, f.channelRows()...)
		for i, e := range flowEvents {
			r := flowRow{id: "notify.events/" + e, kind: flowToggle, label: wordText(e), info: tInfoFlowEvents,
				on: func() bool { return d.events[e] }}
			r.act = func() {
				if d.events[e] {
					delete(d.events, e)
				} else {
					d.events[e] = true
				}
			}
			if i == 0 {
				r.heading, r.aliases = tr(tFlowLblEvents), []string{"notify.events"}
			}
			out = append(out, r)
		}
		kind := flowChoiceRow("webhook.kind", tFlowLblHookKind, &d.hookKind,
			[]string{"", config.WebhookGitHub, config.WebhookGitLab, config.WebhookGitea, config.WebhookHMAC}, flowNoneOr, tInfoFlowHookKind)
		kind.aliases = []string{"webhook"}
		secret := flowPickRow("webhook.secret", tFlowLblHookSecret, &d.hookCred, flowKindsSecret, tInfoFlowHookSecret)
		kind.advanced, secret.advanced = true, true
		out = append(out, kind)
		if d.hookKind != "" {
			out = append(out, secret)
		}
	}
	return out
}

// flowNoneOr draws the empty value of an optional choice as none.
func flowNoneOr(v string) string {
	if v == "" {
		return wordText(flowNone)
	}
	return wordText(v)
}

// setScenario takes a scenario: a template decides the kind of source, and the custom scenario starts from
// the steps of the template of the source kind.
func (f *projectFlow) setScenario(v string) {
	d := &f.draft
	d.scenario = v
	switch v {
	case flowImageTag:
		d.source = flowImages
	case flowGitTag:
		d.source = control.SourceGit
	case flowCustom:
		if len(d.steps) == 0 {
			from := flowImageTag
			if d.source == control.SourceGit {
				from = flowGitTag
			}
			d.steps = flowTemplateSteps(from)
		}
	}
	if d.source == flowImages && len(d.images) == 0 {
		d.images = []flowImage{{}}
	}
}

func (f *projectFlow) sourceRows() []flowRow {
	d := &f.draft
	var out []flowRow
	if d.scenario == flowCustom {
		src := flowChoiceRow("source", tFlowLblSource, &d.source, []string{flowImages, control.SourceGit}, nil, tInfoFlowSource)
		src.set = func(v string) {
			d.source = v
			if v == flowImages && len(d.images) == 0 {
				d.images = []flowImage{{}}
			}
		}
		out = append(out, src)
	}
	if d.source == control.SourceGit {
		out = append(out,
			flowTextRow("git.url", tFlowLblGitURL, &d.gitURL, tInfoFlowGitURL),
			flowPickRow("git.credential", tFlowLblCredential, &d.gitCred, flowKindsGit, tInfoFlowCredential))
	} else {
		for i := range d.images {
			im := &d.images[i]
			p := fmt.Sprintf("images[%d]", i)
			rows := []flowRow{
				flowTextRow(p+".name", tFlowLblImageName, &im.name, tInfoFlowImageName),
				flowTextRow(p+".ref", tFlowLblImageRef, &im.ref, tInfoFlowImageRef),
				flowTextRow(p+".env_var", tFlowLblEnvVar, &im.envVar, tInfoFlowEnvVar),
				flowTextRow(p+".services", tFlowLblServices, &im.services, tInfoFlowServices),
				flowPickRow(p+".credential", tFlowLblCredential, &im.credential, flowKindsImage, tInfoFlowCredential),
			}
			rows[0].heading, rows[0].aliases = tr(tFlowHeadImage, i+1), []string{p}
			if len(d.images) > 1 {
				for j := range rows {
					rows[j].remove = func() { d.images = slices.Delete(d.images, i, i+1) }
				}
			}
			out = append(out, rows...)
		}
		out = append(out, flowRow{id: "images+", kind: flowAction, label: tr(tFlowAddImage), info: tInfoFlowAddImage,
			act: func() { d.images = append(d.images, flowImage{}) }})
	}
	if len(out) > 0 {
		out[0].aliases = append(out[0].aliases, "images", "git")
	}
	return out
}

// ruleRows are the version rule of each image, or of the git source.
func (f *projectFlow) ruleRows() []flowRow {
	d := &f.draft
	rule := func(prefix, heading string, r *flowRule) []flowRow {
		p := prefix + ".version_rule."
		rows := []flowRow{
			flowChoiceRow(p+"mode", tFlowLblMode, &r.mode, []string{"", config.ModeManual, config.ModeAuto}, flowOrDefault(config.ModeManual), tInfoFlowMode),
			flowChoiceRow(p+"max_update", tFlowLblMaxUpdate, &r.maxUpdate, []string{"", config.LevelPatch, config.LevelMinor, config.LevelMajor},
				flowOrDefault(config.LevelMajor), tInfoFlowMaxUpdate),
			flowChoiceRow(p+"major_steps", tFlowLblMajorSteps, &r.majorSteps, []string{"", config.MajorDirect, config.MajorStep},
				flowOrDefault(config.MajorDirect), tInfoFlowMajorSteps),
			flowTextRow(p+"tag_filter", tFlowLblTagFilter, &r.tagFilter, tInfoFlowTagFilter),
			flowTextRow(p+"changelog_repo", tFlowLblChangelogRepo, &r.changelogRepo, tInfoFlowChangelogRepo),
			flowPickRow(p+"changelog_credential", tFlowLblChangelogCred, &r.changelogCredential, flowKindsSecret, tInfoFlowChangelogCred),
		}
		rows[0].aliases = []string{prefix + ".version_rule"}
		for i := range rows {
			rows[i].heading = heading
			rows[i].advanced = i >= 2
		}
		return rows
	}
	if d.source == control.SourceGit {
		return rule("git", control.SourceGit, &d.gitRule)
	}
	var out []flowRow
	for i := range d.images {
		heading := strings.TrimSpace(d.images[i].name)
		if heading == "" {
			heading = tr(tFlowHeadImage, i+1)
		}
		out = append(out, rule(fmt.Sprintf("images[%d]", i), heading, &d.images[i].rule)...)
	}
	return out
}

// stepRows are the project's own steps: the type of each and the field its type needs.
func (f *projectFlow) stepRows() []flowRow {
	d := &f.draft
	var types []string
	for _, t := range config.StepTypes() {
		types = append(types, string(t))
	}
	var out []flowRow
	for i := range d.steps {
		s := &d.steps[i]
		p := fmt.Sprintf("steps[%d]", i)
		typ := flowChoiceRow(p+".type", tFlowLblStepType, &s.typ, types, func(v string) string { return v }, tInfoFlowStepType)
		typ.heading, typ.aliases = tr(tFlowHeadStep, i+1), []string{p}
		rows := []flowRow{typ}
		switch config.StepType(s.typ) {
		case config.StepSetEnv:
			rows = append(rows, flowTextRow(p+".assign", tFlowLblAssign, &s.assign, tInfoFlowAssign))
		case config.StepComposePull, config.StepComposeUp:
			rows = append(rows, flowTextRow(p+".services", tFlowLblServices, &s.services, tInfoFlowStepServices))
		case config.StepGitCheckout:
			rows = append(rows, flowTextRow(p+".tag", tFlowLblTag, &s.tag, tInfoFlowTag))
		case config.StepCommand:
			rows = append(rows, flowTextRow(p+".run", tFlowLblRun, &s.run, tInfoFlowRun))
		}
		for j := range rows {
			rows[j].remove = func() { d.steps = slices.Delete(d.steps, i, i+1) }
		}
		out = append(out, rows...)
	}
	return append(out, flowRow{id: "steps+", kind: flowAction, label: tr(tFlowAddStep), info: tInfoFlowAddStep,
		act: func() { d.steps = append(d.steps, flowStepDraft{typ: string(config.StepCommand)}) }})
}

// checkRows are the backup and the checks of the project.
func (f *projectFlow) checkRows() []flowRow {
	d := &f.draft
	b := &d.backup
	mode := flowChoiceRow("backup.mode", tFlowLblBackup, &b.mode, []string{flowNone, flowVolume, flowCommand}, nil, tInfoFlowBackup)
	mode.heading, mode.aliases = tr(tFlowHeadBackup), []string{"backup", "backup.type"}
	out := []flowRow{mode}
	if b.mode == flowVolume || b.mode == flowCommand {
		if b.mode == flowVolume {
			out = append(out, flowTextRow("backup.volume", tFlowLblVolume, &b.volume, tInfoFlowVolume))
		} else {
			out = append(out, flowTextRow("backup.command", tFlowLblCommand, &b.command, tInfoFlowBackupCommand))
		}
		keep := flowTextRow("backup.keep", tFlowLblKeep, &b.keep, tInfoFlowKeep)
		restore := flowTextRow("backup.restore_command", tFlowLblRestore, &b.restore, tInfoFlowRestore)
		keep.advanced, restore.advanced = true, true
		out = append(out, keep, restore)
	}
	for i := range d.checks {
		c := &d.checks[i]
		p := fmt.Sprintf("checks[%d]", i)
		var rows []flowRow
		if c.typ == config.StepCheckHTTP {
			status := flowTextRow(p+".status", tFlowLblStatus, &c.status, tInfoFlowStatus)
			contains := flowTextRow(p+".contains", tFlowLblContains, &c.contains, tInfoFlowContains)
			status.advanced, contains.advanced = true, true
			rows = []flowRow{flowTextRow(p+".url", tFlowLblURL, &c.url, tInfoFlowURL), status, contains}
		} else {
			containers := flowTextRow(p+".containers", tFlowLblContainers, &c.containers, tInfoFlowContainers)
			containers.advanced = true
			rows = []flowRow{flowTextRow(p+".services", tFlowLblServices, &c.services, tInfoFlowCheckServices), containers}
		}
		rows[0].aliases = []string{p, p + ".type"}
		for j := range rows {
			rows[j].heading = tr(tFlowHeadCheck, i+1, string(c.typ))
			rows[j].remove = func() { d.checks = slices.Delete(d.checks, i, i+1) }
		}
		out = append(out, rows...)
	}
	add := func(id string, label textID, t config.StepType) flowRow {
		return flowRow{id: id, kind: flowAction, label: tr(label), info: tInfoFlowAddCheck,
			act: func() { d.checks = append(d.checks, flowCheck{typ: t}) }}
	}
	return append(out, add("checks+http", tFlowAddHTTP, config.StepCheckHTTP), add("checks+container", tFlowAddContainer, config.StepCheckContainer))
}

// shownRows are the rows of step s as drawn: the rows that are not advanced, then the fold row and, while the
// fold is open, the advanced rows.
func (f *projectFlow) shownRows(s flowStepID) []flowRow {
	var plain, advanced []flowRow
	for _, r := range f.rows(s) {
		if r.advanced {
			advanced = append(advanced, r)
		} else {
			plain = append(plain, r)
		}
	}
	if len(advanced) == 0 {
		return plain
	}
	open := f.advancedOpen(s, advanced)
	fold := flowRow{id: fmt.Sprintf("advanced/%d", s), kind: flowFold, label: tr(tFlowAdvanced), info: tInfoFlowAdvanced,
		on: func() bool { return open }}
	fold.act = func() { f.open[s] = !open }
	out := append(plain, fold)
	if open {
		out = append(out, advanced...)
	}
	return out
}

// advancedOpen reports whether the advanced rows of step s stand open: the person opened them, or one of them
// holds a value or a problem, or has the focus.
func (f *projectFlow) advancedOpen(s flowStepID, advanced []flowRow) bool {
	if f.open[s] {
		return true
	}
	for _, r := range advanced {
		if r.id == f.focus || len(f.problems[r.id]) > 0 || (r.get != nil && r.get() != "") {
			return true
		}
	}
	return false
}

// channelRows are the notification channels: one toggle per channel of the service once the list is known,
// and a channel the project names that the list lacks stays on the list so it can be taken off. Without the
// list the names are typed.
func (f *projectFlow) channelRows() []flowRow {
	d := &f.draft
	if !f.channelsKnown {
		r := flowTextRow("notify.channels", tFlowLblChannels, &d.channels, tInfoFlowChannels)
		r.aliases = []string{"notify"}
		return []flowRow{r}
	}
	names := slices.Clone(f.channels)
	for _, n := range flowList(d.channels) {
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return []flowRow{{id: "notify.channels", kind: flowLine, label: tr(tFlowNoChannels), info: tInfoFlowChannels,
			aliases: []string{"notify"}}}
	}
	var out []flowRow
	for i, n := range names {
		r := flowRow{id: "notify.channels/" + n, kind: flowToggle, label: n, info: tInfoFlowChannels,
			on: func() bool { return slices.Contains(flowList(d.channels), n) }}
		r.act = func() {
			list := flowList(d.channels)
			if at := slices.Index(list, n); at >= 0 {
				list = slices.Delete(list, at, at+1)
			} else {
				list = append(list, n)
			}
			d.channels = strings.Join(list, ", ")
		}
		if i == 0 {
			r.heading, r.aliases = tr(tFlowLblChannels), []string{"notify.channels", "notify"}
		}
		out = append(out, r)
	}
	return out
}
