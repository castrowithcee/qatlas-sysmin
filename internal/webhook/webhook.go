// Package webhook receives the signed webhooks of projects. A valid webhook only asks for an immediate
// check of the project's source; nothing in its body or headers other than the signature is read.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// PathPrefix precedes the project name in the path of a webhook.
const PathPrefix = "/hooks/"

// Limits of the handler and the server. They keep slow or oversized requests from tying the service up.
const (
	DefaultMaxBody = 1 << 20
	// DefaultBurst and DefaultRefill allow short bursts of valid webhooks per project but at most
	// ten a minute over time.
	DefaultBurst  = 5
	DefaultRefill = 6 * time.Second

	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	idleTimeout       = 30 * time.Second
	maxHeaderBytes    = 16 << 10
	shutdownGrace     = 5 * time.Second
)

// Codes of the audit log for a rejected call.
const (
	CodeNotFound     = "not-found"
	CodeUnauthorized = "invalid-signature"
	CodeUnavailable  = "secret-unavailable"
	CodeTooLarge     = "body-too-large"
	CodeRateLimited  = "rate-limited"
	CodeBadRequest   = "bad-request"
)

// Lookup failures.
var (
	// ErrNoWebhook means the project does not exist, has no approved webhook or the path names none.
	ErrNoWebhook = errors.New("no webhook for the project")
	// ErrUnavailable means the secret cannot be read, for example while the vault is locked.
	ErrUnavailable = errors.New("the webhook secret is not available")
)

// Target is what the approved state says about the webhook of one project.
type Target struct {
	Kind   string
	Secret []byte
}

// Event is the outcome of one call; Code is empty for an accepted call. Project is set only for a call
// whose project has a webhook.
type Event struct {
	Project  string
	Code     string
	Duration time.Duration
}

// Config configures a Handler.
type Config struct {
	// Lookup returns the approved webhook of a project: ErrNoWebhook or ErrUnavailable otherwise.
	Lookup func(project string) (Target, error)
	// Trigger asks for a check of the project; its result is not part of the response.
	Trigger func(project string) bool
	// Record is told about every call; nil records nothing.
	Record func(Event)

	MaxBody int64 // zero is DefaultMaxBody
	Burst   int   // zero is DefaultBurst
	Refill  time.Duration
	Now     func() time.Time
}

// Handler serves POST PathPrefix+project. Every answer is a bare status without a body.
type Handler struct {
	cfg Config

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New returns the handler for cfg.
func New(cfg Config) *Handler {
	if cfg.MaxBody <= 0 {
		cfg.MaxBody = DefaultMaxBody
	}
	if cfg.Burst <= 0 {
		cfg.Burst = DefaultBurst
	}
	if cfg.Refill <= 0 {
		cfg.Refill = DefaultRefill
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Handler{cfg: cfg, buckets: map[string]*bucket{}}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	status, ev := h.serve(w, r)
	w.WriteHeader(status)
	if h.cfg.Record != nil {
		ev.Duration = time.Since(start)
		h.cfg.Record(ev)
	}
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) (int, Event) {
	name, ok := strings.CutPrefix(r.URL.Path, PathPrefix)
	// Any other method answers like an unknown path, so the path does not tell whether a project exists.
	if !ok || !config.ValidName(name) || r.Method != http.MethodPost {
		return http.StatusNotFound, Event{Code: CodeNotFound}
	}
	t, err := h.cfg.Lookup(name)
	switch {
	case errors.Is(err, ErrUnavailable):
		return http.StatusUnauthorized, Event{Code: CodeUnavailable}
	case err != nil || len(t.Secret) == 0:
		return http.StatusNotFound, Event{Code: CodeNotFound}
	}
	ev := Event{Project: name}
	body, err := readBody(w, r, h.cfg.MaxBody)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			ev.Code = CodeTooLarge
			return http.StatusRequestEntityTooLarge, ev
		}
		ev.Code = CodeBadRequest
		return http.StatusBadRequest, ev
	}
	if !verify(t, r.Header, body) {
		ev.Code = CodeUnauthorized
		return http.StatusUnauthorized, ev
	}
	// Only valid calls use the budget, so a caller without the secret cannot starve the real sender.
	if !h.allow(name) {
		ev.Code = CodeRateLimited
		return http.StatusTooManyRequests, ev
	}
	h.cfg.Trigger(name)
	return http.StatusAccepted, ev
}

func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func (h *Handler) allow(project string) bool {
	now := h.cfg.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.buckets[project]
	if !ok {
		b = &bucket{tokens: float64(h.cfg.Burst), last: now}
		h.buckets[project] = b
	}
	if d := now.Sub(b.last); d > 0 {
		b.tokens = min(float64(h.cfg.Burst), b.tokens+float64(d)/float64(h.cfg.Refill))
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// verify checks the signature of the provider that t names. Every comparison runs in constant time.
func verify(t Target, header http.Header, body []byte) bool {
	switch t.Kind {
	case config.WebhookGitHub:
		return sigEqual(t.Secret, body, header.Get("X-Hub-Signature-256"), true)
	case config.WebhookGitea:
		return sigEqual(t.Secret, body, header.Get("X-Gitea-Signature"), false)
	case config.WebhookHMAC:
		return sigEqual(t.Secret, body, header.Get("X-Sysmin-Signature"), true)
	case config.WebhookGitLab:
		// Hashing first makes the comparison independent of the lengths.
		got, want := sha256.Sum256([]byte(header.Get("X-Gitlab-Token"))), sha256.Sum256(t.Secret)
		return hmac.Equal(got[:], want[:])
	}
	return false
}

// sigEqual compares the hex HMAC-SHA256 of body in value, with the prefix "sha256=" when prefixed.
func sigEqual(secret, body []byte, value string, prefixed bool) bool {
	if prefixed {
		rest, ok := strings.CutPrefix(value, "sha256=")
		if !ok {
			return false
		}
		value = rest
	}
	got, err := hex.DecodeString(value)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// Serve serves h on ln until ctx ends. It then lets running requests finish for a short grace period.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
		case <-stop:
			return
		}
		sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if srv.Shutdown(sctx) != nil {
			_ = srv.Close()
		}
	}()
	err := srv.Serve(ln)
	close(stop)
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
