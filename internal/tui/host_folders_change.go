package tui

import (
	"errors"
	"slices"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// A folder is changed from Host > Folders with e. The form offers owner and group from the accounts of the
// host and the three levels per position, never free text; a recursive change is counted first, and a system
// path is confirmed in red before the acknowledgement the root action demands is passed.

// folderLevels() are the levels a position can get.
func folderLevels() []hostadmin.Access {
	return []hostadmin.Access{hostadmin.AccessNothing, hostadmin.AccessRead, hostadmin.AccessReadWrite}
}

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

// The fields of the folder form.
const (
	fieldOwner     kittui.FieldID = "owner"
	fieldGroup     kittui.FieldID = "group"
	fieldUserAcc   kittui.FieldID = "user"
	fieldGroupAcc  kittui.FieldID = "groupacc"
	fieldOtherAcc  kittui.FieldID = "other"
	fieldRecursive kittui.FieldID = "recursive"
)

// levelField is the choice of the level of one position, on current.
func levelField(id kittui.FieldID, label textID, current hostadmin.Access) kittui.FormField {
	choices := make([]kittui.Choice, len(folderLevels()))
	sel := 0
	for i, l := range folderLevels() {
		choices[i] = kittui.Choice{ID: kittui.ChoiceID(l), Label: hostAccessID(l)}
		if l == current {
			sel = i
		}
	}
	return choiceField(id, label, choices, sel)
}

// accountField is the choice among account names, shown as they are.
func (m *Model) accountField(id kittui.FieldID, label textID, names []string, sel int) kittui.FormField {
	choices := make([]kittui.Choice, len(names))
	for i, n := range names {
		choices[i] = kittui.Choice{ID: kittui.ChoiceID(n), Label: literal(m.diffText(n))}
	}
	return choiceField(id, label, choices, sel)
}

// openFolderChange opens the form that changes the folder shown.
func (m *Model) openFolderChange() tea.Cmd {
	folder, ok := m.changeableFolder()
	if !ok {
		m.status = m.tr(tHostFolderOnlyDir)
		return nil
	}
	path := realPath(folder)
	users, groups := m.hostAccountNames()
	owners, ownerSel := accountChoices(users, folder.Owner)
	groupNames, groupSel := accountChoices(groups, folder.Group)
	f := &formDlg{title: m.tr(tHostFolderChangeTitle, m.diffText(path)), saveText: answer(tHintSave, actSave)}
	f.fields = []kittui.FormField{
		m.accountField(fieldOwner, tHostFolderOwner, owners, ownerSel),
		m.accountField(fieldGroup, tHostFolderGroup, groupNames, groupSel),
		levelField(fieldUserAcc, tHostFolderOwnerMay, folder.OwnerAcc),
		levelField(fieldGroupAcc, tHostFolderGroupMay, folder.GroupAcc),
		levelField(fieldOtherAcc, tHostFolderOthersMay, folder.OthersAcc),
		choiceField(fieldRecursive, tHostFolderBelow, yesNo(), 0),
	}
	if hostchange.IsSystemPath(path) {
		f.hint = m.tr(tHostFolderSystemHint)
	}
	f.submit = func(m *Model, f *formDlg) tea.Cmd { return m.submitFolderChange(f, path) }
	m.openForm(f)
	return nil
}

func levelOf(f *formDlg, id kittui.FieldID) hostadmin.Access { return hostadmin.Access(f.value(id)) }

func (m *Model) submitFolderChange(f *formDlg, path string) tea.Cmd {
	req := hostchange.FolderRequest{
		Path: path, Owner: f.value(fieldOwner), Group: f.value(fieldGroup),
		UserAccess: levelOf(f, fieldUserAcc), GroupAccess: levelOf(f, fieldGroupAcc), OtherAccess: levelOf(f, fieldOtherAcc),
		Recursive:  f.value(fieldRecursive) == hostYes,
		SystemPath: hostchange.IsSystemPath(path),
	}
	count := 0
	if req.Recursive {
		n, err := m.countEntries(path, true)
		switch {
		case errors.Is(err, hostchange.ErrTooManyEntries):
			f.refuse(m.tr(tHostFolderTooMany, m.diffText(path), hostchange.MaxFolderEntries))
			return nil
		case err != nil:
			f.refuse(m.tr(tHostFolderCountFailed, m.diffText(err.Error())))
			return nil
		}
		count = n
	}
	// The acknowledgement of a system path is part of the arguments the confirmation runs on y; the red
	// confirmation below is the only way to reach that run.
	p, refusal := m.planHost(hostAsk{id: hostchange.ActionFolderSetPermissions, args: hostchange.FolderArgs(req)})
	if refusal != "" {
		f.refuse(refusal)
		return nil
	}
	m.closeForm()
	m.confirmHost(p)
	if req.Recursive {
		m.ask.consequence = m.tr(tHostFolderCount, count) + " " + m.ask.consequence
	}
	if req.SystemPath {
		m.ask.warn, m.ask.danger = true, true
		m.ask.consequence = m.tr(tHostFolderSystemWarn) + " " + m.ask.consequence
	}
	return nil
}
