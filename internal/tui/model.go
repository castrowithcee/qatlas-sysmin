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
	// area is the current area of screenArea; backend serves it; svc is what the service last reported.
	area    area
	backend Backend
	svc     svcState
	// ov is the passphrase overlay drawn as the prompt of screenPrompt, nil while there is none; ovSeq numbers
	// them.
	ov    *passEntry
	ovSeq int

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
	footerSpans     []footerSpan
	// footerLog collects the key lines a dialog draws itself, so its keys can be clicked as buttons.
	footerLog *[]footerLine
	// inModal and inPanel say that a dialog is drawn into a modal or a panel; backdrop, that the frame is
	// drawn only as the dimmed background of a modal; tooSmall, that the size notice replaces the workspace.
	inModal, inPanel, backdrop, tooSmall bool
	quitting                             bool
	wheelSeen                            wheelSeen
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
	return tea.Batch(terminalColourQuery(m.prefs.Theme), m.statusCmd(), tickCmd())
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
		return m, nil
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
	}
	return m, nil
}

// updateKey handles one key press: first the open dropdown and dialogs, then the list.
func (m *Model) updateKey(key tea.KeyPressMsg) tea.Cmd {
	if keyIs(key, actForceQuit) {
		return m.quit()
	}
	if m.dd != nil {
		return m.updateDropdown(key)
	}
	if m.ov != nil {
		return m.updateOverlay(key)
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

// closeDialog closes the question or input dialog and returns to the list.
func (m *Model) closeDialog() {
	m.screen, m.ask, m.dialog = m.home, nil, nil
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

// View draws the frame.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.MouseMode = m.mouseMode()
	return v
}

// render draws the frame and applies the base colour of the theme.
func (m *Model) render() string {
	view, _ := takeCursor(m.renderFrame())
	if styles.filled && view != "" {
		view = paintCanvas(view, styles.canvasPrefix(), m.termWidth, m.termHeight)
	}
	return shown(view)
}

// renderFrame is the frame before the base colour of the theme is applied.
func (m *Model) renderFrame() string {
	if m.quitting {
		return ""
	}
	m.hits, m.frameHits, m.tooSmall = nil, nil, false
	if m.dd != nil {
		m.dd.placed = false
	}
	m.screenKeys, m.screenPos = "", ""
	m.layoutWorkspace()
	if m.terminalTooSmall() {
		m.tooSmall = true
		return m.resizeView()
	}
	if view, ok := m.modalView(); ok {
		return view
	}
	if m.questionScreen() || m.promptScreen() {
		// A dialog that has no room for a modal fills the workspace; its keys stand in the footer, as buttons.
		view := m.frame(m.workspaceView())
		m.footerHits()
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

// wrap is i taken into 0..n-1: past either end it comes round, or, while the wheel presses the key, stops.
func wrap(i, n int) int {
	if n <= 0 {
		return 0
	}
	if wheelClamp {
		return min(max(i, 0), n-1)
	}
	return ((i % n) + n) % n
}
