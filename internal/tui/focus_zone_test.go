package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// headerOf is the first row of the frame, where the back button stands.
func headerOf(m *Model) string { return strings.Split(plain(m), "\n")[0] }

// footerOf is the last row of the frame.
func footerOf(m *Model) string {
	rows := strings.Split(plain(m), "\n")
	return rows[len(rows)-1]
}

func TestTheInterfaceStartsOnTheOverviewWithTheTabLineFocused(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	if m.area != areaOverview || m.zoneNow() != zoneTabs {
		t.Fatalf("start on area %v, zone %v; want the Overview with the tab line focused", m.area, m.zoneNow())
	}
	if strings.Contains(headerOf(m), "[esc] back") {
		t.Errorf("the tab line shows a back button: %q", headerOf(m))
	}
	foot := footerOf(m)
	for _, want := range []string{"left/right area", "down open"} {
		if !strings.Contains(foot, want) {
			t.Errorf("the footer of the tab line lacks %q: %q", want, foot)
		}
	}
	if strings.Contains(foot, "1-6") {
		t.Errorf("the footer still names the digits: %q", foot)
	}
}

// Every area leads back to the tab line with esc, through the page strip where it has one, and the tab line
// leads in again with down, enter and tab.
func TestEscLeadsFromEveryAreaToTheTabLineAndBackIn(t *testing.T) {
	cases := []struct {
		digit string
		area  area
		subs  bool
		w, h  int
	}{
		{"1", areaOverview, false, 80, 24},
		{"2", areaProjects, false, 80, 24},
		{"2", areaProjects, false, 120, 40},
		{"3", areaActivity, true, 80, 24},
		{"4", areaAccess, false, 80, 24},
		{"5", areaLibrary, true, 80, 24},
		{"6", areaSettings, true, 80, 24},
	}
	for _, c := range cases {
		for _, in := range []string{"down", "enter", "tab"} {
			fb := libFake()
			fb.addProject("demo-app", control.StateApproved)
			fb.addProject("sample-job", control.StateApproved)
			m := appModel(t, fb, c.w, c.h)
			send(m, c.digit)
			name := c.area.key()
			if m.area != c.area || m.zoneNow() != zoneContent {
				t.Fatalf("%s: digit opens area %v on %v, want the content", name, m.area, m.zoneNow())
			}
			if c.area == areaProjects && c.w >= projectsDetailMinWidth && !strings.Contains(plain(m), "Details") {
				t.Fatalf("%s at %dx%d: no detail beside the list:\n%s", name, c.w, c.h, plain(m))
			}
			if !strings.Contains(headerOf(m), "[esc] back") || !strings.Contains(footerOf(m), "esc back") {
				t.Errorf("%s: the content names no way back:\n%s", name, plain(m))
			}
			send(m, "esc")
			if c.subs {
				if m.zoneNow() != zoneSubs || !strings.Contains(headerOf(m), "[esc] back") {
					t.Errorf("%s: esc from the content reaches %v, want the page strip with a back button", name, m.zoneNow())
				}
				if foot := footerOf(m); !strings.Contains(foot, "left/right page") || !strings.Contains(foot, "esc back") {
					t.Errorf("%s: the footer of the page strip is %q", name, foot)
				}
				send(m, "esc")
			}
			if m.zoneNow() != zoneTabs || m.area != c.area {
				t.Fatalf("%s: esc reaches area %v on %v, want the tab line", name, m.area, m.zoneNow())
			}
			if strings.Contains(headerOf(m), "[esc] back") {
				t.Errorf("%s: the tab line shows a back button", name)
			}
			send(m, "esc")
			if m.zoneNow() != zoneTabs || m.area != c.area {
				t.Errorf("%s: esc on the tab line moves to area %v on %v", name, m.area, m.zoneNow())
			}
			send(m, "right")
			if want := area(wrap(int(c.area)+1, int(areaCount))); m.area != want || m.zoneNow() != zoneTabs {
				t.Errorf("%s: right on the tab line reaches %v on %v, want %v", name, m.area, m.zoneNow(), want)
			}
			send(m, "left")
			if m.area != c.area {
				t.Errorf("%s: left on the tab line reaches %v", name, m.area)
			}
			send(m, in)
			if c.subs {
				if m.zoneNow() != zoneSubs {
					t.Errorf("%s: %s on the tab line reaches %v, want the page strip", name, in, m.zoneNow())
				}
				send(m, in)
			}
			if m.zoneNow() != zoneContent || m.area != c.area {
				t.Errorf("%s: %s leads to area %v on %v, want the content", name, in, m.area, m.zoneNow())
			}
		}
	}
}

func TestTheTabLineWrapsRoundAndHomeAndEndReachItsEnds(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	send(m, "left")
	if m.area != areaSettings || m.zoneNow() != zoneTabs {
		t.Errorf("left from the first area reaches %v on %v", m.area, m.zoneNow())
	}
	send(m, "right")
	if m.area != areaOverview {
		t.Errorf("right from the last area reaches %v", m.area)
	}
	send(m, "end")
	if m.area != areaSettings || m.zoneNow() != zoneTabs {
		t.Errorf("end reaches %v on %v", m.area, m.zoneNow())
	}
	send(m, "home")
	if m.area != areaOverview || m.zoneNow() != zoneTabs {
		t.Errorf("home reaches %v on %v", m.area, m.zoneNow())
	}
}

func TestThePageStripWrapsRoundAndUpLeadsToTheTabLine(t *testing.T) {
	m := appModel(t, libFake(), 80, 24)
	send(m, "5", "esc", "left")
	if m.lib.sub != libTemplates || m.zoneNow() != zoneSubs {
		t.Errorf("left from the first page reaches %d on %v", m.lib.sub, m.zoneNow())
	}
	send(m, "right")
	if m.lib.sub != libCredentials || m.area != areaLibrary {
		t.Errorf("right from the last page reaches %d in %v", m.lib.sub, m.area)
	}
	send(m, "up")
	if m.zoneNow() != zoneTabs || m.area != areaLibrary {
		t.Errorf("up on the page strip reaches %v in %v", m.zoneNow(), m.area)
	}
}

func TestTabAndShiftTabChangeTheZoneAndNeverTheArea(t *testing.T) {
	m := appModel(t, libFake(), 80, 24)
	send(m, "5", "esc", "esc")
	for _, step := range []struct {
		key  string
		want zone
	}{
		{"tab", zoneSubs}, {"tab", zoneContent}, {"tab", zoneTabs},
		{"shift+tab", zoneContent}, {"shift+tab", zoneSubs}, {"shift+tab", zoneTabs},
	} {
		send(m, step.key)
		if m.zoneNow() != step.want || m.area != areaLibrary {
			t.Fatalf("%s reaches %v in %v, want %v in Library", step.key, m.zoneNow(), m.area, step.want)
		}
	}
	m = appModel(t, accessFake(), 80, 24)
	send(m, "4", "tab")
	if m.zoneNow() != zoneTabs || m.area != areaAccess {
		t.Errorf("tab from the content of Access reaches %v in %v, want the tab line", m.zoneNow(), m.area)
	}
	send(m, "shift+tab")
	if m.zoneNow() != zoneContent || m.area != areaAccess {
		t.Errorf("shift+tab from the tab line of Access reaches %v in %v, want the content", m.zoneNow(), m.area)
	}
}

func TestArrowsInTheContentChangeNeitherTheAreaNorThePage(t *testing.T) {
	m := appModel(t, libFake(), 80, 24)
	send(m, "5", "right", "left")
	if m.area != areaLibrary || m.lib.sub != libCredentials || m.zoneNow() != zoneContent {
		t.Errorf("arrows in the content reach %v page %d on %v", m.area, m.lib.sub, m.zoneNow())
	}
	m = appModel(t, runsFake(), 80, 24)
	send(m, "3", "esc", "right", "enter", "o")
	if !m.act.runs.log.open {
		t.Fatal("no run log")
	}
	send(m, "left", "right", "tab")
	if m.area != areaActivity || m.act.sub != actRuns || !m.act.runs.log.open {
		t.Errorf("keys in the run log reach %v page %d, log open %v", m.area, m.act.sub, m.act.runs.log.open)
	}
}

func TestDigitsOpenTheAreaInItsContentButNotWhileTyping(t *testing.T) {
	m := appModel(t, accessFake(), 80, 24)
	send(m, "4")
	if m.area != areaAccess || m.zoneNow() != zoneContent {
		t.Fatalf("4 opens %v on %v", m.area, m.zoneNow())
	}
	send(m, "/")
	typed(m, "2")
	if m.area != areaAccess || m.acc.page.list.query() != "2" {
		t.Errorf("a digit typed into the filter opens %v, filter %q", m.area, m.acc.page.list.query())
	}
}

func TestProjectsLogUpdateAndSetupKeepTheArea(t *testing.T) {
	fb := projectsFake()
	fb.proj.logs["demo-app"] = control.DeployLogResult{Project: "demo-app", Run: "20261029T120000.000000000Z", Text: "line 00\n"}
	fb.session = true
	for _, w := range []int{80, 120} {
		m := projectsModel(t, fb, w, 30)
		drain(m, m.statusCmd())
		send(m, "o")
		if m.prj.log == nil {
			t.Fatalf("%d: no log", w)
		}
		for _, key := range []string{"left", "right", "tab", "shift+tab"} {
			send(m, key)
			if m.area != areaProjects || m.prj.log == nil {
				t.Errorf("%d: %s in the log reaches %v, log open %v", w, key, m.area, m.prj.log != nil)
			}
		}
		send(m, "esc")
		if m.prj.log != nil || m.zoneNow() != zoneContent {
			t.Errorf("%d: esc does not close the log first", w)
		}
		send(m, "u")
		if m.prj.upd == nil {
			t.Fatalf("%d: no update panel:\n%s", w, plain(m))
		}
		for _, key := range []string{"left", "right", "tab", "shift+tab"} {
			send(m, key)
			if m.area != areaProjects || m.prj.upd == nil {
				t.Errorf("%d: %s in the update reaches %v, update open %v", w, key, m.area, m.prj.upd != nil)
			}
		}
		send(m, "esc", "esc")
		if m.prj.upd != nil || m.zoneNow() != zoneTabs || m.area != areaProjects {
			t.Errorf("%d: esc, esc reach %v on %v, update open %v", w, m.area, m.zoneNow(), m.prj.upd != nil)
		}
		send(m, "down", "n", "enter")
		if m.prj.flow == nil {
			t.Fatalf("%d: no guided setup", w)
		}
		for _, key := range []string{"left", "right", "tab", "shift+tab"} {
			send(m, key)
			if m.area != areaProjects || m.prj.flow == nil {
				t.Errorf("%d: %s in the setup reaches %v, setup open %v", w, key, m.area, m.prj.flow != nil)
			}
		}
	}
}

// From 120 columns the arrows on the rows of Projects still turn the tabs of the detail beside them.
func TestProjectsArrowsTurnTheDetailTabFrom120Columns(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 40)
	send(m, "right")
	if m.prj.tab != projTabDetails || m.area != areaProjects {
		t.Errorf("right on a row reaches tab %v in %v", m.prj.tab, m.area)
	}
}

func TestUpOnTheFirstRowLeadsToTheBarAndListsStopAtTheEnds(t *testing.T) {
	m := appModel(t, accessFake(), 80, 24)
	send(m, "4", "down", "down", "down")
	if got := m.acc.page.list.cursor; got != 1 {
		t.Errorf("down past the last row reaches %d, want it to stay on 1", got)
	}
	send(m, "end", "pgdown")
	if got := m.acc.page.list.cursor; got != 1 {
		t.Errorf("end and pgdown reach %d, want 1", got)
	}
	send(m, "home", "pgup")
	if got := m.acc.page.list.cursor; got != 0 || m.zoneNow() != zoneContent {
		t.Errorf("home and pgup reach %d on %v, want the first row in the content", got, m.zoneNow())
	}
	send(m, "up")
	if m.zoneNow() != zoneTabs || m.area != areaAccess {
		t.Errorf("up on the first row reaches %v in %v, want the tab line", m.zoneNow(), m.area)
	}

	m = appModel(t, libFake(), 80, 24)
	send(m, "5", "up")
	if m.zoneNow() != zoneSubs {
		t.Errorf("up on the first row of Library reaches %v, want the page strip", m.zoneNow())
	}

	m = projectsModel(t, projectsFake(), 80, 24)
	send(m, "down", "down", "down", "down")
	if got := m.prj.list.cursor; got != 2 {
		t.Errorf("down past the last project reaches %d, want 2", got)
	}
	send(m, "up", "up", "up")
	if m.zoneNow() != zoneTabs {
		t.Errorf("up on the first project reaches %v", m.zoneNow())
	}

	m = appModel(t, overviewFake(), 80, 24)
	send(m, "1")
	rows := len(m.overviewRows())
	for range rows + 2 {
		send(m, "down")
	}
	if m.ovr.cursor != rows-1 {
		t.Errorf("down past the last line of the overview reaches %d, want %d", m.ovr.cursor, rows-1)
	}
	send(m, "home", "up")
	if m.zoneNow() != zoneTabs {
		t.Errorf("up on the first line of the overview reaches %v", m.zoneNow())
	}

	m, _ = settingsModel(t, newFake(control.VaultUnlocked), "general")
	send(m, "end", "down")
	if m.set.gen.focus != genRows-1 {
		t.Errorf("down past the last row of General reaches %d", m.set.gen.focus)
	}
	send(m, "home", "up")
	if m.zoneNow() != zoneSubs {
		t.Errorf("up on the first row of General reaches %v, want the page strip", m.zoneNow())
	}
}

// Moving over Settings on the tab line asks nothing while it holds nothing unsaved, and the usual question
// when it does.
func TestCyclingOverSettingsAsksOnlyAboutUnsavedInput(t *testing.T) {
	m := appModel(t, newFake(control.VaultUnlocked), 80, 24)
	send(m, "left", "left", "right", "right")
	if m.screen != screenArea || m.leave != nil || m.area != areaOverview {
		t.Errorf("cycling over unchanged Settings asks: screen %v, area %v", m.screen, m.area)
	}

	m, _ = settingsModel(t, newFake(control.VaultUnlocked), "vault")
	send(m, "down", "enter")
	typed(m, "5")
	send(m, "esc")
	if !m.settingsDirty() {
		t.Fatal("the edit left nothing unsaved")
	}
	send(m, "esc")
	if m.zoneNow() != zoneSubs || m.screen != screenArea {
		t.Fatalf("esc from the content asks or reaches %v", m.zoneNow())
	}
	send(m, "esc", "right")
	if m.screen != screenConfirm || m.leave == nil || m.area != areaSettings {
		t.Fatalf("leaving unsaved Settings does not ask: screen %v, area %v", m.screen, m.area)
	}
	if !strings.Contains(plain(m), "d discard") {
		t.Errorf("not the usual question:\n%s", plain(m))
	}
	send(m, "d")
	if m.area != areaOverview || m.zoneNow() != zoneTabs {
		t.Errorf("discard reaches %v on %v, want the Overview on the tab line", m.area, m.zoneNow())
	}
}

func TestClickOnTheBackButtonActsLikeEscAndTabClicksOpenTheArea(t *testing.T) {
	m := appModel(t, libFake(), 120, 40)
	send(m, "5")
	clickOn(t, m, "[esc] back")
	if m.zoneNow() != zoneSubs {
		t.Errorf("the back button reaches %v, want the page strip", m.zoneNow())
	}
	clickOn(t, m, "[esc] back")
	if m.zoneNow() != zoneTabs || m.area != areaLibrary {
		t.Errorf("the back button reaches %v in %v, want the tab line", m.zoneNow(), m.area)
	}
	if strings.Contains(headerOf(m), "[esc] back") {
		t.Error("the tab line shows a back button")
	}
	clickOn(t, m, "Access")
	if m.area != areaAccess || m.zoneNow() != zoneContent {
		t.Errorf("a click on a tab opens %v on %v, want Access in its content", m.area, m.zoneNow())
	}
}
