package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	kithelp "github.com/castrowithcee/qatlas-kit/helptopics"
	kittui "github.com/castrowithcee/qatlas-kit/tui"

	"github.com/castrowithcee/qatlas-sysmin/internal/helptopics"
)

// Help, info and the command line are parts of the kit over the screen they were opened from. None of them is
// reachable where text is typed, and esc closes each.

type popKind int

const (
	popHelp popKind = iota
	popInfo
	popCommand
)

// popup is the part open over the screen: exactly one of help, info and cmd.
type popup struct {
	help *kittui.Help
	info *kittui.Explanation
	cmd  *kittui.CommandLine
}

// globalKeys are the actions the help lists as acting on every screen.
func globalKeys() []action { return []action{actHelp, actCommand, actInfo, actSave, actBack, actQuit} }

// openPopup shows a part over the screen. The parts are built from what the screen shows now, before it is
// left; a part the kit refuses is a defect of this package and leaves the screen as it was with the reason.
func (m *Model) openPopup(kind popKind) {
	p := &popup{}
	var err error
	switch kind {
	case popHelp:
		p.help, err = kittui.NewHelp(kittui.HelpSpec{
			Topics: helptopics.Topics(m.ui.lang),
			Screen: slices.DeleteFunc(m.areaKeys(), func(h kittui.Hint) bool { return slices.Contains(globalKeys(), h.Action) }),
			Global: globalKeys(),
			Keys: kittui.HelpKeys{Left: actLeft, Right: actRight, Next: actTopicNext, Prev: actTopicPrev, Up: actUp,
				Down: actDown, PageUp: actPageUp, PageDown: actPageDown, First: actHome, Last: actEnd, Back: actBack,
				Help: actHelp},
		}, m.ui.keys, m.ui.text)
	case popInfo:
		p.info, err = kittui.NewExplanation(kittui.ExplanationSpec{
			Item: m.infoItem(),
			Keys: kittui.ExplanationKeys{Back: actBack, Info: actInfo, Copy: actCopy, Up: actUp, Down: actDown},
		}, m.ui.keys, m.ui.text)
	case popCommand:
		p.cmd, err = kittui.NewCommandLine(kittui.CommandLineSpec{Commands: m.commands(), Keys: kittui.CommandLineKeys{
			Up: actUp, Down: actDown, Next: actNext, Prev: actPrev, Enter: actEnter, Back: actBack}}, m.ui.keys, m.ui.text)
		if err == nil {
			p.cmd.SetContext(kittui.Context{Area: m.area.areaID(), Tab: m.activeTab(), Zone: m.zoneNow(), Focus: m.inputFocus()})
		}
	}
	m.clearMessages()
	if err != nil {
		m.fail = err.Error()
		return
	}
	m.pop, m.screen = p, screenPopup
}

func (m *Model) closePopup() { m.pop, m.screen = nil, m.home }

// updatePopup handles a key while a part is open. A key the part does not take is dropped, so it never reaches
// the screen behind it.
func (m *Model) updatePopup(key tea.KeyPressMsg) tea.Cmd {
	p := m.pop
	switch {
	case p.cmd != nil:
		if m.isAction(key) {
			// The command line has no call for an action; only the way out is one.
			if m.keyIs(key, actBack) {
				return m.commandDone(kittui.Command{}, true)
			}
			return nil
		}
		if text, ok := pasted(key); ok {
			key = textKey(text)
		}
		c, done := p.cmd.Update(key)
		return m.commandDone(c, done)
	case p.info != nil:
		act, _ := m.explanationKey(p.info, key)
		return m.infoAct(act)
	default:
		act, _ := m.helpKey(p.help, key)
		return m.helpAct(act)
	}
}

// helpAct runs what the help hands back: its keys close it.
func (m *Model) helpAct(act action) tea.Cmd {
	if act == actBack || act == actHelp {
		m.closePopup()
	}
	return nil
}

// infoAct runs what the info hands back: its keys close it, and the copy key hands its text to the terminal.
func (m *Model) infoAct(act action) tea.Cmd {
	switch act {
	case actBack, actInfo:
		m.closePopup()
	case actCopy:
		if text := m.copyClean(m.pop.info.Text()); text != "" {
			m.status = m.tr(tCopyHandedOver)
			return tea.SetClipboard(text)
		}
	}
	return nil
}

// clickPopup is a click on a target of the open part. A key of a key line that the part does not take itself
// is pressed as its key would be.
func (m *Model) clickPopup(res kittui.MouseResult) tea.Cmd {
	p := m.pop
	switch {
	case p.cmd != nil:
		if c, done := p.cmd.Click(res); done {
			return m.commandDone(c, true)
		}
	case p.info != nil:
		if act, handled := p.info.Click(res); handled {
			return m.infoAct(act)
		}
	default:
		if act, handled := p.help.Click(res); handled {
			if act == actBack || act == actHelp || act == "" {
				return m.helpAct(act)
			}
			return m.pressAction(act)
		}
	}
	if res.Action != "" {
		return m.pressAction(res.Action)
	}
	return nil
}

// wheelPopup is a notch of the wheel while a part is open: the help and the info scroll and stop at their
// ends, and the screen behind them never moves.
func (m *Model) wheelPopup(steps int) {
	switch p := m.pop; {
	case p.help != nil:
		p.help.Wheel(steps)
	case p.info != nil:
		p.info.Wheel(steps)
	}
}

// infoItem is the explanation of the entry under the focus, like a tooltip.
func (m *Model) infoItem() kittui.ExplanationItem {
	title, body := m.infoOf()
	item := kittui.ExplanationItem{Title: title, Body: body}
	if m.area == areaActivity {
		item.Term, _ = kithelp.LookupTerm(helptopics.Terms(m.ui.lang), helptopics.TermAuditLog)
	}
	return item
}

// infoOf is the title and the text of the entry under the focus.
func (m *Model) infoOf() (string, string) {
	pick := func(label string, id textID) (string, string) { return label, m.tr(id) }
	switch m.area {
	case areaOverview:
		if item := m.overviewCurrent(); item.info != "" {
			return pick(item.label, item.info)
		}
	case areaSettings:
		switch m.set.sub {
		case subGeneral:
			focused := m.set.gen.Focused()
			if a, ok := keyFieldAction(focused); ok {
				return pick(m.tr(keyLabelID(a)), tInfoSetKey)
			}
			info := map[kittui.FieldID][2]textID{
				fieldLanguage: {tLblLanguage, tInfoSetLanguage}, fieldTheme: {tLblTheme, tInfoSetTheme},
				fieldIcons: {tLblIcons, tInfoSetIcons}, fieldStart: {tLblStart, tInfoSetStart},
				fieldMouse: {tLblMouse, tInfoSetMouse},
			}[focused]
			return pick(m.tr(info[0]), info[1])
		default:
			return pick(subTitle(m.ui, subUpdate), tInfoSetUpdate)
		}
	case areaActivity:
		return pick(actTitle(m.ui, m.act.sub), []textID{tInfoActLog}[m.act.sub])
	case areaHost:
		return pick(hostTitle(m.ui, m.hst.sub), []textID{tInfoHostUsers, tInfoHostGroups, tInfoHostFolders, tInfoHostChanges}[m.hst.sub])
	}
	return m.area.title(m.ui), m.tr(tListEmpty)
}

// overPart is a part that stands over the screen where it fits and fills the workspace where it does not.
type overPart interface {
	Overlay(styles kittui.Styles, screen string, width, height int, room kittui.Rect) (string, []kittui.Target, bool)
	View(styles kittui.Styles, width, height int) (string, []kittui.Target)
	Hints() []kittui.Hint
}

// popupFrame draws the open part. The command line fills the workspace. The help and the info stand over the
// dimmed screen where the workspace has the room for them, and fill it otherwise, with their keys in the
// footer, whose keys then act on them.
func (m *Model) popupFrame() string {
	p := m.pop
	room := m.ui.newFrame().Workspace(m.frameSpec(""))
	if p.cmd != nil {
		m.keyHint(paletteKeys())
		room = m.ui.newFrame().Workspace(m.frameSpec(""))
		view, targets := p.cmd.View(m.ui.styles, room.Width, room.Height)
		frame := m.frame(view)
		m.addTargets(kittui.OffsetTargets(targets, room.X, room.Y))
		return frame
	}
	var part overPart = p.help
	if p.info != nil {
		part = p.info
	}
	var background string
	m.renderAs(m.baseScreen(), 0, func() {
		m.backdrop = true
		defer func() { m.backdrop = false }()
		background = m.frame(m.workspaceView())
	})
	if view, targets, ok := part.Overlay(m.ui.styles, background, m.termWidth, m.termHeight, room); ok {
		m.addTargets(targets)
		return view
	}
	m.keyHint(part.Hints())
	// The keys decide whether the footer wraps, so the room is asked again with them in place.
	room = m.ui.newFrame().Workspace(m.frameSpec(""))
	view, targets := part.View(m.ui.styles, room.Width, room.Height)
	frame := m.frame(view)
	m.addTargets(kittui.OffsetTargets(targets, room.X, room.Y))
	m.footerHits()
	return frame
}
