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

// adoptAsk walks from the list through the stacks (none) to the question for the stack in dir and sends it.
func adoptAsk(t *testing.T, m *Model, dir string) {
	t.Helper()
	send(m, "n", "down", "enter", "enter")
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
	send(m, "n", "down", "enter", "enter")
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
	if !m.projectsDialogOpen() || m.prj.dialog.picker == nil || !strings.Contains(plain(m), "Running stacks") {
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
	for _, want := range []string{"Laufende Stacks", "Keine laufenden Compose-Stacks gefunden", "Anderes Verzeichnis…"} {
		if view := plain(m); !strings.Contains(view, want) {
			t.Errorf("German empty list lacks %q:\n%s", want, view)
		}
	}
	send(m, "esc")
	fb := adoptFake()
	fb.proj.stacks = demoStacks()
	m = projectsModel(t, fb, 120, 40)
	useLanguage(t, "de")
	send(m, "n", "down", "enter")
	for _, want := range []string{"Container: 3", "verwaltet von shop-prod", "Anderes Verzeichnis…"} {
		if view := plain(m); !strings.Contains(view, want) {
			t.Errorf("German list lacks %q:\n%s", want, view)
		}
	}
	send(m, "esc")
	fb.proj.stacks, fb.proj.stacksErr = control.StacksResult{}, &control.Error{Code: control.CodeDockerUnavailable}
	send(m, "n", "down", "enter")
	if view := flatBoxText(plain(m)); !strings.Contains(view, "Docker kann nicht nach den laufenden Containern gefragt werden") ||
		!strings.Contains(view, "später erneut versuchen") {
		t.Errorf("German failure:\n%s", view)
	}
	send(m, "down", "enter")
	if view := plain(m); !strings.Contains(view, "Verzeichnis") || !strings.Contains(view, "Name (optional)") {
		t.Errorf("German panel:\n%s", view)
	}
}

// demoStacks is two running stacks: one no project manages and one a project manages.
func demoStacks() control.StacksResult {
	return control.StacksResult{Stacks: []control.Stack{
		{ComposeProject: "demo", Dir: "/srv/demo-stack", ComposeFiles: []string{"/srv/demo-stack/compose.yaml"}, Containers: 2},
		{ComposeProject: "shop", Dir: "/srv/shop-prod/deploy/current/releases/2026", Containers: 3, ManagedBy: "shop-prod"},
	}}
}

func stacksFake() *fakeBackend {
	fb := adoptFake()
	fb.proj.stacks = demoStacks()
	return fb
}

func TestAdoptListsTheRunningStacksAndTakesTheChosenOne(t *testing.T) {
	fb := stacksFake()
	m := projectsModel(t, fb, 120, 40)
	send(m, "n", "down", "enter")
	view := plain(m)
	for _, want := range []string{"Running stacks", "demo  /srv/demo-stack  containers: 2", "managed by shop-prod", "Other directory…", "1/3"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list lacks %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "demo  /srv") > strings.Index(view, "shop  ") {
		t.Errorf("the order is not the one delivered:\n%s", view)
	}
	if m.projectPromptOpen() || len(fb.proj.adoptAsked) != 0 {
		t.Fatal("the list opened the panel or asked for a proposal before a choice")
	}
	send(m, "/")
	typed(m, "shop")
	if view := plain(m); strings.Contains(view, "demo-stack") || !strings.Contains(view, "Other directory…") {
		t.Errorf("filter shop:\n%s", view)
	}
	send(m, "enter", "enter")
	if len(fb.proj.adoptAsked) != 1 || fb.proj.adoptAsked[0] != (control.AdoptParams{Dir: "/srv/shop-prod/deploy/current/releases/2026", Name: "shop", DryRun: true}) {
		t.Fatalf("requests = %+v", fb.proj.adoptAsked)
	}
	if view := plain(m); !strings.Contains(view, "Adopt demo-stack") || !strings.Contains(view, "take over") {
		t.Errorf("no proposal:\n%s", view)
	}
	send(m, "esc")
	send(m, "n", "down", "enter", "enter")
	if len(fb.proj.adoptAsked) != 2 || fb.proj.adoptAsked[1].Dir != "/srv/demo-stack" {
		t.Fatalf("requests = %+v", fb.proj.adoptAsked)
	}
}

func TestAdoptListLeadsToThePanelThroughOtherDirectory(t *testing.T) {
	fb := stacksFake()
	m := projectsModel(t, fb, 120, 40)
	runCommand(m, "adopt stack")
	if !strings.Contains(plain(m), "Running stacks") {
		t.Fatalf("the command does not list the stacks:\n%s", plain(m))
	}
	send(m, "/")
	typed(m, "zzz")
	send(m, "enter", "enter")
	if !m.projectPromptOpen() || m.prj.prompt.kind != promptAdopt || len(fb.proj.adoptAsked) != 0 {
		t.Fatalf("other directory does not open the panel:\n%s", plain(m))
	}
}

func TestAdoptListSaysWhyItIsEmptyIncompleteOrFailed(t *testing.T) {
	cases := []struct {
		name   string
		stacks control.StacksResult
		err    error
		want   string
	}{
		{"empty", control.StacksResult{}, nil, "No running Compose stacks found"},
		{"truncated", control.StacksResult{Stacks: demoStacks().Stacks, Truncated: true}, nil, "list may be incomplete"},
		{"docker", control.StacksResult{}, &control.Error{Code: control.CodeDockerUnavailable}, "Docker cannot be asked"},
		{"timeout", control.StacksResult{}, &control.Error{Code: control.CodeTimeout}, "took too long"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fb := adoptFake()
			fb.proj.stacks, fb.proj.stacksErr = c.stacks, c.err
			m := projectsModel(t, fb, 120, 40)
			send(m, "n", "down", "enter")
			view := plain(m)
			flat := flatBoxText(view)
			if !strings.Contains(flat, c.want) || !strings.Contains(flat, "Other directory…") {
				t.Errorf("view lacks %q:\n%s", c.want, view)
			}
			if c.err != nil && !strings.Contains(flat, "try again later") {
				t.Errorf("no next step:\n%s", view)
			}
			send(m, "enter")
			if c.name != "truncated" {
				if !m.projectPromptOpen() {
					t.Errorf("other directory is not chosen first:\n%s", plain(m))
				}
			}
		})
	}
}

func TestAdoptListShowsHostTextSafely(t *testing.T) {
	fb := adoptFake()
	fb.proj.stacks = control.StacksResult{Stacks: []control.Stack{
		{ComposeProject: "demo\x1b[31m", Dir: "/srv/demo\x1b]0;x\x07-stack", Containers: 1, ManagedBy: "shop\x1b[2Jprod"}}}
	m := projectsModel(t, fb, 120, 40)
	send(m, "n", "down", "enter")
	if view := m.render(); strings.Contains(view, "\x1b[2J") || strings.Contains(view, "\x1b]0") || strings.Contains(view, "\a") {
		t.Errorf("the list passes host text on:\n%q", view)
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
			send(m, "2", "n", "down", "enter", "enter")
			typed(m, "/srv/demo-stack")
			assertViewFits(t, plain(m), size[0], size[1])
			assertGolden(t, fmt.Sprintf("projects_adopt_panel_%dx%d", size[0], size[1]), m.render())
		})
	}
}

func TestGoldenProjectsAdoptStacks(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			utcZone(t)
			m := goldenApp(t, stacksFake(), size[0], size[1])
			send(m, "2", "n", "down", "enter")
			assertViewFits(t, plain(m), size[0], size[1])
			assertGolden(t, fmt.Sprintf("projects_adopt_stacks_%dx%d", size[0], size[1]), m.render())
		})
	}
}

// flatBoxText is the text of a view with the frame characters removed and the blanks joined, so a message the
// dialog wrapped reads as one line.
func flatBoxText(view string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ").Replace(view)), " ")
}
