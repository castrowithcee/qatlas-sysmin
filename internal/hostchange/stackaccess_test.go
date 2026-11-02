package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// accessFixture is a stack folder fixture in which the service user is an ID nobody owns, member of the
// group sysmin only, and the folder holding the tree can be passed by everyone.
type accessFixture struct {
	*stackFolderFixture
	sysminGID int
}

func newAccessFixture(t *testing.T) *accessFixture { return newAccessFixtureGID(t, os.Getgid()+1) }

func newAccessFixtureGID(t *testing.T, gid int) *accessFixture {
	t.Helper()
	s := newStackFolderFixture(t, gid)
	s.host.service = func() (serviceIdentity, error) { return serviceIdentity{uid: os.Getuid() + 1, gids: []int{gid}}, nil }
	s.register(t)
	for d := filepath.Dir(s.root); d != filepath.Dir(filepath.Dir(d)) && d != os.TempDir(); d = filepath.Dir(d) {
		if err := os.Chmod(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(filepath.Base(d), "Test") {
			break
		}
	}
	return &accessFixture{s, gid}
}

func (a *accessFixture) apply(t *testing.T, stack string) Result {
	t.Helper()
	res, err := a.runner.Apply(context.Background(), ActionStackSetupAccess, StackAccessArgs(stack), nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// closedStack builds root/closed (0700, the service cannot pass) holding the stacks web and db.
func (a *accessFixture) closedStack(t *testing.T) string {
	t.Helper()
	r := a.root
	write(t, r+"/closed/web/compose.yaml", 0o644)
	write(t, r+"/closed/web/.env", 0o600)
	write(t, r+"/closed/web/data/state.db", 0o644)
	write(t, r+"/closed/db/compose.yaml", 0o644)
	mkdir(t, r+"/closed", 0o700)
	return r + "/closed/web"
}

func needACLSupport(t *testing.T, dir string) {
	t.Helper()
	probe := filepath.Join(dir, "probe")
	mkdir(t, probe, 0o755)
	acl := encodeACL([]aclEntry{{aclUserObj, 7, aclNoID}, {aclGroupObj, 5, aclNoID}, {aclGroup, 1, 4343}, {aclMask, 5, aclNoID}, {aclOther, 5, aclNoID}})
	if err := unix.Setxattr(probe, aclXattr, acl, 0); err != nil {
		t.Skipf("no ACL support here: %v", err)
	}
}

func TestStackAccessSetsUpOneStackAndOnlyClosedParents(t *testing.T) {
	a := newAccessFixture(t)
	stack := a.closedStack(t)
	r := a.root
	res := a.apply(t, stack)
	if res.Entry.Result != ResultApplied || res.Entry.Check != CheckPassed || !res.Entry.Reversible {
		t.Fatalf("%s", brief(res.Entry))
	}
	want := []string{
		"[builtin] setfacl -m g:sysmin:x " + r + "/closed",
		"chown -h -- root:sysmin " + stack + " " + stack + "/.env " + stack + "/compose.yaml",
		"chmod -- 0750 " + stack,
		"chmod -- 0640 " + stack + "/compose.yaml",
		"chmod -- 0660 " + stack + "/.env",
	}
	if strings.Join(a.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(a.calls, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(res.Entry.Sentence, stack) || !strings.Contains(res.Entry.Sentence, "parent folder "+r+"/closed through an ACL entry") {
		t.Errorf("sentence %q", res.Entry.Sentence)
	}
}

func TestStackAccessLeavesPassableParentsAlone(t *testing.T) {
	a := newAccessFixture(t)
	write(t, a.root+"/open/web/compose.yaml", 0o644)
	mkdir(t, a.root+"/open", 0o751) // others may search
	a.apply(t, a.root+"/open/web")
	for _, c := range a.calls {
		if strings.Contains(c, "setfacl") {
			t.Errorf("unexpected %s", c)
		}
	}
}

func TestStackAccessNamesEveryClosedParent(t *testing.T) {
	a := newAccessFixture(t)
	r := a.root
	write(t, r+"/outer/inner/web/compose.yaml", 0o644)
	mkdir(t, r+"/outer/inner", 0o700)
	mkdir(t, r+"/outer", 0o701)
	p, err := a.host.PreviewStackAccess(context.Background(), r+"/outer/inner/web")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Parents) != 1 || p.Parents[0].Path != r+"/outer/inner" || p.Parents[0].HasACL {
		t.Fatalf("%+v", p.Parents)
	}
	mkdir(t, r+"/outer", 0o700)
	p, err = a.host.PreviewStackAccess(context.Background(), r+"/outer/inner/web")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Parents) != 2 || p.Parents[0].Path != r+"/outer" || p.Parents[1].Path != r+"/outer/inner" {
		t.Fatalf("%+v", p.Parents)
	}
	if got := p.Parents[0].Current.String(); !strings.HasSuffix(got, " 0700") {
		t.Errorf("current %s", got)
	}
	plan, err := a.runner.Registry.Describe(ActionStackSetupAccess, []string{r + "/outer/inner/web"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Sentence, "parent folders "+r+"/outer, "+r+"/outer/inner through") {
		t.Errorf("sentence %q", plan.Sentence)
	}
}

func TestStackAccessPreviewJSON(t *testing.T) {
	a := newAccessFixture(t)
	stack := a.closedStack(t)
	data, err := a.runner.Registry.Preview(context.Background(), ActionStackSetupAccess, []string{stack})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Path  string `json:"path"`
		Stack struct {
			Name    string            `json:"name"`
			Changes []json.RawMessage `json:"changes"`
			Kept    []json.RawMessage `json:"kept"`
		} `json:"stack"`
		Parents []struct {
			Path    string `json:"path"`
			Current struct {
				Mode string `json:"mode"`
			} `json:"current"`
			HasACL bool `json:"has_acl"`
		} `json:"parents"`
		OthersLoseAccess bool `json:"others_lose_access"`
		NothingToDo      bool `json:"nothing_to_do"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Path != stack || m.Stack.Name != "web" || len(m.Stack.Changes) != 3 || m.Stack.Kept == nil || !m.OthersLoseAccess || m.NothingToDo ||
		len(m.Parents) != 1 || m.Parents[0].Path != a.root+"/closed" || m.Parents[0].Current.Mode != "0700" || m.Parents[0].HasACL {
		t.Fatalf("%s", raw)
	}
	if len(a.calls) != 0 {
		t.Errorf("the preview ran %v", a.calls)
	}
}

func TestStackAccessNothingToDo(t *testing.T) {
	a := newAccessFixtureGID(t, os.Getgid())
	a.setUpAsRoot(t)
	write(t, a.root+"/web/compose.yaml", 0o640)
	mkdir(t, a.root+"/web", 0o750)
	p, err := a.host.PreviewStackAccess(context.Background(), a.root+"/web")
	if err != nil || !p.NothingToDo || p.OthersLoseAccess || len(p.Parents) != 0 {
		t.Fatalf("%v %+v", err, p)
	}
	_, err = a.runner.Apply(context.Background(), ActionStackSetupAccess, StackAccessArgs(a.root+"/web"), nil)
	var ref *RefusedError
	if !errors.As(err, &ref) || !strings.Contains(ref.Msg, "nothing to change") || len(a.calls) != 0 {
		t.Fatalf("%v %v", err, a.calls)
	}
}

func TestStackAccessUndoRestoresEntriesAndParents(t *testing.T) {
	a := newAccessFixture(t)
	stack := a.closedStack(t)
	res := a.apply(t, stack)
	a.calls = nil
	undo, err := a.runner.Undo(context.Background(), res.Entry.ID)
	if err != nil || undo.Entry.Result != ResultApplied {
		t.Fatalf("%v %s", err, brief(undo.Entry))
	}
	all := strings.Join(a.calls, "\n")
	for _, w := range []string{
		"chmod -- 0600 " + stack + "/.env",
		"chmod -- 0755 " + stack,
		"[builtin] acl-restore " + a.root + "/closed 0700 none",
	} {
		if !contains(a.calls, w) {
			t.Errorf("missing %q in\n%s", w, all)
		}
	}
	if strings.Contains(all, a.root+"/closed/db") {
		t.Errorf("touches the sibling stack:\n%s", all)
	}
}

func TestStackAccessUndoRestoresAnExistingACL(t *testing.T) {
	a := newAccessFixture(t)
	needACLSupport(t, a.root)
	stack := a.closedStack(t)
	prev := encodeACL([]aclEntry{{aclUserObj, 7, aclNoID}, {aclGroupObj, 0, aclNoID}, {aclGroup, 4, 4343}, {aclMask, 4, aclNoID}, {aclOther, 0, aclNoID}})
	if err := unix.Setxattr(a.root+"/closed", aclXattr, prev, 0); err != nil {
		t.Fatal(err)
	}
	p, err := a.host.PreviewStackAccess(context.Background(), stack)
	if err != nil || len(p.Parents) != 1 || !p.Parents[0].HasACL {
		t.Fatalf("%v %+v", err, p)
	}
	res := a.apply(t, stack)
	a.calls = nil
	if _, err := a.runner.Undo(context.Background(), res.Entry.ID); err != nil {
		t.Fatal(err)
	}
	if want := aclRestoreCommand(a.root+"/closed", 0o740, prev).Display(); !contains(a.calls, want) {
		t.Fatalf("want %s in\n%s", want, strings.Join(a.calls, "\n"))
	}
}

func TestStackAccessFailingCheckRestoresEverything(t *testing.T) {
	a := newAccessFixture(t)
	stack := a.closedStack(t)
	a.host.Stacks.CanRead = func(p string) error {
		if strings.HasSuffix(p, "compose.yaml") {
			return errors.New("not readable")
		}
		return nil
	}
	a.register(t)
	res := a.apply(t, stack)
	e := res.Entry
	if e.Result != ResultRestored || !e.Restored || e.Check != CheckFailed || !strings.Contains(e.CheckError, "the service cannot read ") {
		t.Fatalf("%s", brief(e))
	}
	if last := a.calls[len(a.calls)-1]; !strings.HasPrefix(last, "[builtin] acl-restore "+a.root+"/closed 0700 none") {
		t.Fatalf("calls %q", a.calls)
	}
}

func TestStackAccessRefusals(t *testing.T) {
	a := newAccessFixture(t)
	r := a.root
	write(t, r+"/web/compose.yaml", 0o644)
	if err := os.Symlink(r+"/web", r+"/web-link"); err != nil {
		t.Fatal(err)
	}
	mkdir(t, r+"/plain", 0o755)
	special := r + "/special"
	write(t, special+"/compose.yaml", 0o644)
	if err := os.Chmod(special, os.ModeSetgid|0o755); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct{ path, want string }{
		"system path": {"/etc/demo-stack", "system path"},
		"root":        {"/", "system path"},
		"link":        {r + "/web-link", "link"},
		"missing":     {r + "/absent", "does not exist"},
		"no compose":  {r + "/plain", "not a stack"},
		"a file":      {r + "/web/compose.yaml", "not a folder"},
		"special bit": {special, "special bit"},
	} {
		_, err := a.runner.Apply(context.Background(), ActionStackSetupAccess, StackAccessArgs(tc.path), nil)
		var ref *RefusedError
		if !errors.As(err, &ref) || !strings.Contains(ref.Msg, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var arg *ArgumentError
	for name, args := range map[string][]string{"relative": {"srv/demo"}, "none": nil, "two": {r, r}} {
		if _, err := a.runner.Registry.Describe(ActionStackSetupAccess, args, nil); !errors.As(err, &arg) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(a.calls) != 0 {
		t.Fatalf("executed %v", a.calls)
	}
}

func TestStackAccessRefusesWhenTheEntryCannotHelp(t *testing.T) {
	a := newAccessFixture(t)
	needACLSupport(t, a.root)
	stack := a.closedStack(t)
	// A rule for the service user itself comes before every group rule, so an entry for the group cannot help.
	acl := encodeACL([]aclEntry{{aclUserObj, 7, aclNoID}, {aclUser, 0, uint32(os.Getuid() + 1)}, {aclGroupObj, 0, aclNoID}, {aclMask, 7, aclNoID}, {aclOther, 0, aclNoID}})
	if err := unix.Setxattr(a.root+"/closed", aclXattr, acl, 0); err != nil {
		t.Fatal(err)
	}
	_, err := a.runner.Apply(context.Background(), ActionStackSetupAccess, StackAccessArgs(stack), nil)
	var ref *RefusedError
	if !errors.As(err, &ref) || !strings.Contains(ref.Msg, "would still not pass") || len(a.calls) != 0 {
		t.Fatalf("%v %v", err, a.calls)
	}
}

func TestStackAccessInverseRefusesAnUnusableRecord(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterFolderActions(reg, FolderHost{}); err != nil {
		t.Fatal(err)
	}
	def, _ := reg.Lookup(ActionStackSetupAccess)
	for name, rec := range map[string]string{
		"empty":           `{"path":"/srv/demo-stack","entries":[],"parents":[]}`,
		"system parent":   `{"path":"/srv/demo-stack","entries":[],"parents":[{"path":"/etc","uid":0,"gid":0,"mode":493,"acl":null}]}`,
		"foreign parent":  `{"path":"/srv/demo-stack","entries":[],"parents":[{"path":"/opt","uid":0,"gid":0,"mode":493,"acl":null}]}`,
		"broken acl":      `{"path":"/srv/demo-stack","entries":[],"parents":[{"path":"/srv","uid":0,"gid":0,"mode":493,"acl":"AQID"}]}`,
		"unusable parent": `{"path":"/srv/demo-stack","entries":[],"parents":[{"path":"/srv/","uid":0,"gid":0,"mode":493,"acl":null}]}`,
	} {
		if _, err := def.Inverse(json.RawMessage(rec)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	plan, err := def.Inverse(json.RawMessage(`{"path":"/srv/demo-stack","entries":[],"parents":[{"path":"/srv","uid":0,"gid":0,"mode":448,"acl":null}]}`))
	if err != nil || len(plan.Commands) != 1 || plan.Commands[0].Display() != "[builtin] acl-restore /srv 0700 none" {
		t.Fatalf("%v %v", err, plan.Commands)
	}
}

func TestDefaultRegistersTheStackAccessAction(t *testing.T) {
	if _, ok := Default().Lookup(ActionStackSetupAccess); !ok {
		t.Fatal("not registered")
	}
}

func TestGrantSearchBuildsTheACL(t *testing.T) {
	const sysmin = 990
	for _, tc := range []struct {
		name string
		cur  []aclEntry
		mode uint32
		want []aclEntry
		fail string
	}{
		{"no ACL, closed group bits", nil, 0o700,
			[]aclEntry{{aclUserObj, 7, aclNoID}, {aclGroupObj, 0, aclNoID}, {aclGroup, 1, sysmin}, {aclMask, 1, aclNoID}, {aclOther, 0, aclNoID}}, ""},
		{"no ACL, group may search", nil, 0o750,
			[]aclEntry{{aclUserObj, 7, aclNoID}, {aclGroupObj, 5, aclNoID}, {aclGroup, 1, sysmin}, {aclMask, 5, aclNoID}, {aclOther, 0, aclNoID}}, ""},
		{"ACL without the search bit in the mask",
			[]aclEntry{{aclUserObj, 7, aclNoID}, {aclUser, 4, 1001}, {aclGroupObj, 4, aclNoID}, {aclMask, 4, aclNoID}, {aclOther, 0, aclNoID}}, 0o740,
			[]aclEntry{{aclUserObj, 7, aclNoID}, {aclUser, 4, 1001}, {aclGroupObj, 4, aclNoID}, {aclGroup, 1, sysmin}, {aclMask, 5, aclNoID}, {aclOther, 0, aclNoID}}, ""},
		{"named user would gain through the mask",
			[]aclEntry{{aclUserObj, 7, aclNoID}, {aclUser, 7, 1001}, {aclGroupObj, 4, aclNoID}, {aclMask, 4, aclNoID}, {aclOther, 0, aclNoID}}, 0o740,
			nil, "would widen"},
		{"owning group would gain through the mask",
			[]aclEntry{{aclUserObj, 7, aclNoID}, {aclUser, 4, 1001}, {aclGroupObj, 5, aclNoID}, {aclMask, 4, aclNoID}, {aclOther, 0, aclNoID}}, 0o740,
			nil, "would widen"},
		{"another group would gain through the mask",
			[]aclEntry{{aclUserObj, 7, aclNoID}, {aclGroupObj, 4, aclNoID}, {aclGroup, 3, 2002}, {aclMask, 4, aclNoID}, {aclOther, 0, aclNoID}}, 0o740,
			nil, "would widen"},
		{"entry of the group exists",
			[]aclEntry{{aclUserObj, 7, aclNoID}, {aclGroupObj, 0, aclNoID}, {aclGroup, 4, sysmin}, {aclMask, 7, aclNoID}, {aclOther, 0, aclNoID}}, 0o700,
			[]aclEntry{{aclUserObj, 7, aclNoID}, {aclGroupObj, 0, aclNoID}, {aclGroup, 5, sysmin}, {aclMask, 7, aclNoID}, {aclOther, 0, aclNoID}}, ""},
	} {
		var cur []byte
		if tc.cur != nil {
			cur = encodeACL(tc.cur)
		}
		got, err := grantSearch(cur, tc.mode, sysmin)
		if tc.fail != "" {
			if err == nil || !strings.Contains(err.Error(), tc.fail) {
				t.Errorf("%s: err = %v", tc.name, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if want := encodeACL(tc.want); string(got) != string(want) {
			t.Errorf("%s: got %x want %x", tc.name, got, want)
		}
		if _, err := parseACL(got); err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	if _, err := grantSearch([]byte{1, 2, 3}, 0o700, sysmin); err == nil {
		t.Error("a broken ACL was accepted")
	}
}

// The entry never gives any other entry more effective permission than it had.
func TestGrantSearchKeepsEveryOtherEffectivePermission(t *testing.T) {
	effective := func(es []aclEntry) map[[2]uint32]uint16 {
		mask := uint16(7)
		for _, e := range es {
			if e.Tag == aclMask {
				mask = e.Perm
			}
		}
		out := map[[2]uint32]uint16{}
		for _, e := range es {
			p := e.Perm
			if e.Tag == aclUser || e.Tag == aclGroup || e.Tag == aclGroupObj {
				p &= mask
			}
			out[[2]uint32{uint32(e.Tag), e.ID}] = p
		}
		return out
	}
	base := []aclEntry{{aclUserObj, 7, aclNoID}, {aclUser, 5, 1001}, {aclGroupObj, 4, aclNoID}, {aclGroup, 4, 2002}, {aclMask, 5, aclNoID}, {aclOther, 0, aclNoID}}
	next, err := grantSearch(encodeACL(append([]aclEntry(nil), base...)), 0o750, 990)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := parseACL(next)
	before, now := effective(base), effective(after)
	for k, p := range before {
		if now[k] != p {
			t.Errorf("entry %v: %d before, %d after", k, p, now[k])
		}
	}
}

func TestSearchAllowedFollowsTheACLRules(t *testing.T) {
	const owner, group, svc, svcGroup = 100, 200, 300, 990
	acl := func(es ...aclEntry) []byte { return encodeACL(es) }
	for _, tc := range []struct {
		name string
		uid  int
		gids []int
		mode uint32
		acl  []byte
		want bool
	}{
		{"others may search", svc, nil, 0o701, nil, true},
		{"nobody may search", svc, []int{svcGroup}, 0o700, nil, false},
		{"group bits", svc, []int{group}, 0o710, nil, true},
		{"owner bits decide for the owner", owner, []int{group}, 0o077, nil, false},
		{"group bits without search", svc, []int{group}, 0o705, nil, false},
		{"named group with mask", svc, []int{svcGroup}, 0o710, acl(aclEntry{aclUserObj, 7, aclNoID}, aclEntry{aclGroupObj, 0, aclNoID}, aclEntry{aclGroup, 1, svcGroup}, aclEntry{aclMask, 1, aclNoID}, aclEntry{aclOther, 0, aclNoID}), true},
		{"mask withholds", svc, []int{svcGroup}, 0o700, acl(aclEntry{aclUserObj, 7, aclNoID}, aclEntry{aclGroupObj, 0, aclNoID}, aclEntry{aclGroup, 1, svcGroup}, aclEntry{aclMask, 0, aclNoID}, aclEntry{aclOther, 0, aclNoID}), false},
		{"named user decides first", svc, []int{svcGroup}, 0o710, acl(aclEntry{aclUserObj, 7, aclNoID}, aclEntry{aclUser, 0, svc}, aclEntry{aclGroupObj, 0, aclNoID}, aclEntry{aclGroup, 1, svcGroup}, aclEntry{aclMask, 7, aclNoID}, aclEntry{aclOther, 0, aclNoID}), false},
		{"matching group without search denies other", svc, []int{svcGroup}, 0o701, acl(aclEntry{aclUserObj, 7, aclNoID}, aclEntry{aclGroupObj, 0, aclNoID}, aclEntry{aclGroup, 4, svcGroup}, aclEntry{aclMask, 7, aclNoID}, aclEntry{aclOther, 1, aclNoID}), false},
	} {
		if got := searchAllowed(tc.uid, tc.gids, owner, group, tc.mode, tc.acl); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestBuiltinACLStepsChangeOnlyTheACL(t *testing.T) {
	root := t.TempDir()
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	needACLSupport(t, real)
	dir := filepath.Join(real, "closed")
	mkdir(t, dir, 0o700)
	var before unix.Stat_t
	if err := unix.Stat(dir, &before); err != nil {
		t.Fatal(err)
	}
	gid := os.Getgid() + 1
	if err := grantSearchAt(dir, gid); err != nil {
		t.Fatal(err)
	}
	acl, err := readACL(dir)
	if err != nil || acl == nil {
		t.Fatalf("%x %v", acl, err)
	}
	es, err := parseACL(acl)
	if err != nil || len(es) != 5 {
		t.Fatalf("%v %v", es, err)
	}
	var after unix.Stat_t
	if err := unix.Stat(dir, &after); err != nil {
		t.Fatal(err)
	}
	if after.Uid != before.Uid || after.Gid != before.Gid || after.Mode&0o7707 != before.Mode&0o7707 {
		t.Errorf("owner, group or the owner and other bits changed: %o -> %o", before.Mode, after.Mode)
	}
	if err := grantSearchAt(dir, gid); err != nil {
		t.Errorf("a second grant: %v", err)
	}
	if again, _ := readACL(dir); string(again) != string(acl) {
		t.Error("a second grant changed the ACL")
	}
	// The restore step takes the ACL away again and puts the permission bits back.
	if err := runBuiltin(aclRestoreCommand(dir, before.Mode&0o7777, nil), nil); err != nil {
		t.Fatal(err)
	}
	var restored unix.Stat_t
	if err := unix.Stat(dir, &restored); err != nil {
		t.Fatal(err)
	}
	if now, _ := readACL(dir); now != nil || restored.Mode != before.Mode {
		t.Errorf("ACL %x, mode %o, want none and %o", now, restored.Mode, before.Mode)
	}
	// A previous ACL comes back byte for byte.
	if err := runBuiltin(aclRestoreCommand(dir, 0o710, acl), nil); err != nil {
		t.Fatal(err)
	}
	if now, _ := readACL(dir); string(now) != string(acl) {
		t.Errorf("ACL %x, want %x", now, acl)
	}
}

func TestBuiltinACLStepsRefuseWhatTheyShouldNot(t *testing.T) {
	root := t.TempDir()
	real, _ := filepath.EvalSymlinks(root)
	mkdir(t, real+"/target", 0o700)
	if err := os.Symlink(real+"/target", real+"/link"); err != nil {
		t.Fatal(err)
	}
	if err := grantSearchAt(real+"/link", os.Getgid()); err == nil {
		t.Error("followed a link")
	}
	if err := restoreACLAt(real+"/link/", 0o700, nil); err == nil {
		t.Error("accepted an unclean path")
	}
	for _, p := range []string{"/etc", "/usr/local", "/", "relative"} {
		if err := grantSearchAt(p, os.Getgid()); err == nil {
			t.Errorf("changed %s", p)
		}
	}
	for name, c := range map[string]Command{
		"other permission": {Argv: []string{stepACLGrant, "-m", "g:sysmin:rwx", real}, Builtin: true},
		"other group":      {Argv: []string{stepACLGrant, "-m", "g:other:x", real}, Builtin: true},
		"missing dir":      {Argv: []string{stepACLGrant, "-m", "g:sysmin:x"}, Builtin: true},
		"bad mode":         {Argv: []string{stepACLRestore, real, "9999", aclNone}, Builtin: true},
		"bad acl":          {Argv: []string{stepACLRestore, real, "0700", "zz"}, Builtin: true},
		"broken acl":       {Argv: []string{stepACLRestore, real, "0700", "010203"}, Builtin: true},
	} {
		if err := validateCommand(c); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if err := runBuiltin(c, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStackAccessWithoutACLSupportOnParents(t *testing.T) {
	a := newAccessFixture(t)
	stack := a.closedStack(t)
	r := a.root
	a.host.readACL = func(string) ([]byte, error) { return nil, errACLUnsupported }
	a.register(t)

	// A parent that needs the entry cannot get it here.
	_, err := a.host.PreviewStackAccess(context.Background(), stack)
	var ref *RefusedError
	if !errors.As(err, &ref) || !strings.Contains(ref.Msg, r+"/closed") || !strings.Contains(ref.Msg, "does not support ACLs") {
		t.Fatalf("closed parent: %v", err)
	}

	// Passable parents need no entry, so missing ACL support does not matter.
	mkdir(t, r+"/closed", 0o755)
	p, err := a.host.PreviewStackAccess(context.Background(), stack)
	if err != nil || len(p.Parents) != 0 {
		t.Fatalf("%v %+v", err, p.Parents)
	}

	// Any other read error still refuses.
	a.host.readACL = func(string) ([]byte, error) { return nil, errors.New("read failed") }
	if _, err := a.host.PreviewStackAccess(context.Background(), stack); !errors.As(err, &ref) || !strings.Contains(ref.Msg, "read failed") {
		t.Fatalf("other error: %v", err)
	}
}
