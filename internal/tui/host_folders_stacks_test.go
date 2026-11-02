package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

const stackSentence = "Set up the stack folder /srv/demo-stack for the service (2 stacks: web, api)."

const stackPreviewJSON = `{"action":"folder.setup-stacks","sentence":"` + stackSentence + `","commands":["chown root:sysmin /srv/demo-stack","chmod 0750 /srv/demo-stack","chmod 0640 /srv/demo-stack/web/compose.yaml"],"preview":{
 "path":"/srv/demo-stack",
 "folder":{"path":"/srv/demo-stack","role":"folder","before":{"owner":"demo-user","group":"demo-user","mode":"0755"},"after":{"owner":"root","group":"sysmin","mode":"0750"}},
 "stacks":[
  {"name":"web","path":"/srv/demo-stack/web","running":true,"changes":[
    {"path":"/srv/demo-stack/web","role":"stack folder","before":{"owner":"demo-user","group":"demo-user","mode":"0755"},"after":{"owner":"root","group":"sysmin","mode":"0750"}},
    {"path":"/srv/demo-stack/web/compose.yaml","role":"compose file","before":{"owner":"demo-user","group":"demo-user","mode":"0644"},"after":{"owner":"root","group":"sysmin","mode":"0640"}},
    {"path":"/srv/demo-stack/web/.env","role":"env file","before":{"owner":"demo-user","group":"demo-user","mode":"0644"},"after":{"owner":"root","group":"sysmin","mode":"0660"}}],
   "kept":[{"path":"/srv/demo-stack/web/nginx.conf","reason":"a container mounts it and the service must not change who owns it"}]},
  {"name":"api","path":"/srv/demo-stack/api","running":false,"changes":[
    {"path":"/srv/demo-stack/api","role":"stack folder","before":{"owner":"demo-user","group":"demo-user","mode":"0755"},"after":{"owner":"root","group":"sysmin","mode":"0750"}},
    {"path":"/srv/demo-stack/api/compose.yaml","role":"compose file","before":{"owner":"demo-user","group":"demo-user","mode":"0644"},"after":{"owner":"root","group":"sysmin","mode":"0640"}}],
   "kept":[]}],
 "kept":[],"others_lose_access":true,"nothing_to_do":false}}`

const stackNothingJSON = `{"action":"folder.setup-stacks","sentence":"","commands":[],"preview":{"path":"/srv/demo-stack","folder":null,"stacks":[],"kept":[],"others_lose_access":true,"nothing_to_do":true}}`

const stackRefusedJSON = `{"journal_id":"","action":"folder.setup-stacks","sentence":"","commands":[],"result":"refused","check":"","check_error":"","restored":false,"reversible":false,"error":"/srv/demo-stack is not below an allowed folder","warning":""}`

const stackRestoredReport = `{"journal_id":"x","action":"folder.setup-stacks","sentence":"` + stackSentence + `","result":"restored","check":"failed","check_error":"stack web: the service cannot read /srv/demo-stack/web/compose.yaml: permission denied","restored":true,"reversible":false,"error":"check failed"}`

// dialogText is the text inside the box of the dialog, as one line of words.
func dialogText(m *Model) string {
	var words []string
	for _, line := range strings.Split(plain(m), "\n") {
		if i, j := strings.Index(line, "│"), strings.LastIndex(line, "│"); i >= 0 && j > i {
			words = append(words, strings.Fields(line[i+len("│"):j])...)
		}
	}
	return strings.Join(words, " ")
}

// stackAnswer answers the preview with preview and any apply with report.
func stackAnswer(f *fakeSudo, preview, report string) {
	f.answer = func(r HostRequest) (string, error) {
		if len(r.Args) > 0 && r.Args[0] == "preview" {
			return preview, nil
		}
		return report, nil
	}
}

// openStackConfirm reads path on Folders and presses s.
func openStackConfirm(t *testing.T, m *Model, path string) {
	t.Helper()
	runCommand(m, "go host folders")
	send(m, "enter")
	typed(m, path)
	send(m, "enter", "esc", "s")
}

func TestStackSetupReadsThePreviewThroughSudoAndConfirmsBeforeAnythingRuns(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	stackAnswer(f, stackPreviewJSON, reportApplied)
	openStackConfirm(t, m, "/srv/demo-stack")
	args := requestArgs(f)
	if len(args) != 1 || args[0][0] != "preview" || args[0][len(args[0])-2] != hostchange.ActionFolderSetupStacks || args[0][len(args[0])-1] != "/srv/demo-stack" {
		t.Fatalf("the preview was not asked as root first: %q", args)
	}
	view := dialogText(m)
	for _, want := range []string{
		"Set up the stack folder /srv/demo-stack for the service?",
		"web (running): folder root:sysmin 0750 (was demo-user:demo-user 0755), compose.yaml 0640 (was 0644), .env 0660 (was 0644)",
		"api: folder root:sysmin 0750 (was demo-user:demo-user 0755), compose.yaml 0640 (was 0644)",
		"$ chown root:sysmin /srv/demo-stack",
		"Stays: nginx.conf (a container mounts it",
		"Other users except root have no access to these folders and files afterwards",
		"You can undo this under Host > Changes.",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, plain(m))
		}
	}
	if m.confirm == nil || len(f.reqs) != 1 {
		t.Fatalf("only the preview may have run: %d calls", len(f.reqs))
	}
	send(m, "y")
	args = requestArgs(f)
	want := []string{"apply", "--yes", "--output", "json", "--", hostchange.ActionFolderSetupStacks, "/srv/demo-stack"}
	if len(args) != 2 || strings.Join(args[1], " ") != strings.Join(want, " ") {
		t.Errorf("apply args %q", args)
	}
	if !strings.Contains(plain(m), "Done") {
		t.Errorf("no result:\n%s", plain(m))
	}
}

func TestStackSetupCancelledRunsNothing(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	stackAnswer(f, stackPreviewJSON, reportApplied)
	openStackConfirm(t, m, "/srv/demo-stack")
	send(m, "n")
	if len(f.reqs) != 1 || m.confirm != nil {
		t.Errorf("cancel ran something: %d calls", len(f.reqs))
	}
}

func TestStackSetupSaysWhenThereIsNothingToDo(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	stackAnswer(f, stackNothingJSON, "")
	openStackConfirm(t, m, "/srv/demo-stack")
	view := dialogText(m)
	if !strings.Contains(view, "Nothing to set up in /srv/demo-stack") || m.confirm == nil || !m.confirm.notice {
		t.Errorf("no message:\n%s", plain(m))
	}
	send(m, "y")
	if len(f.reqs) != 1 {
		t.Errorf("y on the message ran something: %d calls", len(f.reqs))
	}
}

func TestStackSetupShowsARefusalOfThePreview(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	stackAnswer(f, stackRefusedJSON, "")
	openStackConfirm(t, m, "/srv/demo-stack")
	if view := dialogText(m); !strings.Contains(view, "Not changed: /srv/demo-stack is not below an allowed folder") {
		t.Errorf("no refusal:\n%s", plain(m))
	}
}

func TestStackSetupShowsTheFindingWhenEverythingWasTakenBack(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	stackAnswer(f, stackPreviewJSON, stackRestoredReport)
	openStackConfirm(t, m, "/srv/demo-stack")
	send(m, "y")
	view := dialogText(m)
	for _, want := range []string{"stack web: the service cannot read /srv/demo-stack/web/compose.yaml", "previous state is back"} {
		if !strings.Contains(view, want) {
			t.Errorf("result lacks %q:\n%s", want, plain(m))
		}
	}
}

func TestStackSetupOffersTheKeyOnlyForAFolder(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	runCommand(m, "go host folders")
	if strings.Contains(strings.Join(m.hostKeys(), "|"), "s set up stacks") {
		t.Error("s is offered before a folder was read")
	}
	send(m, "enter")
	typed(m, "/srv/demo-stack/app.conf")
	send(m, "enter", "esc", "s")
	if m.confirm != nil || len(f.reqs) != 0 || m.status == "" {
		t.Errorf("a file must not start the setup: status %q, calls %d", m.status, len(f.reqs))
	}
}

func TestStackSetupCutsALongListToTheRoomOfTheScreen(t *testing.T) {
	m, f, _, _ := userModel(t, 80, 24)
	var stacks []string
	for i := 0; i < 12; i++ {
		stacks = append(stacks, fmt.Sprintf(`{"name":"app%d","path":"/srv/demo-stack/app%d","running":false,"changes":[{"path":"/srv/demo-stack/app%d/compose.yaml","role":"compose file","before":{"owner":"demo-user","group":"demo-user","mode":"0644"},"after":{"owner":"root","group":"sysmin","mode":"0640"}}],"kept":[]}`, i, i, i))
	}
	preview := `{"sentence":"x","preview":{"path":"/srv/demo-stack","folder":null,"stacks":[` + strings.Join(stacks, ",") +
		`],"kept":[],"others_lose_access":true,"nothing_to_do":false}}`
	stackAnswer(f, preview, reportApplied)
	openStackConfirm(t, m, "/srv/demo-stack")
	view := plain(m)
	if !strings.Contains(dialogText(m), "more entries, not shown") || !strings.Contains(view, "Apply") || strings.Contains(view, "app11") {
		t.Errorf("long list not cut:\n%s", view)
	}
}
