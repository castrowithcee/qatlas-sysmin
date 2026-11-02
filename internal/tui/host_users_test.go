package tui

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os/exec"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// inputMarker stands for a secret typed into a form; no view, argument or message may ever contain it.
const inputMarker = "invented-input-marker-7731"

// demoKey is a public key generated for the test, as one authorized_keys line, with its fingerprint.
func demoKey(t *testing.T, comment string) (line, fingerprint string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " " + comment, ssh.FingerprintSHA256(key)
}

// userModel is the Users page over the real user actions and a fake sudo. demo-user is an admin who is not
// enabled and has one key read; demo-ops is a group demo-user is not in.
func userModel(t *testing.T, w, h int) (*Model, *fakeSudo, *countingHost, string) {
	t.Helper()
	m, f, host := hostChangeModel(t, w, h)
	m.hostReg = nil
	host.groups = append(host.groups, hostadmin.Group{Name: "demo-ops", GID: 1003})
	old := hostUID
	hostUID = func() int { return 4242 }
	t.Cleanup(func() { hostUID = old })
	_, fp := demoKey(t, "x")
	for i := range m.hst.users.all {
		if m.hst.users.all[i].Name == "demo-user" {
			m.hst.users.all[i].SSHKeys = []hostadmin.SSHKey{{Type: "ssh-ed25519", Comment: "demo-user@laptop", Fingerprint: fp}}
		}
	}
	m.fillHostUsers()
	return m, f, host, fp
}

// flat is the screen as one line of words: the borders of the panels between the wrapped lines of a modal are
// taken out, so a sentence that wraps is found as it reads. The page behind the modal is in it too.
func flat(m *Model) string {
	return flatText(strings.NewReplacer("│", " ", "║", " ").Replace(plain(m)))
}

func save(m *Model) {
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyF2})
	drain(m, cmd)
}

func selectUser(m *Model, name string) { m.hst.users.page.list.selectName(name) }

// seeNoInput fails when the secret is anywhere on the screen.
func seeNoInput(t *testing.T, m *Model, where string) {
	t.Helper()
	if view := plain(m); strings.Contains(view, inputMarker) {
		t.Errorf("%s shows the secret:\n%s", where, view)
	}
}

func noSecretInRequests(t *testing.T, f *fakeSudo) {
	t.Helper()
	for _, r := range f.reqs {
		if strings.Contains(strings.Join(r.Args, " "), inputMarker) {
			t.Errorf("the secret is in the arguments: %q", r.Args)
		}
	}
}

func TestCreateBuildsTheActionAndPassesPasswordAndKeyOnStandardInputOnly(t *testing.T) {
	m, f, host, _ := userModel(t, 120, 40)
	line, fp := demoKey(t, "demo-new@laptop")
	send(m, "n")
	typed(m, "demo-new")
	send(m, "tab")
	typed(m, inputMarker)
	seeNoInput(t, m, "the password field")
	send(m, "tab")
	typed(m, inputMarker)
	send(m, "tab", "right", "tab", "right", "tab", "space", "tab")
	typed(m, line)
	send(m, "enter")
	if !strings.Contains(plain(m), fp) {
		t.Errorf("the fingerprint of the added key is not listed:\n%s", plain(m))
	}
	save(m)
	seeNoInput(t, m, "the confirmation")
	view := flat(m)
	for _, want := range []string{"<password>", "<ssh key", fp, "practically everything"} {
		if !strings.Contains(view, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, plain(m))
		}
	}
	if m.ask == nil || !m.ask.warn || len(f.reqs) != 0 {
		t.Fatalf("nothing runs before y: ask %+v calls %v", m.ask, f.reqs)
	}

	f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
	reads := host.users
	send(m, "y")
	if len(f.reqs) != 2 || !f.reqs[0].Validate || len(f.reqs[0].Args) != 0 || len(f.reqs[0].Secret) != 0 {
		t.Fatalf("the first call must only ask sudo for the password: %+v", f.reqs)
	}
	want := []string{"apply", "--yes", "--output", "json", "--", "user.create", "demo-new", "admin", "docker", "group=demo-ops"}
	if !slices.Equal(f.reqs[1].Args, want) {
		t.Errorf("args %q, want %q", f.reqs[1].Args, want)
	}
	if !slices.Equal(f.order, []string{"terminal", "quiet"}) {
		t.Errorf("order %v: sudo -v takes the terminal, the change runs without it", f.order)
	}
	var input hostchange.CreateInput
	if len(f.stdins) != 1 || json.Unmarshal([]byte(f.stdins[0]), &input) != nil || input.Password != inputMarker ||
		!slices.Equal(input.Keys, []string{line}) {
		t.Errorf("standard input %q", f.stdins)
	}
	if strings.Count(string(f.reqs[1].Secret), "\x00") != len(f.reqs[1].Secret) {
		t.Error("the secret buffer must be overwritten after the run")
	}
	noSecretInRequests(t, f)
	seeNoInput(t, m, "the result")
	if !strings.Contains(plain(m), "Done") || host.users != reads+1 {
		t.Errorf("result or reload missing (reads %d -> %d):\n%s", reads, host.users, plain(m))
	}
}

func TestCreateFormChecksBeforeAnythingLeavesIt(t *testing.T) {
	cases := []struct {
		name, pw, again, key, want string
	}{
		{"", "a", "a", "", "Enter a name."},
		{"demo-user", "a", "a", "", "exists already"},
		{"demo-new", "", "", "", "Enter a password."},
		{"demo-new", "a", "b", "", "passwords differ"},
		{"demo-new", "a", "a", "not a key", "Not a public key"},
	}
	for _, tc := range cases {
		m, f, _, _ := userModel(t, 120, 40)
		send(m, "n")
		typed(m, tc.name)
		send(m, "tab")
		typed(m, tc.pw)
		send(m, "tab")
		typed(m, tc.again)
		send(m, "tab", "tab", "tab", "tab")
		typed(m, tc.key)
		save(m)
		if m.form == nil || len(f.reqs) != 0 || !strings.Contains(flat(m), tc.want) {
			t.Errorf("%+v: form open %v, calls %v\n%s", tc, m.form != nil, f.reqs, plain(m))
		}
	}
}

func TestCancellingTheCreateConfirmChangesNothing(t *testing.T) {
	for _, key := range []string{"n", "esc"} {
		m, f, host, _ := userModel(t, 120, 40)
		reads := host.users
		send(m, "n")
		typed(m, "demo-new")
		send(m, "tab")
		typed(m, inputMarker)
		send(m, "tab")
		typed(m, inputMarker)
		save(m)
		send(m, key)
		if len(f.reqs) != 0 || m.screen == screenConfirm || m.form != nil || host.users != reads || m.status != "Cancelled." {
			t.Errorf("%s: calls %v screen %v status %q", key, f.reqs, m.screen, m.status)
		}
		seeNoInput(t, m, "the page after cancelling")
	}
}

func TestAWrongSudoPasswordStopsBeforeTheChangeRuns(t *testing.T) {
	m, f, host, _ := userModel(t, 120, 40)
	send(m, "n")
	typed(m, "demo-new")
	send(m, "tab")
	typed(m, inputMarker)
	send(m, "tab")
	typed(m, inputMarker)
	save(m)
	reads := host.users
	f.validateErr = exitError(t, "exit 1")
	send(m, "y")
	if len(f.reqs) != 1 || !f.reqs[0].Validate || !slices.Equal(f.order, []string{"terminal"}) || len(f.stdins) != 0 {
		t.Errorf("only sudo -v may have run: %+v %v", f.reqs, f.order)
	}
	if view := flat(m); !strings.Contains(view, "The change did not run") || !strings.Contains(view, "Nothing was changed") {
		t.Errorf("\n%s", plain(m))
	}
	if host.users != reads {
		t.Error("the page must not be read again after a run that changed nothing")
	}
}

func TestWithoutACachedCredentialTheChangeIsRefusedAndNothingChanges(t *testing.T) {
	m, f, host, _ := userModel(t, 120, 40)
	send(m, "n")
	typed(m, "demo-new")
	send(m, "tab")
	typed(m, inputMarker)
	send(m, "tab")
	typed(m, inputMarker)
	save(m)
	reads := host.users
	f.answer = func(HostRequest) (string, error) { return "", exitError(t, "exit 1") }
	f.stderr = "sudo: a password is required"
	send(m, "y")
	view := flat(m)
	if !strings.Contains(view, "no longer holds your password") || !strings.Contains(view, "Nothing was changed") {
		t.Errorf("\n%s", plain(m))
	}
	if host.users != reads || len(f.stdins) != 1 {
		t.Errorf("reads %d -> %d, runs %d", reads, host.users, len(f.stdins))
	}
	seeNoInput(t, m, "the message")
}

func TestARefusalOfTheHostIsShownWithoutTheSecret(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	send(m, "n")
	typed(m, "demo-new")
	send(m, "tab")
	typed(m, inputMarker)
	send(m, "tab")
	typed(m, inputMarker)
	save(m)
	f.answer = func(HostRequest) (string, error) { return "", exitError(t, "exit 1") }
	f.stderr = "host apply: the user rejected " + inputMarker
	send(m, "y")
	view := flat(m)
	if !strings.Contains(view, "rejected <hidden>") || strings.Contains(view, inputMarker) {
		t.Errorf("\n%s", plain(m))
	}
}

// demoUserChanges are the changes offered for demo-user: an admin who is not enabled, with a key.
var demoUserChanges = []string{chgEnable, chgAddGroups, chgRemoveGroups, chgAdminOff, chgDockerOn, chgPassword, chgAddKey, chgRemoveKey}

// openChange opens the change form of name on the change id.
func openChange(t *testing.T, m *Model, name, id string) {
	t.Helper()
	selectUser(m, name)
	send(m, "e")
	if m.form == nil {
		t.Fatalf("no form:\n%s", plain(m))
	}
	for range slices.Index(demoUserChanges, id) {
		send(m, "right")
	}
}

func TestEachChangeRunsItsOwnActionWithItsOwnConfirm(t *testing.T) {
	line, _ := demoKey(t, "demo-user@desk")
	cases := []struct {
		id    string
		fill  func(m *Model, fp string)
		args  []string
		stdin string
		power bool
	}{
		{chgEnable, func(*Model, string) {}, []string{"user.enable", "demo-user"}, "", false},
		{chgAddGroups, func(m *Model, _ string) { send(m, "tab", "space") }, []string{"user.add-groups", "demo-user", "demo-ops"}, "", false},
		{chgRemoveGroups, func(m *Model, _ string) { send(m, "tab", "space") }, []string{"user.remove-groups", "demo-user", "demo-team"}, "", false},
		{chgAdminOff, func(*Model, string) {}, []string{"user.set-admin", "demo-user", "off", "without-sysmin"}, "", false},
		{chgDockerOn, func(*Model, string) {}, []string{"user.set-docker", "demo-user", "on"}, "", true},
		{chgPassword, func(m *Model, _ string) {
			send(m, "tab")
			typed(m, inputMarker)
			send(m, "tab")
			typed(m, inputMarker)
		}, []string{"user.set-password", "demo-user", "must-change"}, inputMarker, false},
		{chgAddKey, func(m *Model, _ string) {
			send(m, "tab")
			typed(m, line)
		}, []string{"user.add-key", "demo-user"}, line, false},
		{chgRemoveKey, func(*Model, string) {}, nil, "", false},
	}
	for _, tc := range cases {
		m, f, host, fp := userModel(t, 120, 40)
		openChange(t, m, "demo-user", tc.id)
		tc.fill(m, fp)
		save(m)
		seeNoInput(t, m, tc.id+" confirm")
		if m.ask == nil || len(f.reqs) != 0 {
			t.Errorf("%s: no confirmation: %s", tc.id, plain(m))
			continue
		}
		if tc.power != strings.Contains(flat(m), "practically everything") {
			t.Errorf("%s: power warning %v:\n%s", tc.id, tc.power, plain(m))
		}
		args := tc.args
		if tc.id == chgRemoveKey {
			args = []string{"user.remove-key", "demo-user", fp}
		}
		f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
		reads := host.users
		send(m, "y")
		wantReqs, call := 1, 0
		if tc.stdin != "" {
			wantReqs, call = 2, 1
		}
		if len(f.reqs) != wantReqs {
			t.Errorf("%s: calls %+v", tc.id, f.reqs)
			continue
		}
		if wantReqs == 2 && !f.reqs[0].Validate {
			t.Errorf("%s: a secret needs sudo -v first: %+v", tc.id, f.reqs)
		}
		want := append([]string{"apply", "--yes", "--output", "json", "--"}, args...)
		if !slices.Equal(f.reqs[call].Args, want) {
			t.Errorf("%s: args %q, want %q", tc.id, f.reqs[call].Args, want)
		}
		if tc.stdin != "" && (len(f.stdins) != 1 || f.stdins[0] != tc.stdin) {
			t.Errorf("%s: stdin %q", tc.id, f.stdins)
		}
		if tc.stdin == "" && len(f.stdins) != 0 {
			t.Errorf("%s: unexpected stdin %q", tc.id, f.stdins)
		}
		noSecretInRequests(t, f)
		seeNoInput(t, m, tc.id+" result")
		if host.users != reads+1 {
			t.Errorf("%s: the page was not read again", tc.id)
		}
	}
}

func TestOffersFollowTheUser(t *testing.T) {
	m, _, _, _ := userModel(t, 120, 40)
	selectUser(m, "demo-admin")
	send(m, "e")
	view := flat(m)
	if strings.Contains(view, "Enable for sysmin") {
		t.Error("an enabled admin is not offered Enable for sysmin")
	}
	want := []string{chgAddGroups, chgAdminOff, chgDockerOff, chgPassword, chgAddKey, chgRemoveKey}
	if got := m.form.row("what").values; !slices.Equal(got, want) {
		t.Errorf("offers %v, want %v", got, want)
	}
	send(m, "esc")

	selectUser(m, "demo-user")
	send(m, "e")
	if got := m.form.row("what").values; !slices.Equal(got, demoUserChanges) {
		t.Errorf("offers %v, want %v", got, demoUserChanges)
	}
}

// selectAccount marks the user name and unfolds the system section when the user sits in it.
func selectAccount(m *Model, name string) {
	p := &m.hst.users.page
	if !p.list.selectName(name) {
		openSystem(m, p, "enter")
		selectUser(m, name)
	}
}

// chooseChange opens the form of name and moves to the change id.
func chooseChange(t *testing.T, m *Model, name, id string) {
	t.Helper()
	selectAccount(m, name)
	send(m, "e")
	if m.form == nil {
		t.Fatalf("no form for %s:\n%s", name, plain(m))
	}
	for i := 0; m.form.row("what").value() != id; i++ {
		if i > 12 {
			t.Fatalf("%s is not offered for %s", id, name)
		}
		send(m, "right")
	}
}

func TestRootAndSystemUsersAreChangeableWithARedConfirmation(t *testing.T) {
	m, f, host, _ := userModel(t, 120, 40)
	host.groups = append(host.groups, hostadmin.Group{Name: "demo-sys", GID: 65534, System: true})
	m.hst.users.all = append(m.hst.users.all, hostadmin.User{Name: "root", UID: 0, PrimaryGroup: "root", Groups: []string{"root"}, Home: "/root", System: true})
	m.fillHostUsers()
	for _, tc := range []struct {
		user, change string
		pick         bool
		red          bool
		says         []string
	}{
		{"svc-demo", chgDockerOn, false, true, []string{"Packages or services may depend on this system account."}},
		{"root", chgDockerOn, false, true, []string{"root is the account with all rights", "may depend on this system account"}},
		{"demo-user", chgDockerOn, false, false, nil},
		{"demo-user", chgAddGroups, true, true, []string{"may depend on this system group"}},
		{"svc-demo", chgAddGroups, true, true, []string{"may depend on this system account", "may depend on this system group"}},
	} {
		chooseChange(t, m, tc.user, tc.change)
		if tc.pick {
			send(m, "tab", "right", "space") // demo-sys, the second of the groups to choose from
		}
		save(m)
		if m.ask == nil || m.ask.danger != tc.red || len(f.reqs) != 0 {
			t.Fatalf("%s %s: ask %+v\n%s", tc.user, tc.change, m.ask, plain(m))
		}
		for _, want := range tc.says {
			if !strings.Contains(flat(m), want) {
				t.Errorf("%s %s lacks %q:\n%s", tc.user, tc.change, want, plain(m))
			}
		}
		send(m, "n")
	}
}

func TestEveryAccountOffersTheChangeForm(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	for _, name := range []string{"svc-demo", "demo-user"} {
		selectAccount(m, name)
		if keys := strings.Join(m.hostKeys(), " "); !strings.Contains(keys, "e edit") {
			t.Errorf("%s: keys %q", name, keys)
		}
		send(m, "e")
		if m.form == nil || len(f.reqs) != 0 {
			t.Errorf("%s: no form", name)
		}
		send(m, "esc")
	}
}

func TestTakingAdminAwayFromYourselfOrTheLastAdminIsRefusedEarly(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	hostUID = func() int { return 1000 }
	selectUser(m, "demo-admin")
	send(m, "e")
	for m.form.row("what").value() != chgAdminOff {
		send(m, "right")
	}
	save(m)
	if m.form == nil || !strings.Contains(flat(m), "from yourself") || len(f.reqs) != 0 {
		t.Errorf("own admin:\n%s", plain(m))
	}
	send(m, "esc")

	hostUID = func() int { return 4242 }
	selectUser(m, "demo-admin")
	send(m, "e")
	for m.form.row("what").value() != chgAdminOff {
		send(m, "right")
	}
	save(m)
	if m.form == nil || !strings.Contains(flat(m), "last admin") || len(f.reqs) != 0 {
		t.Errorf("last admin:\n%s", plain(m))
	}
}

func TestRemovingAKeyThatWasNotReadOffersTheRootReadFirst(t *testing.T) {
	m, f, _, _ := userModel(t, 120, 40)
	for i := range m.hst.users.all {
		if m.hst.users.all[i].Name == "demo-user" {
			m.hst.users.all[i].SSHKeys = nil
		}
	}
	m.fillHostUsers()
	openChange(t, m, "demo-user", chgRemoveKey)
	if !strings.Contains(flat(m), "not read yet") {
		t.Errorf("no hint:\n%s", plain(m))
	}
	save(m)
	if m.form != nil || m.ask == nil || !strings.Contains(plain(m), "as root") || len(f.reqs) != 0 {
		t.Errorf("\n%s", plain(m))
	}
}

func TestUserFormsSpeakGerman(t *testing.T) {
	m, _, _, _ := userModel(t, 120, 40)
	useLanguage(t, "de")
	send(m, "n")
	view := flat(m)
	for _, want := range []string{"Neuer Benutzer", "Passwort wiederholen", "Weitere Gruppen"} {
		if !strings.Contains(view, want) {
			t.Errorf("lacks %q:\n%s", want, plain(m))
		}
	}
}

func TestParseUserKeyRefusesOptionsAndMultipleLines(t *testing.T) {
	line, fp := demoKey(t, "c")
	if k, problem := parseUserKey("  " + line + " "); problem != "" || k.fingerprint != fp {
		t.Errorf("valid key: %v %q", k, problem)
	}
	for _, bad := range []string{"", "ssh-ed25519", "command=\"x\" " + line, line + "\n" + line} {
		if _, problem := parseUserKey(bad); problem == "" {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestARefusedChangeShowsItsReasonAndChangesNothing(t *testing.T) {
	m, f, host, _ := userModel(t, 120, 40)
	reads := host.users
	f.answer = func(HostRequest) (string, error) {
		return `{"journal_id":"","action":"user.set-admin","sentence":"","commands":[],"result":"refused","check":"","check_error":"","restored":false,"reversible":false,"error":"demo-admin is the last admin","warning":""}`,
			exitError(t, "exit 1")
	}
	drain(m, m.askHostChange(hostchange.ActionUserSetAdmin, []string{"demo-admin", "off"}))
	send(m, "y")
	view := plain(m)
	if !strings.Contains(view, "Not changed: demo-admin is the last admin") || strings.Contains(flat(m), "password was wrong") {
		t.Errorf("\n%s", view)
	}
	if host.users != reads {
		t.Errorf("a refusal reloads nothing: reads %d -> %d", reads, host.users)
	}
}

func TestARunWithoutReportUsesTheStandardErrorAsTheReasonUnlessSudoFailed(t *testing.T) {
	cases := []struct{ stderr, want, not string }{
		{"host apply: something broke", "answered without a report", "password was wrong"},
		{"sudo: 3 incorrect password attempts", "password was wrong", "answered without a report"},
	}
	for _, tc := range cases {
		m, f, _, _ := userModel(t, 120, 40)
		f.answer = func(HostRequest) (string, error) { return "", exitError(t, "exit 1") }
		drain(m, m.askHostChange(hostchange.ActionUserSetDocker, []string{"demo-admin", "off"}))
		m.hostExec = func(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
			return func() tea.Msg {
				_, _ = cmd.Stderr.Write([]byte(tc.stderr))
				return fn(exitError(t, "exit 1"))
			}
		}
		send(m, "y")
		view := flat(m)
		if !strings.Contains(view, tc.want) || strings.Contains(view, tc.not) {
			t.Errorf("%q:\n%s", tc.stderr, plain(m))
		}
	}
}
