package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

const accessSentence = "Set up the stack folder /srv/demo-stack for the service."

const accessPreviewJSON = `{"action":"stack.setup-access","sentence":"` + accessSentence + `","commands":["chown root:sysmin /srv/demo-stack","chmod 0750 /srv/demo-stack","[builtin] setfacl -m g:sysmin:x /mnt/nvme","[builtin] setfacl -m g:sysmin:x /mnt/nvme/stacks"],"preview":{
 "path":"/srv/demo-stack",
 "stack":{"name":"demo-stack","path":"/srv/demo-stack","running":true,"changes":[
   {"path":"/srv/demo-stack","role":"stack folder","before":{"owner":"root","group":"root","mode":"0700"},"after":{"owner":"root","group":"sysmin","mode":"0750"}}],"kept":[]},
 "parents":[{"path":"/mnt/nvme","current":{"owner":"root","group":"root","mode":"0700"},"has_acl":false},
            {"path":"/mnt/nvme/stacks","current":{"owner":"root","group":"root","mode":"0700"},"has_acl":false}],
 "others_lose_access":true,"nothing_to_do":false}}`

const accessNothingJSON = `{"action":"stack.setup-access","sentence":"","commands":[],"preview":{"path":"/srv/demo-stack","stack":{"name":"demo-stack","path":"/srv/demo-stack","running":true,"changes":[],"kept":[]},"parents":[],"others_lose_access":false,"nothing_to_do":true}}`

const accessRefusedJSON = `{"journal_id":"","action":"stack.setup-access","sentence":"","commands":[],"result":"refused","check":"","check_error":"","restored":false,"reversible":false,"error":"/usr/lib/demo-stack is a system path","warning":""}`

const accessApplied = `{"journal_id":"20301003T090000.000000Z-eeeeee","action":"stack.setup-access","sentence":"` + accessSentence + `","commands":[],"result":"applied","check":"passed","check_error":"","restored":false,"reversible":true,"error":"","warning":""}`

const accessRestored = `{"journal_id":"x","action":"stack.setup-access","sentence":"` + accessSentence + `","result":"restored","check":"failed","check_error":"the service cannot read /srv/demo-stack/compose.yaml","restored":true,"reversible":false,"error":"check failed"}`

// accessBlockedFake is the fake service whose proposal is blocked by missing access.
func accessBlockedFake() *fakeBackend {
	fb := adoptFake()
	fb.proj.adopt.Problems = []control.AdoptProblem{{Code: "missing-access", Blocking: true,
		Message: "the service user demo-user may not pass /mnt/nvme",
		Hint:    "Give demo-user the access, for example: sudo setfacl -m u:demo-user:x /mnt/nvme",
		Access:  []control.AdoptAccess{{Path: "/mnt/nvme", Right: control.AccessPass}}}}
	return fb
}

func accessModel(t *testing.T, fb *fakeBackend, w, h int) (*Model, *fakeSudo) {
	t.Helper()
	m := projectsModel(t, fb, w, h)
	f := &fakeSudo{}
	m.hostReg, m.hostRun, m.hostExec, m.hostBg = hostActions(t), f.run, f.exec, f.bg
	return m, f
}

func accessAnswer(f *fakeSudo, preview, report string) { stackAnswer(f, preview, report) }

func TestAdoptMissingAccessOffersTheSetupInsteadOfTheCommand(t *testing.T) {
	m, f := accessModel(t, accessBlockedFake(), 120, 40)
	adoptAsk(t, m, "/srv/demo-stack")
	view := boxText(m)
	for _, want := range []string{"the service user demo-user may not pass /mnt/nvme", "Access is missing. Press y to set it up", "y set up access"} {
		if !strings.Contains(view, want) {
			t.Errorf("the proposal lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "setfacl") || strings.Contains(view, "sudo ") {
		t.Errorf("the proposal shows a command to type:\n%s", view)
	}
	if len(f.reqs) != 0 {
		t.Fatalf("something ran before y: %v", f.reqs)
	}
}

func TestAdoptSetsUpAccessAndContinuesWithTheProposal(t *testing.T) {
	fb := accessBlockedFake()
	open := adoptFake().proj.adopt
	open.Problems = nil
	fb.proj.adoptQueue = []control.AdoptResult{fb.proj.adopt, open}
	m, f := accessModel(t, fb, 120, 40)
	accessAnswer(f, accessPreviewJSON, accessApplied)
	adoptAsk(t, m, "/srv/demo-stack")
	send(m, "y")
	args := requestArgs(f)
	if len(args) != 1 || !slices.Equal(args[0], []string{"preview", "--output", "json", "--", hostchange.ActionStackSetupAccess, "/srv/demo-stack"}) {
		t.Fatalf("the preview was not asked as root first: %q", args)
	}
	view := boxText(m)
	for _, want := range []string{"Set up access?", "/srv/demo-stack is set up for the service: root:sysmin",
		"/mnt/nvme", "/mnt/nvme/stacks", "$ chown root:sysmin /srv/demo-stack", "$ [builtin] setfacl -m g:sysmin:x /mnt/nvme",
		"Other users except root have no access", "You can undo this under Host > Changes."} {
		if !strings.Contains(view, want) {
			t.Errorf("the question lacks %q:\n%s", want, view)
		}
	}
	if len(fb.proj.adoptAsked) != 1 {
		t.Fatalf("the proposal was asked again before the setup: %v", fb.proj.adoptAsked)
	}
	send(m, "y")
	args = requestArgs(f)
	want := []string{"apply", "--yes", "--output", "json", "--", hostchange.ActionStackSetupAccess, "/srv/demo-stack"}
	if len(args) != 2 || !slices.Equal(args[1], want) {
		t.Fatalf("apply = %q", args)
	}
	if len(fb.proj.adoptAsked) != 2 || fb.proj.adoptAsked[1] != fb.proj.adoptAsked[0] {
		t.Fatalf("the proposal is not asked again with the same input: %+v", fb.proj.adoptAsked)
	}
	view = boxText(m)
	if !m.projectsDialogOpen() || !strings.Contains(view, "Access was set up") || !strings.Contains(view, "enter take over") {
		t.Fatalf("the adoption does not go on:\n%s", view)
	}
	if !m.hst.chg.stale {
		t.Error("the journal is not marked as out of date")
	}
	send(m, "enter")
	if m.prj.flow == nil || m.prj.flow.draft.dir != "/srv/demo-stack" {
		t.Fatalf("taking over does not open the setup: %+v", m.prj.flow)
	}
}

func TestAdoptAccessNoLeavesEverythingAsIs(t *testing.T) {
	for _, key := range []string{"n", "esc"} {
		fb := accessBlockedFake()
		m, f := accessModel(t, fb, 120, 40)
		accessAnswer(f, accessPreviewJSON, accessApplied)
		adoptAsk(t, m, "/srv/demo-stack")
		send(m, "y", key)
		if len(f.reqs) != 1 || len(fb.proj.adoptAsked) != 1 {
			t.Errorf("%s: requests = %v, proposals = %d", key, requestArgs(f), len(fb.proj.adoptAsked))
		}
		view := boxText(m)
		if !m.projectsDialogOpen() || !strings.Contains(view, "Access is missing. Press y") || strings.Contains(view, "setfacl") {
			t.Errorf("%s: the blocked proposal is not back:\n%s", key, view)
		}
	}
}

func TestAdoptAccessRefusalIsShownInTheProposal(t *testing.T) {
	for name, preview := range map[string]string{"refused": accessRefusedJSON, "nothing": accessNothingJSON} {
		m, f := accessModel(t, accessBlockedFake(), 120, 40)
		accessAnswer(f, preview, accessApplied)
		adoptAsk(t, m, "/srv/demo-stack")
		send(m, "y")
		view := boxText(m)
		want := "is a system path"
		if name == "nothing" {
			want = "it is set up already"
		}
		if !strings.Contains(view, want) || strings.Contains(view, "Press y") || strings.Contains(view, "setfacl") || strings.Contains(view, "Set up access?") {
			t.Errorf("%s: the reason is not shown in the proposal:\n%s", name, view)
		}
		send(m, "y")
		if len(f.reqs) != 1 {
			t.Errorf("%s: y after a refusal ran something: %v", name, requestArgs(f))
		}
	}
}

func TestAdoptAccessFailureKeepsTheProposalBlocked(t *testing.T) {
	fb := accessBlockedFake()
	m, f := accessModel(t, fb, 120, 40)
	accessAnswer(f, accessPreviewJSON, accessRestored)
	adoptAsk(t, m, "/srv/demo-stack")
	send(m, "y", "y")
	if len(fb.proj.adoptAsked) != 1 {
		t.Errorf("the proposal was asked again after a failed setup: %v", fb.proj.adoptAsked)
	}
	if view := boxText(m); !strings.Contains(view, "the service cannot read /srv/demo-stack/compose.yaml") {
		t.Errorf("the failure is not shown:\n%s", view)
	}
}

func TestAdoptAccessStillMissingIsNotOfferedAgain(t *testing.T) {
	fb := accessBlockedFake()
	still := accessBlockedFake().proj.adopt
	still.Problems[0].Message = "the service user demo-user may not read /home/demo-user/.env"
	still.Problems[0].Access = []control.AdoptAccess{{Path: "/home/demo-user/.env", Right: control.AccessRead}}
	fb.proj.adoptQueue = []control.AdoptResult{fb.proj.adopt, still}
	m, f := accessModel(t, fb, 120, 40)
	accessAnswer(f, accessPreviewJSON, accessApplied)
	adoptAsk(t, m, "/srv/demo-stack")
	send(m, "y", "y")
	view := boxText(m)
	for _, want := range []string{"may not read /home/demo-user/.env", "The setup covers the stack folder"} {
		if !strings.Contains(view, want) {
			t.Errorf("the remaining access is not shown (%q):\n%s", want, view)
		}
	}
	if strings.Contains(view, "Press y") || strings.Contains(view, "set up access ·") {
		t.Errorf("the setup is offered again:\n%s", view)
	}
	send(m, "y")
	if len(f.reqs) != 2 {
		t.Errorf("y ran something: %v", requestArgs(f))
	}
}

func TestAdoptAccessWithoutSudoNamesTheReason(t *testing.T) {
	m, f := accessModel(t, accessBlockedFake(), 120, 40)
	f.buildErr = ErrNoSudo
	adoptAsk(t, m, "/srv/demo-stack")
	send(m, "y")
	if m.projectsDialogOpen() || m.screen != screenConfirm || !strings.Contains(boxText(m), "sudo") {
		t.Errorf("no notice about sudo:\n%s", plain(m))
	}
}

func TestGoldenProjectsAdoptAccess(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			utcZone(t)
			m := goldenApp(t, accessBlockedFake(), size[0], size[1])
			f := &fakeSudo{}
			m.hostReg, m.hostRun, m.hostExec, m.hostBg = hostActions(t), f.run, f.exec, f.bg
			accessAnswer(f, accessPreviewJSON, accessApplied)
			send(m, "2")
			adoptAsk(t, m, "/srv/demo-stack")
			assertViewFits(t, plain(m), size[0], size[1])
			assertGolden(t, fmt.Sprintf("projects_adopt_access_proposal_%dx%d", size[0], size[1]), m.render())
			send(m, "y")
			assertViewFits(t, plain(m), size[0], size[1])
			assertGolden(t, fmt.Sprintf("projects_adopt_access_question_%dx%d", size[0], size[1]), m.render())
		})
	}
}

// boxText is the text inside the topmost box of the view as one line of words, whatever the list behind it shows.
func boxText(m *Model) string {
	lines := strings.Split(plain(m), "\n")
	top := -1
	for i, line := range lines {
		if strings.ContainsRune(line, '╭') {
			top = i
		}
	}
	if top < 0 {
		return ""
	}
	r := []rune(lines[top])
	left, right := slices.Index(r, '╭'), slices.Index(r, '╮')
	if i := slices.Index(r, '╭'); i >= 0 {
		for j := len(r) - 1; j >= 0; j-- {
			if r[j] == '╭' {
				left = j
				break
			}
		}
		for j := len(r) - 1; j >= 0; j-- {
			if r[j] == '╮' {
				right = j
				break
			}
		}
	}
	var words []string
	for _, line := range lines[top+1:] {
		r := []rune(line)
		if len(r) > right && r[left] == '│' && r[right] == '│' {
			words = append(words, strings.Fields(string(r[left+1:right]))...)
		}
	}
	return strings.Join(words, " ")
}
