package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// hostUserList is the users of the host as the page shows them; a source that can be read wins.
func (m *Model) hostUserList() []hostadmin.User {
	if m.hostSrc != nil {
		if list, err := m.hostSrc.Users(); err == nil {
			return list
		}
	}
	return m.hst.users.all
}

// memberChoices are the users e offers for the group: those outside it to add, the listed members to remove. A
// user whose primary group it is is in neither, because that membership is not listed and not removable.
func (m *Model) memberChoices(g hostadmin.Group) (add, remove []string, system map[string]bool) {
	system = map[string]bool{}
	for _, u := range m.hostUserList() {
		system[u.Name] = u.System
		switch {
		case u.PrimaryGroup == g.Name:
		case slices.Contains(g.Members, u.Name):
			remove = append(remove, u.Name)
		default:
			add = append(add, u.Name)
		}
	}
	slices.Sort(add)
	slices.Sort(remove)
	return add, remove, system
}

// openGroupMembers opens the form that adds and removes members of the marked group in one step.
func (m *Model) openGroupMembers() tea.Cmd {
	g, ok := m.currentGroup()
	if !ok {
		return nil
	}
	add, remove, system := m.memberChoices(g)
	notes := func(names []string) []string {
		out := make([]string, len(names))
		for i, n := range names {
			if system[n] {
				out[i] = m.tr(tHostSystem)
			}
		}
		return out
	}
	f := &formDlg{title: m.tr(tHostMembersTitle, m.diffText(g.Name)), hint: m.tr(tHostMembersHint), saveText: "{save} continue"}
	switch g.Name {
	case hostadmin.GroupAdmin:
		f.hint = m.tr(tHostMembersSudoHint)
	case hostadmin.GroupDocker:
		f.hint += " " + m.tr(tHostPowerWarn)
	}
	f.rows = []formRow{
		pickRow("add", m.tr(tHostMembersAdd), add, notes(add)),
		pickRow("remove", m.tr(tHostMembersRemove), remove, notes(remove)),
	}
	f.submit = func(m *Model, f *formDlg) tea.Cmd {
		added, removed := f.row("add").picked(), f.row("remove").picked()
		if len(added)+len(removed) == 0 {
			f.row("add").problem = m.tr(tHostMembersTickOne)
			return nil
		}
		if msg := m.guardMembers(g, added, removed); msg != "" {
			f.notice = msg
			return nil
		}
		ask := hostAsk{id: hostchange.ActionGroupSetMembers, args: []string{g.Name}, risk: m.groupRisk(g),
			power: len(added) > 0 && (g.Name == hostadmin.GroupAdmin || g.Name == hostadmin.GroupDocker)}
		for _, n := range added {
			ask.args = append(ask.args, "+"+n)
		}
		enabled := map[string]bool{}
		for _, u := range m.hostUserList() {
			enabled[u.Name] = u.Enabled
		}
		for _, n := range removed {
			// A sudo member who is not in qsysmin has no qsysmin access to take; the root action cannot decide that
			// for itself, because the commands of a plan do not depend on the host.
			if g.Name == hostadmin.GroupAdmin && !enabled[n] {
				n += ":without-qsysmin"
			}
			ask.args = append(ask.args, "-"+n)
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
	m.openForm(f)
	return nil
}

// guardMembers gives the early message of the rules the root action enforces for taking administrators out of
// sudo or qsysmin: nobody does it to themselves, and an administrator with access to qsysmin remains.
func (m *Model) guardMembers(g hostadmin.Group, added, removed []string) string {
	if (g.Name != hostadmin.GroupAdmin && g.Name != hostadmin.GroupQsysmin) || len(removed) == 0 {
		return ""
	}
	uid := hostUID()
	for _, u := range m.hostUserList() {
		if uid != 0 && u.UID == uid && slices.Contains(removed, u.Name) {
			return m.tr(tHostSelfAdmin)
		}
	}
	for _, u := range m.hostUserList() {
		sudo, qsysmin := u.Admin, u.Enabled
		if slices.Contains(removed, u.Name) {
			qsysmin = false
			sudo = sudo && g.Name != hostadmin.GroupAdmin
		}
		if slices.Contains(added, u.Name) {
			sudo, qsysmin = sudo || g.Name == hostadmin.GroupAdmin, true
		}
		if sudo && qsysmin && (u.Expired == nil || !*u.Expired) {
			return ""
		}
	}
	return m.tr(tHostLastAdmin, m.diffText(strings.Join(removed, ", ")))
}
