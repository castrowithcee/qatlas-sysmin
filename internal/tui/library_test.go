package tui

import (
	"fmt"
	"strings"
	"testing"
)

// newSecret types a new credential of the kind "secret" named ci-token, up to the last field.
func typeNewSecret(m *Model) {
	send(m, "5", "n")
	typed(m, "ci-token")
	send(m, "tab", "right", "right", "tab")
	typed(m, secretValue)
}

func TestCredentialsListShowsUsesWithTheirState(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		m := appModel(t, accessFake(), size[0], size[1])
		send(m, "5")
		view := plain(m)
		for _, want := range []string{"Credentials (3)", "registry-main", "smtp", "demo-app (current, approved)", "unused"} {
			if size[0] == 80 && want == "demo-app (current, approved)" {
				continue
			}
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: the list lacks %q:\n%s", size[0], size[1], want, view)
			}
		}
		assertViewFits(t, view, size[0], size[1])
	}
}

func TestLibraryPagesSwitchAndCommandsReachThem(t *testing.T) {
	m := appModel(t, accessFake(), 80, 24)
	send(m, "5", "esc", "right")
	if m.lib.sub != libChannels || !strings.Contains(plain(m), "Channels") {
		t.Errorf("sub = %d", m.lib.sub)
	}
	send(m, "right", "right")
	if m.lib.sub != libCredentials {
		t.Errorf("sub = %d, want the pages to wrap", m.lib.sub)
	}
	send(m, ":")
	typed(m, "library templates")
	send(m, "enter")
	if m.area != areaLibrary || m.lib.sub != libTemplates {
		t.Errorf("area %v sub %d", m.area, m.lib.sub)
	}
}

func TestCredentialDetailShowsNoValue(t *testing.T) {
	m := appModel(t, accessFake(), 80, 24)
	send(m, "5", "enter")
	for range 3 {
		view := plain(m)
		if !strings.Contains(view, "registry-main") {
			t.Errorf("the detail lacks the entry:\n%s", view)
		}
		send(m, "right")
	}
	send(m, "esc", "v")
	if !strings.Contains(m.status, "never shown") {
		t.Errorf("status = %q", m.status)
	}
}

func TestSetCredentialCallsOnceWithTheTypedFields(t *testing.T) {
	fb := accessFake()
	m := appModel(t, fb, 80, 24)
	typeNewSecret(m)
	if view := m.render(); strings.Contains(view, secretValue) {
		t.Fatalf("the typed value is drawn:\n%s", view)
	}
	send(m, "enter")
	if m.ov == nil || fb.count("credential-set ci-token") != 0 {
		t.Fatal("the passphrase was not asked before the call")
	}
	if m.cred != nil {
		t.Error("the form stays open behind the passphrase")
	}
	typed(m, testPass)
	send(m, "enter")
	if fb.count("credential-set ci-token") != 1 || len(fb.set) != 1 {
		t.Fatalf("calls = %v", fb.log())
	}
	p := fb.set[0]
	if p.Name != "ci-token" || p.Kind != "secret" || p.Fields["value"] != secretValue || len(p.Fields) != 1 {
		t.Errorf("params = %s, fields %d", p, len(p.Fields))
	}
	if fb.count("credentials") < 2 || !strings.Contains(m.status, "stored") {
		t.Errorf("no reload or note: %v, %q", fb.log(), m.status)
	}
	if strings.Contains(fmt.Sprint(m.status, m.fail), secretValue) {
		t.Error("the value is in a message")
	}
}

func TestSetCredentialWithTimeoutZeroAsksOnce(t *testing.T) {
	fb := accessFake()
	fb.once = true
	m := appModel(t, fb, 80, 24)
	typeNewSecret(m)
	send(m, "enter")
	typed(m, testPass)
	send(m, "enter")
	if fb.count("credential-set ci-token") != 1 || fb.count("session") != 1 {
		t.Errorf("calls = %v", fb.log())
	}
}

func TestSetCredentialPendingNamesTheOpenApprovals(t *testing.T) {
	fb := accessFake()
	fb.session, fb.pending = true, []string{"demo-app"}
	m := appModel(t, fb, 80, 24)
	typeNewSecret(m)
	send(m, "enter")
	if fb.count("credential-set ci-token") != 1 || m.ov != nil || !strings.Contains(m.status, "Approvals open in Access: 1") {
		t.Errorf("calls = %v, status %q", fb.log(), m.status)
	}
}

func TestEscInTheFormSetsNothing(t *testing.T) {
	fb := accessFake()
	m := appModel(t, fb, 80, 24)
	typeNewSecret(m)
	send(m, "esc")
	if m.cred != nil || m.ov != nil || m.screen != screenArea || fb.count("credential-set ci-token") != 0 {
		t.Errorf("calls = %v", fb.log())
	}
	send(m, "n")
	if view := m.render(); strings.Contains(view, secretValue) {
		t.Error("the value survived the form")
	}
	if m.cred == nil || m.cred.name.Value() != "" {
		t.Error("the new form is not empty")
	}
}

func TestEditCredentialKeepsNameAndKind(t *testing.T) {
	fb := accessFake()
	fb.session = true
	m := appModel(t, fb, 80, 24)
	send(m, "5", "down", "e")
	view := plain(m)
	if !strings.Contains(view, "mail-relay") || !strings.Contains(view, "smtp") {
		t.Fatalf("the form lacks name and kind:\n%s", view)
	}
	typed(m, "mail-user")
	send(m, "tab")
	typed(m, secretValue)
	send(m, "enter")
	if len(fb.set) != 1 {
		t.Fatalf("calls = %v", fb.log())
	}
	p := fb.set[0]
	if p.Name != "mail-relay" || p.Kind != "smtp" || p.Fields["user"] != "mail-user" || p.Fields["password"] != secretValue {
		t.Errorf("params = %s, fields %d", p, len(p.Fields))
	}
}

func TestFormRefusesEmptyFields(t *testing.T) {
	fb := accessFake()
	m := appModel(t, fb, 80, 24)
	send(m, "5", "n")
	typed(m, "ci-token")
	f2(m)
	if m.cred == nil || !strings.Contains(plain(m), "Fill in the name") || m.ov != nil {
		t.Errorf("an incomplete form was submitted:\n%s", plain(m))
	}
}

func TestRemoveCredentialAsksThenRemoves(t *testing.T) {
	fb := accessFake()
	m := appModel(t, fb, 80, 24)
	send(m, "5", "down", "down", "d")
	if m.screen != screenConfirm || !strings.Contains(plain(m), "Remove credential unused-token?") {
		t.Fatalf("no question:\n%s", plain(m))
	}
	send(m, "y")
	typed(m, testPass)
	send(m, "enter")
	if len(fb.removed) != 1 || fb.removed[0] != "unused-token" || len(m.lib.cred.page.list.all) != 2 {
		t.Errorf("calls = %v, rows %v", fb.log(), m.lib.cred.page.list.all)
	}
}

func TestRemoveInUseShowsTheUses(t *testing.T) {
	fb := accessFake()
	fb.inUse, fb.session = true, true
	m := appModel(t, fb, 80, 24)
	send(m, "5", "d", "y")
	view := plain(m)
	if !strings.Contains(view, "the project demo-app uses the credential") || !strings.Contains(m.fail, "In use") {
		t.Errorf("the uses are not shown (fail %q):\n%s", m.fail, view)
	}
	if len(fb.removed) != 0 {
		t.Error("removed although in use")
	}
}

func TestCopyOfACredentialIsOnlyItsName(t *testing.T) {
	fb := accessFake()
	m := appModel(t, fb, 80, 24)
	typeNewSecret(m)
	send(m, "esc", "5")
	_, cmd := m.Update(keyMsg("c"))
	got := fmt.Sprint(cmd())
	if !strings.Contains(got, "registry-main") || strings.Contains(got, secretValue) {
		t.Errorf("copy = %q", got)
	}
}

// TestTypedSecretIsNeverDrawn types a value and walks every area, tab and size; none draws it.
func TestTypedSecretIsNeverDrawn(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		fb := accessFake()
		m := appModel(t, fb, size[0], size[1])
		check := func(where string) {
			t.Helper()
			if strings.Contains(m.render(), secretValue) {
				t.Errorf("%dx%d: the value is drawn at %s", size[0], size[1], where)
			}
		}
		typeNewSecret(m)
		check("the form")
		send(m, "enter")
		check("the passphrase entry")
		typed(m, testPass)
		send(m, "enter")
		for _, key := range []string{"1", "2", "3", "4", "5", "6"} {
			send(m, key)
			check("area " + key)
			if key == "4" || key == "5" {
				send(m, "enter")
				for range 3 {
					check("a detail of area " + key)
					send(m, "right")
				}
				send(m, "esc")
			}
		}
		send(m, "?")
		check("the help")
		send(m, "esc", ":")
		check("the command line")
		if strings.Contains(fmt.Sprint(m.status, m.fail), secretValue) {
			t.Error("the value is in a message")
		}
	}
}

func TestHelpAndInfoCoverTheNewScreens(t *testing.T) {
	m := appModel(t, accessFake(), 80, 24)
	send(m, "5", "i")
	if view := plain(m); !strings.Contains(view, "Secrets of registries") {
		t.Errorf("no info of the credentials page:\n%s", view)
	}
	send(m, "esc", "?", "tab", "tab", "tab", "tab", "tab")
	if view := plain(m); !strings.Contains(view, "never shown or copied") {
		t.Errorf("no help topic about credentials:\n%s", view)
	}
	send(m, "esc", "4", "?")
	if view := plain(m); !strings.Contains(view, "F2 approve") {
		t.Errorf("the key help of Access lacks the approve key:\n%s", view)
	}
}

func TestGermanTextsOfTheNewScreens(t *testing.T) {
	m := appModel(t, accessFake(), 120, 40)
	useLanguage(t, "de")
	send(m, "4")
	view := plain(m)
	for _, want := range []string{"Freigaben (2)", "freier Befehl", "Änderungen"} {
		if !strings.Contains(view, want) {
			t.Errorf("the German Access lacks %q:\n%s", want, view)
		}
	}
	send(m, "5")
	if view := plain(m); !strings.Contains(view, "Verwendungen") || !strings.Contains(view, "ungenutzt") {
		t.Errorf("the German credentials lack their words:\n%s", view)
	}
}
