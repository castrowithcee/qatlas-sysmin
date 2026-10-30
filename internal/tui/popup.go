package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	// in is the command line; msg says why the last command did not run; cursor is the match chosen.
	in     textinput.Model
	msg    string
	cursor int
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
	{"secrets", tHelpCredentials},
	{"channels", tHelpChannels},
	{"templates", tHelpTemplates},
	{"audit-log", tHelpAuditLog},
	{"runs", tHelpRuns},
}

// openPopup shows a panel over the area screen.
// commandPrompt is the symbol that opens the input line of the command line; it is not the key that opens the
// line, which may be rebound.
const commandPrompt = ": "

func (m *Model) openPopup(kind popKind) {
	p := &popup{kind: kind}
	if kind == popCommand {
		p.in = newTextInput()
		p.in.Prompt = commandPrompt
		p.in.Placeholder = tr(tCmdPlaceholder)
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
	if m.flowOpen() {
		return m.flowInfo()
	}
	switch m.area {
	case areaOverview:
		if item := m.overviewCurrent(); item.info != "" {
			return pick(item.label, item.info)
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
	case areaLibrary:
		return pick(libTitle(m.lib.sub), []textID{tInfoLibCredentials, tInfoLibChannels, tInfoLibTemplates}[m.lib.sub])
	case areaActivity:
		return pick(actTitle(m.act.sub), []textID{tInfoActLog, tInfoActRuns}[m.act.sub])
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

// popupKeys is the key line of a panel.
func popupKeys(kind popKind) string {
	switch kind {
	case popHelp:
		return "↑/↓ scroll · ←/→ topic · {back} close"
	case popInfo:
		return "{back} close"
	}
	return paletteKeys
}

// popupContent is the title and the lines of a panel of the help or the info. The command line draws itself
// through paletteBody.
func (m *Model) popupContent(inner int) (title string, body []string) {
	switch m.pop.kind {
	case popHelp:
		var ids []string
		for _, t := range helpTopics {
			ids = append(ids, t.id)
		}
		// Topics before the first one that fits are left out, so the strip always shows the topic open.
		first, strip := 0, ""
		for ; first <= m.pop.topic; first++ {
			strip, _ = tabStrip(ids[first:], m.pop.topic-first, true)
			if lipgloss.Width(strip) <= inner {
				break
			}
		}
		return tr(tHelpTitle), append([]string{truncateCells(strip, inner), ""}, m.helpBody(inner)...)
	}
	label, text := m.infoOf()
	return tr(tInfoTitle), append([]string{styles.title.Render(label), ""}, strings.Split(render(lipgloss.NewStyle(), inner, text), "\n")...)
}

// popupFrame draws the panel: centred over the dimmed screen where the terminal has the room for it, and in
// place of the screen where it has not.
func (m *Model) popupFrame() string {
	modal := m.termWidth >= modalMinWidth && m.termHeight >= modalMinHeight
	inner := m.usable(0)
	if modal {
		inner = min(modalMaxInner, m.termWidth-8)
	}
	keys := popupKeys(m.pop.kind)
	keyLines := m.keyHintLinesWith(keys, keyHintGroupFoot)
	room := m.height - 2
	if modal {
		room = m.termHeight - 4 - 2 - 1 - len(keyLines)
	}
	var title string
	var body []string
	var index []int
	if m.pop.kind == popCommand {
		title = tr(tCmdTitle)
		body, index = m.paletteBody(inner, room)
	} else {
		title, body = m.popupContent(inner)
		body = m.scrolled(body, room, inner)
	}
	if !modal {
		m.keyHint(keys)
		top := m.workspaceTop() + 2
		for i, idx := range index {
			if idx >= 0 {
				m.addHit(0, top+i, inner, top+i+1, func() tea.Cmd { return m.pickCommand(idx) })
			}
		}
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
	top, left := dialogOrigin(dialog, m.termWidth, m.termHeight)
	for i, idx := range index {
		if idx >= 0 {
			m.addHit(left+2, top+3+i, left+2+inner, top+4+i, func() tea.Cmd { return m.pickCommand(idx) })
		}
	}
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
