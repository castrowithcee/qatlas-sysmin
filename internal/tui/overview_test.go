package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/i18n"
)

// ovNow is the fixed clock of the overview tests: Wednesday, 28 October 2026, in UTC.
var ovNow = time.Date(2026, 10, 28, 12, 30, 0, 0, time.UTC)

// overviewFake is a service with open approvals: the usual case of the overview.
func overviewFake() *fakeBackend {
	fb := newFake(control.VaultUnlocked)
	fb.approvals = []control.ApprovalSummary{{Project: "demo-app", Kind: "new"}, {Project: "sample-app", Kind: "changed"}}
	return fb
}

// calmFake is a service with nothing to do.
func calmFake() *fakeBackend { return newFake(control.VaultUnlocked) }

// overviewModel is appModel with the fixed clock, in UTC.
func overviewModel(t *testing.T, fb *fakeBackend, w, h int) *Model {
	t.Helper()
	utcClock(t)
	m := appModel(t, fb, w, h)
	m.now = func() time.Time { return ovNow }
	return m
}

// focusCard moves the focus of the overview to the card with the label.
func focusCard(t *testing.T, m *Model, label string) {
	t.Helper()
	send(m, "1")
	for i, c := range m.overviewCards() {
		if c.label == label {
			m.ovr.zone, m.ovr.cursor[ovCards] = ovCards, i
			return
		}
	}
	t.Fatalf("the overview has no %q card", label)
}

// focusStep moves the focus of the overview to the step whose text contains label.
func focusStep(t *testing.T, m *Model, label string) {
	t.Helper()
	send(m, "1")
	for i, s := range m.overviewSteps() {
		if strings.Contains(s.label, label) {
			m.ovr.zone, m.ovr.cursor[ovSteps] = ovSteps, i
			return
		}
	}
	t.Fatalf("the overview has no %q step: %v", label, m.overviewSteps())
}

func lastRow(m *Model) string {
	rows := strings.Split(plain(m), "\n")
	return rows[len(rows)-1]
}

func TestOverviewShowsHeaderCardsAndSteps(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		m := overviewModel(t, overviewFake(), size[0], size[1])
		view := plain(m)
		for _, want := range []string{"Overview", "Wed, 28 Oct 2026", "Approvals", "sysmin 0.0.0-test", "/etc/sysmin-test",
			"Next steps", "Review approvals"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: the overview lacks %q:\n%s", size[0], size[1], want, view)
			}
		}
		assertViewFits(t, view, size[0], size[1])
	}
}

func TestOverviewCardsCountApprovals(t *testing.T) {
	m := overviewModel(t, overviewFake(), 80, 24)
	got := map[string]string{}
	for _, c := range m.overviewCards() {
		got[c.label] = c.value
	}
	want := map[string]string{"Approvals": "2"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("card %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestOverviewCardsWithoutDataShowLockedOrNone(t *testing.T) {
	for vault, word := range map[string]string{control.VaultLocked: "locked", control.VaultAbsent: "none"} {
		m := overviewModel(t, newFake(vault), 80, 24)
		for _, c := range m.overviewCards() {
			if c.value != word || c.kind != statusDim {
				t.Errorf("%s: card %s = %q (kind %v), want a dimmed %q", vault, c.label, c.value, c.kind, word)
			}
		}
	}
}

func TestOverviewHasNoPermanentHintText(t *testing.T) {
	auto := overviewFake()
	auto.unlockMode = control.UnlockHost
	foreign := newFake(control.VaultUnlocked)
	foreign.down = &client.PeerError{PID: 4321, UID: 1234}
	fbs := map[string]*fakeBackend{"usual": overviewFake(), "calm": calmFake(), "locked": newFake(control.VaultLocked),
		"novault": newFake(control.VaultAbsent), "auto": auto, "foreign": foreign}
	for name, fb := range fbs {
		for _, size := range [][2]int{{80, 24}, {120, 40}, {60, 20}} {
			m := overviewModel(t, fb, size[0], size[1])
			for _, key := range []string{"", "tab", "down", "tab", "down"} {
				if key != "" {
					send(m, key)
				}
				rows := strings.Split(plain(m), "\n")
				body := strings.Join(rows[3:len(rows)-1], "\n")
				if strings.Contains(body, "Press ") || strings.Contains(body, "enter to") {
					t.Errorf("%s %dx%d: a hint stands in the overview:\n%s", name, size[0], size[1], body)
				}
			}
		}
	}
}

func TestOverviewStepForEveryStateAndEnterTakesIt(t *testing.T) {
	type state struct {
		name  string
		build func() *fakeBackend
		step  string
		check func(t *testing.T, m *Model, fb *fakeBackend)
	}
	area := func(want area) func(*testing.T, *Model, *fakeBackend) {
		return func(t *testing.T, m *Model, _ *fakeBackend) {
			t.Helper()
			if m.area != want {
				t.Errorf("enter leads to area %v, want %v", m.area, want)
			}
		}
	}
	retried := func(t *testing.T, m *Model, _ *fakeBackend) {
		t.Helper()
		if !m.serviceUp() {
			t.Error("enter does not ask the service again")
		}
	}
	states := []state{
		{"unreachable", func() *fakeBackend {
			fb := newFake(control.VaultUnlocked)
			fb.down = errDropped
			return fb
		}, "Retry", retried},
		{"foreign peer", func() *fakeBackend {
			fb := newFake(control.VaultUnlocked)
			fb.down = &client.PeerError{PID: 4321, UID: 1234}
			return fb
		}, "Retry · Another process answers at the socket (pid 4321", nil},
		{"no group", func() *fakeBackend {
			fb := newFake(control.VaultUnlocked)
			fb.down = fmt.Errorf("cannot reach the service: %w", client.ErrSessionGroup)
			return fb
		}, "Restart with group", func(t *testing.T, m *Model, _ *fakeBackend) {
			if !m.regroupAsked || !m.quitting {
				t.Errorf("enter does not quit to restart: regroup %v, quitting %v", m.regroupAsked, m.quitting)
			}
		}},
		{"no vault", func() *fakeBackend { return newFake(control.VaultAbsent) }, "Set up vault",
			func(t *testing.T, m *Model, _ *fakeBackend) {
				if m.ov == nil || m.ov.kind != entrySetup {
					t.Error("enter does not open the setup")
				}
			}},
		{"locked vault", func() *fakeBackend { return newFake(control.VaultLocked) }, "Unlock vault",
			func(t *testing.T, m *Model, _ *fakeBackend) {
				if m.ov == nil || !m.ov.locked {
					t.Error("enter does not ask for the passphrase")
				}
			}},
		{"open approvals", overviewFake, "Review approvals", area(areaAccess)},
		{"auto unlock", func() *fakeBackend {
			fb := calmFake()
			fb.unlockMode = control.UnlockTPM2
			return fb
		}, "Review auto-unlock", func(t *testing.T, m *Model, _ *fakeBackend) {
			if m.area != areaSettings || m.set.sub != subVault {
				t.Errorf("enter leads to area %v page %d, want Settings, Vault", m.area, m.set.sub)
			}
		}},
		{"nothing to do", calmFake, "Nothing to do", func(t *testing.T, m *Model, _ *fakeBackend) {
			if m.area != areaOverview {
				t.Errorf("enter on a step without action leaves the overview for %v", m.area)
			}
		}},
	}
	for _, s := range states {
		t.Run(s.name, func(t *testing.T) {
			fb := s.build()
			m := overviewModel(t, fb, 120, 40)
			if !strings.Contains(plain(m), s.step) {
				t.Fatalf("no %q step:\n%s", s.step, plain(m))
			}
			focusStep(t, m, s.step)
			if s.name == "unreachable" || s.name == "foreign peer" {
				fb.down = nil
				send(m, "enter")
				retried(t, m, fb)
				return
			}
			send(m, "enter")
			s.check(t, m, fb)
		})
	}
}

func TestOverviewSelfUpdateIsAStep(t *testing.T) {
	m := overviewModel(t, calmFake(), 120, 40)
	m.set.upd.phase, m.set.upd.info.Latest = updDone, "9.9.9"
	if view := plain(m); !strings.Contains(view, "Install sysmin 9.9.9") || !strings.Contains(view, "sysmin 0.0.0-test → 9.9.9") {
		t.Fatalf("no update step or version line:\n%s", view)
	}
	focusStep(t, m, "Install sysmin 9.9.9")
	send(m, "enter")
	if m.ask == nil {
		t.Error("enter does not ask before installing")
	}
}

func TestOverviewNoAdminSessionIsNoStep(t *testing.T) {
	m := overviewModel(t, calmFake(), 80, 24)
	if steps := m.overviewSteps(); len(steps) != 1 || steps[0].label != "Nothing to do" || steps[0].act != nil {
		t.Errorf("steps %v, want the dimmed Nothing to do alone", steps)
	}
}

func TestOverviewEnterOnCardsOpensTheirPages(t *testing.T) {
	m := overviewModel(t, overviewFake(), 120, 40)
	focusCard(t, m, "Approvals")
	send(m, "enter")
	if m.area != areaAccess {
		t.Errorf("enter on Approvals leads to %v, want %v", m.area, areaAccess)
	}
}
func TestOverviewTabVisitsTheZonesAndLeavesToTheTabLine(t *testing.T) {
	m := overviewModel(t, overviewFake(), 120, 40)
	send(m, "1")
	if m.ovr.zone != ovSteps {
		t.Fatalf("the Overview starts in zone %v, want the steps", m.ovr.zone)
	}
	send(m, "tab")
	if m.zoneNow() != zoneTabs {
		t.Errorf("tab after the steps reaches %v, want the tab line", m.zoneNow())
	}
	send(m, "tab")
	if m.zoneNow() != zoneContent || m.ovr.zone != ovSteps {
		t.Errorf("tab on the tab line reaches %v in zone %v", m.zoneNow(), m.ovr.zone)
	}
	send(m, "shift+tab")
	if m.ovr.zone != ovCards || m.zoneNow() != zoneContent {
		t.Errorf("shift+tab reaches zone %v on %v, want the cards", m.ovr.zone, m.zoneNow())
	}
	send(m, "right", "left", "left")
	if m.ovr.cursor[ovCards] != 0 {
		t.Errorf("the cursor runs past the cards: %d", m.ovr.cursor[ovCards])
	}
	send(m, "shift+tab")
	if m.zoneNow() != zoneTabs {
		t.Errorf("shift+tab on the cards reaches %v, want the tab line", m.zoneNow())
	}
}
func TestOverviewFooterNamesTheKeyOfTheFocusedEntry(t *testing.T) {
	m := overviewModel(t, overviewFake(), 120, 40)
	focusCard(t, m, "Approvals")
	if foot := lastRow(m); !strings.Contains(foot, "enter open") || !strings.Contains(foot, "↑/↓ move") {
		t.Errorf("footer on a card: %q", foot)
	}
	m = overviewModel(t, calmFake(), 120, 40)
	send(m, "1")
	if foot := lastRow(m); strings.Contains(foot, "enter") {
		t.Errorf("footer on a step without action names enter: %q", foot)
	}
	fb := newFake(control.VaultUnlocked)
	fb.down = errDropped
	m = overviewModel(t, fb, 120, 40)
	send(m, "1")
	if foot := lastRow(m); !strings.Contains(foot, "enter retry") {
		t.Errorf("footer on the retry step: %q", foot)
	}
}

func TestOverviewInfoDescribesTheFocusedEntry(t *testing.T) {
	m := overviewModel(t, overviewFake(), 120, 40)
	focusCard(t, m, "Approvals")
	send(m, "i")
	if view := plain(m); !strings.Contains(view, "wait in Access") {
		t.Errorf("no info for the Approvals card:\n%s", view)
	}
}

func TestOverviewDropsPartsInOrderAndKeepsTitleAndSteps(t *testing.T) {
	steps := []ovItem{{label: "Review approvals"}, {label: "Install sysmin 9.9.9"}}
	rank := func(p ovPlan) int {
		switch {
		case !p.framed:
			return 3
		case !p.version:
			return 2
		case p.line:
			return 1
		}
		return 0
	}
	last, seen := 0, map[int]bool{}
	for room := 40; room >= 3; room-- {
		p := overviewPlan(room, 80, steps)
		if p.steps < 1 {
			t.Fatalf("room %d: no step shown", room)
		}
		if r := rank(p); r < last {
			t.Errorf("room %d: %+v restores something dropped before", room, p)
		} else {
			last = r
		}
		seen[rank(p)] = true
	}
	for r := 0; r <= 3; r++ {
		if !seen[r] {
			t.Errorf("no room drops exactly down to level %d", r)
		}
	}
}

func TestOverviewInASmallTerminalKeepsTitleAndSteps(t *testing.T) {
	for _, h := range []int{14, 15, 16, 18} {
		m := overviewModel(t, overviewFake(), 80, h)
		view := plain(m)
		for _, want := range []string{"Overview", "Wed, 28 Oct 2026", "Review approvals"} {
			if !strings.Contains(view, want) {
				t.Errorf("height %d lacks %q:\n%s", h, want, view)
			}
		}
		assertViewFits(t, view, 80, h)
	}
}

func TestOverviewHeaderDateFollowsTheLanguage(t *testing.T) {
	m := overviewModel(t, overviewFake(), 80, 24)
	useLanguage(t, i18n.German)
	if view := plain(m); !strings.Contains(view, "Mi., 28. Okt. 2026") || !strings.Contains(view, "Nächste Schritte") {
		t.Errorf("the German overview:\n%s", view)
	}
}

func TestOverviewDistinguishesStatesWithoutColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	view := plain(overviewModel(t, overviewFake(), 80, 24))
	for _, mark := range []string{warnMark} {
		if !strings.Contains(view, mark) {
			t.Errorf("the mark %q is missing:\n%s", mark, view)
		}
	}
	if !strings.Contains(plain(overviewModel(t, calmFake(), 80, 24)), "○") {
		t.Error("the mark of a step without action is missing")
	}
}

func TestOverviewIsClickable(t *testing.T) {
	t.Run("card", func(t *testing.T) {
		m := overviewModel(t, overviewFake(), 120, 40)
		m.render()
		clickOn(t, m, "Approvals")
		if m.area != areaAccess {
			t.Errorf("a click on the Approvals card leads to %v, want Access", m.area)
		}
	})
	t.Run("step", func(t *testing.T) {
		m := overviewModel(t, overviewFake(), 120, 40)
		m.render()
		clickOn(t, m, "Review approvals")
		if m.area != areaAccess {
			t.Errorf("a click on the approvals step leads to %v, want Access", m.area)
		}
	})
}

func TestOverviewFocusMarkShowsWithoutColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := overviewModel(t, overviewFake(), 120, 40)
	send(m, "1")
	view := plain(m)
	if !slices.ContainsFunc(strings.Split(view, "\n"), func(l string) bool { return strings.Contains(l, "> "+warnMark+" Review approvals") }) {
		t.Errorf("the focused step has no mark:\n%s", view)
	}
	focusCard(t, m, "Approvals")
	if view := plain(m); !strings.Contains(view, "> Approvals") || !strings.Contains(view, "╔") {
		t.Errorf("the focused card has no mark:\n%s", view)
	}
}
