package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// projectPicker is the search list of a dialog: a finite choice such as the target version of an image or the
// backup to restore from. Typing is only a filter, the choice is always one of the values.
type projectPicker struct {
	title, note string
	list        filterList
	pick        func(value string) tea.Cmd
}

// Rows of values a picker shows at once and the cells of its search field.
const (
	pickerRows  = 8
	pickerField = 40
)

// openProjectPicker shows the picker over the area, the cursor on values[active].
func (m *Model) openProjectPicker(title, note string, values []string, active int, pick func(string) tea.Cmd) {
	pk := &projectPicker{title: title, note: note, pick: pick, list: newFilterList(func(v string) string { return v })}
	pk.list.reset(values)
	pk.list.cursor = min(max(active, 0), max(len(values)-1, 0))
	m.prj.dialog, m.screen = &projectDialog{kind: projDlgPick, picker: pk}, screenConfirm
	m.clearMessages()
	m.refreshPicker()
}

// projectsPickerEditing reports whether the filter of the open picker is being typed.
func (m *Model) projectsPickerEditing() bool {
	return m.projectsDialogOpen() && m.prj.dialog.picker != nil && m.prj.dialog.picker.list.editing
}

// refreshPicker draws the picker into the question the screen shows.
func (m *Model) refreshPicker() {
	pk := m.prj.dialog.picker
	l := &pk.list
	var lines []string
	l.input.Placeholder = tr(tLogSearchPlaceholder)
	switch {
	case l.editing:
		lines = append(lines, "  / "+inputView(fitInput(l.input, pickerField)))
	case l.query() != "":
		lines = append(lines, "  / "+truncateCells(l.query(), pickerField))
	}
	start, end := l.window(pickerRows, func(int) int { return 1 })
	for i := start; i < end; i++ {
		text := truncateCells(safeText(l.matches[i]), pickerField+8)
		if i == l.cursor {
			lines = append(lines, styles.title.Render("  > "+text))
		} else {
			lines = append(lines, "    "+text)
		}
	}
	if len(l.matches) == 0 {
		lines = append(lines, styles.hint.Render("    "+tr(tPickNoMatch)))
	} else {
		lines = append(lines, styles.hint.Render("    "+tr(tPickPosition, l.cursor+1, len(l.matches))))
	}
	keys := "↑/↓ move · / search · enter pick · {back} cancel"
	if l.editing {
		keys = "type to filter · ↑/↓ move · enter keep · {back} clear"
	}
	m.ask = &question{title: pk.title, consequence: pk.note, details: lines, keys: keys}
}

// pickerKey handles a key of the open picker.
func (m *Model) pickerKey(key tea.KeyPressMsg) tea.Cmd {
	pk := m.prj.dialog.picker
	l := &pk.list
	defer func() {
		if m.projectsDialogOpen() && m.prj.dialog.picker == pk {
			m.refreshPicker()
		}
	}()
	if l.editing {
		switch {
		case keyIs(key, actEnter):
			l.stopFilter()
		case keyIs(key, actBack):
			l.clearFilter()
		case keyIs(key, actUp):
			l.move(-1)
		case keyIs(key, actDown):
			l.move(1)
		default:
			return l.updateFilter(key)
		}
		return nil
	}
	switch {
	case keyIs(key, actBack):
		m.closeProjectDialog()
	case keyIs(key, actUp):
		l.move(-1)
	case keyIs(key, actDown):
		l.move(1)
	case pageKey(key):
		l.page(key, 0, pickerRows)
	case keyIs(key, actSearch), keyIs(key, actFilter):
		l.startFilter()
	case keyIs(key, actEnter):
		if v, ok := l.selected(); ok {
			m.closeProjectDialog()
			return pk.pick(v)
		}
	}
	return nil
}

// newestFirst is the values of a list the service gives oldest first, the newest at the top.
func newestFirst(values []string) []string {
	out := slices.Clone(values)
	slices.Reverse(out)
	return out
}

// projectRestore lists the backups of the project under the cursor to choose one from.
func (m *Model) projectRestore() tea.Cmd {
	if m.area != areaProjects || m.backend == nil {
		return nil
	}
	name, ok := m.selectedProject()
	if !ok {
		return nil
	}
	m.clearMessages()
	m.busy = tr(tBusyBackups, name)
	b := m.backend
	return func() tea.Msg {
		list, err := b.Backups(name)
		return projectBackupsMsg{name: name, list: list, err: err}
	}
}

// projectBackupsMsg is the outcome of a backup list read.
type projectBackupsMsg struct {
	name string
	list []string
	err  error
}

func (projectBackupsMsg) projectsAnswer() {}

func (m *Model) takeProjectBackups(msg projectBackupsMsg) {
	m.busy = ""
	switch {
	case msg.err != nil:
		m.fail = projectErrorText(msg.err)
	case m.screen != screenArea || m.area != areaProjects:
	case len(msg.list) == 0:
		m.status = tr(tProjNoBackups, msg.name)
	default:
		m.openProjectPicker(tr(tProjRestorePick, msg.name), tr(tProjRestorePickNote), newestFirst(msg.list), 0,
			func(backup string) tea.Cmd { m.askRestore(msg.name, backup); return nil })
	}
}

// askRestore asks before the data of a project is replaced by a backup.
func (m *Model) askRestore(name, backup string) {
	m.openProjectDialog(&projectDialog{kind: projDlgRestore, name: name, backup: backup}, question{
		title: tr(tProjRestoreTitle, name), consequence: tr(tProjRestoreNote), warn: true, danger: true,
		details: []string{"  " + styles.title.Render(truncateCells(safeText(backup), 56))}, keys: "y restore · n/{back} cancel"})
}

// restoreProject restores the project from the backup; it needs the admin session.
func (m *Model) restoreProject(name, backup string) tea.Cmd {
	var res control.DeployRestoreResult
	return m.requireAdmin(change{
		busy: tr(tBusyRestore, name),
		call: func(b Backend) error {
			var err error
			res, err = b.DeployRestore(name, backup)
			return err
		},
		done: func(m *Model, err error) tea.Cmd {
			m.busy = ""
			switch {
			case err != nil:
				m.fail = projectActionError(err)
			case res.Pending:
				m.status = tr(tProjRestorePending, name)
			case res.Run != nil:
				m.status = tr(tProjRunResult, name, wordText(res.Run.Result))
			default:
				m.status = tr(tProjRestoreNothing, name)
			}
			if m.prj.rows[name] == nil {
				return nil
			}
			return m.projectDataCmd(name)
		},
	})
}
