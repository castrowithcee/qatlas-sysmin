package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/crypto/ssh"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// Users are created, changed and deleted from Host > Users with n, e and d. Each form collects one change, checks what it
// can before anything leaves the form, and ends in the confirmation of hostchange; the root command checks it
// all again and stays the authority.

// Limits the root actions enforce too; checking them here only saves a round through sudo.
const (
	maxFormPassword = 256
	maxFormKeys     = 16
)

// userKindText is the type of a user in the details: system accounts can be changed like others.
func userKindText(ui *uiContext, system bool) string {
	if system {
		return ui.tr(tHostSystem)
	}
	return ui.tr(tHostRegular)
}

// fixedGroup is a group with a switch of its own, never one of the further groups.
func fixedGroup(name string) bool {
	return name == hostadmin.GroupAdmin || name == hostadmin.GroupDocker || name == hostadmin.GroupQsysmin
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

// systemNotes marks the system groups among names for a pick row.
func (m *Model) systemNotes(names []string) []string {
	notes := make([]string, len(names))
	for i, g := range names {
		if m.isSystemGroup(g) {
			notes[i] = m.tr(tHostSystem)
		}
	}
	return notes
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
func parseUserKey(ui *uiContext, text string) (userKey, string) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\r\n") {
		return userKey{}, ui.tr(tHostFormKeyBad)
	}
	pub, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(text))
	if err != nil || len(rest) > 0 || len(options) > 0 {
		return userKey{}, ui.tr(tHostFormKeyBad)
	}
	return userKey{line: text, fingerprint: ssh.FingerprintSHA256(pub)}, ""
}

// The fields of the user forms.
const (
	fieldKind     kittui.FieldID = "kind"
	fieldName     kittui.FieldID = "name"
	fieldPassword kittui.FieldID = "password"
	fieldAgain    kittui.FieldID = "again"
	fieldAdmin    kittui.FieldID = "admin"
	fieldHome     kittui.FieldID = "home"
	fieldDocker   kittui.FieldID = "docker"
	fieldGroups   kittui.FieldID = "groups"
	fieldWhat     kittui.FieldID = "what"
	fieldAdd      kittui.FieldID = "add"
	fieldRemove   kittui.FieldID = "remove"
	fieldMust     kittui.FieldID = "must"
	fieldKey      kittui.FieldID = "key"
	fieldKeyOf    kittui.FieldID = "fp"
)

// passwordProblem checks the two entries of a password field pair and reports whether it found a problem.
func passwordProblem(ui *uiContext, f *formDlg) bool {
	pw, again := f.raw(fieldPassword), f.raw(fieldAgain)
	switch {
	case pw == "":
		f.fail(fieldPassword, ui.tr(tHostFormPwEmpty))
	case utf8.RuneCountInString(pw) > maxFormPassword || !validPasswordText(pw):
		f.fail(fieldPassword, ui.tr(tHostFormPwBad, maxFormPassword))
	case pw != again:
		f.fail(fieldAgain, ui.tr(tHostFormPwMismatch))
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

// keyList is the form's state for the SSH keys added so far.
type keyList struct{ keys []userKey }

// add checks one key line and puts its key on the list; it returns the problem, empty when the key was added.
func (k *keyList) add(ui *uiContext, text string) string {
	key, problem := parseUserKey(ui, text)
	switch {
	case problem != "":
		return problem
	case slices.ContainsFunc(k.keys, func(o userKey) bool { return o.fingerprint == key.fingerprint }):
		return ui.tr(tHostFormKeyDup)
	case len(k.keys) >= maxFormKeys:
		return ui.tr(tHostFormKeyMax, maxFormKeys)
	}
	k.keys = append(k.keys, key)
	return ""
}

// summary lists the fingerprints of the keys added, the latest few.
func (k *keyList) summary(ui *uiContext) string {
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
	return ui.tr(tHostFormKeys, len(k.keys), strings.Join(fps, ", ")+more)
}

// --- create ---

// The kinds of account the create form offers, by the id of the choice.
const (
	kindPerson  = "person"
	kindService = "service"
)

// openUserCreate opens the form for a new person or service account.
func (m *Model) openUserCreate() tea.Cmd {
	f := &formDlg{title: m.tr(tHostNewTitle), saveText: answer(tHintSave, actSave), keys: &keyList{}}
	f.fields = []kittui.FormField{
		choiceField(fieldKind, tHostFormKind, shownChoices([]string{kindPerson, kindService}, []textID{tHostKindPerson, tHostKindService}), 0),
		textField(fieldName, tColName),
		maskedField(fieldPassword, tHostFormPassword),
		maskedField(fieldAgain, tHostFormPassword2),
		choiceField(fieldAdmin, tHostColAdmin, yesNo(), 0),
		choiceField(fieldHome, tHostFormHome, yesNo(), 0),
		choiceField(fieldDocker, tHostColDocker, yesNo(), 0),
		f.pickField(fieldGroups, tHostFormFurther, m.furtherGroups(false), nil),
		listField(fieldKeys, tHostFormKeyList),
	}
	f.changed = func(m *Model, f *formDlg, id kittui.FieldID) {
		if id == fieldKind {
			arrangeCreate(m, f)
		}
	}
	f.submit = submitCreate
	arrangeCreate(m, f)
	m.openForm(f)
	return nil
}

func serviceKind(f *formDlg) bool { return f.value(fieldKind) == kindService }

// arrangeCreate shows the fields of the kind chosen. A service account has no password, Admin or key; the keys
// added for a person are forgotten when the kind changes, and a password is never kept across the change.
func arrangeCreate(m *Model, f *formDlg) {
	service := f.kit != nil && serviceKind(f)
	for _, id := range []kittui.FieldID{fieldPassword, fieldAgain, fieldAdmin, fieldKeys} {
		f.show(id, !service)
	}
	f.show(fieldHome, service)
	f.hint = m.tr(tHostNewHint)
	if service {
		f.keys.keys = nil
		f.hint = m.tr(tHostNewServiceHint)
	}
	if f.kit != nil {
		f.rearrange(m)
	}
}

func submitCreate(m *Model, f *formDlg) tea.Cmd {
	name := f.value(fieldName)
	switch {
	case name == "":
		f.fail(fieldName, m.tr(tHostFormNameEmpty))
	case m.hostUserExists(name):
		f.fail(fieldName, m.tr(tHostFormNameTaken))
	}
	service := serviceKind(f)
	if !service {
		passwordProblem(m.ui, f)
	}
	if f.failed() {
		return nil
	}
	args := []string{name}
	admin, docker := !service && f.value(fieldAdmin) == hostYes, f.value(fieldDocker) == hostYes
	if admin {
		args = append(args, "admin")
	}
	if service && f.value(fieldHome) == hostYes {
		args = append(args, "home")
	}
	if docker {
		args = append(args, "docker")
	}
	for _, g := range f.picked(fieldGroups) {
		args = append(args, "group="+g)
	}
	if service {
		p, refusal := m.planHost(hostAsk{id: hostchange.ActionUserCreateService, args: args, power: docker})
		if refusal != "" {
			f.refuse(refusal)
			return nil
		}
		m.closeForm()
		m.confirmHost(p)
		return nil
	}
	input := hostchange.CreateInput{Password: f.raw(fieldPassword)}
	for _, k := range f.keys.keys {
		input.Keys = append(input.Keys, k.line)
	}
	secret, err := json.Marshal(input)
	pw := []byte(input.Password)
	if err != nil {
		zero(pw)
		f.refuse(err.Error())
		return nil
	}
	p, refusal := m.planHost(hostAsk{id: hostchange.ActionUserCreate, args: args, secret: secret, mask: [][]byte{pw}, power: admin || docker})
	if refusal != "" {
		zero(secret)
		zero(pw)
		f.refuse(refusal)
		return nil
	}
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
	chgLock         = "lock"
	chgUnlock       = "unlock"
)

// changeText is the text drawn for the change of an id.
func changeText(id string) textID {
	return map[string]textID{
		chgAddGroups: tHostChgAddGroups, chgRemoveGroups: tHostChgRemoveGroups, chgAdminOn: tHostChgAdminOn,
		chgAdminOff: tHostChgAdminOff, chgDockerOn: tHostChgDockerOn, chgDockerOff: tHostChgDockerOff,
		chgPassword: tHostChgPassword, chgAddKey: tHostChgAddKey, chgRemoveKey: tHostChgRemoveKey, chgEnable: tHostChgEnable,
		chgLock: tHostChgLock, chgUnlock: tHostChgUnlock,
	}[id]
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
	// An unread lock state offers both; a password lock without the end of the account offers both, too, so
	// that it can be completed or released.
	fullyLocked := u.Locked != nil && *u.Locked && u.Expired != nil && *u.Expired
	clean := u.Locked != nil && !*u.Locked && (u.Expired == nil || !*u.Expired)
	if !fullyLocked {
		ids = append(ids, chgLock)
	}
	if !clean {
		ids = append(ids, chgUnlock)
	}
	if u.Admin && !u.Enabled {
		ids = append([]string{chgEnable}, ids...)
	}
	shown := make([]textID, len(ids))
	for i, id := range ids {
		shown[i] = changeText(id)
	}
	var keys []kittui.Choice
	for _, k := range u.SSHKeys {
		keys = append(keys, kittui.Choice{ID: kittui.ChoiceID(k.Fingerprint), Label: literal(m.diffText(k.Fingerprint + " " + k.Comment))})
	}
	st := &editForm{u: u}
	f := &formDlg{title: m.tr(tHostEditTitle, m.diffText(u.Name)), saveText: answer(tHintSave, actSave), submit: st.submit}
	f.fields = []kittui.FormField{
		choiceField(fieldWhat, tHostFormWhat, shownChoices(ids, shown), 0),
		f.pickField(fieldAdd, tHostFormAddTo, add, m.systemNotes(add)),
		f.pickField(fieldRemove, tHostFormRemoveFrom, remove, m.systemNotes(remove)),
		maskedField(fieldPassword, tHostFormPassword),
		maskedField(fieldAgain, tHostFormPassword2),
		choiceField(fieldMust, tHostFormMust, shownChoices([]string{hostYes, hostNo}, []textID{tHostYes, tHostNo}), 0),
		textField(fieldKey, tHostFormKey),
		choiceField(fieldKeyOf, tHostFormKeyOf, keys, 0),
	}
	f.changed = func(m *Model, f *formDlg, id kittui.FieldID) {
		if id == fieldWhat {
			st.arrange(m, f)
		}
	}
	st.arrange(m, f)
	m.openForm(f)
	return nil
}

type editForm struct {
	u hostadmin.User
}

// arrange shows the fields of the change chosen and the hint that goes with it.
func (st *editForm) arrange(m *Model, f *formDlg) {
	what := chgAddGroups
	if f.kit != nil {
		what = f.value(fieldWhat)
	} else if len(f.fields[0].Choices) > 0 {
		what = string(f.fields[0].Choices[0].ID)
	}
	show := map[kittui.FieldID]bool{
		fieldAdd: what == chgAddGroups, fieldRemove: what == chgRemoveGroups,
		fieldPassword: what == chgPassword, fieldAgain: what == chgPassword, fieldMust: what == chgPassword,
		fieldKey: what == chgAddKey, fieldKeyOf: what == chgRemoveKey && len(st.u.SSHKeys) > 0,
	}
	for _, field := range f.fields {
		if field.ID != fieldWhat {
			f.show(field.ID, show[field.ID])
		}
	}
	f.hint = ""
	switch what {
	case chgAdminOn, chgDockerOn:
		f.hint = m.tr(tHostPowerWarn)
	case chgEnable:
		f.hint = m.tr(tHostChgEnableHint)
	case chgAddKey:
		f.hint = m.tr(tHostChgKeyHint)
	case chgRemoveKey:
		if st.u.SSHKeys == nil {
			f.hint = m.tr(tHostFormKeysUnread)
		}
	}
	if f.kit != nil {
		f.rearrange(m)
	}
}

// guardRevoke gives the early message of the rules the root action enforces for taking admin away.
func (m *Model) guardRevoke(u hostadmin.User) string { return m.guardAdmin(u, tHostSelfAdmin) }

// guardLock gives the early message of the rules the root action enforces for locking.
func (m *Model) guardLock(u hostadmin.User) string {
	if uid := m.hostUID(); uid != 0 && uid == u.UID {
		return m.tr(tHostSelfLock)
	}
	return m.guardAdmin(u, tHostSelfAdmin)
}

// guardDelete gives the early message of the rules the root action enforces for deleting.
func (m *Model) guardDelete(u hostadmin.User) string {
	if uid := m.hostUID(); uid != 0 && uid == u.UID {
		return m.tr(tHostSelfDelete)
	}
	return m.guardAdmin(u, tHostSelfDelete)
}

// guardAdmin refuses the last admin; an admin whose account has ended does not remain.
func (m *Model) guardAdmin(u hostadmin.User, self textID) string {
	if !u.Admin {
		return ""
	}
	if uid := m.hostUID(); uid != 0 && uid == u.UID {
		return m.tr(self)
	}
	for _, o := range m.hst.users.all {
		if o.Name != u.Name && o.Admin && o.Enabled && (o.Expired == nil || !*o.Expired) {
			return ""
		}
	}
	return m.tr(tHostLastAdmin, m.diffText(u.Name))
}

func (st *editForm) submit(m *Model, f *formDlg) tea.Cmd {
	name := st.u.Name
	ask := hostAsk{args: []string{name}}
	var chosen []string // the further groups of the change; the groups of the Admin and Docker switches do not count
	switch what := f.value(fieldWhat); what {
	case chgAddGroups, chgRemoveGroups:
		id, action := fieldAdd, hostchange.ActionUserAddGroups
		if what == chgRemoveGroups {
			id, action = fieldRemove, hostchange.ActionUserRemoveGroups
		}
		picked := f.picked(id)
		if len(picked) == 0 {
			f.fail(id, m.tr(tHostFormTickOne))
			return nil
		}
		ask.id, ask.args, chosen = action, append(ask.args, picked...), picked
	case chgAdminOn, chgDockerOn:
		ask.id, ask.args, ask.power = hostchange.ActionUserSetAdmin, append(ask.args, "on"), true
		if what == chgDockerOn {
			ask.id = hostchange.ActionUserSetDocker
		}
	case chgAdminOff:
		if msg := m.guardRevoke(st.u); msg != "" {
			f.refuse(msg)
			return nil
		}
		ask.id, ask.args = hostchange.ActionUserSetAdmin, append(ask.args, "off")
		if !st.u.Enabled {
			ask.args = append(ask.args, "without-qsysmin")
		}
	case chgDockerOff:
		ask.id, ask.args = hostchange.ActionUserSetDocker, append(ask.args, "off")
	case chgEnable:
		ask.id = hostchange.ActionUserEnable
	case chgLock:
		if msg := m.guardLock(st.u); msg != "" {
			f.refuse(msg)
			return nil
		}
		ask.id = hostchange.ActionUserLock
	case chgUnlock:
		ask.id = hostchange.ActionUserUnlock
	case chgPassword:
		if passwordProblem(m.ui, f) {
			return nil
		}
		ask.id, ask.secret = hostchange.ActionUserSetPassword, []byte(f.raw(fieldPassword))
		if f.value(fieldMust) == hostYes {
			ask.args = append(ask.args, "must-change")
		}
	case chgAddKey:
		key, problem := parseUserKey(m.ui, f.value(fieldKey))
		if problem != "" {
			f.fail(fieldKey, problem)
			return nil
		}
		ask.id, ask.secret = hostchange.ActionUserAddKey, []byte(key.line)
	case chgRemoveKey:
		if st.u.SSHKeys == nil {
			m.closeForm()
			return m.askReadUser()
		}
		ask.id, ask.args = hostchange.ActionUserRemoveKey, append(ask.args, f.value(fieldKeyOf))
	}
	ask.risk = m.riskOf(st.u, chosen)
	p, refusal := m.planHost(ask)
	if refusal != "" {
		zero(ask.secret)
		f.refuse(refusal)
		return nil
	}
	m.closeForm()
	m.confirmHost(p)
	return nil
}

// hostUsersKey handles n, e and d on the Users page.
func (m *Model) hostUsersKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case m.keyIs(key, actNew):
		return m.openUserCreate(), true
	case m.keyIs(key, actEdit):
		return m.openUserEdit(), true
	case m.keyIs(key, actDelete):
		return m.openUserDelete(), true
	}
	return nil, false
}

// --- delete ---

// homeRiskReader is a host source that names the risks of deleting a home itself, as a test fixture does.
type homeRiskReader interface {
	HomeRisks(name, home string) ([]hostchange.HomeRisk, error)
}

// homeRisks reads what deleting the home of u would reach beyond its own files. It reads only what the user
// running the interface may read.
func (m *Model) homeRisks(u hostadmin.User) ([]hostchange.HomeRisk, error) {
	switch src := m.hostSrc.(type) {
	case *hostadmin.Host:
		return hostchange.HomeRisks(src, u.Name, u.Home)
	case homeRiskReader:
		return src.HomeRisks(u.Name, u.Home)
	}
	return nil, nil
}

// homeRiskText says each risk of deleting the home of u in one plain sentence.
func (m *Model) homeRiskText(u hostadmin.User) string {
	home := m.diffText(u.Home)
	risks, err := m.homeRisks(u)
	var parts []string
	for _, r := range risks {
		who := m.diffText(strings.Join(r.Who, ", "))
		switch r.Kind {
		case hostchange.HomeSystemPath:
			parts = append(parts, m.tr(tHostHomeSystem, home))
		case hostchange.HomeHoldsHome:
			parts = append(parts, m.tr(tHostHomeHolds, home, who))
		case hostchange.HomeLink:
			parts = append(parts, m.tr(tHostHomeLink, home))
		case hostchange.HomeNotDirectory:
			parts = append(parts, m.tr(tHostHomeNotDir, home))
		case hostchange.HomeForeignOwner:
			parts = append(parts, m.tr(tHostHomeOwner, home, who))
		}
	}
	if err != nil {
		parts = append(parts, m.tr(tHostHomeUnread, home, m.diffText(err.Error())))
	}
	return strings.Join(parts, " ")
}

// The choices for the home of a deleted user, by the id of the choice.
const (
	homeKeep   = "keep"
	homeDelete = "delete"
)

// openUserDelete asks whether the home of the marked user goes too; F2 leads to the red confirmation. Keeping
// the home is the default and the only delete that can be undone.
func (m *Model) openUserDelete() tea.Cmd {
	name, ok := m.hst.users.page.current()
	if !ok {
		return nil
	}
	u, ok := m.hst.users.items[name]
	if !ok {
		return nil
	}
	f := &formDlg{title: m.tr(tHostDeleteTitle, m.diffText(u.Name)), saveText: answer(tHintContinue, actSave)}
	f.fields = []kittui.FormField{
		choiceField(fieldHome, tHostFormHome, shownChoices([]string{homeKeep, homeDelete}, []textID{tHostHomeKeep, tHostHomeDelete}), 0),
	}
	arrange := func(f *formDlg) {
		f.hint = m.tr(tHostDeleteHint)
		if f.kit != nil && f.value(fieldHome) == homeDelete {
			f.hint = m.tr(tHostDeleteHomeHint)
		}
	}
	f.changed = func(_ *Model, f *formDlg, _ kittui.FieldID) { arrange(f) }
	f.submit = func(m *Model, f *formDlg) tea.Cmd {
		if msg := m.guardDelete(u); msg != "" {
			f.refuse(msg)
			return nil
		}
		ask := hostAsk{id: hostchange.ActionUserDelete, args: []string{u.Name}, risk: m.riskOf(u, nil)}
		if f.value(fieldHome) == homeDelete {
			ask.args = append(ask.args, "with-home")
			ask.final = strings.TrimSpace(m.homeRiskText(u) + " " + m.tr(tHostDeleteFinal, m.diffText(u.Home)))
		}
		p, refusal := m.planHost(ask)
		if refusal != "" {
			f.refuse(refusal)
			return nil
		}
		m.closeForm()
		m.confirmHost(p)
		return nil
	}
	arrange(f)
	m.openForm(f)
	return nil
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
		parts = append(parts, m.tr(tHostRiskRoot))
	}
	if u.System {
		parts = append(parts, m.tr(tHostRiskAccount))
	}
	for _, g := range groups {
		if m.isSystemGroup(g) {
			parts = append(parts, m.tr(tHostRiskGroup))
			break
		}
	}
	return strings.Join(parts, " ")
}
