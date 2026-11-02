package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// Users are created and changed from Host > Users with n and e. Each form collects one change, checks what it
// can before anything leaves the form, and ends in the confirmation of hostchange; the root command checks it
// all again and stays the authority.

// hostUID is the user running the interface, for the early message about taking admin from oneself.
var hostUID = os.Getuid

// Limits the root actions enforce too; checking them here only saves a round through sudo.
const (
	maxFormPassword = 256
	maxFormKeys     = 16
)

// userKindText is the type of a user in the details: system accounts can be changed like others.
func userKindText(system bool) string {
	if system {
		return tr(tHostSystem)
	}
	return tr(tHostRegular)
}

// fixedGroup is a group with a switch of its own, never one of the further groups.
func fixedGroup(name string) bool {
	return name == hostadmin.GroupAdmin || name == hostadmin.GroupDocker || name == hostadmin.GroupSysmin
}

// furtherGroups are the groups a person may be put into besides the switches: existing and not fixed. A new
// user gets only regular groups; an existing one may also get system groups. A source that cannot be read
// offers none.
func (m *Model) furtherGroups(withSystem bool) []string {
	if m.hostSrc == nil {
		return nil
	}
	list, err := m.hostSrc.Groups()
	if err != nil {
		return nil
	}
	var names []string
	for _, g := range list {
		if (withSystem || !g.System) && !fixedGroup(g.Name) {
			names = append(names, g.Name)
		}
	}
	slices.Sort(names)
	return names
}

func (m *Model) isSystemGroup(name string) bool {
	for _, g := range m.hst.groups.all {
		if g.Name == name {
			return g.System
		}
	}
	if m.hostSrc != nil {
		if list, err := m.hostSrc.Groups(); err == nil {
			for _, g := range list {
				if g.Name == name {
					return g.System
				}
			}
		}
	}
	return true
}

// userKey is one SSH key as a person entered it, with the fingerprint shown for it.
type userKey struct{ line, fingerprint string }

// parseUserKey checks one authorized_keys line the way the root action does: one line, a public key, no options.
func parseUserKey(text string) (userKey, string) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\r\n") {
		return userKey{}, tr(tHostFormKeyBad)
	}
	pub, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(text))
	if err != nil || len(rest) > 0 || len(options) > 0 {
		return userKey{}, tr(tHostFormKeyBad)
	}
	return userKey{line: text, fingerprint: ssh.FingerprintSHA256(pub)}, ""
}

// passwordProblem checks the two entries of a password field pair.
func passwordProblem(f *formDlg, first, second string) bool {
	pw, again := f.row(first), f.row(second)
	switch {
	case pw.raw() == "":
		pw.problem = tr(tHostFormPwEmpty)
	case utf8.RuneCountInString(pw.raw()) > maxFormPassword || !validPasswordText(pw.raw()):
		pw.problem = tr(tHostFormPwBad, maxFormPassword)
	case pw.raw() != again.raw():
		again.problem = tr(tHostFormPwMismatch)
	default:
		return false
	}
	return true
}

func validPasswordText(p string) bool {
	if len(p) > maxFormPassword || !utf8.ValidString(p) {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// wipe forgets what a masked pair held.
func wipe(f *formDlg, keys ...string) {
	for _, k := range keys {
		if r := f.row(k); r != nil {
			r.in.SetValue("")
		}
	}
}

// keyList is the form's state for the SSH keys added so far.
type keyList struct{ keys []userKey }

// add checks the text of the key row and puts its key on the list; a problem stands under the row.
func (k *keyList) add(row *formRow) bool {
	key, problem := parseUserKey(row.value())
	switch {
	case problem != "":
		row.problem = problem
	case slices.ContainsFunc(k.keys, func(o userKey) bool { return o.fingerprint == key.fingerprint }):
		row.problem = tr(tHostFormKeyDup)
	case len(k.keys) >= maxFormKeys:
		row.problem = tr(tHostFormKeyMax, maxFormKeys)
	default:
		k.keys = append(k.keys, key)
		row.in.SetValue("")
		return true
	}
	return false
}

// summary lists the fingerprints of the keys added, the latest few.
func (k *keyList) summary() string {
	if len(k.keys) == 0 {
		return ""
	}
	shown := k.keys
	more := ""
	if len(shown) > 2 {
		more = fmt.Sprintf(" +%d", len(shown)-2)
		shown = shown[len(shown)-2:]
	}
	var fps []string
	for _, key := range shown {
		fps = append(fps, key.fingerprint)
	}
	return tr(tHostFormKeys, len(k.keys), strings.Join(fps, ", ")+more)
}

// --- create ---

type createForm struct{ keyList }

// openUserCreate opens the form for a new user.
func (m *Model) openUserCreate() tea.Cmd {
	st := &createForm{}
	f := &formDlg{title: tr(tHostNewTitle), hint: tr(tHostNewHint), saveText: "{save} save"}
	yesNo := []string{hostNo, hostYes}
	shown := []string{tr(tHostNo), tr(tHostYes)}
	f.rows = []formRow{
		textRow("name", tr(tColName), ""),
		maskedRow("password", tr(tHostFormPassword)),
		maskedRow("again", tr(tHostFormPassword2)),
		choiceRow("admin", tr(tHostColAdmin), yesNo, shown, 0),
		choiceRow("docker", tr(tHostColDocker), yesNo, shown, 0),
		multiRow("groups", tr(tHostFormFurther), m.furtherGroups(false)),
		textRow("key", tr(tHostFormKey), ""),
	}
	f.enter = func(_ *Model, f *formDlg, row int) bool {
		if f.rows[row].key != "key" || f.rows[row].value() == "" {
			return false
		}
		if st.add(&f.rows[row]) {
			f.hint = tr(tHostNewHint) + " " + st.summary()
		}
		return true
	}
	f.submit = st.submit
	m.openForm(f)
	return nil
}

func (st *createForm) submit(m *Model, f *formDlg) tea.Cmd {
	name := f.row("name").value()
	switch {
	case name == "":
		f.row("name").problem = tr(tHostFormNameEmpty)
	case m.hostUserExists(name):
		f.row("name").problem = tr(tHostFormNameTaken)
	}
	bad := f.row("name").problem != ""
	if passwordProblem(f, "password", "again") {
		bad = true
	}
	if f.row("key").value() != "" && !st.add(f.row("key")) {
		bad = true
	}
	if bad {
		return nil
	}
	args := []string{name}
	admin, docker := f.row("admin").value() == hostYes, f.row("docker").value() == hostYes
	if admin {
		args = append(args, "admin")
	}
	if docker {
		args = append(args, "docker")
	}
	for _, g := range f.row("groups").picked() {
		args = append(args, "group="+g)
	}
	input := hostchange.CreateInput{Password: f.row("password").raw()}
	for _, k := range st.keys {
		input.Keys = append(input.Keys, k.line)
	}
	secret, err := json.Marshal(input)
	pw := []byte(input.Password)
	if err != nil {
		zero(pw)
		f.notice = err.Error()
		return nil
	}
	p, refusal := m.planHost(hostAsk{id: hostchange.ActionUserCreate, args: args, secret: secret, mask: [][]byte{pw}, power: admin || docker})
	if refusal != "" {
		zero(secret)
		zero(pw)
		f.notice = refusal
		return nil
	}
	wipe(f, "password", "again")
	m.closeForm()
	m.confirmHost(p)
	return nil
}

func (m *Model) hostUserExists(name string) bool {
	for _, u := range m.hst.users.all {
		if u.Name == name {
			return true
		}
	}
	return false
}

// --- change ---

// The changes the form offers, by the id of the choice.
const (
	chgAddGroups    = "add-groups"
	chgRemoveGroups = "remove-groups"
	chgAdminOn      = "admin-on"
	chgAdminOff     = "admin-off"
	chgDockerOn     = "docker-on"
	chgDockerOff    = "docker-off"
	chgPassword     = "password"
	chgAddKey       = "add-key"
	chgRemoveKey    = "remove-key"
	chgEnable       = "enable"
)

func changeText(id string) string {
	return tr(map[string]textID{
		chgAddGroups: tHostChgAddGroups, chgRemoveGroups: tHostChgRemoveGroups, chgAdminOn: tHostChgAdminOn,
		chgAdminOff: tHostChgAdminOff, chgDockerOn: tHostChgDockerOn, chgDockerOff: tHostChgDockerOff,
		chgPassword: tHostChgPassword, chgAddKey: tHostChgAddKey, chgRemoveKey: tHostChgRemoveKey, chgEnable: tHostChgEnable,
	}[id])
}

// editFor are the groups a user may be added to and taken out of.
func (m *Model) editGroups(u hostadmin.User) (add, remove []string) {
	for _, g := range m.furtherGroups(true) {
		if g != u.PrimaryGroup && !slices.Contains(u.Groups, g) {
			add = append(add, g)
		}
	}
	for _, g := range u.Groups {
		if g != u.PrimaryGroup && !fixedGroup(g) {
			remove = append(remove, g)
		}
	}
	slices.Sort(remove)
	return add, remove
}

// openUserEdit opens the form that changes the marked user.
func (m *Model) openUserEdit() tea.Cmd {
	name, ok := m.hst.users.page.current()
	if !ok {
		return nil
	}
	u, ok := m.hst.users.items[name]
	if !ok {
		return nil
	}
	add, remove := m.editGroups(u)
	var ids []string
	if len(add) > 0 {
		ids = append(ids, chgAddGroups)
	}
	if len(remove) > 0 {
		ids = append(ids, chgRemoveGroups)
	}
	if u.Admin {
		ids = append(ids, chgAdminOff)
	} else {
		ids = append(ids, chgAdminOn)
	}
	if u.Docker {
		ids = append(ids, chgDockerOff)
	} else {
		ids = append(ids, chgDockerOn)
	}
	ids = append(ids, chgPassword, chgAddKey)
	if u.SSHKeys == nil || len(u.SSHKeys) > 0 {
		ids = append(ids, chgRemoveKey)
	}
	if u.Admin && !u.Enabled {
		ids = append([]string{chgEnable}, ids...)
	}
	shown := make([]string, len(ids))
	for i, id := range ids {
		shown[i] = changeText(id)
	}
	var fps, fpShown []string
	for _, k := range u.SSHKeys {
		fps = append(fps, k.Fingerprint)
		fpShown = append(fpShown, k.Fingerprint+" "+k.Comment)
	}
	if len(fps) == 0 {
		fps, fpShown = []string{""}, []string{""}
	}
	st := &editForm{u: u, add: add, remove: remove}
	f := &formDlg{title: tr(tHostEditTitle, m.diffText(u.Name)), saveText: "{save} save", submit: st.submit}
	f.rows = []formRow{
		choiceRow("what", tr(tHostFormWhat), ids, shown, 0),
		multiRow("add", tr(tHostFormAddTo), add),
		multiRow("remove", tr(tHostFormRemoveFrom), remove),
		maskedRow("password", tr(tHostFormPassword)),
		maskedRow("again", tr(tHostFormPassword2)),
		choiceRow("must", tr(tHostFormMust), []string{hostYes, hostNo}, []string{tr(tHostYes), tr(tHostNo)}, 0),
		textRow("key", tr(tHostFormKey), ""),
		choiceRow("fp", tr(tHostFormKeyOf), fps, fpShown, 0),
	}
	f.changed = func(_ *Model, f *formDlg, row int) {
		if f.rows[row].key == "what" {
			st.arrange(f)
		}
	}
	st.arrange(f)
	m.openForm(f)
	return nil
}

type editForm struct {
	u           hostadmin.User
	add, remove []string
}

// arrange shows the rows of the change chosen and the hint that goes with it.
func (st *editForm) arrange(f *formDlg) {
	what := f.row("what").value()
	show := map[string]bool{
		"add": what == chgAddGroups, "remove": what == chgRemoveGroups,
		"password": what == chgPassword, "again": what == chgPassword, "must": what == chgPassword,
		"key": what == chgAddKey, "fp": what == chgRemoveKey && len(st.u.SSHKeys) > 0,
	}
	for i := range f.rows {
		if k := f.rows[i].key; k != "what" {
			f.rows[i].hidden = !show[k]
		}
	}
	f.hint = ""
	switch what {
	case chgAdminOn, chgDockerOn:
		f.hint = tr(tHostPowerWarn)
	case chgEnable:
		f.hint = tr(tHostChgEnableHint)
	case chgAddKey:
		f.hint = tr(tHostChgKeyHint)
	case chgRemoveKey:
		if st.u.SSHKeys == nil {
			f.hint = tr(tHostFormKeysUnread)
		}
	}
}

// guardRevoke gives the early message of the rules the root action enforces for taking admin away.
func (m *Model) guardRevoke(u hostadmin.User) string {
	if !u.Admin {
		return ""
	}
	if uid := hostUID(); uid != 0 && uid == u.UID {
		return tr(tHostSelfAdmin)
	}
	for _, o := range m.hst.users.all {
		if o.Name != u.Name && o.Admin && o.Enabled {
			return ""
		}
	}
	return tr(tHostLastAdmin, m.diffText(u.Name))
}

func (st *editForm) submit(m *Model, f *formDlg) tea.Cmd {
	name := st.u.Name
	ask := hostAsk{args: []string{name}}
	var chosen []string // the further groups of the change; the groups of the Admin and Docker switches do not count
	switch f.row("what").value() {
	case chgAddGroups, chgRemoveGroups:
		key, id := "add", hostchange.ActionUserAddGroups
		if f.row("what").value() == chgRemoveGroups {
			key, id = "remove", hostchange.ActionUserRemoveGroups
		}
		picked := f.row(key).picked()
		if len(picked) == 0 {
			f.row(key).problem = tr(tHostFormTickOne)
			return nil
		}
		ask.id, ask.args, chosen = id, append(ask.args, picked...), picked
	case chgAdminOn, chgDockerOn:
		ask.id, ask.args, ask.power = hostchange.ActionUserSetAdmin, append(ask.args, "on"), true
		if f.row("what").value() == chgDockerOn {
			ask.id = hostchange.ActionUserSetDocker
		}
	case chgAdminOff:
		if msg := m.guardRevoke(st.u); msg != "" {
			f.notice = msg
			return nil
		}
		ask.id, ask.args = hostchange.ActionUserSetAdmin, append(ask.args, "off")
		if !st.u.Enabled {
			ask.args = append(ask.args, "without-sysmin")
		}
	case chgDockerOff:
		ask.id, ask.args = hostchange.ActionUserSetDocker, append(ask.args, "off")
	case chgEnable:
		ask.id = hostchange.ActionUserEnable
	case chgPassword:
		if passwordProblem(f, "password", "again") {
			return nil
		}
		ask.id, ask.secret = hostchange.ActionUserSetPassword, []byte(f.row("password").raw())
		if f.row("must").value() == hostYes {
			ask.args = append(ask.args, "must-change")
		}
	case chgAddKey:
		key, problem := parseUserKey(f.row("key").value())
		if problem != "" {
			f.row("key").problem = problem
			return nil
		}
		ask.id, ask.secret = hostchange.ActionUserAddKey, []byte(key.line)
	case chgRemoveKey:
		if st.u.SSHKeys == nil {
			m.closeForm()
			return m.askReadUser()
		}
		ask.id, ask.args = hostchange.ActionUserRemoveKey, append(ask.args, f.row("fp").value())
	}
	ask.risk = m.riskOf(st.u, chosen)
	p, refusal := m.planHost(ask)
	if refusal != "" {
		zero(ask.secret)
		f.notice = refusal
		return nil
	}
	wipe(f, "password", "again")
	m.closeForm()
	m.confirmHost(p)
	return nil
}

// hostUsersKey handles n and e on the Users page.
func (m *Model) hostUsersKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case keyIs(key, actNew):
		return m.openUserCreate(), true
	case keyIs(key, actEdit):
		return m.openUserEdit(), true
	}
	return nil, false
}

// canEditCurrent reports whether a user is marked that e can change.
func (m *Model) canEditCurrent() bool {
	name, ok := m.hst.users.page.current()
	_, known := m.hst.users.items[name]
	return ok && known
}

// riskOf names why a change on the user and the groups it involves needs a red confirmation: root, a system
// account or a system group. It is empty for a change that touches only regular accounts and groups.
func (m *Model) riskOf(u hostadmin.User, groups []string) string {
	var parts []string
	if u.UID == 0 || u.Name == "root" {
		parts = append(parts, tr(tHostRiskRoot))
	}
	if u.System {
		parts = append(parts, tr(tHostRiskAccount))
	}
	for _, g := range groups {
		if m.isSystemGroup(g) {
			parts = append(parts, tr(tHostRiskGroup))
			break
		}
	}
	return strings.Join(parts, " ")
}
