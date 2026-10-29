package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// secretValue is the invented secret the tests type; it must never be drawn, copied or reported.
const secretValue = "fake-token-for-tests-only"

// f2 presses the save key, which approves in Access and saves a form.
func f2(m *Model) {
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyF2})
	drain(m, cmd)
}

// sample selects the second approval and opens its detail.
func openSample(m *Model) {
	send(m, "4", "down", "enter")
}

// accessFake is a service with two open approvals, one of them with a free command, and three credentials.
func accessFake() *fakeBackend {
	fb := newFake(control.VaultUnlocked)
	fb.approvals = []control.ApprovalSummary{
		{Project: "demo-app", Kind: "new", Changes: 3, Fingerprint: "fp-demo-000000000001"},
		{Project: "sample-app", Kind: "changed", Changes: 2, Fingerprint: "fp-sample-0000000002", FreeCommands: true},
	}
	fb.details = map[string][]control.ApprovalChange{
		"demo-app": {{Path: "name", New: "demo-app"}},
		"sample-app": {
			{Path: "images[0].ref", Old: "registry.example.test/web:1", New: "registry.example.test/web:2"},
			{Path: "hooks.post_deploy", New: "echo hello-test", Free: true},
		},
	}
	fb.credentials = []control.CredentialSummary{
		{Name: "registry-main", Kind: "registry", ID: "cred-0001", Uses: []control.CredentialUse{
			{Kind: "project", Name: "demo-app", Current: true, Approved: true},
			{Kind: "project", Name: "sample-app", Current: true}}},
		{Name: "mail-relay", Kind: "smtp", ID: "cred-0002"},
		{Name: "unused-token", Kind: "secret", ID: "cred-0003"},
	}
	fb.projects = []control.ProjectSummary{{Name: "demo-app", State: control.StateApproved}}
	return fb
}

func TestAccessListMarksFreeCommands(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		m := appModel(t, accessFake(), size[0], size[1])
		send(m, "4")
		view := plain(m)
		for _, want := range []string{"Approvals (2)", "demo-app", "sample-app", "⚠ free command", "Changes"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: the list lacks %q:\n%s", size[0], size[1], want, view)
			}
		}
		assertViewFits(t, view, size[0], size[1])
	}
}

func TestOverviewJumpLandsInTheAccessList(t *testing.T) {
	m := appModel(t, accessFake(), 80, 24)
	focusRow(t, m, "approvals")
	send(m, "enter")
	if m.area != areaAccess || len(m.acc.page.list.all) != 2 {
		t.Errorf("area %v with %d rows, want the Access list with 2", m.area, len(m.acc.page.list.all))
	}
	if view := plain(m); !strings.Contains(view, "sample-app") {
		t.Errorf("the list is not drawn:\n%s", view)
	}
}

func TestAccessDetailShowsOldNewAndFreeCommand(t *testing.T) {
	m := appModel(t, accessFake(), 80, 24)
	openSample(m)
	send(m, "right")
	view := plain(m)
	for _, want := range []string{"images[0].ref", "- registry.example.test/web:1", "+ registry.example.test/web:2",
		"hooks.post_deploy  (free command)", "+ echo hello-test", "- (none)"} {
		if !strings.Contains(view, want) {
			t.Errorf("the detail lacks %q:\n%s", want, view)
		}
	}
	send(m, "right", "down")
	if view := plain(m); !strings.Contains(view, "path: hooks.post_deploy") || !strings.Contains(view, "free: true") {
		t.Errorf("the raw tab lacks the entries:\n%s", view)
	}
	send(m, "esc")
	if m.acc.page.open {
		t.Error("esc keeps the detail open")
	}
}

func TestAccessWideShowsDetailBesideTheList(t *testing.T) {
	m := appModel(t, accessFake(), 120, 40)
	send(m, "4", "down")
	view := plain(m)
	if !strings.Contains(view, "Overview") || !strings.Contains(view, "Fingerprint") || !strings.Contains(view, "A free command line runs") {
		t.Errorf("no detail beside the list:\n%s", view)
	}
}

func TestAccessDiffValuesGoThroughTheRedactor(t *testing.T) {
	fb := accessFake()
	fb.details["sample-app"] = []control.ApprovalChange{{Path: "env.TOKEN", Old: "old-" + secretValue, New: "\x1b[31mnew-" + secretValue + "\nnext"}}
	m := appModel(t, fb, 80, 24)
	m.redactor.Add(secretValue)
	openSample(m)
	for range 3 {
		got := m.render()
		if strings.Contains(got, secretValue) || strings.Contains(got, "\x1b[31m") {
			t.Errorf("a registered secret or an escape sequence is drawn:\n%s", got)
		}
		send(m, "right")
	}
}

func TestApproveGivesTheShownFingerprintAndNeedsTheDetail(t *testing.T) {
	fb := accessFake()
	m := appModel(t, fb, 80, 24)
	send(m, "4", "down")
	f2(m)
	if fb.count("approve sample-app fp-sample-0000000002") != 0 || fb.count("change") != 0 || len(fb.approved) != 0 {
		t.Fatalf("approved without the detail on the screen: %v", fb.log())
	}
	if !m.acc.page.open || !strings.Contains(m.status, "Review") {
		t.Errorf("the detail did not open (open %v, status %q)", m.acc.page.open, m.status)
	}
	f2(m)
	if m.ov == nil {
		t.Fatal("no passphrase asked")
	}
	typed(m, testPass)
	send(m, "enter")
	if fb.count("approve sample-app fp-sample-0000000002") != 1 || len(fb.approved) != 1 {
		t.Errorf("calls = %v, want one approval with the shown fingerprint", fb.log())
	}
	if len(m.acc.page.list.all) != 1 || !strings.Contains(m.status, "Approved sample-app") {
		t.Errorf("the list was not reloaded: %v, status %q", m.acc.page.list.all, m.status)
	}
	if m.ovr.approvals == nil || len(m.ovr.approvals) != 1 {
		t.Errorf("the overview counter was not refreshed: %v", m.ovr.approvals)
	}
}

func TestApproveRefusedAsChangedReloadsAndNotes(t *testing.T) {
	fb := accessFake()
	fb.stale, fb.session = true, true
	m := appModel(t, fb, 80, 24)
	openSample(m)
	before := fb.count("approvals")
	f2(m)
	if fb.count("approve sample-app fp-sample-0000000002") != 1 {
		t.Fatalf("calls = %v", fb.log())
	}
	if fb.count("approvals") <= before || len(m.acc.page.list.all) != 2 {
		t.Errorf("the list was not read again: %v", fb.log())
	}
	if !strings.Contains(m.fail, "Changed meanwhile") {
		t.Errorf("no note: %q", m.fail)
	}
}

func TestDiscardAsksThenDiscardsAndReloads(t *testing.T) {
	fb := accessFake()
	fb.session = true
	m := appModel(t, fb, 80, 24)
	send(m, "4", "down", "d")
	if m.screen != screenConfirm || fb.count("discard sample-app") != 0 {
		t.Fatal("no question before the discard")
	}
	send(m, "n")
	if fb.count("discard sample-app") != 0 || m.screen != screenArea {
		t.Fatal("n discarded")
	}
	send(m, "d", "y")
	if fb.count("discard sample-app") != 1 || len(m.acc.page.list.all) != 1 {
		t.Errorf("calls = %v, rows %v", fb.log(), m.acc.page.list.all)
	}
}

func TestApproveAllAsksOnceForThePassphraseAndCallsOnce(t *testing.T) {
	for _, once := range []bool{false, true} {
		fb := accessFake()
		fb.once = once
		m := appModel(t, fb, 80, 24)
		send(m, "4", "a")
		if m.screen != screenConfirm || !strings.Contains(plain(m), "Approve all 2 open approvals?") {
			t.Fatalf("once=%v: no question:\n%s", once, plain(m))
		}
		send(m, "y")
		if m.ov == nil {
			t.Fatalf("once=%v: no passphrase asked", once)
		}
		typed(m, testPass)
		send(m, "enter")
		if fb.count("approve-all") != 1 || fb.count("session") != 1 || m.ov != nil {
			t.Errorf("once=%v: calls = %v, want one passphrase and one approve-all", once, fb.log())
		}
		if len(m.acc.page.list.all) != 0 || !strings.Contains(m.status, "Approved 2") {
			t.Errorf("once=%v: rows %v, status %q", once, m.acc.page.list.all, m.status)
		}
	}
}

func TestApproveAllShowsFailuresPerProject(t *testing.T) {
	fb := accessFake()
	fb.session = true
	fb.failApproveAll = []control.ApprovalFailure{{Project: "sample-app", Code: control.CodeChanged}}
	m := appModel(t, fb, 80, 24)
	send(m, "4", "a", "y")
	if !strings.Contains(m.fail, "1 failed") || !strings.Contains(m.fail, "sample-app") || !strings.Contains(m.fail, "Changed meanwhile") {
		t.Errorf("fail = %q", m.fail)
	}
	if len(m.acc.page.list.all) != 1 {
		t.Errorf("rows = %v", m.acc.page.list.all)
	}
}

func TestEscInTheOverlayRunsNothing(t *testing.T) {
	fb := accessFake()
	m := appModel(t, fb, 80, 24)
	send(m, "4", "a", "y")
	typed(m, testPass)
	send(m, "esc")
	if m.ov != nil || fb.count("approve-all") != 0 || fb.count("session") != 0 {
		t.Errorf("calls = %v", fb.log())
	}
	send(m, "a", "esc")
	if fb.count("approve-all") != 0 || m.screen != screenArea {
		t.Error("esc on the question approved")
	}
}

func TestDiscardAllFromTheCommandLine(t *testing.T) {
	fb := accessFake()
	fb.session = true
	m := appModel(t, fb, 80, 24)
	send(m, ":")
	typed(m, "access discard-all")
	send(m, "enter")
	if m.screen != screenConfirm || !strings.Contains(plain(m), "Discard all open approvals?") {
		t.Fatalf("no question:\n%s", plain(m))
	}
	send(m, "y")
	if fb.count("discard-all") != 1 || len(m.acc.page.list.all) != 0 {
		t.Errorf("calls = %v", fb.log())
	}
}

func TestAccessFilterAndCopy(t *testing.T) {
	m := appModel(t, accessFake(), 80, 24)
	send(m, "4", "/")
	typed(m, "sample")
	send(m, "enter")
	if len(m.acc.page.list.matches) != 1 {
		t.Fatalf("matches = %v", m.acc.page.list.matches)
	}
	_, cmd := m.Update(keyMsg("c"))
	if got := fmt.Sprint(cmd()); !strings.Contains(got, "sample-app") {
		t.Errorf("copy = %q", got)
	}
}

func TestAccessLockedVaultShowsNote(t *testing.T) {
	fb := accessFake()
	fb.vault = control.VaultLocked
	m := appModel(t, fb, 80, 24)
	send(m, "4")
	if view := plain(m); !strings.Contains(view, "locked") {
		t.Errorf("no note about the locked vault:\n%s", view)
	}
}
