package tui

import (
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/i18n"
)

func lipglossWidth(s string) int { return lipgloss.Width(s) }

// headerStates puts m into each state the header can show and calls check with its name.
func headerStates(t *testing.T, check func(name string, m *Model)) {
	t.Helper()
	build := func(vault string, mutate func(*fakeBackend)) *Model {
		fb := newFake(vault)
		if mutate != nil {
			mutate(fb)
		}
		return appModel(t, fb, 60, 20)
	}
	unknown := appModel(t, newFake(control.VaultUnlocked), 60, 20)
	unknown.svc = svcState{}
	states := map[string]*Model{
		"unknown":     unknown,
		"unreachable": build(control.VaultUnlocked, func(f *fakeBackend) { f.down = errDropped }),
		"absent":      build(control.VaultAbsent, nil),
		"locked":      build(control.VaultLocked, nil),
		"open":        build(control.VaultUnlocked, nil),
		"admin":       build(control.VaultUnlocked, func(f *fakeBackend) { f.session, f.remaining = true, 3540 }),
		"once":        build(control.VaultUnlocked, func(f *fakeBackend) { f.session, f.remaining = true, 0 }),
	}
	for name, m := range states {
		check(name, m)
		overlay := appModel(t, newFake(control.VaultLocked), 60, 20)
		send(overlay, "ctrl+l")
		check("overlay", overlay)
	}
}

func TestHeaderStaysOneRowWithTheHelpKeyAtEveryWidth(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		headerStates(t, func(name string, m *Model) {
			for _, w := range []int{40, 50, 60, 80, 120} {
				sizedModel(m, w, 20)
				lines := m.headerLines()
				if len(lines) != 1 {
					t.Fatalf("%s at %d: %d header rows", name, w, len(lines))
				}
				row := ansi.Strip(lines[0])
				if lipgloss.Width(row) > w {
					t.Errorf("%s at %d: header is %d cells: %q", name, w, lipgloss.Width(row), row)
				}
				if !strings.HasPrefix(row, "sysmin") || !strings.Contains(row, "?") {
					t.Errorf("%s at %d: name or help key missing: %q", name, w, row)
				}
				view := plain(m)
				assertViewFits(t, view, w, 20)
			}
		})
	})
}

func TestHeaderNamesEveryStateInWordsWithoutColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	want := map[string]string{
		"unknown": "checking", "unreachable": "service unreachable", "absent": "no vault", "locked": "vault locked",
		"open": "vault open", "admin": "admin 59 min", "once": "admin once",
	}
	headerStates(t, func(name string, m *Model) {
		if name == "overlay" {
			return
		}
		setProfile(detectProfile(os.Stdout))
		if currentProfile() != profileNone {
			t.Fatal("NO_COLOR keeps the colours")
		}
		row := m.render()
		if colourSGR.MatchString(row) {
			t.Errorf("%s: colour sequence under NO_COLOR", name)
		}
		if first := ansi.Strip(strings.SplitN(row, "\n", 2)[0]); !strings.Contains(first, want[name]) {
			t.Errorf("%s: header %q lacks %q", name, first, want[name])
		}
	})
}

func TestHeaderShortensBeforeItDropsTheKeyHints(t *testing.T) {
	m := appModel(t, newFake(control.VaultUnlocked), 80, 20)
	m.svc.st.Admin, m.svc.st.AdminRemaining = true, 900
	for w, wants := range map[int]string{80: "vault open", 50: "open", 40: "open"} {
		sizedModel(m, w, 20)
		row := ansi.Strip(m.headerLines()[0])
		if !strings.Contains(row, wants) || !strings.Contains(row, "?") {
			t.Errorf("%d columns: %q", w, row)
		}
	}
}

func TestHeaderBackButtonIsClickable(t *testing.T) {
	m := appModel(t, newFake(control.VaultLocked), 80, 16)
	send(m, "ctrl+l")
	m.render()
	if len(m.frameHits) == 0 {
		t.Fatal("no back button over the overlay")
	}
}

func TestGermanHeader(t *testing.T) {
	m := appModel(t, newFake(control.VaultUnlocked), 80, 20)
	useLanguage(t, i18n.German)
	if row := ansi.Strip(m.headerLines()[0]); !strings.Contains(row, "Tresor offen") || !strings.Contains(row, "Admin aus") || !strings.Contains(row, "Hilfe") {
		t.Errorf("header = %q", row)
	}
}
