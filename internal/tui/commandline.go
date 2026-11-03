package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// The command line (':') runs the interface's actions by name. It is a register, not a second set of keys:
// every command names the action, area or page its key or tab click already opens, runs through the same
// function and acts only where that key acts; anywhere else the kit names the reason and leaves the line open.

// actRegroup is the action of the command that restarts the interface under the group qsysmin. No key carries
// it; the next step of the overview leads there too.
const actRegroup action = "regroup"

// commands is the register, in the order an empty line lists it: every area by its digit, the pages of the
// areas, which only typing finds, and the actions.
func (m *Model) commands() []kittui.Command {
	var list []kittui.Command
	for i, a := range frameAreas() {
		id := string(a.ID)
		list = append(list, kittui.Command{Name: "go " + id, Aliases: []string{id}, Description: cmdGoID(a.ID, ""),
			Action: actArea, Key: i + 1, Area: a.ID, Available: kittui.Always})
		for _, t := range a.Tabs {
			list = append(list, kittui.Command{Name: "go " + id + " " + string(t.ID), Aliases: []string{id + " " + string(t.ID)},
				Description: cmdGoID(a.ID, t.ID), Area: a.ID, Tab: t.ID, Hidden: true, Available: kittui.Always})
		}
	}
	return append(list,
		kittui.Command{Name: "reload", Aliases: []string{"refresh"}, Description: tCmdReload, Action: actReload,
			Available: func(kittui.Context) (bool, string) {
				if _, ok := m.ui.reloadPress(); !ok {
					return false, m.tr(tCmdNoReloadKey)
				}
				return true, ""
			}},
		kittui.Command{Name: "regroup", Description: tCmdRegroup, Action: actRegroup, Available: kittui.Always},
		kittui.Command{Name: "update", Aliases: []string{"upgrade"}, Description: tCmdUpdate, Action: actSelfUpdate,
			Available: kittui.Always},
		kittui.Command{Name: "help", Description: tCmdHelp, Action: actHelp, Available: kittui.Always},
		kittui.Command{Name: "info", Description: tCmdInfo, Action: actInfo, Available: kittui.Always},
		kittui.Command{Name: "quit", Aliases: []string{"exit"}, Description: tCmdQuit, Action: actQuit,
			Available: kittui.Always},
	)
}

// commandDone runs the command the command line closed with; the zero command closes it without effect.
func (m *Model) commandDone(c kittui.Command, done bool) tea.Cmd {
	if !done {
		return nil
	}
	m.closePopup()
	if c.Area != "" {
		a, _ := areaOf(c.Area)
		sub := 0
		for i, t := range frameAreas()[a].Tabs {
			if t.ID == c.Tab {
				sub = i
			}
		}
		return m.show(a, sub)
	}
	switch c.Action {
	case actReload:
		press, _ := m.ui.reloadPress()
		return m.updateArea(press)
	case actRegroup:
		return m.regroup()
	case actSelfUpdate:
		return m.selfUpdate()
	case actHelp:
		m.openPopup(popHelp)
	case actInfo:
		m.openPopup(popInfo)
	case actQuit:
		return m.guarded(m.quit)
	}
	return nil
}

// cmdGoID is the catalog key of the description of the command that shows an area, or a page of it.
func cmdGoID(a kittui.AreaID, t kittui.TabID) textID {
	if t == "" {
		return textID("cmd.go." + string(a))
	}
	return textID("cmd.go." + string(a) + "." + string(t))
}

// commandTexts adds the descriptions of the commands that show an area or a page to a catalog of one
// language, built from the titles it holds, so they follow every title.
func commandTexts(texts map[textID]string) {
	for _, a := range frameAreas() {
		texts[cmdGoID(a.ID, "")] = fmt.Sprintf(texts[tCmdGoArea], texts[a.Title])
		for _, t := range a.Tabs {
			texts[cmdGoID(a.ID, t.ID)] = fmt.Sprintf(texts[tCmdGoPage], texts[t.Title], texts[a.Title])
		}
	}
}

// paletteKeys is the key line of the command line.
func paletteKeys() []kittui.Hint {
	return keyLine(hint(tHintTypeToSearch), navHint(tHintChoose, actUp, actDown, actNext), hint(tHintRun, actEnter),
		hint(tHintClose, actBack))
}
