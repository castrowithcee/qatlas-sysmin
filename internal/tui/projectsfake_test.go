package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// fakeProjects is what the fake service knows of the deploy state of its projects.
type fakeProjects struct {
	details map[string]control.ProjectDetail
	status  map[string]control.DeployStatusResult
	logs    map[string]control.DeployLogResult
	sources map[string]control.SourceTestResult
	// runNow is what DeployRunNow answers; statusErr and logErr make the reads of a project fail.
	runNow    control.DeployRunNowResult
	statusErr map[string]error
	logErr    error
	// backups, notes and the update and restore outcomes serve the update and the restore. notes answers
	// Changelog by project; a project without an entry gets one release per asked version.
	backups    map[string][]string
	notes      map[string]control.ChangelogResult
	notesErr   error
	updateRuns map[string]control.ProjectUpdateResult
	restoreRun control.DeployRestoreResult
	// saved records every add and edit request; saveErr refuses them.
	saved   []control.ProjectParams
	saveErr error
	// creds and channels are what Credentials and Channels list; credsErr and channelsErr make the reads
	// fail. credSet records every SetCredential request; credErr refuses them.
	creds       []control.CredentialSummary
	channels    []control.ChannelSummary
	credsErr    error
	channelsErr error
	credSet     []control.CredentialSetParams
	credErr     error
	// adopt is the proposal AdoptProject answers; adoptAsked records every request.
	adopt      control.AdoptResult
	adoptErr   error
	adoptAsked []control.AdoptParams
}

const fakeProjectFile = `schema: 1
name: %s
dir: /srv/%s
env_file: /srv/%s/.env
images:
  - name: app
    ref: registry.example.com/demo/app
    env_var: DEMO_APP_TAG
    credential: demo-registry
    version_rule:
      mode: auto
webhook:
  secret: demo-hook
values:
  api_token: abcd-example-token
`

// addProject gives the fake service a project with one image on 1.0.0 and 1.1.0 and 2.0.0 available.
func (f *fakeBackend) addProject(name, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.proj.details == nil {
		f.proj = fakeProjects{details: map[string]control.ProjectDetail{}, status: map[string]control.DeployStatusResult{},
			logs: map[string]control.DeployLogResult{}, sources: map[string]control.SourceTestResult{},
			backups: map[string][]string{}, notes: map[string]control.ChangelogResult{}}
	}
	f.proj.backups[name] = []string{"backup-20261028", "backup-20261029"}
	sum := control.ProjectSummary{Name: name, Source: "images", Dir: "/srv/" + name, State: state}
	f.projects = append(f.projects, sum)
	f.proj.details[name] = control.ProjectDetail{ProjectSummary: sum, Revision: "rev-" + name,
		File: fmt.Sprintf(fakeProjectFile, name, name, name)}
	f.proj.status[name] = control.DeployStatusResult{Project: name, LastCheck: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
		Sources: []control.DeploySource{{Kind: control.SourceImage, Name: "app", Current: "1.0.0", Available: []string{"1.1.0", "2.0.0"}}},
		LastRun: &control.DeployRun{ID: "20261029T120000.000000000Z", Action: "auto", Result: "succeeded",
			Started: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}}
	f.proj.logs[name] = control.DeployLogResult{Project: name, Run: "20261029T120000.000000000Z",
		Text: "2026-10-29T12:00:00Z auto app: 1.0.0 -> 1.1.0\n2026-10-29T12:00:01Z result=succeeded\n"}
	f.proj.sources[name] = control.SourceTestResult{Project: name, Sources: []control.SourceResult{{
		Kind: control.SourceImage, Name: "app", Ref: "registry.example.com/demo/app", Current: "1.0.0",
		Available: []string{"1.1.0", "2.0.0"}, AvailableTotal: 2}}}
}

func (f *fakeBackend) Project(name string) (control.ProjectDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("project " + name)
	d, ok := f.proj.details[name]
	if !ok {
		return control.ProjectDetail{}, &control.Error{Code: control.CodeNotFound}
	}
	return d, nil
}

func (f *fakeBackend) DeployStatus(project string) (control.DeployStatusResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("status " + project)
	if err := f.proj.statusErr[project]; err != nil {
		return control.DeployStatusResult{}, err
	}
	return f.proj.status[project], nil
}

func (f *fakeBackend) DeployLog(project, run string) (control.DeployLogResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("log " + project + " " + run)
	if f.proj.logErr != nil {
		return control.DeployLogResult{}, f.proj.logErr
	}
	return f.proj.logs[project], nil
}

func (f *fakeBackend) SourceTest(project string) (control.SourceTestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("sources " + project)
	return f.proj.sources[project], nil
}

func (f *fakeBackend) DeployRunNow(project string) (control.DeployRunNowResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("run " + project)
	return f.proj.runNow, nil
}

func (f *fakeBackend) DeployControl(op, project string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(op + " " + project)
	st := f.proj.status[project]
	switch op {
	case control.OpDeployPause:
		st.Paused = true
	case control.OpDeployResume:
		st.Paused = false
	case control.OpDeployPin:
		st.Pinned = true
	case control.OpDeployUnpin:
		st.Pinned = false
	}
	f.proj.status[project] = st
	return nil
}

func (f *fakeBackend) RemoveProject(name, revision string) (control.ChangeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("remove " + name + " " + revision)
	if !f.session {
		return control.ChangeResult{}, &control.Error{Code: control.CodeAdminRequired}
	}
	if f.proj.details[name].Revision != revision {
		return control.ChangeResult{}, &control.Error{Code: control.CodeChanged}
	}
	delete(f.proj.details, name)
	for i, p := range f.projects {
		if p.Name == name {
			f.projects = append(f.projects[:i:i], f.projects[i+1:]...)
			break
		}
	}
	return control.ChangeResult{Project: name, State: control.StateApproved}, nil
}

func (f *fakeBackend) Backups(project string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("backups " + project)
	return f.proj.backups[project], nil
}

func (f *fakeBackend) Changelog(project, image, to string) (control.ChangelogResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("changelog " + project + " " + image + " " + to)
	if f.proj.notesErr != nil {
		return control.ChangelogResult{}, f.proj.notesErr
	}
	if res, ok := f.proj.notes[project]; ok {
		return res, nil
	}
	return control.ChangelogResult{Project: project, Sources: []control.ChangelogSource{{
		Kind: control.SourceImage, Name: image, Current: "1.0.0", Target: to, ReleasesTotal: 1,
		Releases: []control.ChangelogRelease{{Version: to, Name: "Release " + to, Body: "Notes for " + to + "\nSecond line"}}}}}, nil
}

func (f *fakeBackend) DeployUpdate(updates []control.ProjectUpdate) (control.DeployUpdateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var parts []string
	for _, u := range updates {
		var vs []string
		for _, v := range u.Versions {
			vs = append(vs, v.Name+"@"+v.Version)
		}
		parts = append(parts, u.Project+"="+strings.Join(vs, ","))
	}
	f.record("update " + strings.Join(parts, " "))
	if !f.session {
		return control.DeployUpdateResult{}, &control.Error{Code: control.CodeAdminRequired}
	}
	var out control.DeployUpdateResult
	for _, u := range updates {
		if r, ok := f.proj.updateRuns[u.Project]; ok {
			out.Results = append(out.Results, r)
			continue
		}
		out.Results = append(out.Results, control.ProjectUpdateResult{Project: u.Project, Run: &control.DeployRun{
			Result: "succeeded", Action: "manual", Changes: []control.DeployChange{{Kind: control.SourceImage, Name: u.Versions[0].Name, From: "1.0.0", To: u.Versions[0].Version}}}})
	}
	return out, nil
}

func (f *fakeBackend) DeployRestore(project, backup string) (control.DeployRestoreResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("restore " + project + " " + backup)
	if !f.session {
		return control.DeployRestoreResult{}, &control.Error{Code: control.CodeAdminRequired}
	}
	if f.proj.restoreRun.Run != nil || f.proj.restoreRun.Pending {
		return f.proj.restoreRun, nil
	}
	return control.DeployRestoreResult{Project: project, Run: &control.DeployRun{Result: "succeeded", Action: "restore"}}, nil
}

func (f *fakeBackend) AddProject(p control.ProjectParams) (control.ChangeResult, error) {
	return f.saveProject("add", p)
}

func (f *fakeBackend) EditProject(p control.ProjectParams) (control.ChangeResult, error) {
	return f.saveProject("edit", p)
}

// saveProject records the request and, with the session, takes the project into the list as approved.
func (f *fakeBackend) saveProject(op string, p control.ProjectParams) (control.ChangeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(op + " " + p.Name)
	f.proj.saved = append(f.proj.saved, p)
	if !f.session {
		return control.ChangeResult{}, &control.Error{Code: control.CodeAdminRequired}
	}
	if f.proj.saveErr != nil {
		return control.ChangeResult{}, f.proj.saveErr
	}
	if op == "add" {
		f.projects = append(f.projects, control.ProjectSummary{Name: p.Name, Source: "images", State: control.StateApproved})
	}
	return control.ChangeResult{Project: p.Name, State: control.StateApproved, Revision: "rev-" + p.Name}, nil
}

func (f *fakeBackend) Credentials() ([]control.CredentialSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("credentials")
	if f.proj.credsErr != nil {
		return nil, f.proj.credsErr
	}
	return f.proj.creds, nil
}

func (f *fakeBackend) Channels() ([]control.ChannelSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("channels")
	if f.proj.channelsErr != nil {
		return nil, f.proj.channelsErr
	}
	return f.proj.channels, nil
}

// SetCredential keeps the request, secret included, so a test can compare it with what was typed.
func (f *fakeBackend) SetCredential(p control.CredentialSetParams) (control.CredentialResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("credential " + p.Name)
	if !f.session {
		return control.CredentialResult{}, &control.Error{Code: control.CodeAdminRequired}
	}
	if f.proj.credErr != nil {
		return control.CredentialResult{}, f.proj.credErr
	}
	fields := map[string]string{}
	for k, v := range p.Fields {
		fields[k] = v
	}
	f.proj.credSet = append(f.proj.credSet, control.CredentialSetParams{Name: p.Name, Kind: p.Kind, Fields: fields})
	f.proj.creds = append(f.proj.creds, control.CredentialSummary{Name: p.Name, Kind: p.Kind})
	return control.CredentialResult{Name: p.Name, Action: "created", State: control.StateApproved}, nil
}

func (f *fakeBackend) AdoptProject(p control.AdoptParams) (control.AdoptResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("adopt " + p.Dir)
	f.proj.adoptAsked = append(f.proj.adoptAsked, p)
	if f.proj.adoptErr != nil {
		return control.AdoptResult{}, f.proj.adoptErr
	}
	return f.proj.adopt, nil
}
