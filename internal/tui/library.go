package tui

import (
	"errors"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// Library has one page per kind of reusable thing: Credentials here, Channels in channels.go and Templates in
// templates.go. A page brings its table through dataPage and its keys through the key function of the page.
const (
	libCredentials = iota
	libChannels
	libTemplates
	libCount
)

func libTitle(sub int) string {
	return tr([...]textID{tSubCredentials, tSubChannels, tSubTemplates}[sub])
}

// libKey is the English name of a Library page, the one commands are given by.
func libKey(sub int) string { return [...]string{"credentials", "channels", "templates"}[sub] }

// libraryState is the Library area: the page shown and the state of each page.
type libraryState struct {
	sub   int
	cred  credState
	chans chanState
	tmpls tmplState
}

// credState is the Credentials page. It holds names, kinds and uses, never a value.
type credState struct {
	page  dataPage
	items map[string]control.CredentialSummary
	err   string
	have  bool
	// problems are the uses that kept the last removal from happening.
	problems []string
}

type credentialsMsg struct {
	list []control.CredentialSummary
	err  error
}

// setLibSub shows page sub of Library.
func (m *Model) setLibSub(sub int) tea.Cmd {
	m.lib.sub = (sub + libCount) % libCount
	m.closeDetails()
	m.clearMessages()
	return m.enterPage()
}

// libraryBody is Library under the tab line: the strip of its pages and the page.
func (m *Model) libraryBody() string {
	words := make([]string, libCount)
	for i := range words {
		words[i] = libTitle(i)
	}
	strip, cells := tabStrip(words, m.lib.sub, false)
	top := m.workspaceTop() + 2
	for i, c := range cells {
		m.addHit(c[0], top, c[1], top+1, func() tea.Cmd { return m.setLibSub(i) })
	}
	var body string
	switch m.lib.sub {
	case libChannels:
		body = m.dataBody(&m.lib.chans.page, m.chanSpec(), top+2)
	case libTemplates:
		body = m.dataBody(&m.lib.tmpls.page, m.tmplSpec(), top+2)
	default:
		body = m.dataBody(&m.lib.cred.page, m.credSpec(), top+2)
		if lines := m.lib.cred.problems; len(lines) > 0 {
			body += "\n" + strings.Join(lines, "\n")
		}
	}
	return strip + "\n\n" + body
}

func credColumns() []column {
	return []column{
		{title: tr(tColName)},
		{title: tr(tColKind)},
		{title: tr(tColUses), flex: true},
	}
}

// useState names where a credential is used from: the current configuration, the approved state, or both.
func useState(u control.CredentialUse) string {
	switch {
	case u.Current && u.Approved:
		return tr(tUseCurrent) + ", " + tr(tUseApproved)
	case u.Approved:
		return tr(tUseApproved)
	}
	return tr(tUseCurrent)
}

// credentialsCmd reads the credentials; the listing never carries a value.
func (m *Model) credentialsCmd() tea.Cmd {
	if m.backend == nil || !m.serviceUp() {
		return nil
	}
	b := m.backend
	return func() tea.Msg {
		list, err := b.Credentials()
		return credentialsMsg{list, err}
	}
}

func (m *Model) takeCredentials(msg credentialsMsg) {
	c := &m.lib.cred
	c.err, c.have = "", msg.err == nil
	if msg.err != nil {
		c.err, msg.list = errorText(msg.err), nil
	}
	c.items = map[string]control.CredentialSummary{}
	rows := map[string][]string{}
	var names []string
	for _, s := range msg.list {
		c.items[s.Name] = s
		names = append(names, s.Name)
		var uses []string
		for _, u := range s.Uses {
			uses = append(uses, u.Name+" ("+useState(u)+")")
		}
		cell := strings.Join(uses, ", ")
		if cell == "" {
			cell = tr(tCredUnused)
		}
		rows[s.Name] = []string{s.Name, s.Kind, cell}
	}
	c.page.cols = credColumns()
	c.page.setRows(names, rows)
	if f := m.form; f != nil && f.refresh != nil {
		f.refresh(m, f)
	}
}

func (m *Model) credSpec() dataSpec {
	empty := tr(tCredEmpty)
	if m.lib.cred.err != "" {
		empty = m.lib.cred.err
	}
	if m.vaultState() == control.VaultLocked {
		empty = tr(tErrLocked)
	}
	return dataSpec{title: tr(tSubCredentials), empty: empty, lines: m.credLines}
}

// credLines are the lines of one tab of a credential: name, kind, id and uses, never a value.
func (m *Model) credLines(name string, tab, width int) []string {
	c, ok := m.lib.cred.items[name]
	if !ok {
		return nil
	}
	switch tab {
	case tabOverview:
		lines := factLines([][2]string{
			{tr(tColName), m.diffText(c.Name)},
			{tr(tColKind), c.Kind},
			{tr(tLblID), m.diffText(c.ID)},
			{tr(tColUses), strconv.Itoa(len(c.Uses))},
			{tr(tLblValue), tr(tValSetHidden)},
		}, width)
		return lines
	case tabDetails:
		if len(c.Uses) == 0 {
			return []string{styles.hint.Render(tr(tCredUnused))}
		}
		var lines []string
		for _, u := range c.Uses {
			lines = append(lines, wrapIndented(lipgloss.NewStyle(), "", u.Kind+" "+m.diffText(u.Name)+"  ("+useState(u)+")", width)...)
		}
		return lines
	}
	lines := []string{"name: " + m.diffText(c.Name), "kind: " + c.Kind, "id: " + m.diffText(c.ID), "uses:"}
	for _, u := range c.Uses {
		lines = append(lines, "  - kind: "+u.Kind, "    name: "+m.diffText(u.Name),
			"    current: "+strconv.FormatBool(u.Current), "    approved: "+strconv.FormatBool(u.Approved))
	}
	return lines
}

// libraryKeys is the key line of Library.
func (m *Model) libraryKeys() []string {
	switch m.lib.sub {
	case libChannels:
		return m.chanKeys()
	case libTemplates:
		return m.tmplKeys()
	}
	if m.lib.cred.page.open {
		return []string{"left/right tab", "up/down scroll", "e edit", "d remove", "{back} close"}
	}
	return []string{"left/right page", "up/down move", "enter details", "n new", "e edit", "d remove", "/ filter"}
}

// libraryKey handles the keys of Library that the page does not.
func (m *Model) libraryKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch m.lib.sub {
	case libChannels:
		return m.chanKey(key)
	case libTemplates:
		return m.tmplKey(key)
	}
	m.lib.cred.problems = nil
	switch {
	case keyIs(key, actNew):
		return m.openCredForm(false), true
	case keyIs(key, actEdit):
		return m.openCredForm(true), true
	case keyIs(key, actDelete):
		return m.credRemove(), true
	case keyIs(key, actReveal):
		m.clearMessages()
		m.status = tr(tCredNoReveal)
		return nil, true
	}
	return nil, false
}

// credRemove asks, then removes the marked credential.
func (m *Model) credRemove() tea.Cmd {
	name, ok := m.lib.cred.page.current()
	if !ok {
		return nil
	}
	m.clearMessages()
	c := m.lib.cred.items[name]
	q := question{title: tr(tCredRemoveTitle, m.diffText(name)), consequence: tr(tCredRemoveConsequence), warn: true, danger: true,
		keys: "y remove · n/{back} cancel"}
	if n := len(c.Uses); n > 0 {
		q.consequence = tr(tCredRemoveUsed, n)
	}
	m.askConfirm(q, func() tea.Cmd {
		var res control.CredentialResult
		return m.requireAdmin(change{
			call: func(b Backend) (err error) { res, err = b.RemoveCredential(name); return err },
			done: func(m *Model, err error) tea.Cmd {
				switch {
				case err == nil:
					m.status = tr(tCredRemoved)
					if len(res.Pending) > 0 {
						m.status = tr(tCredChangedPending, len(res.Pending))
					}
				case isCode(err, control.CodeInUse):
					m.lib.cred.problems = m.inUseLines(err)
				}
				return tea.Batch(m.statusCmd(), m.credentialsCmd(), m.accessCmd())
			},
		})
	})
	return nil
}

// inUseLines are the uses a refused removal lists, as lines under the table.
func (m *Model) inUseLines(err error) []string {
	var ce *control.Error
	if !errors.As(err, &ce) {
		return nil
	}
	lines := []string{}
	for i, p := range ce.Problems {
		if i == 6 {
			lines = append(lines, "  "+styles.hint.Render(tr(tAccMore, len(ce.Problems)-6)))
			break
		}
		lines = append(lines, "  "+styles.warning.Render(warnMark)+" "+truncateCells(m.diffText(p.Message), max(m.usable(0)-6, 8)))
	}
	return lines
}

// credForm is the overlay that sets a credential: name, kind and one masked field per field of the kind. A
// value is typed here and nowhere else; it leaves the fields when the form is submitted.
type credForm struct {
	// edit says the form replaces the values of an existing credential, whose name and kind stay.
	edit    bool
	name    textinput.Model
	kinds   []string
	kind    int
	fields  []string
	inputs  []textinput.Model
	focus   int
	problem string
}

// settableKinds are the kinds the form offers: the ones whose value fits one line.
func settableKinds() []string {
	var kinds []string
	for _, k := range control.CredentialKinds {
		if !k.Multiline {
			kinds = append(kinds, k.Name)
		}
	}
	return kinds
}

// fieldsOf are the fields a kind holds: the plain ones, then the secret one.
func fieldsOf(kind string) []string {
	k, _ := control.KindOf(kind)
	return append(append([]string(nil), k.Plain...), k.Secret)
}

// openCredForm opens the form for a new credential, or for new values of the marked one.
func (m *Model) openCredForm(edit bool) tea.Cmd {
	m.clearMessages()
	f := &credForm{edit: edit, kinds: settableKinds(), name: keyInput("")}
	if edit {
		name, ok := m.lib.cred.page.current()
		if !ok {
			return nil
		}
		c := m.lib.cred.items[name]
		if ck, known := control.KindOf(c.Kind); !known || ck.Multiline {
			m.fail = tr(tCredMultiline)
			return nil
		}
		f.name.SetValue(c.Name)
		for i, k := range f.kinds {
			if k == c.Kind {
				f.kind = i
			}
		}
	}
	f.rebuild()
	f.focusRow(0)
	if edit {
		f.focusRow(2)
	}
	m.cred, m.screen = f, screenPrompt
	return nil
}

// rebuild makes one empty masked field per field of the kind.
func (f *credForm) rebuild() {
	f.fields = fieldsOf(f.kinds[f.kind])
	f.inputs = make([]textinput.Model, len(f.fields))
	for i := range f.inputs {
		f.inputs[i] = maskedInput()
	}
}

func (f *credForm) rows() int { return 2 + len(f.fields) }

// focusRow moves the focus to a row; the name is row 0, the kind row 1, the fields follow.
func (f *credForm) focusRow(i int) {
	f.name.Blur()
	for j := range f.inputs {
		f.inputs[j].Blur()
	}
	f.focus = i
	switch {
	case i == 0:
		f.name.Focus()
	case i >= 2:
		f.inputs[i-2].Focus()
	}
}

// step moves the focus by one row, over the rows that can be edited.
func (f *credForm) step(by int) {
	first := 0
	if f.edit {
		first = 2
	}
	n := f.rows() - first
	f.focusRow(first + ((f.focus-first+by)%n+n)%n)
}

// wipe empties every field.
func (f *credForm) wipe() {
	for i := range f.inputs {
		f.inputs[i].Reset()
	}
	f.name.Reset()
}

// closeCred wipes the form and leaves it.
func (m *Model) closeCred() {
	if m.cred != nil {
		m.cred.wipe()
	}
	m.cred = nil
	m.screen = m.home
}

// updateCred handles one key of the form.
func (m *Model) updateCred(key tea.KeyPressMsg) tea.Cmd {
	f := m.cred
	f.problem = ""
	switch {
	case keyIs(key, actBack):
		m.closeCred()
		m.status = tr(tCancelled)
		return nil
	case keyIs(key, actSave):
		return m.submitCred()
	case keyIs(key, actEnter):
		if f.focus < f.rows()-1 {
			f.step(1)
			return nil
		}
		return m.submitCred()
	case keyIs(key, actNext), keyIs(key, actDown):
		f.step(1)
		return nil
	case keyIs(key, actPrev), keyIs(key, actUp):
		f.step(-1)
		return nil
	}
	var cmd tea.Cmd
	switch {
	case f.focus == 0:
		f.name, cmd = f.name.Update(inputMsg(key))
	case f.focus == 1:
		if !f.edit && (keyIs(key, actLeft) || keyIs(key, actRight) || keyIs(key, actSpace)) {
			by := 1
			if keyIs(key, actLeft) {
				by = -1
			}
			f.kind = (f.kind + by + len(f.kinds)) % len(f.kinds)
			f.rebuild()
		}
	default:
		f.inputs[f.focus-2], cmd = f.inputs[f.focus-2].Update(inputMsg(key))
	}
	return cmd
}

// submitCred takes the typed values out of the fields and hands them to the service. The fields are empty
// from here on, whatever the outcome; the values are known to the redactor before they leave.
func (m *Model) submitCred() tea.Cmd {
	f := m.cred
	name, kind := strings.TrimSpace(f.name.Value()), f.kinds[f.kind]
	fields := make(map[string]string, len(f.fields))
	empty := name == ""
	for i, field := range f.fields {
		v := f.inputs[i].Value()
		fields[field] = v
		empty = empty || v == ""
	}
	if empty {
		f.problem = tr(tCredFieldEmpty)
		return nil
	}
	for _, v := range fields {
		m.redactor.Add(v)
	}
	m.closeCred()
	params := control.CredentialSetParams{Name: name, Kind: kind, Fields: fields}
	var res control.CredentialResult
	return m.requireAdmin(change{
		call: func(b Backend) (err error) { res, err = b.SetCredential(params); return err },
		done: func(m *Model, err error) tea.Cmd {
			switch {
			case err == nil && len(res.Pending) > 0:
				m.status = tr(tCredChangedPending, len(res.Pending))
			case err == nil:
				m.status = tr(tCredStored)
			case isCode(err, control.CodeInvalid):
				m.fail += m.problemsOf(err)
			}
			return tea.Batch(m.statusCmd(), m.credentialsCmd(), m.accessCmd())
		},
	})
}

// problemsOf lists the findings of a refused input, made safe, behind a colon.
func (m *Model) problemsOf(err error) string {
	var ce *control.Error
	if !errors.As(err, &ce) || len(ce.Problems) == 0 {
		return ""
	}
	return ": " + m.diffText(problemText(ce.Problems))
}

// credDialog is the form as the dialog the modal draws. Masked fields show dots; a name is shown as typed.
func (m *Model) credDialog() promptDialog {
	f := m.cred
	width := m.usable(0)
	title := tr(tCredNewTitle)
	if f.edit {
		title = tr(tCredEditTitle)
	}
	labels := []string{tr(tColName), tr(tColKind)}
	labels = append(labels, f.fields...)
	lw := labelWidth(labels)
	var rows []string
	for i, label := range labels {
		prefix := barMarker(f.focus == i) + padLine(label+":", lw+1) + " "
		room := max(width-lipgloss.Width(prefix)-1, 1)
		switch i {
		case 0:
			rows = append(rows, prefix+inputView(fitInput(f.name, room)))
		case 1:
			kind := f.kinds[f.kind]
			if !f.edit {
				kind = "< " + kind + " >"
			}
			rows = append(rows, prefix+fieldStyle(f.focus == i).Render(" "+kind+" "))
		default:
			rows = append(rows, prefix+inputView(fitInput(f.inputs[i-2], room)))
		}
	}
	keys := "enter next · tab next · {save} save · {back} cancel"
	if f.focus == 1 && !f.edit {
		keys = "left/right kind · enter next · {save} save · {back} cancel"
	}
	d := promptDialog{title: title, input: strings.Join(rows, "\n"), hint: tr(tCredHint), keys: keys}
	if f.problem != "" {
		d.notice = styles.fail.Render(warnMark + " " + f.problem)
	}
	return d
}
