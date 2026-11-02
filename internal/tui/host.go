package tui

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// Host shows the users, groups and folder permissions of the machine sysmin runs on, read-only. The data comes
// from the local databases through a HostSource, not from the service: reading them needs no privilege, and
// the fields only root can read stay unknown.
const (
	hostUsers = iota
	hostGroups
	hostFolders
	hostChanges
	hostCount
)

func hostTitle(sub int) string {
	return tr([...]textID{tSubUsers, tSubGroups, tSubFolders, tSubChanges}[sub])
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
		return func() tea.Msg {
			if src == nil {
				return hostUsersMsg{err: errNoHostSource}
			}
			list, err := src.Users()
			return hostUsersMsg{list, err}
		}
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

// adminText is the Admin cell of a user: an administrator who cannot use sysmin yet is marked.
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

func shownHost(v string) string {
	switch v {
	case hostYes:
		return tr(tHostYes)
	case hostNo:
		return tr(tHostNo)
	case hostAdminOff:
		return tr(tHostAdminOff)
	case hostSystem:
		return tr(tHostSystem)
	}
	return v
}

func (m *Model) hostSystemTone(system func(name string) bool) func(string) lipgloss.Style {
	return func(cell string) lipgloss.Style {
		if system(cell) {
			return styles.hint
		}
		return styles.cell
	}
}

func (m *Model) hostUserColumns() []column {
	warn := func(cell string) lipgloss.Style {
		if cell == tr(tHostAdminOff) {
			return styles.warning
		}
		return styles.cell
	}
	return []column{
		{title: tr(tColName), tone: m.hostSystemTone(func(n string) bool { return m.hst.users.items[n].System })},
		{title: tr(tHostColAdmin), shown: shownHost, short: map[string]string{hostAdminOff: tr(tHostAdminOffShort)}, tone: warn},
		{title: tr(tHostColDocker), shown: shownHost},
		{title: tr(tHostColGroups), flex: true, optional: true, drop: 2},
		{title: tr(tHostColSystem), shown: shownHost, optional: true, drop: 1, tone: toneDim},
	}
}

func (m *Model) hostGroupColumns() []column {
	return []column{
		{title: tr(tColName), tone: m.hostSystemTone(func(n string) bool { return m.hst.groups.items[n].System })},
		{title: tr(tHostColMembers), right: true},
		{title: tr(tHostColSystem), shown: shownHost, optional: true, tone: toneDim},
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
func hostKnownBool(b *bool) string {
	switch {
	case b == nil:
		return tr(tHostUnknown)
	case *b:
		return tr(tHostYes)
	}
	return tr(tHostNo)
}

func (m *Model) hostUsersSpec() dataSpec {
	empty := tr(tHostNoUsers)
	if m.hst.users.err != "" {
		empty = m.hst.users.err
	}
	return dataSpec{title: tr(tSubUsers), empty: empty, lines: m.hostUserLines}
}

func (m *Model) hostGroupsSpec() dataSpec {
	empty := tr(tHostNoGroups)
	if m.hst.groups.err != "" {
		empty = m.hst.groups.err
	}
	return dataSpec{title: tr(tSubGroups), empty: empty, lines: m.hostGroupLines}
}

func hostList(items []string) string {
	if len(items) == 0 {
		return tr(tHostNone)
	}
	return strings.Join(items, ", ")
}

func hostKindText(system bool) string {
	if system {
		return tr(tHostSystemRO)
	}
	return tr(tHostRegular)
}

// hostUserLines are the lines of one tab of a user. Keys are shown by type, comment and fingerprint, never as
// the key itself.
func (m *Model) hostUserLines(name string, tab, width int) []string {
	u, ok := m.hst.users.items[name]
	if !ok {
		return nil
	}
	admin := shownHost(adminText(u))
	switch tab {
	case tabOverview:
		lines := factLines([][2]string{
			{tr(tColName), m.diffText(u.Name)},
			{tr(tColType), hostKindText(u.System)},
			{tr(tHostColAdmin), admin},
			{tr(tHostColDocker), shownHost(hostYesNo(u.Docker))},
			{tr(tHostLblEnabled), shownHost(hostYesNo(u.Enabled))},
			{tr(tHostColGroups), m.diffText(hostList(u.Groups))},
			{tr(tHostLblHome), m.diffText(u.Home)},
			{tr(tHostLblLocked), hostKnownBool(u.Locked)},
		}, width)
		if u.Locked == nil {
			lines = append(lines, "")
			lines = append(lines, wrapIndented(styles.hint, "", tr(tHostReadHint, "ctrl+r"), width)...)
		}
		return lines
	case tabDetails:
		if u.SSHKeys == nil {
			lines := wrapIndented(styles.hint, "", tr(tHostKeysUnknown), width)
			return append(append(lines, ""), wrapIndented(styles.hint, "", tr(tHostReadHint, "ctrl+r"), width)...)
		}
		if len(u.SSHKeys) == 0 {
			return []string{styles.hint.Render(tr(tHostNoKeys))}
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
		body += "\n" + m.wrapped(styles.warning, tr(tHostChangesStale, "ctrl+r"))
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
		return factLines([][2]string{
			{tr(tColName), m.diffText(g.Name)},
			{tr(tColType), hostKindText(g.System)},
			{tr(tHostColMembers), strconv.Itoa(len(g.Members))},
		}, width)
	case tabDetails:
		if len(g.Members) == 0 {
			return []string{styles.hint.Render(tr(tHostNoMembers))}
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
		return []string{"enter edit path"}
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
			keys = append(keys, "e edit")
		}
	}
	return append(keys, "/ filter")
}

// hostKey handles the keys of Host that the page does not.
func (m *Model) hostKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.hst.sub == hostChanges {
		return nil, false
	}
	if m.hst.sub == hostUsers && keyIs(key, actReload) {
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
	if m.hst.sub == hostFolders {
		if keyIs(key, actEnter) || keyIs(key, actSearch) || keyIs(key, actFilter) {
			f := m.foldersReady()
			f.editing = true
			f.in.Focus()
			return nil, true
		}
		return nil, false
	}
	return nil, false
}
