package tui

import (
	"strings"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// A key line is the hints of a screen: the kit frame draws it as the footer, a dialog as its own key line.
// Every hint names an action, never a key, so the keys in force show; hints of one label fold into one group.

// The ranks of the hints: a lower one stays longer when the footer runs short. Help always stays.
const (
	rankBack    = 1
	rankPrimary = 3 // enter, y, n, the delete key, the filter key and the bars
	rankSave    = 4
	rankCommand = 5
	rankQuit    = 6
	rankOther   = 10
	rankInfo    = 700
	rankCopy    = 710
)

// hintRank is the rank of a hint by its first action.
func hintRank(a action) int {
	switch a {
	case actBack:
		return rankBack
	case actEnter, actYes, actNo, actFilter, actDelete, actDiscard:
		return rankPrimary
	case actSave:
		return rankSave
	case actCommand:
		return rankCommand
	case actQuit:
		return rankQuit
	case actInfo:
		return rankInfo
	case actCopy:
		return rankCopy
	}
	return rankOther
}

// hintsOf are the hints of the keys of acts under one label, all of one kind and rank.
func hintsOf(label textID, kind kittui.HintKind, rank int, acts []action) []kittui.Hint {
	if len(acts) == 0 {
		return []kittui.Hint{{Label: label, Kind: kind, Priority: rank}}
	}
	hints := make([]kittui.Hint, len(acts))
	for i, a := range acts {
		hints[i] = kittui.Hint{Action: a, Label: label, Kind: kind, Priority: rank}
	}
	return hints
}

// hint names the keys of acts with label, ranked by the first of them; the help key always stays.
func hint(label textID, acts ...action) []kittui.Hint {
	kind, rank := kittui.HintNormal, rankOther
	if len(acts) > 0 {
		rank = hintRank(acts[0])
		if acts[0] == actHelp {
			kind = kittui.HintAlways
		}
	}
	return hintsOf(label, kind, rank, acts)
}

// barHint is a hint that moves along a bar, which ranks with the primary keys.
func barHint(label textID, acts ...action) []kittui.Hint {
	return hintsOf(label, kittui.HintNormal, rankPrimary, acts)
}

// navHint is a hint that moves through the screen: it stands at the right and goes first.
func navHint(label textID, acts ...action) []kittui.Hint {
	return hintsOf(label, kittui.HintNavigation, rankOther, acts)
}

// answer is an answer of a dialog: it is never dropped and becomes a button where its key can be pressed.
func answer(label textID, acts ...action) []kittui.Hint {
	rank := rankOther
	if len(acts) > 0 {
		rank = hintRank(acts[0])
	}
	return hintsOf(label, kittui.HintAnswer, rank, acts)
}

// keyLine joins hint groups into one key line.
func keyLine(parts ...[]kittui.Hint) []kittui.Hint {
	var out []kittui.Hint
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// closeKeys is the key line of a notice that only closes.
func closeKeys() []kittui.Hint { return answer(tHintClose, actBack) }

// hintGroups are the groups of a key line as drawn for the keys that act in focus.
func (m *Model) hintGroups(hints []kittui.Hint, focus kittui.InputFocus) []kittui.HintGroup {
	return m.ui.keys.Hints(hints, focus, m.ui.text)
}

// hintText is a key line as plain text, its groups joined by " · ", as the help lists it.
func (m *Model) hintText(hints []kittui.Hint) string {
	var groups []string
	for _, g := range m.hintGroups(hints, kittui.InputNavigation) {
		groups = append(groups, g.Text())
	}
	return strings.Join(groups, " · ")
}

// hasAction reports whether a key line names a.
func hasAction(hints []kittui.Hint, a action) bool {
	for _, h := range hints {
		if h.Action == a {
			return true
		}
	}
	return false
}

// withCopyKey adds the copy hint to a key line, ahead of the hints every screen ends with.
func withCopyKey(hints []kittui.Hint) []kittui.Hint {
	if hasAction(hints, actCopy) {
		return hints
	}
	at := len(hints)
	for i, h := range hints {
		if h.Action == actCommand || h.Action == actHelp {
			at = i
			break
		}
	}
	return keyLine(hints[:at:at], hint(tHintCopy, actCopy), hints[at:])
}

// The labels of the hints.
const (
	tHintAdd          textID = "hint.add"
	tHintArea         textID = "hint.area"
	tHintBack         textID = "hint.back"
	tHintCancel       textID = "hint.cancel"
	tHintChange       textID = "hint.change"
	tHintCheck        textID = "hint.check"
	tHintChoose       textID = "hint.choose"
	tHintClearFilter  textID = "hint.clearfilter"
	tHintClose        textID = "hint.close"
	tHintCmds         textID = "hint.cmds"
	tHintFold         textID = "hint.fold"
	tHintComplete     textID = "hint.complete"
	tHintContinue     textID = "hint.continue"
	tHintCopy         textID = "hint.copy"
	tHintDelete       textID = "hint.delete"
	tHintDetails      textID = "hint.details"
	tHintDiscard      textID = "hint.discard"
	tHintEdit         textID = "hint.edit"
	tHintEditPath     textID = "hint.editpath"
	tHintFilter       textID = "hint.filter"
	tHintHelp         textID = "hint.help"
	tHintInfo         textID = "hint.info"
	tHintKeep         textID = "hint.keep"
	tHintKeepEditing  textID = "hint.keepediting"
	tHintKeepFilter   textID = "hint.keepfilter"
	tHintLeave        textID = "hint.leave"
	tHintMembers      textID = "hint.members"
	tHintMove         textID = "hint.move"
	tHintNew          textID = "hint.new"
	tHintNext         textID = "hint.next"
	tHintOpen         textID = "hint.open"
	tHintPage         textID = "hint.page"
	tHintQuit         textID = "hint.quit"
	tHintRead         textID = "hint.read"
	tHintReadAsRoot   textID = "hint.readasroot"
	tHintReload       textID = "hint.reload"
	tHintRename       textID = "hint.rename"
	tHintRestart      textID = "hint.restart"
	tHintRetry        textID = "hint.retry"
	tHintRun          textID = "hint.run"
	tHintRunChange    textID = "hint.runchange"
	tHintSave         textID = "hint.save"
	tHintScroll       textID = "hint.scroll"
	tHintSearch       textID = "hint.search"
	tHintStandard     textID = "hint.standard"
	tHintStop         textID = "hint.stop"
	tHintSuggestion   textID = "hint.suggestion"
	tHintTab          textID = "hint.tab"
	tHintTick         textID = "hint.tick"
	tHintTypeToFilter textID = "hint.typetofilter"
	tHintTypeToSearch textID = "hint.typetosearch"
	tHintUndo         textID = "hint.undo"
	tHintUpdate       textID = "hint.update"
	tHintVerify       textID = "hint.verify"
)

// hintTexts are the labels of the hints in English and German.
func hintTexts() (en, de map[textID]string) {
	en = map[textID]string{
		tHintAdd:          "add",
		tHintArea:         "area",
		tHintBack:         "back",
		tHintCancel:       "cancel",
		tHintChange:       "change",
		tHintCheck:        "check",
		tHintChoose:       "choose",
		tHintClearFilter:  "clear filter",
		tHintClose:        "close",
		tHintCmds:         "cmds",
		tHintFold:         "collapse/expand",
		tHintComplete:     "complete",
		tHintContinue:     "continue",
		tHintCopy:         "copy",
		tHintDelete:       "delete",
		tHintDetails:      "details",
		tHintDiscard:      "discard",
		tHintEdit:         "edit",
		tHintEditPath:     "edit path",
		tHintFilter:       "filter",
		tHintHelp:         "help",
		tHintInfo:         "info",
		tHintKeep:         "keep",
		tHintKeepEditing:  "keep editing",
		tHintKeepFilter:   "keep filter",
		tHintLeave:        "leave",
		tHintMembers:      "members",
		tHintMove:         "move",
		tHintNew:          "new",
		tHintNext:         "next",
		tHintOpen:         "open",
		tHintPage:         "page",
		tHintQuit:         "quit",
		tHintRead:         "read",
		tHintReadAsRoot:   "read as root",
		tHintReload:       "reload",
		tHintRename:       "rename",
		tHintRestart:      "restart",
		tHintRetry:        "retry",
		tHintRun:          "run",
		tHintRunChange:    "apply",
		tHintSave:         "save",
		tHintScroll:       "scroll",
		tHintSearch:       "search",
		tHintStandard:     "standard key",
		tHintStop:         "stop",
		tHintSuggestion:   "suggestion",
		tHintTab:          "tab",
		tHintTick:         "tick",
		tHintTypeToFilter: "type to filter",
		tHintTypeToSearch: "type to search",
		tHintUndo:         "undo",
		tHintUpdate:       "update",
		tHintVerify:       "verify",
	}
	de = map[textID]string{
		tHintAdd:          "hinzufügen",
		tHintArea:         "Bereich",
		tHintBack:         "zurück",
		tHintCancel:       "abbrechen",
		tHintChange:       "ändern",
		tHintCheck:        "prüfen",
		tHintChoose:       "wählen",
		tHintClearFilter:  "Filter löschen",
		tHintClose:        "schließen",
		tHintCmds:         "Befehle",
		tHintFold:         "ein-/ausklappen",
		tHintComplete:     "ergänzen",
		tHintContinue:     "fortfahren",
		tHintCopy:         "kopieren",
		tHintDelete:       "löschen",
		tHintDetails:      "Details",
		tHintDiscard:      "verwerfen",
		tHintEdit:         "bearbeiten",
		tHintEditPath:     "Pfad ändern",
		tHintFilter:       "Filter",
		tHintHelp:         "Hilfe",
		tHintInfo:         "Info",
		tHintKeep:         "behalten",
		tHintKeepEditing:  "weiterbearbeiten",
		tHintKeepFilter:   "Filter behalten",
		tHintLeave:        "verlassen",
		tHintMembers:      "Mitglieder",
		tHintMove:         "bewegen",
		tHintNew:          "neu",
		tHintNext:         "weiter",
		tHintOpen:         "öffnen",
		tHintPage:         "Seite",
		tHintQuit:         "Ende",
		tHintRead:         "lesen",
		tHintReadAsRoot:   "als root lesen",
		tHintReload:       "neu laden",
		tHintRename:       "umbenennen",
		tHintRestart:      "neu starten",
		tHintRetry:        "erneut",
		tHintRun:          "ausführen",
		tHintRunChange:    "ausführen",
		tHintSave:         "speichern",
		tHintScroll:       "scrollen",
		tHintSearch:       "Suche",
		tHintStandard:     "Standardtaste",
		tHintStop:         "beenden",
		tHintSuggestion:   "Vorschlag",
		tHintTab:          "Reiter",
		tHintTick:         "wählen",
		tHintTypeToFilter: "tippen zum Filtern",
		tHintTypeToSearch: "tippen zum Suchen",
		tHintUndo:         "rückgängig",
		tHintUpdate:       "aktualisieren",
		tHintVerify:       "prüfen",
	}
	return en, de
}
