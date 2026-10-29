package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// goldenApp is the service interface of the given size as it is drawn with NO_COLOR set, so the files hold the
// marks and attributes that carry the meaning and no colour.
func goldenApp(t *testing.T, fb *fakeBackend, w, h int) *Model {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	m := appModel(t, fb, w, h)
	if currentProfile() != profileNone {
		t.Fatal("the golden frames need a terminal without colour")
	}
	return m
}

func TestGoldenAreas(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 40}}
	screens := []struct {
		name  string
		build func(t *testing.T, w, h int) *Model
	}{
		{"overview", func(t *testing.T, w, h int) *Model { return goldenApp(t, overviewFake(), w, h) }},
		{"overview_autounlock", func(t *testing.T, w, h int) *Model {
			fb := overviewFake()
			fb.unlockMode = control.UnlockTPM2
			return goldenApp(t, fb, w, h)
		}},
		{"overview_novault", func(t *testing.T, w, h int) *Model { return goldenApp(t, newFake(control.VaultAbsent), w, h) }},
		{"settings_general", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "6")
			return m
		}},
		{"settings_vault", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "6", "right")
			return m
		}},
		{"help", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "?")
			return m
		}},
		{"access_list", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, accessFake(), w, h)
			send(m, "4")
			return m
		}},
		{"access_detail", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, accessFake(), w, h)
			send(m, "4", "down", "enter", "right")
			if w >= wideDetailMin {
				send(m, "esc")
			}
			return m
		}},
		{"credentials_list", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, accessFake(), w, h)
			send(m, "5")
			return m
		}},
		{"credential_form", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, accessFake(), w, h)
			typeNewSecret(m)
			return m
		}},
		{"passphrase_change", func(t *testing.T, w, h int) *Model {
			m := goldenApp(t, overviewFake(), w, h)
			send(m, "6", "right", "enter")
			typed(m, "old pass")
			send(m, "tab")
			typed(m, "new pass")
			return m
		}},
	}
	for _, s := range screens {
		for _, size := range sizes {
			t.Run(fmt.Sprintf("%s_%dx%d", s.name, size[0], size[1]), func(t *testing.T) {
				m := s.build(t, size[0], size[1])
				got := m.render()
				if strings.Contains(got, "old pass") || strings.Contains(got, "new pass") || strings.Contains(got, secretValue) {
					t.Error("a typed passphrase is in the frame")
				}
				assertViewFits(t, plain(m), size[0], size[1])
				assertGolden(t, fmt.Sprintf("%s_%dx%d", s.name, size[0], size[1]), got)
			})
		}
	}
}
