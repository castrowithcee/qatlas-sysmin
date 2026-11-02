package hostchange

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// The user actions. Plan checks only the syntax; Capture runs as root before anything changes and enforces
// the protection rules, so a refusal means nothing was changed.
const (
	ActionUserCreate        = "user.create"
	ActionUserCreateService = "user.create-service"
	ActionUserSetPassword   = "user.set-password"
	ActionUserSetAdmin      = "user.set-admin"
	ActionUserSetDocker     = "user.set-docker"
	ActionUserEnable        = "user.enable"
	ActionUserLock          = "user.lock"
	ActionUserUnlock        = "user.unlock"
	ActionUserDelete        = "user.delete"
	ActionUserAddGroups     = "user.add-groups"
	ActionUserRemoveGroups  = "user.remove-groups"
	ActionUserAddKey        = "user.add-key"
	ActionUserRemoveKey     = "user.remove-key"
)

const (
	loginShell          = "/bin/bash"
	noLoginShell        = "/usr/sbin/nologin"
	noHome              = "/nonexistent"
	passwordLabel       = "<password>"
	previousHashLabel   = "<previous password hash>"
	maxPasswordBytes    = 256
	maxCreateKeys       = 16
	maxFurtherGroups    = 32
	optionMustChange    = "must-change"
	optionWithoutSysmin = "without-sysmin"
	optionAdmin         = "admin"
	optionDocker        = "docker"
	optionHome          = "home"
	optionGroupPrefix   = "group="
	optionWithHome      = "with-home"
)

// fixedGroups have switches of their own; the further groups never include them.
var fixedGroups = map[string]bool{hostadmin.GroupAdmin: true, hostadmin.GroupDocker: true, hostadmin.GroupSysmin: true}

type userActions struct{ h UserHost }

// RegisterUserActions adds the user actions, reading the host through h.
func RegisterUserActions(r *Registry, h UserHost) error {
	u := userActions{h}
	for _, d := range []Definition{
		{ID: ActionUserCreate, WantsSecret: true, Plan: u.planCreate, Inverse: u.inverseCreate},
		{ID: ActionUserCreateService, Plan: u.planCreateService, Inverse: u.inverseCreateService},
		{ID: ActionUserSetPassword, WantsSecret: true, Plan: u.planPassword, Inverse: u.inversePassword},
		{ID: ActionUserSetAdmin, Plan: u.planAdmin, Inverse: u.inverseMembership},
		{ID: ActionUserSetDocker, Plan: u.planDocker, Inverse: u.inverseMembership},
		{ID: ActionUserEnable, Plan: u.planEnable, Inverse: u.inverseMembership},
		{ID: ActionUserLock, Plan: u.planLock, Inverse: u.inverseLock},
		{ID: ActionUserUnlock, Plan: u.planUnlock, Inverse: u.inverseLock},
		{ID: ActionUserDelete, Plan: u.planDelete, Inverse: u.inverseDelete},
		{ID: ActionUserAddGroups, Plan: u.planGroups(true), Inverse: u.inverseMembership},
		{ID: ActionUserRemoveGroups, Plan: u.planGroups(false), Inverse: u.inverseMembership},
		{ID: ActionUserAddKey, WantsSecret: true, Plan: u.planAddKey, Inverse: u.inverseAddKey},
		{ID: ActionUserRemoveKey, Plan: u.planRemoveKey, Inverse: u.inverseRemoveKey},
		{ID: ActionGroupCreate, Plan: u.planGroupCreate, Inverse: u.inverseGroupCreate},
		{ID: ActionGroupDelete, Plan: u.planGroupDelete, Inverse: u.inverseGroupDelete},
		{ID: ActionGroupRename, Plan: u.planGroupRename, Inverse: u.inverseGroupRename},
		{ID: ActionGroupSetMembers, Plan: u.planGroupSetMembers, Inverse: u.inverseGroupSetMembers},
	} {
		if err := r.Register(d); err != nil {
			return err
		}
	}
	return nil
}

// validUID accepts every UID a journal state may hold, root's 0 included; a negative one would read as anyUID.
func validUID(id int) bool { return id >= 0 }

func decodeState(prev json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(prev))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("the recorded state cannot be read: %w", err)
	}
	return nil
}

func checkPassword(p string) error {
	if p == "" || len(p) > maxPasswordBytes || !utf8.ValidString(p) {
		return Argf("the password must have 1 to %d bytes of UTF-8", maxPasswordBytes)
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return Argf("the password must not contain control characters")
		}
	}
	return nil
}

// newKey is one key a person enters: a single authorized_keys line without options.
type newKey struct{ line, fingerprint string }

func parseNewKey(text string) (newKey, error) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\r\n") {
		return newKey{}, Argf("an SSH key is exactly one line")
	}
	pub, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(text))
	if err != nil || len(rest) > 0 {
		return newKey{}, Argf("an SSH key must be a public key in authorized_keys format")
	}
	if len(options) > 0 {
		return newKey{}, Argf("SSH keys with options are not accepted")
	}
	for _, r := range comment {
		if r < 0x20 || r == 0x7f {
			return newKey{}, Argf("the comment of an SSH key must not contain control characters")
		}
	}
	line := strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(pub)), "\n")
	if comment != "" {
		line += " " + comment
	}
	return newKey{line: line, fingerprint: ssh.FingerprintSHA256(pub)}, nil
}

func addKeyCommand(name string, lines []string, fingerprint string) Command {
	return Command{
		Argv: []string{stepKeysAdd, name}, Builtin: true,
		Secret: []byte(strings.Join(lines, "\n") + "\n"), SecretLabel: "<ssh key " + fingerprint + ">",
	}
}

// --- create ---

// CreateInput is the standard input of user.create, one JSON object:
//
//	{"password": "<initial password>", "keys": ["<authorized_keys line>", ...]}
//
// The password is required; keys may be missing or empty. Unknown fields and trailing data are refused.
type CreateInput struct {
	Password string   `json:"password"`
	Keys     []string `json:"keys,omitempty"`
}

type createRequest struct {
	name                string
	admin, docker, home bool
	further             []string
}

// groups is the supplementary group list of the new user, in a fixed order.
func (c createRequest) groups() []string {
	var gs []string
	if c.admin {
		gs = append(gs, hostadmin.GroupAdmin, hostadmin.GroupSysmin)
	}
	if c.docker {
		gs = append(gs, hostadmin.GroupDocker)
	}
	return append(gs, c.further...)
}

// parseCreateArgs reads "<name> [admin] [docker] [group=<name>]..." for a person and
// "<name> [home] [docker] [group=<name>]..." for a service account, which is never an administrator.
func parseCreateArgs(args []string, service bool) (createRequest, error) {
	id, options := ActionUserCreate, "admin, docker, group=<name>"
	if service {
		id, options = ActionUserCreateService, "home, docker, group=<name>"
	}
	if len(args) == 0 {
		return createRequest{}, Argf("%s needs a user name", id)
	}
	req := createRequest{name: args[0]}
	if err := checkName("user", req.name); err != nil {
		return createRequest{}, err
	}
	if req.name == "root" {
		return createRequest{}, Argf("root cannot be created")
	}
	seen := map[string]bool{}
	for _, a := range args[1:] {
		if seen[a] {
			return createRequest{}, Argf("%q is given twice", a)
		}
		seen[a] = true
		switch g, isGroup := strings.CutPrefix(a, optionGroupPrefix); {
		case a == optionAdmin && !service:
			req.admin = true
		case a == optionHome && service:
			req.home = true
		case a == optionDocker:
			req.docker = true
		case isGroup:
			if err := checkFurtherGroup(g); err != nil {
				return createRequest{}, err
			}
			if g == req.name {
				return createRequest{}, Argf("the own group of %s is created with the user", g)
			}
			req.further = append(req.further, g)
		default:
			return createRequest{}, Argf("unknown option %q for %s (%s)", a, id, options)
		}
	}
	if len(req.further) > maxFurtherGroups {
		return createRequest{}, Argf("at most %d further groups", maxFurtherGroups)
	}
	slices.Sort(req.further)
	return req, nil
}

func checkFurtherGroup(g string) error {
	if err := checkName("group", g); err != nil {
		return err
	}
	if fixedGroups[g] {
		return Argf("%s is set with its own switch (Admin or Docker), not as a further group", g)
	}
	return nil
}

func parseCreateInput(secret []byte) (string, []newKey, error) {
	if len(secret) == 0 {
		return "", nil, Argf("user.create reads its password and keys as JSON from standard input")
	}
	dec := json.NewDecoder(bytes.NewReader(secret))
	dec.DisallowUnknownFields()
	var in CreateInput
	if err := dec.Decode(&in); err != nil {
		return "", nil, Argf("the standard input of user.create is not the expected JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", nil, Argf("the standard input of user.create has data after the JSON object")
	}
	if err := checkPassword(in.Password); err != nil {
		return "", nil, err
	}
	if len(in.Keys) > maxCreateKeys {
		return "", nil, Argf("at most %d SSH keys", maxCreateKeys)
	}
	var keys []newKey
	seen := map[string]bool{}
	for _, text := range in.Keys {
		k, err := parseNewKey(text)
		if err != nil {
			return "", nil, err
		}
		if seen[k.fingerprint] {
			return "", nil, Argf("the SSH key %s is given twice", k.fingerprint)
		}
		seen[k.fingerprint] = true
		keys = append(keys, k)
	}
	return in.Password, keys, nil
}

type createState struct {
	Name string `json:"name"`
	Home string `json:"home"`
}

func (u userActions) home(name string) string { return filepath.Join(u.h.HomeBase, name) }

func (u userActions) planCreate(args []string, secret []byte) (Plan, error) {
	req, err := parseCreateArgs(args, false)
	if err != nil {
		return Plan{}, err
	}
	pw, keys, err := parseCreateInput(secret)
	if err != nil {
		return Plan{}, err
	}
	home := u.home(req.name)
	useradd := []string{"useradd", "-m", "-d", home, "-U", "-s", loginShell}
	if gs := req.groups(); len(gs) > 0 {
		useradd = append(useradd, "-G", strings.Join(gs, ","))
	}
	cmds := []Command{
		{Argv: append(useradd, req.name)},
		{Argv: []string{"chpasswd"}, Secret: []byte(req.name + ":" + pw + "\n"), SecretLabel: passwordLabel},
		{Argv: []string{"chage", "-d", "0", req.name}},
	}
	var fps []string
	for _, k := range keys {
		cmds = append(cmds, addKeyCommand(req.name, []string{k.line}, k.fingerprint))
		fps = append(fps, k.fingerprint)
	}
	sentence := fmt.Sprintf("Create the user %s with the home %s and an initial password that must be changed at the first login", req.name, home)
	var extras []string
	if req.admin {
		extras = append(extras, "as an administrator with access to sysmin")
	}
	if req.docker {
		extras = append(extras, "with access to Docker")
	}
	if len(req.further) > 0 {
		extras = append(extras, "in "+groupWords(req.further))
	}
	if len(fps) > 0 {
		extras = append(extras, "with the SSH keys "+listWords(fps))
	}
	if len(extras) > 0 {
		sentence += ", " + strings.Join(extras, ", ")
	}
	return Plan{
		Sentence: sentence + ".",
		Commands: cmds,
		Redact:   [][]byte{[]byte(pw)},
		Capture: func(context.Context) (json.RawMessage, error) {
			if _, err := u.account(req.name); err == nil {
				return nil, Refusef("the user %s exists already", req.name)
			} else if !errors.Is(err, hostadmin.ErrNotFound) {
				return nil, err
			}
			groups, err := u.h.Accounts.Groups()
			if err != nil {
				return nil, err
			}
			if slices.ContainsFunc(groups, func(g hostadmin.Group) bool { return g.Name == req.name }) {
				return nil, Refusef("a group %s exists already; the user would not get a group of its own", req.name)
			}
			for _, path := range []string{home, filepath.Join(u.h.MailDir, req.name)} {
				if _, err := os.Lstat(path); err == nil {
					return nil, Refusef("%s exists already; undoing the creation would delete it", path)
				} else if !errors.Is(err, fs.ErrNotExist) {
					return nil, err
				}
			}
			for _, g := range req.groups() {
				if err := checkGroup(groups, g, !fixedGroups[g]); err != nil {
					return nil, err
				}
			}
			return json.Marshal(createState{Name: req.name, Home: home})
		},
		Check: func(context.Context) error {
			usr, err := u.h.Accounts.User(req.name)
			if err != nil {
				return err
			}
			if !hostadmin.Regular(usr.UID) || usr.Home != home {
				return fmt.Errorf("%s got UID %d and the home %s", req.name, usr.UID, usr.Home)
			}
			for _, g := range req.groups() {
				if !memberOf(usr, g) {
					return fmt.Errorf("%s is not in the group %s", req.name, g)
				}
			}
			if sh, err := u.shadow(req.name); err != nil || sh.Lastchg != "0" {
				return fmt.Errorf("the initial password of %s is not marked for a change", req.name)
			}
			lines, err := u.keyLines(req.name)
			if err != nil {
				return err
			}
			for _, fp := range fps {
				if !hasFingerprint(lines, fp) {
					return fmt.Errorf("the SSH key %s is missing", fp)
				}
			}
			return nil
		},
	}, nil
}

// checkGroup requires the group to exist. A new user gets only regular further groups; changing an existing
// user may use any group.
func checkGroup(groups []hostadmin.Group, name string, regularOnly bool) error {
	i := slices.IndexFunc(groups, func(g hostadmin.Group) bool { return g.Name == name })
	if i < 0 {
		return Refusef("there is no group %s", name)
	}
	if regularOnly && !hostadmin.Regular(groups[i].GID) {
		return Refusef("%s is a system group (GID %d) and cannot be given to a new user", name, groups[i].GID)
	}
	return nil
}

func (u userActions) inverseCreate(prev json.RawMessage) (Plan, error) {
	var st createState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := checkName("user", st.Name); err != nil || st.Name == "root" || st.Home != u.home(st.Name) {
		return Plan{}, errors.New("the recorded user cannot be undone")
	}
	return Plan{
		Sentence: fmt.Sprintf("Delete the user %s and its home %s, which is possible only while %s has never logged in.", st.Name, st.Home, st.Name),
		Commands: []Command{{Argv: []string{"userdel", "-r", st.Name}}},
		Capture: func(ctx context.Context) (json.RawMessage, error) {
			usr, err := u.target(st.Name, anyUID)
			if err != nil {
				return nil, err
			}
			// Deleting an account stays a matter of regular accounts; only a created one is ever undone this way.
			if !hostadmin.Regular(usr.UID) {
				return nil, Refusef("%s is a system user (UID %d) and is not deleted here", st.Name, usr.UID)
			}
			if usr.Home != st.Home {
				return nil, Refusef("%s now has the home %s instead of %s", st.Name, usr.Home, st.Home)
			}
			if actor := ActingUID(ctx); actor != 0 && actor == usr.UID {
				return nil, Refusef("you cannot delete your own account")
			}
			if err := u.guardRevoke(ctx, usr); err != nil {
				return nil, err
			}
			// A first login forces the change of the initial password before any session starts, both through
			// PAM and in sshd; a password that is no longer marked for a change means the account was used or
			// changed since, and it is kept.
			sh, err := u.shadow(st.Name)
			if err != nil {
				return nil, err
			}
			if sh.Lastchg != "0" {
				return nil, Refusef("the initial password of %s is no longer pending a change; the account is kept", st.Name)
			}
			logged, err := u.loginRecorded(usr.UID)
			if err != nil {
				return nil, Refusef("whether %s has logged in cannot be determined (%v); the account is kept", st.Name, err)
			}
			if logged {
				return nil, Refusef("%s has logged in; the account is kept", st.Name)
			}
			return json.Marshal(map[string]any{"name": st.Name, "uid": usr.UID})
		},
		Check: func(context.Context) error {
			if _, err := u.account(st.Name); !errors.Is(err, hostadmin.ErrNotFound) {
				return fmt.Errorf("%s still exists", st.Name)
			}
			return nil
		},
	}, nil
}

// --- password ---

type passwordState struct {
	Name    string `json:"name"`
	UID     int    `json:"uid"`
	Hash    string `json:"hash"`
	Lastchg string `json:"lastchg"`
}

// planPassword reads "<name> [must-change]"; the new password is the whole standard input.
func (u userActions) planPassword(args []string, secret []byte) (Plan, error) {
	if len(args) < 1 || len(args) > 2 {
		return Plan{}, Argf("user.set-password needs a user name and optionally %s", optionMustChange)
	}
	name := args[0]
	if err := checkName("user", name); err != nil {
		return Plan{}, err
	}
	mustChange := len(args) == 2
	if mustChange && args[1] != optionMustChange {
		return Plan{}, Argf("unknown option %q for user.set-password (%s)", args[1], optionMustChange)
	}
	if len(secret) == 0 {
		return Plan{}, Argf("user.set-password reads the new password from standard input")
	}
	if err := checkPassword(string(secret)); err != nil {
		return Plan{}, err
	}
	cmds := []Command{{Argv: []string{"chpasswd"}, Secret: append([]byte(name+":"), append(slices.Clone(secret), '\n')...), SecretLabel: passwordLabel}}
	sentence := "Set a new password for " + name + "."
	if mustChange {
		cmds = append(cmds, Command{Argv: []string{"chage", "-d", "0", name}})
		sentence = "Set a new password for " + name + " that must be changed at the next login."
	}
	var before shadowFields
	return Plan{
		Sentence: sentence,
		Commands: cmds,
		Redact:   [][]byte{secret},
		Capture: func(context.Context) (json.RawMessage, error) {
			usr, err := u.target(name, anyUID)
			if err != nil {
				return nil, err
			}
			if before, err = u.shadow(name); err != nil {
				return nil, err
			}
			return json.Marshal(passwordState{Name: name, UID: usr.UID, Hash: before.Hash, Lastchg: before.Lastchg})
		},
		Check: func(context.Context) error {
			now, err := u.shadow(name)
			if err != nil {
				return err
			}
			if now.Hash == before.Hash {
				return fmt.Errorf("the password of %s did not change", name)
			}
			if mustChange && now.Lastchg != "0" {
				return fmt.Errorf("the password of %s is not marked for a change", name)
			}
			return nil
		},
	}, nil
}

func validHash(h string) bool {
	if h == "" || strings.ContainsRune(h, ':') {
		return false
	}
	for _, r := range h {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func validDay(d string) bool {
	if d == "" {
		return true
	}
	if len(d) > 9 {
		return false
	}
	for _, r := range d {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// inversePassword restores the previous shadow entry: the hash through chpasswd -e on standard input and the
// day of the last change through chage, because chpasswd sets it to today.
func (u userActions) inversePassword(prev json.RawMessage) (Plan, error) {
	var st passwordState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := checkName("user", st.Name); err != nil || !validUID(st.UID) || !validDay(st.Lastchg) {
		return Plan{}, errors.New("the recorded password cannot be restored")
	}
	if !validHash(st.Hash) {
		return Plan{}, errors.New("the previous password was empty or unusual and is not restored")
	}
	day := st.Lastchg
	if day == "" {
		day = "-1"
	}
	return Plan{
		Sentence: "Restore the previous password of " + st.Name + ".",
		Commands: []Command{
			{Argv: []string{"chpasswd", "-e"}, Secret: []byte(st.Name + ":" + st.Hash + "\n"), SecretLabel: previousHashLabel},
			{Argv: []string{"chage", "-d", day, st.Name}},
		},
		Redact: [][]byte{[]byte(st.Hash)},
		Capture: func(context.Context) (json.RawMessage, error) {
			if _, err := u.target(st.Name, st.UID); err != nil {
				return nil, err
			}
			return json.Marshal(map[string]any{"name": st.Name, "uid": st.UID})
		},
		Check: func(context.Context) error {
			now, err := u.shadow(st.Name)
			if err != nil {
				return err
			}
			if now.Hash != st.Hash || now.Lastchg != st.Lastchg {
				return fmt.Errorf("the previous password of %s is not back", st.Name)
			}
			return nil
		},
	}, nil
}

// --- groups, admin, docker, enable ---

type membershipState struct {
	Name   string          `json:"name"`
	UID    int             `json:"uid"`
	Add    bool            `json:"add"`
	Groups []string        `json:"groups"`
	Before map[string]bool `json:"before"`
}

type membershipChange struct {
	name     string
	uid      int  // the UID the user must still have, where pinned
	pinned   bool // the user must still have uid; a forward change takes whoever has the name
	groups   []string
	add      bool
	enable   bool // only for an administrator
	sentence string
}

// membershipPlan adds supplementary groups with one usermod -aG, which adds all of them or none and leaves a
// group the user is in already as it is, and removes them with one gpasswd -d per group. gpasswd -d fails for
// a group the user is not in, so a removal is refused unless the user is in every one of them; a restore
// after a partial removal adds the groups back with usermod -aG, which tolerates the ones still there.
func (u userActions) membershipPlan(m membershipChange) Plan {
	var cmds []Command
	if m.add {
		cmds = []Command{{Argv: []string{"usermod", "-aG", strings.Join(m.groups, ","), m.name}}}
	} else {
		for _, g := range m.groups {
			cmds = append(cmds, Command{Argv: []string{"gpasswd", "-d", m.name, g}})
		}
	}
	return Plan{
		Sentence: m.sentence,
		Commands: cmds,
		Capture: func(ctx context.Context) (json.RawMessage, error) {
			want := anyUID
			if m.pinned {
				want = m.uid
			}
			usr, err := u.target(m.name, want)
			if err != nil {
				return nil, err
			}
			groups, err := u.h.Accounts.Groups()
			if err != nil {
				return nil, err
			}
			if m.enable && !usr.Admin {
				return nil, Refusef("%s is no administrator", m.name)
			}
			before := map[string]bool{}
			changes := false
			for _, g := range m.groups {
				if err := checkGroup(groups, g, false); err != nil {
					return nil, err
				}
				if !m.add && usr.PrimaryGroup == g {
					return nil, Refusef("%s is the primary group of %s and cannot be removed", g, m.name)
				}
				if !m.add && !slices.Contains(usr.Groups, g) {
					return nil, Refusef("%s is not in the group %s", m.name, g)
				}
				before[g] = memberOf(usr, g)
				changes = changes || before[g] != m.add
			}
			if !changes {
				return nil, Refusef("nothing to change: %s is already as asked", m.name)
			}
			if !m.add && (slices.Contains(m.groups, hostadmin.GroupAdmin) || slices.Contains(m.groups, hostadmin.GroupSysmin)) {
				if err := u.guardRevoke(ctx, usr); err != nil {
					return nil, err
				}
			}
			return json.Marshal(membershipState{Name: m.name, UID: usr.UID, Add: m.add, Groups: m.groups, Before: before})
		},
		Check: func(context.Context) error {
			usr, err := u.h.Accounts.User(m.name)
			if err != nil {
				return err
			}
			for _, g := range m.groups {
				if memberOf(usr, g) != m.add {
					return fmt.Errorf("the membership of %s in %s did not change", m.name, g)
				}
			}
			return nil
		},
	}
}

func onOff(args []string, action string) (string, bool, error) {
	if len(args) != 2 || (args[1] != "on" && args[1] != "off") {
		return "", false, Argf("%s needs a user name and on or off", action)
	}
	if err := checkName("user", args[0]); err != nil {
		return "", false, err
	}
	return args[0], args[1] == "on", nil
}

// planAdmin reads "<name> on", "<name> off" or, for an administrator who is not in sysmin,
// "<name> off without-sysmin", because the commands of a plan cannot depend on the host.
func (u userActions) planAdmin(args []string, _ []byte) (Plan, error) {
	withoutSysmin := len(args) == 3 && args[1] == "off" && args[2] == optionWithoutSysmin
	if withoutSysmin {
		args = args[:2]
	}
	name, on, err := onOff(args, ActionUserSetAdmin)
	if err != nil {
		return Plan{}, err
	}
	m := membershipChange{name: name, groups: []string{hostadmin.GroupAdmin, hostadmin.GroupSysmin}, add: on}
	switch {
	case on:
		m.sentence = "Make " + name + " an administrator with access to sysmin (groups sudo and sysmin)."
	case withoutSysmin:
		m.groups = []string{hostadmin.GroupAdmin}
		m.sentence = "Take administrator rights from " + name + " (group sudo)."
	default:
		m.sentence = "Take administrator rights and access to sysmin from " + name + " (groups sudo and sysmin)."
	}
	return u.membershipPlan(m), nil
}

func (u userActions) planDocker(args []string, _ []byte) (Plan, error) {
	name, on, err := onOff(args, ActionUserSetDocker)
	if err != nil {
		return Plan{}, err
	}
	m := membershipChange{name: name, groups: []string{hostadmin.GroupDocker}, add: on}
	m.sentence = "Give " + name + " access to Docker (group docker)."
	if !on {
		m.sentence = "Take access to Docker from " + name + " (group docker)."
	}
	return u.membershipPlan(m), nil
}

func (u userActions) planEnable(args []string, _ []byte) (Plan, error) {
	if len(args) != 1 {
		return Plan{}, Argf("user.enable needs a user name")
	}
	if err := checkName("user", args[0]); err != nil {
		return Plan{}, err
	}
	return u.membershipPlan(membershipChange{
		name: args[0], groups: []string{hostadmin.GroupSysmin}, add: true, enable: true,
		sentence: "Give the administrator " + args[0] + " access to sysmin (group sysmin).",
	}), nil
}

// planGroups reads "<name> <group>..." for the further groups.
func (u userActions) planGroups(add bool) func([]string, []byte) (Plan, error) {
	id := ActionUserRemoveGroups
	if add {
		id = ActionUserAddGroups
	}
	return func(args []string, _ []byte) (Plan, error) {
		if len(args) < 2 || len(args) > maxFurtherGroups+1 {
			return Plan{}, Argf("%s needs a user name and 1 to %d groups", id, maxFurtherGroups)
		}
		if err := checkName("user", args[0]); err != nil {
			return Plan{}, err
		}
		groups := slices.Clone(args[1:])
		for _, g := range groups {
			if err := checkFurtherGroup(g); err != nil {
				return Plan{}, err
			}
		}
		slices.Sort(groups)
		if len(slices.Compact(slices.Clone(groups))) != len(groups) {
			return Plan{}, Argf("a group is given twice")
		}
		m := membershipChange{name: args[0], groups: groups, add: add}
		m.sentence = "Add " + args[0] + " to " + groupWords(groups) + "."
		if !add {
			m.sentence = "Remove " + args[0] + " from " + groupWords(groups) + "."
		}
		return u.membershipPlan(m), nil
	}
}

// inverseMembership takes back exactly the groups the change altered, guarded like a forward change.
func (u userActions) inverseMembership(prev json.RawMessage) (Plan, error) {
	var st membershipState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := checkName("user", st.Name); err != nil || !validUID(st.UID) {
		return Plan{}, errors.New("the recorded user cannot be undone")
	}
	var back []string
	for _, g := range st.Groups {
		if err := checkName("group", g); err != nil {
			return Plan{}, err
		}
		if st.Before[g] != st.Add {
			back = append(back, g)
		}
	}
	if len(back) == 0 {
		return Plan{}, errors.New("the change altered no group")
	}
	m := membershipChange{name: st.Name, uid: st.UID, pinned: true, groups: back, add: !st.Add}
	m.sentence = "Add " + st.Name + " to " + groupWords(back) + " again."
	if st.Add {
		m.sentence = "Remove " + st.Name + " from " + groupWords(back) + " again."
	}
	return u.membershipPlan(m), nil
}

// --- SSH keys ---

type keyState struct {
	Name        string   `json:"name"`
	UID         int      `json:"uid"`
	Fingerprint string   `json:"fingerprint"`
	Lines       []string `json:"lines,omitempty"`
}

// planAddKey reads "<name>"; the key is the whole standard input, one authorized_keys line.
func (u userActions) planAddKey(args []string, secret []byte) (Plan, error) {
	if len(args) != 1 {
		return Plan{}, Argf("user.add-key needs a user name")
	}
	name := args[0]
	if err := checkName("user", name); err != nil {
		return Plan{}, err
	}
	if len(secret) == 0 {
		return Plan{}, Argf("user.add-key reads the SSH key from standard input")
	}
	k, err := parseNewKey(string(secret))
	if err != nil {
		return Plan{}, err
	}
	return u.addKeyPlan(name, anyUID, []string{k.line}, k.fingerprint,
		"Add the SSH key "+k.fingerprint+" to "+name+"."), nil
}

func (u userActions) addKeyPlan(name string, uid int, lines []string, fp, sentence string) Plan {
	return Plan{
		Sentence: sentence,
		Commands: []Command{addKeyCommand(name, lines, fp)},
		Capture: func(context.Context) (json.RawMessage, error) {
			usr, err := u.target(name, uid)
			if err != nil {
				return nil, err
			}
			have, err := u.keyLines(name)
			if err != nil {
				return nil, err
			}
			if hasFingerprint(have, fp) {
				return nil, Refusef("%s has the SSH key %s already", name, fp)
			}
			return json.Marshal(keyState{Name: name, UID: usr.UID, Fingerprint: fp})
		},
		Check: func(context.Context) error { return u.checkKey(name, fp, true) },
	}
}

func (u userActions) checkKey(name, fp string, present bool) error {
	lines, err := u.keyLines(name)
	if err != nil {
		return err
	}
	if hasFingerprint(lines, fp) != present {
		return fmt.Errorf("the SSH key %s of %s did not change", fp, name)
	}
	return nil
}

func (u userActions) inverseAddKey(prev json.RawMessage) (Plan, error) {
	var st keyState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := checkName("user", st.Name); err != nil || !validUID(st.UID) || !keyFingerprint.MatchString(st.Fingerprint) {
		return Plan{}, errors.New("the recorded key cannot be undone")
	}
	return u.removeKeyPlan(st.Name, st.UID, st.Fingerprint, "Remove the SSH key "+st.Fingerprint+" from "+st.Name+" again."), nil
}

// planRemoveKey reads "<name> <fingerprint>", the fingerprint as SHA256:… like ssh-keygen -l shows it.
func (u userActions) planRemoveKey(args []string, _ []byte) (Plan, error) {
	if len(args) != 2 {
		return Plan{}, Argf("user.remove-key needs a user name and a key fingerprint")
	}
	if err := checkName("user", args[0]); err != nil {
		return Plan{}, err
	}
	if !keyFingerprint.MatchString(args[1]) {
		return Plan{}, Argf("%q is not a SHA256 key fingerprint", args[1])
	}
	return u.removeKeyPlan(args[0], anyUID, args[1], "Remove the SSH key "+args[1]+" from "+args[0]+"."), nil
}

func (u userActions) removeKeyPlan(name string, uid int, fp, sentence string) Plan {
	return Plan{
		Sentence: sentence,
		Commands: []Command{{Argv: []string{stepKeysRemove, name, fp}, Builtin: true}},
		Capture: func(context.Context) (json.RawMessage, error) {
			usr, err := u.target(name, uid)
			if err != nil {
				return nil, err
			}
			have, err := u.keyLines(name)
			if err != nil {
				return nil, err
			}
			st := keyState{Name: name, UID: usr.UID, Fingerprint: fp}
			for _, k := range have {
				if k.fingerprint == fp {
					st.Lines = append(st.Lines, k.raw)
				}
			}
			if len(st.Lines) == 0 {
				return nil, Refusef("%s has no SSH key %s", name, fp)
			}
			return json.Marshal(st)
		},
		Check: func(context.Context) error { return u.checkKey(name, fp, false) },
	}
}

// inverseRemoveKey writes the removed lines back as they were, options and comment included.
func (u userActions) inverseRemoveKey(prev json.RawMessage) (Plan, error) {
	var st keyState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := checkName("user", st.Name); err != nil || !validUID(st.UID) || !keyFingerprint.MatchString(st.Fingerprint) || len(st.Lines) == 0 {
		return Plan{}, errors.New("the recorded key cannot be restored")
	}
	for _, line := range st.Lines {
		if ks := parseKeyLines([]byte(line)); strings.ContainsAny(line, "\r\n") || len(ks) != 1 || ks[0].fingerprint != st.Fingerprint {
			return Plan{}, errors.New("the recorded key cannot be restored")
		}
	}
	return u.addKeyPlan(st.Name, st.UID, st.Lines, st.Fingerprint, "Add the SSH key "+st.Fingerprint+" to "+st.Name+" again."), nil
}
