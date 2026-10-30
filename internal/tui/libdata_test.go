package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// utcClock makes the log times of a test read the same everywhere.
func utcClock(t *testing.T) {
	t.Helper()
	before := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = before })
}

// testLogEntries is a log of n entries, newest first, with invented values.
func testLogEntries(n int) []control.LogEntry {
	actions := []string{"config.read", "approval.grant", "credential.create", "deploy.run", "session.open"}
	start := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	var out []control.LogEntry
	for i := range n {
		uid := uint32(1000)
		if i%3 == 0 {
			uid = 0
		}
		e := control.LogEntry{Seq: uint64(n - i), Time: start.Add(-time.Duration(i) * time.Minute), ClientUID: &uid,
			Action: actions[i%len(actions)], Result: control.LogResultSuccess, DurationMS: int64(3 + i*7), Signed: i%4 != 0}
		if i%2 == 0 {
			e.Project = "demo-app"
		}
		if i%5 == 4 {
			e.Result, e.ErrorCode = control.LogResultFailure, "refused"
		}
		out = append(out, e)
	}
	return out
}

func intactChain() control.LogVerifyResult {
	return control.LogVerifyResult{Intact: true, Checked: true, Days: []control.LogDay{
		{Date: "2026-10-27", Entries: 40, Intact: true, Signatures: control.LogSignaturesValid},
		{Date: "2026-10-28", Entries: 12, Intact: true, Signatures: control.LogSignaturesValid}}}
}

func brokenChain() control.LogVerifyResult {
	return control.LogVerifyResult{Intact: false, Checked: false, Days: []control.LogDay{
		{Date: "2026-10-27", Entries: 40, Intact: false, Signatures: control.LogSignaturesNotChecked},
		{Date: "2026-10-28", Entries: 12, Intact: true, Signatures: control.LogSignaturesNotChecked}},
		Problems: []control.LogProblem{{Date: "2026-10-27", Seq: 31, Line: 31, Code: control.LogProblemChain},
			{Date: "2026-10-28", Line: 4, Code: control.LogProblemMAC}}}
}

// libFake is a service with channels, templates and a short log.
func libFake() *fakeBackend {
	fb := accessFake()
	fb.credentials = append(fb.credentials, control.CredentialSummary{Name: "demo-hmac", Kind: "secret", ID: "cred-0004"})
	fb.lib.channels = []control.ChannelSummary{
		{ChannelParams: control.ChannelParams{Name: "chat-ops", Type: "telegram", ChatID: "100200", Credential: "unused-token"}, State: control.StatePending},
		{ChannelParams: control.ChannelParams{Name: "ops-hook", Type: "webhook", URL: "https://hooks.example.test/notify", Credential: "demo-hmac"}, State: control.StateApproved},
		{ChannelParams: control.ChannelParams{Name: "ops-mail", Type: "smtp", Host: "smtp.example.test", Port: 587, From: "sysmin@example.test",
			To: []string{"ops@example.test"}, Security: "starttls", Credential: "mail-relay"}, State: control.StateApproved},
	}
	fb.lib.templates = []control.TemplateSummary{
		{Name: "compose-update", Origin: control.TemplateBuiltin, Source: control.TemplateSourceEmbedded, Description: "Pull images and restart the stack", Steps: 4},
		{Name: "git-deploy", Origin: control.TemplateBuiltin, Source: control.TemplateSourceEmbedded, Description: "Fetch a repository and deploy it", Steps: 5},
		{Name: "my-update", Origin: control.TemplateCustom, Source: control.TemplateSourceConfig, Description: "Update with a health check", Steps: 4},
	}
	fb.lib.files = map[string]string{
		"compose-update": "schema: 1\nname: compose-update\ndescription: Pull images and restart the stack\nsteps:\n  - name: pull\n    run: compose pull\n  - name: up\n    run: compose up -d\n",
		"my-update":      "schema: 1\nname: my-update\nsteps:\n  - name: pull\n    run: compose pull\n",
	}
	fb.lib.entries = testLogEntries(12)
	fb.lib.verify = intactChain()
	return fb
}

func TestChannelsListShowsStateAndNoSecret(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		m := appModel(t, libFake(), size[0], size[1])
		send(m, "5", "right")
		view := plain(m)
		for _, want := range []string{"Channels (3)", "ops-hook", "ops-mail", "smtp.example.test", "pending approval", "approved"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: the list lacks %q:\n%s", size[0], size[1], want, view)
			}
		}
		assertViewFits(t, view, size[0], size[1])
	}
}

func TestChannelTestNeedsNoSessionAndShowsAFixedText(t *testing.T) {
	fb := libFake()
	m := appModel(t, fb, 80, 24)
	send(m, "5", "right", "down", "t")
	if fb.count("channel-test ops-hook") != 1 || fb.count("session") != 0 || m.ov != nil {
		t.Fatalf("calls = %v", fb.log())
	}
	if !strings.Contains(m.status, "Test message sent to ops-hook") {
		t.Errorf("status = %q", m.status)
	}
	fb.lib.testErr = &control.Error{Code: control.CodeSendFailed}
	send(m, "t")
	if m.fail != "ops-hook did not take the test message." || m.status != "" {
		t.Errorf("fail = %q, status = %q", m.fail, m.status)
	}
	fb.lib.testErr = &control.Error{Code: control.CodeNotApproved}
	send(m, "t")
	if !strings.Contains(m.fail, "Only approved channels") {
		t.Errorf("fail = %q", m.fail)
	}
}

// typeChannel fills the form of an smtp channel up to its last row.
func typeSMTPChannel(m *Model) {
	send(m, "5", "right", "n")
	typed(m, "team-mail")
	send(m, "tab", "right", "tab")
	typed(m, "smtp.example.test")
	send(m, "tab")
	typed(m, "587")
	send(m, "tab")
	typed(m, "sysmin@example.test")
	send(m, "tab")
	typed(m, "ops@example.test, dev@example.test")
	send(m, "tab", "right", "tab", "right")
}

func TestNewChannelAsksForTheSessionAndAddsOnce(t *testing.T) {
	fb := libFake()
	m := appModel(t, fb, 80, 24)
	typeSMTPChannel(m)
	view := plain(m)
	for _, want := range []string{"New channel", "team-mail", "smtp.example.test", "starttls", "mail-relay"} {
		if !strings.Contains(view, want) {
			t.Errorf("the form lacks %q:\n%s", want, view)
		}
	}
	f2(m)
	if m.ov == nil || fb.count("channel-add team-mail") != 0 || m.form != nil {
		t.Fatal("the passphrase was not asked before the call")
	}
	typed(m, testPass)
	send(m, "enter")
	if fb.count("channel-add team-mail") != 1 || len(fb.lib.added) != 1 {
		t.Fatalf("calls = %v", fb.log())
	}
	p := fb.lib.added[0]
	if p.Type != "smtp" || p.Host != "smtp.example.test" || p.Port != 587 || p.From != "sysmin@example.test" ||
		strings.Join(p.To, "|") != "ops@example.test|dev@example.test" || p.Security != "starttls" || p.Credential != "mail-relay" || p.URL != "" {
		t.Errorf("params = %+v", p)
	}
	if !strings.Contains(m.status, "Channel added") || fb.count("channels") < 2 {
		t.Errorf("status %q, calls %v", m.status, fb.log())
	}
}

func TestNewChannelPendingWithoutApproval(t *testing.T) {
	fb := libFake()
	fb.session, fb.lib.pendingChange = true, true
	m := appModel(t, fb, 80, 24)
	typeSMTPChannel(m)
	f2(m)
	if fb.count("channel-add team-mail") != 1 || !strings.Contains(m.status, "wait for approval in Access") {
		t.Errorf("calls %v, status %q", fb.log(), m.status)
	}
}

func TestChannelFormOffersOnlyTheCredentialsOfTheRightKind(t *testing.T) {
	m := appModel(t, libFake(), 80, 24)
	send(m, "5", "right", "n")
	f := m.form
	_, shown := m.credentialChoices("webhook")
	if strings.Join(shown, ",") != "none,demo-hmac,unused-token" {
		t.Errorf("secret choices = %v", shown)
	}
	if f.row("host").hidden != true || f.row("url").hidden != false {
		t.Error("the rows do not follow the type")
	}
	send(m, "tab", "right")
	if f.row("url").hidden != true || f.row("host").hidden != false {
		t.Error("the rows do not follow the new type")
	}
	if got := strings.Join(f.row("credential").shown, ","); got != "none,mail-relay" {
		t.Errorf("smtp choices = %v", got)
	}
}

func TestChannelFormShowsProblemsUnderTheirFields(t *testing.T) {
	fb := libFake()
	fb.session = true
	fb.lib.addErr = &control.Error{Code: control.CodeInvalid, Problems: []control.Problem{
		{Field: "channels[3].host", Code: "invalid", Message: "host is not a name", Hint: "use a host name"},
		{Field: "channels[3].to[0]", Code: "invalid", Message: "not an email address"}}}
	m := appModel(t, fb, 80, 24)
	typeSMTPChannel(m)
	f2(m)
	if m.form == nil {
		t.Fatal("the form is gone")
	}
	view := plain(m)
	if !strings.Contains(view, "host is not a name (use a host name)") || !strings.Contains(view, "not an email address") {
		t.Errorf("no problems under the fields:\n%s", view)
	}
	if fb.count("channel-add team-mail") != 1 {
		t.Errorf("calls = %v", fb.log())
	}
	// An existing name is reported under the name.
	fb.lib.addErr = &control.Error{Code: control.CodeExists}
	f2(m)
	if !strings.Contains(plain(m), "A channel of that name exists.") {
		t.Errorf("no exists note:\n%s", plain(m))
	}
}

func TestRemoveChannelAsksThenRemoves(t *testing.T) {
	fb := libFake()
	fb.session = true
	m := appModel(t, fb, 80, 24)
	send(m, "5", "right", "d")
	if fb.count("channel-remove chat-ops") != 0 {
		t.Fatal("removed without the answer")
	}
	send(m, "n")
	if fb.count("channel-remove chat-ops") != 0 {
		t.Fatal("removed after n")
	}
	send(m, "d", "y")
	if fb.count("channel-remove chat-ops") != 1 || !strings.Contains(m.status, "Channel removed") {
		t.Errorf("calls %v, status %q", fb.log(), m.status)
	}
}

func TestChannelFieldsAreMaskedAndSanitised(t *testing.T) {
	fb := libFake()
	fb.lib.channels[1].URL = "https://hooks.example.test/" + secretValue + "\x1b[2J\x07"
	m := appModel(t, fb, 120, 40)
	m.redactor.Add(secretValue)
	send(m, "5", "right", "down")
	for range 3 {
		view := m.render()
		if strings.Contains(view, secretValue) || strings.Contains(view, "\x1b[2J") || strings.Contains(view, "\x07") {
			t.Fatalf("a secret or a control sequence is drawn:\n%q", view)
		}
		send(m, "enter", "right")
		send(m, "esc")
	}
	send(m, "c")
	if m.status == "" {
		t.Error("nothing copied")
	}
}

func TestTemplatesListAndDetailShowTheYAML(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		fb := libFake()
		fb.lib.files["compose-update"] += "# " + secretValue + "\x1b[31m\n"
		m := appModel(t, fb, size[0], size[1])
		m.redactor.Add(secretValue)
		send(m, "5", "right", "right")
		view := plain(m)
		for _, want := range []string{"Templates (3)", "compose-update", "builtin", "custom"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: the list lacks %q:\n%s", size[0], size[1], want, view)
			}
		}
		send(m, "enter", "right")
		view = plain(m)
		raw := m.render()
		if !strings.Contains(view, "run: compose pull") || !strings.Contains(view, "Details") {
			t.Errorf("%dx%d: no YAML:\n%s", size[0], size[1], view)
		}
		if strings.Contains(raw, secretValue) || strings.Contains(raw, "\x1b[31m") {
			t.Errorf("%dx%d: secret or sequence drawn:\n%q", size[0], size[1], raw)
		}
		assertViewFits(t, view, size[0], size[1])
	}
}

func TestDeriveTemplateCallsOnceAndShowsRefusals(t *testing.T) {
	fb := libFake()
	m := appModel(t, fb, 80, 24)
	send(m, "5", "right", "right", "p")
	if m.form == nil {
		t.Fatal("no form")
	}
	typed(m, "team-update")
	f2(m)
	if m.ov == nil || fb.count("template-derive team-update") != 0 {
		t.Fatal("the passphrase was not asked")
	}
	typed(m, testPass)
	send(m, "enter")
	if fb.count("template-derive team-update") != 1 || len(fb.lib.derived) != 1 {
		t.Fatalf("calls = %v", fb.log())
	}
	p := fb.lib.derived[0]
	if p.From != "compose-update" || p.Name != "team-update" || p.Description != "" {
		t.Errorf("params = %+v", p)
	}
	if cur, _ := m.lib.tmpls.page.current(); cur != "team-update" || !strings.Contains(m.status, "team-update") {
		t.Errorf("current %q, status %q", cur, m.status)
	}

	fb.lib.addErr = &control.Error{Code: control.CodeExists}
	send(m, "p")
	typed(m, "x")
	f2(m)
	if m.form == nil || !strings.Contains(plain(m), "A template of that name exists.") {
		t.Errorf("exists not shown:\n%s", plain(m))
	}
	send(m, "esc")
	fb.lib.addErr = &control.Error{Code: control.CodeInvalid, Problems: []control.Problem{{Field: "name", Code: "bad-name", Message: "the name is not valid"}}}
	send(m, "p")
	typed(m, "Bad Name")
	f2(m)
	if !strings.Contains(plain(m), "the name is not valid") {
		t.Errorf("problem not shown:\n%s", plain(m))
	}
}

func TestActivityLoadsEntriesAndShowsAnIntactChain(t *testing.T) {
	utcClock(t)
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		fb := libFake()
		m := appModel(t, fb, size[0], size[1])
		send(m, "3")
		view := plain(m)
		for _, want := range []string{"Log (12)", "● Chain intact, 52 entries", "signatures checked", "2026-10-28 12:00:00", "config.read", "demo-app"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: lacks %q:\n%s", size[0], size[1], want, view)
			}
		}
		if fb.count("log") != 1 || fb.count("log-verify") != 1 {
			t.Errorf("calls = %v", fb.log())
		}
		assertViewFits(t, view, size[0], size[1])
	}
}

func TestActivityShowsABrokenChainWithSymbolAndText(t *testing.T) {
	utcClock(t)
	fb := libFake()
	fb.lib.verify = brokenChain()
	m := appModel(t, fb, 80, 24)
	send(m, "3")
	view := plain(m)
	if !strings.Contains(view, "⚠ Chain broken · 2026-10-27 entry 31 · hash mismatch") || strings.Contains(view, "Chain intact") {
		t.Errorf("no break shown:\n%s", view)
	}
	assertViewFits(t, view, 80, 24)
	fb.lib.verify = intactChain()
	send(m, "t")
	if fb.count("log-verify") != 2 || !strings.Contains(plain(m), "● Chain intact") {
		t.Errorf("calls %v:\n%s", fb.log(), plain(m))
	}
}

func TestActivityCountsDocumentedDamage(t *testing.T) {
	utcClock(t)
	fb := libFake()
	fb.lib.verify = intactChain()
	fb.lib.verify.Days[1].Damaged = 1
	m := appModel(t, fb, 120, 40)
	send(m, "3")
	if view := plain(m); !strings.Contains(view, "● Chain intact, 52 entries · unreadable lines documented: 1 · signatures checked") {
		t.Errorf("no documented damage shown:\n%s", view)
	}
	useLanguage(t, "de")
	send(m, "t")
	if view := plain(m); !strings.Contains(view, "unlesbare Zeilen dokumentiert: 1") {
		t.Errorf("no German documented damage shown:\n%s", view)
	}
}

func TestActivityDoesNotPollTheLog(t *testing.T) {
	fb := libFake()
	m := appModel(t, fb, 80, 24)
	send(m, "3")
	logs, checks := fb.count("log"), fb.count("log-verify")
	clock := clockModel(m, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
	for range 5 {
		*clock = clock.Add(2 * pollEvery)
		_, cmd := m.Update(tickMsg{})
		drain(m, cmd)
	}
	if fb.count("log") != logs || fb.count("log-verify") != checks {
		t.Errorf("the log was read on a timer: %v", fb.log())
	}
}

func TestActivityFilterSetsTheParametersAndReloads(t *testing.T) {
	utcClock(t)
	fb := libFake()
	m := appModel(t, fb, 80, 24)
	now := time.Date(2026, 10, 28, 15, 30, 0, 0, time.UTC)
	clockModel(m, now)
	send(m, "3", "f")
	if m.form == nil {
		t.Fatal("no filter form")
	}
	// Period: 7 days; project: demo-app; action: the first one; result: failure.
	send(m, "right", "right", "right", "tab", "right", "tab", "right", "tab", "right", "right")
	f2(m)
	last := fb.lib.logCalls[len(fb.lib.logCalls)-1]
	want := control.LogListParams{From: now.AddDate(0, 0, -7).Format(time.RFC3339), Project: "demo-app", Action: "config.apply",
		Result: control.LogResultFailure, Limit: logPageSize}
	if last != want {
		t.Errorf("params = %+v, want %+v", last, want)
	}
	if view := plain(m); !strings.Contains(view, "Log · 7 days · demo-app · config.apply · failure") {
		t.Errorf("the filters are not in the title:\n%s", view)
	}
	// The period "today" starts at midnight of the local day.
	send(m, "f", "left")
	f2(m)
	last = fb.lib.logCalls[len(fb.lib.logCalls)-1]
	if last.From != "2026-10-28T00:00:00Z" {
		t.Errorf("today = %q", last.From)
	}
}

func TestActivityLoadsFurtherPagesAtTheEnd(t *testing.T) {
	utcClock(t)
	fb := libFake()
	fb.lib.entries = testLogEntries(250)
	m := appModel(t, fb, 80, 24)
	send(m, "3")
	if got := len(m.act.order); got != logPageSize || m.act.next == "" {
		t.Fatalf("first page: %d entries, next %q", got, m.act.next)
	}
	send(m, "end")
	if got := len(m.act.order); got != 2*logPageSize {
		t.Fatalf("second page: %d entries", got)
	}
	if last := fb.lib.logCalls[len(fb.lib.logCalls)-1]; last.After != "100" {
		t.Errorf("after = %q", last.After)
	}
	send(m, "end", "end")
	if got := len(m.act.order); got != 250 || m.act.next != "" {
		t.Errorf("end: %d entries, next %q", got, m.act.next)
	}
	calls := fb.count("log")
	send(m, "end", "end")
	if fb.count("log") != calls {
		t.Error("the log is read past its end")
	}
}

func TestActivityDrawsNoControlSequenceOfAnEntry(t *testing.T) {
	utcClock(t)
	fb := libFake()
	fb.lib.entries[0].Action = "config.read\x1b]52;c;ZXZpbA==\x07"
	fb.lib.entries[0].Project = "demo\x1b[2Japp"
	fb.lib.entries[0].ErrorCode = "x\x1b[31m"
	m := appModel(t, fb, 120, 40)
	m.redactor.Add(secretValue)
	fb.lib.entries[1].Project = secretValue
	send(m, "3")
	for range 3 {
		raw := m.render()
		for _, bad := range []string{"\x1b]52", "\x1b[2J", "\x1b[31m", "\x07", secretValue} {
			if strings.Contains(raw, bad) {
				t.Fatalf("%q is drawn:\n%q", bad, raw)
			}
		}
		send(m, "enter", "right")
		send(m, "esc")
	}
	send(m, "ctrl+r")
	send(m, "c")
	if m.status == "" {
		t.Error("nothing copied")
	}
}

func TestActivityKnowsOnlyActionsTheServiceLogs(t *testing.T) {
	for _, a := range logActions {
		if !auditlog.KnownOperation(a) {
			t.Errorf("%q is no action of the log", a)
		}
	}
}

func TestLibraryAndActivityTextsAreTranslated(t *testing.T) {
	utcClock(t)
	eachLanguage(t, func(t *testing.T) {
		fb := libFake()
		fb.lib.verify = brokenChain()
		m := appModel(t, fb, 120, 40)
		for _, keys := range [][]string{{"3"}, {"3", "f"}, {"5", "right"}, {"5", "right", "n"}, {"5", "right", "right"}, {"5", "right", "right", "p"}} {
			m2 := appModel(t, fb, 120, 40)
			send(m2, keys...)
			assertViewFits(t, plain(m2), 120, 40)
		}
		send(m, "?", "left")
		for range helpTopics {
			send(m, "left")
		}
	})
}

func TestEveryFooterGroupOfThePagesIsTranslated(t *testing.T) {
	utcClock(t)
	useLanguage(t, "de")
	fb := libFake()
	for _, keys := range [][]string{{"3"}, {"5", "right"}, {"5", "right", "right"}} {
		m := appModel(t, fb, 120, 40)
		send(m, keys...)
		for _, group := range strings.Split(m.areaKeys(), " · ") {
			if strings.HasPrefix(group, "{") || group == "1-6 area" {
				continue
			}
			if _, ok := textDE[footerID(group)]; !ok {
				t.Errorf("footer group %q has no German text", group)
			}
		}
	}
}
