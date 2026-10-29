package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// sendSMTP always encrypts the connection, with TLS from the start or with STARTTLS (the default), and
// refuses to go on when STARTTLS is not offered. The login is sent only after that.
func (n *Notifier) sendSMTP(ctx context.Context, ch config.Channel, c credential, p payload) error {
	from, err := mail.ParseAddress(ch.From)
	if err != nil {
		return &deliveryError{msg: "the sender address is invalid", code: codeInvalidChannel}
	}
	var to []string
	for _, t := range ch.To {
		a, err := mail.ParseAddress(t)
		if err != nil {
			return &deliveryError{msg: "a recipient address is invalid", code: codeInvalidChannel}
		}
		to = append(to, a.String())
	}
	security, port := ch.Security, ch.Port
	if security == "" {
		security = config.SecurityStartTLS
	}
	if port == 0 {
		port = 587
		if security == config.SecurityTLS {
			port = 465
		}
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ch.Host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	tlsCfg := n.tlsConfig(ch.Host)
	if security == config.SecurityTLS {
		tc := tls.Client(conn, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return err
		}
		conn = tc
	}
	cl, err := smtp.NewClient(conn, ch.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = cl.Close() }()
	if err := cl.Hello("localhost"); err != nil {
		return err
	}
	if security == config.SecurityStartTLS {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return &deliveryError{msg: "the server does not offer STARTTLS", code: codeSendFailed}
		}
		if err := cl.StartTLS(tlsCfg); err != nil {
			return err
		}
	}
	if c.present {
		auth, err := pickAuth(cl, ch.Host, c)
		if err != nil {
			return err
		}
		if err := cl.Auth(auth); err != nil {
			return err
		}
	}
	if err := cl.Mail(from.Address); err != nil {
		return err
	}
	for _, t := range to {
		a, _ := mail.ParseAddress(t)
		if err := cl.Rcpt(a.Address); err != nil {
			return err
		}
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(buildMessage(from, to, p, n.now())); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

// pickAuth prefers PLAIN and falls back to LOGIN. Both are used on an encrypted connection only, which
// sendSMTP guarantees before it gets here.
func pickAuth(cl *smtp.Client, host string, c credential) (smtp.Auth, error) {
	_, params := cl.Extension("AUTH")
	mechs := strings.Fields(strings.ToUpper(params))
	for _, m := range mechs {
		if m == "PLAIN" {
			return smtp.PlainAuth("", c.user, c.password, host), nil
		}
	}
	for _, m := range mechs {
		if m == "LOGIN" {
			return &loginAuth{user: c.user, password: c.password}, nil
		}
	}
	return nil, &deliveryError{msg: "the server offers no supported login (PLAIN, LOGIN)", code: codeSendFailed}
}

type loginAuth struct{ user, password string }

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, errors.New("refusing to send a login over an unencrypted connection")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(prompt []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(prompt))) {
	case "username:":
		return []byte(a.user), nil
	case "password:":
		return []byte(a.password), nil
	}
	return nil, fmt.Errorf("unexpected login prompt")
}

func buildMessage(from *mail.Address, to []string, p payload, now time.Time) []byte {
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", from.String())
	h("To", strings.Join(to, ", "))
	h("Subject", mime.QEncoding.Encode("utf-8", p.subject()))
	h("Date", now.Format(time.RFC1123Z))
	h("MIME-Version", "1.0")
	h("Content-Type", `text/plain; charset="utf-8"`)
	h("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(strings.ReplaceAll(p.text(), "\n", "\r\n")))
	_ = qp.Close()
	return b.Bytes()
}
