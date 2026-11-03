package tui

import (
	"slices"
	"strings"
	"testing"

	kithelp "github.com/castrowithcee/qatlas-kit/helptopics"

	"github.com/castrowithcee/qatlas-sysmin/internal/helptopics"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// groupsModel is the Groups page over the real group actions and a fake sudo. demo-user is also a group, the
// primary group of the user of that name; demo-ops is a regular group without members; root is the system
// group of the root account.
func groupsModel(t *testing.T, w, h int) (*Model, *fakeSudo, *countingHost) {
	t.Helper()
	m, f, host, _ := userModel(t, w, h)
	host.groups = append(host.groups,
		hostadmin.Group{Name: "demo-user", GID: 1001},
		hostadmin.Group{Name: "root", GID: 0, System: true})
	runCommand(m, "go host groups")
	return m, f, host
}

func selectGroup(m *Model, name string) {
	p := &m.hst.groups.page
	if !p.list.selectName(name) {
		openSystem(m, p, "enter")
		p.list.selectName(name)
	}
}

func TestGroupsPageOffersNewRenameAndDeleteInTheFooter(t *testing.T) {
	m, _, _ := groupsModel(t, 120, 40)
	selectGroup(m, "demo-team")
	keys := m.hintText(m.hostKeys())
	for _, want := range []string{"n new", "r rename", "d delete"} {
		if !strings.Contains(keys, want) {
			t.Errorf("keys %q lack %q", keys, want)
		}
	}
	p := &m.hst.groups.page
	p.list.selectName(sectionRow("groups"))
	if keys := m.hintText(m.hostKeys()); !strings.Contains(keys, "n new") || strings.Contains(keys, "rename") || strings.Contains(keys, "delete") {
		t.Errorf("on a heading only n works: %q", keys)
	}
	send(m, "d", "r")
	if m.form != nil || m.ask != nil {
		t.Error("d and r on a heading must do nothing")
	}
}

func TestGroupCreateChecksTheNameBeforeItAsksAndSendsTheAction(t *testing.T) {
	m, f, host := groupsModel(t, 120, 40)
	send(m, "n")
	if m.form == nil || !strings.Contains(flat(m), "New group") {
		t.Fatalf("no form:\n%s", plain(m))
	}
	for _, tc := range []struct{ name, problem string }{
		{"", "Enter a name."},
		{"Demo-Team", "Lower-case letters"},
		{"demo team", "Lower-case letters"},
		{"demo-team", "This group exists already."},
		{"sudo", "This group exists already."},
	} {
		m.form.row("name").in.SetValue(tc.name)
		m.form.row("name").problem = ""
		save(m)
		if m.form == nil || !strings.Contains(m.form.row("name").problem, tc.problem) || m.ask != nil || len(f.reqs) != 0 {
			t.Fatalf("%q: problem %q, ask %+v", tc.name, m.form.row("name").problem, m.ask)
		}
	}
	m.form.row("name").in.SetValue("demo-new")
	save(m)
	view := flat(m)
	for _, want := range []string{"Create the group demo-new", "$ groupadd demo-new", "You can undo this under Host > Changes."} {
		if !strings.Contains(view, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, plain(m))
		}
	}
	if m.ask == nil || m.ask.danger || len(f.reqs) != 0 {
		t.Fatalf("creating a group is not red and nothing runs before y: ask %+v calls %v", m.ask, f.reqs)
	}
	f.answer = func(HostRequest) (string, error) {
		host.groups = append(host.groups, hostadmin.Group{Name: "demo-new", GID: 1004})
		return reportApplied, nil
	}
	send(m, "y")
	want := []string{"apply", "--confirm", "--output", "json", "--", "group.create", "demo-new"}
	if len(f.reqs) == 0 || !slices.Equal(f.reqs[len(f.reqs)-1].Args, want) {
		t.Fatalf("calls %+v, want %q", f.reqs, want)
	}
	if _, ok := m.hst.groups.items["demo-new"]; !ok {
		t.Error("the page was not read again")
	}
}

func TestGroupDeleteAsksPlainForARegularGroupAndRedForSystemGroupsRootAndDocker(t *testing.T) {
	for _, tc := range []struct {
		group string
		red   bool
		says  []string
	}{
		{"demo-ops", false, []string{"$ groupdel demo-ops", "You can undo this under Host > Changes."}},
		{"docker", true, []string{"$ groupdel docker", "practically root", "may depend on this system group"}},
		{"root", true, []string{"belongs to the root account", "may depend on this system group"}},
	} {
		t.Run(tc.group, func(t *testing.T) {
			m, f, _ := groupsModel(t, 120, 40)
			selectGroup(m, tc.group)
			send(m, "d")
			if m.ask == nil || m.ask.danger != tc.red || len(f.reqs) != 0 {
				t.Fatalf("ask %+v calls %v\n%s", m.ask, f.reqs, plain(m))
			}
			for _, want := range append(tc.says, "Delete the group "+tc.group) {
				if !strings.Contains(flat(m), want) {
					t.Errorf("lacks %q:\n%s", want, plain(m))
				}
			}
			f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
			send(m, "y")
			want := []string{"apply", "--confirm", "--output", "json", "--", "group.delete", tc.group}
			if len(f.reqs) == 0 || !slices.Equal(f.reqs[len(f.reqs)-1].Args, want) {
				t.Errorf("calls %+v, want %q", f.reqs, want)
			}
		})
	}
}

func TestGroupDeleteIsRefusedEarlyForProtectedAndPrimaryGroups(t *testing.T) {
	m, f, _ := groupsModel(t, 120, 40)
	for group, says := range map[string]string{
		"sudo":      "sudo is never deleted",
		"qsysmin":   "qsysmin is never deleted",
		"demo-user": "demo-user is the primary group of demo-user and cannot be deleted",
	} {
		selectGroup(m, group)
		send(m, "d")
		if m.confirm == nil || !m.confirm.notice || !strings.Contains(flat(m), says) || len(f.reqs) != 0 {
			t.Fatalf("%s: ask %+v\n%s", group, m.ask, plain(m))
		}
		send(m, "esc")
	}
}

func TestGroupRenameIsAlwaysRedAndSendsBothNames(t *testing.T) {
	for _, tc := range []struct {
		group string
		says  []string
	}{
		{"demo-ops", []string{"Rename the group demo-ops to demo-crew", "$ groupmod -n demo-crew demo-ops", "address a group by its name"}},
		{"docker", []string{"Rename the group docker to demo-crew", "practically root", "may depend on this system group"}},
	} {
		t.Run(tc.group, func(t *testing.T) {
			m, f, _ := groupsModel(t, 120, 40)
			selectGroup(m, tc.group)
			send(m, "r")
			if m.form == nil || !strings.Contains(flat(m), "Rename "+tc.group) {
				t.Fatalf("no form:\n%s", plain(m))
			}
			for name, problem := range map[string]string{"": "Enter a name.", "Demo": "Lower-case letters", "demo-team": "This group exists already.", tc.group: "This group exists already."} {
				m.form.row("name").in.SetValue(name)
				m.form.row("name").problem = ""
				save(m)
				if m.form == nil || !strings.Contains(m.form.row("name").problem, problem) || m.ask != nil {
					t.Fatalf("%q: problem %q", name, m.form.row("name").problem)
				}
			}
			m.form.row("name").in.SetValue("demo-crew")
			save(m)
			if m.ask == nil || !m.ask.danger || !m.ask.warn || len(f.reqs) != 0 {
				t.Fatalf("a rename asks red before anything runs: ask %+v calls %v", m.ask, f.reqs)
			}
			for _, want := range tc.says {
				if !strings.Contains(flat(m), want) {
					t.Errorf("lacks %q:\n%s", want, plain(m))
				}
			}
			f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
			send(m, "y")
			want := []string{"apply", "--confirm", "--output", "json", "--", "group.rename", tc.group, "demo-crew"}
			if len(f.reqs) == 0 || !slices.Equal(f.reqs[len(f.reqs)-1].Args, want) {
				t.Errorf("calls %+v, want %q", f.reqs, want)
			}
		})
	}
}

func TestGroupRenameIsRefusedEarlyForSudoAndQsysmin(t *testing.T) {
	m, f, _ := groupsModel(t, 120, 40)
	for _, group := range []string{"sudo", "qsysmin"} {
		selectGroup(m, group)
		send(m, "r")
		if m.form != nil || m.confirm == nil || !m.confirm.notice || !strings.Contains(flat(m), group+" keeps its name") || len(f.reqs) != 0 {
			t.Fatalf("%s: form %v ask %+v\n%s", group, m.form, m.ask, plain(m))
		}
		send(m, "esc")
	}
}

func TestCancellingTheGroupFormsRunsNothing(t *testing.T) {
	m, f, _ := groupsModel(t, 120, 40)
	selectGroup(m, "demo-ops")
	send(m, "n", "esc", "r", "esc", "d", "n")
	if m.form != nil || m.ask != nil || len(f.reqs) != 0 {
		t.Fatalf("still open or ran: %+v", f.reqs)
	}
}

func TestGroupFormsSpeakGerman(t *testing.T) {
	m, _, _ := groupsModel(t, 120, 40)
	useLanguage(t, m, "de")
	selectGroup(m, "demo-ops")
	send(m, "r")
	for _, want := range []string{"demo-ops umbenennen", "Neuer Name", "sprechen eine Gruppe über ihren Namen an"} {
		if !strings.Contains(flat(m), want) {
			t.Errorf("lacks %q:\n%s", want, plain(m))
		}
	}
	m.form.row("name").in.SetValue("demo-crew")
	save(m)
	if m.ask == nil || !m.ask.danger || !strings.Contains(flat(m), "wirken nicht mehr, bis sie ebenfalls geändert sind") {
		t.Errorf("\n%s", plain(m))
	}
	send(m, "n")
	selectGroup(m, "sudo")
	send(m, "d")
	if !strings.Contains(flat(m), "sudo wird nie gelöscht") {
		t.Errorf("\n%s", plain(m))
	}
}

func TestGroupsHelpAndInfoNoLongerCallSystemGroupsReadOnly(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		ui := testUI()
		ui.setLanguage(testLang)
		host, _ := kithelp.Lookup(helptopics.Topics(testLang), "host-tui")
		for id, text := range map[string]string{string(tInfoHostGroups): ui.tr(tInfoHostGroups), host.Name: host.Text} {
			if strings.Contains(text, "read-only") || strings.Contains(text, "nur lesbar") || !strings.Contains(text, "sudo") {
				t.Errorf("%s: %q", id, text)
			}
		}
	})
}
