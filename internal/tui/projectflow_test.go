package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// flowModel is the Projects area with the guided setup of a new project open.
func flowModel(t *testing.T, fb *fakeBackend, w, h int) *Model {
	t.Helper()
	m := projectsModel(t, fb, w, h)
	send(m, "n")
	if m.prj.flow == nil {
		t.Fatal("n does not open the guided setup")
	}
	return m
}

// flowToRow moves the focus to the row id by the keys a person uses: to the first row, then down.
func flowToRow(t *testing.T, m *Model, id string) {
	t.Helper()
	send(m, "pgup")
	for range 60 {
		if m.prj.flow.focus == id {
			return
		}
		send(m, "down")
	}
	t.Fatalf("row %q not reached; the focus is on %q", id, m.prj.flow.focus)
}

// flowFill types text into the row id.
func flowFill(t *testing.T, m *Model, id, text string) {
	t.Helper()
	flowToRow(t, m, id)
	typed(m, text)
}

// flowOpenAdvanced opens the advanced rows of step s.
func flowOpenAdvanced(t *testing.T, m *Model, s flowStepID) {
	t.Helper()
	flowToRow(t, m, fmt.Sprintf("advanced/%d", s))
	send(m, "enter")
}

// flowAtStep fails unless the setup shows step s.
func flowAtStep(t *testing.T, m *Model, s flowStepID) {
	t.Helper()
	if m.prj.flow == nil || m.prj.flow.step != s {
		t.Fatalf("the setup is not at step %d: %+v\n%s", s, m.prj.flow, plain(m))
	}
}

// flowMinimal fills the rows a valid image-tag project needs and goes to the summary.
func flowMinimal(t *testing.T, m *Model) {
	t.Helper()
	typed(m, "demo-app")
	send(m, "f2")
	flowFill(t, m, "images[0].name", "app")
	flowFill(t, m, "images[0].ref", "registry.example.com/demo/app")
	send(m, "f2")
	flowFill(t, m, "dir", "/srv/demo/app")
	send(m, "f2", "f2", "f2", "f2")
	flowAtStep(t, m, flowSummary)
}

func flowReadFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The setup of an image-tag project sends the file `sysmin project add demo-app --file
// testdata/flow_image-tag.yaml` sends; the CLI test of the same file proves the other half.
func TestFlowImageTagSendsTheFileOfTheCLI(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := flowModel(t, fb, 120, 40)
	typed(m, "demo-app")
	send(m, "f2")
	flowAtStep(t, m, flowSource)
	flowFill(t, m, "images[0].name", "app")
	flowFill(t, m, "images[0].ref", "registry.example.com/demo/app")
	flowFill(t, m, "images[0].env_var", "DEMO_APP_TAG")
	flowFill(t, m, "images[0].services", "app, worker")
	flowFill(t, m, "images[0].credential", "demo-registry")
	send(m, "f2")
	flowAtStep(t, m, flowTarget)
	flowFill(t, m, "dir", "/srv/demo/app")
	flowFill(t, m, "env_file", "/srv/demo/app/.env")
	flowFill(t, m, "compose_files[0]", "/srv/demo/app/compose.yaml")
	flowOpenAdvanced(t, m, flowTarget)
	flowFill(t, m, "compose_project", "demo-app")
	flowFill(t, m, "poll_interval", "30m")
	send(m, "f2")
	flowAtStep(t, m, flowRules)
	flowToRow(t, m, "images[0].version_rule.mode")
	send(m, "enter", "down", "down", "enter")
	flowToRow(t, m, "images[0].version_rule.max_update")
	send(m, "right", "right")
	flowOpenAdvanced(t, m, flowRules)
	flowToRow(t, m, "images[0].version_rule.major_steps")
	send(m, "right", "right")
	flowFill(t, m, "images[0].version_rule.tag_filter", `^\d+\.\d+\.\d+$`)
	flowFill(t, m, "images[0].version_rule.changelog_repo", "example/demo-app")
	flowFill(t, m, "images[0].version_rule.changelog_credential", "demo-github")
	send(m, "f2")
	flowAtStep(t, m, flowChecks)
	flowToRow(t, m, "backup.mode")
	send(m, "right")
	flowFill(t, m, "backup.volume", "demo-app-data")
	flowOpenAdvanced(t, m, flowChecks)
	flowFill(t, m, "backup.keep", "3")
	flowToRow(t, m, "checks+http")
	send(m, "enter")
	if m.prj.flow.focus != "checks[0].url" {
		t.Fatalf("the added check has no focus: %q", m.prj.flow.focus)
	}
	typed(m, "https://demo.example.com/health")
	flowFill(t, m, "checks[0].status", "200")
	flowFill(t, m, "checks[0].contains", "ok")
	flowToRow(t, m, "checks+container")
	send(m, "enter")
	typed(m, "app")
	send(m, "f2")
	flowAtStep(t, m, flowNotify)
	flowFill(t, m, "notify.channels", "ops-mail")
	flowToRow(t, m, "notify.events/update-succeeded")
	send(m, "space")
	flowToRow(t, m, "notify.events/update-failed")
	send(m, "space")
	flowOpenAdvanced(t, m, flowNotify)
	flowToRow(t, m, "webhook.kind")
	send(m, "right")
	flowFill(t, m, "webhook.secret", "demo-hook")
	send(m, "f2")
	flowAtStep(t, m, flowSummary)
	if fb.count("add demo-app") != 0 {
		t.Fatal("the setup saved before the summary")
	}
	view := plain(m)
	for _, want := range []string{"demo-app", "registry.example.com/demo/app", "/srv/demo/app", "ops-mail"} {
		if !strings.Contains(view, want) {
			t.Errorf("summary lacks %q:\n%s", want, view)
		}
	}
	send(m, "f2")
	want := control.ProjectParams{Name: "demo-app", File: flowReadFile(t, "flow_image-tag.yaml")}
	if len(fb.proj.saved) != 1 || fb.proj.saved[0].Name != want.Name || fb.proj.saved[0].File != want.File ||
		fb.proj.saved[0].Revision != "" {
		t.Fatalf("request = %+v\nwant file:\n%s", fb.proj.saved, want.File)
	}
	if m.prj.flow != nil || !strings.Contains(m.status, "demo-app") {
		t.Errorf("after the save: flow = %v, status = %q, fail = %q", m.prj.flow, m.status, m.fail)
	}
}

func TestFlowGitTagSendsTheFileOfTheCLI(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := flowModel(t, fb, 80, 24)
	typed(m, "demo-site")
	send(m, "down", "left")
	if got := m.prj.flow.draft.scenario; got != flowGitTag {
		t.Fatalf("scenario = %q", got)
	}
	send(m, "f2")
	flowAtStep(t, m, flowSource)
	if m.prj.flow.focus != "git.url" {
		t.Fatalf("the git-tag scenario asks for %q first", m.prj.flow.focus)
	}
	typed(m, "https://git.example.com/demo/site.git")
	send(m, "enter")
	typed(m, "demo-git")
	send(m, "f2")
	flowFill(t, m, "dir", "/srv/demo/site")
	send(m, "f2")
	flowAtStep(t, m, flowRules)
	flowToRow(t, m, "git.version_rule.mode")
	send(m, "enter", "down", "down", "enter")
	send(m, "f2")
	flowToRow(t, m, "checks+http")
	send(m, "enter")
	typed(m, "https://site.example.com/")
	send(m, "f2", "f2")
	flowAtStep(t, m, flowSummary)
	send(m, "f2")
	want := flowReadFile(t, "flow_git-tag.yaml")
	if len(fb.proj.saved) != 1 || fb.proj.saved[0].Name != "demo-site" || fb.proj.saved[0].File != want {
		t.Fatalf("request = %+v\nwant file:\n%s", fb.proj.saved, want)
	}
}

func TestFlowEscAsksBeforeUnsavedInputIsLost(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := flowModel(t, fb, 120, 40)

	// Nothing typed: esc leaves at once.
	send(m, "esc")
	if m.prj.flow != nil || m.screen != screenArea {
		t.Fatalf("an untouched setup does not close: screen = %v", m.screen)
	}

	// esc on the question keeps editing, with the input.
	send(m, "n")
	typed(m, "demo-app")
	send(m, "esc")
	if m.screen != screenConfirm || m.leave == nil {
		t.Fatalf("esc with input does not ask: screen = %v", m.screen)
	}
	view := plain(m)
	for _, want := range []string{"F2 save", "d discard", "esc keep editing"} {
		if !strings.Contains(view, want) {
			t.Errorf("question lacks %q:\n%s", want, view)
		}
	}
	send(m, "esc")
	if m.screen != screenArea || m.prj.flow == nil || m.prj.flow.draft.name != "demo-app" {
		t.Fatalf("esc on the question: screen = %v, flow = %+v", m.screen, m.prj.flow)
	}

	// d drops the input and leaves.
	send(m, "esc", "d")
	if m.prj.flow != nil || m.screen != screenArea || fb.count("add demo-app") != 0 {
		t.Fatalf("d: flow = %v, screen = %v, calls = %v", m.prj.flow, m.screen, fb.log())
	}

	// F2 saves; an incomplete project stays open with its problems.
	send(m, "n")
	typed(m, "demo-app")
	send(m, "esc", "f2")
	if m.prj.flow == nil || fb.count("add demo-app") != 0 || m.fail == "" {
		t.Fatalf("F2 on an incomplete project: flow = %v, fail = %q, calls = %v", m.prj.flow, m.fail, fb.log())
	}
	if m.prj.flow.draft.name != "demo-app" || len(m.prj.flow.problems["dir"]) == 0 {
		t.Errorf("the problems of the setup: %+v", m.prj.flow.problems)
	}

	// F2 on a complete project saves it.
	m.prj.flow.show(flowSource)
	flowFill(t, m, "images[0].name", "app")
	flowFill(t, m, "images[0].ref", "registry.example.com/demo/app")
	send(m, "f2")
	flowFill(t, m, "dir", "/srv/demo/app")
	send(m, "esc", "esc", "esc")
	if m.screen != screenConfirm {
		t.Fatalf("esc from the first step does not ask: screen = %v, step = %v", m.screen, m.prj.flow.step)
	}
	send(m, "f2")
	if fb.count("add demo-app") != 1 || m.prj.flow != nil {
		t.Fatalf("F2 on the question: calls = %v, flow = %v, fail = %q", fb.log(), m.prj.flow, m.fail)
	}
}

func TestFlowAreaChangeAndQuitAsk(t *testing.T) {
	m := flowModel(t, projectsFake(), 120, 40)
	typed(m, "demo-app")
	send(m, "down")
	command(m, "overview")
	if m.screen != screenConfirm || m.area != areaProjects {
		t.Fatalf("an area change does not ask: screen = %v, area = %v", m.screen, m.area)
	}
	send(m, "d")
	if m.area != areaOverview {
		t.Fatalf("discard does not go on to the area: %v", m.area)
	}
	m = flowModel(t, projectsFake(), 120, 40)
	typed(m, "q")
	send(m, "ctrl+c")
	if m.quitting || m.screen != screenConfirm {
		t.Fatalf("ctrl+c with input: quitting = %v, screen = %v", m.quitting, m.screen)
	}
	if got := m.prj.flow.draft.name; got != "q" {
		t.Errorf("q in a text row is text, name = %q", got)
	}
}

func TestFlowEnterNeverSaves(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := flowModel(t, fb, 120, 40)
	flowMinimal(t, m)
	for _, s := range m.prj.flow.stepOrder() {
		m.prj.flow.show(s)
		for range 12 {
			send(m, "enter")
			if m.dd != nil {
				send(m, "esc")
			}
		}
	}
	m.prj.flow.show(flowSummary)
	send(m, "enter", "enter")
	if n := len(fb.proj.saved); n != 0 || m.prj.flow == nil || m.prj.flow.step != flowSummary {
		t.Fatalf("enter saved %d time(s): %v", n, fb.log())
	}
}

func TestFlowProblemsStandUnderTheirRow(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	fb.proj.saveErr = &control.Error{Code: control.CodeInvalid, Problems: []control.Problem{
		{Field: "images[0].ref", Code: "invalid-value", Message: "reference must be a repository without tag", Hint: "Correct the value."},
		{Field: "template.steps[1]", Code: "missing-env-file", Message: "set-env needs the project to set env_file", Hint: "Set env_file to an absolute path."},
		{Field: "", Code: "invalid-file", Message: "the file does not read", Hint: "Pass a project file."},
	}}
	m := flowModel(t, fb, 120, 40)
	flowMinimal(t, m)
	send(m, "f2")
	if len(fb.proj.saved) != 1 {
		t.Fatalf("calls = %v", fb.log())
	}
	f := m.prj.flow
	if f == nil {
		t.Fatal("a refused save closes the setup")
	}
	flowAtStep(t, m, flowSource)
	if f.focus != "images[0].ref" || f.draft.images[0].ref != "registry.example.com/demo/app" {
		t.Errorf("focus = %q, ref = %q", f.focus, f.draft.images[0].ref)
	}
	lines := strings.Split(plain(m), "\n")
	found := false
	for i, l := range lines {
		if strings.Contains(l, "Reference:") && i+1 < len(lines) &&
			strings.Contains(lines[i+1], "reference must be a repository without tag Correct the value.") {
			found = true
		}
	}
	if !found {
		t.Errorf("the problem does not stand under its row:\n%s", plain(m))
	}
	if !strings.Contains(plain(m), "Source [!]") || !strings.Contains(plain(m), "Target [!]") {
		t.Errorf("the steps with problems are not marked:\n%s", plain(m))
	}
	if len(f.problems["env_file"]) != 1 {
		t.Errorf("the problem of the template step is not under env_file: %+v", f.problems)
	}
	f.show(flowSummary)
	if view := plain(m); !strings.Contains(view, "the file does not read") || !strings.Contains(view, "Problems in: Source, Target") {
		t.Errorf("the summary lacks the problems no row shows:\n%s", view)
	}
	// Changing the row answers its problem.
	f.show(flowSource)
	flowToRow(t, m, "images[0].ref")
	typed(m, "x")
	if len(f.problems["images[0].ref"]) != 0 {
		t.Error("the problem stays after the row changed")
	}
}

func TestFlowWithoutSessionAsksThePassphraseAndSaves(t *testing.T) {
	fb := projectsFake()
	m := flowModel(t, fb, 120, 40)
	flowMinimal(t, m)
	send(m, "f2")
	if m.ov == nil || len(fb.proj.saved) != 0 {
		t.Fatalf("F2 without a session: overlay = %v, calls = %v", m.ov, fb.log())
	}
	typed(m, testPass)
	send(m, "enter")
	if len(fb.proj.saved) != 1 || fb.count("session") != 1 || m.prj.flow != nil {
		t.Fatalf("after the passphrase: calls = %v, flow = %v, fail = %q", fb.log(), m.prj.flow, m.fail)
	}
}

func TestFlowExistingNameStandsUnderTheName(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	fb.proj.saveErr = &control.Error{Code: control.CodeExists}
	m := flowModel(t, fb, 120, 40)
	flowMinimal(t, m)
	send(m, "f2")
	flowAtStep(t, m, flowStart)
	if m.prj.flow.focus != "name" || len(m.prj.flow.problems["name"]) != 1 {
		t.Errorf("focus = %q, problems = %+v", m.prj.flow.focus, m.prj.flow.problems)
	}
}

func TestFlowAdvancedOpensForAValue(t *testing.T) {
	m := flowModel(t, projectsFake(), 120, 40)
	m.prj.flow.show(flowTarget)
	if strings.Contains(plain(m), "Poll interval") {
		t.Fatal("the advanced rows are open while empty")
	}
	m.prj.flow.draft.poll = "1h"
	if !strings.Contains(plain(m), "Poll interval") {
		t.Fatal("the advanced rows stay closed over a value")
	}
}

func TestFlowCustomStepsStartFromTheTemplate(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := flowModel(t, fb, 120, 40)
	typed(m, "demo-app")
	send(m, "down", "right")
	f := m.prj.flow
	if f.draft.scenario != flowCustom || len(f.draft.steps) == 0 {
		t.Fatalf("custom: %+v", f.draft)
	}
	if steps := f.stepOrder(); len(steps) != 8 || steps[3] != flowSteps {
		t.Fatalf("steps = %v", steps)
	}
	f.show(flowSteps)
	flowToRow(t, m, "steps+")
	send(m, "enter")
	n := len(f.draft.steps) - 1
	if f.focus != fmt.Sprintf("steps[%d].type", n) {
		t.Fatalf("the added step has no focus: %q", f.focus)
	}
	flowFill(t, m, fmt.Sprintf("steps[%d].run", n), "echo done")
	f.draft.images[0] = flowImage{name: "app", ref: "registry.example.com/demo/app", envVar: "DEMO_APP_TAG"}
	f.draft.dir, f.draft.envFile = "/srv/demo/app", "/srv/demo/app/.env"
	f.show(flowSummary)
	send(m, "f2")
	if len(fb.proj.saved) != 1 {
		t.Fatalf("calls = %v, fail = %q", fb.log(), m.fail)
	}
	file := fb.proj.saved[0].File
	if strings.Contains(file, "template:") || !strings.Contains(file, "run: echo done") || !strings.Contains(file, "type: set-env") {
		t.Errorf("file:\n%s", file)
	}
}

func TestFlowPathCompletes(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := flowModel(t, projectsFake(), 120, 40)
	m.prj.flow.show(flowTarget)
	typed(m, dir+"/a")
	if !strings.Contains(plain(m), dir+"/app/") {
		t.Errorf("no suggestion:\n%s", plain(m))
	}
	send(m, "tab")
	if got := m.prj.flow.draft.dir; got != dir+"/app/" {
		t.Errorf("tab completes to %q", got)
	}
	send(m, "tab")
	if m.prj.flow.focus != "env_file" {
		t.Errorf("tab without a further suggestion stays on %q", m.prj.flow.focus)
	}
}

func TestFlowFromTheCommandLineAndInfo(t *testing.T) {
	m := appModel(t, projectsFake(), 120, 40)
	command(m, "new project")
	if !m.flowOpen() {
		t.Fatalf("the command does not open the setup: area = %v", m.area)
	}
	send(m, "down", "i")
	if m.screen != screenPopup || !strings.Contains(plain(m), "The steps an update runs") {
		t.Errorf("i on the scenario:\n%s", plain(m))
	}
	send(m, "esc", "up", "i")
	if got := m.prj.flow.draft.name; got != "i" || m.screen != screenArea {
		t.Errorf("i in the name row: name = %q, screen = %v", got, m.screen)
	}
}

func TestFlowGerman(t *testing.T) {
	m := flowModel(t, projectsFake(), 120, 40)
	useLanguage(t, "de")
	view := plain(m)
	for _, want := range []string{"Neues Projekt", "Szenario", "Zusammenfassung", "F2 weiter"} {
		if !strings.Contains(view, want) {
			t.Errorf("German view lacks %q:\n%s", want, view)
		}
	}
}

func TestFlowFitsEverySize(t *testing.T) {
	for _, size := range [][2]int{{40, 14}, {60, 16}, {80, 24}, {99, 30}, {120, 40}, {200, 50}} {
		for _, s := range []flowStepID{flowStart, flowSource, flowTarget, flowRules, flowChecks, flowNotify, flowSummary} {
			t.Run(fmt.Sprintf("%dx%d_%d", size[0], size[1], s), func(t *testing.T) {
				m := flowModel(t, projectsFake(), size[0], size[1])
				m.prj.flow.show(s)
				assertViewFits(t, plain(m), size[0], size[1])
			})
		}
	}
}

func TestGoldenProjectsFlow(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		t.Run(fmt.Sprintf("step_%dx%d", size[0], size[1]), func(t *testing.T) {
			fb := projectsFake()
			m := goldenApp(t, fb, size[0], size[1])
			send(m, "2", "n")
			typed(m, "demo-app")
			send(m, "f2")
			typed(m, "app")
			send(m, "down")
			assertViewFits(t, plain(m), size[0], size[1])
			assertGolden(t, fmt.Sprintf("projects_flow_step_%dx%d", size[0], size[1]), m.render())
		})
		t.Run(fmt.Sprintf("summary_%dx%d", size[0], size[1]), func(t *testing.T) {
			fb := projectsFake()
			m := goldenApp(t, fb, size[0], size[1])
			send(m, "2", "n")
			flowMinimal(t, m)
			assertViewFits(t, plain(m), size[0], size[1])
			assertGolden(t, fmt.Sprintf("projects_flow_summary_%dx%d", size[0], size[1]), m.render())
		})
	}
}

func TestFlowChoiceOpensItsListUnderTheRow(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		m := flowModel(t, projectsFake(), size[0], size[1])
		send(m, "down", "enter")
		view := plain(m)
		if m.dd == nil || !strings.Contains(view, "custom steps") || !strings.Contains(view, "git-tag") {
			t.Fatalf("%v: the list of the scenario is not drawn:\n%s", size, view)
		}
		lines := strings.Split(view, "\n")
		for i, l := range lines {
			if strings.Contains(l, "Scenario:") && (i+2 >= len(lines) || !strings.Contains(lines[i+2], "git-tag")) {
				t.Errorf("%v: the list does not stand under its row:\n%s", size, view)
			}
		}
		send(m, "up", "enter")
		if got := m.prj.flow.draft.scenario; got != flowGitTag {
			t.Errorf("%v: scenario = %q", size, got)
		}
	}
}

func TestFlowClickFocusesThenActs(t *testing.T) {
	m := flowModel(t, projectsFake(), 120, 40)
	m.render()
	var scenario *hit
	for i := range m.hits {
		if h := m.hits[i]; h.x0 > 0 && h.y0 > 6 && (scenario == nil || h.y0 > scenario.y0) {
			scenario = &m.hits[i]
		}
	}
	if scenario == nil {
		t.Fatal("no row target")
	}
	scenario.act()
	if m.prj.flow.focus != "template" || m.dd != nil {
		t.Fatalf("first click: focus = %q", m.prj.flow.focus)
	}
	scenario.act()
	if m.dd == nil {
		t.Error("a click on the focused choice does not open its list")
	}
}

func TestFlowNewSetupAsksAboutTheOpenOne(t *testing.T) {
	m := flowModel(t, projectsFake(), 120, 40)
	typed(m, "demo-app")
	send(m, "down")
	command(m, "new project")
	if m.screen != screenConfirm || m.prj.flow.draft.name != "demo-app" {
		t.Fatalf("a new setup drops the open one: screen = %v", m.screen)
	}
	send(m, "d")
	if m.prj.flow == nil || m.prj.flow.draft.name != "" {
		t.Errorf("discard does not open a new setup: %+v", m.prj.flow)
	}
}
