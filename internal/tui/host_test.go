package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func hostModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := appModel(t, overviewFake(), w, h)
	send(m, "2")
	return m
}

func TestHostSitsAfterOverviewAndOpensOnUsers(t *testing.T) {
	if areaHost != areaOverview+1 || areaSettings != areaCount-1 {
		t.Fatalf("areas: host %d, settings %d, count %d", areaHost, areaSettings, areaCount)
	}
	m := hostModel(t, 80, 24)
	if m.area != areaHost || m.hst.sub != hostUsers || m.zoneNow() != zoneContent {
		t.Errorf("digit 2 opens area %v page %d zone %v", m.area, m.hst.sub, m.zoneNow())
	}
	rows := rowsOf(m)
	if !strings.Contains(rows[1], "2 Host") || !strings.Contains(rows[1], "4 Settings") || !strings.Contains(rows[2], "Users") {
		t.Errorf("tab line and page strip:\n%s\n%s", rows[1], rows[2])
	}
}

func TestTabLineStaysOneLineAt80Columns(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	for _, digit := range []string{"1", "3", "4"} {
		send(m, digit)
		rows := rowsOf(m)
		if ansi.StringWidth(rows[1]) > 80 || !strings.Contains(rows[1], "Host") && !strings.Contains(rows[1], "2") {
			t.Errorf("area %s: tab line %q", digit, rows[1])
		}
		if m.workspaceTop() > 4 {
			t.Errorf("area %s: the workspace starts at row %d", digit, m.workspaceTop())
		}
	}
}

func TestGoHostReachesEveryPage(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	for name, sub := range map[string]int{"go host users": hostUsers, "go host groups": hostGroups, "go host folders": hostFolders} {
		runCommand(m, name)
		if m.area != areaHost || m.hst.sub != sub {
			t.Errorf("%s reaches area %v page %d", name, m.area, m.hst.sub)
		}
	}
	runCommand(m, "go host")
	if m.area != areaHost || m.hst.sub != hostUsers {
		t.Errorf("go host reaches area %v page %d", m.area, m.hst.sub)
	}
}

func TestUsersShowAdminsDockerAndSystemMarks(t *testing.T) {
	m := hostModel(t, 120, 40)
	openSystem(m, &m.hst.users.page, "enter")
	view := plain(m)
	for _, want := range []string{"demo-admin", "demo-user", "svc-demo", "not enabled", "system"} {
		if !strings.Contains(view, want) {
			t.Errorf("Users lacks %q:\n%s", want, view)
		}
	}
}

func TestUserDetailShowsUnknownWhereOnlyRootCanRead(t *testing.T) {
	m := hostModel(t, 120, 40)
	view := plain(m)
	if !strings.Contains(view, "unknown") {
		t.Errorf("a field only root reads is not marked unknown:\n%s", view)
	}
	send(m, "down")
	view = plain(m)
	if !strings.Contains(view, "Locked") || !strings.Contains(view, "no") {
		t.Errorf("a known false is not shown as no:\n%s", view)
	}
	send(m, "tab", "right")
	view = plain(m)
	for _, want := range []string{"ssh-ed25519", "demo-user@laptop", "SHA256:DEMOFINGERPRINT"} {
		if !strings.Contains(view, want) {
			t.Errorf("SSH keys lack %q:\n%s", want, view)
		}
	}
}

// openSystem marks the heading of the system section of page p and presses key on it.
func openSystem(m *Model, p *dataPage, key string) {
	p.list.selectName(sectionRow("system"))
	send(m, key)
}

func TestSystemSectionsStartCollapsedWithCountsAndMarks(t *testing.T) {
	m := hostModel(t, 120, 40)
	view := plain(m)
	for _, want := range []string{"▾ Users (2)", "▸ System accounts (1)", "demo-admin", "demo-user"} {
		if !strings.Contains(view, want) {
			t.Errorf("Users lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "svc-demo") {
		t.Errorf("a collapsed section shows its rows:\n%s", view)
	}
	runCommand(m, "go host groups")
	view = plain(m)
	for _, want := range []string{"▾ Groups (1)", "▸ System groups (3)", "demo-team"} {
		if !strings.Contains(view, want) {
			t.Errorf("Groups lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "docker") {
		t.Errorf("collapsed system groups are shown:\n%s", view)
	}
}

func TestEnterAndSpaceOnAHeadingToggleItsSection(t *testing.T) {
	for _, key := range []string{"enter", "space"} {
		m := hostModel(t, 120, 40)
		p := &m.hst.users.page
		openSystem(m, p, key)
		if view := plain(m); !strings.Contains(view, "▾ System accounts (1)") || !strings.Contains(view, "svc-demo") {
			t.Errorf("%s does not expand the section:\n%s", key, view)
		}
		if p.open || m.zoneNow() == zoneDetail {
			t.Errorf("%s on a heading opens a detail", key)
		}
		if view := plain(m); !strings.Contains(view, "to collapse or expand") || strings.Contains(view, "[Overview]") {
			t.Errorf("a heading shows a detail or lacks the hint:\n%s", view)
		}
		send(m, key)
		if view := plain(m); !strings.Contains(view, "▸ System accounts (1)") || strings.Contains(view, "svc-demo") {
			t.Errorf("%s does not collapse the section again:\n%s", key, view)
		}
	}
}

func TestClickOnAHeadingToggles(t *testing.T) {
	m := hostModel(t, 80, 24)
	view := rowsOf(m)
	y := -1
	for i, row := range view {
		if strings.Contains(row, "System accounts") {
			y = i
		}
	}
	if y < 0 {
		t.Fatalf("no heading:\n%s", plain(m))
	}
	clickAt(m, 10, y)
	if view := plain(m); !strings.Contains(view, "▾ System accounts (1)") || !strings.Contains(view, "svc-demo") {
		t.Errorf("a click does not expand the section:\n%s", view)
	}
	clickAt(m, 10, y)
	if view := plain(m); !strings.Contains(view, "▸ System accounts (1)") {
		t.Errorf("a second click does not collapse it:\n%s", view)
	}
}

func TestCollapseStateHoldsPerTableAndAcrossVisits(t *testing.T) {
	m := hostModel(t, 120, 40)
	openSystem(m, &m.hst.users.page, "enter")
	runCommand(m, "go host groups")
	if view := plain(m); !strings.Contains(view, "▸ System groups (3)") {
		t.Errorf("opening the system users opened the system groups:\n%s", view)
	}
	runCommand(m, "go host users")
	if view := plain(m); !strings.Contains(view, "▾ System accounts (1)") || !strings.Contains(view, "svc-demo") {
		t.Errorf("the state is lost on a new visit:\n%s", view)
	}
}

func TestFilterFindsAndOpensAFoldedSection(t *testing.T) {
	m := hostModel(t, 120, 40)
	send(m, "/")
	typed(m, "svc-demo")
	view := plain(m)
	if !strings.Contains(view, "▾ System accounts (1)") || !strings.Contains(view, "svc-demo") {
		t.Errorf("the match inside the folded section is not shown:\n%s", view)
	}
	if strings.Contains(view, "demo-admin") || strings.Contains(view, "Users (2)") {
		t.Errorf("a section without a match stays on view:\n%s", view)
	}
	send(m, "esc")
	if view := plain(m); !strings.Contains(view, "demo-admin") || !strings.Contains(view, "svc-demo") {
		t.Errorf("clearing the filter loses rows:\n%s", view)
	}
}

func TestMovementPassesOverHeadings(t *testing.T) {
	m := hostModel(t, 80, 24)
	p := &m.hst.users.page
	at := func() string { n, _ := p.list.selected(); return n }
	if at() != "demo-admin" {
		t.Fatalf("opens on %q", at())
	}
	send(m, "down", "down")
	if at() != sectionRow("system") {
		t.Errorf("down does not reach the heading: %q", at())
	}
	send(m, "up", "up", "up")
	if at() != sectionRow("users") {
		t.Errorf("up does not reach the first heading: %q", at())
	}
	send(m, "end")
	if at() != sectionRow("system") {
		t.Errorf("end: %q", at())
	}
	send(m, "home")
	if at() != sectionRow("users") {
		t.Errorf("home: %q", at())
	}
	// The wheel is a step of the mark, with the stop at both ends.
	_, cmd := m.Update(tea.MouseWheelMsg{X: 10, Y: 6, Button: tea.MouseWheelDown})
	drain(m, cmd)
	if at() != "demo-admin" {
		t.Errorf("the wheel does not step down: %q", at())
	}
	_, cmd = m.Update(tea.MouseWheelMsg{X: 10, Y: 6, Button: tea.MouseWheelUp})
	drain(m, cmd)
	if at() != sectionRow("users") {
		t.Errorf("the wheel does not step up: %q", at())
	}
}

func TestSpaceOnARowDoesNothingAndAHeadingHasNoDetail(t *testing.T) {
	m := hostModel(t, 80, 24)
	before := plain(m)
	send(m, "space")
	if plain(m) != before {
		t.Errorf("space on a row changes the screen:\n%s", plain(m))
	}
	p := &m.hst.users.page
	p.list.selectName(sectionRow("users"))
	if _, ok := p.current(); ok {
		t.Errorf("a heading is a row")
	}
	if keys := m.hintText(m.hostKeys()); !strings.Contains(keys, "enter collapse") || strings.Contains(keys, "space hide") {
		t.Errorf("keys %q", keys)
	}
	send(m, "enter")
	if p.open || m.zoneNow() == zoneDetail {
		t.Errorf("enter on a heading opens a detail")
	}
}

func TestGroupsShowMemberCountsAndMembers(t *testing.T) {
	m := hostModel(t, 120, 40)
	runCommand(m, "go host groups")
	openSystem(m, &m.hst.groups.page, "enter")
	m.hst.groups.page.list.selectName("sudo")
	view := plain(m)
	for _, want := range []string{"sudo", "docker", "qsysmin", "demo-team", "Members", "system"} {
		if !strings.Contains(view, want) {
			t.Errorf("Groups lacks %q:\n%s", want, view)
		}
	}
	send(m, "tab", "right")
	if view := plain(m); !strings.Contains(view, "demo-admin") {
		t.Errorf("the group detail lists no member:\n%s", view)
	}
}

func TestHostReadFailureIsShownInTheTable(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	m.hostSrc = &fixtureHost{err: errors.New("passwd unreadable")}
	send(m, "2")
	if view := plain(m); !strings.Contains(view, "passwd unreadable") {
		t.Errorf("the failure is not shown:\n%s", view)
	}
}

func TestHostTextsExistInGerman(t *testing.T) {
	m := hostModel(t, 120, 40)
	useLanguage(t, m, "de")
	view := plain(m)
	for _, want := range []string{"Benutzer", "Gruppen", "Ordner", "ohne qsysmin", "System"} {
		if !strings.Contains(view, want) {
			t.Errorf("German Host lacks %q:\n%s", want, view)
		}
	}
}

// foldersModel opens Folders and reads path through the path line.
func foldersModel(t *testing.T, w, h int, path string) *Model {
	t.Helper()
	m := hostModel(t, w, h)
	runCommand(m, "go host folders")
	send(m, "enter")
	typed(m, path)
	send(m, "enter")
	return m
}

func TestFoldersShowsThreeLevelsInPlainWords(t *testing.T) {
	m := foldersModel(t, 120, 40, "/srv/demo-app")
	view := plain(m)
	for _, want := range []string{"Owner root", "read and write", "Group qsysmin", "Others", "nothing", "folder", "ACL", "setgid", "2750"} {
		if !strings.Contains(view, want) {
			t.Errorf("Folders lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Protected") {
		t.Errorf("an unprotected path is marked protected:\n%s", view)
	}
}

func TestFoldersMarksProtectedAndUnknownACL(t *testing.T) {
	m := foldersModel(t, 120, 40, "/etc")
	if view := plain(m); !strings.Contains(view, "Protected") {
		t.Errorf("a protected path is not marked:\n%s", view)
	}
	send(m, "enter")
	src := m.hostSrc.(*fixtureHost)
	folder := src.folders["/etc"]
	folder.ACL = nil
	src.folders["/etc"] = folder
	send(m, "enter")
	if view := plain(m); !strings.Contains(view, "ACL") || !strings.Contains(view, "unknown") {
		t.Errorf("an unknown ACL state is not named:\n%s", view)
	}
}

func TestFoldersShowsAFileAndClearErrors(t *testing.T) {
	m := foldersModel(t, 120, 40, "/srv/demo-app/app.conf")
	if view := plain(m); !strings.Contains(view, "file") || !strings.Contains(view, "Owner demo-user") {
		t.Errorf("a file is not shown:\n%s", view)
	}
	for path, want := range map[string]string{"/srv/missing": "/srv/missing does not exist.", "/srv/locked": "No permission to read /srv/locked."} {
		m := foldersModel(t, 120, 40, path)
		if view := plain(m); !strings.Contains(view, want) {
			t.Errorf("%s: want %q:\n%s", path, want, view)
		}
	}
}

func TestFoldersEscLeavesTheLineThenTheContent(t *testing.T) {
	m := foldersModel(t, 80, 24, "/etc")
	if !m.typing() {
		t.Fatal("the path line does not take letters")
	}
	send(m, "esc")
	if m.typing() || m.zoneNow() != zoneContent {
		t.Errorf("first esc: typing %v zone %v", m.typing(), m.zoneNow())
	}
	send(m, "esc")
	if m.zoneNow() != zoneSubs {
		t.Errorf("second esc: zone %v", m.zoneNow())
	}
	send(m, "down")
	if m.typing() {
		t.Error("entering the page again edits the line at once")
	}
	send(m, "enter")
	if !m.typing() {
		t.Error("enter does not give the line back")
	}
}

func TestHostInfoExplainsAdminDockerAndEnabled(t *testing.T) {
	m := hostModel(t, 120, 40)
	send(m, "i")
	view := plain(m)
	for _, want := range []string{"Admin:", "practically root", "Enabled:", "group qsysmin"} {
		if !strings.Contains(view, want) {
			t.Errorf("Host info lacks %q:\n%s", want, view)
		}
	}
	useLanguage(t, m, "de")
	if view := plain(m); !strings.Contains(view, "Freigeschaltet:") {
		t.Errorf("German Host info lacks Freigeschaltet:\n%s", view)
	}
}

func TestHostHelpTopicExists(t *testing.T) {
	m := hostModel(t, 120, 40)
	send(m, "?")
	for range helpTopics() {
		if strings.Contains(plain(m), "Host shows the users") {
			return
		}
		send(m, "right")
	}
	t.Error("no help topic host")
}
