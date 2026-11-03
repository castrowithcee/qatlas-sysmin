package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// action names a key-bound action of the editor. Dispatch and footers refer to the action, never to the
// key, so the binding lives in exactly one place. The IDs of the rebindable actions are the names under keys
// in tui.yaml.
type action = kittui.ActionID

const (
	actInfo      action = uiprefs.ActionInfo
	actSave      action = uiprefs.ActionSave
	actHelp      action = uiprefs.ActionHelp
	actCommand   action = uiprefs.ActionCommand
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
		return kittui.Action{ID: id, Level: level, DefaultKeys: keys, Help: kittui.TextID(help)}
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

// uiContext is what the screens of one editor read keys with. The model holds it, so no state of the
// interface lives in the package.
type uiContext struct {
	keys *kittui.Keymap
}

// newUIContext builds the context with the default keys; charset decides how labels spell the arrows. The key
// table is fixed in the code, so a table the kit refuses is a defect of this package, never of the input.
func newUIContext(charset kittui.CharsetMode) *uiContext {
	keys, err := kittui.NewKeymap(keyTable(), charset)
	if err != nil {
		panic(fmt.Sprintf("the key table is invalid: %v", err))
	}
	return &uiContext{keys: keys}
}

// bindKeys makes the keys of the preferences the keys in force. An entry the key table refuses keeps the
// standard key of its action with a warning, and the preferences then name that key, so Settings shows the
// key that acts.
func (m *Model) bindKeys() []string {
	keys := m.prefs.Keys.WithDefaults()
	overrides := map[action]string{}
	for _, name := range uiprefs.Actions {
		overrides[action(name)] = keys.Get(name)
	}
	usable, refused := m.ui.keys.Resolve(overrides)
	var warnings []string
	for _, err := range refused {
		name := string(err.Action)
		standard := uiprefs.DefaultKeys().Get(name)
		m.prefs.Keys = m.prefs.Keys.With(name, standard)
		if standard == "" {
			standard = "no extra key"
		}
		warnings = append(warnings, fmt.Sprintf("keys.%s %v, using %s", name, err, standard))
	}
	if err := m.ui.keys.Bind(usable); err != nil {
		warnings = append(warnings, fmt.Sprintf("keys: %v, using the standard keys", err))
	}
	return warnings
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

// footerKeys replaces the key placeholders of a footer text with the current key names, so a footer
// follows the keymap while a screen-local key stays a plain literal in its text.
func (m *Model) footerKeys(text string) string { return m.keyPlaceholders(footerText(text)) }

// keyPlaceholders replaces the key placeholders of an already translated footer text, such as "{save} save".
// Translating comes first because the catalog is keyed by the group with its placeholders.
func (m *Model) keyPlaceholders(text string) string {
	if !strings.Contains(text, "{") {
		return text
	}
	for _, a := range []action{actSave, actHelp, actInfo, actCommand, actBack, actDelete, actCopy} {
		text = strings.ReplaceAll(text, "{"+string(a)+"}", m.keyLabel(a))
	}
	return text
}
