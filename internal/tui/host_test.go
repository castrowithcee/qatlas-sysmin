package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func hostModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := appModel(t, overviewFake(), w, h)
	send(m, "6")
	return m
}

func TestHostSitsBeforeSettingsAndOpensOnUsers(t *testing.T) {
	if areaHost != areaSettings-1 || areaSettings != areaCount-1 {
		t.Fatalf("areas: host %d, settings %d, count %d", areaHost, areaSettings, areaCount)
	}
	m := hostModel(t, 80, 24)
	if m.area != areaHost || m.hst.sub != hostUsers || m.zoneNow() != zoneContent {
		t.Errorf("digit 6 opens area %v page %d zone %v", m.area, m.hst.sub, m.zoneNow())
	}
	rows := rowsOf(m)
	if !strings.Contains(rows[1], "6 Host") || !strings.Contains(rows[1], "7 Settings") || !strings.Contains(rows[2], "Users") {
		t.Errorf("tab line and page strip:\n%s\n%s", rows[1], rows[2])
	}
}

func TestTabLineStaysOneLineAt80Columns(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	for _, digit := range []string{"1", "6", "7"} {
		send(m, digit)
		rows := rowsOf(m)
		if ansi.StringWidth(rows[1]) > 80 || !strings.Contains(rows[1], "Host") && !strings.Contains(rows[1], "6") {
			t.Errorf("area %s: tab line %q", digit, rows[1])
		}
		if m.barRows() > 2 {
			t.Errorf("area %s: %d bar rows", digit, m.barRows())
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

func TestSpaceHidesAndShowsSystemRowsOfBothTables(t *testing.T) {
	m := hostModel(t, 120, 40)
	if !strings.Contains(plain(m), "svc-demo") {
		t.Fatalf("system users are visible by default:\n%s", plain(m))
	}
	send(m, "space")
	if view := plain(m); strings.Contains(view, "svc-demo") || !strings.Contains(view, "space show system") {
		t.Errorf("space does not hide the system users:\n%s", view)
	}
	runCommand(m, "go host groups")
	if view := plain(m); strings.Contains(view, "docker") || !strings.Contains(view, "demo-team") {
		t.Errorf("the system groups stay while hidden:\n%s", view)
	}
	runCommand(m, "go host users")
	send(m, "space")
	if view := plain(m); !strings.Contains(view, "svc-demo") {
		t.Errorf("space does not show the system users again:\n%s", view)
	}
}

func TestGroupsShowMemberCountsAndMembers(t *testing.T) {
	m := hostModel(t, 120, 40)
	runCommand(m, "go host groups")
	view := plain(m)
	for _, want := range []string{"sudo", "docker", "sysmin", "demo-team", "Members", "system"} {
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
	send(m, "6")
	if view := plain(m); !strings.Contains(view, "passwd unreadable") {
		t.Errorf("the failure is not shown:\n%s", view)
	}
}

func TestHostTextsExistInGerman(t *testing.T) {
	m := hostModel(t, 120, 40)
	useLanguage(t, "de")
	view := plain(m)
	for _, want := range []string{"Benutzer", "Gruppen", "Ordner", "ohne sysmin", "System"} {
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
	m := foldersModel(t, 120, 40, "/srv/demo-stack")
	view := plain(m)
	for _, want := range []string{"Owner root", "read and write", "Group sysmin", "Others", "nothing", "folder", "ACL", "setgid", "2750"} {
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
	m := foldersModel(t, 120, 40, "/srv/demo-stack/app.conf")
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
	for _, want := range []string{"Admin:", "practically root", "Enabled:", "group sysmin"} {
		if !strings.Contains(view, want) {
			t.Errorf("Host info lacks %q:\n%s", want, view)
		}
	}
	useLanguage(t, "de")
	if view := plain(m); !strings.Contains(view, "Freigeschaltet:") {
		t.Errorf("German Host info lacks Freigeschaltet:\n%s", view)
	}
}

func TestHostHelpTopicExists(t *testing.T) {
	m := hostModel(t, 120, 40)
	send(m, "?")
	for range helpTopics {
		if strings.Contains(plain(m), "Host shows the users") {
			return
		}
		send(m, "right")
	}
	t.Error("no help topic host")
}
