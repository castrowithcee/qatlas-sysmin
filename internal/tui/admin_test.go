package tui

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// gated is the changing action the tests gate: it counts on the fake service when it is allowed.
func gated(fb *fakeBackend, done *[]error) change {
	return change{call: fb.change, done: func(_ *Model, err error) tea.Cmd {
		*done = append(*done, err)
		return nil
	}}
}

func TestStartsReadOnlyWithoutAPassphrase(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m := appModel(t, fb, 80, 24)
	if m.area != areaOverview || m.screen != screenArea {
		t.Fatalf("area %v screen %v, want the overview", m.area, m.screen)
	}
	if got := fb.log(); len(got) != 1 || got[0] != "status" {
		t.Errorf("calls = %v, want one status and nothing else", got)
	}
	if view := plain(m); !strings.Contains(view, "vault open") || !strings.Contains(view, "admin off") {
		t.Errorf("header lacks the states:\n%s", view)
	}
}

func TestChangeWithoutSessionAsksOnceAndRunsAfterwards(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m := appModel(t, fb, 80, 24)
	var done []error
	drain(m, m.requireAdmin(gated(fb, &done)))
	if m.ov == nil || m.screen != screenPrompt {
		t.Fatal("no passphrase overlay opened")
	}
	if fb.count("change") != 0 {
		t.Fatal("the action ran before the passphrase")
	}
	typed(m, testPass)
	if view := plain(m); strings.Contains(view, testPass) {
		t.Errorf("the passphrase is on screen:\n%s", view)
	}
	send(m, "enter")
	if fb.count("change") != 1 || fb.changes != 1 || len(done) != 1 || done[0] != nil {
		t.Errorf("change calls %d, effects %d, outcomes %v; want the action once", fb.count("change"), fb.changes, done)
	}
	if m.ov != nil || m.screen != screenArea {
		t.Error("the overlay stays after the action")
	}
	if fb.count("unlock") != 0 || fb.count("session") != 1 {
		t.Errorf("calls = %v", fb.log())
	}
	if !strings.Contains(plain(m), "admin 15 min") {
		t.Errorf("the header does not show the session:\n%s", plain(m))
	}
	// With the session open the next change runs at once.
	drain(m, m.requireAdmin(gated(fb, &done)))
	if m.ov != nil || fb.changes != 2 {
		t.Errorf("the second change asked again or did not run: overlay %v, effects %d", m.ov != nil, fb.changes)
	}
}

func TestEscCancelsOverlayAndAction(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m := appModel(t, fb, 80, 24)
	var done []error
	drain(m, m.requireAdmin(gated(fb, &done)))
	typed(m, testPass)
	send(m, "esc")
	if m.ov != nil || m.screen != screenArea {
		t.Error("esc leaves the overlay open")
	}
	if fb.count("change") != 0 || fb.count("session") != 0 || len(done) != 0 {
		t.Errorf("calls = %v, outcomes %v; want none", fb.log(), done)
	}
}

func TestAnswerOfACancelledEntryRunsNothing(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m := appModel(t, fb, 80, 24)
	var done []error
	drain(m, m.requireAdmin(gated(fb, &done)))
	typed(m, testPass)
	_, cmd := m.Update(keyMsg("enter"))
	send(m, "esc")
	drain(m, cmd)
	if fb.count("change") != 0 {
		t.Error("the action ran for an entry that was cancelled")
	}
}

func TestWrongPassphraseKeepsTheOverlayWithTheError(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m := appModel(t, fb, 80, 24)
	var done []error
	drain(m, m.requireAdmin(gated(fb, &done)))
	typed(m, "wrong horse test")
	send(m, "enter")
	if m.ov == nil {
		t.Fatal("the overlay closed on a wrong passphrase")
	}
	view := plain(m)
	if !strings.Contains(view, "Wrong passphrase.") {
		t.Errorf("no error in the overlay:\n%s", view)
	}
	if strings.Contains(view, "wrong horse") || m.ov.inputs[0].Value() != "" {
		t.Error("the typed passphrase is still there")
	}
	if fb.count("change") != 0 {
		t.Error("the action ran")
	}
	typed(m, testPass)
	send(m, "enter")
	if m.ov != nil || fb.changes != 1 {
		t.Errorf("the right passphrase did not finish it: overlay %v, effects %d", m.ov != nil, fb.changes)
	}
}

func TestEmptyPassphraseIsRefusedLocally(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m := appModel(t, fb, 80, 24)
	drain(m, m.requireAdmin(change{call: fb.change}))
	send(m, "enter")
	if m.ov == nil || m.fail != "The passphrase is empty." || fb.count("session") != 0 {
		t.Errorf("fail %q, calls %v", m.fail, fb.log())
	}
}

func TestCtrlLUnlocksALockedVaultAndOpensTheSession(t *testing.T) {
	fb := newFake(control.VaultLocked)
	m := appModel(t, fb, 80, 24)
	if !strings.Contains(plain(m), "vault locked") {
		t.Fatalf("the header does not show the lock:\n%s", plain(m))
	}
	send(m, "ctrl+l")
	if m.ov == nil || !strings.Contains(plain(m), "Unlock vault") {
		t.Fatalf("no unlock overlay:\n%s", plain(m))
	}
	typed(m, testPass)
	send(m, "enter")
	calls := strings.Join(fb.log(), " ")
	if !strings.Contains(calls, "unlock session") {
		t.Errorf("calls = %q, want unlock before session", calls)
	}
	if m.ov != nil || !strings.Contains(plain(m), "vault open") || !strings.Contains(plain(m), "admin 15 min") {
		t.Errorf("not unlocked with a session:\n%s", plain(m))
	}
}

func TestCtrlLWithoutSessionOpensItWithoutAnAction(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m := appModel(t, fb, 80, 24)
	send(m, "ctrl+l")
	typed(m, testPass)
	send(m, "enter")
	if fb.count("session") != 1 || fb.count("unlock") != 0 || fb.count("change") != 0 || !fb.session {
		t.Errorf("calls = %v", fb.log())
	}
}

func TestCtrlLEndsTheSession(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	fb.session = true
	m := appModel(t, fb, 80, 24)
	if !strings.Contains(plain(m), "admin 15 min") {
		t.Fatalf("no session at the start:\n%s", plain(m))
	}
	send(m, "ctrl+l")
	if fb.count("end") != 1 || fb.session || m.ov != nil {
		t.Errorf("calls = %v, session %v", fb.log(), fb.session)
	}
	if !strings.Contains(plain(m), "admin off") {
		t.Errorf("the header still shows a session:\n%s", plain(m))
	}
}

func TestAdminRequiredAfterExpiryAsksOnceMoreAndRepeats(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	fb.session = true
	m := appModel(t, fb, 80, 24)
	fb.expire() // the interface still believes in the session
	var done []error
	drain(m, m.requireAdmin(gated(fb, &done)))
	if m.ov == nil || !m.ov.retried || fb.count("change") != 1 {
		t.Fatalf("overlay %v, change calls %d; want the overlay after one refused try", m.ov != nil, fb.count("change"))
	}
	if len(done) != 0 {
		t.Errorf("the outcome was reported before the repeat: %v", done)
	}
	typed(m, testPass)
	send(m, "enter")
	if fb.count("change") != 2 || fb.changes != 1 || len(done) != 1 || done[0] != nil || m.ov != nil {
		t.Errorf("change calls %d, effects %d, outcomes %v", fb.count("change"), fb.changes, done)
	}
}

func TestAdminRequiredTwiceStopsWithAMessage(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	fb.session = true
	m := appModel(t, fb, 80, 24)
	var done []error
	refuse := change{call: func(Backend) error {
		fb.record("change")
		return &control.Error{Code: control.CodeAdminRequired}
	}, done: func(_ *Model, err error) tea.Cmd { done = append(done, err); return nil }}
	drain(m, m.requireAdmin(refuse))
	typed(m, testPass)
	send(m, "enter")
	if m.ov != nil || fb.count("change") != 2 || len(done) != 1 || m.fail != "The admin session is closed." {
		t.Errorf("overlay %v, calls %v, outcomes %v, fail %q", m.ov != nil, fb.log(), done, m.fail)
	}
}

func TestNoVaultLeadsToTheSetup(t *testing.T) {
	fb := newFake(control.VaultAbsent)
	m := appModel(t, fb, 80, 24)
	view := plain(m)
	if !strings.Contains(view, "no vault") || !strings.Contains(view, "Press enter to set up the vault.") {
		t.Fatalf("the overview does not lead to the setup:\n%s", view)
	}
	var done []error
	if cmd := m.requireAdmin(gated(fb, &done)); cmd != nil || m.ov != nil || m.fail != "There is no vault yet." {
		t.Errorf("a change without a vault: overlay %v, fail %q", m.ov != nil, m.fail)
	}
	send(m, "enter")
	if m.ov == nil || !strings.Contains(plain(m), "Set up vault") {
		t.Fatalf("no setup overlay:\n%s", plain(m))
	}
	// Empty and differing entries are refused before the service hears of them.
	send(m, "enter", "enter")
	if m.fail != "The passphrase is empty." {
		t.Errorf("fail = %q", m.fail)
	}
	typed(m, testPass)
	send(m, "tab")
	typed(m, "another horse test")
	send(m, "enter")
	if m.fail != "The passphrases differ." || fb.count("create") != 0 || m.ov == nil {
		t.Errorf("fail %q, calls %v", m.fail, fb.log())
	}
	if m.ov.inputs[0].Value() != "" || m.ov.inputs[1].Value() != "" || m.ov.focus != 0 {
		t.Error("the fields were not cleared")
	}
	typed(m, testPass)
	send(m, "enter")
	typed(m, testPass)
	send(m, "enter")
	if fb.count("create") != 1 || fb.vault != control.VaultUnlocked || fb.pass != testPass || m.ov != nil {
		t.Errorf("calls %v, vault %q", fb.log(), fb.vault)
	}
	if view := plain(m); !strings.Contains(view, "vault open") || strings.Contains(view, testPass) {
		t.Errorf("after the setup:\n%s", view)
	}
}

func TestCtrlLWithoutVaultOpensTheSetup(t *testing.T) {
	m := appModel(t, newFake(control.VaultAbsent), 80, 24)
	send(m, "ctrl+l")
	if m.ov == nil || m.ov.kind != entrySetup {
		t.Error("ctrl+l does not open the setup")
	}
}

func TestUnreachableServiceShowsTheNextStep(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	fb.down = errDropped
	m := appModel(t, fb, 80, 24)
	view := plain(m)
	if !strings.Contains(view, "service unreachable") || !strings.Contains(view, "then press ctrl+r") {
		t.Errorf("no state or next step:\n%s", view)
	}
	var done []error
	drain(m, m.requireAdmin(gated(fb, &done)))
	if m.ov != nil || m.fail != "Service unreachable." {
		t.Errorf("overlay %v, fail %q", m.ov != nil, m.fail)
	}
	fb.down = nil
	send(m, "ctrl+r")
	if !strings.Contains(plain(m), "vault open") {
		t.Errorf("retry did not reach the service:\n%s", plain(m))
	}
}

func TestPassphraseNeverReachesAScreenOrAMessage(t *testing.T) {
	fb := newFake(control.VaultLocked)
	m := appModel(t, fb, 80, 24)
	seen := func(step string) {
		for _, text := range []string{plain(m), m.status, m.fail, m.busy} {
			if strings.Contains(text, "horse") {
				t.Errorf("%s: the passphrase shows: %q", step, text)
			}
		}
	}
	send(m, "ctrl+l")
	typed(m, "wrong horse test")
	seen("typed")
	send(m, "enter")
	seen("wrong")
	typed(m, testPass)
	seen("typed again")
	send(m, "enter")
	seen("done")
	for _, size := range [][2]int{{40, 14}, {60, 20}, {120, 40}} {
		m = appModel(t, newFake(control.VaultLocked), size[0], size[1])
		send(m, "ctrl+l")
		typed(m, testPass)
		seen("small")
	}
}

func TestStatusIsAskedAfterAnActionAndNotMoreOftenThanPolled(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m := appModel(t, fb, 80, 24)
	now := clockModel(m, m.svc.at)
	m.svc.checked = *now
	before := fb.count("status")
	*now = now.Add(30 * time.Second)
	if _, cmd := m.Update(tickMsg{}); cmd == nil {
		t.Fatal("a tick returns nothing")
	} else {
		drain(m, cmd)
	}
	if fb.count("status") != before {
		t.Error("the service was asked within the poll interval")
	}
	*now = now.Add(31 * time.Second)
	_, cmd := m.Update(tickMsg{})
	drain(m, cmd)
	if fb.count("status") != before+1 {
		t.Errorf("status calls = %d, want %d", fb.count("status"), before+1)
	}
	var done []error
	drain(m, m.requireAdmin(gated(fb, &done)))
	typed(m, testPass)
	send(m, "enter")
	if fb.count("status") < before+2 {
		t.Error("no status after the action")
	}
}

func TestAdminCountdownRunsLocally(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	fb.session, fb.remaining = true, 600
	m := appModel(t, fb, 80, 24)
	now := clockModel(m, m.svc.at)
	asked := fb.count("status")
	for _, step := range []struct {
		after time.Duration
		want  string
	}{{0, "admin 10 min"}, {4 * time.Minute, "admin 6 min"}, {9*time.Minute + 30*time.Second, "admin 1 min"}, {11 * time.Minute, "admin off"}} {
		*now = m.svc.at.Add(step.after)
		if view := plain(m); !strings.Contains(view, step.want) {
			t.Errorf("after %v the header lacks %q:\n%s", step.after, step.want, view)
		}
	}
	if fb.count("status") != asked {
		t.Error("the countdown asked the service")
	}
	if m.adminActive() {
		t.Error("an expired session counts as active")
	}
}

func TestAreasAreChosenByDigitsAndMovement(t *testing.T) {
	m := appModel(t, newFake(control.VaultUnlocked), 80, 24)
	for key, want := range map[string]area{"2": areaProjects, "3": areaActivity, "4": areaAccess, "5": areaLibrary, "6": areaSettings, "1": areaOverview} {
		send(m, key)
		if m.area != want {
			t.Errorf("%s selects %v, want %v", key, m.area, want)
		}
	}
	send(m, "1", "right", "tab")
	if m.area != areaActivity {
		t.Errorf("right and tab reach %v", m.area)
	}
	send(m, "left")
	send(m, "shift+tab")
	send(m, "left")
	if m.area != areaSettings {
		t.Errorf("movement back wraps to %v", m.area)
	}
	if view := plain(m); !strings.Contains(view, "Nothing to show yet.") {
		t.Errorf("a placeholder area shows no hint:\n%s", view)
	}
	for a := area(0); a < areaCount; a++ {
		if b, ok := areaByName(strings.ToLower(a.key())); !ok || b != a {
			t.Errorf("areaByName(%q) = %v", a.key(), b)
		}
	}
}

func TestTabLineStaysOneRowAndShrinksInANarrowTerminal(t *testing.T) {
	for _, w := range []int{40, 60, 80} {
		m := appModel(t, newFake(control.VaultUnlocked), w, 24)
		line, _ := m.tabLine()
		if strings.Contains(line, "\n") || lipglossWidth(line) > w {
			t.Errorf("%d columns: tab line %q", w, line)
		}
	}
}
