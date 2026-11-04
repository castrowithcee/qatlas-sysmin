package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

const restoredFolderReport = `{"journal_id":"x","action":"folder.set-permissions","sentence":"Change /srv/demo-app and everything below it: owner demo-user, group demo-team; the owner gets read and write, the group read, everyone else no access.","result":"restored","check":"failed","check_error":"container demo-web-1: the container became unhealthy","restored":true,"reversible":false,"error":"check failed"}`

// openFolderForm reads path on Folders and opens the change form of the folder; count stands in for the
// file system.
func openFolderForm(t *testing.T, m *Model, path string, count func(string, bool) (int, error)) {
	t.Helper()
	m.folderCount = count
	runCommand(m, "go host folders")
	send(m, "enter")
	typed(m, path)
	send(m, "enter", "esc", "e")
}

func fixedCount(n int) func(string, bool) (int, error) {
	return func(string, bool) (int, error) { return n, nil }
}

func TestFolderFormIsPrefilledWithTheCurrentValues(t *testing.T) {
	m, _, _, _ := userModel(t, 120, 40)
	openFolderForm(t, m, "/srv/demo-app", fixedCount(1))
	view := flat(m)
	for _, want := range []string{"Change /srv/demo-app", "Owner", "root", "Group", "qsysmin", "Owner may", "read and write",
		"Group may", "read", "Others may", "nothing", "Everything below it", "no"} {
		if !strings.Contains(view, want) {
			t.Errorf("form lacks %q:\n%s", want, plain(m))
		}
	}
	if m.form == nil {
		t.Fatal("the form is not open")
	}
}

func TestFolderChangeNeedsAFolderAndIsOfferedOnlyForOne(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	runCommand(m, "go host folders")
	if strings.Contains(m.hintText(m.hostKeys()), "e change") {
		t.Error("e is offered before a folder was read")
	}
	send(m, "enter")
	typed(m, "/srv/demo-app/app.conf")
	send(m, "enter", "esc", "e")
	if m.form != nil || m.status == "" || strings.Contains(m.hintText(m.hostKeys()), "e change") {
		t.Errorf("a file must not open the form: status %q keys %v", m.status, m.hostKeys())
	}
	send(m, "enter")
	for range "/srv/demo-app/app.conf" {
		send(m, "backspace")
	}
	typed(m, "/srv/demo-app")
	send(m, "enter", "esc")
	if !strings.Contains(m.hintText(m.hostKeys()), "e change") || len(f.reqs) != 0 {
		t.Errorf("keys %v", m.hostKeys())
	}
}

func TestRecursiveFolderChangeNamesTheCountAndRunsOnlyAfterYes(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	openFolderForm(t, m, "/srv/demo-app", fixedCount(42))
	send(m, "tab", "tab", "tab", "tab", "tab", "right") // everything below it: yes
	save(m)
	view := flat(m)
	for _, want := range []string{"42 entries are changed", "and everything below it", "You can undo this under Host > Changes."} {
		if !strings.Contains(view, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, plain(m))
		}
	}
	if m.dlg == nil || (m.dlgSpec.Danger != "") || len(f.reqs) != 0 {
		t.Fatalf("a normal path is not red and nothing runs before y: ask %+v calls %v", m.dlgSpec, f.reqs)
	}
	f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
	send(m, "y")
	args := requestArgs(f)
	if len(args) != 1 || !slices.Contains(args[0], "recursive") || slices.Contains(args[0], "system-path") ||
		args[0][5] != hostchange.ActionFolderSetPermissions {
		t.Errorf("args %q", args)
	}
	if !strings.Contains(plain(m), "Done") {
		t.Errorf("no result:\n%s", plain(m))
	}
}

func TestSystemPathAsksInRedAndPassesTheAcknowledgementOnlyOnYes(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	openFolderForm(t, m, "/etc", fixedCount(1))
	if !strings.Contains(flat(m), "system path") {
		t.Errorf("the form does not name the system path:\n%s", plain(m))
	}
	save(m)
	if m.dlg == nil || m.dlgSpec.Danger == "" || !m.dlgSpec.Irreversible || !strings.Contains(flat(m), "stop services and block logins") {
		t.Fatalf("a system path is confirmed in red with its consequence: ask %+v\n%s", m.dlgSpec, plain(m))
	}
	send(m, "n")
	if len(f.reqs) != 0 {
		t.Fatalf("cancelling runs nothing: %v", f.reqs)
	}
	send(m, "e")
	save(m)
	f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
	send(m, "y")
	args := requestArgs(f)
	if len(args) != 1 || args[0][len(args[0])-1] != "system-path" {
		t.Errorf("the acknowledgement is part of the confirmed run: %q", args)
	}
}

func TestTooManyEntriesRefuseInsteadOfAskingAndOtherCountErrorsToo(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{hostchange.ErrTooManyEntries, "more than 5000 entries"},
		{errors.New("/srv/demo-app/x cannot be read"), "cannot be counted"},
	} {
		m, f, _, _ := userModel(t, 120, 40)
		openFolderForm(t, m, "/srv/demo-app", func(string, bool) (int, error) { return 0, tc.err })
		send(m, "tab", "tab", "tab", "tab", "tab", "right")
		save(m)
		if m.form == nil || m.dlg != nil || len(f.reqs) != 0 || !strings.Contains(flat(m), tc.want) {
			t.Errorf("%v: want the form with a refusal %q, ask %+v:\n%s", tc.err, tc.want, m.dlgSpec, plain(m))
		}
	}
}

func TestARestoredFolderChangeNamesContainerAndFinding(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	openFolderForm(t, m, "/srv/demo-app", fixedCount(1))
	save(m)
	f.answer = func(HostRequest) (string, error) { return restoredFolderReport, exitError(t, "exit 1") }
	send(m, "y")
	view := flat(m)
	for _, want := range []string{"previous state restored", "container demo-web-1: the container became unhealthy"} {
		if !strings.Contains(view, want) {
			t.Errorf("result lacks %q:\n%s", want, plain(m))
		}
	}
}

func TestFolderChangeSpeaksGerman(t *testing.T) {
	m, _, _, _ := userModel(t, 120, 40)
	useLanguage(t, m, "de")
	openFolderForm(t, m, "/srv/demo-app", fixedCount(1))
	for _, want := range []string{"Besitzer darf", "Gruppe darf", "Andere dürfen", "Alles darunter"} {
		if !strings.Contains(flat(m), want) {
			t.Errorf("German form lacks %q:\n%s", want, plain(m))
		}
	}
}
