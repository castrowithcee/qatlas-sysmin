package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// The command line (':') runs the interface's actions by name. It is a register, not a second set of keys:
// every command calls the function its key already calls and acts only where that key acts; anywhere else it
// names the reason and leaves the line open. A further command is one more entry of commands().

// command is one entry of the register.
type command struct {
	name    string
	desc    string
	aliases []string
	// hidden keeps the command out of the empty line; it still matches what is typed.
	hidden bool
	// shortcut returns the key that does the same now, or "" for a command without one.
	shortcut func() string
	// unavailable returns the reason the command cannot act in the present state, or "" where it can.
	unavailable func(m *Model) string
	run         func(m *Model) tea.Cmd
}

// reason is why c cannot run now, or "".
func (c command) reason(m *Model) string {
	if c.unavailable == nil {
		return ""
	}
	return c.unavailable(m)
}

// availability checks

// every is the availability of a command that needs all the checks to pass; the first reason wins.
func every(checks ...func(m *Model) string) func(m *Model) string {
	return func(m *Model) string {
		for _, check := range checks {
			if r := check(m); r != "" {
				return r
			}
		}
		return ""
	}
}

func inArea(a area) func(m *Model) string {
	return func(m *Model) string {
		if m.area != a {
			return tr(tCmdOnlyIn, a.title())
		}
		return ""
	}
}

// noFlow is the check for a command whose key the open guided setup would take for itself.
func noFlow(m *Model) string {
	if m.flowOpen() {
		return tr(tCmdFlowOpen, keyLabel(actBack))
	}
	return ""
}

// projectsIdle is the check for the list of Projects with no log or update over it.
func projectsIdle(m *Model) string {
	if m.prj != nil && (m.prj.log != nil || m.prj.upd != nil) {
		return tr(tCmdLevelOpen)
	}
	return ""
}

func projectSelected(m *Model) string {
	if m.prj == nil {
		return tr(tProjNoSel)
	}
	if _, ok := m.prj.current(); !ok {
		return tr(tProjNoSel)
	}
	return ""
}

// inContent is the check for a command whose key acts in the content only, not on the tab line or the page
// strip.
func inContent(m *Model) string {
	if z := m.zoneNow(); z != zoneContent && z != zoneDetail {
		return tr(tCmdFocusContent)
	}
	return ""
}

// commands is the register, in the order an empty line lists it.
func (m *Model) commands() []command {
	var list []command
	digits := func(a area) func() string { return func() string { return strconv.Itoa(int(a) + 1) } }
	for a := area(0); a < areaCount; a++ {
		title := strings.ToLower(a.key())
		list = append(list, command{
			name: "go " + title, desc: tr(tCmdGoArea, a.title()), aliases: []string{title},
			shortcut: digits(a), unavailable: noFlow,
			run: func(m *Model) tea.Cmd { return m.show(a, 0) },
		})
		for _, p := range areaPages(a) {
			list = append(list, command{
				name: "go " + title + " " + p.key, desc: tr(tCmdGoPage, p.title, a.title()),
				aliases: []string{title + " " + p.key}, hidden: true, unavailable: noFlow,
				run: func(m *Model) tea.Cmd { return m.show(a, p.sub) },
			})
		}
	}
	list = append(list, m.projectCommands()...)
	list = append(list,
		command{name: "discard all", desc: tr(tCmdDiscardAll), aliases: []string{"access discard-all"},
			unavailable: every(inArea(areaAccess), func(m *Model) string {
				if !m.acc.have {
					return tr(tCmdNotLoaded)
				}
				return ""
			}),
			run: func(m *Model) tea.Cmd { return m.discardAll() }},
		command{name: "admin session", desc: tr(tCmdAdmin), aliases: []string{"lock", "unlock"},
			shortcut: func() string { return keyLabel(actLock) },
			run:      func(m *Model) tea.Cmd { return m.lockKey() }},
		command{name: "setup", desc: tr(tCmdSetup),
			shortcut: func() string {
				if m.vaultState() == control.VaultAbsent {
					return keyLabel(actLock)
				}
				return ""
			},
			unavailable: func(m *Model) string {
				if s := m.vaultState(); s != "" && s != control.VaultAbsent {
					return tr(tErrExists)
				}
				return ""
			},
			run: func(m *Model) tea.Cmd { return m.lockKey() }},
		command{name: "reload", desc: tr(tCmdReload), aliases: []string{"refresh"},
			shortcut: func() string { return keyLabel(actReload) },
			unavailable: every(noFlow, func(m *Model) string {
				if _, ok := reloadPress(); !ok {
					return tr(tCmdNoReloadKey)
				}
				return ""
			}),
			run: func(m *Model) tea.Cmd {
				press, _ := reloadPress()
				return m.updateArea(press)
			}},
		command{name: "regroup", desc: tr(tCmdRegroup),
			run: func(m *Model) tea.Cmd { return m.regroup() }},
		command{name: "update", desc: tr(tCmdUpdate), aliases: []string{"upgrade"},
			shortcut: func() string { return keyLabel(actSelfUpdate) }, unavailable: noFlow,
			run: func(m *Model) tea.Cmd { return m.selfUpdate() }},
		command{name: "help", desc: tr(tCmdHelp),
			shortcut: func() string { return keyLabel(actHelp) },
			run:      func(m *Model) tea.Cmd { m.openPopup(popHelp); return nil }},
		command{name: "info", desc: tr(tCmdInfo),
			shortcut: func() string { return keyLabel(actInfo) },
			run:      func(m *Model) tea.Cmd { m.openPopup(popInfo); return nil }},
		command{name: "quit", desc: tr(tCmdQuit), aliases: []string{"exit"},
			shortcut: func() string { return keyLabel(actQuit) }, unavailable: noFlow,
			run: func(m *Model) tea.Cmd { return m.guarded(m.quit) }},
	)
	return list
}

// areaPage is one page of an area with its stable name and its title as drawn.
type areaPage struct {
	sub        int
	key, title string
}

// areaPages are the pages of an area; an area without a page strip has none.
func areaPages(a area) []areaPage {
	var out []areaPage
	switch a {
	case areaSettings:
		for sub := 0; sub < subCount; sub++ {
			out = append(out, areaPage{sub, subKey(sub), subTitle(sub)})
		}
	case areaActivity:
		for sub := 0; sub < actPages; sub++ {
			out = append(out, areaPage{sub, actKey(sub), actTitle(sub)})
		}
	case areaLibrary:
		for sub := 0; sub < libCount; sub++ {
			out = append(out, areaPage{sub, libKey(sub), libTitle(sub)})
		}
	}
	return out
}

// reloadPress is the key press of the first key bound to reload, so the command runs exactly what the key runs
// in the area shown.
func reloadPress() (tea.KeyPressMsg, bool) {
	for _, k := range keymap[actReload].Keys() {
		if name, ok := strings.CutPrefix(k, "ctrl+"); ok && len([]rune(name)) == 1 {
			return tea.KeyPressMsg{Code: []rune(name)[0], Mod: tea.ModCtrl}, true
		}
		if n, ok := strings.CutPrefix(k, "f"); ok {
			if i, err := strconv.Atoi(n); err == nil && i >= 1 && i <= 12 {
				return tea.KeyPressMsg{Code: tea.KeyF1 + rune(i-1)}, true
			}
		}
	}
	return tea.KeyPressMsg{}, false
}

// normalizeCommand is typed text in the form names are given: lower case, words separated by one space.
func normalizeCommand(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// matchCommands returns the commands matching query by name or alias: an exact name or alias first, then
// those that begin with it, then those with a word that does, then those with it in the middle of a word. Each
// group keeps the order of the register. An empty query lists the commands that are not hidden.
func matchCommands(list []command, query string) []command {
	query = normalizeCommand(query)
	if query == "" {
		var shown []command
		for _, c := range list {
			if !c.hidden {
				shown = append(shown, c)
			}
		}
		return shown
	}
	var groups [4][]command
	for _, c := range list {
		rank := 4
		for _, n := range append([]string{c.name}, c.aliases...) {
			switch {
			case n == query:
				rank = min(rank, 0)
			case strings.HasPrefix(n, query):
				rank = min(rank, 1)
			case strings.Contains(n, " "+query):
				rank = min(rank, 2)
			case strings.Contains(n, query):
				rank = min(rank, 3)
			}
		}
		if rank < 4 {
			groups[rank] = append(groups[rank], c)
		}
	}
	return append(append(append(groups[0], groups[1]...), groups[2]...), groups[3]...)
}

// commandKey handles a key of the open command line.
func (m *Model) commandKey(key tea.KeyPressMsg) tea.Cmd {
	p := m.pop
	matches := matchCommands(m.commands(), p.in.Value())
	switch {
	case keyIs(key, actBack):
		m.closePopup()
		return nil
	case keyIs(key, actUp) || keyIs(key, actPrev):
		p.cursor, p.msg = wrap(p.cursor-1, max(len(matches), 1)), ""
		return nil
	case keyIs(key, actDown) || keyIs(key, actNext):
		p.cursor, p.msg = wrap(p.cursor+1, max(len(matches), 1)), ""
		return nil
	case keyIs(key, actEnter):
		if len(matches) == 0 {
			p.msg = tr(tCmdUnknown, strings.Join(strings.Fields(p.in.Value()), " "))
			return nil
		}
		c := matches[min(p.cursor, len(matches)-1)]
		if reason := c.reason(m); reason != "" {
			p.msg = c.name + ": " + reason
			return nil
		}
		m.closePopup()
		return c.run(m)
	}
	before := p.in.Value()
	var cmd tea.Cmd
	p.in, cmd = p.in.Update(inputMsg(key))
	if p.in.Value() != before {
		p.cursor, p.msg = 0, ""
	}
	return cmd
}

// pickCommand is a click on the i-th match: it chooses the entry and runs it as enter does.
func (m *Model) pickCommand(i int) tea.Cmd {
	if m.pop == nil || m.pop.kind != popCommand {
		return nil
	}
	m.pop.cursor = i
	return m.commandKey(tea.KeyPressMsg{Code: tea.KeyEnter})
}

// paletteKeys is the key line of the command line.
const paletteKeys = "type to search · ↑/↓ or tab choose · enter run · {back} close"

// paletteMaxRows caps the entries shown at once; the list scrolls with the choice.
const paletteMaxRows = 12

// paletteBody is the command line as lines inner cells wide, at most room lines, and for each line the index
// of the match it shows (-1 for the other lines).
func (m *Model) paletteBody(inner, room int) ([]string, []int) {
	p := m.pop
	matches := matchCommands(m.commands(), p.in.Value())
	field := fitInput(p.in, inner-3)
	lines := []string{surfaceRow(" "+inputView(field), inner), ""}
	index := []int{-1, -1}
	var msg []string
	if p.msg != "" {
		msg = strings.Split(render(styles.fail, inner, tr(tNoteError, p.msg)), "\n")
	}
	switch {
	case len(matches) == 0 && p.msg != "":
	case len(matches) == 0:
		lines, index = append(lines, styles.hint.Render(truncateCells(tr(tCmdNoMatch), inner))), append(index, -1)
	default:
		avail := max(room-len(lines)-len(msg), 1)
		rows := min(avail, paletteMaxRows, len(matches))
		if len(matches) > rows && rows == avail && rows > 1 {
			// A list that goes on shows its position in a line of its own.
			rows--
		}
		cursor := min(p.cursor, len(matches)-1)
		start := max(min(cursor-rows/2, len(matches)-rows), 0)
		end := min(start+rows, len(matches))
		keyW, nameW := 0, 0
		for _, c := range matches[start:end] {
			if c.shortcut != nil {
				keyW = max(keyW, lipgloss.Width(c.shortcut()))
			}
			nameW = max(nameW, lipgloss.Width(c.name))
		}
		for i := start; i < end; i++ {
			lines, index = append(lines, m.paletteRow(matches[i], i == cursor, nameW, keyW, inner)), append(index, i)
		}
		if len(matches) > end-start {
			lines, index = append(lines, styles.hint.Render(truncateCells(tr(tCmdPosition, start+1, end, len(matches)), inner))), append(index, -1)
		}
	}
	for _, l := range msg {
		lines, index = append(lines, l), append(index, -1)
	}
	return lines, index
}

// paletteRow is one entry: marker, name and description at the left, the key at the right. A command that
// cannot run now is faint.
func (m *Model) paletteRow(c command, chosen bool, nameW, keyW, width int) string {
	right := ""
	if c.shortcut != nil {
		right = c.shortcut()
	}
	right = strings.Repeat(" ", max(keyW-lipgloss.Width(right), 0)) + right
	prefix := "  "
	if chosen {
		prefix = "> "
	}
	avail := max(width-lipgloss.Width(prefix)-keyW-1, 1)
	left := padLine(truncateCells(fmt.Sprintf("%-*s  %s", nameW, c.name, c.desc), avail), avail)
	switch {
	case c.reason(m) != "":
		left = styles.hint.Render(left)
	case chosen:
		left = styles.active.Render(left)
	}
	row := prefix + left + " " + styles.hint.Render(right)
	if chosen {
		return surfaceRow(row, width)
	}
	return row
}
