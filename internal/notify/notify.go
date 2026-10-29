package notify

import (
	"context"
	"errors"
	"fmt"
	"net/textproto"
	"os"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// Channels returns the approved channels; *approval.Manager does.
type Channels interface {
	ApprovedChannels() (approval.ApprovedChannels, error)
}

// Credentials returns a vault entry by id; *vault.Vault does.
type Credentials interface {
	Get(id string) (vault.Entry, error)
}

// Auditor records one entry; *auditlog.Logger does. The entry names the channel in Project and the
// outcome in Result and ErrorCode, never a message or a value.
type Auditor interface {
	Append(auditlog.Fields) error
}

// Errors of Send, to be matched with errors.Is.
var (
	// ErrUnknownChannel means no approved channel has the name.
	ErrUnknownChannel = errors.New("no approved channel has this name")
	// ErrNeedsSecret means the channel names a credential that cannot be used now: the vault is locked, or
	// the event may only travel over channels without one.
	ErrNeedsSecret = errors.New("the channel needs a secret that is not available")
)

// Error codes recorded in the audit log for a failed send.
const (
	codeSendFailed      = "send-failed"
	codeVaultLocked     = "vault-locked"
	codeNeedsSecret     = "needs-secret"
	codeUnknownChannel  = "unknown-channel"
	codeNotApproved     = "not-approved"
	codeIntegrity       = "integrity"
	codeNoCredential    = "credential-unavailable"
	codeInvalidChannel  = "invalid-channel"
	codeNoStateWhenLock = "no-approved-state"
	codeInvalidEvent    = "invalid-event"
)

const (
	attempts       = 3
	defaultBackoff = 2 * time.Second
	defaultTimeout = 15 * time.Second
)

// Notifier sends events. Create it with New; it is safe for concurrent use.
type Notifier struct {
	channels Channels
	creds    Credentials
	audit    Auditor

	hostname func() string
	now      func() time.Time
	backoff  time.Duration
	timeout  time.Duration
	redactor *redact.Redactor
	opts     transportOptions

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
	// last is the approved state as it was last verified. Only the locked-vault fallback reads it.
	last *approval.ApprovedChannels
}

// Option configures a Notifier.
type Option func(*Notifier)

// WithHostname sets the function that names this host in messages; the default is os.Hostname.
func WithHostname(f func() string) Option { return func(n *Notifier) { n.hostname = f } }

// WithClock sets the clock for message times and webhook timestamps.
func WithClock(f func() time.Time) Option { return func(n *Notifier) { n.now = f } }

// WithBackoff sets the wait before the second attempt; the wait doubles for the third.
func WithBackoff(d time.Duration) Option { return func(n *Notifier) { n.backoff = d } }

// WithTimeout sets the time limit of one attempt.
func WithTimeout(d time.Duration) Option { return func(n *Notifier) { n.timeout = d } }

// WithRedactor removes the secrets this redactor knows from failure texts, in addition to the credential
// of the channel being used.
func WithRedactor(r *redact.Redactor) Option { return func(n *Notifier) { n.redactor = r } }

// New returns a Notifier. audit may be nil.
func New(channels Channels, creds Credentials, audit Auditor, opts ...Option) *Notifier {
	n := &Notifier{
		channels: channels, creds: creds, audit: audit,
		hostname: func() string {
			h, err := os.Hostname()
			if err != nil {
				return "unknown"
			}
			return h
		},
		now: time.Now, backoff: defaultBackoff, timeout: defaultTimeout, redactor: &redact.Redactor{},
	}
	n.ctx, n.cancel = context.WithCancel(context.Background())
	for _, o := range opts {
		o(n)
	}
	n.opts.init()
	return n
}

// Notify reports ev over the approved channels, or only over those named in channels, and returns at
// once. Delivery runs in the background with up to three attempts per channel; the outcome is recorded in
// the audit log. After Close it does nothing.
func (n *Notifier) Notify(ev Event, channels ...string) {
	if !n.start() {
		return
	}
	go func() {
		defer n.wg.Done()
		n.dispatch(ev, channels)
	}()
}

// Refresh reads the approved state so that vault.locked can still be sent after the vault is locked. It
// fails like approval.Manager.ApprovedChannels.
func (n *Notifier) Refresh() error {
	_, _, err := n.approved()
	if errors.Is(err, vault.ErrLocked) {
		return nil
	}
	return err
}

// Wait blocks until every delivery started so far has finished.
func (n *Notifier) Wait() { n.wg.Wait() }

// Close stops new and running deliveries and waits for them.
func (n *Notifier) Close() {
	n.mu.Lock()
	n.closed = true
	n.mu.Unlock()
	n.cancel()
	n.wg.Wait()
}

func (n *Notifier) start() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return false
	}
	n.wg.Add(1)
	return true
}

// approved returns the approved channels. locked is true when the vault is locked; the state is then the
// last verified one, or the error is vault.ErrLocked when there is none.
func (n *Notifier) approved() (st approval.ApprovedChannels, locked bool, err error) {
	st, err = n.channels.ApprovedChannels()
	n.mu.Lock()
	defer n.mu.Unlock()
	switch {
	case err == nil:
		n.last = &st
		return st, false, nil
	case errors.Is(err, vault.ErrLocked):
		if n.last == nil {
			return approval.ApprovedChannels{}, true, err
		}
		return *n.last, true, nil
	default:
		// The approval is gone or no longer verifies, so the last state must not be used any more.
		n.last = nil
		return approval.ApprovedChannels{}, false, err
	}
}

// restricted says whether the event or the vault state allows only channels that name no credential.
func restricted(ev Event, locked bool) bool { return locked || ev.Type == EventVaultLocked }

func (n *Notifier) dispatch(ev Event, only []string) {
	if !ev.Type.valid() {
		n.record("", time.Now(), codeInvalidEvent)
		return
	}
	start := time.Now()
	st, locked, err := n.approved()
	if err != nil {
		n.record("", start, stateCode(err, locked))
		return
	}
	if locked && ev.Type != EventVaultLocked {
		n.record("", start, codeVaultLocked)
		return
	}
	want := map[string]bool{}
	for _, name := range only {
		want[name] = true
	}
	for _, ch := range st.Channels {
		delete(want, ch.Name)
	}
	for name := range want {
		n.record(name, start, codeUnknownChannel)
	}
	for _, ch := range st.Channels {
		if len(only) > 0 && !contains(only, ch.Name) {
			continue
		}
		if restricted(ev, locked) && ch.Credential != "" {
			n.record(ch.Name, start, codeNeedsSecret)
			continue
		}
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			err := n.deliver(n.ctx, ch, st.Credentials, ev, attempts)
			n.record(ch.Name, start, errCode(err))
		}()
	}
}

// Send delivers ev once to the approved channel with the given name and returns the result; the error
// text holds no secret. It is meant for a manual test, so it does not retry.
func (n *Notifier) Send(ctx context.Context, channel string, ev Event) error {
	start := time.Now()
	err := n.send(ctx, channel, ev)
	n.record(channel, start, errCode(err))
	return err
}

func (n *Notifier) send(ctx context.Context, channel string, ev Event) error {
	if !ev.Type.valid() {
		return errors.New("unknown event type")
	}
	st, locked, err := n.approved()
	if err != nil {
		if errors.Is(err, vault.ErrLocked) {
			return fmt.Errorf("%w: the vault is locked", ErrNeedsSecret)
		}
		return err
	}
	if locked && ev.Type != EventVaultLocked {
		return fmt.Errorf("%w: the vault is locked", ErrNeedsSecret)
	}
	for _, ch := range st.Channels {
		if ch.Name != channel {
			continue
		}
		if restricted(ev, locked) && ch.Credential != "" {
			return ErrNeedsSecret
		}
		return n.deliver(ctx, ch, st.Credentials, ev, 1)
	}
	return ErrUnknownChannel
}

// deliveryError is a failure whose text is already free of secrets.
type deliveryError struct {
	msg   string
	code  string
	retry bool
	err   error
}

func (e *deliveryError) Error() string { return e.msg }
func (e *deliveryError) Unwrap() error { return e.err }

func errCode(err error) string {
	if err == nil {
		return ""
	}
	var d *deliveryError
	if errors.As(err, &d) && d.code != "" {
		return d.code
	}
	switch {
	case errors.Is(err, ErrUnknownChannel):
		return codeUnknownChannel
	case errors.Is(err, ErrNeedsSecret), errors.Is(err, vault.ErrLocked):
		return codeNeedsSecret
	}
	return stateCode(err, false)
}

func stateCode(err error, locked bool) string {
	switch {
	case errors.Is(err, vault.ErrLocked) && locked:
		return codeNoStateWhenLock
	case errors.Is(err, vault.ErrLocked):
		return codeVaultLocked
	case errors.Is(err, approval.ErrNotApproved):
		return codeNotApproved
	case errors.Is(err, approval.ErrIntegrity):
		return codeIntegrity
	}
	return codeSendFailed
}

// deliver resolves the credential of ch and sends ev, retrying up to the given number of attempts.
func (n *Notifier) deliver(ctx context.Context, ch config.Channel, ids map[string]string, ev Event, tries int) error {
	c, err := n.resolve(ch, ids)
	if err != nil {
		return err
	}
	var r redact.Redactor
	r.Add(c.secrets()...)
	redactText := func(s string) string { return r.Apply(n.redactor.Apply(s)) }
	p := n.payload(ev, redactText)

	var last error
	wait := n.backoff
	for i := 0; i < tries; i++ {
		if i > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return &deliveryError{msg: "sending was cancelled", code: codeSendFailed}
			}
			wait *= 2
		}
		actx, cancel := context.WithTimeout(ctx, n.timeout)
		last = n.attempt(actx, ch, c, p, redactText)
		cancel()
		var d *deliveryError
		if last == nil || (errors.As(last, &d) && !d.retry) {
			break
		}
	}
	return last
}

// resolve reads the approved credential of ch by its vault id.
func (n *Notifier) resolve(ch config.Channel, ids map[string]string) (credential, error) {
	if ch.Credential == "" {
		return credential{}, nil
	}
	id := ids[ch.Credential]
	if id == "" {
		return credential{}, &deliveryError{msg: "the approved credential of the channel is missing", code: codeNoCredential}
	}
	e, err := n.creds.Get(id)
	switch {
	case errors.Is(err, vault.ErrLocked):
		return credential{}, fmt.Errorf("%w: the vault is locked", ErrNeedsSecret)
	case err != nil:
		return credential{}, &deliveryError{msg: "the approved credential of the channel cannot be read", code: codeNoCredential}
	}
	return credentialOf(ch, e)
}

func (n *Notifier) attempt(ctx context.Context, ch config.Channel, c credential, p payload, redactText func(string) string) error {
	var err error
	switch ch.Type {
	case config.ChannelWebhook:
		err = n.sendWebhook(ctx, ch, c, p)
	case config.ChannelSlack:
		err = n.sendSlack(ctx, c, p)
	case config.ChannelTelegram:
		err = n.sendTelegram(ctx, ch, c, p)
	case config.ChannelSMTP:
		err = n.sendSMTP(ctx, ch, c, p)
	default:
		return &deliveryError{msg: "unsupported channel type", code: codeInvalidChannel}
	}
	if err == nil {
		return nil
	}
	var d *deliveryError
	if errors.As(err, &d) {
		return &deliveryError{msg: redactText(d.msg), code: d.code, retry: d.retry}
	}
	// A permanent refusal by an SMTP server (5xx, for example a wrong login) is not repeated.
	var te *textproto.Error
	retry := !errors.As(err, &te) || te.Code < 500
	return &deliveryError{msg: redactText(err.Error()), code: codeSendFailed, retry: retry}
}

// record writes the outcome of one send; code is empty for success. A failing log never changes the
// outcome.
func (n *Notifier) record(channel string, start time.Time, code string) {
	if n.audit == nil {
		return
	}
	f := auditlog.Fields{
		ClientUID: uint32(os.Getuid()), Action: auditlog.OperationNotifySend, Project: channel,
		Result: auditlog.ResultSuccess, Duration: time.Since(start),
	}
	if code != "" {
		f.Result, f.ErrorCode = auditlog.ResultFailure, code
	}
	if f.Duration > auditlog.MaxDuration {
		f.Duration = 0
	}
	if err := n.audit.Append(f); err != nil && f.Project != "" {
		f.Project = ""
		_ = n.audit.Append(f)
	}
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}
