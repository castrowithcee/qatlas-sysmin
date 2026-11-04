package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// folderWorld records the commands and never changes anything: the folders it is asked about are plain
// temporary files that are only read.
type folderWorld struct {
	calls []string
}

func (w *folderWorld) Run(_ context.Context, c Command) ([]byte, error) {
	w.calls = append(w.calls, c.Display())
	return nil, nil
}

type folderFixture struct {
	*folderWorld
	runner   *Runner
	accounts *hostadmin.Host
	root     string
}

func newFolderFixture(t *testing.T) *folderFixture {
	t.Helper()
	dir := t.TempDir()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(real, "tree")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	passwd := filepath.Join(dir, "passwd")
	group := filepath.Join(dir, "group")
	if err := os.WriteFile(passwd, []byte("demo-user:x:4242:4343:Demo:/home/demo-user:/bin/bash\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(group, []byte("demo-group:x:4343:demo-user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	accounts := &hostadmin.Host{PasswdPath: passwd, GroupPath: group, ShadowPath: filepath.Join(dir, "shadow"), Now: time.Now}
	f := &folderFixture{folderWorld: &folderWorld{}, root: root, accounts: accounts}
	reg := NewRegistry()
	if err := RegisterFolderActions(reg, FolderHost{Accounts: accounts}); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(filepath.Join(dir, "host", "journal"), os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	f.runner = &Runner{
		Registry: reg, Journal: j, Exec: f.folderWorld, SudoUID: 1000,
		Now: func() time.Time { clock = clock.Add(time.Second); return clock },
	}
	return f
}

func (f *folderFixture) args(path string, extra ...string) []string {
	return append([]string{path, "demo-user", "demo-group", "read-write", "read", "nothing"}, extra...)
}

func TestFolderChangeCommandsAreSymbolic(t *testing.T) {
	f := newFolderFixture(t)
	res, err := f.runner.Apply(context.Background(), ActionFolderSetPermissions, f.args(f.root), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"chown -- demo-user:demo-group " + f.root,
		"chmod -- u=rwX,g=rX,o= " + f.root,
	}
	if strings.Join(f.calls, "\n") != strings.Join(want, "\n") || strings.Join(res.Entry.Commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("executed %q, journaled %q", f.calls, res.Entry.Commands)
	}
	if res.Entry.Result != ResultApplied || res.Entry.Check != CheckPassed || !res.Entry.Reversible {
		t.Fatalf("%+v", res.Entry)
	}
	wantSentence := "Change " + f.root + ": owner demo-user, group demo-group; the owner gets read and write, the group read, everyone else no access."
	if res.Entry.Sentence != wantSentence {
		t.Errorf("sentence %q", res.Entry.Sentence)
	}
}

func TestFolderChangeRecursiveArgv(t *testing.T) {
	f := newFolderFixture(t)
	plan, err := f.runner.Registry.Describe(ActionFolderSetPermissions,
		FolderArgs(FolderRequest{
			Path: f.root, Owner: "demo-user", Group: "demo-group", Recursive: true,
			UserAccess: hostadmin.AccessReadWrite, GroupAccess: hostadmin.AccessRead, OtherAccess: hostadmin.AccessNothing,
		}), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"chown -R -h -- demo-user:demo-group " + f.root,
		"chmod -R -- u=rwX,g=rX,o= " + f.root,
	}
	if got := displays(plan.Commands); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(plan.Sentence, "and everything below it") {
		t.Errorf("sentence %q", plan.Sentence)
	}
}

func TestFolderLevelsMapToSymbolicModes(t *testing.T) {
	f := newFolderFixture(t)
	plan, err := f.runner.Registry.Describe(ActionFolderSetPermissions,
		[]string{f.root, "demo-user", "demo-group", "nothing", "read-write", "read"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Commands[1].Display(); got != "chmod -- u=,g=rwX,o=rX "+f.root {
		t.Fatalf("got %q", got)
	}
}

func TestFolderChangeRefusesBadArguments(t *testing.T) {
	f := newFolderFixture(t)
	for name, args := range map[string][]string{
		"relative":    f.args("srv/demo"),
		"dotdot":      f.args("/srv/demo/../x"),
		"trailing":    f.args("/srv/demo/"),
		"bad level":   {"/srv/demo", "demo-user", "demo-group", "write", "read", "nothing"},
		"bad owner":   {"/srv/demo", "Demo User", "demo-group", "read", "read", "nothing"},
		"bad option":  f.args("/srv/demo", "everything"),
		"repeated":    f.args("/srv/demo", "recursive", "recursive"),
		"too few":     {"/srv/demo"},
		"newline":     f.args("/srv/demo\nx"),
		"empty owner": {"/srv/demo", "", "demo-group", "read", "read", "nothing"},
	} {
		_, err := f.runner.Registry.Describe(ActionFolderSetPermissions, args, nil)
		var arg *ArgumentError
		if !errors.As(err, &arg) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestSystemPathsAreRefusedWithoutTheAcknowledgement(t *testing.T) {
	f := newFolderFixture(t)
	for _, p := range []string{"/", "/etc", "/etc/demo-app", "/usr/local/demo", "/boot/demo", "/usr"} {
		_, err := f.runner.Apply(context.Background(), ActionFolderSetPermissions, f.args(p), nil)
		var ref *RefusedError
		if !errors.As(err, &ref) || !strings.Contains(ref.Msg, "system path") {
			t.Errorf("%s: err = %v", p, err)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("executed %v", f.calls)
	}
}

func TestSystemPathsStayRefusedWhereAcknowledgementIsNotEnough(t *testing.T) {
	f := newFolderFixture(t)
	for _, args := range [][]string{
		f.args("/etc", "recursive", "system-path"),
		f.args("/", "recursive", "system-path"),
		f.args("/proc/sys", "system-path"),
		f.args("/sys/kernel", "system-path"),
	} {
		_, err := f.runner.Registry.Describe(ActionFolderSetPermissions, args, nil)
		var ref *RefusedError
		if !errors.As(err, &ref) {
			t.Errorf("%v: err = %v", args, err)
		}
	}
	// With the acknowledgement a system path below the roots is planned, and says so.
	plan, err := f.runner.Registry.Describe(ActionFolderSetPermissions, f.args("/etc/demo-app", "system-path"), nil)
	if err != nil || !strings.HasSuffix(plan.Sentence, "This is a system path.") {
		t.Fatalf("%v %q", err, plan.Sentence)
	}
}

func TestIsSystemPath(t *testing.T) {
	for p, want := range map[string]bool{
		"/": true, "/etc": true, "/etc/x": true, "/usr/local": true, "/boot": true, "/proc/1": true, "/sys/x": true,
		"/etcetera": false, "/srv/demo-app": false, "/home": false, "/etc/../srv": false,
	} {
		if got := IsSystemPath(p); got != want {
			t.Errorf("IsSystemPath(%q) = %v", p, got)
		}
	}
}

func TestFolderChangeRefusesWhatItCannotChangeSafely(t *testing.T) {
	f := newFolderFixture(t)
	link := filepath.Join(filepath.Dir(f.root), "link")
	if err := os.Symlink(f.root, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(f.root, "plain")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"missing":       f.args(filepath.Join(f.root, "absent")),
		"link":          f.args(link),
		"file":          f.args(file),
		"unknown owner": {f.root, "demo-nobody", "demo-group", "read", "read", "nothing"},
		"unknown group": {f.root, "demo-user", "demo-nogroup", "read", "read", "nothing"},
	} {
		_, err := f.runner.Apply(context.Background(), ActionFolderSetPermissions, args, nil)
		var ref *RefusedError
		if !errors.As(err, &ref) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("executed %v", f.calls)
	}
}

// buildTree makes a folder with files, a sub folder and a link, and returns its entries as the capture sees them.
func buildTree(t *testing.T, root string) {
	t.Helper()
	for _, d := range []string{"data", "data/deep"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"compose.yml", "data/a.txt", "data/deep/b.txt", "it's here.txt"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("/srv/demo-elsewhere", filepath.Join(root, "data", "ln")); err != nil {
		t.Fatal(err)
	}
}

func TestCountFolderEntries(t *testing.T) {
	f := newFolderFixture(t)
	buildTree(t, f.root)
	if n, err := CountFolderEntries(f.root, true); err != nil || n != 8 {
		t.Fatalf("recursive: %d %v", n, err)
	}
	if n, err := CountFolderEntries(f.root, false); err != nil || n != 1 {
		t.Fatalf("single: %d %v", n, err)
	}
}

func TestRecursiveCaptureAndUndoRestoreEveryEntry(t *testing.T) {
	f := newFolderFixture(t)
	buildTree(t, f.root)
	entries, err := collectEntries(f.root, true)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.runner.Apply(context.Background(), ActionFolderSetPermissions, f.args(f.root, "recursive"), nil)
	if err != nil || res.Entry.Result != ResultApplied {
		t.Fatalf("%v %+v", err, res.Entry)
	}
	var st folderState
	if err := decodeState(res.Entry.Previous, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Entries) != len(entries) || !st.Recursive || st.Path != f.root {
		t.Fatalf("journaled %d entries, want %d", len(st.Entries), len(entries))
	}
	f.calls = nil
	undo, err := f.runner.Undo(context.Background(), res.Entry.ID)
	if err != nil || undo.Entry.Result != ResultApplied {
		t.Fatalf("%v %+v", err, undo.Entry)
	}
	uid, gid := os.Getuid(), os.Getgid()
	var chowned, chmodded []string
	for _, c := range f.calls {
		argv := splitDisplay(c)
		switch argv[0] {
		case "chown":
			if argv[1] != "-h" || argv[3] != fmt.Sprintf("%d:%d", uid, gid) {
				t.Errorf("unexpected %q", c)
			}
			chowned = append(chowned, argv[4:]...)
		case "chmod":
			chmodded = append(chmodded, argv[3:]...)
			if want := map[string]bool{"0755": true, "0640": true}[argv[2]]; !want {
				t.Errorf("unexpected mode in %q", c)
			}
		}
	}
	for _, e := range entries {
		if !contains(chowned, e.Path) {
			t.Errorf("owner of %s not restored", e.Path)
		}
		if e.Kind != "link" && !contains(chmodded, e.Path) {
			t.Errorf("mode of %s not restored", e.Path)
		}
		if e.Kind == "link" && contains(chmodded, e.Path) {
			t.Errorf("mode of the link %s must not be set", e.Path)
		}
	}
	if len(chowned) != len(entries) || len(chmodded) != len(entries)-1 {
		t.Errorf("chown %d, chmod %d, entries %d", len(chowned), len(chmodded), len(entries))
	}
	if undo.Entry.UndoOf != res.Entry.ID || len(undo.Entry.Commands) != len(f.calls) {
		t.Errorf("%+v", undo.Entry)
	}
}

// splitDisplay undoes the display of plain words and the quoting of 'it'\”s here.txt'.
func splitDisplay(s string) []string {
	var out []string
	var cur strings.Builder
	quoted, any := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !quoted:
			quoted, any = true, true
		case c == '\'' && quoted:
			quoted = false
		case c == '\\' && !quoted && strings.HasPrefix(s[i:], `\'`):
			cur.WriteByte('\'')
			i++
		case c == ' ' && !quoted:
			if any || cur.Len() > 0 {
				out = append(out, cur.String())
			}
			cur.Reset()
			any = false
		default:
			cur.WriteByte(c)
		}
	}
	if any || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestInverseChunksLongPathLists(t *testing.T) {
	var st folderState
	st.Path = "/srv/demo-app"
	for i := range 2500 {
		st.Entries = append(st.Entries, folderEntry{Path: fmt.Sprintf("/srv/demo-app/file-%04d", i), Kind: "file", UID: 1, GID: 2, Mode: 0o644})
	}
	b, _ := jsonMarshal(st)
	plan, err := folderActions{}.inverseSetPermissions(b)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, c := range plan.Commands {
		if len(c.Argv) > maxChunkPaths+5 {
			t.Errorf("a command has %d arguments", len(c.Argv))
		}
		if c.Argv[0] == "chown" {
			total += len(c.Argv) - 4
		}
	}
	if total != 2500 || len(plan.Commands) != 6 {
		t.Errorf("chowned %d paths in %d commands", total, len(plan.Commands))
	}
}

func TestInverseRefusesAnUnusableRecord(t *testing.T) {
	for name, st := range map[string]folderState{
		"outside": {Path: "/srv/demo-app", Entries: []folderEntry{{Path: "/srv/other/x", Kind: "file", Mode: 0o644}}},
		"mode":    {Path: "/srv/demo-app", Entries: []folderEntry{{Path: "/srv/demo-app/x", Kind: "file", Mode: 0o17777}}},
		"kind":    {Path: "/srv/demo-app", Entries: []folderEntry{{Path: "/srv/demo-app/x", Kind: "odd", Mode: 0o644}}},
		"empty":   {Path: "/srv/demo-app"},
		"unclean": {Path: "/srv/demo-app", Entries: []folderEntry{{Path: "/srv/demo-app/../x", Kind: "file", Mode: 0o644}}},
	} {
		b, _ := jsonMarshal(st)
		if _, err := (folderActions{}).inverseSetPermissions(b); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFolderChangeNeverStartsDocker(t *testing.T) {
	f := newFolderFixture(t)
	f.runner.Exec = noDockerRun{f.folderWorld}
	res, err := f.runner.Apply(context.Background(), ActionFolderSetPermissions, f.args(f.root, "recursive"), nil)
	if err != nil || res.Entry.Result != ResultApplied || res.Entry.Check != CheckPassed {
		t.Fatalf("%v %+v", err, res.Entry)
	}
}

// noDockerRun fails the test-run of any command that would start docker.
type noDockerRun struct{ *folderWorld }

func (w noDockerRun) Run(ctx context.Context, c Command) ([]byte, error) {
	if len(c.Argv) > 0 && filepath.Base(c.Argv[0]) == "docker" {
		return nil, errors.New("docker must not be started: " + c.Display())
	}
	return w.folderWorld.Run(ctx, c)
}

func TestSpecialBitsMustStayUnchanged(t *testing.T) {
	entries := []folderEntry{
		{Path: "/srv/demo-app", Kind: "dir", Mode: 0o2755},
		{Path: "/srv/demo-app/ln", Kind: "link", Mode: 0o777},
	}
	same := func(p string) (folderEntry, error) {
		for _, e := range entries {
			if e.Path == p {
				e.Mode = e.Mode&0o7000 | 0o750
				return e, nil
			}
		}
		return folderEntry{}, errors.New("gone")
	}
	if err := specialBitsUnchanged(entries, same); err != nil {
		t.Fatal(err)
	}
	dropped := func(p string) (folderEntry, error) { return folderEntry{Path: p, Kind: "dir", Mode: 0o750}, nil }
	if err := specialBitsUnchanged(entries, dropped); err == nil || !strings.Contains(err.Error(), "2000 to 0000") {
		t.Fatalf("got %v", err)
	}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// failingRun fails the command whose display starts with prefix and records all.
type failingRun struct {
	*folderWorld
	prefix string
}

func (w failingRun) Run(ctx context.Context, c Command) ([]byte, error) {
	out, _ := w.folderWorld.Run(ctx, c)
	if strings.HasPrefix(c.Display(), w.prefix) {
		return []byte("simulated failure"), errors.New("exit status 1")
	}
	return out, nil
}

func TestFailingRecursiveChownRestoresEveryCapturedEntry(t *testing.T) {
	f := newFolderFixture(t)
	buildTree(t, f.root)
	entries, err := collectEntries(f.root, true)
	if err != nil {
		t.Fatal(err)
	}
	f.runner.Exec = failingRun{f.folderWorld, "chown -R"}
	res, err := f.runner.Apply(context.Background(), ActionFolderSetPermissions, f.args(f.root, "recursive"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry.Result != ResultRestored || !res.Entry.Restored {
		t.Fatalf("%+v", res.Entry)
	}
	var chowned []string
	for _, c := range f.calls[1:] {
		if argv := splitDisplay(c); argv[0] == "chown" {
			chowned = append(chowned, argv[4:]...)
		}
	}
	for _, e := range entries {
		if !contains(chowned, e.Path) {
			t.Errorf("owner of %s not restored", e.Path)
		}
	}
}

func TestFailingLaterChmodRestoresThePreviousState(t *testing.T) {
	f := newFolderFixture(t)
	buildTree(t, f.root)
	f.runner.Exec = failingRun{f.folderWorld, "chmod -R"}
	res, err := f.runner.Apply(context.Background(), ActionFolderSetPermissions, f.args(f.root, "recursive"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry.Result != ResultRestored || !res.Entry.Restored {
		t.Fatalf("%+v", res.Entry)
	}
	last := f.calls[len(f.calls)-1]
	if !strings.HasPrefix(f.calls[1], "chmod -R") || !strings.HasPrefix(last, "chmod -- 0") {
		t.Fatalf("calls %q", f.calls)
	}
}
