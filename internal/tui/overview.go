package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The Overview: key figures as cards and the next steps. The cards and steps are two focus zones; hints for
// the focused entry stand in the footer only.

// ovZone is the part of the Overview that takes the keys.
type ovZone int

const (
	ovCards ovZone = iota
	ovSteps
)

const (
	ovCardRows = 3 // a card: frame and one line
	// ovCardMin is the narrowest a card gets; below four of them in a row the cards become one text line.
	ovCardMin = 8
)

// overviewState is what the Overview knows besides the status: the open approvals, which the service reports
// only for an unlocked vault, and where the focus is.
type overviewState struct {
	zone          ovZone
	cursor        [2]int
	approvals     []control.ApprovalSummary
	haveApprovals bool
}

func (o *overviewState) clear() { o.approvals, o.haveApprovals = nil, false }

// reset puts the focus on the first next step, the thing to do first.
func (o *overviewState) reset() { o.zone, o.cursor = ovSteps, [2]int{} }

// overviewMsg is the answer of the read of the overview; a failed read leaves it without data.
type overviewMsg struct {
	approvals []control.ApprovalSummary
	aerr      error
}

// overviewCmd reads the approvals while the overview is shown and the vault is open.
func (m *Model) overviewCmd() tea.Cmd {
	if m.backend == nil || m.area != areaOverview || m.vaultState() != control.VaultUnlocked {
		return nil
	}
	b := m.backend
	return func() tea.Msg {
		a, aerr := b.Approvals()
		return overviewMsg{approvals: a, aerr: aerr}
	}
}

func (m *Model) takeOverview(msg overviewMsg) {
	o := &m.ovr
	o.approvals, o.haveApprovals = msg.approvals, msg.aerr == nil
}

// ovItem is a card or a next step. act is what enter does there and key how the footer names it; both are
// empty for a step that only informs.
type ovItem struct {
	label, value, key string
	kind              statusKind
	info              textID
	act               func() tea.Cmd
}

// noData is the dimmed word a card shows without figures: the vault is locked or there is nothing.
func (m *Model) noData() string {
	if m.vaultState() == control.VaultLocked {
		return tr(tValLocked)
	}
	return tr(tOvNone)
}

// overviewCards are the key figures; without data each shows the word of noData.
func (m *Model) overviewCards() []ovItem {
	o := &m.ovr
	figure := func(label string, have bool, n int, warn statusKind, info textID, act func() tea.Cmd) ovItem {
		c := ovItem{label: label, kind: statusDim, value: m.noData(), key: "enter open", info: info, act: act}
		if have {
			c.kind, c.value = statusOK, fmt.Sprint(n)
			if n > 0 {
				c.kind = warn
			}
		}
		return c
	}
	return []ovItem{
		figure(tr(tOvApprovals), o.haveApprovals, len(o.approvals), statusWarn, tInfoOvApprovals,
			func() tea.Cmd { return m.show(areaAccess, 0) }),
	}
}

// overviewSteps are the next steps in the order of the table of states, never empty once the service is known.
func (m *Model) overviewSteps() []ovItem {
	if !m.svc.known {
		return nil
	}
	step := func(kind statusKind, label, key string, act func() tea.Cmd) ovItem {
		return ovItem{label: label, kind: kind, key: key, act: act, info: tInfoOvSteps}
	}
	if !m.serviceUp() {
		if m.svc.noGroup {
			return []ovItem{step(statusFail, tr(tOvRegroup), "enter restart", m.regroup)}
		}
		retry := step(statusFail, tr(tOvRetry), "enter retry", m.statusCmd)
		if m.svc.foreign {
			// The error carries the way out; it must stay readable.
			retry.label += " · " + oneLine(m.svc.fail)
		}
		return []ovItem{retry}
	}
	st, o := m.svc.st, &m.ovr
	var steps []ovItem
	switch st.Vault {
	case control.VaultAbsent:
		steps = append(steps, step(statusWarn, tr(tOvSetup), "enter set up",
			func() tea.Cmd { m.openOverlay(entrySetup, 2, false, nil, false); return nil }))
	case control.VaultLocked:
		steps = append(steps, step(statusWarn, tr(tOvUnlockVault), "enter unlock", m.lockKey))
	}
	if len(o.approvals) > 0 {
		steps = append(steps, step(statusWarn, tr(tOvStepApprovals), "enter open",
			func() tea.Cmd { return m.show(areaAccess, 0) }))
	}
	if st.Vault != control.VaultAbsent && (st.Unlock == control.UnlockTPM2 || st.Unlock == control.UnlockHost) {
		steps = append(steps, step(statusWarn, tr(tOvStepAuto), "enter open",
			func() tea.Cmd { return m.show(areaSettings, subVault) }))
	}
	if latest := m.updateAvailable(); latest != "" {
		steps = append(steps, step(statusWarn, tr(tOvInstall, latest), "enter update", m.selfUpdate))
	}
	if len(steps) == 0 {
		steps = append(steps, ovItem{label: tr(tOvNothing), kind: statusDim, info: tInfoOvSteps})
	}
	return steps
}

// ovPlan says what the Overview shows at the room it has.
type ovPlan struct {
	blank   bool // a blank line under the title
	line    bool // the cards as one text line
	version bool
	framed  bool // the steps in a panel
	steps   int  // steps shown
}

// stepTextWidth is the room of a step's text: the width less the frame, the bar and the mark.
func stepTextWidth(width int, framed bool) int {
	if framed {
		return width - 8
	}
	return width - 4
}

// stepPieces are the lines of every step wrapped to width cells.
func stepPieces(steps []ovItem, width int) [][]string {
	out := make([][]string, len(steps))
	for i, s := range steps {
		out[i] = strings.Split(render(lipgloss.NewStyle(), width, s.label), "\n")
	}
	return out
}

// overviewPlan fits the parts into room lines. A short terminal gives up, in this order: the cards (they
// become a text line), the version line, the frame of the steps. The title and the steps stay.
func overviewPlan(room, width int, steps []ovItem) ovPlan {
	p := ovPlan{line: width < 4*ovCardMin+3, version: true, framed: true, steps: len(steps)}
	cost := func() int {
		n := 1
		if p.blank {
			n++
		}
		n += ovCardRows
		if p.line {
			n = n - ovCardRows + 1
		}
		if p.version {
			n++
		}
		frame := 1 // the heading
		if p.framed {
			frame = 2
		}
		for _, piece := range stepPieces(steps[:p.steps], stepTextWidth(width, p.framed)) {
			n += len(piece)
		}
		n += frame
		return n
	}
	fits := func() bool { return cost() <= room }
	if !fits() {
		p.line = true
	}
	if !fits() {
		p.version = false
	}
	if !fits() {
		p.framed = false
	}
	for p.steps > 1 && !fits() {
		p.steps--
	}
	p.blank = true
	if !fits() {
		p.blank = false
	}
	return p
}

// zones are the zones the plan shows, in the order tab visits them.
func (p ovPlan) zones() []ovZone { return []ovZone{ovCards, ovSteps} }

// overviewNow is the data and the plan of the Overview as it is drawn now; the steps are empty while the
// service is unknown.
func (m *Model) overviewNow() (cards, steps []ovItem, p ovPlan) {
	cards, steps = m.overviewCards(), m.overviewSteps()
	if len(steps) > 0 {
		p = overviewPlan(m.height, m.usable(0), steps)
	}
	return
}

// overviewCurrent is the card or step under the cursor, as an item; the zero item while the service is
// unknown.
func (m *Model) overviewCurrent() ovItem {
	cards, steps, _ := m.overviewNow()
	if len(steps) == 0 {
		return ovItem{}
	}
	z, i := m.ovr.zone, m.ovr.cursor
	switch z {
	case ovCards:
		return cards[min(max(i[z], 0), len(cards)-1)]
	}
	return steps[min(max(i[z], 0), len(steps)-1)]
}

// overviewKey handles a key of the overview: up and down move inside a zone and stop at its ends (left and
// right too among the cards), up on the first entry goes to the tab line, tab and shift+tab go through the
// zones and out to the tab line, enter takes the entry. It reports false for a key it leaves to the area.
func (m *Model) overviewKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	cards, steps, p := m.overviewNow()
	if len(steps) == 0 {
		return nil, false
	}
	z := m.ovr.zone
	n := [2]int{len(cards), len(steps)}[z]
	c := &m.ovr.cursor[z]
	zones := p.zones()
	at := slices.Index(zones, z)
	switch {
	case keyIs(key, actUp) && *c <= 0:
		m.toBar()
	case keyIs(key, actUp), keyIs(key, actLeft) && z == ovCards:
		*c = max(*c-1, 0)
	case keyIs(key, actDown), keyIs(key, actRight) && z == ovCards:
		*c = max(min(*c+1, n-1), 0)
	case keyIs(key, actHome):
		*c = 0
	case keyIs(key, actEnd):
		*c = max(n-1, 0)
	case keyIs(key, actNext):
		if at+1 >= len(zones) {
			m.toBar()
		} else {
			m.ovr.zone = zones[at+1]
		}
	case keyIs(key, actPrev):
		if at == 0 {
			m.toBar()
		} else {
			m.ovr.zone = zones[at-1]
		}
	case keyIs(key, actEnter):
		if act := m.overviewCurrent().act; act != nil {
			return act(), true
		}
	default:
		return nil, false
	}
	return nil, true
}

// ovDate is the date of the header in the language of the interface.
func ovDate(t time.Time) string {
	t = t.Local()
	days, months := strings.Split(tr(tOvWeekdays), ","), strings.Split(tr(tOvMonths), ",")
	return tr(tOvDate, days[t.Weekday()], t.Day(), months[t.Month()-1], t.Year())
}

// ovMark is the leading mark of a step: its shape tells the kind as well as its colour.
func ovMark(kind statusKind) string {
	switch kind {
	case statusWarn:
		return styles.warning.Render(warnMark)
	case statusFail:
		return styles.fail.Render("✗")
	case statusDim:
		return styles.hint.Render("○")
	}
	return styles.ok.Render("●")
}

// ovLine is one entry line of width cells; the selected one carries the bar, which is ">" without colour.
func ovLine(text string, selected bool, width int) string {
	room := max(width-2, 0)
	if !selected {
		return "  " + clipCells(text, room)
	}
	return styles.selMark.Render(selectionMark()) + styles.activeRow.Render(keepStyle(" "+padLine(clipCells(text, room), room), styles.activeRow))
}

// ovValueStyle is the style of the figure on a card.
func ovValueStyle(k statusKind) lipgloss.Style {
	switch k {
	case statusDim:
		return styles.hint
	case statusWarn:
		return styles.warning
	case statusFail:
		return styles.fail
	}
	return styles.title
}

// overviewBody draws the Overview and registers its click targets. A click marks the entry and acts like
// enter on it.
func (m *Model) overviewBody() string {
	cards, steps, p := m.overviewNow()
	if len(steps) == 0 {
		return m.wrapped(styles.hint, tr(tChecking))
	}
	o := &m.ovr
	width := m.usable(0)
	focus := m.zoneNow() == zoneContent && m.screen == screenArea
	zone := m.ovr.zone
	for z, n := range [2]int{len(cards), len(steps)} {
		o.cursor[z] = min(max(o.cursor[z], 0), max(n-1, 0))
	}
	sel := func(z ovZone, i int) bool { return focus && zone == z && o.cursor[z] == i }
	top := m.workspaceTop()
	var lines []string
	// hit makes the lines [row, row+rows) of the body, in the cells [x0, x1), the entry i of zone z.
	hit := func(row, rows, x0, x1 int, z ovZone, i int, act func() tea.Cmd) {
		m.addHit(x0, top+row, x1, top+row+rows, func() tea.Cmd {
			o.zone, o.cursor[z], m.zone = z, i, zoneContent
			if act == nil {
				return nil
			}
			return act()
		})
	}

	// wheel lets the wheel step the mark of zone z over the lines [row, row+rows) of the body, among the n
	// entries shown, without giving the zone the focus.
	wheel := func(row, rows, n int, z ovZone) {
		m.addWheel(0, top+row, width, top+row+rows, func(up bool) tea.Cmd {
			wheelCursor(&o.cursor[z], n, up)
			return nil
		})
	}

	lines = append(lines, titleLine(m.area.title(), ovDate(m.timeNow()), width))
	if p.blank {
		lines = append(lines, "")
	}

	cardsRow := len(lines)
	if p.line {
		var parts []string
		x := 0
		for i, c := range cards {
			text := c.label + " " + ovValueStyle(c.kind).Render(c.value)
			if sel(ovCards, i) {
				text = styles.activeRow.Render(keepStyle("> "+c.label+" "+c.value, styles.activeRow))
			}
			w := lipgloss.Width(text)
			hit(len(lines), 1, x, x+w, ovCards, i, c.act)
			parts = append(parts, text)
			x += w + 2
		}
		lines = append(lines, clipCells(strings.Join(parts, "  "), width))
	} else {
		space := width - (len(cards) - 1)
		var panels [][]string
		x := 0
		for i, c := range cards {
			w := space / len(cards)
			if i < space%len(cards) {
				w++
			}
			inner, label := w-4, c.label
			if sel(ovCards, i) {
				label = ">" + label
				if lipgloss.Width(label)+2 <= inner {
					label = "> " + c.label
				}
			}
			value := ovValueStyle(c.kind).Render(truncateCells(c.value, inner))
			panels = append(panels, panel(truncateCells(label, inner), []string{value}, inner, 1, panelOpts{focused: sel(ovCards, i)}))
			hit(len(lines), ovCardRows, x, x+w, ovCards, i, c.act)
			x += w + 1
		}
		for r := 0; r < ovCardRows; r++ {
			row := make([]string, len(panels))
			for i := range panels {
				row[i] = panels[i][r]
			}
			lines = append(lines, strings.Join(row, " "))
		}
	}

	wheel(cardsRow, len(lines)-cardsRow, len(cards), ovCards)

	if p.version {
		lines = append(lines, m.overviewVersionLine(width))
	}

	// A step that needs several lines is one entry. The first line of the body follows the border or heading.
	inner := width
	if p.framed {
		inner = width - 4
	}
	var body []string
	first := len(lines) + 1
	for i, piece := range stepPieces(steps[:p.steps], stepTextWidth(width, p.framed)) {
		for j, text := range piece {
			lead := "  "
			if j == 0 {
				lead = ovMark(steps[i].kind) + " "
			}
			body = append(body, ovLine(lead+text, sel(ovSteps, i), inner))
		}
		hit(first+len(body)-len(piece), len(piece), 0, width, ovSteps, i, steps[i].act)
	}
	stepsRow := len(lines)
	if p.framed {
		lines = append(lines, panel(tr(tOvSteps), body, inner, len(body), panelOpts{focused: focus && zone == ovSteps})...)
	} else {
		lines = append(lines, styles.title.Render(tr(tOvSteps)))
		lines = append(lines, body...)
	}
	wheel(stepsRow, len(lines)-stepsRow, min(len(steps), p.steps), ovSteps)

	return strings.Join(lines, "\n")
}

// overviewVersionLine is the running version, with the newer release where there is one, and the config path
// cut at its start to the room left.
func (m *Model) overviewVersionLine(width int) string {
	version := m.version
	if version == "" {
		version = m.svc.st.Version
	}
	name := strings.TrimSpace("sysmin " + version)
	text, plain := styles.hint.Render(name), name
	if latest := m.updateAvailable(); latest != "" {
		text += styles.warning.Render(" → " + latest)
		plain += " → " + latest
	}
	if path := m.svc.st.ConfigDir; path != "" {
		if room := width - lipgloss.Width(plain) - 2; room >= 8 {
			text += "  " + styles.hint.Render(truncateLeft(path, room))
		}
	}
	return clipCells(text, width)
}

// statusRow leads a line with a mark whose shape tells the kind as well as its colour: a dot for what is well,
// a warning sign, a cross for what failed, a ring for what has no data.
func statusRow(kind statusKind, text string) string { return ovMark(kind) + " " + text }
