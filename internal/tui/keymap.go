package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// action names a key-bound action of the editor. Dispatch and footers refer to the action, never to the
// key, so the binding lives in exactly one place. The IDs of the rebindable actions are the names under keys
// in tui.yaml.
type action = kittui.ActionID

const (
	actInfo      action = "info"
	actSave      action = "save"
	actHelp      action = "help"
	actCommand   action = "command"
	actBack      action = "back"
	actDelete    action = "delete"
	actForceQuit action = "forcequit"

	actUp       action = "up"
	actDown     action = "down"
	actLeft     action = "left"
	actRight    action = "right"
	actHome     action = "home"
	actEnd      action = "end"
	actPageUp   action = "pageup"
	actPageDown action = "pagedown"
	actNext     action = "next"
	actPrev     action = "prev"
	actEnter    action = "enter"
	actSpace    action = "space"
	actSearch   action = "search"
	actQuit     action = "quit"

	actVimUp    action = "vimup"
	actVimDown  action = "vimdown"
	actVimLeft  action = "vimleft"
	actVimRight action = "vimright"

	actTopicNext action = "topicnext"
	actTopicPrev action = "topicprev"

	actYes     action = "yes"
	actNo      action = "no"
	actDiscard action = "discard"

	actCopy       action = "copy"
	actNew        action = "new"
	actEdit       action = "edit"
	actRename     action = "rename"
	actFilter     action = "filter"
	actTest       action = "test"
	actSelfUpdate action = "selfupdate"

	actSuggestPrev action = "suggestprev"
	actSuggestNext action = "suggestnext"
	actReload      action = "reload"
)

// keyTable is the key table: every action with its level, default keys, and the catalog text of its help.
// Keys are the names bubbletea reports (tea.KeyPressMsg.String). The vim letters are entries of their own
// that only moveKey reads: on a screen that reads text they are text, so the movement bindings hold arrows
// only. The help level holds the keys only the help overlay knows; tab there steps through topics, not fields.
func keyTable() []kittui.Action {
	act := func(id action, level kittui.KeyLevel, help textID, keys ...string) kittui.Action {
		return kittui.Action{ID: id, Level: level, DefaultKeys: keys, Help: help}
	}
	rebind := func(a kittui.Action, r kittui.Rebinding) kittui.Action { a.Rebind = r; return a }
	second := func(a kittui.Action, of action) kittui.Action { a.SecondWayOf = of; return a }
	return []kittui.Action{
		rebind(act(actSave, kittui.LevelGlobal, tKeyHelpSave, "f2"), kittui.RebindReplace),
		rebind(act(actHelp, kittui.LevelGlobal, tKeyHelpHelp, "?"), kittui.RebindReplace),
		rebind(act(actCommand, kittui.LevelGlobal, tKeyHelpCommand, ":"), kittui.RebindReplace),
		act(actBack, kittui.LevelGlobal, tKeyHelpBack, "esc"),
		act(actForceQuit, kittui.LevelGlobal, tKeyHelpForceQuit, "ctrl+c"),

		// The letter actions: each letter means one thing on every screen where nothing is typed. Info keeps
		// its letter; the extra key of the preferences joins it and also works on text lines.
		rebind(act(actInfo, kittui.LevelLetter, tKeyHelpInfo, "i"), kittui.RebindExtra),
		act(actDelete, kittui.LevelLetter, tKeyHelpDelete, "d"),
		act(actQuit, kittui.LevelLetter, tKeyHelpQuit, "q"),
		act(actCopy, kittui.LevelLetter, tKeyHelpCopy, "c"),
		act(actNew, kittui.LevelLetter, tKeyHelpNew, "n"),
		act(actEdit, kittui.LevelLetter, tKeyHelpEdit, "e"),
		act(actRename, kittui.LevelLetter, tKeyHelpRename, "r"),
		act(actFilter, kittui.LevelLetter, tKeyHelpFilter, "f"),
		act(actTest, kittui.LevelLetter, tKeyHelpTest, "t"),
		act(actSelfUpdate, kittui.LevelLetter, tKeyHelpSelfUpdate, "U"),

		// The control keys of text lines and of the target picker. The suggestion keys are the second way of
		// the arrows, for a terminal where a multiplexer does not take them.
		second(act(actSuggestPrev, kittui.LevelCtrl, tKeyHelpSuggestPrev, "ctrl+p"), actUp),
		second(act(actSuggestNext, kittui.LevelCtrl, tKeyHelpSuggestNext, "ctrl+n"), actDown),
		act(actReload, kittui.LevelCtrl, tKeyHelpReload, "ctrl+r"),

		act(actUp, kittui.LevelMove, tKeyHelpUp, "up"),
		act(actDown, kittui.LevelMove, tKeyHelpDown, "down"),
		act(actLeft, kittui.LevelMove, tKeyHelpLeft, "left"),
		act(actRight, kittui.LevelMove, tKeyHelpRight, "right"),
		act(actHome, kittui.LevelMove, tKeyHelpHome, "home"),
		act(actEnd, kittui.LevelMove, tKeyHelpEnd, "end"),
		act(actPageUp, kittui.LevelMove, tKeyHelpPageUp, "pgup"),
		act(actPageDown, kittui.LevelMove, tKeyHelpPageDown, "pgdown"),
		act(actNext, kittui.LevelMove, tKeyHelpNext, "tab"),
		act(actPrev, kittui.LevelMove, tKeyHelpPrev, "shift+tab"),
		act(actEnter, kittui.LevelMove, tKeyHelpEnter, "enter"),
		act(actSpace, kittui.LevelMove, tKeyHelpSpace, "space"),
		act(actSearch, kittui.LevelMove, tKeyHelpSearch, "/"),

		act(actVimUp, kittui.LevelVim, tKeyHelpUp, "k"),
		act(actVimDown, kittui.LevelVim, tKeyHelpDown, "j"),
		act(actVimLeft, kittui.LevelVim, tKeyHelpLeft, "h"),
		act(actVimRight, kittui.LevelVim, tKeyHelpRight, "l"),

		act(actTopicNext, kittui.LevelHelp, tKeyHelpTopicNext, "tab"),
		act(actTopicPrev, kittui.LevelHelp, tKeyHelpTopicPrev, "shift+tab"),

		act(actYes, kittui.LevelConfirm, tKeyHelpYes, "y"),
		act(actNo, kittui.LevelConfirm, tKeyHelpNo, "n"),
		act(actDiscard, kittui.LevelConfirm, tKeyHelpDiscard, "d"),
	}
}

// rebindable are the actions whose keys the preferences hold, in the order Settings shows them.
func rebindable() []action { return []action{actSave, actHelp, actCommand, actInfo} }

// bindKeys makes the keys of the preferences the keys in force. An entry the key table refuses keeps the
// standard key of its action with a warning and leaves the preferences, so Settings shows the key that acts.
func (m *Model) bindKeys() []string {
	usable, refused := m.ui.keys.Resolve(m.prefs.Keys)
	var warnings []string
	for _, err := range refused {
		warnings = append(warnings, fmt.Sprintf("keys.%s %v, using %s", err.Action, err, m.standardKey(err.Action)))
	}
	if err := m.ui.keys.Bind(usable); err != nil {
		warnings = append(warnings, fmt.Sprintf("keys: %v, using the standard keys", err))
		usable = nil
	}
	m.prefs.Keys = usable
	return warnings
}

// standardKey names the key an action of the preferences has by default: its own key, or no extra key for
// the info action, which keeps its letter.
func (m *Model) standardKey(a action) string {
	if key := m.prefKey(nil, a); key != "" {
		return key
	}
	return "no extra key"
}

// prefKey is the key of a rebindable action under the bindings keys: the binding, or else the default key of
// an action whose key a binding replaces, and none for the extra info key.
func (m *Model) prefKey(keys map[action]string, a action) string {
	if key, ok := keys[a]; ok {
		return key
	}
	for _, t := range m.ui.keys.Actions() {
		if t.ID == a && t.Rebind == kittui.RebindReplace {
			return t.DefaultKeys[0]
		}
	}
	return ""
}

// inputFocus is what the screen shown does with a key press: where text is typed, a key that types a
// character is text.
func (m *Model) inputFocus() kittui.InputFocus {
	if m.typing() {
		return kittui.InputText
	}
	return kittui.InputNavigation
}

// keyIs reports whether the key press triggers the action, whatever the screen reads. A handler that runs
// while text is typed asks it only for keys that type nothing, or asks keyIsTyped.
func (m *Model) keyIs(msg tea.KeyPressMsg, a action) bool {
	return m.ui.keys.Is(msg, a, kittui.InputNavigation)
}

// keyIsTyped reports whether the key press triggers the action on a text line: only the keys of the action
// that type no character count, such as the extra info key.
func (m *Model) keyIsTyped(msg tea.KeyPressMsg, a action) bool {
	return m.ui.keys.Is(msg, a, kittui.InputText)
}

// keyLabel is how a footer names the keys of an action, joined by "/": function keys capitalized, arrows as
// glyphs, everything else as bound, the extra info key last.
func (m *Model) keyLabel(a action) string { return m.ui.keys.Label(a) }

// firstKeyLabel is the label of the first key of an action alone, such as "i" of "i/F3".
func (m *Model) firstKeyLabel(a action) string {
	first, _, _ := strings.Cut(m.keyLabel(a), "/")
	return first
}

// reloadPress is the key press of the first key bound to reload, so the command runs exactly what the key
// runs in the area shown. Reload cannot be rebound, so its keys are those of the table.
func (u *uiContext) reloadPress() (tea.KeyPressMsg, bool) {
	for _, a := range u.keys.Actions() {
		if a.ID != actReload {
			continue
		}
		for _, k := range a.DefaultKeys {
			if name, ok := strings.CutPrefix(k, "ctrl+"); ok && len([]rune(name)) == 1 {
				return tea.KeyPressMsg{Code: []rune(name)[0], Mod: tea.ModCtrl}, true
			}
			if code, ok := functionKeyCode(strings.ToUpper(k)); ok {
				return tea.KeyPressMsg{Code: code}, true
			}
		}
	}
	return tea.KeyPressMsg{}, false
}
