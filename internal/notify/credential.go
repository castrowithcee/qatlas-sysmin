package notify

import (
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// credential is the secret material of one channel: value for the kinds with a single secret, user and
// password for SMTP.
type credential struct {
	present        bool
	value          string
	user, password string
}

func (c credential) secrets() []string {
	return []string{c.value, c.user, c.password}
}

// credentialOf checks that the entry has the kind the channel type needs and copies its fields.
func credentialOf(ch config.Channel, e vault.Entry) (credential, error) {
	want := vault.KindSecret
	if ch.Type == config.ChannelSMTP {
		want = vault.KindSMTP
	}
	if e.Kind != want {
		return credential{}, &deliveryError{msg: fmt.Sprintf("the credential of a %s channel must be of kind %s", ch.Type, want), code: codeNoCredential}
	}
	get := func(field string) string { return e.Fields[field].Reveal() }
	if want == vault.KindSMTP {
		return credential{present: true, user: get(vault.FieldUser), password: get(vault.FieldPassword)}, nil
	}
	return credential{present: true, value: get(vault.FieldValue)}, nil
}

// transportOptions are the hooks tests use to reach local servers.
type transportOptions struct {
	client      *http.Client
	telegramURL string
	tlsConfig   *tls.Config
}

func (o *transportOptions) init() {
	if o.client == nil {
		o.client = &http.Client{
			// A redirect would carry the body and the signature to another place.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	if o.telegramURL == "" {
		o.telegramURL = "https://api.telegram.org"
	}
}

// WithTelegramBaseURL replaces the Telegram API address. Like every address that carries a secret, it must
// use https unless it points to the local machine.
func WithTelegramBaseURL(u string) Option { return func(n *Notifier) { n.opts.telegramURL = u } }

// WithTLSConfig sets the base TLS settings for SMTP connections, for example the root certificates of a
// test server. The server name is filled in from the channel's host when the config leaves it empty.
func WithTLSConfig(c *tls.Config) Option { return func(n *Notifier) { n.opts.tlsConfig = c } }

func (n *Notifier) tlsConfig(host string) *tls.Config {
	var c *tls.Config
	if n.opts.tlsConfig != nil {
		c = n.opts.tlsConfig.Clone()
	} else {
		c = &tls.Config{}
	}
	if c.ServerName == "" {
		c.ServerName = host
	}
	if c.MinVersion < tls.VersionTLS12 {
		c.MinVersion = tls.VersionTLS12
	}
	return c
}
