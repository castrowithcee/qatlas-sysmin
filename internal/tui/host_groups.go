package tui

import (
	"regexp"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// Groups are created, renamed and deleted from Host > Groups with n, r and d, and their members are changed with e. Each ends in the confirmation of
// hostchange; the root command checks it all again and stays the authority.

// groupNameForm is the format the root action accepts for a group name.
var groupNameForm = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// protectedGroup is a group that is neither renamed nor deleted: administrator rights and access to qsysmin
// rest on its name.
func protectedGroup(name string) bool {
	return name == hostadmin.GroupAdmin || name == hostadmin.GroupQsysmin
}

// currentGroup is the group marked on the Groups page.
func (m *Model) currentGroup() (hostadmin.Group, bool) {
	name, ok := m.hst.groups.page.current()
	g, known := m.hst.groups.items[name]
	return g, ok && known
}

func (m *Model) hostGroupExists(name string) bool {
	return slices.ContainsFunc(m.hst.groups.all, func(g hostadmin.Group) bool { return g.Name == name })
}

// primaryUsers names the users whose primary group is the group. A source that cannot be read names none; the
// root action refuses in any case.
func (m *Model) primaryUsers(group string) []string {
	users := m.hst.users.all
	if m.hostSrc != nil {
		if list, err := m.hostSrc.Users(); err == nil {
			users = list
		}
	}
	var names []string
	for _, u := range users {
		if u.PrimaryGroup == group {
			names = append(names, u.Name)
		}
	}
	return names
}

// groupRisk names why a change on the group needs a red confirmation: root, docker or a system group. It is
// empty for a regular group.
func (m *Model) groupRisk(g hostadmin.Group) string {
	var parts []string
	if g.Name == "root" {
		parts = append(parts, m.tr(tHostRiskGroupRoot))
	}
	if g.Name == hostadmin.GroupDocker {
		parts = append(parts, m.tr(tHostRiskGroupDocker))
	}
	if g.System {
		parts = append(parts, m.tr(tHostRiskGroup))
	}
	return strings.Join(parts, " ")
}

// hostGroupsKey handles n, e, r and d on the Groups page.
func (m *Model) hostGroupsKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case m.keyIs(key, actNew):
		return m.openGroupCreate(), true
	case m.keyIs(key, actEdit):
		return m.openGroupMembers(), true
	case m.keyIs(key, actRename):
		return m.openGroupRename(), true
	case m.keyIs(key, actDelete):
		return m.askGroupDelete(), true
	}
	return nil, false
}

// groupNameProblem checks a name typed into a form the way the root action does and says what is wrong.
func (m *Model) groupNameProblem(name string) string {
	switch {
	case name == "":
		return m.tr(tHostFormNameEmpty)
	case !groupNameForm.MatchString(name):
		return m.tr(tHostFormGroupBad)
	case m.hostGroupExists(name):
		return m.tr(tHostFormGroupTaken)
	}
	return ""
}

func (m *Model) openGroupCreate() tea.Cmd {
	f := &formDlg{title: m.tr(tHostGroupNewTitle), hint: m.tr(tHostGroupNewHint), saveText: answer(tHintSave, actSave)}
	f.rows = []formRow{textRow("name", m.tr(tColName), "")}
	f.submit = func(m *Model, f *formDlg) tea.Cmd {
		name := f.row("name").value()
		if f.row("name").problem = m.groupNameProblem(name); f.row("name").problem != "" {
			return nil
		}
		p, refusal := m.planHost(hostAsk{id: hostchange.ActionGroupCreate, args: []string{name}})
		if refusal != "" {
			f.notice = refusal
			return nil
		}
		m.closeForm()
		m.confirmHost(p)
		return nil
	}
	m.openForm(f)
	return nil
}

// refuseProtected shows why a protected group is left alone and reports whether it did.
func (m *Model) refuseProtected(g hostadmin.Group, why textID) bool {
	if !protectedGroup(g.Name) {
		return false
	}
	m.hostNotice(question{title: m.tr(tHostChangeRefused), consequence: m.tr(why, m.diffText(g.Name)), warn: true, keys: closeKeys()})
	return true
}

func (m *Model) openGroupRename() tea.Cmd {
	g, ok := m.currentGroup()
	if !ok || m.refuseProtected(g, tHostGroupKeepName) {
		return nil
	}
	f := &formDlg{title: m.tr(tHostGroupRenameTitle, m.diffText(g.Name)), hint: m.tr(tHostGroupRenameHint), saveText: answer(tHintContinue, actSave)}
	f.rows = []formRow{textRow("name", m.tr(tHostFormNewName), "")}
	f.submit = func(m *Model, f *formDlg) tea.Cmd {
		name := f.row("name").value()
		if f.row("name").problem = m.groupNameProblem(name); f.row("name").problem != "" {
			return nil
		}
		// A rename is always red: what addresses a group by its name keeps the old one.
		risk := strings.TrimSpace(m.tr(tHostRiskRenameGroup) + " " + m.groupRisk(g))
		p, refusal := m.planHost(hostAsk{id: hostchange.ActionGroupRename, args: []string{g.Name, name}, risk: risk})
		if refusal != "" {
			f.notice = refusal
			return nil
		}
		m.closeForm()
		m.confirmHost(p)
		return nil
	}
	m.openForm(f)
	return nil
}

func (m *Model) askGroupDelete() tea.Cmd {
	g, ok := m.currentGroup()
	if !ok || m.refuseProtected(g, tHostGroupKeepDelete) {
		return nil
	}
	if users := m.primaryUsers(g.Name); len(users) > 0 {
		m.hostNotice(question{title: m.tr(tHostChangeRefused), consequence: m.tr(tHostGroupPrimary, m.diffText(g.Name), m.diffText(strings.Join(users, ", "))),
			warn: true, keys: closeKeys()})
		return nil
	}
	p, refusal := m.planHost(hostAsk{id: hostchange.ActionGroupDelete, args: []string{g.Name}, risk: m.groupRisk(g)})
	if refusal != "" {
		m.hostNotice(question{title: m.tr(tHostChangeRefused), consequence: refusal, warn: true, keys: closeKeys()})
		return nil
	}
	m.confirmHost(p)
	return nil
}
