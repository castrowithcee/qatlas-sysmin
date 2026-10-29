package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// Help, info and the command line stand as a panel over the screen they were opened from. None of them is
// reachable where text is typed, and esc closes each.

type popKind int

const (
	popHelp popKind = iota
	popInfo
	popCommand
)

// popup is the state of the panel over the screen.
type popup struct {
	kind popKind
	// topic and scroll are the help topic shown and its first line.
	topic, scroll int
	// in is the command line; err says why the last command failed; pick cycles through the completions.
	in   textinput.Model
	err  string
	pick int
}

// helpTopics are the topics of the help by their stable identifiers, which name the technical terms and so are
// the same in every language. keys is built from the screen the help was opened from.
var helpTopics = []struct {
	id   string
	text textID
}{
	{"keys", ""},
	{"vault", tHelpVault},
	{"admin-session", tHelpAdmin},
	{"approvals", tHelpApprovals},
	{"unlock", tHelpUnlock},
}

// openPopup shows a panel over the area screen.
func (m *Model) openPopup(kind popKind) {
	p := &popup{kind: kind}
	if kind == popCommand {
		p.in = newTextInput()
		p.in.Prompt = ": "
		p.in.Focus()
	}
	m.pop, m.screen = p, screenPopup
	m.clearMessages()
}

func (m *Model) closePopup() { m.pop, m.screen = nil, m.home }

// updatePopup handles a key while a panel is open.
func (m *Model) updatePopup(key tea.KeyPressMsg) tea.Cmd {
	p := m.pop
	switch p.kind {
	case popHelp:
		switch {
		case keyIs(key, actBack), keyIs(key, actHelp):
			m.closePopup()
		case keyIs(key, actRight), keyIs(key, actNext):
			p.topic, p.scroll = (p.topic+1)%len(helpTopics), 0
		case keyIs(key, actLeft), keyIs(key, actPrev):
			p.topic, p.scroll = (p.topic+len(helpTopics)-1)%len(helpTopics), 0
		case keyIs(key, actUp):
			p.scroll = max(p.scroll-1, 0)
		case keyIs(key, actDown):
			p.scroll++
		case keyIs(key, actPageUp):
			p.scroll = max(p.scroll-5, 0)
		case keyIs(key, actPageDown):
			p.scroll += 5
		case keyIs(key, actHome):
			p.scroll = 0
		}
	case popInfo:
		if keyIs(key, actBack) || keyIs(key, actEnter) || m.infoKey(key) {
			m.closePopup()
		}
	case popCommand:
		return m.commandKey(key)
	}
	return nil
}

// infoOf is the title and the text of the entry under the focus, like a tooltip.
func (m *Model) infoOf() (string, string) {
	pick := func(label string, id textID) (string, string) { return label, tr(id) }
	switch m.area {
	case areaOverview:
		row := m.overviewCurrent()
		ids := map[string]textID{"service": tInfoOvService, "vault": tInfoOvVault, "version": tInfoOvVersion,
			"config": tInfoOvConfig, "unlock": tInfoOvUnlock, "approvals": tInfoOvApprovals,
			"projects": tInfoOvProjects, "updates": tInfoOvUpdates}
		if id, ok := ids[row.id]; ok {
			return pick(row.label, id)
		}
	case areaSettings:
		switch m.set.sub {
		case subGeneral:
			g := m.set.gen
			ids := []textID{tInfoSetLanguage, tInfoSetTheme, tInfoSetMouse}
			labels := []textID{tLblLanguage, tLblTheme, tLblMouse, tLblKeySave, tLblKeyHelp, tLblKeyCommand, tLblKeyLock, tLblKeyInfo}
			if g.focus < len(ids) {
				return pick(tr(labels[g.focus]), ids[g.focus])
			}
			return pick(tr(labels[g.focus]), tInfoSetKey)
		case subVault:
			ids := []textID{tInfoSetPassphrase, tInfoSetTimeout, tInfoSetUnlock}
			labels := []textID{tLblPassphrase, tLblAdminTimeout, tOvUnlock}
			f := m.set.vault.focus
			return pick(tr(labels[f]), ids[f])
		default:
			return pick(subTitle(subUpdate), tInfoSetUpdate)
		}
	default:
		ids := map[area]textID{areaProjects: tInfoAreaProjects, areaActivity: tInfoAreaActivity, areaAccess: tInfoAreaAccess,
			areaLibrary: tInfoAreaLibrary}
		if id, ok := ids[m.area]; ok {
			return pick(m.area.title(), id)
		}
	}
	return m.area.title(), tr(tListEmpty)
}

// helpBody is the text of the help topic shown, wrapped into width cells.
func (m *Model) helpBody(width int) []string {
	topic := helpTopics[m.pop.topic]
	var lines []string
	if topic.id == "keys" {
		everywhere := []string{"{help} help", "{command} cmds", "{info} info", "{lock} admin", "{save} save", "{back} back", "q quit"}
		lines = append(lines, styles.title.Render(tr(tHelpKeysScreen)))
		for _, g := range strings.Split(m.areaKeys(), " · ") {
			if !slices.Contains(everywhere, g) && !strings.HasPrefix(g, "{lock}") {
				lines = append(lines, "  "+truncateCells(footerKeys(g), width-2))
			}
		}
		lines = append(lines, "", styles.title.Render(tr(tHelpKeysEverywhere)))
		for _, g := range everywhere {
			lines = append(lines, "  "+truncateCells(footerKeys(g), width-2))
		}
		return lines
	}
	return strings.Split(render(lipgloss.NewStyle(), width, tr(topic.text)), "\n")
}

// popupContent is the title, the lines and the key line of the panel.
func (m *Model) popupContent(inner int) (title string, body []string, keys string) {
	p := m.pop
	switch p.kind {
	case popHelp:
		var ids []string
		for _, t := range helpTopics {
			ids = append(ids, t.id)
		}
		strip, _ := tabStrip(ids, p.topic, true)
		return tr(tHelpTitle), append([]string{truncateCells(strip, inner), ""}, m.helpBody(inner)...),
			"up/down scroll · left/right topic · {back} close"
	case popInfo:
		label, text := m.infoOf()
		return tr(tInfoTitle), append([]string{styles.title.Render(label), ""}, strings.Split(render(lipgloss.NewStyle(), inner, text), "\n")...),
			"{back} close"
	}
	body = []string{inputView(fitInput(p.in, inner-2)), ""}
	if p.err != "" {
		body = append(body, strings.Split(render(styles.fail, inner, tr(tNoteError, p.err)), "\n")...)
	} else {
		for _, name := range matchCommands(p.in.Value()) {
			body = append(body, styles.hint.Render("  "+name))
		}
	}
	return tr(tCmdTitle), body, "enter run · tab complete · {back} close"
}

// popupFrame draws the panel: centred over the dimmed screen where the terminal has the room for it, and in
// place of the screen where it has not.
func (m *Model) popupFrame() string {
	modal := m.termWidth >= modalMinWidth && m.termHeight >= modalMinHeight
	inner := m.usable(0)
	if modal {
		inner = min(modalMaxInner, m.termWidth-8)
	}
	title, body, keys := m.popupContent(inner)
	keyLines := m.keyHintLinesWith(keys, keyHintGroupFoot)
	room := m.height - 2
	if modal {
		room = m.termHeight - 4 - 2 - 1 - len(keyLines)
	}
	body = m.scrolled(body, room, inner)
	if !modal {
		m.keyHint(keys)
		return m.frame(m.boundView(styles.title.Render(title) + "\n\n" + strings.Join(body, "\n")))
	}
	lines := append([]string{styles.title.Render(title), ""}, body...)
	lines = append(lines, "")
	lines = append(lines, keyLines...)
	dialog := panel("", lines, inner, len(lines), panelOpts{focused: true})
	var background string
	m.renderAs(m.baseScreen(), 0, func() {
		m.backdrop = true
		defer func() { m.backdrop = false }()
		background = m.frame(m.workspaceView())
	})
	return overlay(background, dialog, m.termWidth, m.termHeight)
}

// scrolled cuts a help topic to the room it has, from the scroll position of the panel. A topic that does not
// fit gets a position line.
func (m *Model) scrolled(body []string, room, width int) []string {
	p := m.pop
	if len(body) <= max(room, 1) {
		p.scroll = 0
		return body
	}
	room = max(room-1, 1)
	p.scroll = min(max(p.scroll, 0), len(body)-room)
	out := slices.Clone(body[p.scroll : p.scroll+room])
	return append(out, styles.hint.Render(truncateCells(tr(tScrollPosition, p.scroll+1, p.scroll+room, len(body)), width)))
}

// commandEntry is one command: its name, which is not translated, and what it does.
type commandEntry struct {
	name string
	run  func(m *Model) tea.Cmd
}

// commands are the commands of the command line: the same actions the keys reach.
func commands() []commandEntry {
	var list []commandEntry
	for a := area(0); a < areaCount; a++ {
		list = append(list, commandEntry{strings.ToLower(a.key()), func(m *Model) tea.Cmd { return m.setArea(a) }})
		if a == areaSettings {
			for sub := 0; sub < subCount; sub++ {
				list = append(list, commandEntry{"settings " + subKey(sub), func(m *Model) tea.Cmd { return m.show(areaSettings, sub) }})
			}
		}
	}
	return append(list,
		commandEntry{"lock", func(m *Model) tea.Cmd { return m.lockKey() }},
		commandEntry{"setup", func(m *Model) tea.Cmd {
			if m.vaultState() != "" && m.vaultState() != control.VaultAbsent {
				m.fail = tr(tErrExists)
				return nil
			}
			return m.lockKey()
		}},
		commandEntry{"reload", func(m *Model) tea.Cmd { return m.statusCmd() }},
		commandEntry{"help", func(m *Model) tea.Cmd { m.openPopup(popHelp); return nil }},
		commandEntry{"info", func(m *Model) tea.Cmd { m.openPopup(popInfo); return nil }},
		commandEntry{"quit", func(m *Model) tea.Cmd { return m.guarded(m.quit) }},
	)
}

// normalizeCommand is a typed command in the form the table names it.
func normalizeCommand(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// matchCommands are the commands the typed text begins, at most six.
func matchCommands(text string) []string {
	text = normalizeCommand(text)
	var out []string
	for _, c := range commands() {
		if strings.HasPrefix(c.name, text) {
			out = append(out, c.name)
		}
	}
	return out[:min(len(out), 6)]
}

// commandKey handles a key of the command line: enter runs, tab completes, esc closes.
func (m *Model) commandKey(key tea.KeyPressMsg) tea.Cmd {
	p := m.pop
	switch {
	case keyIs(key, actBack):
		m.closePopup()
	case keyIs(key, actEnter):
		name := normalizeCommand(p.in.Value())
		for _, c := range commands() {
			if c.name == name {
				m.closePopup()
				return c.run(m)
			}
		}
		p.err = tr(tCmdUnknown)
	case keyIs(key, actNext):
		if all := matchCommands(p.in.Value()); len(all) > 0 {
			p.in.SetValue(all[p.pick%len(all)])
			p.in.CursorEnd()
			p.pick++
		}
	default:
		p.err, p.pick = "", 0
		var cmd tea.Cmd
		p.in, cmd = p.in.Update(inputMsg(key))
		return cmd
	}
	return nil
}
