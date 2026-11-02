package tui

import (
	"errors"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// A folder is changed from Host > Folders with e. The form offers owner and group from the accounts of the
// host and the three levels per position, never free text; a recursive change is counted first, and a system
// path is confirmed in red before the acknowledgement the root action demands is passed.

// folderLevels are the levels a position can get.
var folderLevels = []hostadmin.Access{hostadmin.AccessNothing, hostadmin.AccessRead, hostadmin.AccessReadWrite}

// countEntries counts what a change of path touches; a test stands in for the file system.
func (m *Model) countEntries(path string, recursive bool) (int, error) {
	if m.folderCount != nil {
		return m.folderCount(path, recursive)
	}
	return hostchange.CountFolderEntries(path, recursive)
}

// changeableFolder is the folder e changes: a folder that was read, not a file.
func (m *Model) changeableFolder() (hostadmin.Folder, bool) {
	f := m.foldersReady().folder
	if f == nil || f.Kind != "directory" {
		return hostadmin.Folder{}, false
	}
	return *f, true
}

// realPath is the path the change is made on: the one without links, as the root action demands.
func realPath(f hostadmin.Folder) string {
	if f.Resolved != "" {
		return f.Resolved
	}
	return f.Path
}

// accountChoices are the names to choose from, with current kept in the list so that saving never changes it
// by accident.
func accountChoices(names []string, current string) (values []string, sel int) {
	values = slices.Clone(names)
	slices.Sort(values)
	if !slices.Contains(values, current) {
		values = append([]string{current}, values...)
	}
	return values, slices.Index(values, current)
}

func (m *Model) hostAccountNames() (users, groups []string) {
	if m.hostSrc == nil {
		return nil, nil
	}
	if list, err := m.hostSrc.Users(); err == nil {
		for _, u := range list {
			users = append(users, u.Name)
		}
	}
	if list, err := m.hostSrc.Groups(); err == nil {
		for _, g := range list {
			groups = append(groups, g.Name)
		}
	}
	return users, groups
}

func levelRow(key, label string, current hostadmin.Access) formRow {
	values := make([]string, len(folderLevels))
	shown := make([]string, len(folderLevels))
	sel := 0
	for i, l := range folderLevels {
		values[i], shown[i] = string(l), hostAccessText(l)
		if l == current {
			sel = i
		}
	}
	return choiceRow(key, label, values, shown, sel)
}

// openFolderChange opens the form that changes the folder shown.
func (m *Model) openFolderChange() tea.Cmd {
	folder, ok := m.changeableFolder()
	if !ok {
		m.status = tr(tHostFolderOnlyDir)
		return nil
	}
	path := realPath(folder)
	users, groups := m.hostAccountNames()
	owners, ownerSel := accountChoices(users, folder.Owner)
	groupNames, groupSel := accountChoices(groups, folder.Group)
	f := &formDlg{title: tr(tHostFolderChangeTitle, m.diffText(path)), saveText: "{save} save"}
	f.rows = []formRow{
		choiceRow("owner", tr(tHostFolderOwner), owners, nil, ownerSel),
		choiceRow("group", tr(tHostFolderGroup), groupNames, nil, groupSel),
		levelRow("user", tr(tHostFolderOwnerMay), folder.OwnerAcc),
		levelRow("groupacc", tr(tHostFolderGroupMay), folder.GroupAcc),
		levelRow("other", tr(tHostFolderOthersMay), folder.OthersAcc),
		choiceRow("recursive", tr(tHostFolderBelow), []string{hostNo, hostYes}, []string{tr(tHostNo), tr(tHostYes)}, 0),
	}
	if hostchange.IsSystemPath(path) {
		f.hint = tr(tHostFolderSystemHint)
	}
	f.submit = func(m *Model, f *formDlg) tea.Cmd { return m.submitFolderChange(f, path) }
	m.openForm(f)
	return nil
}

func levelOf(f *formDlg, key string) hostadmin.Access { return hostadmin.Access(f.row(key).value()) }

func (m *Model) submitFolderChange(f *formDlg, path string) tea.Cmd {
	req := hostchange.FolderRequest{
		Path: path, Owner: f.row("owner").value(), Group: f.row("group").value(),
		UserAccess: levelOf(f, "user"), GroupAccess: levelOf(f, "groupacc"), OtherAccess: levelOf(f, "other"),
		Recursive:  f.row("recursive").value() == hostYes,
		SystemPath: hostchange.IsSystemPath(path),
	}
	count := 0
	if req.Recursive {
		n, err := m.countEntries(path, true)
		switch {
		case errors.Is(err, hostchange.ErrTooManyEntries):
			f.notice = tr(tHostFolderTooMany, m.diffText(path), hostchange.MaxFolderEntries)
			return nil
		case err != nil:
			f.notice = tr(tHostFolderCountFailed, m.diffText(err.Error()))
			return nil
		}
		count = n
	}
	// The acknowledgement of a system path is part of the arguments the confirmation runs on y; the red
	// confirmation below is the only way to reach that run.
	p, refusal := m.planHost(hostAsk{id: hostchange.ActionFolderSetPermissions, args: hostchange.FolderArgs(req)})
	if refusal != "" {
		f.notice = refusal
		return nil
	}
	m.closeForm()
	m.confirmHost(p)
	if req.Recursive {
		m.ask.consequence = tr(tHostFolderCount, count) + " " + m.ask.consequence
	}
	if req.SystemPath {
		m.ask.warn, m.ask.danger = true, true
		m.ask.consequence = tr(tHostFolderSystemWarn) + " " + m.ask.consequence
	}
	return nil
}
