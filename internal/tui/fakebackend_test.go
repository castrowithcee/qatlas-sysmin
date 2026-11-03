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
)

// fakeBackend is a service in memory.
type fakeBackend struct {
	mu    sync.Mutex
	down  error
	calls []string
	// audit is the state of the log.
	audit fakeLog
}

func newFake() *fakeBackend { return &fakeBackend{} }

func (f *fakeBackend) record(call string) { f.calls = append(f.calls, call) }

func (f *fakeBackend) Status() (control.StatusResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("status")
	if f.down != nil {
		return control.StatusResult{}, f.down
	}
	return control.StatusResult{Version: "0.0.0-test"}, nil
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
	m := testModel(defaultPrefs(), nil)
	m.tickEvery = time.Millisecond
	m.enableAreas(fb)
	m.hostSrc = newFixtureHost()
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

// typed enters text into the focused field.
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

// fakeLog is the state of the log in the fake service.
type fakeLog struct {
	// entries is the whole log, newest first; calls the requests made; verify the answer to VerifyLog.
	entries   []control.LogEntry
	calls     []control.LogListParams
	verify    control.LogVerifyResult
	verifyErr error
}

// LogEntries pages through entries by an index in Next. It does not apply the filters: a test reads them
// from logCalls.
func (f *fakeBackend) LogEntries(p control.LogListParams) (control.LogListResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("log")
	f.audit.calls = append(f.audit.calls, p)
	start := 0
	if p.After != "" {
		start, _ = strconv.Atoi(p.After)
	}
	limit := p.Limit
	if limit <= 0 {
		limit = control.LogDefaultLimit
	}
	end := min(start+limit, len(f.audit.entries))
	res := control.LogListResult{Entries: slices.Clone(f.audit.entries[min(start, end):end])}
	if end < len(f.audit.entries) {
		res.Next = strconv.Itoa(end)
	}
	return res, nil
}

func (f *fakeBackend) VerifyLog() (control.LogVerifyResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("log-verify")
	return f.audit.verify, f.audit.verifyErr
}
