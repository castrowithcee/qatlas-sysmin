package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

const (
	secretA = "example-secret-a"
	secretB = "example-secret-b"
)

type harness struct {
	h        *Handler
	mu       sync.Mutex
	triggers []string
	events   []Event
	targets  map[string]Target
	locked   bool
	now      time.Time
}

func newHarness(t *testing.T, mod func(*Config)) *harness {
	t.Helper()
	h := &harness{now: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), targets: map[string]Target{
		"gh":    {Kind: config.WebhookGitHub, Secret: []byte(secretA)},
		"gl":    {Kind: config.WebhookGitLab, Secret: []byte(secretA)},
		"gt":    {Kind: config.WebhookGitea, Secret: []byte(secretA)},
		"mac":   {Kind: config.WebhookHMAC, Secret: []byte(secretA)},
		"other": {Kind: config.WebhookGitHub, Secret: []byte(secretB)},
	}}
	cfg := Config{
		Lookup: func(p string) (Target, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.locked {
				return Target{}, ErrUnavailable
			}
			if tg, ok := h.targets[p]; ok {
				return tg, nil
			}
			return Target{}, ErrNoWebhook
		},
		Trigger: func(p string) bool { h.mu.Lock(); h.triggers = append(h.triggers, p); h.mu.Unlock(); return true },
		Record:  func(e Event) { h.mu.Lock(); h.events = append(h.events, e); h.mu.Unlock() },
		Now:     func() time.Time { h.mu.Lock(); defer h.mu.Unlock(); return h.now },
	}
	if mod != nil {
		mod(&cfg)
	}
	h.h = New(cfg)
	return h
}

func sign(secret, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil))
}

// post sends body to the handler; set adds the signature header.
func (h *harness) post(project, body string, set func(r *http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, PathPrefix+project, strings.NewReader(body))
	if set != nil {
		set(r)
	}
	w := httptest.NewRecorder()
	h.h.ServeHTTP(w, r)
	return w
}

func (h *harness) count() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.triggers) }

func TestValidSignatureTriggersOneCheckPerProvider(t *testing.T) {
	body := `{"ref":"example"}`
	cases := map[string]func(r *http.Request){
		"gh":  func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "sha256="+sign(secretA, body)) },
		"gl":  func(r *http.Request) { r.Header.Set("X-Gitlab-Token", secretA) },
		"gt":  func(r *http.Request) { r.Header.Set("X-Gitea-Signature", sign(secretA, body)) },
		"mac": func(r *http.Request) { r.Header.Set("X-Sysmin-Signature", "sha256="+sign(secretA, body)) },
	}
	for project, set := range cases {
		t.Run(project, func(t *testing.T) {
			h := newHarness(t, nil)
			w := h.post(project, body, set)
			if w.Code != http.StatusAccepted || w.Body.Len() != 0 {
				t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
			}
			if h.count() != 1 || h.triggers[0] != project {
				t.Errorf("triggers = %v", h.triggers)
			}
			if len(h.events) != 1 || h.events[0].Project != project || h.events[0].Code != "" {
				t.Errorf("events = %+v", h.events)
			}
		})
	}
}

func TestInvalidSignaturesAreRejectedWithoutTrigger(t *testing.T) {
	body := "payload"
	cases := map[string]struct {
		project string
		set     func(r *http.Request)
	}{
		"missing":         {"gh", nil},
		"wrong secret":    {"gh", func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "sha256="+sign(secretB, body)) }},
		"other project":   {"gh", func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "sha256="+sign(secretB, body)) }},
		"no prefix":       {"gh", func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", sign(secretA, body)) }},
		"not hex":         {"gh", func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "sha256=zz") }},
		"other body":      {"gt", func(r *http.Request) { r.Header.Set("X-Gitea-Signature", sign(secretA, "other")) }},
		"gitea prefix":    {"gt", func(r *http.Request) { r.Header.Set("X-Gitea-Signature", "sha256="+sign(secretA, body)) }},
		"gitlab wrong":    {"gl", func(r *http.Request) { r.Header.Set("X-Gitlab-Token", secretB) }},
		"gitlab empty":    {"gl", func(r *http.Request) { r.Header.Set("X-Gitlab-Token", "") }},
		"hmac wrong hdr":  {"mac", func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "sha256="+sign(secretA, body)) }},
		"hmac github hdr": {"mac", func(r *http.Request) { r.Header.Set("X-Gitea-Signature", sign(secretA, body)) }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, nil)
			w := h.post(c.project, body, c.set)
			if w.Code != http.StatusUnauthorized || w.Body.Len() != 0 {
				t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
			}
			if h.count() != 0 {
				t.Error("triggered")
			}
			if len(h.events) != 1 || h.events[0].Code != CodeUnauthorized || h.events[0].Project != c.project {
				t.Errorf("events = %+v", h.events)
			}
		})
	}
}

func TestSignatureOfAnotherProjectIsRejected(t *testing.T) {
	h := newHarness(t, nil)
	body := "payload"
	w := h.post("other", body, func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "sha256="+sign(secretA, body)) })
	if w.Code != http.StatusUnauthorized || h.count() != 0 {
		t.Fatalf("status = %d triggers = %d", w.Code, h.count())
	}
}

func TestOversizedBodyIsRejectedBeforeAnyTrigger(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.MaxBody = 16 })
	body := strings.Repeat("x", 17)
	w := h.post("gh", body, func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "sha256="+sign(secretA, body)) })
	if w.Code != http.StatusRequestEntityTooLarge || w.Body.Len() != 0 || h.count() != 0 {
		t.Fatalf("status = %d body = %q triggers = %d", w.Code, w.Body.String(), h.count())
	}
	if h.events[0].Code != CodeTooLarge {
		t.Errorf("events = %+v", h.events)
	}
}

func TestUnknownPathsAndMethodsAre404(t *testing.T) {
	h := newHarness(t, nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/hooks/unknown"},
		{http.MethodPost, "/hooks/"},
		{http.MethodPost, "/hooks/gh/extra"},
		{http.MethodPost, "/hooks/../gh"},
		{http.MethodPost, "/gh"},
		{http.MethodGet, "/hooks/gh"},
		{http.MethodPut, "/hooks/gh"},
	} {
		r := httptest.NewRequest(c.method, c.path, nil)
		w := httptest.NewRecorder()
		h.h.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound || w.Body.Len() != 0 {
			t.Errorf("%s %s = %d %q", c.method, c.path, w.Code, w.Body.String())
		}
	}
	if h.count() != 0 {
		t.Error("triggered")
	}
	for _, e := range h.events {
		if e.Project != "" || e.Code != CodeNotFound {
			t.Errorf("event = %+v", e)
		}
	}
}

func TestLockedVaultIs401WithoutTrigger(t *testing.T) {
	h := newHarness(t, nil)
	h.locked = true
	body := "payload"
	w := h.post("gh", body, func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "sha256="+sign(secretA, body)) })
	if w.Code != http.StatusUnauthorized || w.Body.Len() != 0 || h.count() != 0 {
		t.Fatalf("status = %d triggers = %d", w.Code, h.count())
	}
	if h.events[0].Code != CodeUnavailable || h.events[0].Project != "" {
		t.Errorf("events = %+v", h.events)
	}
}

func TestRateLimitPerProject(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Burst = 2; c.Refill = time.Minute })
	send := func(project string) int {
		return h.post(project, "b", func(r *http.Request) { r.Header.Set("X-Gitlab-Token", secretA) }).Code
	}
	// gl is a GitLab project; other uses another secret and kind, so use the same header on a second GitLab one.
	h.targets["gl2"] = Target{Kind: config.WebhookGitLab, Secret: []byte(secretA)}
	if a, b, c := send("gl"), send("gl"), send("gl"); a != 202 || b != 202 || c != 429 {
		t.Fatalf("codes = %d %d %d", a, b, c)
	}
	if got := send("gl2"); got != 202 {
		t.Errorf("another project is limited too: %d", got)
	}
	if h.count() != 3 {
		t.Errorf("triggers = %v", h.triggers)
	}
	// An invalid call neither uses the budget nor gets a 429.
	bad := h.post("gl", "b", func(r *http.Request) { r.Header.Set("X-Gitlab-Token", secretB) })
	if bad.Code != 401 {
		t.Errorf("invalid call = %d", bad.Code)
	}
	h.mu.Lock()
	h.now = h.now.Add(time.Minute)
	h.mu.Unlock()
	if got := send("gl"); got != 202 {
		t.Errorf("after refill = %d", got)
	}
	last := h.events[2]
	if last.Code != CodeRateLimited || last.Project != "gl" {
		t.Errorf("event = %+v", last)
	}
}

func TestServeEndsWithItsContext(t *testing.T) {
	h := newHarness(t, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, ln, h.h) }()
	body := "x"
	req, _ := http.NewRequest(http.MethodPost, "http://"+ln.Addr().String()+PathPrefix+"gt", strings.NewReader(body))
	req.Header.Set("X-Gitea-Signature", sign(secretA, body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d", resp.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not end")
	}
}
