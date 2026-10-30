package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// overviewState is what the overview knows besides the status: the open approvals and the projects, which the
// service reports only for an unlocked vault.
type overviewState struct {
	cursor                      int
	approvals                   []control.ApprovalSummary
	projects                    []control.ProjectSummary
	haveApprovals, haveProjects bool
}

func (o *overviewState) clear() {
	o.approvals, o.projects, o.haveApprovals, o.haveProjects = nil, nil, false, false
}

// overviewMsg is the answer of the two reads of the overview; a failed read leaves its part without data.
type overviewMsg struct {
	approvals  []control.ApprovalSummary
	projects   []control.ProjectSummary
	aerr, perr error
}

// overviewCmd reads the approvals and the projects while the overview is shown and the vault is open.
func (m *Model) overviewCmd() tea.Cmd {
	if m.backend == nil || m.area != areaOverview || m.vaultState() != control.VaultUnlocked {
		return nil
	}
	b := m.backend
	return func() tea.Msg {
		a, aerr := b.Approvals()
		p, perr := b.Projects()
		return overviewMsg{approvals: a, projects: p, aerr: aerr, perr: perr}
	}
}

func (m *Model) takeOverview(msg overviewMsg) {
	o := &m.ovr
	o.approvals, o.haveApprovals = msg.approvals, msg.aerr == nil
	o.projects, o.haveProjects = msg.projects, msg.perr == nil
}

// ovRow is one line of the overview: a state with its next step. act is what enter does there, key how the
// footer names it; both are empty for a line that only informs.
type ovRow struct {
	id, label, value, next, key string
	kind                        statusKind
	act                         func() tea.Cmd
}

// target names a page the way the next steps do: "Settings › Vault".
func target(a area, sub int) string {
	if a == areaSettings {
		return a.title() + " › " + subTitle(sub)
	}
	return a.title()
}

// problemProjects counts the projects whose state is none the service reports as ordinary: approved, awaiting
// approval, or not comparable while the vault is locked. The service names no error state today, so the count
// stays 0 until it does.
func problemProjects(list []control.ProjectSummary) int {
	n := 0
	for _, p := range list {
		switch p.State {
		case control.StateApproved, control.StatePending, control.StateUnknown:
		default:
			n++
		}
	}
	return n
}

// overviewRows are the lines of the overview in the order they are drawn; empty while the service is unknown.
func (m *Model) overviewRows() []ovRow {
	if !m.svc.known {
		return nil
	}
	open := func(a area, sub int) (string, string, func() tea.Cmd) {
		return tr(tNextOpen, target(a, sub)), "enter open", func() tea.Cmd { return m.show(a, sub) }
	}
	if !m.serviceUp() && m.svc.noGroup {
		return []ovRow{{id: "service", label: tr(tOvService), value: tr(tValNoGroup), kind: statusFail,
			next: tr(tNextRegroup), key: "enter restart", act: m.regroup}}
	}
	if !m.serviceUp() {
		return []ovRow{{id: "service", label: tr(tOvService), value: tr(tValDown), kind: statusFail,
			next: tr(tNextRetry, keyName(keyOf(actReload))), key: "enter retry", act: m.statusCmd}}
	}
	st := m.svc.st
	var rows []ovRow
	vault := ovRow{id: "vault", label: tr(tOvVault), kind: statusOK, value: tr(tValOpen)}
	switch st.Vault {
	case control.VaultAbsent:
		vault.kind, vault.value, vault.next = statusWarn, tr(tValNotSetUp), tr(tNextSetup)
		vault.key, vault.act = "enter set up", func() tea.Cmd { m.openOverlay(entrySetup, 2, false, nil, false); return nil }
	case control.VaultLocked:
		vault.kind, vault.value, vault.next = statusWarn, tr(tValLocked), tr(tNextUnlock, keyLabel(actLock))
		vault.key, vault.act = "enter unlock", m.lockKey
	case control.VaultUnlocked:
	default:
		vault.kind, vault.value = statusWarn, tr(tValUnknown)
	}
	rows = append(rows, vault)

	version := ovRow{id: "version", label: tr(tOvVersion), kind: statusOK, value: st.Version}
	version.next, version.key, version.act = open(areaSettings, subUpdate)
	if latest := m.updateAvailable(); latest != "" {
		// The lookup compares with this program, which can differ from the service until it restarts.
		running := m.version
		if running == "" {
			running = st.Version
		}
		version.kind, version.value = statusWarn, running+" → "+latest
		version.next, version.key, version.act = tr(tUpdNext, latest, keyLabel(actSelfUpdate)), "enter update", m.selfUpdate
	}
	rows = append(rows, version)

	config := tr(tValUnknown)
	if st.ConfigDir != "" {
		config = st.ConfigDir
	}
	rows = append(rows, ovRow{id: "config", label: tr(tOvConfig), kind: statusOK, value: config})

	if st.Vault != control.VaultAbsent && st.Unlock != "" {
		unlock := ovRow{id: "unlock", label: tr(tOvUnlock), kind: statusOK, value: tr(tValManual)}
		switch st.Unlock {
		case control.UnlockTPM2, control.UnlockHost:
			unlock.kind, unlock.value = statusWarn, tr(tValAutoTPM)
			if st.Unlock == control.UnlockHost {
				unlock.value = tr(tValAutoHost)
			}
			unlock.next, unlock.key, unlock.act = open(areaSettings, subVault)
		case control.UnlockManual:
		default:
			unlock.kind, unlock.value = statusWarn, tr(tValUnknown)
		}
		rows = append(rows, unlock)
	}

	approvals := ovRow{id: "approvals", label: tr(tOvApprovals), kind: statusDim, value: tr(tValNoData)}
	if m.ovr.haveApprovals {
		approvals.kind, approvals.value = statusOK, tr(tValNoneOpen)
		if n := len(m.ovr.approvals); n > 0 {
			approvals.kind, approvals.value = statusWarn, tr(tValOpenCount, n)
			approvals.next, approvals.key, approvals.act = open(areaAccess, 0)
		}
	}
	rows = append(rows, approvals)

	projects := ovRow{id: "projects", label: tr(tOvProjects), kind: statusDim, value: tr(tValNoData)}
	if m.ovr.haveProjects {
		projects.kind, projects.value = statusOK, tr(tValProjectsTotal, len(m.ovr.projects))
		if n := problemProjects(m.ovr.projects); n > 0 {
			projects.kind, projects.value = statusWarn, tr(tValProjectsBad, n)
			projects.next, projects.key, projects.act = open(areaProjects, 0)
		}
	}
	rows = append(rows, projects)

	// The service does not report available project updates, so the line has no data to show.
	updates := ovRow{id: "updates", label: tr(tOvUpdates), kind: statusDim, value: tr(tValNoData)}
	updates.next, updates.key, updates.act = open(areaProjects, 0)
	rows = append(rows, updates)
	return rows
}

// overviewCurrent is the row under the cursor, the zero row when there is none.
func (m *Model) overviewCurrent() ovRow {
	rows := m.overviewRows()
	if len(rows) == 0 {
		return ovRow{}
	}
	return rows[min(max(m.ovr.cursor, 0), len(rows)-1)]
}

// overviewKey handles a key of the overview: up and down move over the lines and stop at the ends, up on the
// first line goes to the tab line, enter takes the step. It reports false for a key it leaves to the area.
func (m *Model) overviewKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	rows := m.overviewRows()
	switch {
	case keyIs(key, actUp) && m.ovr.cursor <= 0:
		m.toBar()
	case keyIs(key, actUp):
		m.ovr.cursor--
	case keyIs(key, actDown):
		m.ovr.cursor = max(min(m.ovr.cursor+1, len(rows)-1), 0)
	case keyIs(key, actHome):
		m.ovr.cursor = 0
	case keyIs(key, actEnd):
		m.ovr.cursor = max(len(rows)-1, 0)
	case keyIs(key, actEnter):
		if act := m.overviewCurrent().act; act != nil {
			return act(), true
		}
	default:
		return nil, false
	}
	return nil, true
}

// overviewBody is the state of the service and the vault, line by line, each with its next step.
func (m *Model) overviewBody() string {
	rows := m.overviewRows()
	if len(rows) == 0 {
		return m.wrapped(styles.hint, tr(tChecking))
	}
	label := 0
	for _, r := range rows {
		label = max(label, len([]rune(r.label)))
	}
	m.ovr.cursor = min(max(m.ovr.cursor, 0), len(rows)-1)
	y := m.workspaceTop() + 2
	var lines []string
	for i, r := range rows {
		text := statusRow(r.kind, padLine(r.label, label)+"  "+r.value)
		block := m.row(i == m.ovr.cursor, text)
		if r.next != "" {
			block += "\n" + m.indented(r.next)
		}
		height := strings.Count(block, "\n") + 1
		i := i
		m.addHit(0, y, m.usable(0), y+height, func() tea.Cmd {
			if m.ovr.cursor != i {
				m.ovr.cursor = i
				return nil
			}
			return m.contentPress("enter")
		})
		y += height
		lines = append(lines, block)
	}
	return strings.Join(lines, "\n")
}

// statusRow leads a line with a mark whose shape tells the kind as well as its colour: a dot for what is well,
// a warning sign, a cross for what failed, a ring for what has no data.
func statusRow(kind statusKind, text string) string {
	glyph, style := "●", styles.ok
	switch kind {
	case statusWarn:
		glyph, style = warnMark, styles.warning
	case statusFail:
		glyph, style = "✗", styles.fail
	case statusDim:
		glyph, style = "○", styles.hint
	}
	return style.Render(glyph) + " " + text
}
