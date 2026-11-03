package hostchange

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func (f *userFixture) members(group string) []string {
	g := f.group(group)
	if g == nil {
		return nil
	}
	s := slices.Clone(g.members)
	slices.Sort(s)
	return s
}

func TestGroupSetMembersAddsThreeAndUndoRestoresExactly(t *testing.T) {
	f := newUserFixture(t, 1000)
	before := f.accountFiles(t) + f.groupText(t)
	res := f.apply(t, ActionGroupSetMembers, "", "projects", "+demo-user", "+demo-other", "+demo-boss")
	sameList(t, "executed", f.calls, []string{
		"gpasswd -a demo-user projects", "gpasswd -a demo-other projects", "gpasswd -a demo-boss projects"})
	sameList(t, "journaled", res.Entry.Commands, f.calls)
	sameList(t, "members", f.members("projects"), []string{"demo-boss", "demo-other", "demo-user"})
	var st membersState
	if err := decodeState(res.Entry.Previous, &st); err != nil || st.Group != "projects" || st.GID != 2000 || len(st.Changes) != 3 ||
		st.Changes[0].UID != 1001 || !st.Changes[0].Add {
		t.Fatalf("recorded %+v (%v)", st, err)
	}
	if res.Entry.Check != CheckPassed || !res.Entry.Reversible {
		t.Fatalf("%+v", res.Entry)
	}
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{
		"gpasswd -d demo-user projects", "gpasswd -d demo-other projects", "gpasswd -d demo-boss projects"})
	if f.accountFiles(t)+f.groupText(t) != before {
		t.Errorf("files differ:\n%s", f.groupText(t))
	}
}

func TestGroupSetMembersRemovesTwoAndUndoRestoresExactly(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.group("media").members = []string{"demo-user", "demo-other", "demo-boss"}
	f.flush()
	before := f.groupText(t)
	res := f.apply(t, ActionGroupSetMembers, "", "media", "-demo-user", "-demo-boss")
	sameList(t, "executed", f.calls, []string{"gpasswd -d demo-user media", "gpasswd -d demo-boss media"})
	sameList(t, "members", f.members("media"), []string{"demo-other"})
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"gpasswd -a demo-user media", "gpasswd -a demo-boss media"})
	sameList(t, "members", f.members("media"), []string{"demo-boss", "demo-other", "demo-user"})
	if got := f.groupText(t); strings.Count(got, "media") != strings.Count(before, "media") {
		t.Errorf("files differ:\n%s", got)
	}
}

func TestGroupSetMembersMixedInOneStep(t *testing.T) {
	f := newUserFixture(t, 1000)
	res := f.apply(t, ActionGroupSetMembers, "", "media", "+demo-other", "-demo-user")
	sameList(t, "executed", f.calls, []string{"gpasswd -a demo-other media", "gpasswd -d demo-user media"})
	sameList(t, "members", f.members("media"), []string{"demo-other"})
	if !strings.HasPrefix(res.Entry.Sentence, "Add demo-other to the group media and remove demo-user from the group media") {
		t.Errorf("sentence %q", res.Entry.Sentence)
	}
	f.undo(t, res.Entry.ID)
	sameList(t, "members", f.members("media"), []string{"demo-user"})
}

func TestGroupSetMembersSudoAddsQsysminToo(t *testing.T) {
	f := newUserFixture(t, 1000)
	// demo-solo is in qsysmin already and must keep it when the change is undone.
	f.users = append(f.users, worldUser{"demo-solo", 1500, 1500, "/nonexistent", "*", "19000"})
	f.group("qsysmin").members = append(f.group("qsysmin").members, "demo-solo")
	f.flush()
	before := f.groupText(t)
	res := f.apply(t, ActionGroupSetMembers, "", "sudo", "+demo-user", "+demo-solo")
	sameList(t, "executed", f.calls, []string{
		"gpasswd -a demo-user sudo", "usermod -aG qsysmin demo-user", "gpasswd -a demo-solo sudo", "usermod -aG qsysmin demo-solo"})
	if !slices.Contains(f.members("qsysmin"), "demo-user") || !slices.Contains(f.members("sudo"), "demo-solo") {
		t.Fatalf("sudo %v qsysmin %v", f.members("sudo"), f.members("qsysmin"))
	}
	var st membersState
	if err := decodeState(res.Entry.Previous, &st); err != nil || !st.Changes[0].Qsysmin || st.Changes[1].Qsysmin {
		t.Fatalf("recorded %+v (%v): only the user who was not in qsysmin changed it", st, err)
	}
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"gpasswd -d demo-user sudo", "gpasswd -d demo-user qsysmin", "gpasswd -d demo-solo sudo"})
	if f.groupText(t) != before {
		t.Errorf("files differ:\n%s", f.groupText(t))
	}
}

func TestGroupSetMembersRemovingFromSudoFollowsTheAdminSwitch(t *testing.T) {
	f := newUserFixture(t, 1000)
	before := f.groupText(t)
	res := f.apply(t, ActionGroupSetMembers, "", "sudo", "-demo-other")
	sameList(t, "executed", f.calls, []string{"gpasswd -d demo-other sudo", "gpasswd -d demo-other qsysmin"})
	f.calls = nil
	f.undo(t, res.Entry.ID)
	sameList(t, "undo", f.calls, []string{"gpasswd -a demo-other sudo", "usermod -aG qsysmin demo-other"})
	if strings.Count(f.groupText(t), "demo-other") != strings.Count(before, "demo-other") {
		t.Errorf("files differ:\n%s", f.groupText(t))
	}

	f.calls = nil
	f.apply(t, ActionGroupSetMembers, "", "sudo", "-demo-boss"+optionNoQsysmin)
	sameList(t, "without qsysmin", f.calls, []string{"gpasswd -d demo-boss sudo"})
}

func TestGroupSetMembersRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		actor  int
		setup  func(*userFixture)
		args   []string
		reason string
	}{
		{"self from sudo", 1000, nil, []string{"sudo", "-demo-admin"}, "from yourself"},
		{"self from sudo among others", 1000, nil, []string{"sudo", "-demo-other", "-demo-admin"}, "from yourself"},
		{"self from qsysmin", 1000, nil, []string{"qsysmin", "-demo-admin"}, "from yourself"},
		{"last admins from sudo", 1003, nil, []string{"sudo", "-demo-admin", "-demo-other"}, "no administrator with access to qsysmin"},
		{"last admins from qsysmin", 1003, nil, []string{"qsysmin", "-demo-admin", "-demo-other"}, "no administrator with access to qsysmin"},
		{"no qsysmin to leave", 1000, nil, []string{"sudo", "-demo-boss"}, "not in the group qsysmin"},
		{"primary group", 1000, func(f *userFixture) { f.group("demo-user").members = []string{"demo-user"}; f.flush() },
			[]string{"demo-user", "-demo-user"}, "primary group of demo-user and cannot be removed"},
		{"already in its primary group", 1000, nil, []string{"demo-user", "+demo-user"}, "in the group demo-user already"},
		{"already a member", 1000, nil, []string{"media", "+demo-user"}, "in the group media already"},
		{"not a member", 1000, nil, []string{"media", "-demo-boss"}, "not a member of the group media"},
		{"unknown user", 1000, nil, []string{"media", "+demo-nobody"}, "no local user demo-nobody"},
		{"unknown user removed", 1000, nil, []string{"media", "-demo-nobody"}, "no local user demo-nobody"},
		{"unknown group", 1000, nil, []string{"demo-none", "+demo-user"}, "no group demo-none"},
		{"twice", 1000, nil, []string{"projects", "+demo-user", "-demo-user"}, "given twice"},
		{"twice added", 1000, nil, []string{"projects", "+demo-user", "+demo-user"}, "given twice"},
		{"no change", 1000, nil, []string{"projects"}, "needs a group name"},
		{"no group", 1000, nil, nil, "needs a group name"},
		{"no sign", 1000, nil, []string{"projects", "demo-user"}, "write +user"},
		{"bad user", 1000, nil, []string{"projects", "+Demo"}, "not a valid user name"},
		{"bad group", 1000, nil, []string{"../x", "+demo-user"}, "not a valid group name"},
		{"option outside sudo", 1000, nil, []string{"media", "-demo-user" + optionNoQsysmin}, "applies only to removing a user from sudo"},
		{"option on add", 1000, nil, []string{"sudo", "+demo-user" + optionNoQsysmin}, "applies only to removing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, tc.actor)
			if tc.setup != nil {
				tc.setup(f)
			}
			f.refuses(t, ActionGroupSetMembers, tc.args, tc.reason)
		})
	}
}

func TestGroupSetMembersRefusalChangesNothingEvenWhenOnlyTheLastUserIsWrong(t *testing.T) {
	f := newUserFixture(t, 1000)
	f.refuses(t, ActionGroupSetMembers, []string{"projects", "+demo-user", "+demo-other", "-demo-boss"}, "not a member")
}

func TestGroupSetMembersUndoRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		actor  int
		args   []string
		setup  func(*userFixture)
		reason string
	}{
		{"user left meanwhile", 1000, []string{"projects", "+demo-user"}, func(f *userFixture) { f.group("projects").members = nil }, "not a member of the group projects"},
		{"user joined again", 1000, []string{"media", "-demo-user"}, func(f *userFixture) { f.group("media").members = []string{"demo-user"} }, "in the group media already"},
		{"user replaced", 1000, []string{"projects", "+demo-user"}, func(f *userFixture) { f.user("demo-user").uid = 1999 }, "now has UID 1999 instead of 1001"},
		{"user gone", 1000, []string{"projects", "+demo-user"}, func(f *userFixture) {
			f.users = slices.DeleteFunc(f.users, func(u worldUser) bool { return u.name == "demo-user" })
		}, "no local user demo-user"},
		{"group replaced", 1000, []string{"projects", "+demo-user"}, func(f *userFixture) { f.group("projects").gid = 3000 }, "now has GID 3000 instead of 2000"},
		{"group gone", 1000, []string{"projects", "+demo-user"}, func(f *userFixture) {
			f.groups = slices.DeleteFunc(f.groups, func(g worldGroup) bool { return g.name == "projects" })
		}, "no group projects"},
		{"became a primary group", 1000, []string{"projects", "+demo-user"}, func(f *userFixture) { f.user("demo-user").gid = 2000 }, "primary group of demo-user"},
		{"undo takes the actor's own rights", 1001, []string{"sudo", "+demo-user"}, nil, "from yourself"},
		{"undo takes the last administrators", 1003, []string{"sudo", "+demo-user"}, func(f *userFixture) {
			f.group("sudo").members = []string{"demo-user"}
			f.group("qsysmin").members = []string{"demo-user"}
		}, "no administrator with access to qsysmin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUserFixture(t, tc.actor)
			res := f.apply(t, ActionGroupSetMembers, "", tc.args...)
			if tc.setup != nil {
				tc.setup(f)
			}
			f.flush()
			f.undoRefuses(t, res.Entry.ID, tc.reason)
		})
	}
}

func TestGroupSetMembersInverseRefusesInvalidRecordedStates(t *testing.T) {
	u := userActions{}
	const valid = `{"group":"projects","gid":2000,"changes":[{"name":"demo-user","uid":1001,"add":true,"qsysmin":false}]}`
	if _, err := u.inverseGroupSetMembers(json.RawMessage(valid)); err != nil {
		t.Fatalf("the valid state is refused: %v", err)
	}
	for what, state := range map[string]string{
		"group":                `{"group":"../x","gid":2000,"changes":[{"name":"demo-user","uid":1001,"add":true}]}`,
		"gid":                  `{"group":"projects","gid":-1,"changes":[{"name":"demo-user","uid":1001,"add":true}]}`,
		"no changes":           `{"group":"projects","gid":2000,"changes":[]}`,
		"user name":            `{"group":"projects","gid":2000,"changes":[{"name":"a,b","uid":1001,"add":true}]}`,
		"uid":                  `{"group":"projects","gid":2000,"changes":[{"name":"demo-user","uid":-1,"add":true}]}`,
		"twice":                `{"group":"projects","gid":2000,"changes":[{"name":"demo-user","uid":1001,"add":true},{"name":"demo-user","uid":1001,"add":false}]}`,
		"qsysmin outside sudo": `{"group":"media","gid":2001,"changes":[{"name":"demo-user","uid":1001,"add":true,"qsysmin":true}]}`,
		"unknown field":        `{"group":"projects","gid":2000,"changes":[{"name":"demo-user","uid":1001,"add":true}],"more":1}`,
		"unknown in item":      `{"group":"projects","gid":2000,"changes":[{"name":"demo-user","uid":1001,"add":true,"more":1}]}`,
		"not an object":        `[]`,
	} {
		if _, err := u.inverseGroupSetMembers(json.RawMessage(state)); err == nil {
			t.Errorf("%s: accepted", what)
		}
	}
}
