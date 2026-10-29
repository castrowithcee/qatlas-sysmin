package notify

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const (
	demoBotToken = "demo-bot-token-not-real"
	demoHookKey  = "demo-hook-signing-key-not-real"
	demoSMTPUser = "demo-mailer"
	demoSMTPPass = "demo-mail-password-not-real"
)

// fakeChannels is a Channels with a switchable vault state.
type fakeChannels struct {
	mu     sync.Mutex
	state  approval.ApprovedChannels
	err    error
	called int
	// snapshot is what UnverifiedChannels returns; snapshotErr fails it.
	snapshot    []config.Channel
	snapshotErr error
}

func (f *fakeChannels) UnverifiedChannels() ([]config.Channel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshotErr != nil {
		return nil, f.snapshotErr
	}
	return f.snapshot, nil
}

func (f *fakeChannels) ApprovedChannels() (approval.ApprovedChannels, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called++
	return f.state, f.err
}

func (f *fakeChannels) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// fakeCreds serves entries by id and can be locked.
type fakeCreds struct {
	mu      sync.Mutex
	entries map[string]vault.Entry
	locked  bool
}

func (f *fakeCreds) Get(id string) (vault.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.locked {
		return vault.Entry{}, vault.ErrLocked
	}
	e, ok := f.entries[id]
	if !ok {
		return vault.Entry{}, vault.ErrNotFound
	}
	return e, nil
}

func secretEntry(value string) vault.Entry {
	return vault.Entry{Kind: vault.KindSecret, Fields: map[string]vault.Secret{vault.FieldValue: vault.NewSecret(value)}}
}

func smtpEntry(user, pass string) vault.Entry {
	return vault.Entry{Kind: vault.KindSMTP, Fields: map[string]vault.Secret{
		vault.FieldUser: vault.NewSecret(user), vault.FieldPassword: vault.NewSecret(pass)}}
}

type recordingAudit struct {
	mu      sync.Mutex
	entries []auditlog.Fields
}

func (r *recordingAudit) Append(f auditlog.Fields) error {
	if err := f.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, f)
	return nil
}

func (r *recordingAudit) all() []auditlog.Fields {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]auditlog.Fields(nil), r.entries...)
}

func (r *recordingAudit) dump() string { return fmt.Sprintf("%+v", r.all()) }

// newNotifier builds a Notifier with a fixed host name and clock and a short backoff.
func newNotifier(ch Channels, cr Credentials, a Auditor, opts ...Option) *Notifier {
	base := []Option{
		WithHostname(func() string { return "demo-host" }),
		WithClock(func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }),
		WithBackoff(time.Millisecond), WithTimeout(2 * time.Second),
	}
	return New(ch, cr, a, append(base, opts...)...)
}

func approved(creds map[string]string, chans ...config.Channel) approval.ApprovedChannels {
	return approval.ApprovedChannels{Channels: chans, Credentials: creds}
}

// smtpServer is a minimal SMTP server that records what it receives.
type smtpServer struct {
	addr      string
	port      int
	rootCAs   *x509.CertPool
	startTLS  bool
	authMechs string
	mu        sync.Mutex
	user      string
	pass      string
	authTLS   bool
	authSeen  bool
	from      string
	rcpt      []string
	data      string
	plainAuth bool
}

func newCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "demo"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

// startSMTP serves one connection at a time. implicitTLS wraps the connection in TLS from the start;
// otherwise STARTTLS is offered when startTLS is true.
func startSMTP(t *testing.T, implicitTLS, startTLS bool, mechs string) *smtpServer {
	t.Helper()
	cert, pool := newCert(t)
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	var ln net.Listener
	var err error
	if implicitTLS {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", cfg)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s := &smtpServer{addr: ln.Addr().String(), port: ln.Addr().(*net.TCPAddr).Port, rootCAs: pool,
		startTLS: startTLS, authMechs: mechs}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.serve(conn, cfg, implicitTLS)
		}
	}()
	return s
}

func (s *smtpServer) serve(conn net.Conn, cfg *tls.Config, encrypted bool) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)
	say := func(f string, a ...any) { _, _ = fmt.Fprintf(conn, f+"\r\n", a...) }
	line := func() string {
		l, err := r.ReadString('\n')
		if err != nil {
			return ""
		}
		return strings.TrimRight(l, "\r\n")
	}
	say("220 demo ready")
	for {
		l := line()
		cmd := strings.ToUpper(l)
		switch {
		case l == "":
			return
		case strings.HasPrefix(cmd, "EHLO"):
			say("250-demo")
			if s.startTLS && !encrypted {
				say("250-STARTTLS")
			}
			say("250 AUTH %s", s.authMechs)
		case cmd == "STARTTLS":
			say("220 go ahead")
			tc := tls.Server(conn, cfg)
			if err := tc.Handshake(); err != nil {
				return
			}
			conn, encrypted = tc, true
			r = bufio.NewReader(tc)
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			s.noteAuth(encrypted)
			parts := strings.Fields(l)
			var raw []byte
			if len(parts) > 2 {
				raw, _ = base64.StdEncoding.DecodeString(parts[2])
			}
			f := strings.Split(string(raw), "\x00")
			if len(f) == 3 {
				s.setCreds(f[1], f[2], true)
			}
			s.authReply(say)
		case cmd == "AUTH LOGIN":
			s.noteAuth(encrypted)
			say("334 %s", base64.StdEncoding.EncodeToString([]byte("Username:")))
			u, _ := base64.StdEncoding.DecodeString(line())
			say("334 %s", base64.StdEncoding.EncodeToString([]byte("Password:")))
			p, _ := base64.StdEncoding.DecodeString(line())
			s.setCreds(string(u), string(p), false)
			s.authReply(say)
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			s.mu.Lock()
			s.from = l[len("MAIL FROM:"):]
			s.mu.Unlock()
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			s.mu.Lock()
			s.rcpt = append(s.rcpt, l[len("RCPT TO:"):])
			s.mu.Unlock()
			say("250 ok")
		case cmd == "DATA":
			say("354 go")
			var b strings.Builder
			for {
				d := line()
				if d == "." {
					break
				}
				b.WriteString(d + "\n")
			}
			s.mu.Lock()
			s.data = b.String()
			s.mu.Unlock()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("500 unknown")
		}
	}
}

func (s *smtpServer) noteAuth(encrypted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authSeen = true
	s.authTLS = encrypted
}

func (s *smtpServer) setCreds(u, p string, plain bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.user, s.pass, s.plainAuth = u, p, plain
}

func (s *smtpServer) authReply(say func(string, ...any)) {
	s.mu.Lock()
	ok := s.user == demoSMTPUser && s.pass == demoSMTPPass
	s.mu.Unlock()
	if ok {
		say("235 welcome")
	} else {
		say("535 wrong login")
	}
}
