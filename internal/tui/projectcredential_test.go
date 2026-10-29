package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// credSecret is the invented secret the tests type; it must appear on no screen.
const credSecret = "tok-Zq81-example-secret"

// noSecret fails when any drawing of the interface, with or without the escape sequences, shows the secret
// or the copy key could hand it over.
func noSecret(t *testing.T, m *Model, step string) {
	t.Helper()
	if strings.Contains(m.render(), credSecret) || strings.Contains(plain(m), credSecret) {
		t.Fatalf("%s: the screen shows the secret:\n%s", step, plain(m))
	}
	for _, text := range []string{m.status, m.fail, m.busy} {
		if strings.Contains(text, credSecret) {
			t.Fatalf("%s: a message shows the secret: %q", step, text)
		}
	}
	if src := m.copySource(); src != nil && strings.Contains(m.copyClean(src()), credSecret) {
		t.Fatalf("%s: the copy key hands the secret over", step)
	}
}

// credFlow is the setup at the source step with the focus on the credential of the first image.
func credFlow(t *testing.T, fb *fakeBackend) *Model {
	t.Helper()
	flowFakeLists(fb)
	m := flowModel(t, fb, 120, 40)
	typed(m, "demo-app")
	send(m, "f2")
	flowToRow(t, m, "images[0].credential")
	return m
}

// credChooseNew opens the panel that creates a credential from the list of the focused row.
func credChooseNew(t *testing.T, m *Model) {
	t.Helper()
	send(m, "enter", "/")
	typed(m, "New")
	send(m, "enter", "enter")
	if !m.projectPromptOpen() || m.prj.prompt.kind != promptCredential {
		t.Fatalf("the panel is not open: screen = %v, fail = %q\n%s", m.screen, m.fail, plain(m))
	}
}

func TestCredentialListsTheNamesAndKindsThatFit(t *testing.T) {
	fb := projectsFake()
	m := credFlow(t, fb)
	send(m, "enter")
	view := plain(m)
	for _, want := range []string{"demo-registry · registry", "none", "New credential..."} {
		if !strings.Contains(view, want) {
			t.Errorf("the list lacks %q:\n%s", want, view)
		}
	}
	for _, other := range []string{"demo-hook", "demo-git"} {
		if strings.Contains(view, other) {
			t.Errorf("the list of an image credential offers %q:\n%s", other, view)
		}
	}
	send(m, "down", "enter")
	if got := m.prj.flow.draft.images[0].credential; got != "demo-registry" {
		t.Errorf("credential = %q", got)
	}
	send(m, "enter", "up", "enter")
	if got := m.prj.flow.draft.images[0].credential; got != "" {
		t.Errorf("none leaves %q", got)
	}
}

func TestCredentialCreatedOnTheWayAsksThePassphraseAndSetsTheField(t *testing.T) {
	fb := projectsFake()
	m := credFlow(t, fb)
	credChooseNew(t, m)
	panel := m.prj.prompt
	noSecret(t, m, "panel")
	for _, want := range []string{"New credential", "Name:", "Kind:", "registry", "user:", "token:"} {
		if !strings.Contains(plain(m), want) {
			t.Errorf("the panel lacks %q:\n%s", want, plain(m))
		}
	}
	typed(m, "new-registry")
	send(m, "enter")
	noSecret(t, m, "kind")
	send(m, "enter")
	typed(m, "demo-user")
	send(m, "enter")
	for _, r := range credSecret {
		typed(m, string(r))
		noSecret(t, m, "typing")
	}
	if !strings.Contains(plain(m), "****") {
		t.Errorf("the secret is not drawn masked:\n%s", plain(m))
	}
	send(m, "c")
	noSecret(t, m, "after c")
	if got := panel.fields[len(panel.fields)-1].in.Value(); got != credSecret+"c" {
		t.Fatalf("c in the secret field is text, field = %q", got)
	}
	send(m, "backspace")
	send(m, "f2")
	if m.ov == nil || len(fb.set) != 0 || fb.count("credential-set new-registry") != 0 {
		t.Fatalf("F2 without a session: overlay = %v, calls = %v", m.ov, fb.log())
	}
	noSecret(t, m, "passphrase")
	typed(m, testPass)
	send(m, "enter")
	if len(fb.set) != 1 {
		t.Fatalf("calls = %v, fail = %q", fb.log(), m.fail)
	}
	want := control.CredentialSetParams{Name: "new-registry", Kind: "registry", Fields: map[string]string{"user": "demo-user", "token": credSecret}}
	got := fb.set[0]
	if got.Name != want.Name || got.Kind != want.Kind || got.Fields["user"] != "demo-user" || got.Fields["token"] != credSecret || len(got.Fields) != 2 {
		t.Fatalf("request = %+v", got)
	}
	if fb.count("session") != 1 {
		t.Errorf("calls = %v", fb.log())
	}
	if got := m.prj.flow.draft.images[0].credential; got != "new-registry" {
		t.Errorf("the field holds %q", got)
	}
	if !strings.Contains(m.status, "new-registry") || m.prj.prompt != nil {
		t.Errorf("status = %q, prompt = %v", m.status, m.prj.prompt)
	}
	for i, f := range panel.fields {
		if f.in.Value() != "" {
			t.Errorf("field %d keeps %q after the send", i, f.in.Value())
		}
	}
	noSecret(t, m, "done")
	send(m, "c")
	noSecret(t, m, "c after the save")
	// The summary and the project file carry the name only.
	m.prj.flow.show(flowSummary)
	noSecret(t, m, "summary")
	if p, _ := m.prj.flow.draft.project(); strings.Contains(fmt.Sprint(p), credSecret) {
		t.Error("the project holds the secret")
	}
}

func TestCredentialWithASessionSavesAtOnce(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := credFlow(t, fb)
	credChooseNew(t, m)
	typed(m, "new-registry")
	send(m, "down", "down")
	typed(m, "demo-user")
	send(m, "down")
	typed(m, credSecret)
	send(m, "f2")
	if m.ov != nil || len(fb.set) != 1 || m.prj.flow.draft.images[0].credential != "new-registry" {
		t.Fatalf("overlay = %v, calls = %v", m.ov, fb.log())
	}
	noSecret(t, m, "saved")
}

func TestCredentialPanelRefusesWhatIsWrongBeforeSending(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := credFlow(t, fb)
	credChooseNew(t, m)
	typed(m, "Bad Name")
	send(m, "f2")
	if m.fail != tr(tCredBadName) || !m.projectPromptOpen() {
		t.Fatalf("bad name: fail = %q", m.fail)
	}
	send(m, "ctrl+u")
	typed(m, "demo-registry")
	send(m, "f2")
	if m.fail != tr(tCredExists) {
		t.Fatalf("existing name: fail = %q", m.fail)
	}
	send(m, "ctrl+u")
	typed(m, "other")
	send(m, "f2")
	if m.fail != tr(tCredFill) || len(fb.set) != 0 {
		t.Fatalf("empty value: fail = %q, calls = %v", m.fail, fb.log())
	}
	send(m, "esc")
	if m.prj.prompt != nil || m.screen != screenArea || m.prj.flow.draft.images[0].credential != "" {
		t.Errorf("esc: prompt = %v, screen = %v", m.prj.prompt, m.screen)
	}
}

func TestCredentialRefusedByTheServiceReopensWithoutTheSecret(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	fb.proj.credErr = &control.Error{Code: control.CodeInvalid}
	m := credFlow(t, fb)
	credChooseNew(t, m)
	typed(m, "new-registry")
	send(m, "down", "down")
	typed(m, "demo-user")
	send(m, "down")
	typed(m, credSecret)
	send(m, "f2")
	if !m.projectPromptOpen() || m.fail != tr(tErrInvalid) {
		t.Fatalf("prompt = %v, fail = %q", m.projectPromptOpen(), m.fail)
	}
	p := m.prj.prompt
	if p.fields[0].in.Value() != "new-registry" || p.fields[2].in.Value() != "demo-user" || p.fields[3].in.Value() != "" {
		t.Errorf("fields = %q %q %q", p.fields[0].in.Value(), p.fields[2].in.Value(), p.fields[3].in.Value())
	}
	if m.prj.flow.draft.images[0].credential != "" {
		t.Error("the field was set by a refused credential")
	}
	noSecret(t, m, "refused")
}

func TestCredentialKindsFollowTheRow(t *testing.T) {
	cases := []struct {
		accepted, want []string
	}{
		{flowKindsImage, []string{"registry"}},
		{flowKindsGit, []string{"git-https"}},
		{flowKindsSecret, []string{"secret"}},
	}
	for _, c := range cases {
		if got := credentialKinds(c.accepted); !slices.Equal(got, c.want) {
			t.Errorf("kinds(%v) = %v, want %v", c.accepted, got, c.want)
		}
	}
	for _, k := range credentialKinds([]string{"registry", "git-https", "secret", "smtp", "ssh-key"}) {
		if k == "ssh-key" {
			t.Error("a secret that spans lines is offered for a single line")
		}
	}
}

func TestCredentialKindChangeEmptiesWhatWasTyped(t *testing.T) {
	m := credFlow(t, projectsFake())
	f := m.prj.flow
	f.show(flowNotify)
	f.draft.hookKind = "github"
	flowToRow(t, m, "webhook.secret")
	send(m, "enter", "/")
	typed(m, "New")
	send(m, "enter", "enter")
	p := m.prj.prompt
	if len(p.fields) != 3 || p.fields[2].id != "value" {
		t.Fatalf("a secret credential has a single value field: %+v", p.fields)
	}
	typed(m, "x")
	// The kinds a webhook accepts are one, so the kind cannot change here; the panel of an image does.
	m.closeProjectPrompt()
	f.show(flowSource)
	flowToRow(t, m, "images[0].credential")
	credChooseNew(t, m)
	p = m.prj.prompt
	p.focusOn(1)
	p.fields[1].choices = []string{"registry", "smtp"}
	send(m, "down", "down")
	typed(m, "demo-user")
	send(m, "up", "up", "right")
	if p.fields[1].choices[p.fields[1].at] != "smtp" || p.fields[3].id != "password" {
		t.Fatalf("fields = %+v", p.fields)
	}
	for i, field := range p.fields[2:] {
		if field.in.Value() != "" {
			t.Errorf("field %d keeps %q", i+2, field.in.Value())
		}
	}
}

func TestFlowChannelsArePickedFromTheService(t *testing.T) {
	fb := projectsFake()
	fb.lib.channels = []control.ChannelSummary{{ChannelParams: control.ChannelParams{Name: "ops-mail"}},
		{ChannelParams: control.ChannelParams{Name: "ops-chat"}}}
	m := flowModel(t, fb, 120, 40)
	f := m.prj.flow
	f.draft.channels = "old-channel"
	f.show(flowNotify)
	for _, want := range []string{"ops-mail", "ops-chat", "old-channel"} {
		if !strings.Contains(plain(m), want) {
			t.Errorf("the channels lack %q:\n%s", want, plain(m))
		}
	}
	flowToRow(t, m, "notify.channels/ops-chat")
	send(m, "space")
	flowToRow(t, m, "notify.channels/old-channel")
	send(m, "space")
	if got := f.draft.channels; got != "ops-chat" {
		t.Errorf("channels = %q", got)
	}
}

func TestFlowChannelsFallBackToTextAndNoneIsSaid(t *testing.T) {
	fb := projectsFake()
	fb.proj.channelsErr = &control.Error{Code: control.CodeLocked}
	m := flowModel(t, fb, 120, 40)
	m.prj.flow.show(flowNotify)
	flowFill(t, m, "notify.channels", "ops-mail")
	if got := m.prj.flow.draft.channels; got != "ops-mail" {
		t.Errorf("channels = %q", got)
	}
	m = flowModel(t, projectsFake(), 120, 40)
	m.prj.flow.show(flowNotify)
	if !strings.Contains(plain(m), "No channels yet") {
		t.Errorf("an empty list is not said:\n%s", plain(m))
	}
}

func TestCredentialListNeedsTheVault(t *testing.T) {
	fb := projectsFake()
	fb.proj.credsErr = &control.Error{Code: control.CodeLocked}
	m := credFlow(t, fb)
	flowFakeLists(fb)
	fb.proj.credsErr = &control.Error{Code: control.CodeLocked}
	send(m, "enter")
	if m.fail != tr(tErrLocked) || m.prj.dialog != nil {
		t.Errorf("fail = %q, dialog = %v", m.fail, m.prj.dialog)
	}
}

func TestGoldenProjectsCredentialPanel(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			fb := projectsFake()
			flowFakeLists(fb)
			m := goldenApp(t, fb, size[0], size[1])
			send(m, "2", "n", "enter")
			typed(m, "demo-app")
			send(m, "f2")
			flowToRow(t, m, "images[0].credential")
			credChooseNew(t, m)
			typed(m, "new-registry")
			send(m, "down", "down")
			typed(m, "demo-user")
			send(m, "down")
			typed(m, credSecret)
			assertViewFits(t, plain(m), size[0], size[1])
			noSecret(t, m, "golden")
			assertGolden(t, fmt.Sprintf("projects_credential_%dx%d", size[0], size[1]), m.render())
		})
	}
}
