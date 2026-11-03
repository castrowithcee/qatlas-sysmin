package tui

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
)

// Settings has two pages. General is device-local and needs no service; Update only looks at the releases.
const (
	subGeneral = iota
	subUpdate
	subCount
)

// subTitle is the name of a Settings page as drawn.
func subTitle(ui *uiContext, sub int) string {
	return ui.tr([...]textID{tSubGeneral, tSubUpdate}[sub])
}

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

// settingsState holds the pages of Settings. The forms are loaded when Settings is shown and dropped when it
// is left, so a page always starts from what is saved.
type settingsState struct {
	sub int
	// editing says a text field of the current page takes the keys.
	editing bool
	loaded  bool
	gen     generalForm
	upd     updateState
}

// The values the mouse row offers.
const (
	choiceMouseOn  = "on"
	choiceMouseOff = "off"
)

// General rows: three choices, then one key line per rebindable action in the order of rebindable.
const (
	genLanguage = iota
	genTheme
	genMouse
	genKeys
	genRows = genKeys + 4
)

// generalForm is the General page as it is edited.
type generalForm struct {
	language, theme, mouse string
	keys                   [4]textinput.Model
	focus                  int
	problems               [4]string
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

func keyInput(value string) textinput.Model {
	in := newTextInput()
	in.Prompt = ""
	in.SetValue(value)
	return in
}

// loadForms builds the forms of Settings from what is saved, once per visit.
func (m *Model) loadForms() {
	if m.set.loaded {
		return
	}
	m.set.loaded, m.set.editing = true, false
	g := &m.set.gen
	*g = generalForm{language: m.prefs.Language, mouse: mouseChoice(m.prefs)}
	g.theme, _ = m.ui.knownTheme(m.prefs.Theme)
	for i, a := range rebindable() {
		g.keys[i] = keyInput(m.prefKey(m.prefs.Keys, a))
	}
}

// mouseChoice is the value of the mouse row for prefs.
func mouseChoice(prefs kitprefs.Prefs) string {
	if prefs.MouseEnabled {
		return choiceMouseOn
	}
	return choiceMouseOff
}

// resetForms drops the forms; the next visit loads them anew.
func (m *Model) resetForms() { m.set.loaded, m.set.editing, m.set.sub = false, false, 0 }

func (m *Model) generalDirty() bool {
	g := &m.set.gen
	prefs := m.prefs
	theme, _ := m.ui.knownTheme(prefs.Theme)
	if g.language != prefs.Language || g.theme != theme || g.mouse != mouseChoice(prefs) {
		return true
	}
	for i, a := range rebindable() {
		if strings.TrimSpace(g.keys[i].Value()) != m.prefKey(prefs.Keys, a) {
			return true
		}
	}
	return false
}

// settingsDirty reports unsaved input on the page shown.
func (m *Model) settingsDirty() bool {
	if m.area != areaSettings || !m.set.loaded {
		return false
	}
	return m.set.sub == subGeneral && m.generalDirty()
}

// setSub shows page sub of Settings, asking first about unsaved input.
func (m *Model) setSub(sub int) tea.Cmd {
	sub = (sub + subCount) % subCount
	return m.guarded(func() tea.Cmd {
		m.set.sub, m.set.editing = sub, false
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
func (m *Model) settingsKeys() []string {
	switch m.set.sub {
	case subGeneral:
		return []string{"↑/↓ move", "enter edit", "{save} save"}
	}
	return []string{"t check"}
}

// settingsKey handles a key of Settings while no field takes the text. It reports false for a key the area
// screen handles itself.
func (m *Model) settingsKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.keyIs(key, actSave) {
		return m.saveCurrent(), true
	}
	if m.set.sub == subGeneral {
		return m.generalKey(key)
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

// formMove moves the focus over the n rows of a Settings form: up and down stop at the ends, and up on the
// first row goes to the page strip; tab and shift+tab step from field to field and wrap round, as they do
// while a field is edited.
func (m *Model) formMove(focus *int, n int, key tea.KeyPressMsg) bool {
	switch {
	case m.keyIs(key, actUp) && *focus <= 0:
		m.toBar()
	case m.keyIs(key, actUp):
		*focus--
	case m.keyIs(key, actDown):
		*focus = min(*focus+1, n-1)
	case m.keyIs(key, actNext):
		*focus = wrap(*focus+1, n)
	case m.keyIs(key, actPrev):
		*focus = wrap(*focus-1, n)
	case m.keyIs(key, actHome):
		*focus = 0
	case m.keyIs(key, actEnd):
		*focus = n - 1
	default:
		return false
	}
	return true
}

func (m *Model) generalKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	g := &m.set.gen
	if m.formMove(&g.focus, genRows, key) {
		return nil, true
	}
	if !m.keyIs(key, actEnter) {
		return nil, false
	}
	pick := func(id string, values []string, current string, set func(string)) {
		active := 0
		for i, v := range values {
			if v == current {
				active = i
			}
		}
		m.openDropdown(id, values, active, func(i int) tea.Cmd { set(values[i]); return nil })
	}
	switch g.focus {
	case genLanguage:
		pick("language", []string{kitprefs.LanguageEnglish, kitprefs.LanguageGerman, kitprefs.LanguageSystem}, g.language,
			func(v string) { g.language = v })
	case genTheme:
		pick("theme", m.ui.themes.Names(), g.theme, func(v string) { g.theme = v })
	case genMouse:
		pick("mouse", []string{choiceMouseOn, choiceMouseOff}, g.mouse, func(v string) { g.mouse = v })
	default:
		m.startEditing()
	}
	return nil, true
}

// field is the text field under the focus of the page shown, nil when the focus is on none.
func (m *Model) field() *textinput.Model {
	if m.set.sub == subGeneral && m.set.gen.focus >= genKeys {
		return &m.set.gen.keys[m.set.gen.focus-genKeys]
	}
	return nil
}

func (m *Model) startEditing() {
	if in := m.field(); in != nil {
		m.set.editing = true
		in.Focus()
		in.CursorEnd()
	}
}

func (m *Model) stopEditing() {
	if in := m.field(); in != nil {
		in.Blur()
	}
	m.set.editing = false
}

// editKey handles a key while a text field takes the text. enter and tab go on to the next field, esc stops
// editing and keeps what was typed; nothing is saved but by the save key.
func (m *Model) editKey(key tea.KeyPressMsg) tea.Cmd {
	in := m.field()
	if in == nil {
		m.set.editing = false
		return nil
	}
	rows, focus := genRows, &m.set.gen.focus
	switch {
	case m.keyIs(key, actSave):
		m.stopEditing()
		return m.saveCurrent()
	case m.keyIs(key, actBack):
		m.stopEditing()
	case m.keyIs(key, actEnter), m.keyIs(key, actNext), m.keyIs(key, actDown):
		m.stopEditing()
		*focus = wrap(*focus+1, rows)
	case m.keyIs(key, actPrev), m.keyIs(key, actUp):
		m.stopEditing()
		*focus = wrap(*focus-1, rows)
	case m.keyIsTyped(key, actInfo):
		m.openPopup(popInfo)
	default:
		var cmd tea.Cmd
		*in, cmd = in.Update(inputMsg(key))
		return cmd
	}
	return nil
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
// configuration. Every key is checked first and a problem stands under its line.
func (m *Model) saveGeneral() tea.Cmd {
	g := &m.set.gen
	typed := map[action]string{}
	for i, a := range rebindable() {
		typed[a] = g.keys[i].Value()
	}
	keys, ok := map[action]string{}, true
	for i, a := range rebindable() {
		others := map[action]string{}
		for id, key := range typed {
			if id != a {
				others[id] = key
			}
		}
		norm, err := m.ui.keys.CheckBinding(a, typed[a], others)
		g.problems[i] = ""
		if err != nil {
			g.problems[i], ok = err.Error(), false
			continue
		}
		keys[a] = norm
	}
	if !ok {
		return nil
	}
	if m.prefsPath == "" {
		m.fail = m.tr(tPrefsNoPath)
		return nil
	}
	usable, _ := m.ui.keys.Resolve(keys)
	next := m.prefs
	next.Language, next.Theme, next.MouseEnabled, next.Keys = g.language, g.theme, g.mouse == choiceMouseOn, usable
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
	focus := g.focus
	m.set.loaded = false
	m.loadForms()
	m.set.gen.focus = focus
	return query
}

// leaveAsk is the question before unsaved input is dropped; next is what the person was about to do.
type leaveAsk struct{ next func() tea.Cmd }

// guarded runs next at once, or after the question when the page shown holds unsaved input.
func (m *Model) guarded(next func() tea.Cmd) tea.Cmd {
	if m.screen != screenArea || !(m.settingsDirty()) {
		return next()
	}
	m.set.editing = false
	m.leave = &leaveAsk{next}
	m.ask = &question{title: m.tr(tLeaveTitle), consequence: m.tr(tLeaveConsequence), warn: true,
		keys: "{save} save · d discard · {back} keep editing"}
	m.screen = screenConfirm
	return nil
}

// updateLeave handles the answer to the leave question: save keeps the person on the page, discard goes on.
func (m *Model) updateLeave(key tea.KeyPressMsg) tea.Cmd {
	switch {
	case m.keyIs(key, actSave):
		m.closeDialog()
		return m.saveCurrent()
	case m.keyIs(key, actDiscard):
		next := m.leave.next
		m.closeDialog()
		m.set.loaded = false
		m.loadForms()
		return next()
	case m.keyIs(key, actBack), m.keyIs(key, actNo):
		m.closeDialog()
	}
	return nil
}
