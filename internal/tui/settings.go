package tui

import (
	"context"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// Settings has two pages. General is device-local and needs no service; Update only looks at the releases.
const (
	subGeneral = iota
	subUpdate
	subCount
)

// subTitle is the name of a Settings page as drawn.
func subTitle(ui *uiContext, sub int) string { return ui.tr(subTitleID(sub)) }

func subTitleID(sub int) textID { return [...]textID{tSubGeneral, tSubUpdate}[sub] }

// subKey is the English name of a Settings page, the one commands are given by.
func subKey(sub int) string { return [...]string{"general", "update"}[sub] }

// ReleaseInfo is the answer of a release lookup: the newest stable release that is newer than the running
// program, empty when there is none, or Dev for a build that has no release version to compare.
type ReleaseInfo struct {
	Latest string
	Dev    bool
}

// ReleaseCheck looks the releases up. It may block and must stop with ctx.
type ReleaseCheck func(ctx context.Context) (ReleaseInfo, error)

// updateTimeout bounds one release lookup.
const updateTimeout = 20 * time.Second

// settingsState holds the pages of Settings. The form of General is loaded when Settings is shown and dropped
// when it is left, so the page always starts from what is saved.
type settingsState struct {
	sub    int
	loaded bool
	gen    *kittui.Form
	upd    updateState
}

// The values the mouse field offers.
const (
	choiceMouseOn  = "on"
	choiceMouseOff = "off"
)

// The fields and sections of General. A key field is named after its action.
const (
	fieldLanguage kittui.FieldID = "language"
	fieldTheme    kittui.FieldID = "theme"
	fieldIcons    kittui.FieldID = "icons"
	fieldStart    kittui.FieldID = "start"
	fieldMouse    kittui.FieldID = "mouse"

	sectionDisplay kittui.SectionID = "display"
	sectionKeys    kittui.SectionID = "keys"
)

func keyFieldID(a action) kittui.FieldID { return kittui.FieldID("key." + string(a)) }

// keyFieldAction is the action of a key field, and false for another field.
func keyFieldAction(id kittui.FieldID) (action, bool) {
	for _, a := range rebindable() {
		if keyFieldID(a) == id {
			return a, true
		}
	}
	return "", false
}

// keyLabelID is the label of the key field of a rebindable action.
func keyLabelID(a action) textID {
	return map[action]textID{actSave: tLblKeySave, actHelp: tLblKeyHelp, actCommand: tLblKeyCommand, actInfo: tLblKeyInfo}[a]
}

type updatePhase int

const (
	updNone updatePhase = iota
	updChecking
	updDone
	updFailed
)

// updateState is the result of the release lookup of this run.
type updateState struct {
	phase updatePhase
	info  ReleaseInfo
	seq   int
}

// updateMsg is the answer of a release lookup.
type updateMsg struct {
	seq  int
	info ReleaseInfo
	err  error
}

// choiceOr is value where valid holds it and fallback otherwise, so a form never opens on a value it does not
// offer.
func choiceOr(value string, valid []string, fallback string) kittui.ChoiceID {
	if slices.Contains(valid, value) {
		return kittui.ChoiceID(value)
	}
	return kittui.ChoiceID(fallback)
}

// generalFields are the fields of General as prefs hold them: fixed choices and one key field per rebindable
// action, in the order of rebindable.
func (m *Model) generalFields(prefs kitprefs.Prefs) []kittui.FormField {
	languages := []string{kitprefs.LanguageEnglish, kitprefs.LanguageGerman, kitprefs.LanguageSystem}
	icons := []string{kitprefs.IconsUnicode, kitprefs.IconsNerdFont, kitprefs.IconsNone}
	theme, _ := m.ui.knownTheme(prefs.Theme)
	starts := make([]kittui.Choice, areaCount)
	for a := area(0); a < areaCount; a++ {
		starts[a] = kittui.Choice{ID: kittui.ChoiceID(a.id()), Label: a.titleID()}
	}
	choice := func(id kittui.FieldID, label textID, choices []kittui.Choice, value kittui.ChoiceID) kittui.FormField {
		return kittui.FormField{ID: id, Section: sectionDisplay, Kind: kittui.FieldChoice, Label: label, Choices: choices,
			Value: kittui.FormValue{Choice: value}}
	}
	fields := []kittui.FormField{
		choice(fieldLanguage, tLblLanguage, literalChoices(languages...), choiceOr(prefs.Language, languages, kitprefs.LanguageEnglish)),
		choice(fieldTheme, tLblTheme, literalChoices(m.ui.themes.Names()...), kittui.ChoiceID(theme)),
		choice(fieldIcons, tLblIcons, literalChoices(icons...), choiceOr(prefs.Icons, icons, kitprefs.IconsUnicode)),
		choice(fieldStart, tLblStart, starts, choiceOr(prefs.Start, areaIDs(), areaOverview.id())),
		choice(fieldMouse, tLblMouse, literalChoices(choiceMouseOn, choiceMouseOff), kittui.ChoiceID(mouseChoice(prefs))),
	}
	for _, a := range rebindable() {
		fields = append(fields, kittui.FormField{ID: keyFieldID(a), Section: sectionKeys, Kind: kittui.FieldKey,
			Label: keyLabelID(a), Value: kittui.FormValue{Text: m.prefKey(prefs.Keys, a)}, Hint: tSetKeyHint})
	}
	return fields
}

// loadForms builds the form of General from what is saved, once per visit. Its fields come from the code and
// from preferences already checked, so a form the kit refuses is a defect of this package.
func (m *Model) loadForms() {
	if m.set.loaded {
		return
	}
	sections := []kittui.FormSection{{ID: sectionDisplay, Title: tSecDisplay}, {ID: sectionKeys, Title: tSecKeys}}
	form, err := m.newForm("", "", m.generalFields(m.prefs), sections, nil)
	if err != nil {
		panic(fmt.Sprintf("the form of General is invalid: %v", err))
	}
	m.set.loaded, m.set.gen = true, form
}

// mouseChoice is the value of the mouse field for prefs.
func mouseChoice(prefs kitprefs.Prefs) string {
	if prefs.MouseEnabled {
		return choiceMouseOn
	}
	return choiceMouseOff
}

// resetForms drops the forms; the next visit loads them anew.
func (m *Model) resetForms() { m.set.loaded, m.set.gen, m.set.sub = false, nil, 0 }

// settingsDirty reports unsaved input on the page shown.
func (m *Model) settingsDirty() bool {
	if m.area != areaSettings || !m.set.loaded {
		return false
	}
	return m.set.sub == subGeneral && m.set.gen.Changed()
}

// generalFocused reports whether the form of General takes the keys of the content.
func (m *Model) generalFocused() bool {
	return m.screen == screenArea && m.area == areaSettings && m.set.sub == subGeneral && m.set.gen != nil &&
		m.zoneNow() == zoneContent
}

// generalCapturing reports whether a key field of General waits for the new key.
func (m *Model) generalCapturing() bool {
	return m.area == areaSettings && m.set.sub == subGeneral && m.set.gen != nil && m.set.gen.Capturing()
}

// generalInner reports whether General shows a level of its own: the open list of a choice or a key field
// waiting for its key. Only the keys of the form act then.
func (m *Model) generalInner() bool {
	return m.generalFocused() && (m.set.gen.Open() || m.set.gen.Capturing())
}

// setSub shows page sub of Settings, asking first about unsaved input.
func (m *Model) setSub(sub int) tea.Cmd {
	sub = (sub + subCount) % subCount
	return m.guarded(func() tea.Cmd {
		m.set.sub = sub
		m.set.loaded = false
		m.loadForms()
		m.clearMessages()
		return m.enterPage()
	})
}

// checkUpdate looks the releases up in the background. The result of this run is kept; force asks again.
func (m *Model) checkUpdate(force bool) tea.Cmd {
	u := &m.set.upd
	if m.releases == nil || u.phase == updChecking || (u.phase != updNone && !force) {
		return nil
	}
	u.phase, u.seq = updChecking, u.seq+1
	seq, check, timeout := u.seq, m.releases, m.updateTimeout
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		info, err := check(ctx)
		return updateMsg{seq, info, err}
	}
}

func (m *Model) takeUpdate(msg updateMsg) {
	u := &m.set.upd
	if msg.seq != u.seq {
		return
	}
	u.phase, u.info = updDone, msg.info
	if msg.err != nil {
		u.phase = updFailed
	}
}

// settingsKeys is the key text of the page shown.
func (m *Model) settingsKeys() []kittui.Hint {
	if m.set.sub != subGeneral || m.set.gen == nil {
		return hint(tHintCheck, actTest)
	}
	g := m.set.gen
	keys := g.Hints()
	if _, ok := keyFieldAction(g.Focused()); ok && !g.Open() && !g.Capturing() {
		keys = append(keys, hint(tHintStandard, actDelete)...)
	}
	return keys
}

// settingsKey handles a key of the Update page. It reports false for a key the area screen handles itself.
func (m *Model) settingsKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.set.sub != subUpdate {
		return nil, false
	}
	switch {
	case m.keyIs(key, actTest):
		return m.checkUpdate(true), true
	case m.keyIs(key, actUp):
		m.toBar()
		return nil, true
	}
	return nil, false
}

// generalKey hands a key of General to its form. The delete key on a key field restores the standard key. It
// reports false for a key the form leaves to the area screen; up above the first field goes to the page strip.
func (m *Model) generalKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	g := m.set.gen
	if content, ok := pasted(key); ok {
		return nil, g.Paste(content)
	}
	if a, ok := keyFieldAction(g.Focused()); ok && !g.Open() && !g.Capturing() && m.keyIs(key, actDelete) {
		if err := g.SetValue(g.Focused(), kittui.FormValue{Text: m.prefKey(nil, a)}); err != nil {
			m.fail = err.Error()
		}
		return nil, true
	}
	inner := g.Open() || g.Capturing()
	act, handled := m.formKey(g, key)
	switch {
	case act == actSave:
		return m.saveGeneral(), true
	case handled, inner:
		return nil, true
	case m.keyIs(key, actUp):
		m.toBar()
		return nil, true
	}
	return nil, false
}

// clickGeneral is a click on a target of the form of General: the kit focuses the field, opens a choice or
// takes the value clicked.
func (m *Model) clickGeneral(res kittui.MouseResult) tea.Cmd {
	if !m.generalReady() {
		return nil
	}
	m.focusRows()
	if act, _ := m.set.gen.Click(res); act == actSave {
		return m.saveGeneral()
	}
	return nil
}

// generalReady reports whether General is the page shown with its form loaded.
func (m *Model) generalReady() bool {
	return m.screen == screenArea && m.area == areaSettings && m.set.sub == subGeneral && m.set.gen != nil
}

// saveCurrent saves the page shown.
func (m *Model) saveCurrent() tea.Cmd {
	m.clearMessages()
	if m.set.sub == subGeneral {
		return m.saveGeneral()
	}
	return nil
}

// saveGeneral stores the preferences on this device. It needs no service: the file is not part of the
// configuration. Every key is checked first and a problem stands under its field.
func (m *Model) saveGeneral() tea.Cmd {
	m.clearMessages()
	g := m.set.gen
	values := g.Values()
	typed := map[action]string{}
	for _, a := range rebindable() {
		typed[a] = values[keyFieldID(a)].Text
	}
	keys := map[action]string{}
	var problems []kittui.FormProblem
	for _, a := range rebindable() {
		others := map[action]string{}
		for id, key := range typed {
			if id != a {
				others[id] = key
			}
		}
		norm, err := m.ui.keys.CheckBinding(a, typed[a], others)
		if err != nil {
			problems = append(problems, kittui.FormProblem{Field: keyFieldID(a), Cause: err.Error()})
			continue
		}
		keys[a] = norm
	}
	if len(problems) > 0 {
		g.SetProblems(problems)
		m.status = m.tr(tFormProblems, len(problems))
		return nil
	}
	if m.prefsPath == "" {
		m.fail = m.tr(tPrefsNoPath)
		return nil
	}
	usable, _ := m.ui.keys.Resolve(keys)
	next := m.prefs
	next.Language, next.Theme = string(values[fieldLanguage].Choice), string(values[fieldTheme].Choice)
	next.Icons, next.Start = string(values[fieldIcons].Choice), string(values[fieldStart].Choice)
	next.MouseEnabled, next.Keys = values[fieldMouse].Choice == choiceMouseOn, usable
	if err := kitprefs.Save(m.prefsPath, next, m.ui.prefsSchema()); err != nil {
		m.fail = m.tr(tPrefsSaveFailed)
		return nil
	}
	var query tea.Cmd
	before := m.ui.theme
	m.prefs = next
	m.ui.setTheme(next.Theme)
	if m.ui.theme != before {
		query = m.ui.terminalColorQuery()
	}
	m.ui.setLanguage(kitprefs.Language(next.Language))
	m.status = m.tr(tPrefsSaved)
	if warnings := m.bindKeys(); len(warnings) > 0 {
		m.status = "warning: " + warnings[0]
	}
	focus := g.Focused()
	m.set.loaded = false
	m.loadForms()
	m.set.gen.Focus(focus)
	return query
}

// leaveAsk is the question before unsaved input is dropped; next is what the person was about to do.
type leaveAsk struct{ next func() tea.Cmd }

// guarded runs next at once, or after the question when the page shown holds unsaved input.
func (m *Model) guarded(next func() tea.Cmd) tea.Cmd {
	dirty := m.screen == screenArea && m.settingsDirty() || m.screen == screenPrompt && m.form != nil && m.form.dirty()
	if !dirty {
		return next()
	}
	if m.openDialog(kittui.DialogSpec{
		Title: tDlgLeave, Question: m.tr(tLeaveTitle), Consequence: m.tr(tLeaveConsequence), Irreversible: true,
		Answers: []kittui.Answer{{Action: actSave, Label: tHintSave}, {Action: actDiscard, Label: tHintDiscard},
			{Action: actNo, Label: tHintKeepEditing}, {Action: actBack, Label: tHintKeepEditing}},
		Keys: kittui.DialogKeys{Enter: actEnter},
	}) {
		m.leave = &leaveAsk{next}
	}
	return nil
}

// updateLeave handles the answer to the leave question: save keeps the person on the page or in the form,
// discard drops the input and goes on, and keep editing goes back to it.
func (m *Model) updateLeave(key tea.KeyPressMsg) tea.Cmd {
	form := m.form
	a, ok := m.dialogAnswer(key)
	if !ok {
		return nil
	}
	switch a {
	case actSave:
		m.closeDialog()
		if form != nil {
			m.screen = screenPrompt
			return m.submitForm(form)
		}
		return m.saveCurrent()
	case actDiscard:
		next := m.leave.next
		m.closeDialog()
		if form != nil {
			m.closeForm()
		} else {
			m.set.loaded = false
			m.loadForms()
		}
		return next()
	default:
		m.closeDialog()
		if form != nil {
			m.screen = screenPrompt
		}
	}
	return nil
}
