package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/crypto/ssh"

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

// passwordProblem checks the two entries of a password field pair.
func passwordProblem(ui *uiContext, f *formDlg, first, second string) bool {
	pw, again := f.row(first), f.row(second)
	switch {
	case pw.raw() == "":
		pw.problem = ui.tr(tHostFormPwEmpty)
	case utf8.RuneCountInString(pw.raw()) > maxFormPassword || !validPasswordText(pw.raw()):
		pw.problem = ui.tr(tHostFormPwBad, maxFormPassword)
	case pw.raw() != again.raw():
		again.problem = ui.tr(tHostFormPwMismatch)
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
func (k *keyList) add(ui *uiContext, row *formRow) bool {
	key, problem := parseUserKey(ui, row.value())
	switch {
	case problem != "":
		row.problem = problem
	case slices.ContainsFunc(k.keys, func(o userKey) bool { return o.fingerprint == key.fingerprint }):
		row.problem = ui.tr(tHostFormKeyDup)
	case len(k.keys) >= maxFormKeys:
		row.problem = ui.tr(tHostFormKeyMax, maxFormKeys)
	default:
		k.keys = append(k.keys, key)
		row.in.SetValue("")
		return true
	}
	return false
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

type createForm struct{ keyList }

// openUserCreate opens the form for a new person or service account.
func (m *Model) openUserCreate() tea.Cmd {
	st := &createForm{}
	f := &formDlg{title: m.tr(tHostNewTitle), saveText: "{save} save"}
	yesNo := []string{hostNo, hostYes}
	shown := []string{m.tr(tHostNo), m.tr(tHostYes)}
	f.rows = []formRow{
		choiceRow("kind", m.tr(tHostFormKind), []string{kindPerson, kindService}, []string{m.tr(tHostKindPerson), m.tr(tHostKindService)}, 0),
		textRow("name", m.tr(tColName), ""),
		maskedRow("password", m.tr(tHostFormPassword)),
		maskedRow("again", m.tr(tHostFormPassword2)),
		choiceRow("admin", m.tr(tHostColAdmin), yesNo, shown, 0),
		choiceRow("home", m.tr(tHostFormHome), yesNo, shown, 0),
		choiceRow("docker", m.tr(tHostColDocker), yesNo, shown, 0),
		pickRow("groups", m.tr(tHostFormFurther), m.furtherGroups(false), nil),
		textRow("key", m.tr(tHostFormKey), ""),
	}
	f.enter = func(_ *Model, f *formDlg, row int) bool {
		if f.rows[row].key != "key" || f.rows[row].value() == "" {
			return false
		}
		if st.add(m.ui, &f.rows[row]) {
			f.hint = m.tr(tHostNewHint) + " " + st.summary(m.ui)
		}
		return true
	}
	f.changed = func(_ *Model, f *formDlg, row int) {
		if f.rows[row].key == "kind" {
			st.arrange(m.ui, f)
		}
	}
	f.submit = st.submit
	st.arrange(m.ui, f)
	m.openForm(f)
	return nil
}

func serviceKind(f *formDlg) bool { return f.row("kind").value() == kindService }

// arrange shows the rows of the kind chosen. A service account has no password, Admin or key; what a person
// typed there is forgotten when the kind changes.
func (st *createForm) arrange(ui *uiContext, f *formDlg) {
	service := serviceKind(f)
	for _, k := range []string{"password", "again", "admin", "key"} {
		f.row(k).hidden = service
	}
	f.row("home").hidden = !service
	f.hint = ui.tr(tHostNewHint)
	if service {
		wipe(f, "password", "again")
		f.row("key").in.SetValue("")
		st.keys = nil
		f.hint = ui.tr(tHostNewServiceHint)
	}
}

func (st *createForm) submit(m *Model, f *formDlg) tea.Cmd {
	name := f.row("name").value()
	switch {
	case name == "":
		f.row("name").problem = m.tr(tHostFormNameEmpty)
	case m.hostUserExists(name):
		f.row("name").problem = m.tr(tHostFormNameTaken)
	}
	bad := f.row("name").problem != ""
	service := serviceKind(f)
	if !service {
		if passwordProblem(m.ui, f, "password", "again") {
			bad = true
		}
		if f.row("key").value() != "" && !st.add(m.ui, f.row("key")) {
			bad = true
		}
	}
	if bad {
		return nil
	}
	args := []string{name}
	admin, docker := !service && f.row("admin").value() == hostYes, f.row("docker").value() == hostYes
	if admin {
		args = append(args, "admin")
	}
	if service && f.row("home").value() == hostYes {
		args = append(args, "home")
	}
	if docker {
		args = append(args, "docker")
	}
	for _, g := range f.row("groups").picked() {
		args = append(args, "group="+g)
	}
	if service {
		p, refusal := m.planHost(hostAsk{id: hostchange.ActionUserCreateService, args: args, power: docker})
		if refusal != "" {
			f.notice = refusal
			return nil
		}
		m.closeForm()
		m.confirmHost(p)
		return nil
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
	chgLock         = "lock"
	chgUnlock       = "unlock"
)

func changeText(ui *uiContext, id string) string {
	return ui.tr(map[string]textID{
		chgAddGroups: tHostChgAddGroups, chgRemoveGroups: tHostChgRemoveGroups, chgAdminOn: tHostChgAdminOn,
		chgAdminOff: tHostChgAdminOff, chgDockerOn: tHostChgDockerOn, chgDockerOff: tHostChgDockerOff,
		chgPassword: tHostChgPassword, chgAddKey: tHostChgAddKey, chgRemoveKey: tHostChgRemoveKey, chgEnable: tHostChgEnable,
		chgLock: tHostChgLock, chgUnlock: tHostChgUnlock,
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
	shown := make([]string, len(ids))
	for i, id := range ids {
		shown[i] = changeText(m.ui, id)
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
	f := &formDlg{title: m.tr(tHostEditTitle, m.diffText(u.Name)), saveText: "{save} save", submit: st.submit}
	f.rows = []formRow{
		choiceRow("what", m.tr(tHostFormWhat), ids, shown, 0),
		pickRow("add", m.tr(tHostFormAddTo), add, m.systemNotes(add)),
		pickRow("remove", m.tr(tHostFormRemoveFrom), remove, m.systemNotes(remove)),
		maskedRow("password", m.tr(tHostFormPassword)),
		maskedRow("again", m.tr(tHostFormPassword2)),
		choiceRow("must", m.tr(tHostFormMust), []string{hostYes, hostNo}, []string{m.tr(tHostYes), m.tr(tHostNo)}, 0),
		textRow("key", m.tr(tHostFormKey), ""),
		choiceRow("fp", m.tr(tHostFormKeyOf), fps, fpShown, 0),
	}
	f.changed = func(_ *Model, f *formDlg, row int) {
		if f.rows[row].key == "what" {
			st.arrange(m.ui, f)
		}
	}
	st.arrange(m.ui, f)
	m.openForm(f)
	return nil
}

type editForm struct {
	u           hostadmin.User
	add, remove []string
}

// arrange shows the rows of the change chosen and the hint that goes with it.
func (st *editForm) arrange(ui *uiContext, f *formDlg) {
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
		f.hint = ui.tr(tHostPowerWarn)
	case chgEnable:
		f.hint = ui.tr(tHostChgEnableHint)
	case chgAddKey:
		f.hint = ui.tr(tHostChgKeyHint)
	case chgRemoveKey:
		if st.u.SSHKeys == nil {
			f.hint = ui.tr(tHostFormKeysUnread)
		}
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
	switch f.row("what").value() {
	case chgAddGroups, chgRemoveGroups:
		key, id := "add", hostchange.ActionUserAddGroups
		if f.row("what").value() == chgRemoveGroups {
			key, id = "remove", hostchange.ActionUserRemoveGroups
		}
		picked := f.row(key).picked()
		if len(picked) == 0 {
			f.row(key).problem = m.tr(tHostFormTickOne)
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
			ask.args = append(ask.args, "without-qsysmin")
		}
	case chgDockerOff:
		ask.id, ask.args = hostchange.ActionUserSetDocker, append(ask.args, "off")
	case chgEnable:
		ask.id = hostchange.ActionUserEnable
	case chgLock:
		if msg := m.guardLock(st.u); msg != "" {
			f.notice = msg
			return nil
		}
		ask.id = hostchange.ActionUserLock
	case chgUnlock:
		ask.id = hostchange.ActionUserUnlock
	case chgPassword:
		if passwordProblem(m.ui, f, "password", "again") {
			return nil
		}
		ask.id, ask.secret = hostchange.ActionUserSetPassword, []byte(f.row("password").raw())
		if f.row("must").value() == hostYes {
			ask.args = append(ask.args, "must-change")
		}
	case chgAddKey:
		key, problem := parseUserKey(m.ui, f.row("key").value())
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
	f := &formDlg{title: m.tr(tHostDeleteTitle, m.diffText(u.Name)), saveText: "{save} continue"}
	f.rows = []formRow{
		choiceRow("home", m.tr(tHostFormHome), []string{homeKeep, homeDelete}, []string{m.tr(tHostHomeKeep), m.tr(tHostHomeDelete)}, 0),
	}
	arrange := func(f *formDlg) {
		f.hint = m.tr(tHostDeleteHint)
		if f.row("home").value() == homeDelete {
			f.hint = m.tr(tHostDeleteHomeHint)
		}
	}
	f.changed = func(_ *Model, f *formDlg, _ int) { arrange(f) }
	f.submit = func(m *Model, f *formDlg) tea.Cmd {
		if msg := m.guardDelete(u); msg != "" {
			f.notice = msg
			return nil
		}
		ask := hostAsk{id: hostchange.ActionUserDelete, args: []string{u.Name}, risk: m.riskOf(u, nil)}
		if f.row("home").value() == homeDelete {
			ask.args = append(ask.args, "with-home")
			ask.final = strings.TrimSpace(m.homeRiskText(u) + " " + m.tr(tHostDeleteFinal, m.diffText(u.Home)))
		}
		p, refusal := m.planHost(ask)
		if refusal != "" {
			f.notice = refusal
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
