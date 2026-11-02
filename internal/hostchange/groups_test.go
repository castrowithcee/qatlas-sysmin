package hostchange

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// groupText is the group and gshadow file as the fake host wrote them, line by line in sorted order: a group
// created again is appended, like groupadd does, and the members keep the order they were listed in.
func (w *userWorld) groupText(t *testing.T) string {
	t.Helper()
	var out []string
	for _, path := range []string{w.host.Accounts.GroupPath, w.host.GShadow} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		slices.Sort(lines)
		out = append(out, strings.Join(lines, "\n"))
	}
	return strings.Join(out, "--\n")
}

func (f *userFixture) refuses(t *testing.T, id string, args []string, reason string) {
	t.Helper()
	before := f.groupText(t)
	_, err := f.runner.Apply(context.Background(), id, args, nil)
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("%s %q: err %v, want %q", id, args, err, reason)
	}
	if len(f.calls) != 0 || f.journalText(t) != "" || len(f.audited) != 0 || f.groupText(t) != before {
		t.Errorf("something ran or changed: %q", f.calls)
	}
}

func (f *userFixture) undoRefuses(t *testing.T, journalID, reason string) {
	t.Helper()
	f.calls = nil
	before := f.groupText(t)
	if _, err := f.runner.Undo(context.Background(), journalID); err == nil || !strings.Contains(err.Error(), reason) || len(f.calls) != 0 {
		t.Fatalf("undo: err %v calls %q, want %q", err, f.calls, reason)
	}
	if f.groupText(t) != before {
		t.Error("the refused undo changed the groups")
	}
}

func TestGroupCreateGetsAGIDFromTheSystemAndUndoDeletesExactlyThatGroup(t *testing.T) {
	f := newUserFixture(t, 1000)
	before := f.accountFiles(t) + f.groupText(t)
	res := f.apply(t, ActionGroupCreate, "", "demo-team")
	sameList(t, "executed", f.calls, []string{"groupadd demo-team"})
	sameList(t, "journaled", res.Entry.Commands, []string{"groupadd demo-team"})
	if g := f.group("demo-team"); g == nil || g.gid != 2002 || len(g.members) != 0 {
		t.Fatalf("group %+v", g)
	}
	var st groupState
	if err := decodeState(res.Entry.Previous, &st); err != nil || st.Name != "demo-team" || st.GID != 2002 {
		t.Fatalf("recorded %+v (%v): the system's GID belongs in the state", st, err)
	}
	if res.Entry.Check != CheckPassed || !res.Entry.Reversible {
		t.Fatalf("%+v", res.Entry)
	}

	f.calls = nil
	undo := f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"groupdel demo-team"})
	if !strings.Contains(undo.Entry.Sentence, "GID 2002") || f.accountFiles(t)+f.groupText(t) != before {
		t.Errorf("sentence %q, files differ:\n%s", undo.Entry.Sentence, f.groupText(t))
	}
}

func TestGroupCreateRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		reason string
	}{
		{"taken", []string{"projects"}, "the group projects exists already"},
		{"taken by a system group", []string{"docker"}, "exists already"},
		{"upper case", []string{"Demo"}, "not a valid group name"},
		{"space", []string{"demo team"}, "not a valid group name"},
		{"path", []string{"../x"}, "not a valid group name"},
		{"too long", []string{strings.Repeat("a", 33)}, "not a valid group name"},
		{"empty", []string{""}, "not a valid group name"},
		{"missing", nil, "needs a group name"},
		{"too many", []string{"demo-a", "demo-b"}, "needs a group name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newUserFixture(t, 1000).refuses(t, ActionGroupCreate, tc.args, tc.reason)
		})
	}
}

func TestGroupCreateMayUseTheNameOfAUser(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.users = append(f.users, worldUser{"demo-solo", 1500, 1001, "/nonexistent", "*", "19000"})
	f.flush()
	f.apply(t, ActionGroupCreate, "", "demo-solo")
}

func TestGroupCreateThatFailsChangesNothing(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.failOn = "groupadd demo-team"
	res, err := f.runner.Apply(context.Background(), ActionGroupCreate, []string{"demo-team"}, nil)
	if err != nil || res.Entry.Result != ResultFailed || f.group("demo-team") != nil || len(f.calls) != 1 {
		t.Fatalf("%v %+v calls %q", err, res.Entry, f.calls)
	}
}

func TestGroupCreateUndoRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*userFixture)
		reason string
	}{
		{"gained a member", func(f *userFixture) { f.group("demo-team").members = []string{"demo-user"} }, "has members now"},
		{"became a primary group", func(f *userFixture) { f.user("demo-user").gid = 2002 }, "primary group of demo-user now"},
		{"gone", func(f *userFixture) {
			f.groups = slices.DeleteFunc(f.groups, func(g worldGroup) bool { return g.name == "demo-team" })
		}, "no group demo-team any more"},
		{"another group of the name", func(f *userFixture) { f.group("demo-team").gid = 3000 }, "now has GID 3000 instead of 2002"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			res := f.apply(t, ActionGroupCreate, "", "demo-team")
			tc.setup(f)
			f.flush()
			f.undoRefuses(t, res.Entry.ID, tc.reason)
		})
	}
}

func TestGroupInversesRefuseInvalidRecordedStates(t *testing.T) {
	u := userActions{}
	for name, tc := range map[string]struct {
		inverse func(json.RawMessage) (Plan, error)
		valid   string
		bad     map[string]string
	}{
		"create": {u.inverseGroupCreate, `{"name":"demo-team","gid":2002,"members":[]}`, map[string]string{
			"name":          `{"name":"../x","gid":2002,"members":[]}`,
			"sudo":          `{"name":"sudo","gid":2002,"members":[]}`,
			"unsealed GID":  `{"name":"demo-team","gid":-1,"members":[]}`,
			"system GID":    `{"name":"demo-team","gid":27,"members":[]}`,
			"with members":  `{"name":"demo-team","gid":2002,"members":["demo-user"]}`,
			"unknown field": `{"name":"demo-team","gid":2002,"members":[],"more":1}`,
			"not an object": `[]`,
		}},
		"delete": {u.inverseGroupDelete, `{"name":"media","gid":2001,"members":["demo-user","demo-other"]}`, map[string]string{
			"name":          `{"name":"Bad Name","gid":2001,"members":[]}`,
			"sysmin":        `{"name":"sysmin","gid":990,"members":[]}`,
			"sudo":          `{"name":"sudo","gid":27,"members":[]}`,
			"gid":           `{"name":"media","gid":-1,"members":[]}`,
			"member":        `{"name":"media","gid":2001,"members":["a,b"]}`,
			"double member": `{"name":"media","gid":2001,"members":["demo-user","demo-user"]}`,
			"unknown field": `{"name":"media","gid":2001,"members":[],"more":1}`,
			"not an object": `"x"`,
		}},
		"rename": {u.inverseGroupRename, `{"old":"projects","new":"demo-projects","gid":2000}`, map[string]string{
			"old name":      `{"old":"../x","new":"demo-projects","gid":2000}`,
			"new name":      `{"old":"projects","new":"Bad","gid":2000}`,
			"same":          `{"old":"projects","new":"projects","gid":2000}`,
			"old is sudo":   `{"old":"sudo","new":"demo-projects","gid":27}`,
			"new is sysmin": `{"old":"projects","new":"sysmin","gid":2000}`,
			"gid":           `{"old":"projects","new":"demo-projects","gid":-5}`,
			"unknown field": `{"old":"projects","new":"demo-projects","gid":2000,"more":1}`,
			"not an object": `null`,
		}},
	} {
		if _, err := tc.inverse(json.RawMessage(tc.valid)); err != nil {
			t.Fatalf("%s: the valid state is refused: %v", name, err)
		}
		for what, state := range tc.bad {
			if _, err := tc.inverse(json.RawMessage(state)); err == nil {
				t.Errorf("%s %s: accepted", name, what)
			}
		}
	}
}

func TestGroupDeleteUndoCreatesTheGroupAgainWithTheSameGIDAndMembers(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.group("projects").members = []string{"demo-other", "demo-user", "demo-boss"}
	f.flush()
	before := f.groupText(t)
	res := f.apply(t, ActionGroupDelete, "", "projects")
	sameList(t, "executed", f.calls, []string{"groupdel projects"})
	if f.group("projects") != nil || !res.Entry.Reversible {
		t.Fatalf("%+v", res.Entry)
	}
	f.calls = nil
	undo := f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"groupadd -g 2000 projects", "gpasswd -M demo-other,demo-user,demo-boss projects"})
	sameList(t, "journaled undo", undo.Entry.Commands, f.calls)
	if got := f.groupText(t); got != before {
		t.Errorf("the group is not back as it was:\n%s\nwas:\n%s", got, before)
	}
	for _, want := range []string{"GID 2000", "demo-other, demo-user and demo-boss"} {
		if !strings.Contains(undo.Entry.Sentence, want) {
			t.Errorf("sentence %q lacks %q", undo.Entry.Sentence, want)
		}
	}
}

func TestGroupDeleteOfAGroupWithoutMembersNeedsNoMemberStep(t *testing.T) {
	f := newUserFixture(t, 1000)
	res := f.apply(t, ActionGroupDelete, "", "projects")
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"groupadd -g 2000 projects"})
}

func TestGroupDeleteAllowsSystemGroupsAndRoot(t *testing.T) {
	for _, name := range []string{"docker", "adm"} {
		f := newUserFixture(t, 1000)
		before := f.groupText(t)
		res := f.apply(t, ActionGroupDelete, "", name)
		if f.group(name) != nil {
			t.Fatalf("%s is still there", name)
		}
		f.undo(t, res.Entry.ID)
		if f.groupText(t) != before {
			t.Errorf("%s is not back as it was", name)
		}
	}
	f := newUserFixture(t, 1000)
	f.users[0].gid = 4 // root's primary group is adm here, so the group root is free to go
	f.flush()
	f.apply(t, ActionGroupDelete, "", "root")
	if f.group("root") != nil {
		t.Error("root is still there")
	}
}

func TestGroupDeleteRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		setup  func(*userFixture)
		reason string
	}{
		{"primary group", []string{"demo-user"}, nil, "the group demo-user is the primary group of demo-user and cannot be deleted"},
		{"primary group of two", []string{"projects"}, func(f *userFixture) {
			f.user("demo-user").gid, f.user("demo-other").gid = 2000, 2000
		}, "primary group of demo-user and demo-other"},
		{"root group", []string{"root"}, nil, "primary group of root"},
		{"sudo", []string{"sudo"}, nil, "never deleted"},
		{"sysmin", []string{"sysmin"}, nil, "never deleted"},
		{"unknown", []string{"demo-nobody"}, nil, "there is no group demo-nobody"},
		{"shared GID", []string{"projects"}, func(f *userFixture) {
			f.groups = append(f.groups, worldGroup{"demo-twin", 2000, nil})
		}, "shares GID 2000 with demo-twin"},
		{"member without an account", []string{"media"}, func(f *userFixture) {
			f.group("media").members = []string{"demo-user", "demo-gone"}
		}, "demo-gone is listed in the group media but is no local user"},
		{"group password", []string{"projects"}, func(f *userFixture) { f.gshadow["projects"] = "$6$demo:" }, "group password or group administrators"},
		{"group administrators", []string{"projects"}, func(f *userFixture) { f.gshadow["projects"] = "!:demo-admin" }, "group password or group administrators"},
		{"invalid name", []string{"../x"}, nil, "not a valid group name"},
		{"missing", nil, nil, "needs a group name"},
		{"too many", []string{"projects", "media"}, nil, "needs a group name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			if tc.setup != nil {
				tc.setup(f)
				f.flush()
			}
			f.refuses(t, ActionGroupDelete, tc.args, tc.reason)
		})
	}
}

func TestGroupDeleteUndoRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*userFixture)
		reason string
	}{
		{"name taken", func(f *userFixture) { f.groups = append(f.groups, worldGroup{"media", 3000, nil}) }, "a group media exists again"},
		{"GID taken", func(f *userFixture) { f.groups = append(f.groups, worldGroup{"demo-twin", 2001, nil}) }, "GID 2001 belongs to the group demo-twin"},
		{"member gone", func(f *userFixture) {
			f.users = slices.DeleteFunc(f.users, func(u worldUser) bool { return u.name == "demo-user" })
		}, "demo-user is no user any more"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			res := f.apply(t, ActionGroupDelete, "", "media")
			tc.setup(f)
			f.flush()
			f.undoRefuses(t, res.Entry.ID, tc.reason)
		})
	}
}

func TestGroupDeleteUndoThatFailsMidwayRemovesTheHalfRestoredGroup(t *testing.T) {
	f := newUserFixture(t, 1000)
	res := f.apply(t, ActionGroupDelete, "", "media")
	f.failOn = "gpasswd -M demo-user media"
	undo, err := f.runner.Undo(context.Background(), res.Entry.ID)
	if err != nil || undo.Entry.Result != ResultRestored || f.group("media") != nil {
		t.Fatalf("%v %+v group %+v", err, undo.Entry, f.group("media"))
	}
}

func TestGroupRenameKeepsGIDAndMembersAndUndoRenamesBack(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.group("media").members = []string{"demo-other", "demo-user"}
	f.gshadow["media"] = "!:demo-admin"
	f.flush()
	before := f.groupText(t)
	res := f.apply(t, ActionGroupRename, "", "media", "demo-media")
	sameList(t, "executed", f.calls, []string{"groupmod -n demo-media media"})
	g := f.group("demo-media")
	if g == nil || g.gid != 2001 || !slices.Equal(g.members, []string{"demo-other", "demo-user"}) || f.group("media") != nil {
		t.Fatalf("group %+v", g)
	}
	if res.Entry.Sentence != "Rename the group media to demo-media." || !res.Entry.Reversible || res.Entry.Check != CheckPassed {
		t.Fatalf("%+v", res.Entry)
	}
	if text := f.groupText(t); !strings.Contains(text, "demo-media:!:demo-admin:") {
		t.Errorf("the gshadow entry did not follow:\n%s", text)
	}

	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"groupmod -n media demo-media"})
	if got := f.groupText(t); got != before {
		t.Errorf("not back as it was:\n%s\nwas:\n%s", got, before)
	}
}

func TestGroupRenameAllowsDockerAndRoot(t *testing.T) {
	for _, name := range []string{"docker", "root"} {
		f := newUserFixture(t, 1000)
		before := f.groupText(t)
		res := f.apply(t, ActionGroupRename, "", name, "demo-renamed")
		if f.group("demo-renamed") == nil {
			t.Fatalf("%s was not renamed", name)
		}
		f.undo(t, res.Entry.ID)
		if f.groupText(t) != before {
			t.Errorf("%s is not back as it was", name)
		}
	}
}

func TestGroupRenameRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		reason string
	}{
		{"sudo", []string{"sudo", "demo-admins"}, "never renamed"},
		{"sysmin", []string{"sysmin", "demo-sysmin"}, "never renamed"},
		{"new name taken", []string{"media", "projects"}, "the group projects exists already"},
		{"to sudo", []string{"media", "sudo"}, "the group sudo exists already"},
		{"unknown", []string{"demo-nobody", "demo-x"}, "there is no group demo-nobody"},
		{"same name", []string{"media", "media"}, "is the current one"},
		{"invalid old", []string{"../x", "demo-x"}, "not a valid group name"},
		{"invalid new", []string{"media", "Demo X"}, "not a valid group name"},
		{"missing", []string{"media"}, "needs the current and the new group name"},
		{"too many", []string{"media", "a", "b"}, "needs the current and the new group name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newUserFixture(t, 1000).refuses(t, ActionGroupRename, tc.args, tc.reason)
		})
	}
}

func TestGroupRenameUndoRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		setup  func(*userFixture)
		reason string
	}{
		{"old name taken", func(f *userFixture) { f.groups = append(f.groups, worldGroup{"media", 3000, nil}) }, "the group media exists already"},
		{"renamed again", func(f *userFixture) { f.group("demo-media").name = "demo-other-name" }, "there is no group demo-media"},
		{"another group of the name", func(f *userFixture) { f.group("demo-media").gid = 3000 }, "now has GID 3000 instead of 2001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, 1000)
			res := f.apply(t, ActionGroupRename, "", "media", "demo-media")
			tc.setup(f)
			f.flush()
			f.undoRefuses(t, res.Entry.ID, tc.reason)
		})
	}
}

func TestGroupRenameThatFailsChangesNothing(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.failOn = "groupmod -n demo-media media"
	res, err := f.runner.Apply(context.Background(), ActionGroupRename, []string{"media", "demo-media"}, nil)
	if err != nil || res.Entry.Result != ResultFailed || f.group("media") == nil || len(f.calls) != 1 {
		t.Fatalf("%v %+v calls %q", err, res.Entry, f.calls)
	}
}
