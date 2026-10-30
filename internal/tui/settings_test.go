package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// settingsModel is the interface on the page of Settings, with a preferences file in a temporary directory.
func settingsModel(t *testing.T, fb *fakeBackend, page string) (*Model, string) {
	t.Helper()
	m := appModel(t, fb, 80, 24)
	// The keys and the language are process-wide; put them back when the test ends.
	t.Cleanup(func() { applyKeys(uiprefs.DefaultKeys()) })
	m.prefsPath = filepath.Join(t.TempDir(), "tui.yaml")
	send(m, "6")
	switch page {
	case "vault":
		send(m, "esc", "right", "enter")
	case "update":
		send(m, "esc", "right", "right", "enter")
	}
	return m, m.prefsPath
}

// choose opens the list of the focused row and takes the value with the given number of steps down.
func choose(t *testing.T, m *Model, steps int) {
	t.Helper()
	send(m, "enter")
	if m.dd == nil {
		t.Fatal("enter opens no list")
	}
	for range steps {
		send(m, "down")
	}
	send(m, "enter")
}

func TestSettingsOpensOnGeneralWithTheThreePages(t *testing.T) {
	m, _ := settingsModel(t, newFake(control.VaultUnlocked), "general")
	view := plain(m)
	for _, want := range []string{"[General]", "Vault", "Update", "Language:", "Theme:", "Mouse:", "Save key:"} {
		if !strings.Contains(view, want) {
			t.Errorf("the General page lacks %q:\n%s", want, view)
		}
	}
	send(m, "esc", "right")
	if m.set.sub != subVault {
		t.Errorf("right leads to page %d, want Vault", m.set.sub)
	}
	send(m, "5", "6")
	if m.set.sub != subGeneral {
		t.Errorf("Settings reopens on page %d, want General", m.set.sub)
	}
}

func TestGeneralSavesToTheFileWithoutTheServiceAndActsAtOnce(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m, path := settingsModel(t, fb, "general")
	calls := len(fb.log())
	choose(t, m, 1) // language: deutsch
	send(m, "down", "down", "enter", "down", "enter")
	if m.prefs.Language != uiprefs.LanguageEnglish {
		t.Fatal("a choice acts before it is saved")
	}
	send(m, "f2")
	prefs, warnings := uiprefs.Load(path)
	if len(warnings) > 0 || prefs.Language != uiprefs.LanguageGerman || !prefs.MouseOff {
		t.Errorf("saved %+v, warnings %v; want German with the mouse off", prefs, warnings)
	}
	if m.prefs.Language != uiprefs.LanguageGerman || !m.prefs.MouseOff {
		t.Errorf("the saved choices do not act: %+v", m.prefs)
	}
	if view := plain(m); !strings.Contains(view, "Hilfe") || !strings.Contains(view, "Auf diesem Gerät gespeichert.") {
		t.Errorf("the language does not change at once:\n%s", view)
	}
	for _, c := range fb.log()[calls:] {
		if c != "status" && c != "settings" {
			t.Errorf("saving the preferences called the service: %v", fb.log()[calls:])
		}
	}
	if fb.session || m.ov != nil {
		t.Error("saving the preferences asked for an admin session")
	}
}

func TestGeneralPicksTheThemeFromAList(t *testing.T) {
	m, path := settingsModel(t, newFake(control.VaultUnlocked), "general")
	send(m, "down")
	choose(t, m, 1)
	if m.set.gen.theme != themes[1].name {
		t.Fatalf("theme = %q, want %q", m.set.gen.theme, themes[1].name)
	}
	send(m, "f2")
	if prefs, _ := uiprefs.Load(path); prefs.Theme != themes[1].name || m.prefs.Theme != themes[1].name {
		t.Errorf("saved theme %q, model %q", prefs.Theme, m.prefs.Theme)
	}
}

func TestGeneralRebindsAKeyAndRefusesAnInvalidOne(t *testing.T) {
	m, path := settingsModel(t, newFake(control.VaultUnlocked), "general")
	send(m, "down", "down", "down") // save key
	send(m, "enter")
	if !m.set.editing {
		t.Fatal("enter does not start editing the key")
	}
	m.set.gen.keys[0].SetValue("")
	typed(m, "f1")
	send(m, "f2")
	if _, err := uiprefs.Load(path); err != nil {
		t.Fatal(err)
	}
	if view := plain(m); !strings.Contains(view, "terminals take F1") {
		t.Errorf("no cause under the field:\n%s", view)
	}
	if keyOf(actSave) != "f2" {
		t.Errorf("an invalid key was applied: %s", keyOf(actSave))
	}
	if m.set.editing {
		t.Error("saving leaves the field in editing")
	}
	m.set.gen.keys[0].SetValue("f3")
	send(m, "f3") // the old key no longer saves; the new one is not applied yet
	send(m, "f2")
	if keyOf(actSave) != "f3" {
		t.Errorf("the save key = %q, want f3", keyOf(actSave))
	}
	prefs, _ := uiprefs.Load(path)
	if prefs.Keys.Save != "f3" {
		t.Errorf("saved key = %q", prefs.Keys.Save)
	}
}

func TestEditingATextFieldTakesQuestionMarksAndLetters(t *testing.T) {
	m, _ := settingsModel(t, newFake(control.VaultUnlocked), "general")
	send(m, "end") // the extra info key, the last row
	send(m, "enter")
	typed(m, "?q:i")
	if m.screen != screenArea || m.pop != nil || m.quitting {
		t.Fatal("a key meant for the text did something else")
	}
	if got := m.set.gen.keys[4].Value(); got != "?q:i" {
		t.Errorf("field = %q", got)
	}
	send(m, "esc")
	if m.set.editing {
		t.Error("esc keeps editing")
	}
}

func TestVaultTimeoutAsksOnceThenSetsOnce(t *testing.T) {
	for _, value := range []string{"30m", "0"} {
		fb := newFake(control.VaultUnlocked)
		m, _ := settingsModel(t, fb, "vault")
		if got := m.set.vault.timeout.Value(); got != "15m0s" {
			t.Fatalf("the field shows %q, want the timeout of the service", got)
		}
		send(m, "down", "enter")
		m.set.vault.timeout.SetValue("")
		typed(m, value)
		send(m, "f2")
		if m.ov == nil || fb.count("timeout "+value) != 0 {
			t.Fatalf("%s: no passphrase asked before the change (calls %v)", value, fb.log())
		}
		typed(m, testPass)
		send(m, "enter")
		if n := fb.count("timeout " + value); n != 1 {
			t.Errorf("%s: SetAdminTimeout ran %d times, want once (calls %v)", value, n, fb.log())
		}
		if m.ov != nil || m.set.vault.problem != "" || m.vaultDirty() {
			t.Errorf("%s: overlay %v, problem %q, dirty %v", value, m.ov != nil, m.set.vault.problem, m.vaultDirty())
		}
	}
}

func TestVaultTimeoutRunsAtOnceWithASession(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	fb.session = true
	m, _ := settingsModel(t, fb, "vault")
	send(m, "down", "enter")
	m.set.vault.timeout.SetValue("")
	typed(m, "5m")
	send(m, "f2")
	if m.ov != nil || fb.count("timeout 5m") != 1 {
		t.Errorf("overlay %v, calls %v", m.ov != nil, fb.log())
	}
	if !strings.Contains(plain(m), "Admin timeout saved.") {
		t.Errorf("no confirmation:\n%s", plain(m))
	}
}

func TestVaultTimeoutShowsAProblemUnderTheField(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	fb.session, fb.invalidTimeout = true, true
	m, _ := settingsModel(t, fb, "vault")
	send(m, "down", "enter")
	m.set.vault.timeout.SetValue("")
	typed(m, "-5m")
	send(m, "f2")
	lines := strings.Split(plain(m), "\n")
	for i, line := range lines {
		if strings.Contains(line, "Admin timeout:") {
			if i+1 >= len(lines) || !strings.Contains(lines[i+1], "must not be negative (use 0 or a positive duration)") {
				t.Errorf("the problem does not stand under the field:\n%s", plain(m))
			}
			return
		}
	}
	t.Errorf("no timeout row:\n%s", plain(m))
}

func TestVaultTimeoutRefusesWhatIsNoDuration(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m, _ := settingsModel(t, fb, "vault")
	send(m, "down", "enter")
	m.set.vault.timeout.SetValue("soon")
	send(m, "f2")
	if m.ov != nil || !strings.Contains(plain(m), "Use a duration such as 15m, or 0.") {
		t.Errorf("overlay %v:\n%s", m.ov != nil, plain(m))
	}
}

func TestVaultShowsTheUnlockModeAndHowToBindIt(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	fb.unlockMode = control.UnlockHost
	m, _ := settingsModel(t, fb, "vault")
	if view := plain(m); !strings.Contains(view, "automatic (host key)") || !strings.Contains(view, "sudo sysmin vault bind") {
		t.Errorf("unlock mode or hint missing:\n%s", view)
	}
}

func TestChangePassphraseKeepsTheOverlayOnAWrongCurrentOne(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m, _ := settingsModel(t, fb, "vault")
	send(m, "enter")
	if m.ov == nil || m.ov.kind != entryChange || len(m.ov.inputs) != 3 {
		t.Fatal("enter on the passphrase row opens no change overlay")
	}
	typed(m, "wrong current")
	send(m, "tab")
	typed(m, "brand new pass")
	send(m, "tab")
	typed(m, "brand new pass")
	if view := plain(m); strings.Contains(view, "wrong current") || strings.Contains(view, "brand new") {
		t.Errorf("a passphrase is on screen:\n%s", view)
	}
	send(m, "enter")
	if m.ov == nil || !strings.Contains(plain(m), "Wrong passphrase.") {
		t.Fatalf("the overlay closed or hides the error:\n%s", plain(m))
	}
	if fb.count("passphrase") != 0 || fb.pass != testPass {
		t.Error("the passphrase changed after a wrong current one")
	}
	for _, in := range m.ov.inputs {
		if in.Value() != "" {
			t.Error("a field keeps its passphrase after use")
		}
	}
}

func TestChangePassphraseChangesItOnce(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m, _ := settingsModel(t, fb, "vault")
	send(m, "enter")
	typed(m, testPass)
	send(m, "tab")
	typed(m, "brand new pass")
	send(m, "tab")
	typed(m, "brand new pass")
	send(m, "enter")
	if fb.count("passphrase") != 1 || fb.pass != "brand new pass" || m.ov != nil {
		t.Errorf("calls %v, passphrase %q, overlay %v", fb.log(), fb.pass, m.ov != nil)
	}
	if !strings.Contains(plain(m), "Passphrase changed.") {
		t.Errorf("no confirmation:\n%s", plain(m))
	}
}

func TestChangePassphraseRefusesEmptyAndDifferingEntries(t *testing.T) {
	fb := newFake(control.VaultUnlocked)
	m, _ := settingsModel(t, fb, "vault")
	send(m, "enter")
	typed(m, testPass)
	send(m, "tab")
	typed(m, "one")
	send(m, "tab")
	typed(m, "other")
	send(m, "enter")
	if m.fail != "The passphrases differ." || fb.count("session") != 0 {
		t.Errorf("fail %q, calls %v", m.fail, fb.log())
	}
	send(m, "enter", "enter", "enter")
	if m.fail != "The passphrase is empty." {
		t.Errorf("fail %q", m.fail)
	}
}

func releases(info ReleaseInfo, err error, calls *int) ReleaseCheck {
	return func(ctx context.Context) (ReleaseInfo, error) {
		*calls++
		return info, err
	}
}

func TestUpdatePageShowsTheLookupOfThisRun(t *testing.T) {
	for _, c := range []struct {
		name string
		info ReleaseInfo
		err  error
		want []string
	}{
		{"available", ReleaseInfo{Latest: "v0.0.1-test"}, nil, []string{"v0.0.1-test available", "Press U to install it."}},
		{"current", ReleaseInfo{}, nil, []string{"up to date"}},
		{"dev", ReleaseInfo{Dev: true}, nil, []string{"development build"}},
		{"failed", ReleaseInfo{}, errors.New("no network"), []string{"check failed"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var calls int
			m := appModel(t, newFake(control.VaultUnlocked), 80, 24)
			m.version, m.releases = "0.0.0-test", releases(c.info, c.err, &calls)
			send(m, "6", "esc", "right", "right", "enter")
			view := plain(m)
			if calls != 1 || !strings.Contains(view, "0.0.0-test") {
				t.Errorf("lookups %d:\n%s", calls, view)
			}
			for _, want := range c.want {
				if !strings.Contains(view, want) {
					t.Errorf("the page lacks %q:\n%s", want, view)
				}
			}
			if strings.Contains(view, "no network") {
				t.Errorf("the page repeats the cause of the failure:\n%s", view)
			}
			send(m, "esc", "left", "right", "enter")
			if calls != 1 {
				t.Errorf("reopening the page looked up again (%d)", calls)
			}
			send(m, "t")
			if calls != 2 {
				t.Errorf("t did not look up again (%d)", calls)
			}
		})
	}
}

func TestUpdatePageWithoutALookupShowsNoData(t *testing.T) {
	m, _ := settingsModel(t, newFake(control.VaultUnlocked), "update")
	if view := plain(m); !strings.Contains(view, "no data") {
		t.Errorf("no state:\n%s", view)
	}
	send(m, "t")
}

func TestLeavingUnsavedInputAsksFirst(t *testing.T) {
	m, path := settingsModel(t, newFake(control.VaultUnlocked), "general")
	choose(t, m, 1)
	send(m, "1")
	if m.screen != screenConfirm || m.area != areaSettings {
		t.Fatalf("screen %v area %v: no question before leaving unsaved input", m.screen, m.area)
	}
	if view := plain(m); !strings.Contains(view, "Unsaved changes") || !strings.Contains(view, "d discard") {
		t.Errorf("the question lacks its answers:\n%s", view)
	}
	send(m, "esc")
	if m.screen != screenArea || m.area != areaSettings || m.set.gen.language != uiprefs.LanguageGerman {
		t.Error("esc does not keep editing")
	}
	send(m, "1", "d")
	if m.area != areaOverview {
		t.Error("d does not discard and leave")
	}
	send(m, "6")
	if m.set.gen.language != uiprefs.LanguageEnglish {
		t.Error("the discarded choice came back")
	}
	choose(t, m, 1)
	send(m, "q", "f2")
	if prefs, _ := uiprefs.Load(path); prefs.Language != uiprefs.LanguageGerman || m.quitting {
		t.Errorf("save in the question: saved %q, quitting %v", prefs.Language, m.quitting)
	}
	if m.screen != screenArea || m.area != areaSettings {
		t.Error("saving in the question leaves the page")
	}
}

func TestSettingsPagesFitEverySizeInBothLanguages(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		for _, size := range [][2]int{{40, 14}, {60, 16}, {80, 24}, {120, 40}} {
			for _, page := range []string{"general", "vault", "update"} {
				m, _ := settingsModel(t, overviewFake(), page)
				sizedModel(m, size[0], size[1])
				assertViewFits(t, plain(m), size[0], size[1])
			}
			m := appModel(t, overviewFake(), size[0], size[1])
			assertViewFits(t, plain(m), size[0], size[1])
		}
	})
}
