package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

func projectsFake() *fakeBackend {
	fb := newFake(control.VaultUnlocked)
	fb.addProject("demo-app", control.StateApproved)
	fb.addProject("demo-db", control.StatePending)
	fb.addProject("sample-job", control.StateApproved)
	return fb
}

// projectsModel is the interface at the Projects area, loaded.
func projectsModel(t *testing.T, fb *fakeBackend, w, h int) *Model {
	t.Helper()
	zone := projectZone
	projectZone = time.UTC
	t.Cleanup(func() { projectZone = zone })
	m := appModel(t, fb, w, h)
	send(m, "2")
	if m.area != areaProjects {
		t.Fatalf("area = %v, want Projects", m.area)
	}
	return m
}

func command(m *Model, name string) {
	send(m, ":")
	typed(m, name)
	send(m, "enter")
}

func TestProjectsListShowsTheColumns(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 30)
	view := plain(m)
	assertViewFits(t, view, 120, 30)
	for _, want := range []string{"Projects (3)", "Name", "Source", "Current", "Available", "Mode", "State", "Last run",
		"demo-app", "images", "1.0.0", "2.0.0", "auto", "● approved", "⚠ pending", "2026-10-29 12:00 succeeded", "1/3"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q:\n%s", want, view)
		}
	}
	if got := m.prj.gen; got != 1 {
		t.Errorf("gen = %d, want one load", got)
	}
}

func TestProjectsLastRunIsAShortCellAndGivesWayFirst(t *testing.T) {
	m := projectsModel(t, projectsFake(), 80, 24)
	view := plain(m)
	assertViewFits(t, view, 80, 24)
	for _, want := range []string{"Last run", "✓ 10-29 12:00", "State", "Name"} {
		if !strings.Contains(view, want) {
			t.Errorf("80 columns lack %q:\n%s", want, view)
		}
	}
	m = projectsModel(t, projectsFake(), 150, 30)
	if view := plain(m); !strings.Contains(view, "✓ 10-29 12:00") {
		t.Errorf("a wide list lacks the last run:\n%s", view)
	}
	m = projectsModel(t, projectsFake(), 64, 24)
	view = plain(m)
	assertViewFits(t, view, 64, 24)
	if strings.Contains(view, "Last run") || !strings.Contains(view, "State") || !strings.Contains(view, "Name") {
		t.Errorf("the columns do not give way in order:\n%s", view)
	}
}

func TestProjectsRowFailureIsAFixedText(t *testing.T) {
	fb := projectsFake()
	fb.proj.statusErr = map[string]error{"demo-db": &control.Error{Code: control.CodeInternal, Problems: nil}}
	m := projectsModel(t, fb, 120, 30)
	view := plain(m)
	if !strings.Contains(view, "○ no data") {
		t.Errorf("a failed row is not marked:\n%s", view)
	}
}

func TestProjectsMarkWithSpace(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 30)
	send(m, "space", "space")
	if got := m.prj.markedProjects(); strings.Join(got, ",") != "demo-app,demo-db" {
		t.Fatalf("marked = %v", got)
	}
	if view := plain(m); !strings.Contains(view, "✓ demo-app") || !strings.Contains(view, "✓ demo-db") {
		t.Errorf("the marks are not shown:\n%s", view)
	}
	send(m, "up", "up", "space")
	if got := m.prj.markedProjects(); strings.Join(got, ",") != "demo-db" {
		t.Errorf("marked after unmarking = %v", got)
	}
}

func TestProjectsFilter(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 30)
	send(m, "/")
	typed(m, "sample")
	if got := len(m.prj.list.matches); got != 1 {
		t.Fatalf("matches = %d", got)
	}
	if view := plain(m); !strings.Contains(view, "1/1 (3 total)") {
		t.Errorf("no filtered position:\n%s", view)
	}
	send(m, "esc")
	if len(m.prj.list.matches) != 3 {
		t.Error("esc does not clear the filter")
	}
}

func TestProjectsDetailBesideTheListAndRawHidesSecrets(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 40)
	view := plain(m)
	for _, want := range []string{"Overview", "Details", "Raw", "● approved", "app", "1.0.0 → 2.0.0 (2 newer)"} {
		if !strings.Contains(view, want) {
			t.Errorf("overview lacks %q:\n%s", want, view)
		}
	}
	send(m, "right")
	if view := plain(m); !strings.Contains(view, "registry.example.com/demo/app") || !strings.Contains(view, "demo-registry") || !strings.Contains(view, "rev-demo-app") {
		t.Errorf("details lack the image:\n%s", view)
	}
	send(m, "right")
	view = plain(m)
	if !strings.Contains(view, "schema: 1") || !strings.Contains(view, "secret: [redacted]") {
		t.Errorf("raw lacks the file:\n%s", view)
	}
	for _, leak := range []string{"abcd-example-token", "demo-hook"} {
		if strings.Contains(view, leak) {
			t.Errorf("raw shows %q:\n%s", leak, view)
		}
	}
	if !strings.Contains(view, "[redacted]") {
		t.Errorf("raw does not mark the hidden values:\n%s", view)
	}
}

func TestProjectsDetailLayerBelow120Columns(t *testing.T) {
	m := projectsModel(t, projectsFake(), 100, 30)
	if strings.Contains(plain(m), "Raw") {
		t.Fatal("the detail pane stands beside a list of 100 columns")
	}
	send(m, "enter")
	view := plain(m)
	if !m.prj.focusDetail || !strings.Contains(view, "Raw") || strings.Contains(view, "Projects (3)") {
		t.Errorf("enter does not open the detail:\n%s", view)
	}
	send(m, "right")
	if m.prj.tab != projTabDetails {
		t.Errorf("tab = %v", m.prj.tab)
	}
	send(m, "esc")
	if m.prj.focusDetail || !strings.Contains(plain(m), "Projects (3)") {
		t.Error("esc does not return to the list")
	}
}

func TestProjectsSourceTestShowsAPanel(t *testing.T) {
	fb := projectsFake()
	m := projectsModel(t, fb, 100, 30)
	send(m, "t")
	if fb.count("sources demo-app") != 1 {
		t.Fatalf("calls = %v", fb.log())
	}
	view := plain(m)
	if m.screen != screenConfirm || !strings.Contains(view, "Sources of demo-app") || !strings.Contains(view, "1.0.0 → 2.0.0 (2 newer)") {
		t.Errorf("no result panel:\n%s", view)
	}
	send(m, "esc")
	if m.screen != screenArea || m.prj.dialog != nil {
		t.Error("esc does not close the panel")
	}
}

func TestProjectsControlsRunThroughTheCommandLine(t *testing.T) {
	fb := projectsFake()
	m := projectsModel(t, fb, 120, 30)
	command(m, "pause")
	command(m, "pin")
	if got := fb.log(); !strings.Contains(strings.Join(got, "|"), control.OpDeployPause+" demo-app") ||
		!strings.Contains(strings.Join(got, "|"), control.OpDeployPin+" demo-app") {
		t.Fatalf("calls = %v", got)
	}
	if view := plain(m); !strings.Contains(view, "auto, paused, pinned") {
		t.Errorf("the mode does not show the controls:\n%s", view)
	}
	command(m, "resume")
	command(m, "unpin")
	if view := plain(m); !strings.Contains(view, "unpinned") || strings.Contains(view, "paused,") {
		t.Errorf("the controls are not undone:\n%s", view)
	}
	if fb.count("session") != 0 {
		t.Error("a control asked for the admin session")
	}
}

func TestProjectsRunNowAsksFirst(t *testing.T) {
	fb := projectsFake()
	fb.proj.runNow = control.DeployRunNowResult{Run: &control.DeployRun{Result: "succeeded"}}
	m := projectsModel(t, fb, 120, 30)
	command(m, "run")
	if fb.count("run demo-app") != 0 || m.screen != screenConfirm {
		t.Fatal("run-now started without the question")
	}
	send(m, "n")
	if fb.count("run demo-app") != 0 {
		t.Fatal("run-now ran after no")
	}
	command(m, "run")
	send(m, "y")
	if fb.count("run demo-app") != 1 || !strings.Contains(plain(m), "demo-app: run succeeded.") {
		t.Errorf("calls = %v\n%s", fb.log(), plain(m))
	}
	fb.proj.runNow = control.DeployRunNowResult{Skipped: control.SkipPaused}
	command(m, "run")
	send(m, "y")
	if !strings.Contains(plain(m), "not run (paused)") {
		t.Errorf("no skip note:\n%s", plain(m))
	}
}

func TestProjectsCommandsNeedAProject(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m := projectsModel(t, fb, 120, 30)
	command(m, "pause")
	if !strings.Contains(plain(m), "No project selected.") || fb.count(control.OpDeployPause+" ") != 0 {
		t.Errorf("view:\n%s", plain(m))
	}
}

func TestProjectsDeleteAsksThenNeedsThePassphrase(t *testing.T) {
	fb := projectsFake()
	m := projectsModel(t, fb, 120, 30)
	send(m, "d")
	if m.screen != screenConfirm || !strings.Contains(plain(m), "Delete demo-app?") {
		t.Fatalf("no question:\n%s", plain(m))
	}
	send(m, "n")
	if fb.count("remove demo-app rev-demo-app") != 0 || m.ov != nil {
		t.Fatal("no cancelled the delete")
	}
	send(m, "d", "y")
	if m.ov == nil || m.screen != screenPrompt {
		t.Fatalf("no passphrase asked (screen %v)", m.screen)
	}
	if fb.count("remove demo-app rev-demo-app") != 0 {
		t.Fatal("the delete ran before the passphrase")
	}
	typed(m, testPass)
	send(m, "enter")
	if fb.count("remove demo-app rev-demo-app") != 1 {
		t.Fatalf("calls = %v", fb.log())
	}
	view := plain(m)
	if !strings.Contains(view, "demo-app removed.") || strings.Contains(view, "Projects (3)") || !strings.Contains(view, "Projects (2)") {
		t.Errorf("the list is not refreshed:\n%s", view)
	}
}

func TestProjectsDeleteWithASessionAsksNoPassphrase(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := projectsModel(t, fb, 120, 30)
	drain(m, m.statusCmd())
	send(m, "d", "y")
	if m.ov != nil || fb.count("remove demo-app rev-demo-app") != 1 {
		t.Errorf("calls = %v", fb.log())
	}
}

func TestProjectsDeleteOfAChangedProjectSaysSo(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := projectsModel(t, fb, 120, 30)
	drain(m, m.statusCmd())
	d := fb.proj.details["demo-app"]
	d.Revision = "rev-other"
	fb.proj.details["demo-app"] = d
	send(m, "d", "y")
	if view := plain(m); !strings.Contains(view, "changed meanwhile") || !strings.Contains(view, "Projects (3)") {
		t.Errorf("view:\n%s", view)
	}
}

func TestProjectsLogOpensAndScrolls(t *testing.T) {
	fb := projectsFake()
	var b strings.Builder
	for i := range 60 {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	fb.proj.logs["demo-app"] = control.DeployLogResult{Project: "demo-app", Run: "20261029T120000.000000000Z", Text: b.String() + "\x1b[31mred\x1b[0m\n", Truncated: true}
	m := projectsModel(t, fb, 80, 24)
	send(m, "o")
	if fb.count("deploy-log demo-app 20261029T120000.000000000Z") != 1 || m.prj.log == nil {
		t.Fatalf("calls = %v", fb.log())
	}
	view := plain(m)
	assertViewFits(t, view, 80, 24)
	for _, want := range []string{"Log demo-app", "line 00", "The log is cut", "lines 1-"} {
		if !strings.Contains(view, want) {
			t.Errorf("log lacks %q:\n%s", want, view)
		}
	}
	send(m, "end")
	view = plain(m)
	if !strings.Contains(view, "red") || strings.Contains(view, "\x1b") || strings.Contains(view, "line 00") {
		t.Errorf("end does not show the last lines:\n%s", view)
	}
	send(m, "esc")
	if m.prj.log != nil || !strings.Contains(plain(m), "Projects (3)") {
		t.Error("esc does not leave the log")
	}
}

func TestProjectsLogErrorsAreFixedTexts(t *testing.T) {
	fb := projectsFake()
	fb.proj.logErr = &control.Error{Code: control.CodeNotFound}
	m := projectsModel(t, fb, 120, 30)
	send(m, "o")
	if m.prj.log != nil || !strings.Contains(plain(m), "The log of the run is gone.") {
		t.Errorf("view:\n%s", plain(m))
	}
}

func TestProjectsLogWithoutARun(t *testing.T) {
	fb := projectsFake()
	st := fb.proj.status["demo-app"]
	st.LastRun = nil
	fb.proj.status["demo-app"] = st
	m := projectsModel(t, fb, 120, 30)
	send(m, "o")
	if fb.count("deploy-log demo-app ") != 0 || !strings.Contains(plain(m), "has no run yet") {
		t.Errorf("calls = %v\n%s", fb.log(), plain(m))
	}
}

func TestProjectsLogNamesTheLastRunFromTheHistory(t *testing.T) {
	fb := projectsFake()
	m := projectsModel(t, fb, 120, 30)
	r, _ := m.prj.currentRow()
	r.status = nil
	fb.lib.history = map[string][]control.DeployRun{"demo-app": {{ID: "20261028T120000.000000000Z"}, {ID: "20261029T120000.000000000Z"}}}
	send(m, "o")
	if fb.count("deploy-history demo-app") != 1 || fb.count("deploy-log demo-app 20261029T120000.000000000Z") != 1 || m.prj.log == nil {
		t.Errorf("calls = %v", fb.log())
	}
	m.prj.log = nil
	fb.lib.history = nil
	send(m, "o")
	if !strings.Contains(plain(m), "has no run yet") {
		t.Errorf("view:\n%s", plain(m))
	}
}

func TestProjectsGerman(t *testing.T) {
	fb := projectsFake()
	m := projectsModel(t, fb, 120, 30)
	useLanguage(t, "de")
	view := plain(m)
	for _, want := range []string{"Projekte (3)", "Zustand", "freigegeben", "Letzter Lauf", "Quelle"} {
		if !strings.Contains(view, want) {
			t.Errorf("German view lacks %q:\n%s", want, view)
		}
	}
}

func TestProjectsFootersNameTheirKeys(t *testing.T) {
	m := projectsModel(t, projectsFake(), 160, 30)
	m.render()
	for _, want := range []string{"space", "enter", "t test", "u update", "d delete", "o log", "run/pause/pin/restore"} {
		if !strings.Contains(m.footerLine(), want) && !strings.Contains(plain(m), want) {
			t.Errorf("footer lacks %q: %q", want, m.screenKeys)
		}
	}
	if !strings.Contains(m.screenKeys, "run/pause/pin/restore via {command}") {
		t.Errorf("screenKeys = %q", m.screenKeys)
	}
}

func TestGoldenProjects(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			fb := projectsFake()
			zone := projectZone
			projectZone = time.UTC
			defer func() { projectZone = zone }()
			m := goldenApp(t, fb, size[0], size[1])
			send(m, "2", "space", "down")
			got := m.render()
			assertViewFits(t, plain(m), size[0], size[1])
			assertGolden(t, fmt.Sprintf("projects_%dx%d", size[0], size[1]), got)
		})
	}
}

func TestProjectsViewsFitEverySize(t *testing.T) {
	for _, size := range [][2]int{{40, 14}, {60, 16}, {80, 24}, {119, 30}, {120, 40}, {200, 50}} {
		for name, keys := range map[string][]string{"list": nil, "detail": {"enter"}, "raw": {"enter", "right", "right"},
			"log": {"o"}, "sources": {"t"}, "delete": {"d"}, "filter": {"/"}, "update": {"u"}, "pick": {"u", "enter"},
			"restore-pick": {":", "r", "e", "s", "t", "o", "r", "e", "enter"}} {
			t.Run(fmt.Sprintf("%dx%d_%s", size[0], size[1], name), func(t *testing.T) {
				m := projectsModel(t, projectsFake(), size[0], size[1])
				send(m, keys...)
				assertViewFits(t, plain(m), size[0], size[1])
			})
		}
	}
}

func TestProjectsClickMarksThenOpens(t *testing.T) {
	m := projectsModel(t, projectsFake(), 100, 30)
	m.render()
	var row *hit
	for i := range m.hits {
		if h := m.hits[i]; h.y1-h.y0 == 1 && h.x1 >= 90 && h.y0 > 6 {
			if row == nil || h.y0 > row.y0 {
				row = &m.hits[i]
			}
		}
	}
	if row == nil {
		t.Fatal("no row target")
	}
	row.act()
	if name, _ := m.prj.current(); name != "sample-job" {
		t.Errorf("first click selects %q", name)
	}
	m.render()
	for _, h := range m.hits {
		if h.y0 == row.y0 && h.x0 == 0 {
			h.act()
			break
		}
	}
	if !m.prj.focusDetail {
		t.Error("a click on the marked row does not open the detail")
	}
}
