package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// goldenApp is the service interface of the given size as it is drawn with NO_COLOR set, so the files hold the
// marks and attributes that carry the meaning and no colour.
func goldenApp(t *testing.T, fb *fakeBackend, w, h int) *Model {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	m := appModel(t, fb, w, h)
	if currentProfile() != profileNone {
		t.Fatal("the golden frames need a terminal without colour")
	}
	return m
}

// goldenOverview is goldenApp with the fixed clock of the overview tests, for the screens that show the date.
func goldenOverview(t *testing.T, fb *fakeBackend, w, h int) *Model {
	t.Helper()
	utcClock(t)
	m := goldenApp(t, fb, w, h)
	m.now = func() time.Time { return ovNow }
	return m
}

func TestGoldenAreas(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}}
	screens := []struct {
		name  string
		build func(t *testing.T, w, h int) *Model
	}{
		{"overview", func(t *testing.T, w, h int) *Model { return goldenOverview(t, overviewFake(), w, h) }},
		{"settings_general", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "4")
			return m
		}},
		{"settings_update", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "4", "esc", "right", "enter")
			return m
		}},
		{"help", func(t *testing.T, w, h int) *Model {
			m := goldenOverview(t, overviewFake(), w, h)
			send(m, "?")
			return m
		}},
		{"activity_intact", func(t *testing.T, w, h int) *Model {
			utcClock(t)
			m := goldenApp(t, activityFake(), w, h)
			send(m, "3")
			return m
		}},
		{"activity_broken", func(t *testing.T, w, h int) *Model {
			utcClock(t)
			fb := activityFake()
			fb.audit.verify = brokenChain()
			m := goldenApp(t, fb, w, h)
			send(m, "3")
			return m
		}},
		{"activity_filter", func(t *testing.T, w, h int) *Model {
			utcClock(t)
			m := goldenApp(t, activityFake(), w, h)
			send(m, "3", "f", "right", "right", "right", "tab", "right")
			return m
		}},
		{"host_users", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "2")
			return m
		}},
		{"host_user_detail", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "2", "down", "enter")
			return m
		}},
		{"host_groups", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "2", "esc", "right")
			return m
		}},
		{"host_users_expanded", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "2", "down", "down", "enter", "down")
			return m
		}},
		{"host_groups_expanded", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "2", "esc", "right", "down", "down", "enter", "down")
			return m
		}},
		{"host_folders", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "2", "esc", "right", "right", "enter", "enter")
			typed(m, "/srv/demo-app")
			send(m, "enter", "esc")
			return m
		}},
		{"host_folders_protected", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "2", "esc", "right", "right", "enter", "enter")
			typed(m, "/etc")
			send(m, "enter", "esc")
			return m
		}},
		{"host_folders_empty", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "2", "esc", "right", "right")
			return m
		}},
		{"host_change_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostChange(t, w, h)
			drain(m, m.askHostChange("demo.long", nil))
			return m
		}},
		{"host_change_confirm_irreversible", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostChange(t, w, h)
			drain(m, m.askHostChange("demo.wipe", nil))
			return m
		}},
		{"host_change_result", func(t *testing.T, w, h int) *Model {
			m, f := goldenHostChange(t, w, h)
			f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
			drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
			send(m, "y")
			return m
		}},
		{"host_change_failed", func(t *testing.T, w, h int) *Model {
			m, f := goldenHostChange(t, w, h)
			f.answer = func(HostRequest) (string, error) {
				return `{"journal_id":"x","sentence":"Set alpha to new.","result":"restored","check":"failed","check_error":"the value was rejected","restored":true,"error":"check failed","warning":"the audit log was not written"}`, exitError(t, "exit 1")
			}
			drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
			send(m, "y")
			return m
		}},
		{"host_user_create_form", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			send(m, "n", "tab")
			typed(m, "demo-new")
			send(m, "tab")
			typed(m, "abcdefgh")
			send(m, "tab")
			typed(m, "abcdefgh")
			send(m, "tab", "right", "tab", "tab", "enter", "space", "enter")
			return m
		}},
		{"host_service_create_form", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			send(m, "n", "right", "tab")
			typed(m, "demo-svc")
			send(m, "tab", "right", "tab", "right", "tab", "enter", "space", "enter")
			return m
		}},
		{"host_service_create_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			send(m, "n", "right", "tab")
			typed(m, "demo-svc")
			send(m, "tab", "right", "tab", "right", "tab", "enter", "space", "enter")
			save(m)
			return m
		}},
		{"host_user_create_pick", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			send(m, "n", "tab", "tab", "tab", "tab", "tab", "tab", "enter", "space")
			return m
		}},
		{"host_user_edit_pick_system", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			src := m.hostSrc.(*fixtureHost)
			src.groups = append(src.groups, hostadmin.Group{Name: "demo-sys", GID: 65534, System: true})
			selectAccount(m, "svc-demo")
			send(m, "e", "tab", "enter", "down", "space")
			return m
		}},
		{"host_user_create_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			send(m, "n", "tab")
			typed(m, "demo-new")
			send(m, "tab")
			typed(m, "abcdefgh")
			send(m, "tab")
			typed(m, "abcdefgh")
			send(m, "tab", "right", "tab", "right", "tab", "enter", "space", "enter")
			save(m)
			return m
		}},
		{"host_user_edit_form", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			selectUser(m, "demo-user")
			send(m, "e", "right")
			return m
		}},
		{"host_user_enable_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			selectUser(m, "demo-user")
			send(m, "e")
			save(m)
			return m
		}},
		{"host_folder_form", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			openFolderForm(t, m, "/srv/demo-app", fixedCount(42))
			return m
		}},
		{"host_folder_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			openFolderForm(t, m, "/srv/demo-app", fixedCount(42))
			send(m, "tab", "tab", "tab", "tab", "tab", "right")
			save(m)
			return m
		}},
		{"host_folder_confirm_system", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			openFolderForm(t, m, "/etc", fixedCount(1))
			save(m)
			return m
		}},
		{"host_folder_restored", func(t *testing.T, w, h int) *Model {
			m, f := goldenHostUsers(t, w, h)
			openFolderForm(t, m, "/srv/demo-app", fixedCount(1))
			save(m)
			f.answer = func(HostRequest) (string, error) { return restoredFolderReport, exitError(t, "exit 1") }
			send(m, "y")
			return m
		}},
		{"host_user_lock_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			chooseChange(t, m, "demo-user", chgLock)
			save(m)
			return m
		}},
		{"host_user_delete_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			selectUser(m, "demo-user")
			send(m, "d")
			save(m)
			return m
		}},
		{"host_user_delete_home_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			selectUser(m, "demo-user")
			send(m, "d", "right")
			save(m)
			return m
		}},
		{"host_user_delete_system_home_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			for i := range m.hst.users.all {
				if m.hst.users.all[i].Name == "svc-demo" {
					m.hst.users.all[i].Home = "/usr/lib/svc-demo"
				}
			}
			m.fillHostUsers()
			m.hostSrc.(*fixtureHost).homeRisks = map[string][]hostchange.HomeRisk{"svc-demo": {
				{Kind: hostchange.HomeSystemPath}, {Kind: hostchange.HomeForeignOwner, Who: []string{"root"}},
			}}
			selectAccount(m, "svc-demo")
			send(m, "d", "right")
			save(m)
			return m
		}},
		{"host_group_create_form", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			runCommand(m, "go host groups")
			send(m, "n")
			typed(m, "demo-crew")
			return m
		}},
		{"host_group_delete_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			runCommand(m, "go host groups")
			selectGroup(m, "demo-team")
			send(m, "d")
			return m
		}},
		{"host_group_delete_system_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			runCommand(m, "go host groups")
			selectGroup(m, "docker")
			send(m, "d")
			return m
		}},
		{"host_group_members_form", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			runCommand(m, "go host groups")
			selectGroup(m, "demo-team")
			send(m, "e")
			tickIn(m, 1)
			send(m, "tab")
			tickIn(m, 1)
			return m
		}},
		{"host_group_members_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			runCommand(m, "go host groups")
			selectGroup(m, "demo-team")
			send(m, "e")
			tickIn(m, 2)
			send(m, "tab")
			tickIn(m, 1)
			save(m)
			return m
		}},
		{"host_group_members_docker_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			runCommand(m, "go host groups")
			selectGroup(m, "docker")
			send(m, "e")
			tickIn(m, 1)
			save(m)
			return m
		}},
		{"host_group_rename_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			runCommand(m, "go host groups")
			selectGroup(m, "demo-team")
			send(m, "r")
			typed(m, "demo-crew")
			save(m)
			return m
		}},
		{"host_user_system_confirm", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostUsers(t, w, h)
			chooseChange(t, m, "svc-demo", chgDockerOn)
			save(m)
			return m
		}},
		{"host_changes_empty", func(t *testing.T, w, h int) *Model {
			m, _ := goldenHostChange(t, w, h)
			runCommand(m, "go host changes")
			return m
		}},
		{"host_changes", func(t *testing.T, w, h int) *Model {
			m, f := goldenHostChange(t, w, h)
			runCommand(m, "go host changes")
			f.answer = func(HostRequest) (string, error) { return journalJSON, nil }
			reload(m)
			return m
		}},
		{"host_changes_undo_confirm", func(t *testing.T, w, h int) *Model {
			m, f := goldenHostChange(t, w, h)
			runCommand(m, "go host changes")
			f.answer = func(HostRequest) (string, error) { return journalJSON, nil }
			reload(m)
			send(m, "enter")
			return m
		}},
		{"host_info", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "2", "i")
			return m
		}},
		{"host_help", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "2", "?")
			for range helpTopics {
				if strings.Contains(plain(m), "Host shows the users") {
					break
				}
				send(m, "right")
			}
			return m
		}},
	}
	for _, s := range screens {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%s_%dx%d", s.name, size[0], size[1]), func(t *testing.T) {
				m := s.build(t, size[0], size[1])
				got := m.render()
				if strings.Contains(got, secretValue) {
					t.Error("a secret is in the frame")
				}
				assertViewFits(t, plain(m), size[0], size[1])
				assertGolden(t, fmt.Sprintf("%s_%dx%d", s.name, size[0], size[1]), got)
			})
		}
	}
}

// goldenHostChange is the Host area without colour over the fake sudo and the fixed clock of the journal.
// goldenHostUsers is the Users page over the real user actions and a fake sudo.
func goldenHostUsers(t *testing.T, w, h int) (*Model, *fakeSudo) {
	t.Helper()
	m, f := goldenHostChange(t, w, h)
	m.hostReg = nil
	old := hostUID
	hostUID = func() int { return 4242 }
	t.Cleanup(func() { hostUID = old })
	return m, f
}

func goldenHostChange(t *testing.T, w, h int) (*Model, *fakeSudo) {
	t.Helper()
	utcClock(t)
	m := goldenApp(t, overviewFake(), w, h)
	f := &fakeSudo{}
	m.hostReg, m.hostRun, m.hostExec, m.hostBg = hostActions(t), f.run, f.exec, f.bg
	send(m, "2")
	return m, f
}
