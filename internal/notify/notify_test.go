package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

var demoEvent = Event{
	Type: EventDeployFailed, Project: "demo-app", FailedStep: "pull",
	Images: []ImageVersion{{Image: "app", Current: "1.2.0", Previous: "1.1.0", Target: "1.3.0"}},
	Error:  "pull failed",
}

type captured struct {
	header http.Header
	body   []byte
}

// captureServer records the requests it gets and answers with status.
func captureServer(t *testing.T, status int) (*httptest.Server, func() []captured) {
	t.Helper()
	var mu sync.Mutex
	var got []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, captured{header: r.Header.Clone(), body: b})
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "response body")
	}))
	t.Cleanup(srv.Close)
	return srv, func() []captured {
		mu.Lock()
		defer mu.Unlock()
		return append([]captured(nil), got...)
	}
}

func TestWebhookPostsSignedJSON(t *testing.T) {
	srv, got := captureServer(t, 200)
	chans := &fakeChannels{state: approved(map[string]string{"hook-key": "id-1"},
		config.Channel{Name: "ops", Type: config.ChannelWebhook, URL: srv.URL, Credential: "hook-key"})}
	creds := &fakeCreds{entries: map[string]vault.Entry{"id-1": secretEntry(demoHookKey)}}
	n := newNotifier(chans, creds, nil)
	if err := n.Send(context.Background(), "ops", demoEvent); err != nil {
		t.Fatal(err)
	}
	reqs := got()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	r := reqs[0]
	var p map[string]any
	if err := json.Unmarshal(r.body, &p); err != nil {
		t.Fatal(err)
	}
	if p["event"] != "deploy.failed" || p["host"] != "demo-host" || p["project"] != "demo-app" ||
		p["failed_step"] != "pull" || p["error"] != "pull failed" || p["time"] != "2026-01-02T03:04:05Z" {
		t.Errorf("payload = %v", p)
	}
	if v := p["versions"].([]any)[0].(map[string]any); v["current"] != "1.2.0" || v["previous"] != "1.1.0" || v["target"] != "1.3.0" {
		t.Errorf("versions = %v", v)
	}
	ts := r.header.Get(HeaderTimestamp)
	if ts != "1767323045" {
		t.Errorf("timestamp = %q", ts)
	}
	mac := hmac.New(sha256.New, []byte(demoHookKey))
	mac.Write([]byte(ts + "."))
	mac.Write(r.body)
	if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); r.header.Get(HeaderSignature) != want {
		t.Errorf("signature = %q, want %q", r.header.Get(HeaderSignature), want)
	}
	if r.header.Get(HeaderEvent) != "deploy.failed" || r.header.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", r.header)
	}
	if strings.Contains(string(r.body), demoHookKey) {
		t.Error("the signing key is in the body")
	}
}

func TestWebhookWithoutCredentialIsUnsigned(t *testing.T) {
	srv, got := captureServer(t, 200)
	chans := &fakeChannels{state: approved(nil, config.Channel{Name: "ops", Type: config.ChannelWebhook, URL: srv.URL})}
	n := newNotifier(chans, &fakeCreds{}, nil)
	if err := n.Send(context.Background(), "ops", demoEvent); err != nil {
		t.Fatal(err)
	}
	if h := got()[0].header; h.Get(HeaderSignature) != "" || h.Get(HeaderTimestamp) != "" {
		t.Errorf("headers = %v", h)
	}
}

func TestTelegramSendsMessage(t *testing.T) {
	srv, got := captureServer(t, 200)
	chans := &fakeChannels{state: approved(map[string]string{"bot": "id-1"},
		config.Channel{Name: "tg", Type: config.ChannelTelegram, ChatID: "-100123", Credential: "bot"})}
	creds := &fakeCreds{entries: map[string]vault.Entry{"id-1": secretEntry(demoBotToken)}}
	n := newNotifier(chans, creds, nil, WithTelegramBaseURL(srv.URL))
	if err := n.Send(context.Background(), "tg", demoEvent); err != nil {
		t.Fatal(err)
	}
	r := got()[0]
	var p map[string]string
	if err := json.Unmarshal(r.body, &p); err != nil {
		t.Fatal(err)
	}
	if p["chat_id"] != "-100123" || !strings.Contains(p["text"], "[demo-host] deploy.failed demo-app") ||
		!strings.Contains(p["text"], "Failed step: pull") || !strings.Contains(p["text"], "app: current 1.2.0, previous 1.1.0, target 1.3.0") {
		t.Errorf("body = %v", p)
	}
}

func TestTelegramRequestPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gotPath = r.URL.Path }))
	defer srv.Close()
	chans := &fakeChannels{state: approved(map[string]string{"bot": "id-1"},
		config.Channel{Name: "tg", Type: config.ChannelTelegram, ChatID: "1", Credential: "bot"})}
	creds := &fakeCreds{entries: map[string]vault.Entry{"id-1": secretEntry(demoBotToken)}}
	n := newNotifier(chans, creds, nil, WithTelegramBaseURL(srv.URL))
	if err := n.Send(context.Background(), "tg", demoEvent); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/bot"+demoBotToken+"/sendMessage" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestSlackPostsToCredentialURL(t *testing.T) {
	srv, got := captureServer(t, 200)
	chans := &fakeChannels{state: approved(map[string]string{"slack": "id-1"},
		config.Channel{Name: "chat", Type: config.ChannelSlack, Credential: "slack"})}
	creds := &fakeCreds{entries: map[string]vault.Entry{"id-1": secretEntry(srv.URL + "/services/demo-hook-not-real")}}
	n := newNotifier(chans, creds, nil)
	if err := n.Send(context.Background(), "chat", demoEvent); err != nil {
		t.Fatal(err)
	}
	var p map[string]string
	if err := json.Unmarshal(got()[0].body, &p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p["text"], "deploy.failed") {
		t.Errorf("text = %q", p["text"])
	}
}

func TestSecretAddressesNeedHTTPS(t *testing.T) {
	chans := &fakeChannels{state: approved(map[string]string{"slack": "id-1"},
		config.Channel{Name: "chat", Type: config.ChannelSlack, Credential: "slack"})}
	creds := &fakeCreds{entries: map[string]vault.Entry{"id-1": secretEntry("http://hooks.example.com/services/demo-not-real")}}
	n := newNotifier(chans, creds, nil)
	err := n.Send(context.Background(), "chat", demoEvent)
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err = %v", err)
	}
}

func smtpChannel(s *smtpServer, security string) config.Channel {
	return config.Channel{Name: "mail", Type: config.ChannelSMTP, Host: "127.0.0.1", Port: s.port,
		From: "sysmin@example.com", To: []string{"ops@example.com", "dev@example.com"}, Security: security,
		Credential: "mailer"}
}

func smtpNotifier(s *smtpServer, ch config.Channel, a Auditor) *Notifier {
	chans := &fakeChannels{state: approved(map[string]string{"mailer": "id-1"}, ch)}
	creds := &fakeCreds{entries: map[string]vault.Entry{"id-1": smtpEntry(demoSMTPUser, demoSMTPPass)}}
	return newNotifier(chans, creds, a, WithTLSConfig(&tls.Config{RootCAs: s.rootCAs}))
}

type smtpSeen struct {
	user, pass        string
	authTLS, authSeen bool
	plainAuth         bool
	from, data        string
	rcpt              []string
}

func (s *smtpServer) snapshot() smtpSeen {
	s.mu.Lock()
	defer s.mu.Unlock()
	return smtpSeen{user: s.user, pass: s.pass, authTLS: s.authTLS, authSeen: s.authSeen, from: s.from,
		rcpt: append([]string(nil), s.rcpt...), data: s.data, plainAuth: s.plainAuth}
}

func TestSMTPStartTLSAuthenticatesAndSends(t *testing.T) {
	s := startSMTP(t, false, true, "PLAIN LOGIN")
	n := smtpNotifier(s, smtpChannel(s, "starttls"), nil)
	if err := n.Send(context.Background(), "mail", demoEvent); err != nil {
		t.Fatal(err)
	}
	g := s.snapshot()
	if !g.authSeen || !g.authTLS || !g.plainAuth || g.user != demoSMTPUser || g.pass != demoSMTPPass {
		t.Errorf("auth = %+v", g)
	}
	if !strings.Contains(g.from, "sysmin@example.com") || len(g.rcpt) != 2 {
		t.Errorf("envelope from=%q rcpt=%v", g.from, g.rcpt)
	}
	for _, want := range []string{"Subject: [demo-host] deploy.failed demo-app", "From: <sysmin@example.com>", "To: "} {
		if !strings.Contains(g.data, want) {
			t.Errorf("message lacks %q:\n%s", want, g.data)
		}
	}
	if strings.Contains(g.data, demoSMTPPass) {
		t.Error("password in message")
	}
}

func TestSMTPImplicitTLSWithLoginMechanism(t *testing.T) {
	s := startSMTP(t, true, false, "LOGIN")
	n := smtpNotifier(s, smtpChannel(s, "tls"), nil)
	if err := n.Send(context.Background(), "mail", demoEvent); err != nil {
		t.Fatal(err)
	}
	g := s.snapshot()
	if !g.authTLS || g.plainAuth || g.user != demoSMTPUser || g.pass != demoSMTPPass {
		t.Errorf("auth = %+v", g)
	}
}

func TestSMTPRefusesWithoutSTARTTLSAndSendsNoLogin(t *testing.T) {
	s := startSMTP(t, false, false, "PLAIN")
	n := smtpNotifier(s, smtpChannel(s, "starttls"), nil)
	err := n.Send(context.Background(), "mail", demoEvent)
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err = %v", err)
	}
	if g := s.snapshot(); g.authSeen {
		t.Error("login attempted on a plain connection")
	}
}

func TestSMTPRejectsUntrustedCertificate(t *testing.T) {
	s := startSMTP(t, false, true, "PLAIN")
	chans := &fakeChannels{state: approved(map[string]string{"mailer": "id-1"}, smtpChannel(s, "starttls"))}
	creds := &fakeCreds{entries: map[string]vault.Entry{"id-1": smtpEntry(demoSMTPUser, demoSMTPPass)}}
	n := newNotifier(chans, creds, nil) // default roots do not know the test certificate
	if err := n.Send(context.Background(), "mail", demoEvent); err == nil {
		t.Fatal("expected a certificate error")
	}
	if g := s.snapshot(); g.authSeen {
		t.Error("login attempted over an untrusted connection")
	}
}

func TestSMTPWrongLoginIsNotRetriedAndNotLeaked(t *testing.T) {
	s := startSMTP(t, false, true, "PLAIN")
	a := &recordingAudit{}
	chans := &fakeChannels{state: approved(map[string]string{"mailer": "id-1"}, smtpChannel(s, "starttls"))}
	creds := &fakeCreds{entries: map[string]vault.Entry{"id-1": smtpEntry(demoSMTPUser, "demo-wrong-password")}}
	n := newNotifier(chans, creds, a, WithTLSConfig(&tls.Config{RootCAs: s.rootCAs}))
	n.Notify(demoEvent)
	n.Wait()
	es := a.all()
	if len(es) != 1 || es[0].Result != "failure" || es[0].ErrorCode != "send-failed" {
		t.Fatalf("entries = %+v", es)
	}
	err := n.Send(context.Background(), "mail", demoEvent)
	if err == nil || strings.Contains(err.Error(), "demo-wrong-password") {
		t.Errorf("err = %v", err)
	}
}

func TestFailureIsRetriedThreeTimesAndLogged(t *testing.T) {
	srv, got := captureServer(t, 500)
	a := &recordingAudit{}
	chans := &fakeChannels{state: approved(nil, config.Channel{Name: "ops", Type: config.ChannelWebhook, URL: srv.URL})}
	n := newNotifier(chans, &fakeCreds{}, a)
	n.Notify(demoEvent)
	n.Wait()
	if c := len(got()); c != 3 {
		t.Errorf("attempts = %d, want 3", c)
	}
	es := a.all()
	if len(es) != 1 || es[0].Action != "notify.send" || es[0].Project != "ops" || es[0].Result != "failure" || es[0].ErrorCode != "send-failed" {
		t.Errorf("entries = %+v", es)
	}
}

func TestSuccessIsLoggedOnceAndClientErrorIsNotRetried(t *testing.T) {
	ok, gotOK := captureServer(t, 204)
	bad, gotBad := captureServer(t, 404)
	a := &recordingAudit{}
	chans := &fakeChannels{state: approved(nil,
		config.Channel{Name: "good", Type: config.ChannelWebhook, URL: ok.URL},
		config.Channel{Name: "bad", Type: config.ChannelWebhook, URL: bad.URL})}
	n := newNotifier(chans, &fakeCreds{}, a)
	n.Notify(demoEvent)
	n.Wait()
	if len(gotOK()) != 1 || len(gotBad()) != 1 {
		t.Errorf("requests ok=%d bad=%d", len(gotOK()), len(gotBad()))
	}
	results := map[string]string{}
	for _, e := range a.all() {
		results[e.Project] = e.Result
	}
	if len(results) != 2 || results["good"] != "success" || results["bad"] != "failure" {
		t.Errorf("results = %v", results)
	}
}

func TestNotifyDoesNotBlockTheCaller(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	chans := &fakeChannels{state: approved(nil, config.Channel{Name: "ops", Type: config.ChannelWebhook, URL: srv.URL})}
	n := newNotifier(chans, &fakeCreds{}, nil, WithTimeout(30*time.Millisecond))
	start := time.Now()
	n.Notify(demoEvent)
	if d := time.Since(start); d > 20*time.Millisecond {
		t.Errorf("Notify blocked for %v", d)
	}
	n.Wait()
}

func TestCloseStopsDeliveries(t *testing.T) {
	chans := &fakeChannels{state: approved(nil, config.Channel{Name: "ops", Type: config.ChannelWebhook, URL: "http://127.0.0.1:1"})}
	a := &recordingAudit{}
	n := newNotifier(chans, &fakeCreds{}, a)
	n.Close()
	n.Notify(demoEvent)
	n.Wait()
	if len(a.all()) != 0 {
		t.Errorf("entries after Close = %+v", a.all())
	}
}

func lockedSetup(t *testing.T) (*Notifier, *fakeChannels, *fakeCreds, *recordingAudit, func() []captured) {
	t.Helper()
	srv, got := captureServer(t, 200)
	chans := &fakeChannels{state: approved(map[string]string{"hook-key": "id-1", "bot": "id-2"},
		config.Channel{Name: "plain", Type: config.ChannelWebhook, URL: srv.URL},
		config.Channel{Name: "signed", Type: config.ChannelWebhook, URL: srv.URL, Credential: "hook-key"},
		config.Channel{Name: "tg", Type: config.ChannelTelegram, ChatID: "1", Credential: "bot"})}
	creds := &fakeCreds{entries: map[string]vault.Entry{"id-1": secretEntry(demoHookKey), "id-2": secretEntry(demoBotToken)}}
	a := &recordingAudit{}
	n := newNotifier(chans, creds, a, WithTelegramBaseURL(srv.URL))
	return n, chans, creds, a, got
}

func TestLockedVaultSendsOnlyVaultLockedOverChannelsWithoutSecret(t *testing.T) {
	n, chans, creds, a, got := lockedSetup(t)
	if err := n.Refresh(); err != nil {
		t.Fatal(err)
	}
	creds.locked = true
	chans.setErr(vault.ErrLocked)
	n.Notify(Event{Type: EventVaultLocked})
	n.Wait()
	reqs := got()
	if len(reqs) != 1 || reqs[0].header.Get(HeaderSignature) != "" {
		t.Fatalf("requests = %d", len(reqs))
	}
	res := map[string]string{}
	for _, e := range a.all() {
		res[e.Project] = e.Result + "/" + e.ErrorCode
	}
	if res["plain"] != "success/" || res["signed"] != "failure/needs-secret" || res["tg"] != "failure/needs-secret" {
		t.Errorf("results = %v", res)
	}
}

func TestLockedVaultSkipsOtherEvents(t *testing.T) {
	n, chans, creds, a, got := lockedSetup(t)
	if err := n.Refresh(); err != nil {
		t.Fatal(err)
	}
	creds.locked = true
	chans.setErr(vault.ErrLocked)
	n.Notify(demoEvent)
	n.Wait()
	if len(got()) != 0 {
		t.Error("an event other than vault.locked was sent")
	}
	if es := a.all(); len(es) != 1 || es[0].ErrorCode != "vault-locked" {
		t.Errorf("entries = %+v", es)
	}
}

func TestLockedVaultWithoutVerifiedStateSendsNothing(t *testing.T) {
	n, chans, _, a, got := lockedSetup(t)
	chans.setErr(vault.ErrLocked)
	n.Notify(Event{Type: EventVaultLocked})
	n.Wait()
	if len(got()) != 0 {
		t.Error("sent without a verified state")
	}
	if es := a.all(); len(es) != 1 || es[0].ErrorCode != "no-approved-state" {
		t.Errorf("entries = %+v", es)
	}
}

func TestVaultLockedEventSkipsChannelsWithSecretEvenWhenUnlocked(t *testing.T) {
	n, _, _, a, got := lockedSetup(t)
	n.Notify(Event{Type: EventVaultLocked})
	n.Wait()
	if len(got()) != 1 {
		t.Errorf("requests = %d, want 1", len(got()))
	}
	skipped := 0
	for _, e := range a.all() {
		if e.ErrorCode == "needs-secret" {
			skipped++
		}
	}
	if skipped != 2 {
		t.Errorf("skipped = %d, want 2", skipped)
	}
}

func TestLostApprovalDropsTheLockedFallback(t *testing.T) {
	n, chans, _, _, got := lockedSetup(t)
	if err := n.Refresh(); err != nil {
		t.Fatal(err)
	}
	chans.setErr(approval.ErrNotApproved)
	n.Notify(Event{Type: EventVaultLocked})
	n.Wait()
	chans.setErr(vault.ErrLocked)
	n.Notify(Event{Type: EventVaultLocked})
	n.Wait()
	if len(got()) != 0 {
		t.Error("a state that is no longer approved was used")
	}
}

func TestNoSecretInMessageErrorOrLog(t *testing.T) {
	var seen []byte
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, b...)
		mu.Unlock()
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "echo "+demoBotToken)
	}))
	base := srv.URL
	chans := &fakeChannels{state: approved(map[string]string{"bot": "id-1"},
		config.Channel{Name: "tg", Type: config.ChannelTelegram, ChatID: "1", Credential: "bot"})}
	creds := &fakeCreds{entries: map[string]vault.Entry{"id-1": secretEntry(demoBotToken)}}
	a := &recordingAudit{}
	n := newNotifier(chans, creds, a, WithTelegramBaseURL(base))

	ev := demoEvent
	ev.Error = "login with " + demoBotToken + " failed"
	err := n.Send(context.Background(), "tg", ev)
	if err == nil {
		t.Fatal("expected an error")
	}
	srv.Close() // now the connection is refused and net/http puts the URL into its error
	n.Notify(ev)
	n.Wait()
	err2 := n.Send(context.Background(), "tg", ev)
	if err2 == nil {
		t.Fatal("expected an error")
	}
	mu.Lock()
	defer mu.Unlock()
	for name, text := range map[string]string{"error": err.Error(), "refused error": err2.Error(),
		"message": string(seen), "log": a.dump()} {
		if strings.Contains(text, demoBotToken) {
			t.Errorf("%s contains the token: %s", name, text)
		}
	}
	if !strings.Contains(string(seen), "[redacted]") {
		t.Errorf("failure text not redacted: %s", seen)
	}
}

func TestErrorTextIsCleanedAndShortened(t *testing.T) {
	srv, got := captureServer(t, 200)
	chans := &fakeChannels{state: approved(nil, config.Channel{Name: "ops", Type: config.ChannelWebhook, URL: srv.URL})}
	n := newNotifier(chans, &fakeCreds{}, nil)
	ev := demoEvent
	ev.Error = "line one\nline two\x00" + strings.Repeat("x", 2000)
	if err := n.Send(context.Background(), "ops", ev); err != nil {
		t.Fatal(err)
	}
	var p payload
	if err := json.Unmarshal(got()[0].body, &p); err != nil {
		t.Fatal(err)
	}
	if len([]rune(p.Error)) != maxErrorRunes || strings.ContainsAny(p.Error, "\n\x00") || !strings.HasSuffix(p.Error, "...") {
		t.Errorf("error = %q", p.Error)
	}
}

func TestResponseSizeIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 1000; i++ {
			if _, err := w.Write(make([]byte, 1<<10)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()
	chans := &fakeChannels{state: approved(nil, config.Channel{Name: "ops", Type: config.ChannelWebhook, URL: srv.URL})}
	n := newNotifier(chans, &fakeCreds{}, nil)
	if err := n.Send(context.Background(), "ops", demoEvent); err != nil {
		t.Fatal(err)
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	target, got := captureServer(t, 200)
	srv := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusTemporaryRedirect))
	defer srv.Close()
	chans := &fakeChannels{state: approved(nil, config.Channel{Name: "ops", Type: config.ChannelWebhook, URL: srv.URL})}
	n := newNotifier(chans, &fakeCreds{}, nil)
	if err := n.Send(context.Background(), "ops", demoEvent); err == nil {
		t.Error("a redirect counted as success")
	}
	if len(got()) != 0 {
		t.Error("the redirect was followed")
	}
}

func TestSendErrors(t *testing.T) {
	n, chans, creds, _, _ := lockedSetup(t)
	if err := n.Send(context.Background(), "nope", demoEvent); !errors.Is(err, ErrUnknownChannel) {
		t.Errorf("unknown channel: %v", err)
	}
	if err := n.Send(context.Background(), "plain", Event{Type: "bogus"}); err == nil {
		t.Error("invalid event accepted")
	}
	creds.locked = true
	if err := n.Send(context.Background(), "signed", demoEvent); !errors.Is(err, ErrNeedsSecret) {
		t.Errorf("locked vault: %v", err)
	}
	chans.setErr(vault.ErrLocked)
	if err := n.Send(context.Background(), "plain", demoEvent); !errors.Is(err, ErrNeedsSecret) {
		t.Errorf("locked vault, state from memory: %v", err)
	}
}

func TestOnlyApprovedChannelsAreUsed(t *testing.T) {
	approvedSrv, gotApproved := captureServer(t, 200)
	unapprovedSrv, gotUnapproved := captureServer(t, 200)
	e := newApprovalEnv(t)
	e.writeSettings("channels:\n  - name: ops\n    type: webhook\n    url: " + approvedSrv.URL + "\n")
	if err := e.mgr.Approve(approval.ChannelsName, ""); err != nil {
		t.Fatal(err)
	}
	// Added by hand, never approved.
	e.writeSettings("channels:\n  - name: ops\n    type: webhook\n    url: " + approvedSrv.URL + "\n" +
		"  - name: sneaky\n    type: webhook\n    url: " + unapprovedSrv.URL + "\n")

	a := &recordingAudit{}
	n := newNotifier(e.mgr, e.vault, a)
	n.Notify(demoEvent)
	n.Wait()
	if len(gotApproved()) != 1 || len(gotUnapproved()) != 0 {
		t.Errorf("approved=%d unapproved=%d", len(gotApproved()), len(gotUnapproved()))
	}
	if err := n.Send(context.Background(), "sneaky", demoEvent); !errors.Is(err, ErrUnknownChannel) {
		t.Errorf("Send to an unapproved channel: %v", err)
	}
	n.Notify(demoEvent, "sneaky")
	n.Wait()
	if len(gotUnapproved()) != 0 {
		t.Error("an unapproved channel was addressed by name")
	}
}

func TestApprovedCredentialIDIsUsedNotTheName(t *testing.T) {
	srv, got := captureServer(t, 200)
	e := newApprovalEnv(t)
	e.addSecret("hook-key", demoHookKey)
	e.writeSettings("channels:\n  - name: ops\n    type: webhook\n    url: " + srv.URL + "\n    credential: hook-key\n")
	if err := e.mgr.Approve(approval.ChannelsName, ""); err != nil {
		t.Fatal(err)
	}
	// Replace the credential under the same name: the approved entry stays in use.
	newID, err := e.vault.Replace("hook-key", map[string]string{vault.FieldValue: "demo-other-key-not-real"})
	if err != nil || newID == e.ids["hook-key"] {
		t.Fatalf("Replace() = %q, %v", newID, err)
	}
	n := newNotifier(e.mgr, e.vault, nil)
	if err := n.Send(context.Background(), "ops", demoEvent); err != nil {
		t.Fatal(err)
	}
	r := got()[0]
	mac := hmac.New(sha256.New, []byte(demoHookKey))
	mac.Write([]byte(r.header.Get(HeaderTimestamp) + "."))
	mac.Write(r.body)
	if r.header.Get(HeaderSignature) != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Error("the signature was not made with the approved credential")
	}
}
