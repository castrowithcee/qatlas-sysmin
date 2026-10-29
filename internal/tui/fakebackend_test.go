package tui

import (
	"errors"
	"slices"
	"strconv"
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
	// invalidTimeout makes SetAdminTimeout refuse its input with a problem of the field.
	invalidTimeout bool
	// once makes the session cover one change only, like an admin timeout of 0.
	once bool
	// details are the differing fields of an approval by project; credentials the vault listing.
	details     map[string][]control.ApprovalChange
	credentials []control.CredentialSummary
	// stale makes Approve refuse its fingerprint as changed; inUse makes RemoveCredential refuse as in use.
	stale, inUse bool
	// set holds what SetCredential received, by name; removed the names RemoveCredential took.
	set     []control.CredentialSetParams
	removed []string
	// fingerprints are handed out by Approval and Approvals; approved the projects approved, discarded the
	// ones discarded.
	approved, discarded []string
	failApproveAll      []control.ApprovalFailure
	// pending is what SetCredential reports as open approvals.
	pending []string
	// lib is the state of channels, templates and log.
	lib fakeLibrary
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
		if f.once {
			st.AdminRemaining = 0
		}
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

// spend checks the session for a changing call and uses it up where it covers one change only.
func (f *fakeBackend) spend() error {
	if !f.session {
		return &control.Error{Code: control.CodeAdminRequired}
	}
	if f.once {
		f.session = false
	}
	return nil
}

func (f *fakeBackend) Approval(project string) (control.ApprovalDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("approval " + project)
	for _, a := range f.approvals {
		if a.Project == project {
			return control.ApprovalDetail{ApprovalSummary: a, Details: slices.Clone(f.details[project])}, nil
		}
	}
	return control.ApprovalDetail{}, &control.Error{Code: control.CodeNothingOpen}
}

func (f *fakeBackend) Credentials() ([]control.CredentialSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("credentials")
	if f.vault != control.VaultUnlocked {
		return nil, &control.Error{Code: control.CodeLocked}
	}
	return slices.Clone(f.credentials), nil
}

func (f *fakeBackend) drop(project string) {
	f.approvals = slices.DeleteFunc(f.approvals, func(a control.ApprovalSummary) bool { return a.Project == project })
}

func (f *fakeBackend) Approve(project, fingerprint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("approve " + project + " " + fingerprint)
	if err := f.spend(); err != nil {
		return err
	}
	if f.stale {
		return &control.Error{Code: control.CodeChanged}
	}
	f.approved = append(f.approved, project)
	f.drop(project)
	return nil
}

func (f *fakeBackend) ApproveAll() (control.ApprovalsAllResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("approve-all")
	if err := f.spend(); err != nil {
		return control.ApprovalsAllResult{}, err
	}
	var res control.ApprovalsAllResult
	for _, a := range slices.Clone(f.approvals) {
		failed := slices.IndexFunc(f.failApproveAll, func(x control.ApprovalFailure) bool { return x.Project == a.Project })
		if failed >= 0 {
			res.Failed = append(res.Failed, f.failApproveAll[failed])
			continue
		}
		res.Done = append(res.Done, a.Project)
		f.approved = append(f.approved, a.Project)
		f.drop(a.Project)
	}
	return res, nil
}

func (f *fakeBackend) Discard(project string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("discard " + project)
	if err := f.spend(); err != nil {
		return err
	}
	f.discarded = append(f.discarded, project)
	f.drop(project)
	return nil
}

func (f *fakeBackend) DiscardAll() (control.ApprovalsAllResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("discard-all")
	if err := f.spend(); err != nil {
		return control.ApprovalsAllResult{}, err
	}
	var res control.ApprovalsAllResult
	for _, a := range f.approvals {
		res.Done = append(res.Done, a.Project)
	}
	f.approvals = nil
	return res, nil
}

func (f *fakeBackend) SetCredential(p control.CredentialSetParams) (control.CredentialResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("credential-set " + p.Name)
	if err := f.spend(); err != nil {
		return control.CredentialResult{}, err
	}
	f.set = append(f.set, p)
	return control.CredentialResult{Name: p.Name, Action: "created", State: control.StateApproved, Pending: f.pending}, nil
}

func (f *fakeBackend) RemoveCredential(name string) (control.CredentialResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("credential-remove " + name)
	if f.inUse {
		return control.CredentialResult{}, &control.Error{Code: control.CodeInUse, Problems: []control.Problem{
			{Field: "project:demo-app", Code: control.CodeInUse, Message: "the project demo-app uses the credential in the current configuration"}}}
	}
	if err := f.spend(); err != nil {
		return control.CredentialResult{}, err
	}
	f.removed = append(f.removed, name)
	f.credentials = slices.DeleteFunc(f.credentials, func(c control.CredentialSummary) bool { return c.Name == name })
	return control.CredentialResult{Name: name, Action: "removed", State: control.StateApproved}, nil
}

// The state of Library's channels and templates and of the log in the fake service.
type fakeLibrary struct {
	channels []control.ChannelSummary
	// added and removed are what AddChannel and RemoveChannel took; tested the channels TestChannel got.
	added   []control.ChannelParams
	removed []string
	tested  []string
	// testErr is what TestChannel answers; addErr what AddChannel and DeriveTemplate answer instead of success;
	// pendingChange makes AddChannel and RemoveChannel report the change as not approved.
	testErr, addErr error
	pendingChange   bool

	templates []control.TemplateSummary
	files     map[string]string
	derived   []control.TemplateDeriveParams

	// entries is the whole log, newest first; logCalls the requests made; verify the answer to VerifyLog.
	entries   []control.LogEntry
	logCalls  []control.LogListParams
	verify    control.LogVerifyResult
	verifyErr error

	// status and history are the deploy state and runs by project (history oldest first); logs the run logs by
	// project and run ID; deployErr is what the three deploy reads answer instead.
	status    map[string]control.DeployStatusResult
	history   map[string][]control.DeployRun
	logs      map[string]string
	deployErr error
}

func (f *fakeBackend) Channels() ([]control.ChannelSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("channels")
	return slices.Clone(f.lib.channels), nil
}

func (f *fakeBackend) TestChannel(name string) (control.NotifyTestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("channel-test " + name)
	f.lib.tested = append(f.lib.tested, name)
	if f.lib.testErr != nil {
		return control.NotifyTestResult{}, f.lib.testErr
	}
	return control.NotifyTestResult{Channel: name, Event: "notify.test"}, nil
}

func (f *fakeBackend) changeState() string {
	if f.lib.pendingChange {
		return control.StatePending
	}
	return control.StateApproved
}

func (f *fakeBackend) AddChannel(p control.ChannelParams) (control.ChangeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("channel-add " + p.Name)
	if err := f.spend(); err != nil {
		return control.ChangeResult{}, err
	}
	if f.lib.addErr != nil {
		return control.ChangeResult{}, f.lib.addErr
	}
	f.lib.added = append(f.lib.added, p)
	f.lib.channels = append(f.lib.channels, control.ChannelSummary{ChannelParams: p, State: f.changeState()})
	return control.ChangeResult{Project: "channels", State: f.changeState()}, nil
}

func (f *fakeBackend) RemoveChannel(name string) (control.ChangeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("channel-remove " + name)
	if err := f.spend(); err != nil {
		return control.ChangeResult{}, err
	}
	f.lib.removed = append(f.lib.removed, name)
	f.lib.channels = slices.DeleteFunc(f.lib.channels, func(c control.ChannelSummary) bool { return c.Name == name })
	return control.ChangeResult{Project: "channels", State: f.changeState()}, nil
}

func (f *fakeBackend) Templates() ([]control.TemplateSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("templates")
	return slices.Clone(f.lib.templates), nil
}

func (f *fakeBackend) Template(name string) (control.TemplateDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("template " + name)
	for _, t := range f.lib.templates {
		if t.Name == name {
			return control.TemplateDetail{TemplateSummary: t, File: f.lib.files[name]}, nil
		}
	}
	return control.TemplateDetail{}, &control.Error{Code: control.CodeNotFound}
}

func (f *fakeBackend) DeriveTemplate(p control.TemplateDeriveParams) (control.TemplateDetail, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("template-derive " + p.Name)
	if err := f.spend(); err != nil {
		return control.TemplateDetail{}, err
	}
	if f.lib.addErr != nil {
		return control.TemplateDetail{}, f.lib.addErr
	}
	f.lib.derived = append(f.lib.derived, p)
	t := control.TemplateSummary{Name: p.Name, Origin: control.TemplateCustom, Source: control.TemplateSourceConfig, Description: p.Description, Steps: 2}
	f.lib.templates = append(f.lib.templates, t)
	return control.TemplateDetail{TemplateSummary: t}, nil
}

// LogEntries pages through entries by an index in Next. It does not apply the filters: a test reads them
// from logCalls.
func (f *fakeBackend) LogEntries(p control.LogListParams) (control.LogListResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("log")
	f.lib.logCalls = append(f.lib.logCalls, p)
	start := 0
	if p.After != "" {
		start, _ = strconv.Atoi(p.After)
	}
	limit := p.Limit
	if limit <= 0 {
		limit = control.LogDefaultLimit
	}
	end := min(start+limit, len(f.lib.entries))
	res := control.LogListResult{Entries: slices.Clone(f.lib.entries[min(start, end):end])}
	if end < len(f.lib.entries) {
		res.Next = strconv.Itoa(end)
	}
	return res, nil
}

func (f *fakeBackend) VerifyLog() (control.LogVerifyResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("log-verify")
	return f.lib.verify, f.lib.verifyErr
}

func (f *fakeBackend) DeployStatus(project string) (control.DeployStatusResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("deploy-status " + project)
	if f.lib.deployErr != nil {
		return control.DeployStatusResult{}, f.lib.deployErr
	}
	st, ok := f.lib.status[project]
	if !ok {
		st = control.DeployStatusResult{Project: project, Sources: []control.DeploySource{}}
	}
	return st, nil
}

func (f *fakeBackend) DeployHistory(project string) (control.DeployHistoryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("deploy-history " + project)
	if f.lib.deployErr != nil {
		return control.DeployHistoryResult{}, f.lib.deployErr
	}
	return control.DeployHistoryResult{Project: project, Runs: slices.Clone(f.lib.history[project])}, nil
}

func (f *fakeBackend) DeployLog(project, run string) (control.DeployLogResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("deploy-log " + project + " " + run)
	if f.lib.deployErr != nil {
		return control.DeployLogResult{}, f.lib.deployErr
	}
	text, ok := f.lib.logs[project+"/"+run]
	if !ok {
		return control.DeployLogResult{}, &control.Error{Code: control.CodeNotFound}
	}
	return control.DeployLogResult{Project: project, Run: run, Text: text}, nil
}
