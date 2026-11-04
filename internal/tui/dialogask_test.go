package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

func TestEscDropsEveryQuestionAndRunsNothing(t *testing.T) {
	shiftU := tea.KeyPressMsg{Code: 'U', ShiftedCode: 'U', Text: "U", Mod: tea.ModShift}
	for name, open := range map[string]func(t *testing.T) (*Model, *fakeSudo){
		"change": func(t *testing.T) (*Model, *fakeSudo) {
			m, f, _ := hostChangeModel(t, 120, 40)
			drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
			return m, f
		},
		"irreversible change": func(t *testing.T) (*Model, *fakeSudo) {
			m, f, _ := hostChangeModel(t, 120, 40)
			drain(m, m.askHostChange("demo.wipe", nil))
			return m, f
		},
		"read as root": func(t *testing.T) (*Model, *fakeSudo) {
			m, f, _ := hostChangeModel(t, 120, 40)
			reload(m)
			return m, f
		},
		"self-update": func(t *testing.T) (*Model, *fakeSudo) {
			m, _ := updateModel(t, ReleaseInfo{Latest: "v0.1.1"})
			_, cmd := m.Update(shiftU)
			drain(m, cmd)
			return m, &fakeSudo{}
		},
	} {
		for _, size := range [][2]int{{120, 40}, {60, 20}} {
			t.Run(name, func(t *testing.T) {
				m, f := open(t)
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				if m.screen != screenConfirm || m.dlg == nil {
					t.Fatalf("no question:\n%s", plain(m))
				}
				send(m, "esc")
				if m.screen == screenConfirm || m.dlg != nil || m.quitting || m.updateTo != "" || len(f.reqs) != 0 || m.status != "Cancelled." {
					t.Errorf("esc did not drop the question: screen %v quitting %v update %q calls %v status %q",
						m.screen, m.quitting, m.updateTo, f.reqs, m.status)
				}
			})
		}
	}
}

func TestEnterNeverAnswersAQuestion(t *testing.T) {
	m, f, _ := hostChangeModel(t, 120, 40)
	drain(m, m.askHostChange("demo.wipe", nil))
	spec := m.dlgSpec
	if spec.Danger != actYes || spec.Default != "" || !spec.Irreversible {
		t.Fatalf("an irreversible change needs a danger that is no default: %+v", spec)
	}
	send(m, "enter")
	if m.screen != screenConfirm || len(f.reqs) != 0 {
		t.Errorf("enter answered the question: screen %v calls %v", m.screen, f.reqs)
	}
}

func TestADialogWithTheDangerAsDefaultIsRefused(t *testing.T) {
	m, _, _ := hostChangeModel(t, 120, 40)
	spec := planSpec(m.ui, "Wipe the demo folder.", nil, "the old content is not kept", tHintRunChange)
	spec.Default = actYes
	if m.openDialog(spec) || m.dlg != nil || m.fail == "" {
		t.Errorf("the kit must refuse a danger that enter answers: fail %q", m.fail)
	}
}

func TestAnIrreversibleChangeIsAskedInTheWarnColourWithADangerButton(t *testing.T) {
	m, _, _ := hostChangeModel(t, 120, 40)
	drain(m, m.askHostChange("demo.wipe", nil))
	if s := m.dlgSpec; s.Notice || s.Danger != actYes || !s.Irreversible || len(s.Details) < 2 || s.Details[0] != (kittui.DialogLine{}) ||
		s.Details[1].Prefix != "$ " || s.Details[1].Style != kittui.StyleHint {
		t.Errorf("the commands are dimmed behind a prompt sign after an empty line: %+v", s)
	}
}

func TestASuccessIsACalmMessageAndAFailureHasNoDangerButton(t *testing.T) {
	m, f, _ := hostChangeModel(t, 120, 40)
	f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
	drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
	send(m, "y")
	s := m.dlgSpec
	if !s.Notice || s.Danger != "" || s.Irreversible || strings.Contains(plain(m), m.ui.styles.Glyph(kittui.GlyphWarning)) {
		t.Errorf("a success is a calm message: %+v\n%s", s, plain(m))
	}
	send(m, "enter")
	if m.screen == screenConfirm {
		t.Error("enter does not close a message")
	}
}
