package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// overviewFake is a service with open approvals and one project, the usual case of the overview.
func overviewFake() *fakeBackend {
	fb := newFake(control.VaultUnlocked)
	fb.approvals = []control.ApprovalSummary{{Project: "demo-app", Kind: "new"}, {Project: "sample-app", Kind: "changed"}}
	fb.projects = []control.ProjectSummary{{Name: "demo-app", State: control.StateApproved}}
	return fb
}

// focusRow moves the cursor of the overview to the row with the given id.
func focusRow(t *testing.T, m *Model, id string) {
	t.Helper()
	send(m, "1")
	for i, r := range m.overviewRows() {
		if r.id == id {
			m.ovr.cursor = i
			return
		}
	}
	t.Fatalf("the overview has no %q row", id)
}

func TestOverviewShowsEveryRowWithItsNextStep(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	view := plain(m)
	for _, want := range []string{"Version", "0.0.0-test", "/etc/sysmin-test", "manual", "2 open", "Press enter to open Access.",
		"1 total", "Updates", "no data", "Press enter to open Projects."} {
		if !strings.Contains(view, want) {
			t.Errorf("the overview lacks %q:\n%s", want, view)
		}
	}
}

func TestOverviewEnterJumpsToTheNamedPage(t *testing.T) {
	for _, c := range []struct {
		row  string
		area area
		sub  int
	}{
		{"approvals", areaAccess, 0},
		{"version", areaSettings, subUpdate},
		{"updates", areaProjects, 0},
	} {
		m := appModel(t, overviewFake(), 80, 24)
		focusRow(t, m, c.row)
		send(m, "enter")
		if m.area != c.area || (c.area == areaSettings && m.set.sub != c.sub) {
			t.Errorf("enter on %s leads to area %v page %d, want %v %d", c.row, m.area, m.set.sub, c.area, c.sub)
		}
	}
}

func TestOverviewWarnsAboutAutomaticUnlock(t *testing.T) {
	for mode, word := range map[string]string{control.UnlockTPM2: "automatic (TPM2)", control.UnlockHost: "automatic (host key)"} {
		fb := overviewFake()
		fb.unlockMode = mode
		m := appModel(t, fb, 80, 24)
		view := plain(m)
		if !strings.Contains(view, warnMark+" Unlock") || !strings.Contains(view, word) ||
			!strings.Contains(view, "Press enter to open Settings › Vault.") {
			t.Errorf("%s: no warning with its next step:\n%s", mode, view)
		}
		focusRow(t, m, "unlock")
		send(m, "enter")
		if m.area != areaSettings || m.set.sub != subVault {
			t.Errorf("%s: enter leads to area %v page %d, want Settings, Vault", mode, m.area, m.set.sub)
		}
	}
}

func TestOverviewFlagsProjectsInAnErrorState(t *testing.T) {
	fb := overviewFake()
	fb.projects = append(fb.projects, control.ProjectSummary{Name: "broken-app", State: "failed"})
	m := appModel(t, fb, 80, 24)
	if view := plain(m); !strings.Contains(view, "1 in error") {
		t.Errorf("a project in error is not flagged:\n%s", view)
	}
	focusRow(t, m, "projects")
	send(m, "enter")
	if m.area != areaProjects {
		t.Errorf("enter leads to area %v, want Projects", m.area)
	}
}

func TestOverviewWithoutVaultLeadsToTheSetup(t *testing.T) {
	m := appModel(t, newFake(control.VaultAbsent), 80, 24)
	view := plain(m)
	if !strings.Contains(view, "not set up") || !strings.Contains(view, "Press enter to set up the vault.") {
		t.Fatalf("no setup step:\n%s", view)
	}
	if strings.Contains(view, "Unlock") {
		t.Errorf("an unlock mode is shown without a vault:\n%s", view)
	}
	send(m, "down", "enter")
	if m.ov == nil || m.ov.kind != entrySetup {
		t.Error("enter does not open the setup")
	}
}

func TestOverviewWithALockedVaultHasNoDataAndUnlocks(t *testing.T) {
	fb := newFake(control.VaultLocked)
	m := appModel(t, fb, 80, 24)
	view := plain(m)
	if !strings.Contains(view, "Press ctrl+l to unlock it.") || fb.count("approvals") != 0 {
		t.Fatalf("locked overview:\n%s\ncalls %v", view, fb.log())
	}
	send(m, "down", "enter")
	if m.ov == nil || !m.ov.locked {
		t.Error("enter does not ask for the passphrase")
	}
}

func TestOverviewStaysVisibleWithoutTheService(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	fb.down = errDropped
	m := appModel(t, fb, 80, 24)
	view := plain(m)
	if !strings.Contains(view, "unreachable") || !strings.Contains(view, "Check that the service runs, then press ctrl+r.") {
		t.Errorf("the overview hides the missing service:\n%s", view)
	}
	fb.down = nil
	send(m, "down", "enter")
	if !m.serviceUp() {
		t.Error("enter on the service row does not ask again")
	}
}

func TestOverviewOffersARestartToASessionWithoutTheGroup(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	fb.down = fmt.Errorf("cannot reach the service: %w", client.ErrSessionGroup)
	m := appModel(t, fb, 120, 24)
	view := plain(m)
	if !strings.Contains(view, "session lacks group sysmin") || !strings.Contains(view, "Press enter to restart with sudo -g sysmin") {
		t.Fatalf("the overview does not name the missing group:\n%s", view)
	}
	if got := errorText(fb.down); got != "This login session lacks the group sysmin; run regroup or log in again." {
		t.Errorf("error text %q", got)
	}
	send(m, "down", "enter")
	if !m.regroupAsked || !m.quitting {
		t.Errorf("enter does not quit to restart: regroup %v, quitting %v", m.regroupAsked, m.quitting)
	}

	m = appModel(t, fb, 120, 24)
	send(m, ":")
	typed(m, "regroup")
	send(m, "enter")
	if !m.regroupAsked || !m.quitting {
		t.Errorf("the command does not quit to restart: regroup %v, quitting %v", m.regroupAsked, m.quitting)
	}
}

func TestOverviewDistinguishesStatesWithoutColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	fb := overviewFake()
	fb.unlockMode = control.UnlockHost
	m := appModel(t, fb, 80, 24)
	view := plain(m)
	for _, mark := range []string{"●", warnMark, "○"} {
		if !strings.Contains(view, mark) {
			t.Errorf("the mark %q is missing:\n%s", mark, view)
		}
	}
}

func TestOverviewIsClickable(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	m.render()
	var y int
	for i, line := range strings.Split(plain(m), "\n") {
		if strings.Contains(line, "Approvals") {
			y = i
		}
	}
	click := func() {
		m.render()
		for _, h := range m.hits {
			if h.y0 <= y && y < h.y1 && h.x0 == 0 {
				drain(m, h.act())
				return
			}
		}
		t.Fatal("no click target on the approvals row")
	}
	click()
	if m.area != areaOverview || m.overviewCurrent().id != "approvals" {
		t.Fatal("the first click does not mark the row")
	}
	click()
	if m.area != areaAccess {
		t.Errorf("the second click leads to %v, want Access", m.area)
	}
}
