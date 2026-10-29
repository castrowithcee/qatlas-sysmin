package service

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
	"github.com/castrowithcee/qatlas-sysmin/internal/webhook"
)

// defaultWebhookRecheck is how often the service looks again for an approved webhook; an unlock checks at
// once.
const defaultWebhookRecheck = 10 * time.Second

// webhookListen reads the listen address from sysmin.yaml when the listener starts.
func (s *Service) webhookListen() string {
	data, err := os.ReadFile(filepath.Join(s.cfg.ConfigDir, config.SettingsFile))
	if err != nil {
		return config.DefaultListen
	}
	settings, err := config.DecodeSettings(data)
	if err != nil {
		return config.DefaultListen
	}
	return settings.EffectiveListen()
}

// hasWebhook reports whether a project has an approved webhook. Only the approved state counts, so a
// locked vault, which hides it, means no listener.
func (s *Service) hasWebhook() bool {
	for _, name := range s.projectNames() {
		if ap, err := s.approvals.Approved(name); err == nil && ap.Project.Webhook != nil {
			return true
		}
	}
	return false
}

// webhookTarget gives the webhook handler the approved kind and the secret of the credential by the vault
// id that was approved.
func (s *Service) webhookTarget(project string) (webhook.Target, error) {
	ap, err := s.approvals.Approved(project)
	switch {
	case errors.Is(err, vault.ErrLocked):
		return webhook.Target{}, webhook.ErrUnavailable
	case err != nil || ap.Project.Webhook == nil:
		return webhook.Target{}, webhook.ErrNoWebhook
	}
	w := ap.Project.Webhook
	creds := &approvedCredentials{vault: s.vault, ids: ap.Credentials}
	secret, err := creds.Secret(w.Secret)
	if err != nil {
		return webhook.Target{}, webhook.ErrUnavailable
	}
	return webhook.Target{Kind: w.Kind, Secret: []byte(secret)}, nil
}

func (s *Service) recordWebhook(ev webhook.Event) {
	f := auditlog.Fields{
		ClientUID: uint32(os.Getuid()), Action: auditlog.OperationWebhookReceive, Project: ev.Project,
		Result: auditlog.ResultSuccess, Duration: min(ev.Duration, auditlog.MaxDuration),
	}
	if ev.Code != "" {
		f.Action, f.Result, f.ErrorCode = auditlog.OperationWebhookReject, auditlog.ResultFailure, ev.Code
	}
	if err := (notifyAudit{s}).Append(f); err != nil {
		slog.Warn("cannot log a webhook call")
		if s.cfg.OnLogError != nil {
			s.cfg.OnLogError(err)
		}
	}
}

// wakeWebhook asks the webhook supervisor to look again now.
func (s *Service) wakeWebhook() {
	select {
	case s.webhookWake <- struct{}{}:
	default:
	}
}

// superviseWebhook runs the webhook listener while at least one approved project has a webhook and
// stops it when none has or the address changed. Without a webhook nothing listens.
func (s *Service) superviseWebhook(ctx context.Context) {
	recheck := s.cfg.WebhookRecheck
	if recheck <= 0 {
		recheck = defaultWebhookRecheck
	}
	handler := webhook.New(webhook.Config{
		Lookup:  s.webhookTarget,
		Trigger: s.engine.Trigger,
		Record:  s.recordWebhook,
	})
	var cur *webhookRun
	halt := func() {
		if cur != nil {
			cur.stop()
			<-cur.done
			cur = nil
		}
	}
	defer halt()
	tick := time.NewTicker(recheck)
	defer tick.Stop()
	for {
		want := ""
		if s.hasWebhook() {
			want = s.webhookListen()
		}
		if cur != nil && want != cur.addr {
			halt()
		}
		if cur == nil && want != "" {
			if ln, err := net.Listen("tcp", want); err != nil {
				slog.Warn("cannot listen for webhooks", "address", want)
			} else {
				cur = startWebhook(ctx, want, ln, handler)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-s.webhookWake:
		}
	}
}

// webhookRun is a running webhook listener.
type webhookRun struct {
	addr string
	stop context.CancelFunc
	done chan struct{}
}

func startWebhook(ctx context.Context, addr string, ln net.Listener, h http.Handler) *webhookRun {
	ctx, stop := context.WithCancel(ctx)
	r := &webhookRun{addr: addr, stop: stop, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		if err := webhook.Serve(ctx, ln, h); err != nil {
			slog.Warn("the webhook listener stopped")
		}
	}()
	return r
}
