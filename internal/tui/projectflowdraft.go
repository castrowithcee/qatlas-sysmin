package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The guided setup edits a flowDraft: the project as the text the person types, one string per line of the
// setup. project turns it into the config.Project that is encoded into the file the service receives, which is
// the file `sysmin project add NAME --file FILE` would send. flowDraftOf goes the other way, so a setup can start
// from a project that exists. What the setup has no line for (values, step timeouts) is kept in base and
// survives the round trip.

// Values of the choice lines that are no words of the configuration.
const (
	// flowCustom is the scenario of a project with steps of its own instead of a template.
	flowCustom = "custom"
	// flowImages is the source kind of a project with container images.
	flowImages = "images"
	// The backup modes; none leaves the backup out.
	flowNone    = "none"
	flowVolume  = "volume"
	flowCommand = "command"
	// The built-in templates the setup knows by name.
	flowImageTag = "image-tag"
	flowGitTag   = "git-tag"
)

// flowRule is the version rule of one source.
type flowRule struct {
	mode, maxUpdate, majorSteps, tagFilter, changelogRepo, changelogCredential string
}

type flowImage struct {
	name, ref, envVar, services, credential string
	rule                                    flowRule
	base                                    config.Image
}

type flowStepDraft struct {
	typ                        string
	assign, services, tag, run string
	base                       config.Step
}

type flowCheck struct {
	typ                                         config.StepType
	url, status, contains, services, containers string
	base                                        config.Step
}

type flowBackup struct {
	mode, volume, command, keep, restore string
	base                                 config.Step
}

type flowDraft struct {
	base config.Project

	name string
	// scenario is the name of a template or flowCustom; source is flowImages or control.SourceGit.
	scenario, source string
	// ownTemplate is the template of the project the draft came from when it is no built-in one, so the
	// choice of the scenario keeps offering it after another one was chosen.
	ownTemplate     string
	images          []flowImage
	gitURL, gitCred string
	gitRule         flowRule

	dir, envFile   string
	composeFiles   []string
	composeProject string
	poll           string

	steps  []flowStepDraft
	backup flowBackup
	checks []flowCheck

	channels string
	events   map[string]bool
	hookKind string
	hookCred string
}

// flowEvents are the notification events in the order the configuration lists them.
var flowEvents = []string{config.EventUpdateAvailable, config.EventUpdateSucceeded, config.EventUpdateFailed,
	config.EventRestoreSucceed, config.EventRestoreFailed}

// newFlowDraft is the draft of a new project: the image-tag scenario with one image to fill in.
func newFlowDraft() flowDraft {
	return flowDraft{scenario: flowImageTag, source: flowImages, images: []flowImage{{}}, backup: flowBackup{mode: flowNone},
		events: map[string]bool{}}
}

// flowDraftOf is the draft of an existing project.
func flowDraftOf(p config.Project) flowDraft {
	d := flowDraft{base: p, name: p.Name, scenario: p.Template, source: flowImages, gitRule: flowRuleDraft(config.VersionRule{}),
		dir: p.Dir, envFile: p.EnvFile, composeFiles: slices.Clone(p.ComposeFiles), composeProject: p.ComposeProject,
		events: map[string]bool{}, backup: flowBackup{mode: flowNone}}
	if len(p.Steps) > 0 || p.Template == "" {
		d.scenario = flowCustom
	} else if !slices.ContainsFunc(config.BuiltinTemplates(), func(t config.Template) bool { return t.Name == p.Template }) {
		d.ownTemplate = p.Template
	}
	if p.PollInterval != 0 {
		d.poll = flowDurationText(p.PollInterval)
	}
	for _, im := range p.Images {
		d.images = append(d.images, flowImage{name: im.Name, ref: im.Ref, envVar: im.EnvVar,
			services: strings.Join(im.Services, ", "), credential: im.Credential, rule: flowRuleDraft(im.VersionRule), base: im})
	}
	if g := p.Git; g != nil {
		d.source, d.gitURL, d.gitCred, d.gitRule = control.SourceGit, g.URL, g.Credential, flowRuleDraft(g.VersionRule)
	}
	if len(d.images) == 0 && d.source == flowImages {
		d.images = []flowImage{{}}
	}
	for _, s := range p.Steps {
		d.steps = append(d.steps, flowStepDraft{typ: string(s.Type), assign: strings.Join(s.Assign, ", "),
			services: strings.Join(s.Services, ", "), tag: s.Tag, run: s.Run, base: s})
	}
	if b := p.Backup; b != nil {
		d.backup = flowBackup{mode: flowVolume, volume: b.Volume, command: b.Command, restore: b.RestoreCommand, base: *b}
		if b.Command != "" {
			d.backup.mode = flowCommand
		}
		if b.Keep != 0 {
			d.backup.keep = strconv.Itoa(b.Keep)
		}
	}
	for _, c := range p.Checks {
		fc := flowCheck{typ: c.Type, url: c.URL, contains: c.Contains, services: strings.Join(c.Services, ", "),
			containers: strings.Join(c.Containers, ", "), base: c}
		if c.Status != 0 {
			fc.status = strconv.Itoa(c.Status)
		}
		d.checks = append(d.checks, fc)
	}
	if n := p.Notify; n != nil {
		d.channels = strings.Join(n.Channels, ", ")
		for _, e := range n.Events {
			d.events[e] = true
		}
	}
	if w := p.Webhook; w != nil {
		d.hookKind, d.hookCred = w.Kind, w.Secret
	}
	return d
}

func flowRuleDraft(r config.VersionRule) flowRule {
	return flowRule{r.Mode, r.MaxUpdate, r.MajorSteps, r.TagFilter, r.ChangelogRepo, r.ChangelogCredential}
}

func (r flowRule) rule() config.VersionRule {
	return config.VersionRule{Mode: r.mode, MaxUpdate: r.maxUpdate, MajorSteps: r.majorSteps,
		TagFilter: strings.TrimSpace(r.tagFilter), ChangelogRepo: strings.TrimSpace(r.changelogRepo),
		ChangelogCredential: strings.TrimSpace(r.changelogCredential)}
}

// flowDurationText is a duration the way the configuration writes it.
func flowDurationText(d config.Duration) string {
	v, _ := d.MarshalYAML()
	return fmt.Sprint(v)
}

// flowList is a line of names separated by commas as a list; empty entries drop out.
func flowList(text string) []string {
	var out []string
	for _, part := range strings.Split(text, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// signature identifies the typed state of the draft; the setup holds unsaved input while it differs from the
// one it started with.
func (d flowDraft) signature() string {
	d.base = config.Project{}
	return fmt.Sprintf("%#v", d)
}

// flowNote is a finding of the setup itself on one of its lines, in the shape of a problem of the service.
func flowNote(field string, id textID) control.Problem {
	return control.Problem{Field: field, Code: "local", Message: tr(id)}
}

// project is the project the draft describes and what is plainly wrong in it: a required line left empty, a
// path that is not absolute, a number or duration that does not read. Everything else is left to the
// validation of the service.
func (d flowDraft) project() (config.Project, []control.Problem) {
	var notes []control.Problem
	need := func(field, value string) string {
		value = strings.TrimSpace(value)
		if value == "" {
			notes = append(notes, flowNote(field, tFlowRequired))
		}
		return value
	}
	path := func(field, value string, required bool) string {
		value = strings.TrimSpace(value)
		switch {
		case value == "" && required:
			notes = append(notes, flowNote(field, tFlowRequired))
		case value != "" && !filepath.IsAbs(value):
			notes = append(notes, flowNote(field, tFlowAbsolute))
		}
		return value
	}
	number := func(field, value string) int {
		value = strings.TrimSpace(value)
		if value == "" {
			return 0
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			notes = append(notes, flowNote(field, tFlowNumber))
			return 0
		}
		return n
	}

	p := d.base
	p.Name = need("name", d.name)
	p.Dir = path("dir", d.dir, true)
	p.EnvFile = path("env_file", d.envFile, false)
	p.ComposeFiles = nil
	for i, f := range d.composeFiles {
		if f = path(fmt.Sprintf("compose_files[%d]", i), f, false); f != "" {
			p.ComposeFiles = append(p.ComposeFiles, f)
		}
	}
	p.ComposeProject = strings.TrimSpace(d.composeProject)
	p.PollInterval = 0
	if text := strings.TrimSpace(d.poll); text != "" {
		if v, err := time.ParseDuration(text); err != nil {
			notes = append(notes, flowNote("poll_interval", tFlowDuration))
		} else {
			p.PollInterval = config.Duration(v)
		}
	}

	p.Template, p.Steps = d.scenario, nil
	if d.scenario == flowCustom {
		p.Template = ""
		for i, s := range d.steps {
			st := s.base
			if t := config.StepType(s.typ); t != st.Type {
				// A step of another type keeps none of the fields of the old one.
				st = config.Step{Type: t, Timeout: s.base.Timeout}
			}
			switch st.Type {
			case config.StepSetEnv:
				st.Assign = flowList(s.assign)
			case config.StepComposePull, config.StepComposeUp:
				st.Services = flowList(s.services)
			case config.StepGitCheckout:
				st.Tag = strings.TrimSpace(s.tag)
			case config.StepCommand:
				st.Run = need(fmt.Sprintf("steps[%d].run", i), s.run)
			}
			p.Steps = append(p.Steps, st)
		}
	}

	p.Images, p.Git = nil, nil
	if d.source == control.SourceGit {
		p.Git = &config.GitSource{URL: need("git.url", d.gitURL), Credential: strings.TrimSpace(d.gitCred), VersionRule: d.gitRule.rule()}
	} else {
		for i, im := range d.images {
			f := fmt.Sprintf("images[%d]", i)
			out := im.base
			out.Name, out.Ref = need(f+".name", im.name), need(f+".ref", im.ref)
			out.EnvVar, out.Services, out.Credential = strings.TrimSpace(im.envVar), flowList(im.services), strings.TrimSpace(im.credential)
			out.VersionRule = im.rule.rule()
			p.Images = append(p.Images, out)
		}
	}

	p.Backup = nil
	if b := d.backup; b.mode == flowVolume || b.mode == flowCommand {
		st := b.base
		st.Volume, st.Command = "", ""
		if b.mode == flowVolume {
			st.Volume = need("backup.volume", b.volume)
		} else {
			st.Command = need("backup.command", b.command)
		}
		st.Keep = number("backup.keep", b.keep)
		st.RestoreCommand = strings.TrimSpace(b.restore)
		p.Backup = &st
	}

	p.Checks = nil
	for i, c := range d.checks {
		f := fmt.Sprintf("checks[%d]", i)
		st := c.base
		st.Type = c.typ
		st.URL, st.Status, st.Contains, st.Services, st.Containers = "", 0, "", nil, nil
		if c.typ == config.StepCheckHTTP {
			st.URL = need(f+".url", c.url)
			st.Status = number(f+".status", c.status)
			st.Contains = strings.TrimSpace(c.contains)
		} else {
			st.Services, st.Containers = flowList(c.services), flowList(c.containers)
		}
		p.Checks = append(p.Checks, st)
	}

	p.Notify = nil
	channels := flowList(d.channels)
	var events []string
	for _, e := range flowEvents {
		if d.events[e] {
			events = append(events, e)
		}
	}
	if len(channels) > 0 || len(events) > 0 {
		p.Notify = &config.Notify{Channels: channels, Events: events}
	}

	p.Webhook = nil
	if d.hookKind != "" {
		p.Webhook = &config.ProjectWebhook{Kind: d.hookKind, Secret: need("webhook.secret", d.hookCred)}
	}
	return p, notes
}

// flowTemplateSteps are the steps of a template as the lines of a project's own steps.
func flowTemplateSteps(name string) []flowStepDraft {
	var out []flowStepDraft
	for _, t := range config.BuiltinTemplates() {
		if t.Name != name {
			continue
		}
		for _, s := range t.Steps {
			out = append(out, flowStepDraft{typ: string(s.Type), assign: strings.Join(s.Assign, ", "),
				services: strings.Join(s.Services, ", "), tag: s.Tag, run: s.Run})
		}
	}
	return out
}
