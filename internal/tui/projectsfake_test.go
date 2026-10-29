package tui

import (
	"fmt"
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
			logs: map[string]control.DeployLogResult{}, sources: map[string]control.SourceTestResult{}}
	}
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
