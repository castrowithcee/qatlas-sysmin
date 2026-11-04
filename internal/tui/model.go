package tui

import (
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/redact"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// The defaults bridge the first frame until the terminal reports its real size, which it does before a
// person can press a key; they are generous so that frame is not squeezed.
const (
	defaultWidth  = 120
	minimumWidth  = 40
	minimumHeight = 14
)

// Below these sizes the table of a list screen is drawn without its frame, which costs a row and four cells.
const (
	tableFrameMinWidth  = 60
	tableFrameMinHeight = 16
)

// hintIndent is where the hint under a row starts.
const hintIndent = "    "

// screen is what the workspace shows.
type screen int

const (
	// screenList is a filterable list, the base of every other screen.
	screenList screen = iota
	// screenConfirm is a question or message of the kit's dialog.
	screenConfirm
	// screenPrompt is a form, drawn by the kit, with the editor open over it.
	screenPrompt
	// screenArea is the area screen of the service interface: the tab line with the current area. It is
	// the base of the dialogs in place of screenList where the interface runs against a service.
	screenArea
	// screenPopup is the help, the info or the command line over the area screen.
	screenPopup
)

// Model is the state of the editor frame: the list in the workspace and the dialog over it. Areas of the
// editor put their rows into list and their dialogs into ask or dialog.
type Model struct {
	prefs kitprefs.Prefs
	// acting is the action being run for a click or a command, which the key handlers see as its key press.
	acting   action
	redactor *redact.Redactor
	// ui holds the keys the screens read.
	ui *uiContext

	screen screen
	// title heads the list of the bare frame, which base holds.
	title string
	base  dataPage
	// dlg is the kit dialog of screenConfirm and dlgSpec what it asks, nil while there is none.
	dlg     *kittui.Dialog
	dlgSpec kittui.DialogSpec

	// home is the base screen: screenList for the bare frame, screenArea once enableAreas ran.
	home screen
	// area is the current area of screenArea and zone the part of it that takes the keys; backend serves it;
	// svc is what the service last reported.
	area    area
	zone    kittui.FocusZone
	backend Backend
	svc     svcState
	// confirm is the question of screenConfirm that waits for y, nil while there is none.
	confirm *confirmAsk
	// form is a short form, the screen of screenPrompt, nil while there is none.
	form *formDlg

	// The state of the areas: the rows of the overview, the pages of Settings, the popup over the screen, the
	// question before unsaved input is dropped; see overview.go, settings.go, popup.go.
	ovr overviewState
	act activityState
	// hst is the Host area and hostSrc what it reads; see host.go.
	hst     hostState
	hostSrc HostSource
	// hostRun builds the sudo call of a change, hostReg describes the actions, hostExec hands the terminal to a
	// command (tea.ExecProcess unless a test stands in), and hostIn and hostOut are the real terminal.
	hostRun         HostRunner
	hostReg         *hostchange.Registry
	hostExec        func(*exec.Cmd, tea.ExecCallback) tea.Cmd
	hostIn, hostOut *os.File
	// hostBg runs a command that needs no terminal (exec.Cmd.Run unless a test stands in).
	hostBg func(*exec.Cmd) error
	// folderCount counts the entries a folder change touches (hostchange.CountFolderEntries unless a test stands in).
	folderCount func(path string, recursive bool) (int, error)
	set         settingsState
	pop         *popup
	// leave is the question before unsaved input is dropped, nil while there is none.
	leave *leaveAsk
	// prefsPath is the preferences file Settings writes; empty means there is none to write.
	prefsPath string
	// version is the running program; releases looks up the releases. Nil means the lookup is not offered.
	version  string
	releases ReleaseCheck

	status string
	fail   string
	busy   string

	// screenKeys and screenPos are what the screen drawn last hands to the footer: its key line and its
	// position, such as "3/12".
	screenKeys []kittui.Hint
	screenPos  string

	// termWidth and termHeight are what the terminal reported. width and height are the room of the
	// workspace inside the frame; every screen lays itself out in them.
	termWidth, termHeight int
	width, height         int

	// mt are the targets of the frame being drawn and mouse the map of the frame drawn last; see mouse.go.
	mt    mouseTargets
	mouse *kittui.MouseMap
	// footerTargets are the keys of the footer drawn last, clickable where they answer a dialog.
	footerTargets []kittui.Target
	// backdrop says the frame is drawn only as the dimmed background of a dialog; tooSmall, that the size
	// notice replaces the workspace.
	backdrop, tooSmall bool
	quitting           bool
	// regroupAsked says the person quit to restart the interface under sudo -g qsysmin; Run reports it.
	regroupAsked bool
	// denied is the error that took the access away while the interface ran; Run returns it after the quit.
	denied error
	// updateTo is the release the person quit to install; Run reports it.
	updateTo string
	// now is the clock of the editor; nil means time.Now.
	now func() time.Time
	// hostUID is the user running the interface, for the early message about taking admin from oneself.
	hostUID func() int
	// tickEvery is how often the header looks at the service, updateTimeout how long a release lookup may
	// take; tests shorten them.
	tickEvery, updateTimeout time.Duration
}

// newModel returns the frame with an empty list and the default keys, in the language and the theme of prefs,
// for a terminal of the given colour profile and character set. Preferences without a theme, such as the zero
// value, are read as the defaults; a nil redactor as one that knows no secret. A theme this version does not
// have draws the terminal theme.
func newModel(prefs kitprefs.Prefs, redactor *redact.Redactor, profile kittui.ColorProfile, charset kittui.CharsetMode) *Model {
	if redactor == nil {
		redactor = &redact.Redactor{}
	}
	ui := newUIContext(profile, charset)
	if prefs.Theme == "" {
		prefs = kitprefs.Default(ui.prefsSchema())
	}
	m := &Model{prefs: prefs, redactor: redactor, ui: ui, hostUID: os.Getuid, tickEvery: tickEvery,
		updateTimeout: updateTimeout}
	m.ui.setLanguage(kitprefs.Language(prefs.Language))
	m.ui.setTheme(prefs.Theme)
	m.ovr.reset()
	return m
}

// Init asks the terminal for its colours where the theme mixes from them, and for what the start area shows;
// the overview reads its data once the service has answered.
func (m *Model) Init() tea.Cmd {
	if m.backend == nil {
		return m.ui.terminalColorQuery()
	}
	cmds := []tea.Cmd{m.ui.terminalColorQuery(), m.statusCmd(), m.tickCmd(), m.checkUpdate(false)}
	if m.area != areaOverview {
		cmds = append(cmds, m.enterPage())
	}
	return tea.Batch(cmds...)
}

// Update handles one message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer m.layoutWorkspace()
	switch msg := msg.(type) {
	case kittui.InputBatch:
		var cmds []tea.Cmd
		for _, in := range msg {
			_, cmd := m.Update(in)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	case tea.MouseMsg:
		return m, m.updateMouse(msg)
	case tea.WindowSizeMsg:
		// A zero dimension is also how focused tests report only the dimension they exercise. Real size
		// messages carry both; keep the last known value until a non-zero replacement arrives.
		if msg.Width != 0 {
			m.termWidth = max(msg.Width, 1)
		}
		if msg.Height != 0 {
			m.termHeight = max(msg.Height, 1)
		}
		return m, m.dataWant()
	case tea.BackgroundColorMsg, tea.ForegroundColorMsg:
		m.ui.takeTerminalColor(msg)
		return m, nil
	case actionMsg:
		return m, m.runAction(msg.act, m.updateKey)
	case tea.KeyPressMsg:
		return m, m.updateKey(m.moveKey(legacyKey(msg)))
	case tea.PasteMsg:
		// A paste travels through the key handlers as one key no binding matches.
		return m, m.updateKey(pasteKey(msg.Content))
	case statusMsg:
		return m, m.takeStatus(msg)
	case tickMsg:
		return m, m.takeTick()
	case overviewMsg:
		m.takeOverview(msg)
		return m, nil
	case hostUsersMsg:
		m.takeHostUsers(msg)
		return m, nil
	case hostGroupsMsg:
		m.takeHostGroups(msg)
		return m, nil
	case hostFolderMsg:
		m.takeHostFolder(msg)
		return m, nil
	case hostRanMsg:
		return m, m.takeHostRan(msg)
	case logMsg:
		return m, m.takeLog(msg)
	case verifyMsg:
		m.takeVerify(msg)
		return m, nil
	case updateMsg:
		m.takeUpdate(msg)
		return m, nil
	}
	return m, nil
}

// updateKey handles one key press: first the open dropdown and dialogs, then the list.
func (m *Model) updateKey(key tea.KeyPressMsg) tea.Cmd {
	if m.keyIs(key, actForceQuit) {
		if m.screen == screenArea || m.screen == screenPrompt && m.form != nil {
			return m.guarded(m.quit)
		}
		return m.quit()
	}
	if m.screen == screenPopup {
		return m.updatePopup(key)
	}
	if m.screen == screenConfirm && m.leave != nil && m.dlg != nil {
		return m.updateLeave(key)
	}
	if m.screen == screenConfirm && m.confirm != nil && m.dlg != nil {
		return m.updateDialog(key)
	}
	if m.screen == screenPrompt && m.form != nil {
		return m.updateForm(key)
	}
	if m.screen == screenArea {
		return m.updateArea(key)
	}
	switch m.screen {
	case screenConfirm, screenPrompt:
		if m.keyIs(key, actBack) || m.keyIs(key, actNo) {
			m.closeDialog()
		}
		return nil
	}
	p := &m.base
	if p.filtering() {
		return m.filterKey(p, key)
	}
	switch {
	case m.keyIs(key, actQuit):
		return m.quit()
	case m.keyIs(key, actCopy):
		return m.copyCurrent()
	}
	cmd, _ := m.pageKey(p, key)
	return cmd
}

// quit ends the program.
func (m *Model) quit() tea.Cmd {
	m.quitting = true
	return tea.Quit
}

// regroup quits, after the same question as quit about unsaved changes, so that the interface starts again
// under sudo -g qsysmin and carries the group without a new login.
func (m *Model) regroup() tea.Cmd {
	return m.guarded(func() tea.Cmd {
		m.regroupAsked = true
		return m.quit()
	})
}

// closeDialog closes the question or input dialog and returns to the list.
func (m *Model) closeDialog() {
	m.screen, m.leave, m.confirm = m.home, nil, nil
	m.dlg, m.dlgSpec = nil, kittui.DialogSpec{}
}

// clearMessages forgets the status and the error of the last action.
func (m *Model) clearMessages() { m.status, m.fail = "", "" }

// selected is the name of the marked row of the bare list.
func (m *Model) selected() (string, bool) { return m.base.current() }

// View draws the frame on the alternate screen, so it stays a fixed window and the shell is left as it was.
func (m *Model) View() tea.View {
	frame, cursor := m.renderCursor()
	v := tea.NewView(frame)
	v.Cursor = cursor
	v.AltScreen = true
	v.MouseMode = m.mouseMode()
	return v
}

// render draws the frame and applies the base colour of the theme.
func (m *Model) render() string {
	frame, _ := m.renderCursor()
	return frame
}

// renderCursor is render together with the cell of the terminal cursor, nil without a focused text field.
func (m *Model) renderCursor() (string, *tea.Cursor) {
	view, cursor := takeCursor(m.renderFrame())
	if view != "" {
		view = m.ui.styles.Paint(view, m.termWidth, m.termHeight)
	}
	return m.ui.shown(view), cursor
}

// renderFrame is the frame before the base colour of the theme is applied.
func (m *Model) renderFrame() string {
	if m.quitting {
		return ""
	}
	m.mt, m.tooSmall = mouseTargets{}, false
	view := m.drawFrame()
	m.mouse = kittui.NewMouseMap(m.mt.targets, m.mouseEnabled() && !m.tooSmall)
	return view
}

// drawFrame draws the frame of the screen shown and collects its targets. The keys of a screen decide whether
// the footer wraps onto a second row, and the screen learns them only while it draws into a workspace laid out
// for the footer before; where they take another number of rows, it is drawn again for the right one.
func (m *Model) drawFrame() string {
	m.screenKeys, m.screenPos = nil, ""
	m.layoutWorkspace()
	laidOut := m.footerRows()
	view := m.drawScreen()
	if m.tooSmall || m.footerRows() == laidOut {
		return view
	}
	m.mt = mouseTargets{}
	m.layoutWorkspace()
	return m.drawScreen()
}

// drawScreen draws the screen into the workspace as it is laid out.
func (m *Model) drawScreen() string {
	if m.terminalTooSmall() {
		m.tooSmall = true
		return m.resizeView()
	}
	if m.screen == screenPopup && m.pop != nil {
		return m.popupFrame()
	}
	if view, ok := m.filterModalFrame(); ok {
		return view
	}
	if view, ok := m.dialogFrame(); ok {
		return view
	}
	if view, ok := m.pickFrame(); ok {
		return view
	}
	if view, ok := m.formFrame(); ok {
		return view
	}
	if view, ok := m.keyEditFrame(); ok {
		return view
	}
	return m.frame(m.workspaceView())
}

// workspaceView is the screen of the workspace.
func (m *Model) workspaceView() string {
	switch {
	case m.screen == screenArea:
		return m.boundView(m.areaView())
	}
	return m.boundView(m.listView())
}

// listView draws the bare list: its title and its table.
func (m *Model) listView() string {
	p := &m.base
	keys := keyLine(navHint(tHintMove, actUp, actDown), hint(tHintFilter, actSearch), hint(tHintQuit, actQuit))
	if p.filtering() {
		keys = keyLine(hint(tHintTypeToFilter), navHint(tHintMove, actUp, actDown), hint(tHintKeepFilter, actEnter),
			hint(tHintClearFilter, actBack))
	}
	m.keyHint(keys)
	position := ""
	if p.list != nil {
		position = p.list.Position()
	}
	m.screenPos = position
	spec := dataSpec{title: m.title, gap: true, note: m.tr(tListEmpty)}
	return strings.Join(m.tableBlock(p, spec, m.usable(0), m.height, m.workspaceTop(), position, true), "\n")
}

func (m *Model) terminalTooSmall() bool {
	return m.termWidth < minimumWidth || m.termHeight < minimumHeight
}

// resizeView is the notice that replaces the screen while the terminal is smaller than the editor needs.
func (m *Model) resizeView() string {
	way := hint(tHintQuit, actQuit)
	if m.screen != m.home {
		way = hint(tHintLeave, actBack)
	}
	// The way out comes first, so it is the last thing a terminal of a single row loses.
	lines := []string{
		m.ui.st(kittui.StyleKey).Render(m.hintText(way)),
		m.ui.st(kittui.StyleWarn).Render(m.ui.styles.Glyph(kittui.GlyphWarning) + " " + m.tr(tResizeTitle)),
		m.ui.st(kittui.StyleHint).Render("  " + m.tr(tResizeNeed, minimumWidth, minimumHeight)),
		m.ui.st(kittui.StyleHint).Render("  " + m.tr(tResizeNow, m.termWidth, m.termHeight)),
	}
	return m.boundTo(strings.Join(lines, "\n"), m.termWidth, m.termHeight)
}
