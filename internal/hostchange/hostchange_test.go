package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// world is an invented host: demo-set writes a key, and the fake executor records every call.
type world struct {
	values map[string]string
	calls  []string
	failOn string // display text of a command that fails
	secret string
}

func (w *world) Run(_ context.Context, c Command) ([]byte, error) {
	w.calls = append(w.calls, c.Display())
	if w.failOn != "" && c.Display() == w.failOn {
		return []byte("simulated failure"), errors.New("exit status 1")
	}
	switch c.Argv[0] {
	case "demo-set":
		w.values[c.Argv[1]] = c.Argv[2]
	case "demo-login":
		w.secret = string(c.Secret)
	}
	return nil, nil
}

func testRegistry(t *testing.T, w *world, checkFails bool) *Registry {
	t.Helper()
	reg := NewRegistry()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(reg.Register(Definition{
		ID: "demo.set",
		Plan: func(args []string, _ []byte) (Plan, error) {
			if len(args) != 2 {
				return Plan{}, Argf("demo.set needs a key and a value")
			}
			key, value := args[0], args[1]
			return Plan{
				Sentence: "Set " + key + " to " + value + ".",
				Commands: []Command{{Argv: []string{"demo-set", key, value}}, {Argv: []string{"demo-set", key + "-copy", "it's " + value}}},
				Capture: func(context.Context) (json.RawMessage, error) {
					return json.Marshal(map[string]string{"key": key, "old": w.values[key]})
				},
				Check: func(context.Context) error {
					if checkFails {
						return errors.New("value rejected")
					}
					return nil
				},
			}, nil
		},
		Inverse: func(prev json.RawMessage) (Plan, error) {
			var m map[string]string
			if err := json.Unmarshal(prev, &m); err != nil {
				return Plan{}, err
			}
			return Plan{
				Sentence: "Put " + m["key"] + " back.",
				Commands: []Command{{Argv: []string{"demo-set", m["key"], m["old"]}}},
			}, nil
		},
	}))
	must(reg.Register(Definition{
		ID: "demo.login", WantsSecret: true,
		Plan: func(args []string, secret []byte) (Plan, error) {
			return Plan{
				Sentence:      "Set the login secret.",
				Commands:      []Command{{Argv: []string{"demo-login", "user1"}, Secret: secret}},
				NotReversible: "the old secret is not kept",
			}, nil
		},
	}))
	return reg
}

type fixture struct {
	w       *world
	runner  *Runner
	journal *Journal
	dir     string
	clock   time.Time
	audited []AuditEntry
}

func newFixture(t *testing.T, checkFails bool) *fixture {
	t.Helper()
	f := &fixture{w: &world{values: map[string]string{"alpha": "old"}}, clock: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	f.dir = filepath.Join(t.TempDir(), "host", "journal")
	j, err := OpenJournal(f.dir, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	f.journal = j
	f.runner = &Runner{
		Registry: testRegistry(t, f.w, checkFails), Journal: j, Exec: f.w, SudoUID: 1000,
		Now:    func() time.Time { f.clock = f.clock.Add(time.Second); return f.clock },
		Record: func(a AuditEntry) error { f.audited = append(f.audited, a); return nil },
	}
	return f
}

func TestDisplayedCommandsAreTheExecutedOnes(t *testing.T) {
	f := newFixture(t, false)
	res, err := f.runner.Apply(context.Background(), "demo.set", []string{"alpha", "new"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"demo-set alpha new", `demo-set alpha-copy 'it'\''s new'`}
	if strings.Join(f.w.calls, "\n") != strings.Join(want, "\n") || strings.Join(res.Entry.Commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("executed %q, journaled %q, want %q", f.w.calls, res.Entry.Commands, want)
	}
	plan, err := f.runner.Registry.Describe("demo.set", []string{"alpha", "new"}, nil)
	if err != nil || displays(plan.Commands)[0] != want[0] {
		t.Fatalf("describe: %v %v", plan, err)
	}
	if res.Entry.Result != ResultApplied || res.Entry.Check != CheckPassed || f.w.values["alpha"] != "new" {
		t.Fatalf("result %+v values %v", res.Entry, f.w.values)
	}
	if len(f.audited) != 1 || f.audited[0].ID != res.Entry.ID || f.audited[0].SudoUID != 1000 {
		t.Fatalf("audit %+v", f.audited)
	}
}

func TestSecretsNeverAppearOutsideStdin(t *testing.T) {
	f := newFixture(t, false)
	const marker = "hunter-demo-42"
	res, err := f.runner.Apply(context.Background(), "demo.login", nil, []byte(marker))
	if err != nil {
		t.Fatal(err)
	}
	if f.w.secret != marker {
		t.Fatalf("stdin not delivered: %q", f.w.secret)
	}
	cmd := Command{Argv: []string{"demo-login", "user1"}, Secret: []byte(marker)}
	if cmd.Display() != "demo-login user1 < <password>" {
		t.Errorf("display %q", cmd.Display())
	}
	blobs := []string{cmd.String(), cmd.GoString(), strings.Join(f.w.calls, "|")}
	for _, format := range []func(any) ([]byte, error){json.Marshal} {
		b, _ := format(cmd)
		blobs = append(blobs, string(b))
		b, _ = format(res.Entry)
		blobs = append(blobs, string(b))
	}
	files, _ := os.ReadDir(f.dir)
	for _, de := range files {
		b, _ := os.ReadFile(filepath.Join(f.dir, de.Name()))
		blobs = append(blobs, string(b))
	}
	for _, b := range blobs {
		if strings.Contains(b, marker) {
			t.Errorf("marker leaked in %q", b)
		}
	}
	if res.Entry.Reversible || res.Entry.NotReversible == "" {
		t.Errorf("entry %+v", res.Entry)
	}
	if _, err := f.runner.Undo(context.Background(), res.Entry.ID); err == nil {
		t.Error("undo of a non-reversible change must fail")
	}
}

func TestSecretInArgumentsIsRefused(t *testing.T) {
	if err := validateCommand(Command{Argv: []string{"demo-login", "xx-secret-xx"}, Secret: []byte("secret")}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestFailingCommandScrubsSecretFromError(t *testing.T) {
	f := newFixture(t, false)
	f.w.failOn = "demo-login user1 < <password>"
	res, err := f.runner.Apply(context.Background(), "demo.login", nil, []byte("hunter-demo-42"))
	if err != nil || res.Entry.Result != ResultFailed || strings.Contains(res.Entry.Error, "hunter") {
		t.Fatalf("%+v %v", res.Entry, err)
	}
}

func TestJournalEntryWrittenBeforeTheChange(t *testing.T) {
	f := newFixture(t, false)
	var seen Entry
	f.runner.Exec = execFunc(func(c Command) error {
		if seen.ID == "" {
			list, err := f.journal.List()
			if err != nil || len(list) != 1 {
				t.Fatalf("journal before the first command: %v %v", list, err)
			}
			seen = list[0]
		}
		return nil
	})
	if _, err := f.runner.Apply(context.Background(), "demo.set", []string{"alpha", "new"}, nil); err != nil {
		t.Fatal(err)
	}
	if seen.Result != ResultPending || !strings.Contains(strings.Join(strings.Fields(string(seen.Previous)), ""), `"old":"old"`) || seen.SudoUID != 1000 {
		t.Fatalf("entry before the change: %+v", seen)
	}
}

type execFunc func(Command) error

func (f execFunc) Run(_ context.Context, c Command) ([]byte, error) { return nil, f(c) }

func TestJournalModes(t *testing.T) {
	f := newFixture(t, false)
	res, err := f.runner.Apply(context.Background(), "demo.set", []string{"alpha", "new"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	di, _ := os.Stat(f.dir)
	fi, _ := os.Stat(filepath.Join(f.dir, res.Entry.ID+".json"))
	if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
		t.Errorf("modes dir %v file %v", di.Mode().Perm(), fi.Mode().Perm())
	}
	parent, _ := os.Stat(filepath.Dir(f.dir))
	if parent.Mode().Perm() != 0o700 {
		t.Errorf("parent %v", parent.Mode().Perm())
	}
}

func TestJournalRefusesWiderModesAndForeignOwners(t *testing.T) {
	f := newFixture(t, false)
	res, _ := f.runner.Apply(context.Background(), "demo.set", []string{"alpha", "new"}, nil)
	file := filepath.Join(f.dir, res.Entry.ID+".json")
	if err := os.Chmod(file, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := f.journal.List(); err == nil {
		t.Error("an entry open to the group must be refused")
	}
	if err := os.Chmod(f.dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(f.dir, os.Getuid()); err == nil {
		t.Error("a directory open to the group must be refused")
	}
	if err := os.Chmod(f.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(f.dir, os.Getuid()+1); err == nil {
		t.Error("a foreign owner must be refused")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(f.dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenJournal(link, os.Getuid()); err == nil {
		t.Error("a symlink must be refused")
	}
}

func TestJournalKeepsTheLatest50(t *testing.T) {
	f := newFixture(t, false)
	var first Entry
	for i := 0; i < KeepEntries+5; i++ {
		res, err := f.runner.Apply(context.Background(), "demo.set", []string{"alpha", "v"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = res.Entry
		}
	}
	list, err := f.journal.List()
	if err != nil || len(list) != KeepEntries {
		t.Fatalf("%d entries, %v", len(list), err)
	}
	if _, err := f.journal.Get(first.ID); !errors.Is(err, ErrNoEntry) {
		t.Errorf("oldest entry survived: %v", err)
	}
	if list[0].Time.Before(list[1].Time) {
		t.Error("list must be newest first")
	}
}

func TestUndoRestoresAndMarksTheOriginal(t *testing.T) {
	f := newFixture(t, false)
	res, _ := f.runner.Apply(context.Background(), "demo.set", []string{"alpha", "new"}, nil)
	f.w.calls = nil
	undo, err := f.runner.Undo(context.Background(), res.Entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.w.values["alpha"] != "old" || undo.Entry.Result != ResultApplied || undo.Entry.UndoOf != res.Entry.ID || undo.Entry.Reversible {
		t.Fatalf("values %v undo %+v", f.w.values, undo.Entry)
	}
	if strings.Join(f.w.calls, "|") != strings.Join(undo.Entry.Commands, "|") {
		t.Errorf("shown %q executed %q", undo.Entry.Commands, f.w.calls)
	}
	orig, _ := f.journal.Get(res.Entry.ID)
	if orig.Result != ResultUndone || orig.UndoneBy != undo.Entry.ID {
		t.Errorf("original %+v", orig)
	}
	if _, err := f.runner.Undo(context.Background(), res.Entry.ID); err == nil {
		t.Error("a second undo must fail")
	}
	if _, err := f.runner.Undo(context.Background(), undo.Entry.ID); err == nil {
		t.Error("an undo must not be undone")
	}
}

func TestFailedCheckRestoresAutomatically(t *testing.T) {
	f := newFixture(t, true)
	res, err := f.runner.Apply(context.Background(), "demo.set", []string{"alpha", "new"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := res.Entry
	if e.Result != ResultRestored || !e.Restored || e.Check != CheckFailed || !strings.Contains(e.CheckError, "value rejected") {
		t.Fatalf("%+v", e)
	}
	if f.w.values["alpha"] != "old" {
		t.Errorf("not restored: %v", f.w.values)
	}
	if len(f.audited) != 1 || f.audited[0].Result != ResultRestored {
		t.Errorf("audit %+v", f.audited)
	}
}

func TestFailingCommandMidwayRestores(t *testing.T) {
	f := newFixture(t, false)
	f.w.failOn = `demo-set alpha-copy 'it'\''s new'`
	res, err := f.runner.Apply(context.Background(), "demo.set", []string{"alpha", "new"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := res.Entry
	if e.Result != ResultRestored || !e.Restored || e.Check != CheckNone || !strings.Contains(e.Error, "command 2") {
		t.Fatalf("%+v", e)
	}
	if f.w.values["alpha"] != "old" {
		t.Errorf("not restored: %v", f.w.values)
	}
}

func TestFailedRestoreIsReported(t *testing.T) {
	f := newFixture(t, false)
	f.w.failOn = `demo-set alpha-copy 'it'\''s new'`
	f.runner.Exec = execFunc(func(c Command) error {
		if c.Argv[1] == "alpha-copy" || c.Argv[2] == "old" {
			return errors.New("exit status 1")
		}
		return nil
	})
	res, _ := f.runner.Apply(context.Background(), "demo.set", []string{"alpha", "new"}, nil)
	if res.Entry.Result != ResultFailed || res.Entry.Restored || !strings.Contains(res.Entry.Error, "restore failed") {
		t.Fatalf("%+v", res.Entry)
	}
}

func TestAuditFailureLeavesTheChangeAndWarns(t *testing.T) {
	f := newFixture(t, false)
	f.runner.Record = func(AuditEntry) error { return errors.New("service unreachable") }
	res, err := f.runner.Apply(context.Background(), "demo.set", []string{"alpha", "new"}, nil)
	if err != nil || res.Entry.Result != ResultApplied || !strings.Contains(res.Warning, "audit entry is missing") {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestRegistryValidates(t *testing.T) {
	reg := NewRegistry()
	plan := func([]string, []byte) (Plan, error) { return Plan{}, nil }
	for _, id := range []string{"", "nodot", "Group.add", "group.", "a.b.c"} {
		if err := reg.Register(Definition{ID: id, Plan: plan}); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
	if err := reg.Register(Definition{ID: "group.add-member", Plan: plan}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(Definition{ID: "group.add-member", Plan: plan}); err == nil {
		t.Error("duplicate accepted")
	}
	f := newFixture(t, false)
	if _, err := f.runner.Apply(context.Background(), "demo.set", []string{"only-one"}, nil); err == nil || len(f.w.calls) != 0 {
		t.Errorf("bad arguments must fail before anything runs: %v", err)
	}
	if _, err := f.runner.Apply(context.Background(), "demo.nope", nil, nil); err == nil {
		t.Error("unknown action")
	}
}

func TestFailedFirstCommandRestoresOnlyForPartialSteps(t *testing.T) {
	for _, partial := range []bool{false, true} {
		f := newFixture(t, false)
		reg := NewRegistry()
		if err := reg.Register(Definition{
			ID: "demo.partial",
			Plan: func([]string, []byte) (Plan, error) {
				return Plan{
					Sentence: "Set alpha.", PartialSteps: partial,
					Commands: []Command{{Argv: []string{"demo-set", "alpha", "new"}}},
				}, nil
			},
			Inverse: func(json.RawMessage) (Plan, error) {
				return Plan{Sentence: "Put alpha back.", Commands: []Command{{Argv: []string{"demo-set", "alpha", "old"}}}}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
		f.runner.Registry = reg
		f.w.failOn = "demo-set alpha new"
		res, err := f.runner.Apply(context.Background(), "demo.partial", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		e := res.Entry
		if partial {
			if e.Result != ResultRestored || !e.Restored || strings.Contains(e.Error, "nothing was taken back") || f.w.calls[len(f.w.calls)-1] != "demo-set alpha old" {
				t.Errorf("partial: %+v calls %q", e, f.w.calls)
			}
		} else if e.Result != ResultFailed || e.Restored || !strings.Contains(e.Error, "nothing was taken back") || len(f.w.calls) != 1 {
			t.Errorf("plain: %+v calls %q", e, f.w.calls)
		}
	}
}
