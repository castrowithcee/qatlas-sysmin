package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/i18n"
)

// adoptFake is the fake service with a stack to adopt.
func adoptFake() *fakeBackend {
	fb := projectsFake()
	fb.session = true
	fb.proj.adopt = control.AdoptResult{
		Project: control.AdoptProject{Name: "demo-stack", Dir: "/srv/demo-stack", ComposeProject: "demo-stack",
			ComposeFiles: []string{"/srv/demo-stack/compose.yaml", "/srv/demo-stack/compose.override.yaml"},
			EnvFile:      "/srv/demo-stack/.env", Template: "image-tag",
			Images: []control.AdoptImage{{Name: "web", Ref: "registry.example.com/demo/web", EnvVar: "WEB_TAG",
				Services: []string{"web"}, Current: "1.4.2"}}},
		Problems: []control.AdoptProblem{{Code: "not-writable", Message: "sysmin may not write the env file",
			Hint: "Give the account write access."}},
	}
	return fb
}

// adoptAsk walks from the list to the question for the stack in dir and sends it.
func adoptAsk(t *testing.T, m *Model, dir string) {
	t.Helper()
	send(m, "n", "down", "enter")
	if !m.projectPromptOpen() || m.prj.prompt.kind != promptAdopt {
		t.Fatalf("the adoption asks no directory: screen = %v\n%s", m.screen, plain(m))
	}
	typed(m, dir)
	send(m, "enter", "enter")
}

func TestAdoptShowsTheProposalAndFillsTheSetup(t *testing.T) {
	fb := adoptFake()
	m := projectsModel(t, fb, 120, 40)
	adoptAsk(t, m, "/srv/demo-stack")
	if len(fb.proj.adoptAsked) != 1 || fb.proj.adoptAsked[0] != (control.AdoptParams{Dir: "/srv/demo-stack", DryRun: true}) {
		t.Fatalf("requests = %+v", fb.proj.adoptAsked)
	}
	view := plain(m)
	for _, want := range []string{"Adopt demo-stack", "demo-stack", "Compose file: /srv/demo-stack/compose.yaml", "Compose file: /srv/demo-stack/compose.override.yaml",
		"registry.example.com/demo/web (1.4.2)", "sysmin may not write the env file", "take over"} {
		if !strings.Contains(view, want) {
			t.Errorf("the proposal lacks %q:\n%s", want, view)
		}
	}
	if m.prj.flow != nil {
		t.Fatal("the proposal opened the setup before it was taken over")
	}
	send(m, "enter")
	f := m.prj.flow
	if f == nil || f.revision != "" || f.draft.name != "demo-stack" || f.draft.dir != "/srv/demo-stack" ||
		len(f.draft.composeFiles) != 2 || f.draft.images[0].ref != "registry.example.com/demo/web" {
		t.Fatalf("the setup is not filled in: %+v", f)
	}
	if len(fb.proj.saved) != 0 {
		t.Fatal("taking over saved the project")
	}
	for range 6 {
		send(m, "f2")
	}
	flowAtStep(t, m, flowSummary)
	if !strings.Contains(plain(m), "compose.override.yaml") {
		t.Errorf("summary:\n%s", plain(m))
	}
	send(m, "f2")
	if len(fb.proj.saved) != 1 || fb.proj.saved[0].Name != "demo-stack" || fb.proj.saved[0].Revision != "" ||
		!strings.Contains(fb.proj.saved[0].File, "compose.override.yaml") || fb.count("add demo-stack") != 1 {
		t.Fatalf("saved = %+v, calls = %v, fail = %q", fb.proj.saved, fb.log(), m.fail)
	}
	for _, p := range fb.proj.adoptAsked {
		if !p.DryRun {
			t.Errorf("a request without DryRun: %+v", p)
		}
	}
}

func TestAdoptFromTheProposalFieldsWhenTheFileIsMissing(t *testing.T) {
	res := adoptFake().proj.adopt
	p := projectOfAdoption(res)
	if p.Name != "demo-stack" || p.Template != "image-tag" || len(p.Images) != 1 || p.Images[0].EnvVar != "WEB_TAG" ||
		len(p.ComposeFiles) != 2 || p.EnvFile != "/srv/demo-stack/.env" {
		t.Errorf("project = %+v", p)
	}
}

func TestAdoptBlockingProblemsPreventTakingOver(t *testing.T) {
	fb := adoptFake()
	fb.proj.adopt.Problems = []control.AdoptProblem{{Code: "name-taken", Message: "a project named demo-stack exists already",
		Hint: "Choose another name.", Blocking: true}}
	m := projectsModel(t, fb, 80, 24)
	adoptAsk(t, m, "/srv/demo-stack")
	view := plain(m)
	for _, want := range []string{"Blocking problems keep this stack from being taken over.", "a project named demo-stack exists already"} {
		if !strings.Contains(view, want) {
			t.Errorf("the dialog lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "take over ") && strings.Contains(view, "enter take over") {
		t.Errorf("the dialog offers to take over:\n%s", view)
	}
	send(m, "enter")
	if m.prj.flow != nil || !m.projectsDialogOpen() {
		t.Fatalf("enter took a blocked stack over: flow = %v", m.prj.flow)
	}
	send(m, "esc")
	if m.projectsDialogOpen() || m.prj.flow != nil || m.screen != screenArea {
		t.Errorf("esc: screen = %v", m.screen)
	}
}

func TestAdoptRefusesWhatIsWrongBeforeAskingAndNamesTheFailure(t *testing.T) {
	fb := adoptFake()
	m := projectsModel(t, fb, 120, 40)
	send(m, "n", "down", "enter")
	typed(m, "srv/demo")
	send(m, "enter", "enter")
	if len(fb.proj.adoptAsked) != 0 || m.fail != tr(tFlowAbsolute) || !m.projectPromptOpen() {
		t.Fatalf("a relative directory: requests = %v, fail = %q", fb.proj.adoptAsked, m.fail)
	}
	send(m, "up", "ctrl+u")
	typed(m, "/srv/demo-stack")
	send(m, "enter")
	typed(m, "Bad Name")
	send(m, "enter")
	if len(fb.proj.adoptAsked) != 0 || m.fail != tr(tPrjBadName) {
		t.Fatalf("a bad name: requests = %v, fail = %q", fb.proj.adoptAsked, m.fail)
	}
	send(m, "ctrl+u")
	typed(m, "demo-stack")
	fb.proj.adoptErr = &control.Error{Code: control.CodeDockerUnavailable}
	send(m, "enter")
	if m.fail != tr(tPrjNoDocker) || !m.projectPromptOpen() || len(fb.proj.adoptAsked) != 1 ||
		fb.proj.adoptAsked[0] != (control.AdoptParams{Dir: "/srv/demo-stack", Name: "demo-stack", DryRun: true}) {
		t.Fatalf("a refused request: fail = %q, requests = %+v", m.fail, fb.proj.adoptAsked)
	}
	fb.proj.adoptErr = &control.Error{Code: control.CodeNotFound}
	send(m, "enter")
	if m.fail != tr(tPrjNoDir) {
		t.Errorf("fail = %q", m.fail)
	}
	fb.proj.adoptErr = &control.Error{Code: control.CodeNoAccess, Paths: []string{"/srv/demo"}}
	send(m, "enter")
	if want := tr(tPrjNoAccess, "/srv/demo"); m.fail != want || strings.Contains(m.fail, "does not exist") ||
		!strings.Contains(m.fail, "may not pass /srv/demo") || !strings.Contains(m.fail, "sudo setfacl -m u:sysmin:x /srv/demo") {
		t.Errorf("no access: fail = %q", m.fail)
	}
	send(m, "esc")
	if m.projectPromptOpen() || m.prj.prompt != nil {
		t.Error("esc keeps the panel")
	}
}

func TestAdoptNoAccessTextInGerman(t *testing.T) {
	useLanguage(t, i18n.German)
	err := &control.Error{Code: control.CodeNoAccess, Paths: []string{"/srv/demo"}}
	got := projectAdoptError(err)
	if !strings.Contains(got, "/srv/demo nicht durchqueren") || !strings.Contains(got, "sudo setfacl -m u:sysmin:x /srv/demo") ||
		strings.Contains(got, "gibt es nicht") {
		t.Errorf("German no-access text = %q", got)
	}
}

func TestAdoptIsRefusedWhileASetupIsOpen(t *testing.T) {
	m := projectsModel(t, adoptFake(), 120, 40)
	send(m, "n", "enter")
	typed(m, "demo-app")
	send(m, "down")
	runCommand(m, "adopt stack")
	if m.pop == nil || m.prj.flow == nil || m.prj.flow.draft.name != "demo-app" || !strings.Contains(plain(m), "adopt stack: Close the guided setup first") {
		t.Fatalf("the command is not refused while a setup is open: screen = %v\n%s", m.screen, plain(m))
	}
	send(m, "esc", "esc")
	send(m, "d")
	runCommand(m, "adopt stack")
	if !m.projectPromptOpen() {
		t.Errorf("after discard:\n%s", plain(m))
	}
}

func TestAdoptGerman(t *testing.T) {
	m := projectsModel(t, adoptFake(), 120, 40)
	useLanguage(t, "de")
	send(m, "n")
	if view := plain(m); !strings.Contains(view, "Laufenden Stack übernehmen") {
		t.Errorf("German picker:\n%s", view)
	}
	send(m, "down", "enter")
	if view := plain(m); !strings.Contains(view, "Verzeichnis") || !strings.Contains(view, "Name (optional)") {
		t.Errorf("German panel:\n%s", view)
	}
}

// utcZone shows the times of the list in UTC for a frame that is kept.
func utcZone(t *testing.T) {
	t.Helper()
	zone := projectZone
	projectZone = time.UTC
	t.Cleanup(func() { projectZone = zone })
}

func TestGoldenProjectsAdopt(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			utcZone(t)
			m := goldenApp(t, adoptFake(), size[0], size[1])
			send(m, "2")
			adoptAsk(t, m, "/srv/demo-stack")
			assertViewFits(t, plain(m), size[0], size[1])
			assertGolden(t, fmt.Sprintf("projects_adopt_%dx%d", size[0], size[1]), m.render())
		})
		t.Run(fmt.Sprintf("panel_%dx%d", size[0], size[1]), func(t *testing.T) {
			utcZone(t)
			m := goldenApp(t, adoptFake(), size[0], size[1])
			send(m, "2", "n", "down", "enter")
			typed(m, "/srv/demo-stack")
			assertViewFits(t, plain(m), size[0], size[1])
			assertGolden(t, fmt.Sprintf("projects_adopt_panel_%dx%d", size[0], size[1]), m.render())
		})
	}
}
