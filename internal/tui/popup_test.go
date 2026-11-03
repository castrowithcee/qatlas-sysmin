package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

func TestHelpInfoAndCommandsOpenAndCloseWithEsc(t *testing.T) {
	for key, kind := range map[string]popKind{"?": popHelp, "i": popInfo, ":": popCommand} {
		m := appModel(t, overviewFake(), 80, 24)
		send(m, key)
		if m.pop == nil || m.pop.kind != kind || m.screen != screenPopup {
			t.Fatalf("%s opens no panel of kind %v", key, kind)
		}
		assertViewFits(t, plain(m), 80, 24)
		send(m, "esc")
		if m.pop != nil || m.screen != screenArea {
			t.Errorf("esc leaves the %s panel open", key)
		}
	}
}

func TestHelpTogglesWithItsOwnKeyAndNamesTheKeysOfTheScreen(t *testing.T) {
	m := appModel(t, overviewFake(), 120, 40)
	send(m, "?")
	view := plain(m)
	for _, want := range []string{"[keys]", "vault", "admin-session", "approvals", "unlock", "←/→ area", "Everywhere", "F2 save"} {
		if !strings.Contains(view, want) {
			t.Errorf("the help lacks %q:\n%s", want, view)
		}
	}
	send(m, "right")
	if view := plain(m); !strings.Contains(view, "The vault holds the secrets") {
		t.Errorf("the vault topic is not shown:\n%s", view)
	}
	send(m, "?")
	if m.pop != nil {
		t.Error("? does not close the help")
	}
}

func TestHelpTopicsAreTranslatedButKeepTheirIdentifiers(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		m := appModel(t, overviewFake(), 120, 40)
		send(m, "?")
		for _, topic := range helpTopics {
			view := plain(m)
			if !strings.Contains(view, "["+topic.id+"]") {
				t.Errorf("the strip does not show the open topic %q:\n%s", topic.id, view)
			}
			send(m, "right")
		}
	})
}

func TestInfoDescribesTheFocusedEntry(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	focusCard(t, m, "Approvals")
	send(m, "i")
	if view := plain(m); !strings.Contains(view, "Approvals") || !strings.Contains(view, "wait in Access") {
		t.Errorf("no info for the focused row:\n%s", view)
	}
	send(m, "esc", "6", "down")
	send(m, "i")
	if view := plain(m); !strings.Contains(view, "Theme") || !strings.Contains(view, "Colors of this interface") {
		t.Errorf("no info for the focused field:\n%s", view)
	}
}

func TestCommandLineChangesAreasAndPages(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	for text, check := range map[string]func() bool{
		"access":            func() bool { return m.area == areaAccess },
		"settings vault":    func() bool { return m.area == areaSettings && m.set.sub == subVault },
		"Settings   Update": func() bool { return m.area == areaSettings && m.set.sub == subUpdate },
		"settings":          func() bool { return m.area == areaSettings && m.set.sub == subGeneral },
		"overview":          func() bool { return m.area == areaOverview },
	} {
		send(m, ":")
		typed(m, text)
		send(m, "enter")
		if m.pop != nil || !check() {
			t.Errorf("%q: area %v page %d, panel open %v", text, m.area, m.set.sub, m.pop != nil)
		}
	}
}

func TestCommandLineRunsTheActionsOfTheKeys(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m := appModel(t, fb, 80, 24)
	send(m, ":")
	typed(m, "lock")
	send(m, "enter")
	if m.ov == nil {
		t.Error("lock asks for no passphrase")
	}
	send(m, "esc", ":")
	typed(m, "help")
	send(m, "enter")
	if m.pop == nil || m.pop.kind != popHelp {
		t.Error("help opens no help")
	}
	send(m, "esc", ":")
	typed(m, "quit")
	_, cmd := m.Update(keyMsg("enter"))
	if cmd == nil || !m.quitting {
		t.Error("quit does not end the program")
	}
}

func TestCommandLineSetupNeedsNoVault(t *testing.T) {
	m := appModel(t, newFake(control.VaultAbsent), 80, 24)
	send(m, ":")
	typed(m, "setup")
	send(m, "enter")
	if m.ov == nil || m.ov.kind != entrySetup {
		t.Error("setup opens no setup")
	}
	m = appModel(t, newFake(control.VaultUnlocked), 80, 24)
	send(m, ":")
	typed(m, "setup")
	send(m, "enter")
	if m.ov != nil || m.pop == nil || !strings.Contains(plain(m), "setup: A vault exists already.") {
		t.Errorf("setup with a vault: overlay %v, view:\n%s", m.ov != nil, plain(m))
	}
}

func TestPanelKeysDoNotActWhereTextIsTyped(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	send(m, ":")
	typed(m, "?i:q")
	if m.pop == nil || m.pop.kind != popCommand || m.pop.in.Value() != "?i:q" || m.quitting {
		t.Errorf("the command line did not take its text: %v", m.pop)
	}
	send(m, "esc", "ctrl+l")
	typed(m, "?i:")
	if m.ov == nil || m.pop != nil || m.screen != screenPrompt {
		t.Error("the passphrase overlay lets help, info or the command line open")
	}
	if strings.Contains(plain(m), "?i:") {
		t.Error("a typed passphrase is on screen")
	}
}

func TestFooterNamesTheKeysOfTheScreenAndKeepsHelpVisible(t *testing.T) {
	for _, size := range [][2]int{{40, 14}, {60, 20}, {80, 24}, {120, 40}} {
		for _, page := range []string{"1", "2", "6"} {
			m := appModel(t, overviewFake(), size[0], size[1])
			send(m, page)
			rows := strings.Split(plain(m), "\n")
			foot := rows[len(rows)-1]
			if !strings.Contains(foot, "? help") {
				t.Errorf("%dx%d area %s: footer %q hides the help key", size[0], size[1], page, foot)
			}
		}
	}
	m := appModel(t, overviewFake(), 120, 40)
	rows := strings.Split(plain(m), "\n")
	for _, want := range []string{"←/→ area", "↓ open", "i info", ": cmds", "q quit"} {
		if !strings.Contains(rows[len(rows)-1], want) {
			t.Errorf("the footer of the overview lacks %q: %q", want, rows[len(rows)-1])
		}
	}
}
