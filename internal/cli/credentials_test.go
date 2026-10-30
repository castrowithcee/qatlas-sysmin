package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const (
	demoToken   = "demo-token-not-real"
	demoToken2  = "demo-token-not-real-two"
	demoKeyBody = "-----BEGIN DEMO KEY-----\nZGVtby1rZXktbm90LXJlYWw=\nZGVtby1rZXktbm90LXJlYWwtMg==\n-----END DEMO KEY-----"
	demoPass    = "example-passphrase-1"
)

// credEnv is a service for the credential and channel commands, with a log of everything the commands
// print so a test can search it for secrets.
type credEnv struct {
	t      *testing.T
	sock   string
	state  string
	config string

	mu  sync.Mutex
	all strings.Builder
}

func newCredEnv(t *testing.T, bound bool) *credEnv {
	sock, state, config := startServiceDirs(t, bound, projectSetup(t))
	return &credEnv{t: t, sock: sock, state: state, config: config}
}

// run executes the command with the given standard input and secret prompt reader (nil: none attached).
func (e *credEnv) run(stdin string, ask func(string) ([]byte, error), args ...string) (int, string, string) {
	e.t.Helper()
	if ask == nil {
		ask = func(string) ([]byte, error) { return nil, errNoTerminal }
	}
	opts := &Options{Redactor: &redact.Redactor{}, readSecret: ask}
	root := newRootCommand(opts)
	root.SetIn(strings.NewReader(stdin))
	var out, errOut bytes.Buffer
	code := run(root, opts, append(args, "--socket", e.sock), &out, &errOut)
	e.mu.Lock()
	e.all.WriteString(out.String() + "\n" + errOut.String() + "\n")
	e.mu.Unlock()
	return code, out.String(), errOut.String()
}

func (e *credEnv) json(stdin string, args ...string) (int, string, string) {
	e.t.Helper()
	return e.run(stdin, nil, append(args, "--format", "json")...)
}

// noSecretAnywhere fails if a value appears in anything printed so far or in a file of the audit log.
func (e *credEnv) noSecretAnywhere(values ...string) {
	e.t.Helper()
	e.mu.Lock()
	printed := e.all.String()
	e.mu.Unlock()
	files, _ := filepath.Glob(filepath.Join(e.state, "logs", "*"))
	if len(files) == 0 {
		e.t.Error("no audit log files found")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			e.t.Fatal(err)
		}
		printed += "\n" + string(data)
	}
	for _, v := range values {
		if strings.Contains(printed, v) {
			e.t.Errorf("the value %q is in the output or the audit log", v)
		}
	}
}

func (e *credEnv) vault() *vault.Vault {
	e.t.Helper()
	v := vault.New(filepath.Join(e.state, "vault"))
	if _, err := v.Unlock(demoPass); err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *credEnv) admin() *client.Client {
	e.t.Helper()
	c, err := client.Dial(e.sock)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { c.Close() })
	if err := c.OpenSession(demoPass); err != nil {
		e.t.Fatal(err)
	}
	return c
}

// shownAll lists the open approvals as a client shows them before an all-at-once request.
func shownAll(t *testing.T, c *client.Client) []control.ShownApproval {
	t.Helper()
	items, err := c.Approvals()
	if err != nil {
		t.Fatal(err)
	}
	shown := make([]control.ShownApproval, 0, len(items))
	for _, it := range items {
		shown = append(shown, it.Shown())
	}
	return shown
}

// approveAll approves what c lists now, as a client does after it showed the list.
func approveAll(t *testing.T, c *client.Client) (control.ApprovalsAllResult, error) {
	t.Helper()
	return c.ApproveAll(shownAll(t, c))
}

func TestCredentialSetFromStandardInputListAndRemove(t *testing.T) {
	e := newCredEnv(t, true)
	code, stdout, stderr := e.json(demoToken+"\n", "credential", "set", "demo-registry", "--kind", "registry", "--user", "demo-user", "--from-stdin")
	obj := jsonObject(t, stdout)
	if code != exitOK || obj["credential"] != "demo-registry" || obj["action"] != "created" || obj["state"] != "approved" || obj["id"] == "" {
		t.Fatalf("set: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	en, err := e.vault().GetByName("demo-registry")
	if err != nil || en.Fields[vault.FieldToken].Reveal() != demoToken || en.Fields[vault.FieldUser].Reveal() != "demo-user" {
		t.Fatalf("stored entry: %v", err)
	}

	for _, format := range []string{"json", "table", "compact"} {
		code, stdout, stderr = e.run("", nil, "credential", "list", "--format", format)
		if code != exitOK || !strings.Contains(stdout, "demo-registry") || !strings.Contains(stdout, obj["id"].(string)) {
			t.Fatalf("list %s: code = %d, stdout = %q, stderr = %q", format, code, stdout, stderr)
		}
	}

	// A second set with the same name replaces it under a new id.
	code, stdout, stderr = e.json(demoToken2, "credential", "set", "demo-registry", "--kind", "registry", "--user", "demo-user", "--from-stdin")
	repl := jsonObject(t, stdout)
	if code != exitOK || repl["action"] != "replaced" || repl["id"] == obj["id"] {
		t.Fatalf("replace: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	// A different kind under the same name is refused.
	code, _, stderr = e.json(demoToken, "credential", "set", "demo-registry", "--kind", "secret", "--from-stdin")
	if code != exitRuntime || !strings.Contains(stderr, "another kind") {
		t.Fatalf("kind conflict: code = %d, stderr = %q", code, stderr)
	}

	code, stdout, stderr = e.json("", "credential", "remove", "demo-registry")
	if code != exitOK || jsonObject(t, stdout)["action"] != "removed" {
		t.Fatalf("remove: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, _ = e.json("", "credential", "list")
	if code != exitOK || strings.Contains(stdout, "demo-registry") {
		t.Fatalf("list after remove: %q", stdout)
	}
	code, _, stderr = e.json("", "credential", "remove", "demo-registry")
	if code != exitRuntime || !strings.Contains(stderr, "does not exist") {
		t.Fatalf("second remove: code = %d, stderr = %q", code, stderr)
	}
	e.noSecretAnywhere(demoToken, demoToken2)
}

func TestCredentialSetKeepsAMultiLineKeyAndTrimsOneLineEnd(t *testing.T) {
	e := newCredEnv(t, true)
	hostKey := "ssh-ed25519 AAAAdemohostkeynotreal"
	code, stdout, stderr := e.json(demoKeyBody+"\n", "credential", "set", "demo-deploy", "--kind", "ssh-key", "--host-key", hostKey, "--from-stdin")
	if code != exitOK {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	en, err := e.vault().GetByName("demo-deploy")
	if err != nil || en.Fields[vault.FieldPrivateKey].Reveal() != demoKeyBody || en.Fields[vault.FieldHostKey].Reveal() != hostKey {
		t.Fatalf("stored entry: %v", err)
	}
	// Only one line end goes; the value is otherwise as given.
	if code, _, _ = e.json("demo-value\n\n", "credential", "set", "demo-blank", "--kind", "secret", "--from-stdin"); code != exitOK {
		t.Fatal("set failed")
	}
	if en, _ := e.vault().GetByName("demo-blank"); en.Fields[vault.FieldValue].Reveal() != "demo-value\n" {
		t.Fatalf("value = %q", en.Fields[vault.FieldValue].Reveal())
	}
	e.noSecretAnywhere("ZGVtby1rZXktbm90LXJlYWw=", "demo-value")
}

func TestCredentialSetNeverTakesASecretAsArgumentOrEnvironment(t *testing.T) {
	e := newCredEnv(t, true)
	t.Setenv("SYSMIN_TOKEN", demoToken)
	t.Setenv("SYSMIN_VALUE", demoToken)
	t.Setenv("TOKEN", demoToken)
	t.Setenv("DEMO_PASSWORD", demoToken)
	for name, args := range map[string][]string{
		"positional":  {"credential", "set", "demo-x", demoToken, "--kind", "secret"},
		"flag token":  {"credential", "set", "demo-x", "--kind", "registry", "--user", "u", "--token", demoToken},
		"flag value":  {"credential", "set", "demo-x", "--kind", "secret", "--value=" + demoToken},
		"flag stdin":  {"credential", "set", "demo-x", "--kind", "smtp", "--user", "u", "--password", demoToken},
		"two names":   {"credential", "set", "demo-x", "demo-y", "--kind", "secret"},
		"no name":     {"credential", "set", "--kind", "secret"},
		"no kind":     {"credential", "set", "demo-x"},
		"unknown":     {"credential", "set", "demo-x", "--kind", "nope"},
		"extra list":  {"credential", "list", demoToken},
		"extra remov": {"credential", "remove", "demo-x", demoToken},
	} {
		code, stdout, stderr := e.run("", nil, args...)
		if code != exitUsage {
			t.Errorf("%s: code = %d, stdout = %q, stderr = %q", name, code, stdout, stderr)
		}
	}
	// With the environment set and no terminal, the command asks for the secret and fails.
	code, stdout, stderr := e.run("", nil, "credential", "set", "demo-x", "--kind", "secret")
	if code != exitUsage || !strings.Contains(stderr, "--from-stdin") {
		t.Fatalf("no terminal: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	// An options that does not belong to the kind, or a missing one, is a usage error as well.
	if code, _, _ = e.run("x", nil, "credential", "set", "demo-x", "--kind", "secret", "--user", "u", "--from-stdin"); code != exitUsage {
		t.Errorf("--user for a secret: code = %d", code)
	}
	if code, _, _ = e.run("x", nil, "credential", "set", "demo-x", "--kind", "registry", "--from-stdin"); code != exitUsage {
		t.Errorf("registry without --user: code = %d", code)
	}
	if code, _, stderr = e.run("", nil, "credential", "set", "demo-x", "--kind", "secret", "--from-stdin"); code != exitUsage || !strings.Contains(stderr, "empty") {
		t.Errorf("empty standard input: code = %d, stderr = %q", code, stderr)
	}
	if list, _, _ := e.json("", "credential", "list"); list != exitOK {
		t.Fatal("list failed")
	}
	_, stdout, _ = e.json("", "credential", "list")
	if strings.Contains(stdout, "demo-x") {
		t.Fatalf("a credential was stored: %q", stdout)
	}
	e.noSecretAnywhere(demoToken)
}

func TestCredentialSetAsksForTheSecretTwiceAtAHiddenPrompt(t *testing.T) {
	e := newCredEnv(t, true)
	var prompts []string
	answers := [][]byte{[]byte(demoToken), []byte(demoToken)}
	ask := func(p string) ([]byte, error) {
		prompts = append(prompts, p)
		a := answers[0]
		answers = answers[1:]
		return a, nil
	}
	code, stdout, stderr := e.run("must not be read", ask, "credential", "set", "demo-hook", "--kind", "secret", "--format", "json")
	if code != exitOK || len(prompts) != 2 || strings.Contains(strings.ToLower(strings.Join(prompts, " ")), "passphrase") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q, prompts = %q", code, stdout, stderr, prompts)
	}
	if en, err := e.vault().GetByName("demo-hook"); err != nil || en.Fields[vault.FieldValue].Reveal() != demoToken {
		t.Fatalf("stored entry: %v", err)
	}

	// Two different entries store nothing.
	answers = [][]byte{[]byte(demoToken), []byte(demoToken2)}
	code, _, stderr = e.run("", ask, "credential", "set", "demo-other", "--kind", "secret")
	if code != exitUsage || !strings.Contains(stderr, "differ") {
		t.Fatalf("mismatch: code = %d, stderr = %q", code, stderr)
	}
	if _, err := e.vault().GetByName("demo-other"); err == nil {
		t.Fatal("a mismatching entry was stored")
	}
	// A private key spans lines and is not typed.
	code, _, stderr = e.run("", ask, "credential", "set", "demo-key", "--kind", "ssh-key", "--host-key", "ssh-ed25519 AAAA")
	if code != exitUsage || !strings.Contains(stderr, "--from-stdin") {
		t.Fatalf("typed key: code = %d, stderr = %q", code, stderr)
	}
	// A failing terminal ends the command.
	code, _, _ = e.run("", func(string) ([]byte, error) { return nil, errors.New("broken") }, "credential", "set", "demo-hook2", "--kind", "secret")
	if code == exitOK {
		t.Fatal("a broken terminal succeeded")
	}
	e.noSecretAnywhere(demoToken, demoToken2)
}

func TestCredentialReplaceOpensTheProjectThatUsesIt(t *testing.T) {
	e := newCredEnv(t, true)
	if code, _, stderr := e.json(demoToken, "credential", "set", "demo-registry", "--kind", "registry", "--user", "demo-user", "--from-stdin"); code != exitOK {
		t.Fatalf("set: %q", stderr)
	}
	args := append(append([]string(nil), demoAddArgs...), "--image-credential", "app=demo-registry")
	if code, _, stderr := e.json("", args...); code != exitOK {
		t.Fatalf("project add: %q", stderr)
	}
	adm := e.admin()
	if _, err := approveAll(t, adm); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := e.json(demoToken2, "credential", "set", "demo-registry", "--kind", "registry", "--user", "demo-user", "--from-stdin")
	obj := jsonObject(t, stdout)
	if code != exitOK || obj["state"] != "pending approval" || obj["pending"] != "demo-app" ||
		!strings.Contains(obj["message"].(string), "not approved yet") {
		t.Fatalf("replace: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, _ = e.json("", "approvals", "list")
	if code != exitOK || !strings.Contains(stdout, `"project":"demo-app"`) || !strings.Contains(stdout, `"kind":"credential"`) {
		t.Fatalf("approvals: code = %d, stdout = %q", code, stdout)
	}
	code, stdout, _ = e.json("", "credential", "list")
	if code != exitOK || !strings.Contains(stdout, "project demo-app (current, approved)") {
		t.Fatalf("list: code = %d, stdout = %q", code, stdout)
	}

	// Used: the command line cannot remove it; a session can, and the project stays open, not approved.
	code, stdout, stderr = e.json("", "credential", "remove", "demo-registry")
	if code != exitRuntime || !strings.Contains(stderr, "in use") || !strings.Contains(stdout, "in-use") || !strings.Contains(stdout, "project:demo-app") {
		t.Fatalf("remove used: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if _, err := e.vault().GetByName("demo-registry"); err != nil {
		t.Fatalf("a refused removal deleted it: %v", err)
	}
	res, err := adm.RemoveCredential("demo-registry")
	if err != nil || res.State != "pending approval" || len(res.Approved) != 0 {
		t.Fatalf("remove in a session = %+v, %v", res, err)
	}
	e.noSecretAnywhere(demoToken, demoToken2)
}

func TestCredentialCommandsNeedAnUnlockedVault(t *testing.T) {
	e := newCredEnv(t, false)
	for name, c := range map[string]struct {
		stdin string
		args  []string
	}{
		"set":     {demoToken, []string{"credential", "set", "demo-x", "--kind", "secret", "--from-stdin"}},
		"list":    {"", []string{"credential", "list"}},
		"remove":  {"", []string{"credential", "remove", "demo-x"}},
		"channel": {"", []string{"notify", "channel", "add", "ops-hook", "--type", "webhook", "--url", "https://hooks.example.com/x"}},
		"clist":   {"", []string{"notify", "channel", "remove", "ops-hook"}},
	} {
		code, stdout, stderr := e.run(c.stdin, nil, c.args...)
		if code != exitRuntime || stdout != "" || !strings.Contains(stderr, "vault is locked") {
			t.Errorf("%s: code = %d, stdout = %q, stderr = %q", name, code, stdout, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(e.state, "vault", "pending")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(e.state, "vault", "pending"))
		if len(entries) > 0 {
			t.Fatalf("a credential was queued while the vault was locked: %v", entries)
		}
	}
	e.noSecretAnywhere(demoToken)
}

func TestNotifyChannelLifecycle(t *testing.T) {
	e := newCredEnv(t, true)
	code, stdout, stderr := e.json("", "notify", "channel", "add", "ops-hook", "--type", "webhook", "--url", "https://hooks.example.com/demo")
	obj := jsonObject(t, stdout)
	if code != exitOK || obj["channel"] != "ops-hook" || obj["approval"] != "@channels" || obj["state"] != "pending approval" ||
		!strings.Contains(obj["message"].(string), "not approved yet") {
		t.Fatalf("add: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, _ = e.json("", "approvals", "list")
	if code != exitOK || !strings.Contains(stdout, `"project":"@channels"`) || !strings.Contains(stdout, `"kind":"new"`) {
		t.Fatalf("approvals: code = %d, stdout = %q", code, stdout)
	}
	code, stdout, stderr = e.json("", "notify", "channel", "list")
	rows := jsonRows(t, stdout)
	if code != exitOK || len(rows) != 1 || rows[0]["name"] != "ops-hook" || rows[0]["type"] != "webhook" || rows[0]["state"] != "pending approval" {
		t.Fatalf("list: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if code, _, _ = e.json("", "notify", "channel", "add", "ops-hook", "--type", "webhook", "--url", "https://hooks.example.com/demo"); code != exitRuntime {
		t.Fatalf("add of an existing channel: code = %d", code)
	}

	code, stdout, stderr = e.json("", "notify", "channel", "remove", "ops-hook")
	obj = jsonObject(t, stdout)
	// Nothing approved it, so its removal leaves nothing open.
	if code != exitOK || obj["action"] != "removed" || obj["state"] != "approved" {
		t.Fatalf("remove: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, _, stderr = e.json("", "notify", "channel", "remove", "ops-hook")
	if code != exitRuntime || !strings.Contains(stderr, "does not exist") {
		t.Fatalf("second remove: code = %d, stderr = %q", code, stderr)
	}
}

func TestNotifyChannelInASessionIsApprovedAndListsNoSecret(t *testing.T) {
	e := newCredEnv(t, true)
	if code, _, stderr := e.json(demoToken, "credential", "set", "demo-smtp", "--kind", "smtp", "--user", "demo-user", "--from-stdin"); code != exitOK {
		t.Fatalf("set: %q", stderr)
	}
	adm := e.admin()
	res, err := adm.AddChannel(controlChannel())
	if err != nil || res.State != "approved" {
		t.Fatalf("add in a session = %+v, %v", res, err)
	}
	for _, format := range []string{"json", "table", "compact"} {
		code, stdout, stderr := e.run("", nil, "notify", "channel", "list", "--format", format)
		if code != exitOK || !strings.Contains(stdout, "ops-mail") || !strings.Contains(stdout, "demo-smtp") || !strings.Contains(stdout, "approved") || strings.Contains(stdout, "pending") {
			t.Fatalf("list %s: code = %d, stdout = %q, stderr = %q", format, code, stdout, stderr)
		}
	}
	code, stdout, _ := e.json("", "credential", "list")
	if code != exitOK || !strings.Contains(stdout, "channel ops-mail (current, approved)") {
		t.Fatalf("credential list: %q", stdout)
	}
	e.noSecretAnywhere(demoToken)
}

func TestNotifyChannelInvalidInputListsProblems(t *testing.T) {
	e := newCredEnv(t, true)
	code, stdout, stderr := e.json("", "notify", "channel", "add", "ops-mail", "--type", "smtp", "--host", "smtp.example.com",
		"--from", "not-an-address", "--to", "ops@example.com", "--credential", "demo-missing")
	rows := jsonRows(t, stdout)
	if code != exitUsage || len(rows) < 2 || !strings.Contains(stderr, "problems found; nothing was changed") {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	codes := map[string]bool{}
	for _, r := range rows {
		codes[r["code"].(string)] = true
	}
	if !codes["invalid-channel"] || !codes["unknown-credential"] {
		t.Fatalf("problems = %v", rows)
	}
	if code, _, _ = e.json("", "notify", "channel", "add", "ops-x"); code != exitUsage {
		t.Errorf("add without a type: code = %d", code)
	}
	if code, _, _ = e.json("", "notify", "channel", "add", "ops-x", "ops-y", "--type", "slack"); code != exitUsage {
		t.Errorf("add with two names: code = %d", code)
	}
	code, stdout, _ = e.json("", "notify", "channel", "list")
	if code != exitOK || strings.Contains(stdout, "ops-mail") {
		t.Fatalf("an invalid channel was stored: %q", stdout)
	}
}

// The credential and channel commands never ask for the vault passphrase, not even when the output is a
// terminal. The secret itself comes from standard input here, so nothing is asked at all.
func TestCredentialCommandsDoNotPromptForAPassphraseOnATerminal(t *testing.T) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminal: %v", err)
	}
	defer master.Close()
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("cannot unlock the pseudo terminal: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("cannot name the pseudo terminal: %v", err)
	}
	slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot open the pseudo terminal: %v", err)
	}
	defer slave.Close()
	if !isTerminal(slave) {
		t.Skip("the pseudo terminal is no terminal")
	}

	e := newCredEnv(t, true)
	runOn := func(stdin io.Reader, args ...string) int {
		opts := &Options{Redactor: &redact.Redactor{}}
		root := newRootCommand(opts)
		root.SetIn(stdin)
		root.SetOut(slave)
		root.SetErr(io.Discard)
		root.SetArgs(append(args, "--socket", e.sock, "--format", "json"))
		done := make(chan int, 1)
		go func() {
			if _, err := root.ExecuteC(); err != nil {
				done <- exitCode(err)
				return
			}
			done <- exitOK
		}()
		select {
		case code := <-done:
			return code
		case <-time.After(10 * time.Second):
			t.Fatal("the command waits for input on the terminal")
			return -1
		}
	}
	if code := runOn(strings.NewReader(demoToken), "credential", "set", "demo-hook", "--kind", "secret", "--from-stdin"); code != exitOK {
		t.Fatalf("set: code = %d", code)
	}
	if code := runOn(strings.NewReader(""), "credential", "list"); code != exitOK {
		t.Fatalf("list: code = %d", code)
	}
	if code := runOn(strings.NewReader(""), "notify", "channel", "add", "ops-hook", "--type", "webhook", "--url", "https://hooks.example.com/x"); code != exitOK {
		t.Fatalf("channel add: code = %d", code)
	}
	_ = master.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 16384)
	got, _ := master.Read(buf)
	lower := strings.ToLower(string(buf[:got]))
	if strings.Contains(lower, "passphrase") || strings.Contains(lower, "password") {
		t.Fatalf("the terminal shows a prompt: %q", lower)
	}
	if !strings.Contains(lower, "demo-hook") || strings.Contains(lower, demoToken) {
		t.Fatalf("terminal output: %q", lower)
	}
}

func controlChannel() control.ChannelParams {
	return control.ChannelParams{
		Name: "ops-mail", Type: "smtp", Host: "smtp.example.com", From: "sysmin@example.com",
		To: []string{"ops@example.com"}, Security: "starttls", Credential: "demo-smtp",
	}
}
