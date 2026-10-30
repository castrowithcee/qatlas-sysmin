package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// updateModel is the interface right after its start, with release v0.1.1 newer than the running v0.1.0.
func updateModel(t *testing.T, info ReleaseInfo) (*Model, *int) {
	t.Helper()
	var calls int
	m := appModel(t, newFake(control.VaultUnlocked), 120, 24)
	m.version, m.releases = "v0.1.0", releases(info, nil, &calls)
	drain(m, m.Init())
	return m, &calls
}

func TestStartFindsANewerReleaseAndShowsIt(t *testing.T) {
	m, calls := updateModel(t, ReleaseInfo{Latest: "v0.1.1"})
	view := plain(m)
	if *calls != 1 || !strings.Contains(view, "v0.1.0 → v0.1.1") || !strings.Contains(view, "v0.1.1 available") || !strings.Contains(view, "Press enter or U to install it.") {
		t.Fatalf("lookups %d:\n%s", *calls, view)
	}

	m, _ = updateModel(t, ReleaseInfo{})
	if view := plain(m); strings.Contains(view, "available") {
		t.Errorf("no newer release, yet:\n%s", view)
	}
}

func TestSelfUpdateAsksThenQuitsToInstall(t *testing.T) {
	shiftU := tea.KeyPressMsg{Code: 'u', ShiftedCode: 'U', Text: "U", Mod: tea.ModShift}
	for name, start := range map[string]func(m *Model){
		"U":       func(m *Model) { _, cmd := m.Update(shiftU); drain(m, cmd) },
		"enter":   func(m *Model) { send(m, "1", "down", "enter") },
		"command": func(m *Model) { send(m, ":"); typed(m, "update"); send(m, "enter") },
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := updateModel(t, ReleaseInfo{Latest: "v0.1.1"})
			start(m)
			if m.screen != screenConfirm || !strings.Contains(plain(m), "Update sysmin from v0.1.0 to v0.1.1?") {
				t.Fatalf("no question:\n%s", plain(m))
			}
			send(m, "n")
			if m.quitting || m.updateTo != "" {
				t.Fatal("n went on")
			}
			start(m)
			send(m, "y")
			if !m.quitting || m.updateTo != "v0.1.1" {
				t.Errorf("y: quitting %v, update to %q", m.quitting, m.updateTo)
			}
		})
	}
}

func TestSelfUpdateWithoutAKnownReleaseAsksAgain(t *testing.T) {
	m, calls := updateModel(t, ReleaseInfo{})
	send(m, ":")
	typed(m, "update")
	send(m, "enter")
	if m.screen == screenConfirm || m.quitting || *calls != 2 {
		t.Errorf("screen %v, quitting %v, lookups %d", m.screen, m.quitting, *calls)
	}
}
