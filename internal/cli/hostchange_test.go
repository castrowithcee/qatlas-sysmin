package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
)

type changeWorld struct {
	values  map[string]string
	calls   []string
	stdin   string
	failSet string
}

func (w *changeWorld) Run(_ context.Context, c hostchange.Command) ([]byte, error) {
	w.calls = append(w.calls, c.Display())
	w.stdin = string(c.Secret)
	if c.Argv[0] == "demo-set" {
		if c.Argv[2] == w.failSet {
			return []byte("simulated"), errors.New("exit status 1")
		}
		w.values[c.Argv[1]] = c.Argv[2]
	}
	return nil, nil
}

type changeFixture struct {
	w       *changeWorld
	env     *changeEnv
	dir     string
	audited []hostchange.AuditEntry
	auditEr error
}

func newChangeFixture(t *testing.T) *changeFixture {
	t.Helper()
	f := &changeFixture{w: &changeWorld{values: map[string]string{"alpha": "old"}}}
	reg := hostchange.NewRegistry()
	if err := reg.Register(hostchange.Definition{
		ID: "demo.set",
		Plan: func(args []string, _ []byte) (hostchange.Plan, error) {
			if len(args) != 2 {
				return hostchange.Plan{}, hostchange.Argf("demo.set needs a key and a value")
			}
			return hostchange.Plan{
				Sentence: "Set " + args[0] + " to " + args[1] + ".",
				Commands: []hostchange.Command{{Argv: []string{"demo-set", args[0], args[1]}}},
				Capture: func(context.Context) (json.RawMessage, error) {
					return json.Marshal([]string{args[0], f.w.values[args[0]]})
				},
			}, nil
		},
		Inverse: func(prev json.RawMessage) (hostchange.Plan, error) {
			var kv []string
			if err := json.Unmarshal(prev, &kv); err != nil {
				return hostchange.Plan{}, err
			}
			return hostchange.Plan{Sentence: "Put it back.", Commands: []hostchange.Command{{Argv: []string{"demo-set", kv[0], kv[1]}}}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(hostchange.Definition{
		ID: "demo.login", WantsSecret: true,
		Plan: func(_ []string, secret []byte) (hostchange.Plan, error) {
			return hostchange.Plan{
				Sentence: "Set the login secret.", NotReversible: "the old one is not kept",
				Commands: []hostchange.Command{{Argv: []string{"demo-login", "user1"}, Secret: secret}},
			}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	tick := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	f.dir = filepath.Join(t.TempDir(), "sysmin-host", "journal")
	f.env = &changeEnv{
		Registry: reg, Exec: f.w, JournalDir: f.dir, OwnerUID: os.Getuid(),
		Now:     func() time.Time { tick = tick.Add(time.Second); return tick },
		SudoUID: func() int { return 1000 },
		Record: func(a hostchange.AuditEntry) error {
			f.audited = append(f.audited, a)
			return f.auditEr
		},
	}
	return f
}

func runChange(t *testing.T, f *changeFixture, euid int, stdin string, args ...string) (int, string, string) {
	t.Helper()
	hostEnv, _ := hostFixture(t, euid)
	opts := &Options{Redactor: &redact.Redactor{}, host: hostEnv, change: f.env,
		sys: &system{euid: func() int { return euid }}}
	root := newRootCommand(opts)
	root.SetIn(strings.NewReader(stdin))
	var out, errOut bytes.Buffer
	code := run(root, opts, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not JSON: %q: %v", s, err)
	}
	return m
}

func TestHostApplyRunsTheActionAndReportsIt(t *testing.T) {
	f := newChangeFixture(t)
	code, out, errOut := runChange(t, f, 0, "", "host", "apply", "demo.set", "alpha", "new", "--yes", "--output", "json")
	if code != 0 {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	m := decode(t, out)
	if m["result"] != "applied" || m["action"] != "demo.set" || m["sentence"] != "Set alpha to new." ||
		m["check"] != "none" || m["restored"] != false || m["reversible"] != true || m["warning"] != "" || m["journal_id"] == "" {
		t.Fatalf("%v", m)
	}
	if cmds := m["commands"].([]any); len(cmds) != 1 || cmds[0] != "demo-set alpha new" || strings.Join(f.w.calls, "") != "demo-set alpha new" {
		t.Fatalf("commands %v executed %v", cmds, f.w.calls)
	}
	if len(f.audited) != 1 || f.audited[0].SudoUID != 1000 {
		t.Fatalf("audit %+v", f.audited)
	}
}

func TestHostChangeRefusesNonRootAndMissingYes(t *testing.T) {
	f := newChangeFixture(t)
	for _, args := range [][]string{
		{"host", "apply", "demo.set", "alpha", "new", "--yes"}, {"host", "undo", "x", "--yes"}, {"host", "journal"},
	} {
		code, out, errOut := runChange(t, f, 1000, "", args...)
		if code == 0 || out != "" || !strings.Contains(errOut, "must run as root") {
			t.Errorf("%v: code %d out %q err %q", args, code, out, errOut)
		}
	}
	code, out, errOut := runChange(t, f, 0, "", "host", "apply", "demo.set", "alpha", "new")
	if code != 2 || out != "" || !strings.Contains(errOut, "--yes") {
		t.Errorf("missing --yes: code %d out %q err %q", code, out, errOut)
	}
	code, _, _ = runChange(t, f, 0, "", "host", "undo", "20300102T030405.000000Z-aaaaaa")
	if code != 2 {
		t.Errorf("undo without --yes: code %d", code)
	}
	if len(f.w.calls) != 0 || f.w.values["alpha"] != "old" {
		t.Errorf("nothing may run: %v", f.w.calls)
	}
	// A caller who is not an administrator learns nothing else.
	code, _, errOut = runChange(t, f, 1001, "", "host", "apply", "demo.set", "--yes")
	if code != 1 || !strings.HasPrefix(errOut, "sysmin is reserved") {
		t.Errorf("non-admin: code %d err %q", code, errOut)
	}
}

func TestHostApplyRefusesBadActionAndArguments(t *testing.T) {
	f := newChangeFixture(t)
	for _, args := range [][]string{
		{"host", "apply", "--yes"}, {"host", "apply", "demo.unknown", "--yes"}, {"host", "apply", "demo.set", "alpha", "--yes"},
	} {
		if code, _, _ := runChange(t, f, 0, "", args...); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
	}
	if len(f.w.calls) != 0 {
		t.Errorf("executed %v", f.w.calls)
	}
}

func TestHostApplySecretComesFromStdinOnly(t *testing.T) {
	f := newChangeFixture(t)
	const marker = "hunter-demo-42"
	code, out, errOut := runChange(t, f, 0, marker+"\n", "host", "apply", "demo.login", "--yes", "--output", "json")
	if code != 0 {
		t.Fatalf("code %d %q", code, errOut)
	}
	if f.w.stdin != marker {
		t.Errorf("stdin %q", f.w.stdin)
	}
	m := decode(t, out)
	if m["commands"].([]any)[0] != "demo-login user1 < <password>" || m["reversible"] != false {
		t.Errorf("%v", m)
	}
	files, _ := os.ReadDir(f.dir)
	all := out + errOut + strings.Join(f.w.calls, "")
	for _, de := range files {
		b, _ := os.ReadFile(filepath.Join(f.dir, de.Name()))
		all += string(b)
	}
	if strings.Contains(all, marker) {
		t.Error("marker leaked into output, argv or journal")
	}
	if code, _, _ := runChange(t, f, 0, "", "host", "apply", "demo.login", "--yes"); code != 2 {
		t.Errorf("empty stdin: code %d", code)
	}
}

func TestHostFailedChangeReportsAndExitsNonZero(t *testing.T) {
	f := newChangeFixture(t)
	f.w.failSet = "new"
	code, out, errOut := runChange(t, f, 0, "", "host", "apply", "demo.set", "alpha", "new", "--yes", "--output", "json")
	m := decode(t, out)
	if code != 1 || m["result"] != "restored" || m["restored"] != true || !strings.Contains(m["error"].(string), "command 1") ||
		!strings.Contains(errOut, "previous state is back") {
		t.Fatalf("code %d %v err %q", code, m, errOut)
	}
}

func TestHostAuditFailureWarnsButTheChangeStands(t *testing.T) {
	f := newChangeFixture(t)
	f.auditEr = errors.New("service unreachable")
	code, out, _ := runChange(t, f, 0, "", "host", "apply", "demo.set", "alpha", "new", "--yes", "--output", "json")
	m := decode(t, out)
	if code != 0 || m["result"] != "applied" || !strings.Contains(m["warning"].(string), "audit entry is missing") {
		t.Fatalf("code %d %v", code, m)
	}
}

func TestHostJournalAndUndo(t *testing.T) {
	f := newChangeFixture(t)
	_, out, _ := runChange(t, f, 0, "", "host", "apply", "demo.set", "alpha", "new", "--yes", "--output", "json")
	id := decode(t, out)["journal_id"].(string)

	code, out, errOut := runChange(t, f, 0, "", "host", "journal", "--output", "json")
	if code != 0 {
		t.Fatalf("%d %q", code, errOut)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 || rows[0]["id"] != id ||
		rows[0]["result"] != "applied" || rows[0]["reversible"] != true || rows[0]["sentence"] != "Set alpha to new." {
		t.Fatalf("%s %v", out, err)
	}

	code, out, errOut = runChange(t, f, 0, "", "host", "undo", id, "--yes", "--output", "json")
	if code != 0 {
		t.Fatalf("%d %q", code, errOut)
	}
	m := decode(t, out)
	if m["result"] != "applied" || m["reversible"] != false || f.w.values["alpha"] != "old" {
		t.Fatalf("%v %v", m, f.w.values)
	}
	_, out, _ = runChange(t, f, 0, "", "host", "journal", "--output", "json")
	_ = json.Unmarshal([]byte(out), &rows)
	if len(rows) != 2 || rows[1]["result"] != "undone" || rows[1]["reversible"] != false {
		t.Fatalf("%v", rows)
	}
	if code, _, _ := runChange(t, f, 0, "", "host", "undo", id, "--yes"); code == 0 {
		t.Error("second undo must fail")
	}
	if code, _, _ := runChange(t, f, 0, "", "host", "undo", "../../etc/passwd", "--yes"); code == 0 {
		t.Error("an invalid id must fail")
	}
}

func TestHostApplyRecordsTheHumanAndTheActionWithoutSecrets(t *testing.T) {
	f := newChangeFixture(t)
	code, _, _ := runChange(t, f, 0, "", "host", "apply", "demo.set", "alpha", "new", "--yes", "--output", "json")
	if code != 0 || len(f.audited) != 1 {
		t.Fatalf("code %d, audited %d", code, len(f.audited))
	}
	a := f.audited[0]
	if a.SudoUID != 1000 || a.Action != "demo.set" || a.Result != hostchange.ResultApplied || !hostchange.ValidID(a.ID) {
		t.Fatalf("audit = %+v", a)
	}

	f = newChangeFixture(t)
	code, _, _ = runChange(t, f, 0, "pass-for-test\n", "host", "apply", "demo.login", "user1", "--yes", "--output", "json")
	if code != 0 || len(f.audited) != 1 {
		t.Fatalf("code %d, audited %d", code, len(f.audited))
	}
	raw, _ := json.Marshal(f.audited[0])
	if strings.Contains(string(raw), "pass-for-test") {
		t.Fatalf("the audit entry carries the secret input: %s", raw)
	}
}

func TestSudoUIDIsReadStrictly(t *testing.T) {
	for value, want := range map[string]int{
		"1000": 1000, "0": 0, "": 0, "+1000": 0, "-5": 0, " 1000": 0, "1000 ": 0, "01000": 0, "1e3": 0,
		"abc": 0, "99999999999": 0, "2147483648": 0,
	} {
		t.Setenv("SUDO_UID", value)
		if got := sudoUID(); got != want {
			t.Errorf("SUDO_UID %q = %d, want %d", value, got, want)
		}
	}
}
