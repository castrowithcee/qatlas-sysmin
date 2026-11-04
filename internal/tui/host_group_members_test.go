package tui

import (
	"slices"
	"strings"
	"testing"
)

// tickIn ticks the first n values of the pick row that the form has focused, in the order of the list.
func tickIn(m *Model, n int) {
	send(m, "enter")
	for i := 0; i < n; i++ {
		if i > 0 {
			send(m, "down")
		}
		send(m, "space")
	}
	send(m, "enter")
}

// flatBoxes is flat with every border character taken out, because the frame of the page behind a modal sits
// between the wrapped lines of its text.
func flatBoxes(m *Model) string {
	return flatText(strings.NewReplacer("│", " ", "║", " ", "─", " ", "╰", " ", "╯", " ", "╭", " ", "╮", " ").Replace(plain(m)))
}

// flatDlg is the text of the dialog on one line: the frame is gone and a word the kit broke at a hyphen is whole.
func flatDlg(m *Model) string { return strings.ReplaceAll(flatBoxes(m), "- ", "-") }

func lastArgs(f *fakeSudo) []string {
	if len(f.reqs) == 0 {
		return nil
	}
	return f.reqs[len(f.reqs)-1].Args
}

func TestGroupMembersFormOffersTheRightUsersAndSendsOneActionForAllChanges(t *testing.T) {
	m, f, _ := groupsModel(t, 120, 40)
	selectGroup(m, "demo-team")
	if keys := m.hintText(m.hostKeys()); !strings.Contains(keys, "e members") {
		t.Errorf("keys %q lack e members", keys)
	}
	send(m, "e")
	if m.form == nil || !strings.Contains(flatBoxes(m), "Members of demo-team") {
		t.Fatalf("no form:\n%s", plain(m))
	}
	if got := listValues(m, fieldAdd); !slices.Equal(got, []string{"demo-admin", "svc-demo"}) {
		t.Errorf("add offers %q: members and primary users do not belong", got)
	}
	if got := listValues(m, fieldRemove); !slices.Equal(got, []string{"demo-user"}) {
		t.Errorf("remove offers %q", got)
	}
	tickIn(m, 2)
	send(m, "tab")
	tickIn(m, 1)
	save(m)
	for _, want := range []string{"Add demo-admin and svc-demo to the group demo-team and remove demo-user from the group demo-team",
		"$ gpasswd -a demo-admin demo-team", "$ gpasswd -a svc-demo demo-team", "$ gpasswd -d demo-user demo-team", "You can undo this under Host > Changes."} {
		if !strings.Contains(flatBoxes(m), want) {
			t.Errorf("confirmation lacks %q:\n%s", want, plain(m))
		}
	}
	if m.dlg == nil || (m.dlgSpec.Danger != "") || len(f.reqs) != 0 {
		t.Fatalf("a regular group asks plain and nothing runs before y: ask %+v calls %v", m.dlgSpec, f.reqs)
	}
	f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
	send(m, "y")
	want := []string{"apply", "--confirm", "--output", "json", "--", "group.set-members", "demo-team", "+demo-admin", "+svc-demo", "-demo-user"}
	if !slices.Equal(lastArgs(f), want) {
		t.Errorf("calls %+v, want %q", f.reqs, want)
	}
}

func TestGroupMembersAskRedForSystemGroupsRootAndDockerAndWarnAsPower(t *testing.T) {
	for _, tc := range []struct {
		group string
		power bool
		says  []string
	}{
		{"docker", true, []string{"practically root", "may depend on this system group", "allow practically everything"}},
		{"root", false, []string{"belongs to the root account", "may depend on this system group"}},
		{"sudo", true, []string{"may depend on this system group", "allow practically everything"}},
	} {
		t.Run(tc.group, func(t *testing.T) {
			m, f, _ := groupsModel(t, 120, 40)
			selectGroup(m, tc.group)
			send(m, "e")
			if m.form == nil || len(listValues(m, fieldAdd)) == 0 {
				t.Fatalf("no users to add:\n%s", plain(m))
			}
			tickIn(m, 1)
			save(m)
			if m.dlg == nil || m.dlgSpec.Danger == "" || len(f.reqs) != 0 {
				t.Fatalf("ask %+v calls %v\n%s", m.dlgSpec, f.reqs, plain(m))
			}
			for _, want := range tc.says {
				if !strings.Contains(flatBoxes(m), want) {
					t.Errorf("lacks %q:\n%s", want, plain(m))
				}
			}
			if power := strings.Contains(flatBoxes(m), "allow practically everything on this host"); power != tc.power {
				t.Errorf("power warning %v, want %v", power, tc.power)
			}
		})
	}
}

func TestRemovingFromDockerAsksRedButNotAsPower(t *testing.T) {
	m, _, _ := groupsModel(t, 120, 40)
	selectGroup(m, "docker")
	send(m, "e", "tab")
	tickIn(m, 1)
	save(m)
	if m.dlg == nil || m.dlgSpec.Danger == "" || strings.Contains(flatBoxes(m), "allow practically everything on this host") {
		t.Fatalf("ask %+v\n%s", m.dlgSpec, plain(m))
	}
}

func TestGroupMembersOfSudoFollowTheAdminSwitch(t *testing.T) {
	m, f, _ := groupsModel(t, 120, 40)
	selectGroup(m, "sudo")
	send(m, "e")
	tickIn(m, 1) // demo-admin is in sudo already; the first user to add is svc-demo
	send(m, "tab")
	tickIn(m, 2)
	save(m)
	if m.dlg == nil || m.dlgSpec.Danger == "" || !strings.Contains(flatBoxes(m), "allow practically everything on this host") {
		t.Fatalf("ask %+v\n%s", m.dlgSpec, plain(m))
	}
	f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
	send(m, "y")
	// demo-admin is in qsysmin and demo-user is not: the second is taken out of sudo only.
	want := []string{"apply", "--confirm", "--output", "json", "--", "group.set-members", "sudo", "+svc-demo", "-demo-admin", "-demo-user:without-qsysmin"}
	if !slices.Equal(lastArgs(f), want) {
		t.Errorf("calls %+v, want %q", f.reqs, want)
	}
}

func TestGroupMembersAreRefusedEarlyForSelfAndTheLastAdmin(t *testing.T) {
	m, f, _ := groupsModel(t, 120, 40)
	selectGroup(m, "sudo")
	send(m, "e", "tab")
	tickIn(m, 1) // demo-admin is the first member and the only one with access to qsysmin
	save(m)
	if m.form == nil || !strings.Contains(flat(m), "last admin") || m.dlg != nil || len(f.reqs) != 0 {
		t.Fatalf("no refusal, ask %+v:\n%s", m.dlgSpec, plain(m))
	}
	m.hostUID = func() int { return 1001 }
	remove := m.form.lists[fieldRemove]
	remove.ticked = make([]bool, len(remove.values))
	remove.ticked[1] = true
	save(m)
	if m.form == nil || !strings.Contains(flat(m), "cannot take admin rights from yourself") || m.dlg != nil {
		t.Fatalf("no refusal, ask %+v:\n%s", m.dlgSpec, plain(m))
	}
}

func TestGroupMembersNeedAtLeastOneChoice(t *testing.T) {
	m, f, _ := groupsModel(t, 120, 40)
	selectGroup(m, "demo-team")
	send(m, "e")
	save(m)
	if m.form == nil || !strings.Contains(flat(m), "[!] Choose at least one user.") || m.dlg != nil || len(f.reqs) != 0 {
		t.Fatalf("no problem:\n%s", plain(m))
	}
	send(m, "esc")
	if m.form != nil || len(f.reqs) != 0 {
		t.Error("esc must close the form without running anything")
	}
}

func TestGroupMembersLeaveOutPrimaryUsersAndHeadings(t *testing.T) {
	m, _, _ := groupsModel(t, 120, 40)
	selectGroup(m, "demo-user")
	send(m, "e")
	if got := listValues(m, fieldAdd); slices.Contains(got, "demo-user") {
		t.Errorf("add offers the user whose primary group it is: %q", got)
	}
	send(m, "esc")
	markGroup(&m.hst.groups.page, 0)
	send(m, "e")
	if m.form != nil {
		t.Error("e on a heading must do nothing")
	}
}

func TestGroupMembersFormSpeaksGerman(t *testing.T) {
	m, _, _ := groupsModel(t, 120, 40)
	useLanguage(t, m, "de")
	selectGroup(m, "docker")
	if keys := m.hintText(m.hostKeys()); !strings.Contains(keys, "e Mitglieder") {
		t.Errorf("keys %q", keys)
	}
	send(m, "e")
	for _, want := range []string{"Mitglieder von docker", "Hinzufügen", "Entfernen", "praktisch alles"} {
		if !strings.Contains(flatBoxes(m), want) {
			t.Errorf("lacks %q:\n%s", want, plain(m))
		}
	}
	tickIn(m, 1)
	save(m)
	if m.dlg == nil || m.dlgSpec.Danger == "" || !strings.Contains(flatBoxes(m), "docker nutzen Docker") {
		t.Errorf("\n%s", plain(m))
	}
}
