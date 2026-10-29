// Package gitrepo lists the tags of a remote git repository with the system git. Secrets reach git only
// through files in a private temporary directory that the child process reads; they are never part of
// arguments, URLs, the environment, errors or log records.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Limits and defaults.
const (
	DefaultTimeout = 60 * time.Second
	maxStdout      = 4 << 20
	maxStderr      = 16 << 10
	maxErrorText   = 512
)

// Credential kinds.
const (
	KindHTTPS = "https"
	KindSSH   = "ssh"
)

// Credential is the login for one remote. The zero value is anonymous access over HTTPS.
type Credential struct {
	// Kind is KindHTTPS or KindSSH; empty means no credential.
	Kind string
	// User and Token are the HTTPS login. A token without a user is sent as the password of an empty user.
	User, Token string
	// PrivateKey is the SSH private key; HostKey the pinned host key, either a known_hosts line or the bare
	// "type base64" form of a public key, which is bound to the host of the URL.
	PrivateKey, HostKey string
}

// Options configures a Lister.
type Options struct {
	// Timeout bounds one listing; zero means DefaultTimeout.
	Timeout time.Duration
	// Run executes the prepared git command. It exists so tests can inspect the command; nil runs it.
	Run func(*exec.Cmd) error

	// extraProtocols are further schemes git may use; tests allow file and http.
	extraProtocols []string
}

// Lister lists remote tags.
type Lister struct{ opts Options }

// New returns a Lister.
func New(opts Options) *Lister { return &Lister{opts: opts} }

// ErrAuth is wrapped by errors for a missing or mismatching credential.
var ErrAuth = errors.New("credential does not fit the url")

// Tags returns the names of all tags of the repository at rawURL, sorted as git prints them and without
// duplicates.
func (l *Lister) Tags(ctx context.Context, rawURL string, cred Credential) ([]string, error) {
	target, err := l.parseURL(rawURL)
	if err != nil {
		return nil, err
	}
	switch {
	case cred.Kind == KindSSH && target.scheme != KindSSH:
		return nil, fmt.Errorf("%w: an ssh key needs an ssh url", ErrAuth)
	case cred.Kind == KindHTTPS && target.scheme != KindHTTPS:
		return nil, fmt.Errorf("%w: a https login needs an https url", ErrAuth)
	case cred.Kind == KindSSH && (strings.TrimSpace(cred.PrivateKey) == "" || strings.TrimSpace(cred.HostKey) == ""):
		return nil, fmt.Errorf("%w: private key and host key are both required", ErrAuth)
	case cred.Kind == KindHTTPS && cred.Token == "":
		return nil, fmt.Errorf("%w: the token is empty", ErrAuth)
	case cred.Kind != "" && cred.Kind != KindSSH && cred.Kind != KindHTTPS:
		return nil, fmt.Errorf("%w: unknown credential kind %q", ErrAuth, cred.Kind)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git is not installed")
	}

	dir, err := os.MkdirTemp("", "sysmin-*")
	if err != nil {
		return nil, fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	env, err := l.environment(dir, target, cred)
	if err != nil {
		return nil, err
	}

	timeout := l.opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, gitPath, "-c", "credential.helper=", "ls-remote", "--tags", "--refs", "--", rawURL)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr capped
	stdout.limit, stderr.limit = maxStdout, maxStderr
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	confine(cmd)

	run := l.opts.Run
	if run == nil {
		run = (*exec.Cmd).Run
	}
	if err := run(cmd); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("listing tags of %s: %w", redactURL(rawURL), ctx.Err())
		}
		return nil, fmt.Errorf("listing tags of %s failed: %s", redactURL(rawURL), failureText(err, stderr.String(), cred))
	}
	if stdout.over {
		return nil, errors.New("git printed more output than expected")
	}
	return parseTags(stdout.String()), nil
}

type remote struct {
	scheme string // KindHTTPS or KindSSH, or an extra scheme
	host   string
	port   string
}

var scpRe = regexp.MustCompile(`^([A-Za-z0-9._-]+)@([A-Za-z0-9.-]+):([^\s]+)$`)

func (l *Lister) parseURL(raw string) (remote, error) {
	switch {
	case raw == "" || strings.HasPrefix(raw, "-") || strings.ContainsAny(raw, " \t\r\n\x00"):
		return remote{}, errors.New("invalid git url")
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil || (u.Host == "" && u.Scheme != "file") {
			return remote{}, errors.New("invalid git url")
		}
		allowed := u.Scheme == KindHTTPS || u.Scheme == KindSSH
		for _, p := range l.opts.extraProtocols {
			allowed = allowed || p == u.Scheme
		}
		if !allowed {
			return remote{}, fmt.Errorf("git url scheme %q is not supported", u.Scheme)
		}
		if u.User != nil {
			if _, hasPass := u.User.Password(); hasPass || u.Scheme != KindSSH {
				return remote{}, errors.New("git url must not contain credentials; use a credential")
			}
		}
		scheme := u.Scheme
		if scheme == "http" { // only reachable when a test allowed it
			scheme = KindHTTPS
		}
		return remote{scheme: scheme, host: u.Hostname(), port: u.Port()}, nil
	default:
		m := scpRe.FindStringSubmatch(raw)
		if m == nil {
			return remote{}, errors.New("invalid git url")
		}
		return remote{scheme: KindSSH, host: m[2]}, nil
	}
}

func (l *Lister) environment(dir string, target remote, cred Credential) ([]string, error) {
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o700); err != nil {
		return nil, err
	}
	protocols := []string{KindHTTPS, KindSSH}
	protocols = append(protocols, l.opts.extraProtocols...)
	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=" + strings.Join(protocols, ":"),
		"SSH_AUTH_SOCK=",
	}
	switch cred.Kind {
	case KindHTTPS:
		// git executes the askpass program itself, not through a shell, so the script needs the exec bit.
		script := filepath.Join(dir, "askpass.sh")
		const body = "#!/bin/sh\ncase \"$1\" in\n  Username*) cat \"$(dirname \"$0\")/user\" ;;\n  *) cat \"$(dirname \"$0\")/token\" ;;\nesac\n"
		if err := writeFile(script, body); err != nil {
			return nil, err
		}
		if err := os.Chmod(script, 0o700); err != nil {
			return nil, err
		}
		if err := writeFile(filepath.Join(dir, "user"), cred.User); err != nil {
			return nil, err
		}
		if err := writeFile(filepath.Join(dir, "token"), cred.Token); err != nil {
			return nil, err
		}
		env = append(env, "GIT_ASKPASS="+script)
	case KindSSH:
		keyPath := filepath.Join(dir, "id")
		key := cred.PrivateKey
		if !strings.HasSuffix(key, "\n") {
			key += "\n"
		}
		if err := writeFile(keyPath, key); err != nil {
			return nil, err
		}
		hosts, err := knownHosts(cred.HostKey, target)
		if err != nil {
			return nil, err
		}
		hostsPath := filepath.Join(dir, "known_hosts")
		if err := writeFile(hostsPath, hosts); err != nil {
			return nil, err
		}
		env = append(env, "GIT_SSH_COMMAND="+strings.Join([]string{
			"ssh", "-F", "/dev/null",
			"-o", "StrictHostKeyChecking=yes",
			"-o", "UserKnownHostsFile=" + shellQuote(hostsPath),
			"-o", "GlobalKnownHostsFile=/dev/null",
			"-o", "IdentitiesOnly=yes",
			"-o", "IdentityFile=" + shellQuote(keyPath),
			"-o", "BatchMode=yes",
			"-o", "PasswordAuthentication=no",
			"-o", "KbdInteractiveAuthentication=no",
			"-o", "ForwardAgent=no",
			"-o", "UpdateHostKeys=no",
			"-o", "ConnectTimeout=20",
		}, " "))
	}
	return env, nil
}

func writeFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// shellQuote quotes s for the shell git runs GIT_SSH_COMMAND through.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

var keyTypeRe = regexp.MustCompile(`^(ssh-[a-z0-9]+|ecdsa-sha2-nistp[0-9]+|sk-[a-z0-9@.-]+)$`)

// knownHosts turns the stored host key into known_hosts content. A bare "type base64" key is bound to the
// host of the URL; full lines are used as stored, except marker lines and wildcard or negated host patterns,
// which would weaken the pin.
func knownHosts(hostKey string, target remote) (string, error) {
	var out strings.Builder
	n := 0
	for _, line := range strings.Split(hostKey, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		switch {
		case strings.HasPrefix(line, "@"), len(fields) < 2:
			return "", errors.New("host key must be a plain known_hosts line or a public key")
		case keyTypeRe.MatchString(fields[0]):
			host := target.host
			if target.port != "" && target.port != "22" {
				host = "[" + host + "]:" + target.port
			}
			line = host + " " + strings.Join(fields[:2], " ")
		case strings.ContainsAny(fields[0], "*?!") || len(fields) < 3 || !keyTypeRe.MatchString(fields[1]):
			return "", errors.New("host key must be a plain known_hosts line or a public key")
		}
		out.WriteString(line + "\n")
		n++
	}
	if n == 0 {
		return "", errors.New("host key is empty")
	}
	return out.String(), nil
}

func parseTags(out string) []string {
	var tags []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		_, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		name, isTag := strings.CutPrefix(ref, "refs/tags/")
		if !ok || !isTag || name == "" || strings.HasSuffix(name, "^{}") || seen[name] {
			continue
		}
		seen[name] = true
		tags = append(tags, name)
	}
	return tags
}

var userinfoRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/@\s'"]*@`)

func redactURL(s string) string { return userinfoRe.ReplaceAllString(s, "${1}") }

// failureText builds the error detail from git's stderr with every secret value and URL userinfo removed.
func failureText(runErr error, stderr string, cred Credential) string {
	text := redactURL(stderr)
	secrets := []string{cred.Token, cred.User}
	for _, l := range strings.Split(cred.PrivateKey, "\n") {
		secrets = append(secrets, strings.TrimSpace(l))
	}
	for _, s := range secrets {
		if len(s) >= 3 {
			text = strings.ReplaceAll(text, s, "[redacted]")
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		text = runErr.Error()
	}
	if len(text) > maxErrorText {
		text = text[:maxErrorText] + "..."
	}
	return text
}

// capped keeps the head of what is written and counts whether more arrived; it never fails the writer.
type capped struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (c *capped) Write(p []byte) (int, error) {
	n := len(p)
	if room := c.limit - c.buf.Len(); n > room {
		c.over = true
		p = p[:max(room, 0)]
	}
	c.buf.Write(p)
	return n, nil
}

func (c *capped) String() string { return c.buf.String() }
