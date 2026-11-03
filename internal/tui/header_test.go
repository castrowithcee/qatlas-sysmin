package tui

import (
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-kit/i18n"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

func lipglossWidth(s string) int { return lipgloss.Width(s) }

// headerStates puts m into each state the header can show and calls check with its name.
func headerStates(t *testing.T, check func(name string, m *Model)) {
	t.Helper()
	build := func(mutate func(*Model, *fakeBackend)) *Model {
		fb := newFake()
		m := appModel(t, fb, 60, 20)
		if mutate != nil {
			mutate(m, fb)
		}
		return m
	}
	unknown := build(nil)
	unknown.svc = svcState{}
	states := map[string]*Model{
		"unknown": unknown,
		"unreachable": build(func(m *Model, f *fakeBackend) {
			f.down = errDropped
			drain(m, m.statusCmd())
		}),
		"up": build(nil),
		"update": build(func(m *Model, _ *fakeBackend) {
			m.set.upd = updateState{phase: updDone, info: ReleaseInfo{Latest: "9.9.9"}}
		}),
	}
	for name, m := range states {
		check(name, m)
	}
}

func TestHeaderStaysOneRowWithTheHelpKeyAtEveryWidth(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		headerStates(t, func(name string, m *Model) {
			for _, w := range []int{40, 50, 60, 80, 120} {
				sizedModel(m, w, 20)
				row := headerOf(m)
				if lipgloss.Width(row) > w {
					t.Errorf("%s at %d: header is %d cells: %q", name, w, lipgloss.Width(row), row)
				}
				if !strings.HasPrefix(row, "qsysmin") || !strings.Contains(row, "?") {
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
		"unknown": "checking", "unreachable": "service unreachable", "up": "qsysmin", "update": "9.9.9 available",
	}
	headerStates(t, func(name string, m *Model) {
		useProfile(m, kittui.DetectColorProfile(os.Stdout, os.Environ()))
		if !m.ui.colorless() {
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
	m := appModel(t, newFake(), 80, 20)
	m.set.upd = updateState{phase: updDone, info: ReleaseInfo{Latest: "9.9.9"}}
	for w, wants := range map[int]string{80: "9.9.9 available", 40: "update"} {
		sizedModel(m, w, 20)
		row := headerOf(m)
		if !strings.Contains(row, wants) || !strings.Contains(row, "?") {
			t.Errorf("%d columns: %q", w, row)
		}
	}
}

func TestGermanHeader(t *testing.T) {
	m := appModel(t, newFake(), 80, 20)
	m.set.upd = updateState{phase: updDone, info: ReleaseInfo{Latest: "9.9.9"}}
	useLanguage(t, m, i18n.German)
	if row := headerOf(m); !strings.Contains(row, "9.9.9 verfügbar") || !strings.Contains(row, "Hilfe") {
		t.Errorf("header = %q", row)
	}
}
