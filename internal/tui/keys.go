package tui

import (
	"os"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/charmbracelet/x/ansi"
)

// typing reports whether the current screen reads typed text right now. Letters are text there, so neither
// the vim movement keys, ? nor : mean anything else.
func (m *Model) typing() bool {
	switch m.screen {
	case screenPrompt:
		return true
	case screenList:
		return m.base.filtering()
	case screenArea:
		return m.generalCapturing() || m.hostTyping() || m.currentPage().filtering()
	case screenPopup:
		return m.pop != nil && m.pop.cmd != nil
	}
	return false
}

// actionMsg asks the editor to run an action without a key: a click on a key hint, a button or an element of the
// frame, and a command. It reaches the screens as the key press of no key that triggers exactly that action, so a
// screen answers the action in the state it is in as it answers the key.
type actionMsg struct{ act action }

// actionCode is the code of the key press an action travels as; no key of a keyboard has it.
const actionCode rune = -2

// isAction reports whether key is the key press an action being run travels as.
func (m *Model) isAction(key tea.KeyPressMsg) bool { return key.Code == actionCode && m.acting != "" }

// actsInText reports whether the action has a key that types nothing, which is the one thing that may act where
// text is typed: a letter hint never makes a character and never acts there.
func (m *Model) actsInText(a action) bool {
	label, ok := m.ui.keys.Labels(kittui.InputText).KeyLabel(a)
	return ok && label != ""
}

// runAction runs f with the key press of the action a, which is all a key handler sees of it.
func (m *Model) runAction(a action, f func(key tea.KeyPressMsg) tea.Cmd) tea.Cmd {
	if a == "" || (m.typing() && !m.actsInText(a)) {
		return nil
	}
	prev := m.acting
	m.acting = a
	defer func() { m.acting = prev }()
	return f(tea.KeyPressMsg{Code: actionCode})
}

// helpReachable reports whether ? opens the help (and : the command line) on the current screen: wherever
// nothing is typed.
func (m *Model) helpReachable() bool { return !m.typing() }

// moveKey turns the vim keys into the arrow keys on a screen that reads no text (where text is typed they are
// text), so every screen moves the same way and no handler needs to know the letters.
func (m *Model) moveKey(key tea.KeyPressMsg) tea.KeyPressMsg {
	focus := m.inputFocus()
	for _, d := range []struct {
		act  action
		code rune
	}{{actVimLeft, tea.KeyLeft}, {actVimDown, tea.KeyDown}, {actVimUp, tea.KeyUp}, {actVimRight, tea.KeyRight}} {
		if m.ui.keys.Is(key, d.act, focus) {
			return tea.KeyPressMsg{Code: d.code}
		}
	}
	return key
}

// pasteCode is the code of the key a paste travels as; no key of a keyboard has it.
const pasteCode rune = -1

// pasteKey is a paste as the key it travels as through the key handlers of the screens. It is named after the
// pasted text in brackets, so no key binding matches it, and a text field takes it as a paste.
func pasteKey(content string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: pasteCode, Text: "[" + content + "]"}
}

// pasted returns the text of a paste key and whether key is one.
func pasted(key tea.KeyPressMsg) (string, bool) {
	if key.Code != pasteCode || len(key.Text) < 2 {
		return "", false
	}
	return key.Text[1 : len(key.Text)-1], true
}

// typedKey is key as a filter of the kit reads it: a paste key as the text it stands for, and alt with a
// character a text line binds nothing to as that character, which a text field has always typed.
func typedKey(key tea.KeyPressMsg) tea.KeyPressMsg {
	if content, ok := pasted(key); ok {
		return textKey(content)
	}
	if r, ok := altCharacter(key); ok {
		return textKey(string(r))
	}
	return key
}

// altCharacter is the character of alt with a printable key that no text line binds, and false for any other
// key.
func altCharacter(key tea.KeyPressMsg) (rune, bool) {
	if key.Text != "" || key.Mod&^tea.ModShift != tea.ModAlt || !unicode.IsPrint(key.Code) || altWordKey(key.Code) {
		return 0, false
	}
	if key.Mod.Contains(tea.ModShift) {
		return unicode.ToUpper(key.Code), true
	}
	return key.Code, true
}

// legacyKey names a key as the terminal library always named it where its input reader now names it
// otherwise: the shifted F1 to F8 of xterm-like terminals are F13 to F20, as a stored key binding knows them.
func legacyKey(key tea.KeyPressMsg) tea.KeyPressMsg {
	if key.Mod&^tea.ModAlt == tea.ModShift && key.Code >= tea.KeyF1 && key.Code <= tea.KeyF8 {
		key.Code += tea.KeyF13 - tea.KeyF1
		key.Mod &^= tea.ModShift
	}
	return key
}

// textKey is the key press of typed text.
func textKey(text string) tea.KeyPressMsg {
	code := tea.KeyExtended
	if runes := []rune(text); len(runes) == 1 {
		code = runes[0]
	}
	return tea.KeyPressMsg{Code: code, Text: text}
}

// legacyKeys is the terminal output of the editor without the keyboard enhancements. Bubble Tea writes each
// frame, and each set of terminal modes, in one write, so no sequence is split between two.
type legacyKeys struct {
	f *os.File
	// modes removes what Bubble Tea writes to switch the keyboard enhancements of the terminal on, off, or to
	// ask about them: modifyOtherKeys and the Kitty keyboard protocol. With them a terminal reports keys such
	// as ctrl+i, shift+enter or the keypad's enter under names of their own; without them every key keeps the
	// name the legacy encoding gives it, the name every key binding, stored ones included, was taken under.
	modes *strings.Replacer
}

func newLegacyKeys(f *os.File) legacyKeys {
	return legacyKeys{f, strings.NewReplacer(ansi.SetModifyOtherKeys2, "", ansi.ResetModifyOtherKeys, "",
		ansi.PushKittyKeyboard(1), "", ansi.PopKittyKeyboard(1), "", ansi.RequestKittyKeyboard, "")}
}

func (o legacyKeys) Write(p []byte) (int, error) {
	if _, err := o.f.WriteString(o.modes.Replace(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (o legacyKeys) Read(p []byte) (int, error) { return o.f.Read(p) }
func (o legacyKeys) Close() error               { return o.f.Close() }
func (o legacyKeys) Fd() uintptr                { return o.f.Fd() }

// The kit building blocks read keys themselves. For an action being run they take the action instead, through
// the call the kit offers for it: Act, or Click on the target of the action.

func (m *Model) formKey(f *kittui.Form, key tea.KeyPressMsg) (kittui.ActionID, bool) {
	if m.isAction(key) {
		return f.Act(m.acting)
	}
	return f.Update(key)
}

func (m *Model) listKey(l *kittui.List, key tea.KeyPressMsg) (kittui.ItemID, bool) {
	if m.isAction(key) {
		return l.Act(m.acting)
	}
	return l.Update(key)
}

func (m *Model) helpKey(h *kittui.Help, key tea.KeyPressMsg) (kittui.ActionID, bool) {
	if m.isAction(key) {
		return h.Act(m.acting)
	}
	return h.Update(key)
}

func (m *Model) explanationKey(e *kittui.Explanation, key tea.KeyPressMsg) (kittui.ActionID, bool) {
	if m.isAction(key) {
		return e.Act(m.acting)
	}
	return e.Update(key)
}

// dialogKey has no Act in the kit: the click on the target of an answer gives the answer its key gives.
func (m *Model) dialogKey(d *kittui.Dialog, key tea.KeyPressMsg) (kittui.ActionID, bool) {
	if m.isAction(key) {
		return d.Click(kittui.MouseResult{Handled: true, Action: m.acting})
	}
	return d.Update(key)
}

// filterBarUpdate is the same for the filter bar, whose open modal takes the click on a key of its key line as the key.
func (m *Model) filterBarUpdate(b *kittui.FilterBar, key tea.KeyPressMsg) ([]kittui.FilterField, bool) {
	if m.isAction(key) {
		return b.Click(kittui.MouseResult{Handled: true, Action: m.acting})
	}
	return b.Update(key)
}
