package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// fakeSudo stands in for sudo: it records what the interface asked for and answers with a prepared report, so
// no test runs sudo or a program.
type fakeSudo struct {
	reqs []HostRequest
	// answer gives the standard output and the error of a request; nil answers with an empty success.
	answer func(HostRequest) (string, error)
	// buildErr makes the hook fail before anything runs.
	buildErr error
	ran      int
	// validateErr is the end of the run that asks sudo for the password (sudo -v); stderr is what the run
	// without a prompt wrote to standard error; stdins are the standard inputs it was given.
	validateErr error
	stderr      string
	stdins      []string
	// order records the kind of each run: "terminal" for a hand-over, "quiet" for a run without a terminal.
	order []string
}

func (f *fakeSudo) run(req HostRequest) (*exec.Cmd, error) {
	f.reqs = append(f.reqs, req)
	if f.buildErr != nil {
		return nil, f.buildErr
	}
	if req.Validate {
		return exec.Command("fake-sudo", "-v"), nil
	}
	cmd := exec.Command("fake-sudo", req.Args...)
	if len(req.Secret) > 0 {
		cmd.Stdin = bytes.NewReader(req.Secret)
	}
	return cmd, nil
}

// bg is the stand-in for running a command without a terminal.
func (f *fakeSudo) bg(cmd *exec.Cmd) error {
	f.order = append(f.order, "quiet")
	f.ran++
	if cmd.Stdin != nil {
		body, _ := io.ReadAll(cmd.Stdin)
		f.stdins = append(f.stdins, string(body))
	}
	var out string
	var err error
	if f.answer != nil {
		out, err = f.answer(HostRequest{Args: cmd.Args[1:]})
	}
	_, _ = cmd.Stdout.Write([]byte(out))
	_, _ = cmd.Stderr.Write([]byte(f.stderr))
	return err
}

// exec is the stand-in for tea.ExecProcess: it writes the prepared output to the command's standard output
// and reports the prepared error, without starting anything.
func (f *fakeSudo) exec(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	return func() tea.Msg {
		f.ran++
		f.order = append(f.order, "terminal")
		if len(cmd.Args) == 2 && cmd.Args[1] == "-v" {
			return fn(f.validateErr)
		}
		var out string
		var err error
		if f.answer != nil {
			out, err = f.answer(HostRequest{Args: cmd.Args[1:]})
		}
		_, _ = cmd.Stdout.Write([]byte(out))
		return fn(err)
	}
}

// exitError is the error of a finished process with the given shell command, the way a real run reports it.
func exitError(t *testing.T, script string) error {
	t.Helper()
	err := exec.Command("sh", "-c", script).Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("%q ended without an exit error: %v", script, err)
	}
	return err
}

// countingHost counts how often Users is read, so a test sees whether a page was reloaded.
type countingHost struct {
	*fixtureHost
	users int
}

func (c *countingHost) Users() ([]hostadmin.User, error) {
	c.users++
	return c.fixtureHost.Users()
}

func hostActions(t *testing.T) *hostchange.Registry {
	t.Helper()
	reg := hostchange.NewRegistry()
	inverse := func(json.RawMessage) (hostchange.Plan, error) {
		return hostchange.Plan{Sentence: "Put alpha back.", Commands: []hostchange.Command{{Argv: []string{"demo-set", "alpha", "old"}}}}, nil
	}
	for _, d := range []hostchange.Definition{
		{ID: "demo.set", Inverse: inverse, Plan: func(args []string, _ []byte) (hostchange.Plan, error) {
			if len(args) != 2 {
				return hostchange.Plan{}, hostchange.Argf("demo.set needs a key and a value")
			}
			return hostchange.Plan{Sentence: "Set " + args[0] + " to " + args[1] + ".",
				Commands: []hostchange.Command{{Argv: []string{"demo-set", args[0], args[1]}}},
				Capture:  func(context.Context) (json.RawMessage, error) { return nil, nil }}, nil
		}},
		{ID: "demo.wipe", Plan: func([]string, []byte) (hostchange.Plan, error) {
			return hostchange.Plan{Sentence: "Wipe the demo folder.", NotReversible: "the old content is not kept",
				Commands: []hostchange.Command{{Argv: []string{"demo-wipe", "/srv/demo stack"}}}}, nil
		}},
		{ID: "demo.login", WantsSecret: true, Plan: func(_ []string, secret []byte) (hostchange.Plan, error) {
			return hostchange.Plan{Sentence: "Set the login secret.", NotReversible: "the old one is not kept",
				Commands: []hostchange.Command{{Argv: []string{"demo-login"}, Secret: secret}}}, nil
		}},
		{ID: "demo.long", Inverse: inverse, Plan: func([]string, []byte) (hostchange.Plan, error) {
			return hostchange.Plan{Sentence: "Run a long command.", Commands: []hostchange.Command{{Argv: []string{
				"demo-tool", "--option-one", "value-number-one", "--option-two", "value-number-two", "--option-three",
				"value number three with spaces", "--option-four", "/srv/demo-stack/a/very/long/path/that/keeps/going/on"}}}}, nil
		}},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

const journalJSON = `[
 {"id":"20301002T101500.000000Z-aaaaaa","time":"2030-10-02T10:15:00Z","action":"demo.set","sentence":"Set alpha to new.","result":"applied","reversible":true,"sudo_uid":1000,"undo_sentence":"Put alpha back.","undo_commands":["demo-set alpha old"]},
 {"id":"20301001T081000.000000Z-bbbbbb","time":"2030-10-01T08:10:00Z","action":"demo.wipe","sentence":"Wipe the demo folder.","result":"applied","reversible":false,"sudo_uid":1000,"undo_sentence":"","undo_commands":[]},
 {"id":"20300930T120000.000000Z-cccccc","time":"2030-09-30T12:00:00Z","action":"demo.set","sentence":"Set beta to 2.","result":"restored","reversible":false,"sudo_uid":1000,"undo_sentence":"","undo_commands":[]}
]`

const reportApplied = `{"journal_id":"20301003T090000.000000Z-dddddd","action":"demo.set","sentence":"Set alpha to new.","commands":["demo-set alpha new"],"result":"applied","check":"passed","check_error":"","restored":false,"reversible":true,"error":"","warning":""}`

// hostChangeModel is the Host area over a counting host source and a fake sudo.
func hostChangeModel(t *testing.T, w, h int) (*Model, *fakeSudo, *countingHost) {
	t.Helper()
	m := appModel(t, overviewFake(), w, h)
	host := &countingHost{fixtureHost: newFixtureHost()}
	f := &fakeSudo{}
	m.hostSrc, m.hostReg, m.hostRun, m.hostExec, m.hostBg = host, hostActions(t), f.run, f.exec, f.bg
	send(m, "5")
	return m, f, host
}

func requestArgs(f *fakeSudo) [][]string {
	var out [][]string
	for _, r := range f.reqs {
		out = append(out, r.Args)
	}
	return out
}

func TestConfirmShowsTheSentenceAndTheExactCommandsBeforeAnythingRuns(t *testing.T) {
	m, f, _ := hostChangeModel(t, 120, 40)
	drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
	view := plain(m)
	for _, want := range []string{"Set alpha to new.", "$ demo-set alpha new", "You can undo this under Host > Changes.", "Apply", "Cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, view)
		}
	}
	if m.ask == nil || m.ask.danger || len(f.reqs) != 0 {
		t.Fatalf("a reversible change is not dangerous and nothing runs before y: ask %+v, calls %v", m.ask, f.reqs)
	}
	plan, err := m.hostReg.Describe("demo.set", []string{"alpha", "new"}, nil)
	if err != nil || !strings.Contains(flatText(view), plan.Commands[0].Display()) {
		t.Errorf("the shown command is not the planned one: %v", err)
	}

	f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
	send(m, "y")
	want := []string{"apply", "--yes", "--output", "json", "--", "demo.set", "alpha", "new"}
	if got := requestArgs(f); len(got) != 1 || !slices.Equal(got[0], want) {
		t.Errorf("hook calls %v, want %v", got, want)
	}
}

func TestConfirmWrapsLongCommandsInsideTheModal(t *testing.T) {
	m, _, _ := hostChangeModel(t, 80, 24)
	drain(m, m.askHostChange("demo.long", nil))
	view := plain(m)
	assertViewFits(t, view, 80, 24)
	for _, want := range []string{"--option-three", "'value number three", "with spaces'", "/srv/demo-stack/a/very/long/path/that/keeps/going/on"} {
		if !strings.Contains(flatText(view), want) {
			t.Errorf("a wrapped command lost %q:\n%s", want, view)
		}
	}
}

func TestIrreversibleChangesAreDangerousAndSayTheyCannotBeUndone(t *testing.T) {
	m, _, _ := hostChangeModel(t, 120, 40)
	drain(m, m.askHostChange("demo.wipe", nil))
	view := plain(m)
	if m.ask == nil || !m.ask.danger || !m.ask.warn || !strings.Contains(flatText(view), "This cannot be undone: the old content is not kept") {
		t.Errorf("ask %+v\n%s", m.ask, view)
	}
	if !strings.Contains(view, "'/srv/demo stack'") {
		t.Errorf("the quoted argument is not shown as it runs:\n%s", view)
	}
}

func TestACancelledOrRefusedChangeRunsNothingAndChangesNothing(t *testing.T) {
	for _, key := range []string{"n", "esc"} {
		m, f, host := hostChangeModel(t, 120, 40)
		send(m, "down")
		before, reads := m.hst.users.all, host.users
		drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
		send(m, key)
		name, _ := m.hst.users.page.current()
		if len(f.reqs) != 0 || m.screen == screenConfirm || host.users != reads || !slices.EqualFunc(before, m.hst.users.all, func(a, b hostadmin.User) bool { return a.Name == b.Name }) || name != "demo-user" {
			t.Errorf("%s: calls %v screen %v reads %d row %q", key, f.reqs, m.screen, host.users, name)
		}
		if m.status != "Cancelled." {
			t.Errorf("%s: status %q", key, m.status)
		}
	}

	m, f, _ := hostChangeModel(t, 120, 40)
	for name, args := range map[string][]string{"demo.set": {"alpha"}, "demo.login": nil, "demo.missing": nil} {
		drain(m, m.askHostChange(name, args))
		if m.confirm == nil || !m.confirm.notice || len(f.reqs) != 0 {
			t.Errorf("%s: a refusal is a message and runs nothing: %+v %v", name, m.confirm, f.reqs)
		}
		send(m, "esc")
	}
	drain(m, m.askHostChange("demo.login", nil))
	if !strings.Contains(flatText(plain(m)), "needs a secret input") {
		t.Errorf("a secret action is refused clearly:\n%s", plain(m))
	}
}

func TestAnAppliedChangeShowsItsResultAndReloadsTheSameRow(t *testing.T) {
	m, f, host := hostChangeModel(t, 120, 40)
	send(m, "down")
	reads := host.users
	f.answer = func(HostRequest) (string, error) { return reportApplied, nil }
	drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
	send(m, "y")
	view := plain(m)
	for _, want := range []string{"Done: Set alpha to new.", "Check: passed.", "The change is in the journal."} {
		if !strings.Contains(flatText(view), want) {
			t.Errorf("result lacks %q:\n%s", want, view)
		}
	}
	if host.users != reads+1 {
		t.Errorf("the page was read %d times after the change, want 1", host.users-reads)
	}
	send(m, "esc")
	if name, _ := m.hst.users.page.current(); name != "demo-user" || m.area != areaHost || m.hst.sub != hostUsers || m.screen != screenArea {
		t.Errorf("back at %q on area %v page %d screen %v", name, m.area, m.hst.sub, m.screen)
	}
}

func TestAFailedOrRestoredChangeShowsCheckErrorAndWarning(t *testing.T) {
	report := func(result, check string) string {
		b, _ := json.Marshal(map[string]any{"journal_id": "x", "sentence": "Set alpha to new.", "result": result, "check": check,
			"check_error": "the value was rejected", "restored": result == "restored", "reversible": false,
			"error": "demo-set failed: exit status 1", "warning": "the audit log was not written"})
		return string(b)
	}
	for _, tc := range []struct {
		result, check, title, check_ string
	}{
		{"failed", "none", "Failed: Set alpha to new.", ""},
		{"restored", "failed", "Failed, previous state restored: Set alpha to new.", "Check failed: the value was rejected"},
	} {
		m, f, host := hostChangeModel(t, 120, 40)
		f.answer = func(HostRequest) (string, error) { return report(tc.result, tc.check), exitError(t, "exit 1") }
		reads := host.users
		drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
		send(m, "y")
		view := flatText(plain(m))
		for _, want := range []string{tc.title, "demo-set failed: exit status 1", "Warning: the audit log was not written", tc.check_} {
			if !strings.Contains(view, want) {
				t.Errorf("%s: result lacks %q:\n%s", tc.result, want, plain(m))
			}
		}
		if !m.ask.danger || host.users != reads+1 {
			t.Errorf("%s: danger %v, reads %d", tc.result, m.ask.danger, host.users-reads)
		}
	}
}

func TestRunsThatGiveNoAnswerChangeNothingAndSayWhy(t *testing.T) {
	cases := []struct {
		name   string
		answer func(HostRequest) (string, error)
		build  error
		want   string
	}{
		{"wrong password", func(HostRequest) (string, error) { return "", exitError(t, "exit 1") }, nil, "the password was wrong"},
		{"ctrl+c as a signal", func(HostRequest) (string, error) { return "", exitError(t, "kill -INT $$") }, nil, "interrupted"},
		{"ctrl+c as 130", func(HostRequest) (string, error) { return "", exitError(t, "exit 130") }, nil, "interrupted"},
		{"other exit", func(HostRequest) (string, error) { return "", exitError(t, "exit 2") }, nil, "exit code 2"},
		{"output not readable", func(HostRequest) (string, error) { return "not json", nil }, nil, "could not be read"},
		{"sudo missing", nil, ErrNoSudo, "sudo is not installed"},
		{"sudo broken", nil, errors.New("cannot find this program"), "cannot find this program"},
	}
	for _, tc := range cases {
		m, f, host := hostChangeModel(t, 120, 40)
		send(m, "down")
		f.answer, f.buildErr = tc.answer, tc.build
		reads, users := host.users, slices.Clone(m.hst.users.all)
		drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
		send(m, "y")
		view := flatText(plain(m))
		if !strings.Contains(view, "The change did not run") || !strings.Contains(view, tc.want) {
			t.Errorf("%s: message lacks %q:\n%s", tc.name, tc.want, plain(m))
		}
		send(m, "enter")
		name, _ := m.hst.users.page.current()
		if host.users != reads || !slices.EqualFunc(users, m.hst.users.all, func(a, b hostadmin.User) bool { return a.Name == b.Name }) || name != "demo-user" || m.screen != screenArea {
			t.Errorf("%s: model changed: reads %d row %q screen %v", tc.name, host.users-reads, name, m.screen)
		}
	}
}

func TestMessagesAreGerman(t *testing.T) {
	m, f, _ := hostChangeModel(t, 120, 40)
	useLanguage(t, "de")
	f.answer = func(HostRequest) (string, error) { return "", exitError(t, "exit 1") }
	drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
	send(m, "y")
	if view := flatText(plain(m)); !strings.Contains(view, "Das Passwort war falsch") || strings.Contains(view, "password was wrong") {
		t.Errorf("not German:\n%s", plain(m))
	}
}

// reload presses ctrl+r, which send cannot spell.
func reload(m *Model) {
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	drain(m, cmd)
}

func openChanges(m *Model) { runCommand(m, "go host changes") }

func TestChangesPageReadsTheJournalOnlyOnRequest(t *testing.T) {
	utcClock(t)
	m, f, _ := hostChangeModel(t, 120, 40)
	openChanges(m)
	view := flatText(plain(m))
	if m.hst.sub != hostChanges || len(f.reqs) != 0 || !strings.Contains(view, "Press ctrl+r to read the latest changes as root") {
		t.Fatalf("sub %d calls %v\n%s", m.hst.sub, f.reqs, plain(m))
	}
	f.answer = func(HostRequest) (string, error) { return journalJSON, nil }
	reload(m)
	if got := requestArgs(f); len(got) != 1 || !slices.Equal(got[0], []string{"journal", "--output", "json"}) {
		t.Fatalf("calls %v", got)
	}
	view = plain(m)
	for _, want := range []string{"2030-10-02 10:15:00", "Set alpha to new.", "applied", "restored", "Wipe the demo folder."} {
		if !strings.Contains(view, want) {
			t.Errorf("list lacks %q:\n%s", want, view)
		}
	}
}

func TestEnterOnAReversibleChangeUndoesItAfterTheSameConfirm(t *testing.T) {
	utcClock(t)
	m, f, host := hostChangeModel(t, 120, 40)
	openChanges(m)
	f.answer = func(HostRequest) (string, error) { return journalJSON, nil }
	reload(m)
	f.reqs = nil

	send(m, "enter")
	view := flatText(plain(m))
	for _, want := range []string{"Put alpha back.", "$ demo-set alpha old", "This takes back: Set alpha to new.", "Undo", "Cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("undo confirmation lacks %q:\n%s", want, plain(m))
		}
	}
	send(m, "n")
	if len(f.reqs) != 0 {
		t.Fatalf("cancel ran %v", f.reqs)
	}

	reads := host.users
	f.answer = func(HostRequest) (string, error) {
		return `{"journal_id":"20301003T100000.000000Z-eeeeee","sentence":"Put alpha back.","result":"applied","check":"none","reversible":false}`, nil
	}
	send(m, "enter", "y")
	want := []string{"undo", "--yes", "--output", "json", "--", "20301002T101500.000000Z-aaaaaa"}
	if got := requestArgs(f); len(got) != 1 || !slices.Equal(got[0], want) {
		t.Fatalf("undo call %v, want %v", got, want)
	}
	if view := flatText(plain(m)); !strings.Contains(view, "Done: Put alpha back.") || !strings.Contains(view, "cannot be undone") {
		t.Errorf("undo result:\n%s", plain(m))
	}
	send(m, "esc")
	if id, _ := m.hst.chg.page.current(); id != "20301002T101500.000000Z-aaaaaa" || m.hst.sub != hostChanges {
		t.Errorf("back at %q on page %d", id, m.hst.sub)
	}
	if e := m.hst.chg.items["20301002T101500.000000Z-aaaaaa"]; e.Reversible || e.Result != "undone" {
		t.Errorf("the undone entry is still offered: %+v", e)
	}
	if host.users != reads {
		t.Errorf("the journal page re-read the users")
	}
	if !strings.Contains(flatText(plain(m)), "older than your last change") {
		t.Errorf("no note that the list is out of date:\n%s", plain(m))
	}
	f.reqs = nil
	send(m, "enter")
	if len(f.reqs) != 0 || m.confirm == nil || !m.confirm.notice {
		t.Errorf("an undone entry is refused with a message: %v", f.reqs)
	}
}

func TestEnterOnAnIrreversibleChangeOffersNoUndo(t *testing.T) {
	m, f, _ := hostChangeModel(t, 120, 40)
	openChanges(m)
	f.answer = func(HostRequest) (string, error) { return journalJSON, nil }
	reload(m)
	f.reqs = nil
	send(m, "down", "enter")
	if len(f.reqs) != 0 || m.confirm == nil || !m.confirm.notice || !strings.Contains(flatText(plain(m)), "cannot be undone") {
		t.Errorf("calls %v\n%s", f.reqs, plain(m))
	}
}

func TestRootOnlyFieldsAreReadOnlyAfterTheKeyAndTheAnswer(t *testing.T) {
	m, f, _ := hostChangeModel(t, 120, 40)
	if len(f.reqs) != 0 || !strings.Contains(plain(m), "Press ctrl+r to read the fields") {
		t.Fatalf("opening the page reads nothing: %v\n%s", f.reqs, plain(m))
	}
	reload(m)
	if m.confirm == nil || len(f.reqs) != 0 || !strings.Contains(flatText(plain(m)), "Read demo-admin as root?") {
		t.Fatalf("calls %v\n%s", f.reqs, plain(m))
	}
	send(m, "n")
	if len(f.reqs) != 0 {
		t.Fatalf("n read: %v", f.reqs)
	}
	f.answer = func(HostRequest) (string, error) {
		return `{"name":"demo-admin","locked":true,"expired":false,"ssh_keys":[{"type":"ssh-ed25519","comment":"admin@laptop","fingerprint":"SHA256:DEMOADMINKEY"}]}`, nil
	}
	reload(m)
	send(m, "y")
	if got := requestArgs(f); len(got) != 1 || !slices.Equal(got[0], []string{"user", "--output", "json", "--", "demo-admin"}) {
		t.Fatalf("calls %v", got)
	}
	if view := plain(m); !strings.Contains(view, "Locked") || strings.Contains(view, "Press ctrl+r to read the fields") {
		t.Errorf("the locked state is not shown:\n%s", view)
	}
	send(m, "tab", "right")
	if view := plain(m); !strings.Contains(view, "admin@laptop") || !strings.Contains(view, "SHA256:DEMOADMINKEY") {
		t.Errorf("keys not shown:\n%s", view)
	}
}

func TestReadingAsRootThatFailsLeavesTheUserAsItWas(t *testing.T) {
	m, f, _ := hostChangeModel(t, 120, 40)
	f.answer = func(HostRequest) (string, error) { return "", exitError(t, "exit 1") }
	reload(m)
	send(m, "y")
	if view := flatText(plain(m)); !strings.Contains(view, "Reading as root did not work") || !strings.Contains(view, "the password was wrong") {
		t.Errorf("\n%s", plain(m))
	}
	send(m, "esc")
	if u := m.hst.users.items["demo-admin"]; u.Locked != nil || u.SSHKeys != nil {
		t.Errorf("user changed: %+v", u)
	}
}

func TestTheChildGetsTheRealTerminalAndAStdoutOfItsOwn(t *testing.T) {
	m, _, _ := hostChangeModel(t, 120, 40)
	in, out := t.TempDir()+"/in", t.TempDir()+"/out"
	inFile, err := os.Create(in)
	if err != nil {
		t.Fatal(err)
	}
	outFile, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer inFile.Close()
	defer outFile.Close()
	m.setHostTerminal(inFile, outFile)
	var seen *exec.Cmd
	m.hostExec = func(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
		seen = cmd
		return func() tea.Msg { return fn(nil) }
	}
	drain(m, m.askHostChange("demo.set", []string{"alpha", "new"}))
	send(m, "y")
	if seen == nil || seen.Stdin != io.Reader(inFile) || seen.Stderr == nil || seen.Stderr == io.Writer(outFile) || seen.Stdout == nil || seen.Stdout == io.Writer(outFile) {
		t.Errorf("stdin %v stdout %v stderr %v", seen.Stdin, seen.Stdout, seen.Stderr)
	}
}
