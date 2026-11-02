package hostchange

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stackFolderFixture is a folder fixture whose group database knows the group sysmin. The files belong to the
// test user, so the root owner of the setup is an ID nobody has unless a test says the folder is set up.
type stackFolderFixture struct {
	*folderFixture
	host FolderHost
}

func newStackFolderFixture(t *testing.T, sysminGID int) *stackFolderFixture {
	t.Helper()
	f := newFolderFixture(t)
	group := fmt.Sprintf("demo-group:x:4343:demo-user\nsysmin:x:%d:\n", sysminGID)
	if err := os.WriteFile(f.accounts.GroupPath, []byte(group), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &stackFolderFixture{folderFixture: f}
	s.host = FolderHost{Accounts: f.accounts, Stacks: newCheck(f.docker), rootUID: os.Getuid() + 1}
	s.host.Stacks.CanWrite = func(string) error { return nil }
	s.register(t)
	return s
}

func (s *stackFolderFixture) register(t *testing.T) {
	t.Helper()
	reg := NewRegistry()
	if err := RegisterFolderActions(reg, s.host); err != nil {
		t.Fatal(err)
	}
	s.runner.Registry = reg
}

func (s *stackFolderFixture) setUpAsRoot(t *testing.T) {
	t.Helper()
	s.host.rootUID = os.Getuid()
	s.register(t)
}

func write(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("A=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// buildStacks makes: web (compose.yaml, .env, data/), db (docker-compose.yml), alt (compose.yml and
// docker-compose.yaml), old (docker-compose.yaml), plus a folder without compose file and a loose file.
func buildStacks(t *testing.T, root string) {
	t.Helper()
	write(t, filepath.Join(root, "web", "compose.yaml"), 0o644)
	write(t, filepath.Join(root, "web", ".env"), 0o600)
	write(t, filepath.Join(root, "web", "data", "state.db"), 0o644)
	write(t, filepath.Join(root, "web", "data", "compose.yaml"), 0o644) // below a data folder: not a compose file of the stack
	write(t, filepath.Join(root, "db", "docker-compose.yml"), 0o644)
	write(t, filepath.Join(root, "alt", "compose.yml"), 0o644)
	write(t, filepath.Join(root, "alt", "docker-compose.yaml"), 0o644)
	write(t, filepath.Join(root, "old", "docker-compose.yaml"), 0o644)
	write(t, filepath.Join(root, "plain", "notes.txt"), 0o644)
	write(t, filepath.Join(root, "loose.txt"), 0o644)
}

func (s *stackFolderFixture) apply(t *testing.T) Result {
	t.Helper()
	res, err := s.runner.Apply(context.Background(), ActionFolderSetupStacks, StackFolderArgs(s.root), nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestStackFolderFindsStacksAndSetsOnlyTheTargets(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid()+1)
	buildStacks(t, s.root)
	r := s.root
	res := s.apply(t)
	if res.Entry.Result != ResultApplied || res.Entry.Check != CheckPassed || !res.Entry.Reversible {
		t.Fatalf("%s", brief(res.Entry))
	}
	want := []string{
		"chown -h -- root:sysmin " + strings.Join([]string{
			r, r + "/alt", r + "/alt/compose.yml", r + "/alt/docker-compose.yaml", r + "/db", r + "/db/docker-compose.yml",
			r + "/old", r + "/old/docker-compose.yaml", r + "/web", r + "/web/.env", r + "/web/compose.yaml"}, " "),
		"chmod -- 0750 " + strings.Join([]string{r, r + "/alt", r + "/db", r + "/old", r + "/web"}, " "),
		"chmod -- 0640 " + strings.Join([]string{r + "/alt/compose.yml", r + "/alt/docker-compose.yaml", r + "/db/docker-compose.yml",
			r + "/old/docker-compose.yaml", r + "/web/compose.yaml"}, " "),
		"chmod -- 0660 " + r + "/web/.env",
	}
	if strings.Join(s.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(s.calls, "\n"), strings.Join(want, "\n"))
	}
	for _, c := range s.calls {
		if strings.Contains(c, "/data") || strings.Contains(c, "plain") || strings.Contains(c, "loose") || strings.Contains(c, "-R") {
			t.Errorf("touches more than the targets: %s", c)
		}
	}
	if !strings.Contains(res.Entry.Sentence, "Other users except root have no access") {
		t.Errorf("sentence %q", res.Entry.Sentence)
	}
}

func TestStackFolderPreviewNamesChangesAndWhatStays(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid()+1)
	buildStacks(t, s.root)
	if err := os.Symlink(filepath.Join(s.root, "db"), filepath.Join(s.root, "db-link")); err != nil {
		t.Fatal(err)
	}
	p, err := s.host.PreviewStackFolder(context.Background(), s.root)
	if err != nil {
		t.Fatal(err)
	}
	if !p.OthersLoseAccess || p.NothingToDo || p.Folder == nil || p.Folder.Role != RoleFolder || len(p.Stacks) != 4 {
		t.Fatalf("%+v", p)
	}
	if p.Folder.After.String() != "root:sysmin 0750" || !strings.HasSuffix(p.Folder.Before.String(), " 0755") {
		t.Errorf("folder %v -> %v", p.Folder.Before, p.Folder.After)
	}
	var web StackFolderStack
	for _, st := range p.Stacks {
		if st.Name == "web" {
			web = st
		}
	}
	roles := []string{}
	for _, c := range web.Changes {
		roles = append(roles, c.Role+"="+c.After.String())
	}
	if got := strings.Join(roles, ";"); got != "stack folder=root:sysmin 0750;compose file=root:sysmin 0640;env file=root:sysmin 0660" {
		t.Errorf("web changes %s", got)
	}
	if len(p.Kept) != 1 || p.Kept[0].Path != filepath.Join(s.root, "db-link") || !strings.Contains(p.Kept[0].Reason, "link") {
		t.Errorf("kept %+v", p.Kept)
	}
	if len(s.calls) != 0 {
		t.Errorf("the preview ran %v", s.calls)
	}
}

func TestStackFolderLeavesMountedFilesAndForeignEnvFilesAlone(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid()+1)
	r := s.root
	write(t, r+"/web/compose.yaml", 0o644)
	write(t, r+"/web/.env", 0o600)
	write(t, r+"/web/config/app.env", 0o600)
	write(t, r+"/web/other.env", 0o600)
	write(t, r+"/db/compose.yaml", 0o644)
	write(t, r+"/outside/app.env", 0o600)
	ct := healthy(idWeb, "demo-web-1")
	ct.EnvFiles = r + "/web/config/app.env," + r + "/outside/app.env," + r + "/web/other.env"
	ct.Mounts = []fakeMount{{"bind", r + "/web/compose.yaml"}, {"bind", r + "/web/other.env"}, {"bind", r + "/web/data"}}
	s.docker.stacks = stackRow(idWeb, "demo", r+"/web", r+"/web/compose.yaml")
	s.docker.states = [][]fakeContainer{{ct}}
	res := s.apply(t)
	if res.Entry.Result != ResultApplied {
		t.Fatalf("%s", brief(res.Entry))
	}
	all := strings.Join(s.calls, "\n")
	for _, p := range []string{r + "/web/compose.yaml", r + "/web/other.env", r + "/outside", r + "/web/data"} {
		if strings.Contains(all, p) {
			t.Errorf("%s was changed:\n%s", p, all)
		}
	}
	for _, p := range []string{r + "/web/.env", r + "/web/config/app.env", r + "/db/compose.yaml"} {
		if !strings.Contains(all, p) {
			t.Errorf("%s was not changed:\n%s", p, all)
		}
	}
	p, err := s.host.PreviewStackFolder(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]string{}
	for _, st := range p.Stacks {
		if st.Name == "web" {
			if !st.Running {
				t.Error("web is not marked running")
			}
			for _, k := range st.Kept {
				kept[k.Path] = k.Reason
			}
		}
	}
	for path, reason := range map[string]string{
		r + "/web/compose.yaml": "mounted into a container",
		r + "/web/other.env":    "mounted into a container",
		r + "/outside/app.env":  "outside the stack folder",
	} {
		if !strings.Contains(kept[path], reason) {
			t.Errorf("kept[%s] = %q, want %q", path, kept[path], reason)
		}
	}
}

func TestStackFolderIsIdempotent(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid())
	s.setUpAsRoot(t)
	r := s.root
	write(t, r+"/web/compose.yaml", 0o640)
	write(t, r+"/web/.env", 0o660)
	mkdir(t, r+"/web", 0o750)
	mkdir(t, r, 0o750)
	p, err := s.host.PreviewStackFolder(context.Background(), r)
	if err != nil || !p.NothingToDo || p.Folder != nil {
		t.Fatalf("%v %+v", err, p)
	}
	_, err = s.runner.Apply(context.Background(), ActionFolderSetupStacks, StackFolderArgs(r), nil)
	var ref *RefusedError
	if !errors.As(err, &ref) || !strings.Contains(ref.Msg, "nothing to change") || len(s.calls) != 0 {
		t.Fatalf("%v %v", err, s.calls)
	}
	// A stack added later is covered by the next run, and only its entries change.
	write(t, r+"/new/compose.yaml", 0o644)
	mkdir(t, r+"/new", 0o755)
	res := s.apply(t)
	want := []string{
		"chmod -- 0750 " + r + "/new",
		"chmod -- 0640 " + r + "/new/compose.yaml",
	}
	if res.Entry.Result != ResultApplied || strings.Join(s.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("%s %q", brief(res.Entry), s.calls)
	}
}

func TestStackFolderUndoRestoresEveryTouchedEntry(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid()+1)
	buildStacks(t, s.root)
	res := s.apply(t)
	touched := len(strings.Fields(s.calls[0])) - 4
	s.calls = nil
	undo, err := s.runner.Undo(context.Background(), res.Entry.ID)
	if err != nil || undo.Entry.Result != ResultApplied {
		t.Fatalf("%v %s", err, brief(undo.Entry))
	}
	uid, gid := os.Getuid(), os.Getgid()
	var chowned, chmodded []string
	for _, c := range s.calls {
		argv := splitDisplay(c)
		switch argv[0] {
		case "chown":
			if argv[1] != "-h" || argv[3] != fmt.Sprintf("%d:%d", uid, gid) {
				t.Errorf("unexpected %q", c)
			}
			chowned = append(chowned, argv[4:]...)
		case "chmod":
			chmodded = append(chmodded, argv[3:]...)
		}
	}
	if len(chowned) != touched || len(chmodded) != touched {
		t.Errorf("restored %d owners and %d modes of %d entries", len(chowned), len(chmodded), touched)
	}
	if contains(chowned, s.root+"/web/data") || !contains(chmodded, s.root+"/web/.env") || !contains(chmodded, s.root) {
		t.Errorf("chown %q chmod %q", chowned, chmodded)
	}
	if all := strings.Join(s.calls, "\n"); !strings.Contains(all, "chmod -- 0600 "+s.root+"/web/.env") || !strings.Contains(all, "chmod -- 0644 ") || !strings.Contains(all, "chmod -- 0755 "+s.root+" ") {
		t.Errorf("modes: %q", s.calls)
	}
}

func TestStackFolderFailingStackCheckRestoresEverything(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid()+1)
	buildStacks(t, s.root)
	s.docker.stacks = stackRow(idWeb, "demo", s.root+"/web", s.root+"/web/compose.yaml")
	bad := healthy(idWeb, "demo-web-1")
	bad.Health = "unhealthy"
	ok := healthy(idWeb, "demo-web-1")
	s.docker.states = [][]fakeContainer{{ok}, {ok}, {ok}, {bad}}
	res := s.apply(t)
	e := res.Entry
	if e.Result != ResultRestored || !e.Restored || e.Check != CheckFailed ||
		e.CheckError != "stack demo, container demo-web-1: the container became unhealthy" {
		t.Fatalf("%s", brief(e))
	}
	last := s.calls[len(s.calls)-1]
	if !strings.HasPrefix(s.calls[4], "chown -h -- ") || !strings.HasPrefix(last, "chmod -- 0") || !strings.Contains(strings.Join(s.calls[4:], "\n"), s.root+"/db/docker-compose.yml") {
		t.Fatalf("calls %q", s.calls)
	}
}

func TestStackFolderServiceAccessFailuresRestoreEverything(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		read       func(string) error
		write      func(string) error
	}{
		{"read of a compose file", "stack db: the service cannot read ",
			func(p string) error {
				if strings.HasSuffix(p, "docker-compose.yml") {
					return errors.New("not readable")
				}
				return nil
			}, func(string) error { return nil }},
		{"write of an env file", "stack web: the service cannot write ",
			func(string) error { return nil },
			func(p string) error {
				if strings.HasSuffix(p, ".env") {
					return errors.New("not writable")
				}
				return nil
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStackFolderFixture(t, os.Getgid()+1)
			buildStacks(t, s.root)
			s.host.Stacks.CanRead, s.host.Stacks.CanWrite = tc.read, tc.write
			s.register(t)
			e := s.apply(t).Entry
			if e.Result != ResultRestored || !e.Restored || e.Check != CheckFailed || !strings.HasPrefix(e.CheckError, tc.want) {
				t.Fatalf("%s", brief(e))
			}
			if !strings.HasPrefix(s.calls[4], "chown -h -- ") {
				t.Fatalf("calls %q", s.calls)
			}
		})
	}
}

func TestStackFolderFailingCommandRestores(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid()+1)
	buildStacks(t, s.root)
	s.runner.Exec = failingRun{s.folderWorld, "chmod -- 0640"}
	e := s.apply(t).Entry
	if e.Result != ResultRestored || !e.Restored {
		t.Fatalf("%s", brief(e))
	}
}

func TestStackFolderOnlyChecksWhatItSetsOrReads(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid()+1)
	r := s.root
	write(t, r+"/web/compose.yaml", 0o644)
	write(t, r+"/web/.env", 0o600)
	var written []string
	s.host.Stacks.CanWrite = func(p string) error { written = append(written, p); return nil }
	s.register(t)
	s.apply(t)
	if strings.Join(written, ",") != r+"/web/.env" {
		t.Errorf("write checks %v", written)
	}
}

func TestStackFolderLinksStayUnchanged(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid()+1)
	r := s.root
	write(t, r+"/web/compose.yaml", 0o644)
	write(t, r+"/elsewhere/real.yaml", 0o644)
	if err := os.Symlink(r+"/elsewhere/real.yaml", r+"/web/compose.yml"); err != nil {
		t.Fatal(err)
	}
	write(t, r+"/elsewhere/real.env", 0o600)
	if err := os.Symlink(r+"/elsewhere/real.env", r+"/web/.env"); err != nil {
		t.Fatal(err)
	}
	p, err := s.host.PreviewStackFolder(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]string{}
	for _, k := range p.Stacks[0].Kept {
		kept[k.Path] = k.Reason
	}
	if !strings.Contains(kept[r+"/web/compose.yml"], "link") || !strings.Contains(kept[r+"/web/.env"], "link") {
		t.Errorf("kept %+v", kept)
	}
	s.apply(t)
	if all := strings.Join(s.calls, "\n"); strings.Contains(all, "compose.yml") || strings.Contains(all, ".env") || strings.Contains(all, "elsewhere") {
		t.Errorf("a link or its target was changed:\n%s", all)
	}
}

func TestStackFolderRefusals(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid()+1)
	r := s.root
	write(t, r+"/web/compose.yaml", 0o644)
	if err := os.Symlink(r, r+"-link"); err != nil {
		t.Fatal(err)
	}
	empty := r + "-empty"
	mkdir(t, empty+"/none", 0o755)
	special := r + "-special"
	write(t, special+"/web/compose.yaml", 0o644)
	if err := os.Chmod(special+"/web", os.ModeSetgid|0o755); err != nil {
		t.Fatal(err)
	}
	notAFolder := r + "/web/compose.yaml"
	for name, tc := range map[string]struct{ path, want string }{
		"system path":  {"/etc/demo-stacks", "system path"},
		"root":         {"/", "system path"},
		"link":         {r + "-link", "link"},
		"missing":      {r + "/absent", "does not exist"},
		"no stacks":    {empty, "holds no stack"},
		"not a folder": {notAFolder, "not a folder"},
		"special bit":  {special, "special bit"},
		"below a link": {r + "-link/web", "link"},
	} {
		_, err := s.runner.Apply(context.Background(), ActionFolderSetupStacks, StackFolderArgs(tc.path), nil)
		var ref *RefusedError
		if !errors.As(err, &ref) || !strings.Contains(ref.Msg, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	_, err := s.runner.Registry.Describe(ActionFolderSetupStacks, []string{"srv/relative"}, nil)
	var arg *ArgumentError
	if !errors.As(err, &arg) {
		t.Errorf("relative: %v", err)
	}
	if _, err := s.runner.Registry.Describe(ActionFolderSetupStacks, nil, nil); !errors.As(err, &arg) {
		t.Errorf("no argument: %v", err)
	}
	if len(s.calls) != 0 {
		t.Fatalf("executed %v", s.calls)
	}
}

func TestStackFolderNeedsTheGroupSysmin(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid()+1)
	write(t, s.root+"/web/compose.yaml", 0o644)
	if err := os.WriteFile(s.accounts.GroupPath, []byte("demo-group:x:4343:demo-user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.runner.Apply(context.Background(), ActionFolderSetupStacks, StackFolderArgs(s.root), nil)
	var ref *RefusedError
	if !errors.As(err, &ref) || !strings.Contains(ref.Msg, "no local group sysmin") || len(s.calls) != 0 {
		t.Fatalf("%v %v", err, s.calls)
	}
}

func TestStackFolderRefusesWhenTheStacksCannotBeChecked(t *testing.T) {
	s := newStackFolderFixture(t, os.Getgid()+1)
	write(t, s.root+"/web/compose.yaml", 0o644)
	s.host.Stacks = StackCheck{Docker: failingDocker{err: errors.New("docker: cannot connect")}}
	s.register(t)
	_, err := s.runner.Apply(context.Background(), ActionFolderSetupStacks, StackFolderArgs(s.root), nil)
	var ref *RefusedError
	if !errors.As(err, &ref) || !strings.Contains(ref.Msg, "cannot be checked") || len(s.calls) != 0 {
		t.Fatalf("%v %v", err, s.calls)
	}
}

func TestStackFolderInverseRefusesAnUnusableRecord(t *testing.T) {
	f := folderActions{}
	for _, raw := range []string{
		`{"path":"/srv/demo-stacks","entries":[]}`,
		`{"path":"/srv/demo-stacks","entries":[{"path":"/srv/other/x","kind":"file","uid":0,"gid":0,"mode":416}]}`,
		`{"path":"/srv/demo-stacks","entries":[{"path":"/srv/demo-stacks/x","kind":"link","uid":0,"gid":0,"mode":416}]}`,
		`{"path":"srv","entries":[]}`,
	} {
		if _, err := f.inverseSetupStacks([]byte(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestDefaultRegistersTheStackFolderAction(t *testing.T) {
	if _, ok := Default().Lookup(ActionFolderSetupStacks); !ok {
		t.Fatal("not registered")
	}
}

func brief(e Entry) string {
	return fmt.Sprintf("result %s, check %s, error %q, check error %q", e.Result, e.Check, e.Error, e.CheckError)
}
