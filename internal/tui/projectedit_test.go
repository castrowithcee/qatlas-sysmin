package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// editFile is a stored project with a template of its own and a credential.
const editFile = `schema: 1
name: demo-app
dir: /srv/demo-app
template: acme-deploy
env_file: /srv/demo-app/.env
images:
  - name: app
    ref: registry.example.com/demo/app
    env_var: DEMO_APP_TAG
    credential: demo-registry
values:
  api_token: abcd-example-token
`

func editFake() *fakeBackend {
	fb := projectsFake()
	fb.session = true
	d := fb.proj.details["demo-app"]
	d.File = editFile
	fb.proj.details["demo-app"] = d
	return fb
}

func TestEditSendsTheRevisionAndKeepsTheName(t *testing.T) {
	fb := editFake()
	m := projectsModel(t, fb, 120, 40)
	send(m, "e")
	f := m.prj.flow
	if f == nil || f.revision != "rev-demo-app" || f.draft.name != "demo-app" || !strings.Contains(plain(m), "Edit project") {
		t.Fatalf("the edit does not open: flow = %+v\n%s", f, plain(m))
	}
	if fb.count("project demo-app") == 0 {
		t.Errorf("the project is not read: %v", fb.log())
	}
	// The name stands on its row and takes no text.
	if r, ok := f.focused(); !ok || r.id != "name" || r.typed() {
		t.Fatalf("focus = %q, typed = %v", f.focus, ok && r.typed())
	}
	typed(m, "xyz")
	if f.draft.name != "demo-app" {
		t.Errorf("the name changed to %q", f.draft.name)
	}
	send(m, "enter", "backspace", " ")
	if f.draft.name != "demo-app" {
		t.Errorf("the name changed to %q", f.draft.name)
	}
	for range 6 {
		send(m, "f2")
	}
	flowAtStep(t, m, flowSummary)
	send(m, "f2")
	if len(fb.proj.saved) != 1 || fb.count("edit demo-app") != 1 {
		t.Fatalf("calls = %v, fail = %q", fb.log(), m.fail)
	}
	got := fb.proj.saved[0]
	if got.Name != "demo-app" || got.Revision != "rev-demo-app" || !strings.Contains(got.File, "template: acme-deploy") ||
		!strings.Contains(got.File, "credential: demo-registry") || !strings.Contains(got.File, "api_token") {
		t.Errorf("request = %+v", got)
	}
	if m.prj.flow != nil {
		t.Error("the setup stays after the save")
	}
}

func TestEditKeepsTheTemplateOfItsOwnSelectable(t *testing.T) {
	m := projectsModel(t, editFake(), 120, 40)
	send(m, "e")
	f := m.prj.flow
	if f.draft.scenario != "acme-deploy" {
		t.Fatalf("scenario = %q", f.draft.scenario)
	}
	flowToRow(t, m, "template")
	send(m, "right")
	if f.draft.scenario == "acme-deploy" {
		t.Fatal("right does not leave the template")
	}
	send(m, "left")
	if f.draft.scenario != "acme-deploy" {
		t.Errorf("left does not return to the template: %q", f.draft.scenario)
	}
	send(m, "enter")
	if m.dd == nil || !strings.Contains(plain(m), "acme-deploy") {
		t.Errorf("the list lacks the template:\n%s", plain(m))
	}
}

func TestEditOfAChangedProjectIsRefused(t *testing.T) {
	fb := editFake()
	fb.proj.saveErr = &control.Error{Code: control.CodeChanged}
	m := projectsModel(t, fb, 120, 40)
	send(m, "e")
	m.prj.flow.show(flowSummary)
	send(m, "f2")
	if m.prj.flow == nil || m.fail != tr(tProjChanged) {
		t.Errorf("flow = %v, fail = %q", m.prj.flow, m.fail)
	}
}

func TestEditNeedsAReadableFile(t *testing.T) {
	fb := editFake()
	d := fb.proj.details["demo-app"]
	d.File = "not: [a project"
	fb.proj.details["demo-app"] = d
	m := projectsModel(t, fb, 120, 40)
	send(m, "e")
	if m.prj.flow != nil || m.fail != tr(tProjFileBad) {
		t.Errorf("flow = %v, fail = %q", m.prj.flow, m.fail)
	}
	m = projectsModel(t, editFake(), 120, 40)
	send(m, "/")
	typed(m, "zzz")
	send(m, "enter", "e")
	if m.prj.flow != nil || m.fail == "" {
		t.Errorf("without a project: flow = %v, fail = %q", m.prj.flow, m.fail)
	}
}

func TestDuplicateStartsANewProjectFromAStoredOne(t *testing.T) {
	fb := editFake()
	m := projectsModel(t, fb, 120, 40)
	send(m, "p")
	f := m.prj.flow
	if f == nil || f.revision != "" || f.draft.name != "demo-app-copy" || !strings.Contains(plain(m), "New project") {
		t.Fatalf("the copy does not open: flow = %+v\n%s", f, plain(m))
	}
	if r, _ := f.focused(); !r.typed() {
		t.Error("the name of a copy cannot be typed")
	}
	if m.flowDirty() {
		t.Error("an untouched copy asks before it is left")
	}
	if got := f.draft.images[0].credential; got != "demo-registry" {
		t.Errorf("credential = %q", got)
	}
	for range 6 {
		send(m, "f2")
	}
	send(m, "f2")
	if len(fb.proj.saved) != 1 || fb.count("add demo-app-copy") != 1 {
		t.Fatalf("calls = %v, fail = %q", fb.log(), m.fail)
	}
	got := fb.proj.saved[0]
	if got.Name != "demo-app-copy" || got.Revision != "" || !strings.Contains(got.File, "name: demo-app-copy") ||
		!strings.Contains(got.File, "credential: demo-registry") {
		t.Errorf("request = %+v", got)
	}
}

func TestDuplicateNamesAvoidTakenOnes(t *testing.T) {
	fb := editFake()
	fb.addProject("demo-app-copy", control.StateApproved)
	m := projectsModel(t, fb, 120, 40)
	send(m, "p")
	if got := m.prj.flow.draft.name; got != "demo-app-copy-2" {
		t.Errorf("name = %q", got)
	}
}

func TestStartFromAStoredProjectIsACommand(t *testing.T) {
	m := projectsModel(t, editFake(), 120, 40)
	runCommand(m, "new from project")
	if f := m.prj.flow; f == nil || f.revision != "" || f.draft.name != "demo-app-copy" {
		t.Fatalf("flow = %+v", f)
	}
	m = projectsModel(t, editFake(), 120, 40)
	runCommand(m, "edit project")
	if f := m.prj.flow; f == nil || f.revision != "rev-demo-app" {
		t.Fatalf("flow = %+v", m.prj.flow)
	}
}

func TestEditAndDuplicateInFooterAndGerman(t *testing.T) {
	m := projectsModel(t, editFake(), 200, 40)
	if view := plain(m); !strings.Contains(view, "e edit") || !strings.Contains(view, "p duplicate") {
		t.Errorf("footer:\n%s", view)
	}
	useLanguage(t, "de")
	if view := plain(m); !strings.Contains(view, "e bearbeiten") {
		t.Errorf("German footer:\n%s", view)
	}
	send(m, "e")
	if view := plain(m); !strings.Contains(view, "Projekt bearbeiten") {
		t.Errorf("German title:\n%s", view)
	}
}
