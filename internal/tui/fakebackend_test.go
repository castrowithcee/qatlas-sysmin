package tui

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// testPass is the invented passphrase of the fake service.
const testPass = "correct horse test"

// fakeBackend is a service in memory: a vault with a passphrase and one admin session.
type fakeBackend struct {
	mu        sync.Mutex
	vault     string
	pass      string
	session   bool
	remaining int64
	timeout   string
	down      error
	calls     []string
	changes   int
	// unlockMode is what Status reports as the unlock mode; empty reads as manual.
	unlockMode string
	approvals  []control.ApprovalSummary
	projects   []control.ProjectSummary
	// proj is the state of the project reads and controls; see projectsfake_test.go.
	proj fakeProjects
	// invalidTimeout makes SetAdminTimeout refuse its input with a problem of the field.
	invalidTimeout bool
}

func newFake(vault string) *fakeBackend {
	return &fakeBackend{vault: vault, pass: testPass, remaining: 900, timeout: "15m0s"}
}

func (f *fakeBackend) record(call string) { f.calls = append(f.calls, call) }

func (f *fakeBackend) Status() (control.StatusResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("status")
	if f.down != nil {
		return control.StatusResult{}, f.down
	}
	st := control.StatusResult{Version: "0.0.0-test", Vault: f.vault, AdminTimeout: f.timeout, ConfigDir: "/etc/sysmin-test"}
	if f.vault != control.VaultAbsent {
		st.Unlock = control.UnlockManual
		if f.unlockMode != "" {
			st.Unlock = f.unlockMode
		}
	}
	if f.session {
		st.Admin, st.AdminRemaining = true, f.remaining
	}
	return st, nil
}

func (f *fakeBackend) Unlock(pass string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("unlock")
	if pass != f.pass {
		return &control.Error{Code: control.CodeWrongPassphrase}
	}
	f.vault = control.VaultUnlocked
	return nil
}

func (f *fakeBackend) OpenSession(pass string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("session")
	if f.vault != control.VaultUnlocked {
		return &control.Error{Code: control.CodeLocked}
	}
	if pass != f.pass {
		return &control.Error{Code: control.CodeWrongPassphrase}
	}
	f.session = true
	return nil
}

func (f *fakeBackend) EndSession() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("end")
	f.session = false
	return nil
}

func (f *fakeBackend) CreateVault(pass string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("create")
	if f.vault != control.VaultAbsent {
		return &control.Error{Code: control.CodeExists}
	}
	f.vault, f.pass = control.VaultUnlocked, pass
	return nil
}

func (f *fakeBackend) ChangePassphrase(old, next string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("passphrase")
	switch {
	case !f.session:
		return &control.Error{Code: control.CodeAdminRequired}
	case old != f.pass:
		return &control.Error{Code: control.CodeWrongPassphrase}
	}
	f.pass = next
	return nil
}

func (f *fakeBackend) SetAdminTimeout(text string) (control.SettingsResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("timeout " + text)
	if !f.session {
		return control.SettingsResult{}, &control.Error{Code: control.CodeAdminRequired}
	}
	if f.invalidTimeout {
		return control.SettingsResult{}, &control.Error{Code: control.CodeInvalid, Problems: []control.Problem{
			{Field: "vault.admin_timeout", Code: "range", Message: "must not be negative", Hint: "use 0 or a positive duration"}}}
	}
	d, err := time.ParseDuration(text)
	if err != nil {
		return control.SettingsResult{}, &control.Error{Code: control.CodeBadRequest}
	}
	f.timeout = d.String()
	return control.SettingsResult{AdminTimeout: f.timeout, Explicit: true}, nil
}

func (f *fakeBackend) Settings() (control.SettingsResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("settings")
	if f.down != nil {
		return control.SettingsResult{}, f.down
	}
	return control.SettingsResult{AdminTimeout: f.timeout}, nil
}

func (f *fakeBackend) Approvals() ([]control.ApprovalSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("approvals")
	if f.vault != control.VaultUnlocked {
		return nil, &control.Error{Code: control.CodeLocked}
	}
	return slices.Clone(f.approvals), nil
}

func (f *fakeBackend) Projects() ([]control.ProjectSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("projects")
	return slices.Clone(f.projects), nil
}

// expire ends the session on the service without the interface hearing of it.
func (f *fakeBackend) expire() { f.mu.Lock(); f.session = false; f.mu.Unlock() }

// change is a changing call of the fake service: it needs the session.
func (f *fakeBackend) change(Backend) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("change")
	if !f.session {
		return &control.Error{Code: control.CodeAdminRequired}
	}
	f.changes++
	return nil
}

func (f *fakeBackend) count(call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}

func (f *fakeBackend) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// drain runs a command the way the program would, feeding every message it yields back into m. The ticks are
// left out: they would only wait and call themselves again.
func drain(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil, tickMsg:
	case tea.BatchMsg:
		for _, c := range msg {
			drain(m, c)
		}
	default:
		_, next := m.Update(msg)
		drain(m, next)
	}
}

// appModel is the service interface of the given size over fb, past its first status.
func appModel(t *testing.T, fb *fakeBackend, w, h int) *Model {
	t.Helper()
	restoreRenderer(t)
	setProfile(profileNone)
	applyTheme(uiprefs.ThemeTerminal)
	useLanguage(t, "en")
	every := tickEvery
	tickEvery = time.Millisecond
	t.Cleanup(func() { tickEvery = every })
	m := New(uiprefs.Default(), nil)
	m.enableAreas(fb)
	sizedModel(m, w, h)
	drain(m, m.statusCmd())
	return m
}

// send hands a key to m and runs what it asks for.
func send(m *Model, keys ...string) {
	for _, k := range keys {
		_, cmd := m.Update(keyMsg(k))
		drain(m, cmd)
	}
}

// typed enters text into the focused field of the overlay.
func typed(m *Model, text string) {
	for _, r := range text {
		_, cmd := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		drain(m, cmd)
	}
}

// clockModel gives m a clock the test moves.
func clockModel(m *Model, start time.Time) *time.Time {
	now := start
	m.now = func() time.Time { return now }
	return &now
}

var errDropped = errors.New("connection dropped")
