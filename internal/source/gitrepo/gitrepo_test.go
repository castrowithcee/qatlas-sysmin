package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// sshLogin is the made-up user and host of the SSH test URLs.
var sshLogin = "git" + "@" + "git.example.invalid"

// Made-up secret values.
const (
	demoGitToken  = "demo-git-token-not-real"
	demoGitUser   = "demo-git-user"
	demoKeyLine   = "demo-private-key-line-not-real"
	demoHostKey   = "ssh-ed25519 AAAAdemoHostKeyNotReal"
	demoSSHScheme = "ssh://"
)

func gitOut(t *testing.T, home, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Demo", "GIT_AUTHOR_EMAIL=demo@example.invalid",
		"GIT_COMMITTER_NAME=Demo", "GIT_COMMITTER_EMAIL=demo@example.invalid",
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// bareRepo creates a bare repository holding the given tags (one of them annotated) and returns its parent
// directory and path.
func bareRepo(t *testing.T, tags ...string) (root, bare string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root = t.TempDir()
	home := filepath.Join(root, "home")
	work := filepath.Join(root, "work")
	bare = filepath.Join(root, "srv", "demo.git")
	for _, d := range []string{home, work, bare} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitOut(t, home, bare, "init", "--bare", "-q")
	gitOut(t, home, work, "init", "-q")
	gitOut(t, home, work, "commit", "-q", "--allow-empty", "-m", "demo")
	for i, tag := range tags {
		if i == 0 {
			gitOut(t, home, work, "tag", "-a", "-m", "demo", tag)
		} else {
			gitOut(t, home, work, "tag", tag)
		}
	}
	gitOut(t, home, work, "push", "-q", bare, "--tags")
	return root, bare
}

func fileLister(opts Options) *Lister {
	opts.extraProtocols = []string{"file", "http"}
	return New(opts)
}

func TestTagsFromLocalRepo(t *testing.T) {
	_, bare := bareRepo(t, "v1.0.0", "v1.1.0", "2.0.0")
	tags, err := fileLister(Options{}).Tags(context.Background(), "file://"+bare, Credential{})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(tags)
	if want := []string{"2.0.0", "v1.0.0", "v1.1.0"}; !slices.Equal(tags, want) {
		t.Errorf("tags = %v, want %v", tags, want)
	}
}

func httpBackend(t *testing.T, root string) *httptest.Server {
	t.Helper()
	backend := filepath.Join(execPath(t), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git http-backend is not installed")
	}
	cgiHandler := &cgi.Handler{
		Path:       backend,
		Env:        []string{"GIT_PROJECT_ROOT=" + filepath.Join(root, "srv"), "GIT_HTTP_EXPORT_ALL=1", "HOME=" + filepath.Join(root, "home"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"},
		InheritEnv: []string{"PATH"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != demoGitUser || pass != demoGitToken {
			w.Header().Set("WWW-Authenticate", `Basic realm="demo"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		cgiHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func execPath(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Skip("git is not usable")
	}
	return strings.TrimSpace(string(out))
}

// recordRun remembers the command of each call and then runs it.
type recordRun struct{ cmds []*exec.Cmd }

func (r *recordRun) run(cmd *exec.Cmd) error {
	r.cmds = append(r.cmds, cmd)
	return cmd.Run()
}

func assertNoSecret(t *testing.T, where string, parts []string, secrets ...string) {
	t.Helper()
	for _, p := range parts {
		for _, s := range secrets {
			if strings.Contains(p, s) {
				t.Errorf("%s contains a secret: %q", where, p)
			}
		}
	}
}

func TestHTTPSCredentialReachesGitWithoutArgvOrEnv(t *testing.T) {
	root, _ := bareRepo(t, "v1.0.0", "v1.2.0")
	srv := httpBackend(t, root)
	rec := &recordRun{}
	l := fileLister(Options{Run: rec.run})

	tags, err := l.Tags(context.Background(), srv.URL+"/demo.git", Credential{Kind: KindHTTPS, User: demoGitUser, Token: demoGitToken})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(tags)
	if !slices.Equal(tags, []string{"v1.0.0", "v1.2.0"}) {
		t.Errorf("tags = %v", tags)
	}
	cmd := rec.cmds[0]
	assertNoSecret(t, "argument", cmd.Args, demoGitToken, demoGitUser)
	assertNoSecret(t, "environment", cmd.Env, demoGitToken, demoGitUser)
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "SSH_AUTH_SOCK=", "GIT_ALLOW_PROTOCOL=https:ssh:file:http"} {
		if !slices.Contains(cmd.Env, want) {
			t.Errorf("environment lacks %s", want)
		}
	}
	if !slices.Contains(cmd.Args, "credential.helper=") {
		t.Errorf("credential helpers are not disabled: %v", cmd.Args)
	}
	if _, err := os.Stat(cmd.Dir); !os.IsNotExist(err) {
		t.Errorf("temporary directory %s was not removed", cmd.Dir)
	}
}

func TestHTTPSWrongTokenErrorHasNoSecret(t *testing.T) {
	root, _ := bareRepo(t, "v1.0.0")
	srv := httpBackend(t, root)
	_, err := fileLister(Options{}).Tags(context.Background(), srv.URL+"/demo.git", Credential{Kind: KindHTTPS, User: demoGitUser, Token: demoGitToken + "-wrong"})
	if err == nil {
		t.Fatal("expected an authentication error")
	}
	assertNoSecret(t, "error", []string{err.Error()}, demoGitToken, "-wrong")
}

func TestFailureTextRedacts(t *testing.T) {
	cred := Credential{Kind: KindHTTPS, User: demoGitUser, Token: demoGitToken, PrivateKey: "-----BEGIN\n" + demoKeyLine + "\n-----END"}
	// The URL is built at run time so the test source holds no literal login.
	leaky := "https://" + demoGitUser + ":" + demoGitToken + "@git.example.invalid/x.git"
	l := New(Options{Run: func(cmd *exec.Cmd) error {
		cmd.Stderr.Write([]byte("fatal: unable to access '" + leaky + "': token " + demoGitToken + "\n"))
		return errors.New("exit status 128")
	}})
	_, err := l.Tags(context.Background(), "https://git.example.invalid/x.git", Credential{Kind: KindHTTPS, User: demoGitUser, Token: demoGitToken})
	if err == nil {
		t.Fatal("expected an error")
	}
	assertNoSecret(t, "error", []string{err.Error()}, demoGitToken, demoGitUser)
	assertNoSecret(t, "key error", []string{failureText(errors.New("x"), "bad key "+demoKeyLine, cred)}, demoKeyLine)
	if got := failureText(errors.New("x"), strings.Repeat("a ", 2000), cred); len(got) > maxErrorText+3 {
		t.Errorf("error text is %d bytes long", len(got))
	}
}

func TestSSHOptions(t *testing.T) {
	var gotEnv []string
	var keyMode, hostsMode os.FileMode
	var hosts string
	var dir string
	l := New(Options{Run: func(cmd *exec.Cmd) error {
		gotEnv, dir = cmd.Env, cmd.Dir
		fi, err := os.Stat(filepath.Join(cmd.Dir, "id"))
		if err != nil {
			t.Error(err)
			return nil
		}
		keyMode = fi.Mode().Perm()
		fi, _ = os.Stat(filepath.Join(cmd.Dir, "known_hosts"))
		hostsMode = fi.Mode().Perm()
		b, _ := os.ReadFile(filepath.Join(cmd.Dir, "known_hosts"))
		hosts = string(b)
		return nil
	}})
	cred := Credential{Kind: KindSSH, PrivateKey: "-----BEGIN\n" + demoKeyLine + "\n-----END", HostKey: demoHostKey}
	if _, err := l.Tags(context.Background(), demoSSHScheme+sshLogin+":2222/team/demo.git", cred); err != nil {
		t.Fatal(err)
	}
	var sshCmd string
	for _, e := range gotEnv {
		if v, ok := strings.CutPrefix(e, "GIT_SSH_COMMAND="); ok {
			sshCmd = v
		}
	}
	for _, want := range []string{"StrictHostKeyChecking=yes", "UserKnownHostsFile='" + dir, "IdentitiesOnly=yes", "IdentityFile='" + dir, "BatchMode=yes", "-F /dev/null"} {
		if !strings.Contains(sshCmd, want) {
			t.Errorf("ssh command lacks %q: %s", want, sshCmd)
		}
	}
	assertNoSecret(t, "environment", gotEnv, demoKeyLine)
	if !slices.Contains(gotEnv, "SSH_AUTH_SOCK=") {
		t.Error("ssh agent is not disabled")
	}
	if keyMode != 0o600 || hostsMode != 0o600 {
		t.Errorf("file modes: key %v, known_hosts %v", keyMode, hostsMode)
	}
	if want := "[git.example.invalid]:2222 " + demoHostKey + "\n"; hosts != want {
		t.Errorf("known_hosts = %q, want %q", hosts, want)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("temporary directory %s was not removed", dir)
	}
}

func TestKnownHosts(t *testing.T) {
	target := remote{scheme: KindSSH, host: "git.example.invalid"}
	line := "git.example.invalid " + demoHostKey
	if got, err := knownHosts(line, target); err != nil || got != line+"\n" {
		t.Errorf("full line: %q, %v", got, err)
	}
	for _, bad := range []string{"", "@cert-authority * " + demoHostKey, "* " + demoHostKey, "!host " + demoHostKey, "onlyone"} {
		if _, err := knownHosts(bad, target); err == nil {
			t.Errorf("host key %q was accepted", bad)
		}
	}
}

func TestURLChecks(t *testing.T) {
	l := New(Options{Run: func(*exec.Cmd) error { return nil }})
	withUser := "https://" + "someone:" + "pw" + "@git.example.invalid/x.git"
	userOnly := "https://" + "someone" + "@git.example.invalid/x.git"
	sshPass := demoSSHScheme + "git:" + "pw" + "@git.example.invalid/x.git"
	for _, raw := range []string{withUser, userOnly, sshPass, "", "-oProxyCommand=x", "file:///tmp/x", "http://git.example.invalid/x.git", "ftp://git.example.invalid/x"} {
		if _, err := l.Tags(context.Background(), raw, Credential{}); err == nil {
			t.Errorf("url %q was accepted", raw)
		}
	}
	for _, ok := range []string{"https://git.example.invalid/x.git", sshLogin + ":team/x.git", demoSSHScheme + sshLogin + "/x.git"} {
		if _, err := l.Tags(context.Background(), ok, Credential{}); err != nil {
			t.Errorf("url %q: %v", ok, err)
		}
	}
}

func TestCredentialMismatch(t *testing.T) {
	l := New(Options{Run: func(*exec.Cmd) error { return nil }})
	https := Credential{Kind: KindHTTPS, User: demoGitUser, Token: demoGitToken}
	ssh := Credential{Kind: KindSSH, PrivateKey: demoKeyLine, HostKey: demoHostKey}
	for name, c := range map[string]struct {
		url  string
		cred Credential
	}{
		"https login on ssh url": {demoSSHScheme + sshLogin + "/x.git", https},
		"ssh key on https url":   {"https://git.example.invalid/x.git", ssh},
		"empty token":            {"https://git.example.invalid/x.git", Credential{Kind: KindHTTPS}},
		"missing host key":       {demoSSHScheme + sshLogin + "/x.git", Credential{Kind: KindSSH, PrivateKey: demoKeyLine}},
	} {
		if _, err := l.Tags(context.Background(), c.url, c.cred); !errors.Is(err, ErrAuth) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestTimeoutKillsProcess(t *testing.T) {
	l := New(Options{Timeout: 200 * time.Millisecond, Run: func(cmd *exec.Cmd) error {
		sleep := exec.Command("sleep", "30")
		cmd.Path, cmd.Args = sleep.Path, sleep.Args
		return cmd.Run()
	}})
	start := time.Now()
	_, err := l.Tags(context.Background(), "https://git.example.invalid/x.git", Credential{})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 10*time.Second {
		t.Errorf("err = %v after %v", err, time.Since(start))
	}
}

func TestOutputIsCapped(t *testing.T) {
	l := New(Options{Run: func(cmd *exec.Cmd) error {
		cmd.Stdout.Write(bytes.Repeat([]byte("x"), maxStdout+1))
		cmd.Stderr.Write(bytes.Repeat([]byte("y"), maxStderr*2))
		return nil
	}})
	if _, err := l.Tags(context.Background(), "https://git.example.invalid/x.git", Credential{}); err == nil {
		t.Error("oversized output was accepted")
	}
	var c capped
	c.limit = 4
	if n, err := c.Write([]byte("abcdefgh")); n != 8 || err != nil || c.String() != "abcd" || !c.over {
		t.Errorf("capped: %d %v %q", n, err, c.String())
	}
}

func TestParseTags(t *testing.T) {
	out := "aaa\trefs/tags/v1\nbbb\trefs/tags/v1^{}\nccc\trefs/heads/main\nddd\trefs/tags/v1\neee\trefs/tags/feature/x\nbroken\n"
	if got := parseTags(out); !slices.Equal(got, []string{"v1", "feature/x"}) {
		t.Errorf("tags = %v", got)
	}
}
