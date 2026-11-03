package tui

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// Host shows the users, groups and folder permissions of the machine qsysmin runs on. The data comes from the
// local databases through a HostSource, not from the service: reading them needs no privilege, and the fields
// only root can read stay unknown. Changes go through root actions, never through the source.
const (
	hostUsers = iota
	hostGroups
	hostFolders
	hostChanges
	hostCount
)

func hostTitle(ui *uiContext, sub int) string {
	return ui.tr([...]textID{tSubUsers, tSubGroups, tSubFolders, tSubChanges}[sub])
}

// hostKey is the English name of a Host page, the one commands are given by.
func hostKey(sub int) string { return [...]string{"users", "groups", "folders", "changes"}[sub] }

// HostSource is what Host reads. *hostadmin.Host is the real one; tests hand in a fixture.
type HostSource interface {
	Users() ([]hostadmin.User, error)
	Groups() ([]hostadmin.Group, error)
	Folder(path string) (hostadmin.Folder, error)
}

var errNoHostSource = errors.New("no host source")

// hostState is the Host area: the page shown and the state of each page.
type hostState struct {
	sub    int
	users  hostUsersState
	groups hostGroupsState
	fold   hostFoldersState
	chg    hostChangesState
}

type hostUsersState struct {
	page  dataPage
	all   []hostadmin.User
	items map[string]hostadmin.User
	err   string
	have  bool
}

type hostGroupsState struct {
	page  dataPage
	all   []hostadmin.Group
	items map[string]hostadmin.Group
	err   string
	have  bool
}

type hostUsersMsg struct {
	list []hostadmin.User
	err  error
}

type hostGroupsMsg struct {
	list []hostadmin.Group
	err  error
}

// setHostSub shows page sub of Host.
func (m *Model) setHostSub(sub int) tea.Cmd {
	m.hst.sub = (sub + hostCount) % hostCount
	m.closeDetails()
	m.clearMessages()
	return m.enterPage()
}

// hostCmd reads the page shown; the files are read again each time the page opens.
func (m *Model) hostCmd() tea.Cmd {
	src := m.hostSrc
	switch m.hst.sub {
	case hostUsers:
		return m.readHostUsers()
	case hostGroups:
		return func() tea.Msg {
			if src == nil {
				return hostGroupsMsg{err: errNoHostSource}
			}
			list, err := src.Groups()
			return hostGroupsMsg{list, err}
		}
	case hostFolders:
		return m.enterFolders()
	}
	// Changes reads the journal only when asked: that needs root.
	return nil
}

// readHostUsers reads the users from the files; Host and the Overview both show what it brings.
func (m *Model) readHostUsers() tea.Cmd {
	src := m.hostSrc
	return func() tea.Msg {
		if src == nil {
			return hostUsersMsg{err: errNoHostSource}
		}
		list, err := src.Users()
		return hostUsersMsg{list, err}
	}
}

func (m *Model) takeHostUsers(msg hostUsersMsg) {
	u := &m.hst.users
	u.err, u.have = "", msg.err == nil
	u.all = msg.list
	if msg.err != nil {
		u.err, u.all = m.diffText(msg.err.Error()), nil
	}
	m.fillHostUsers()
}

func (m *Model) takeHostGroups(msg hostGroupsMsg) {
	g := &m.hst.groups
	g.err, g.have = "", msg.err == nil
	g.all = msg.list
	if msg.err != nil {
		g.err, g.all = m.diffText(msg.err.Error()), nil
	}
	m.fillHostGroups()
}

// adminText is the Admin cell of a user: an administrator who cannot use qsysmin yet is marked.
func adminText(u hostadmin.User) string {
	switch {
	case u.Admin && !u.Enabled:
		return hostAdminOff
	case u.Admin:
		return hostYes
	}
	return hostNo
}

// The raw values of the cells; shownHost draws them in the language of the editor.
const (
	hostYes      = "yes"
	hostNo       = "no"
	hostAdminOff = "yes-off"
	hostSystem   = "system"
)

func (m *Model) shownHost(v string) string {
	switch v {
	case hostYes:
		return m.tr(tHostYes)
	case hostNo:
		return m.tr(tHostNo)
	case hostAdminOff:
		return m.tr(tHostAdminOff)
	case hostSystem:
		return m.tr(tHostSystem)
	}
	return v
}

func (m *Model) hostSystemTone(system func(name string) bool) func(string) lipgloss.Style {
	return func(cell string) lipgloss.Style {
		if system(cell) {
			return m.ui.st(kittui.StyleHint)
		}
		return m.ui.st(kittui.StyleText)
	}
}

func (m *Model) hostUserColumns() []column {
	warn := func(cell string) lipgloss.Style {
		if cell == m.tr(tHostAdminOff) {
			return m.ui.st(kittui.StyleWarn)
		}
		return m.ui.st(kittui.StyleText)
	}
	return []column{
		{title: m.tr(tColName), tone: m.hostSystemTone(func(n string) bool { return m.hst.users.items[n].System })},
		{title: m.tr(tHostColAdmin), shown: m.shownHost, short: map[string]string{hostAdminOff: m.tr(tHostAdminOffShort)}, tone: warn},
		{title: m.tr(tHostColDocker), shown: m.shownHost},
		{title: m.tr(tHostColGroups), flex: true, optional: true, drop: 2},
		{title: m.tr(tHostColSystem), shown: m.shownHost, optional: true, drop: 1, tone: m.ui.toneDim},
	}
}

func (m *Model) hostGroupColumns() []column {
	return []column{
		{title: m.tr(tColName), tone: m.hostSystemTone(func(n string) bool { return m.hst.groups.items[n].System })},
		{title: m.tr(tHostColMembers), right: true},
		{title: m.tr(tHostColSystem), shown: m.shownHost, optional: true, tone: m.ui.toneDim},
	}
}

// fillHostUsers rebuilds the rows of Users from what was read: the users, and the system accounts in a section
// of their own that starts folded.
func (m *Model) fillHostUsers() {
	u := &m.hst.users
	u.items = map[string]hostadmin.User{}
	rows := map[string][]string{}
	regular := dataSection{key: "users", title: tSubUsers}
	system := dataSection{key: "system", title: tHostSysAccounts, collapsed: true}
	for _, user := range u.all {
		u.items[user.Name] = user
		if user.System {
			system.names = append(system.names, user.Name)
		} else {
			regular.names = append(regular.names, user.Name)
		}
		mark := ""
		if user.System {
			mark = hostSystem
		}
		rows[user.Name] = []string{m.diffText(user.Name), adminText(user), hostYesNo(user.Docker),
			m.diffText(strings.Join(user.Groups, ", ")), mark}
	}
	u.page.cols = m.hostUserColumns()
	u.page.setSections([]dataSection{regular, system}, rows)
}

func (m *Model) fillHostGroups() {
	g := &m.hst.groups
	g.items = map[string]hostadmin.Group{}
	rows := map[string][]string{}
	regular := dataSection{key: "groups", title: tSubGroups}
	system := dataSection{key: "system", title: tHostSysGroups, collapsed: true}
	for _, group := range g.all {
		g.items[group.Name] = group
		mark := ""
		if group.System {
			mark = hostSystem
			system.names = append(system.names, group.Name)
		} else {
			regular.names = append(regular.names, group.Name)
		}
		rows[group.Name] = []string{m.diffText(group.Name), strconv.Itoa(len(group.Members)), mark}
	}
	g.page.cols = m.hostGroupColumns()
	g.page.setSections([]dataSection{regular, system}, rows)
}

func hostYesNo(b bool) string {
	if b {
		return hostYes
	}
	return hostNo
}

// knownBool draws a value only root can read: nil is unknown, never "no".
func hostKnownBool(ui *uiContext, b *bool) string {
	switch {
	case b == nil:
		return ui.tr(tHostUnknown)
	case *b:
		return ui.tr(tHostYes)
	}
	return ui.tr(tHostNo)
}

func (m *Model) hostUsersSpec() dataSpec {
	empty := m.tr(tHostNoUsers)
	if m.hst.users.err != "" {
		empty = m.hst.users.err
	}
	return dataSpec{title: m.tr(tSubUsers), empty: empty, lines: m.hostUserLines}
}

func (m *Model) hostGroupsSpec() dataSpec {
	empty := m.tr(tHostNoGroups)
	if m.hst.groups.err != "" {
		empty = m.hst.groups.err
	}
	return dataSpec{title: m.tr(tSubGroups), empty: empty, lines: m.hostGroupLines}
}

func hostList(ui *uiContext, items []string) string {
	if len(items) == 0 {
		return ui.tr(tHostNone)
	}
	return strings.Join(items, ", ")
}

func hostKindText(ui *uiContext, system bool) string {
	if system {
		return ui.tr(tHostSystemRO)
	}
	return ui.tr(tHostRegular)
}

// hostUserLines are the lines of one tab of a user. Keys are shown by type, comment and fingerprint, never as
// the key itself.
func (m *Model) hostUserLines(name string, tab, width int) []string {
	u, ok := m.hst.users.items[name]
	if !ok {
		return nil
	}
	admin := m.shownHost(adminText(u))
	switch tab {
	case tabOverview:
		lines := factLines(m.ui, [][2]string{
			{m.tr(tColName), m.diffText(u.Name)},
			{m.tr(tColType), userKindText(m.ui, u.System)},
			{m.tr(tHostColAdmin), admin},
			{m.tr(tHostColDocker), m.shownHost(hostYesNo(u.Docker))},
			{m.tr(tHostLblEnabled), m.shownHost(hostYesNo(u.Enabled))},
			{m.tr(tHostColGroups), m.diffText(hostList(m.ui, u.Groups))},
			{m.tr(tHostLblHome), m.diffText(u.Home)},
			{m.tr(tHostLblLocked), hostKnownBool(m.ui, u.Locked)},
		}, width)
		if u.Locked == nil {
			lines = append(lines, "")
			lines = append(lines, wrapIndented(m.ui.st(kittui.StyleHint), "", m.tr(tHostReadHint, "ctrl+r"), width)...)
		}
		return lines
	case tabDetails:
		if u.SSHKeys == nil {
			lines := wrapIndented(m.ui.st(kittui.StyleHint), "", m.tr(tHostKeysUnknown), width)
			return append(append(lines, ""), wrapIndented(m.ui.st(kittui.StyleHint), "", m.tr(tHostReadHint, "ctrl+r"), width)...)
		}
		if len(u.SSHKeys) == 0 {
			return []string{m.ui.st(kittui.StyleHint).Render(m.tr(tHostNoKeys))}
		}
		var lines []string
		for _, k := range u.SSHKeys {
			lines = append(lines, wrapIndented(lipgloss.NewStyle(), "", m.diffText(k.Type+" "+k.Fingerprint+"  "+k.Comment), width)...)
		}
		return lines
	}
	lines := []string{
		"name: " + m.diffText(u.Name), "uid: " + strconv.Itoa(u.UID), "system: " + strconv.FormatBool(u.System),
		"primary_group: " + m.diffText(u.PrimaryGroup), "groups: " + m.diffText(strings.Join(u.Groups, ", ")),
		"home: " + m.diffText(u.Home), "admin: " + strconv.FormatBool(u.Admin), "docker: " + strconv.FormatBool(u.Docker),
		"enabled: " + strconv.FormatBool(u.Enabled), "locked: " + hostRawBool(u.Locked), "expired: " + hostRawBool(u.Expired),
	}
	if u.SSHKeys == nil {
		return append(lines, "ssh_keys: unknown")
	}
	lines = append(lines, "ssh_keys:")
	for _, k := range u.SSHKeys {
		lines = append(lines, "  - type: "+m.diffText(k.Type), "    comment: "+m.diffText(k.Comment), "    fingerprint: "+m.diffText(k.Fingerprint))
	}
	return lines
}

// hostChangesView is the Changes page: the table of the journal, with a note when it is out of date.
func (m *Model) hostChangesView(top int) string {
	body := m.dataBody(&m.hst.chg.page, m.hostChangesSpec(), top)
	if m.hst.chg.stale && m.hst.chg.have && !m.backdrop {
		body += "\n" + m.wrapped(m.ui.st(kittui.StyleWarn), m.tr(tHostChangesStale, "ctrl+r"))
	}
	return body
}

func hostRawBool(b *bool) string {
	if b == nil {
		return "unknown"
	}
	return strconv.FormatBool(*b)
}

func (m *Model) hostGroupLines(name string, tab, width int) []string {
	g, ok := m.hst.groups.items[name]
	if !ok {
		return nil
	}
	switch tab {
	case tabOverview:
		return factLines(m.ui, [][2]string{
			{m.tr(tColName), m.diffText(g.Name)},
			{m.tr(tColType), hostKindText(m.ui, g.System)},
			{m.tr(tHostColMembers), strconv.Itoa(len(g.Members))},
		}, width)
	case tabDetails:
		if len(g.Members) == 0 {
			return []string{m.ui.st(kittui.StyleHint).Render(m.tr(tHostNoMembers))}
		}
		members := append([]string(nil), g.Members...)
		sort.Strings(members)
		var lines []string
		for _, member := range members {
			lines = append(lines, wrapIndented(lipgloss.NewStyle(), "", m.diffText(member), width)...)
		}
		return lines
	}
	lines := []string{"name: " + m.diffText(g.Name), "gid: " + strconv.Itoa(g.GID), "system: " + strconv.FormatBool(g.System), "members:"}
	for _, member := range g.Members {
		lines = append(lines, "  - "+m.diffText(member))
	}
	return lines
}

// hostBody is the page of Host under the bars of the frame.
func (m *Model) hostBody() string {
	top := m.workspaceTop()
	switch m.hst.sub {
	case hostGroups:
		m.hst.groups.page.cols = m.hostGroupColumns()
		return m.dataBody(&m.hst.groups.page, m.hostGroupsSpec(), top)
	case hostFolders:
		return m.hostFoldersBody()
	case hostChanges:
		m.hst.chg.page.cols = m.hostChangeColumns()
		return m.hostChangesView(top)
	}
	m.hst.users.page.cols = m.hostUserColumns()
	return m.dataBody(&m.hst.users.page, m.hostUsersSpec(), top)
}

// hostKeys is the key line of Host.
func (m *Model) hostKeys() []string {
	if m.hst.sub == hostFolders {
		keys := []string{"enter edit path"}
		if _, ok := m.changeableFolder(); ok {
			keys = append(keys, "e change")
		}
		return keys
	}
	if p := m.currentPage(); p != nil && m.detailFocus(p) {
		keys := []string{"←/→ tab", "↑/↓ scroll", "{back} back"}
		if m.hst.sub == hostUsers {
			keys = append(keys, "ctrl+r read as root")
		}
		return keys
	}
	if m.hst.sub == hostChanges {
		return []string{"↑/↓ move", "enter undo", "ctrl+r read as root", "/ filter"}
	}
	enter := "enter details"
	if p := m.currentPage(); p != nil && p.onHeading() {
		enter = "enter collapse"
		if p.headingFolded() {
			enter = "enter expand"
		}
	}
	keys := []string{"↑/↓ move", enter}
	if m.hst.sub == hostUsers {
		keys = append(keys, "n new")
		if m.canEditCurrent() {
			keys = append(keys, "e edit", "d delete")
		}
	}
	if m.hst.sub == hostGroups {
		keys = append(keys, "n new")
		if _, ok := m.currentGroup(); ok {
			keys = append(keys, "e members", "r rename", "d delete")
		}
	}
	return append(keys, "/ filter")
}

// hostKey handles the keys of Host that the page does not.
func (m *Model) hostKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.hst.sub == hostChanges {
		return nil, false
	}
	if m.hst.sub == hostUsers && m.keyIs(key, actReload) {
		if p := m.currentPage(); p != nil && m.detailSeen() {
			if _, ok := p.current(); ok {
				return m.askReadUser(), true
			}
		}
	}
	if m.hst.sub == hostUsers {
		if cmd, ok := m.hostUsersKey(key); ok {
			return cmd, true
		}
	}
	if m.hst.sub == hostGroups {
		return m.hostGroupsKey(key)
	}
	if m.hst.sub == hostFolders {
		if m.keyIs(key, actEnter) || m.keyIs(key, actSearch) || m.keyIs(key, actFilter) {
			f := m.foldersReady()
			f.editing = true
			f.in.Focus()
			return nil, true
		}
		if m.keyIs(key, actEdit) {
			return m.openFolderChange(), true
		}
		return nil, false
	}
	return nil, false
}
