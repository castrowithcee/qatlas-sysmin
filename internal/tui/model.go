package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
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
	// screenConfirm is a yes/no question, drawn as a modal over the list where the terminal has room.
	screenConfirm
	// screenPrompt is a short input dialog, drawn like a question.
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
	prefs    uiprefs.Prefs
	redactor *redact.Redactor

	screen screen
	// title heads the list; table lays its entries out as columns.
	title string
	table table
	list  filterList
	// groups is the grouping of list, nil while it is flat; folds keeps the fold state of each group.
	groups *listGroups
	folds  map[string]bool
	// ask is the question of screenConfirm, dialog the input dialog of screenPrompt.
	ask    *question
	dialog *promptDialog
	// dd is the open list of a dropdown field (see dropdown.go).
	dd *dropdown

	// home is the base screen: screenList for the bare frame, screenArea once enableAreas ran.
	home screen
	// area is the current area of screenArea and zone the part of it that takes the keys; backend serves it;
	// svc is what the service last reported.
	area    area
	zone    zone
	backend Backend
	svc     svcState
	// ov is the passphrase overlay drawn as the prompt of screenPrompt, nil while there is none; ovSeq numbers
	// them.
	ov    *passEntry
	ovSeq int
	// confirm is the question of screenConfirm that waits for y; cred is the form of a credential, drawn as the
	// prompt of screenPrompt; both are nil while there is none.
	confirm *confirmAsk
	cred    *credForm
	// form is a short form of Library or Activity, drawn as the prompt of screenPrompt like cred.
	form *formDlg

	// The state of the areas: the rows of the overview, the pages of Settings, the popup over the screen, the
	// question before unsaved input is dropped; see overview.go, settings.go, popup.go.
	ovr overviewState
	acc accessState
	act activityState
	lib libraryState
	set settingsState
	// prj is the state of the Projects area; see projects.go.
	prj *projectsState
	pop *popup
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
	screenKeys string
	screenPos  string

	// termWidth and termHeight are what the terminal reported. width and height are the room of the
	// workspace inside the frame; every screen lays itself out in them.
	termWidth, termHeight int
	width, height         int

	// The click targets of the frame drawn last; see mouse.go.
	hits, frameHits []hit
	// zoneHits are the click targets of the zones, which rank beneath every other target; wheelHits are the
	// areas the wheel scrolls under the pointer.
	zoneHits    []hit
	wheelHits   []wheelHit
	footerSpans []footerSpan
	// footerLog collects the key lines a dialog draws itself, so its keys can be clicked as buttons.
	footerLog *[]footerLine
	// inModal and inPanel say that a dialog is drawn into a modal or a panel; backdrop, that the frame is
	// drawn only as the dimmed background of a modal; tooSmall, that the size notice replaces the workspace.
	inModal, inPanel, backdrop, tooSmall bool
	quitting                             bool
	// regroupAsked says the person quit to restart the interface under sudo -g sysmin; Run reports it.
	regroupAsked bool
	// updateTo is the release the person quit to install; Run reports it.
	updateTo  string
	wheelSeen wheelSeen
	// now is the clock of the wheel filter; nil means time.Now.
	now func() time.Time
}

// New returns the frame with an empty list. A nil redactor is read as one that knows no secret.
func New(prefs uiprefs.Prefs, redactor *redact.Redactor) *Model {
	if prefs == (uiprefs.Prefs{}) {
		prefs = uiprefs.Default()
	}
	if redactor == nil {
		redactor = &redact.Redactor{}
	}
	return &Model{prefs: prefs, redactor: redactor, list: newFilterList(func(name string) string { return name })}
}

// Init asks the terminal for its colours where the theme mixes from them.
func (m *Model) Init() tea.Cmd {
	if m.backend == nil {
		return terminalColourQuery(m.prefs.Theme)
	}
	return tea.Batch(terminalColourQuery(m.prefs.Theme), m.statusCmd(), tickCmd(), m.checkUpdate(false))
}

// Update handles one message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer m.layoutWorkspace()
	switch msg := msg.(type) {
	case keysMsg:
		var cmds []tea.Cmd
		for _, key := range msg {
			_, cmd := m.Update(key)
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
		m.takeTerminalColour(msg)
		return m, nil
	case tea.KeyPressMsg:
		return m, m.updateKey(m.moveKey(legacyKey(msg)))
	case statusMsg:
		return m, m.takeStatus(msg)
	case tickMsg:
		return m, m.takeTick()
	case verifiedMsg:
		return m, m.takeVerified(msg)
	case createdMsg:
		return m, m.takeCreated(msg)
	case changeDoneMsg:
		return m, m.takeChange(msg)
	case sessionEndedMsg:
		return m, m.takeSessionEnded(msg)
	case overviewMsg:
		m.takeOverview(msg)
		return m, nil
	case settingsMsg:
		m.takeSettings(msg)
		return m, nil
	case accessMsg:
		m.takeAccess(msg)
		return m, m.dataWant()
	case accDetailMsg:
		m.takeAccDetail(msg)
		return m, nil
	case credentialsMsg:
		m.takeCredentials(msg)
		return m, nil
	case channelsMsg:
		m.takeChannels(msg)
		return m, nil
	case channelTestMsg:
		m.takeChannelTest(msg)
		return m, nil
	case templatesMsg:
		m.takeTemplates(msg)
		return m, m.dataWant()
	case templateDetailMsg:
		m.takeTemplateDetail(msg)
		return m, nil
	case logMsg:
		return m, m.takeLog(msg)
	case verifyMsg:
		m.takeVerify(msg)
		return m, nil
	case runsMsg:
		m.takeRuns(msg)
		return m, nil
	case runLogMsg:
		m.takeRunLog(msg)
		return m, nil
	case projectsMsg:
		m.takeProjects(msg)
		return m, nil
	case updateMsg:
		m.takeUpdate(msg)
		return m, nil
	case projectsAnswer:
		return m, m.takeProjectsAnswer(msg)
	}
	return m, nil
}

// updateKey handles one key press: first the open dropdown and dialogs, then the list.
func (m *Model) updateKey(key tea.KeyPressMsg) tea.Cmd {
	if keyIs(key, actForceQuit) {
		if m.screen == screenArea && m.ov == nil && m.dd == nil {
			return m.guarded(m.quit)
		}
		return m.quit()
	}
	if m.dd != nil {
		return m.updateDropdown(key)
	}
	if m.ov != nil {
		return m.updateOverlay(key)
	}
	if m.projectPromptOpen() {
		return m.projectPromptKey(key)
	}
	if m.screen == screenPopup {
		return m.updatePopup(key)
	}
	if m.projectsDialogOpen() {
		return m.updateProjectDialog(key)
	}
	if m.screen == screenConfirm && m.leave != nil {
		return m.updateLeave(key)
	}
	if m.screen == screenConfirm && m.confirm != nil {
		return m.updateConfirm(key)
	}
	if m.screen == screenPrompt && m.cred != nil {
		return m.updateCred(key)
	}
	if m.screen == screenPrompt && m.form != nil {
		return m.updateForm(key)
	}
	if m.screen == screenArea {
		if keyIs(key, actLock) {
			return m.lockKey()
		}
		return m.updateArea(key)
	}
	switch m.screen {
	case screenConfirm, screenPrompt:
		if keyIs(key, actBack) || keyIs(key, actNo) {
			m.closeDialog()
		}
		return nil
	}
	if m.list.editing {
		switch {
		case keyIs(key, actEnter):
			m.list.stopFilter()
		case keyIs(key, actBack):
			m.list.clearFilter()
		case keyIs(key, actUp):
			m.list.move(-1)
		case keyIs(key, actDown):
			m.list.move(1)
		default:
			return m.list.updateFilter(key)
		}
		return nil
	}
	switch {
	case keyIs(key, actQuit):
		return m.quit()
	case keyIs(key, actSearch) || keyIs(key, actFilter):
		m.list.startFilter()
	case keyIs(key, actBack):
		m.list.clearFilter()
	case keyIs(key, actUp):
		m.list.move(-1)
	case keyIs(key, actDown):
		m.list.move(1)
	case keyIs(key, actPageUp), keyIs(key, actPageDown):
		start, end := m.listWindow()
		m.list.page(key, start, end)
	case keyIs(key, actHome):
		m.list.jump(-len(m.list.matches))
	case keyIs(key, actEnd):
		m.list.jump(len(m.list.matches))
	case keyIs(key, actCopy):
		return m.copyCurrent()
	default:
		m.foldKey(key)
	}
	return nil
}

// quit ends the program.
func (m *Model) quit() tea.Cmd {
	m.quitting = true
	return tea.Quit
}

// regroup quits, after the same question as quit about unsaved changes, so that the interface starts again
// under sudo -g sysmin and carries the group without a new login.
func (m *Model) regroup() tea.Cmd {
	return m.guarded(func() tea.Cmd {
		m.regroupAsked = true
		return m.quit()
	})
}

// closeDialog closes the question or input dialog and returns to the list.
func (m *Model) closeDialog() {
	m.screen, m.ask, m.dialog, m.leave, m.confirm = m.home, nil, nil, nil, nil
}

// clearMessages forgets the status and the error of the last action.
func (m *Model) clearMessages() { m.status, m.fail = "", "" }

// selected is the name under the cursor of the list.
func (m *Model) selected() (string, bool) {
	name, ok := m.list.selected()
	if !ok || isGroupRow(name) {
		return "", false
	}
	return name, true
}

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
	if styles.filled && view != "" {
		view = paintCanvas(view, styles.canvasPrefix(), m.termWidth, m.termHeight)
	}
	return shown(view), cursor
}

// renderFrame is the frame before the base colour of the theme is applied.
func (m *Model) renderFrame() string {
	if m.quitting {
		return ""
	}
	m.hits, m.frameHits, m.zoneHits, m.wheelHits, m.tooSmall = nil, nil, nil, nil, false
	if m.dd != nil {
		m.dd.placed = false
	}
	m.screenKeys, m.screenPos = "", ""
	m.layoutWorkspace()
	if m.terminalTooSmall() {
		m.tooSmall = true
		return m.resizeView()
	}
	if m.screen == screenPopup && m.pop != nil {
		return m.popupFrame()
	}
	if view, ok := m.modalView(); ok {
		return view
	}
	if m.questionScreen() || m.promptScreen() {
		// A dialog that has no room for a modal fills the workspace; its keys stand in the footer, as buttons.
		view := m.frame(m.workspaceView())
		m.footerHits()
		m.hitPickerWheel(0, m.workspaceTop(), m.termWidth, m.termHeight)
		return view
	}
	return m.overlayDropdown(m.frame(m.workspaceView()))
}

// workspaceView is the screen of the workspace.
func (m *Model) workspaceView() string {
	switch {
	case m.screen == screenArea:
		return m.boundView(m.areaView())
	case m.questionScreen():
		return m.boundKeepingKeys(m.framedQuestion(m.questionView), m.width, m.height) + m.notes()
	case m.promptScreen():
		d, _ := m.prompt()
		return m.boundKeepingKeys(m.promptView(d), m.width, m.height)
	}
	return m.boundView(m.listView())
}

func (m *Model) terminalTooSmall() bool {
	return m.termWidth < minimumWidth || m.termHeight < minimumHeight
}

// resizeView is the notice that replaces the screen while the terminal is smaller than the editor needs.
func (m *Model) resizeView() string {
	way := "q quit"
	if m.screen != m.home {
		way = "{back} leave"
	}
	// The way out comes first, so it is the last thing a terminal of a single row loses.
	lines := []string{
		styles.key.Render(footerKeys(way)),
		styles.warning.Render(warnMark + " " + tr(tResizeTitle)),
		styles.hint.Render("  " + tr(tResizeNeed, minimumWidth, minimumHeight)),
		styles.hint.Render("  " + tr(tResizeNow, m.termWidth, m.termHeight)),
	}
	return m.boundTo(strings.Join(lines, "\n"), m.termWidth, m.termHeight)
}

// notes are the lines under every screen: what is still running, and what the last action did.
func (m *Model) notes() string {
	out := ""
	if m.busy != "" {
		out += "\n\n" + m.wrapped(styles.hint, tr(tNoteRunning, m.busy))
	}
	if m.fail != "" {
		out += "\n\n" + m.wrapped(styles.fail, tr(tNoteError, m.fail))
	} else if m.status != "" {
		out += "\n\n" + m.wrapped(styles.hint, m.status)
	}
	return out
}

// searchLine shows the filter of l behind the given label.
func (m *Model) searchLine(l *filterList, label string) string {
	return m.searchLineIn(m.usable(0), l, label)
}

// searchLineIn is searchLine within the first room cells of the workspace.
func (m *Model) searchLineIn(room int, l *filterList, label string) string {
	prefix, width := m.fitIn(room, label)
	l.input.Placeholder = tr(tLogSearchPlaceholder)
	if !l.editing {
		return prefix + truncateCells(l.query(), width)
	}
	return prefix + inputView(fitInput(l.input, width))
}

// wrap is i taken into 0..n-1: past either end it comes round.
func wrap(i, n int) int {
	if n <= 0 {
		return 0
	}
	return ((i % n) + n) % n
}
