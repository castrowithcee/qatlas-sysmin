package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// Channels is the Library page of the notification channels. A channel holds names and addresses; the secret
// it needs is a credential that it names. Nothing here reads or enters a secret. A stored channel cannot be
// changed, only removed and added again, because the service has no edit.

// chanState is the Channels page.
type chanState struct {
	page  dataPage
	items map[string]control.ChannelSummary
	err   string
	have  bool
}

type channelsMsg struct {
	list []control.ChannelSummary
	err  error
}

// channelTestMsg is the outcome of a test message.
type channelTestMsg struct {
	name string
	err  error
}

// The channel types the service knows and the fields each takes besides its name and type.
var (
	channelTypes  = []string{"webhook", "smtp", "telegram", "slack"}
	channelFields = map[string][]string{
		"webhook":  {"url", "credential"},
		"smtp":     {"host", "port", "from", "to", "security", "credential"},
		"telegram": {"chat_id", "credential"},
		"slack":    {"credential"},
	}
	smtpSecurities = []string{"", "starttls", "tls"}
)

// chanStateText is the state of a channel as drawn: the mark and the word.
func chanStateText(state string) string {
	switch state {
	case control.StateApproved:
		return tr(tUseApproved)
	case control.StatePending:
		return tr(tChanPending)
	}
	return tr(tChanUnknown)
}

func chanColumns() []column {
	return []column{
		{title: tr(tColName)},
		{title: tr(tColType)},
		{title: tr(tColTarget), flex: true},
		{title: tr(tColCredential), optional: true},
		{title: tr(tColState), shown: chanStateText, tone: func(cell string) lipgloss.Style {
			if cell == tr(tChanPending) {
				return styles.warning
			}
			return styles.cell
		}},
	}
}

// chanTarget is where a channel sends to, short.
func chanTarget(c control.ChannelSummary) string {
	switch {
	case c.Type == "webhook":
		return strings.TrimPrefix(strings.TrimPrefix(c.URL, "https://"), "http://")
	case c.Type == "smtp":
		if c.Port != 0 {
			return c.Host + ":" + strconv.Itoa(c.Port)
		}
		return c.Host
	case c.Type == "telegram":
		return c.ChatID
	}
	return ""
}

func (m *Model) channelsCmd() tea.Cmd {
	if m.backend == nil || !m.serviceUp() {
		return nil
	}
	b := m.backend
	return func() tea.Msg {
		list, err := b.Channels()
		return channelsMsg{list, err}
	}
}

func (m *Model) takeChannels(msg channelsMsg) {
	c := &m.lib.chans
	c.err, c.have = "", msg.err == nil
	if msg.err != nil {
		c.err, msg.list = errorText(msg.err), nil
	}
	c.items = map[string]control.ChannelSummary{}
	rows := map[string][]string{}
	var names []string
	for _, s := range msg.list {
		c.items[s.Name] = s
		names = append(names, s.Name)
		credential := s.Credential
		if credential == "" {
			credential = "-"
		}
		target := m.diffText(chanTarget(s))
		if target == "" {
			target = "-"
		}
		rows[s.Name] = []string{m.diffText(s.Name), m.diffText(s.Type), target, m.diffText(credential), s.State}
	}
	c.page.cols = chanColumns()
	c.page.setRows(names, rows)
}

func (m *Model) chanSpec() dataSpec {
	empty := tr(tChanEmpty)
	if m.lib.chans.err != "" {
		empty = m.lib.chans.err
	}
	return dataSpec{title: tr(tSubChannels), empty: empty, lines: m.chanLines}
}

// chanFacts are the fields a channel has, label first, in the order of the form.
func (m *Model) chanFacts(c control.ChannelSummary) [][2]string {
	var out [][2]string
	add := func(label, value string) {
		if value != "" {
			out = append(out, [2]string{label, m.diffText(value)})
		}
	}
	add(tr(tColName), c.Name)
	add(tr(tColType), c.Type)
	add(tr(tLblURL), c.URL)
	add(tr(tLblHost), c.Host)
	if c.Port != 0 {
		add(tr(tLblPort), strconv.Itoa(c.Port))
	}
	add(tr(tLblFrom), c.From)
	add(tr(tLblTo), strings.Join(c.To, ", "))
	add(tr(tLblSecurity), c.Security)
	add(tr(tLblChatID), c.ChatID)
	return out
}

// chanLines are the lines of one tab of a channel: addresses and the name of its credential, never a secret.
func (m *Model) chanLines(name string, tab, width int) []string {
	c, ok := m.lib.chans.items[name]
	if !ok {
		return nil
	}
	credential := m.diffText(c.Credential)
	if credential == "" {
		credential = "-"
	}
	switch tab {
	case tabOverview:
		return factLines([][2]string{
			{tr(tColName), m.diffText(c.Name)},
			{tr(tColType), m.diffText(c.Type)},
			{tr(tColState), chanStateText(c.State)},
			{tr(tColCredential), credential},
		}, width)
	case tabDetails:
		return factLines(m.chanFacts(c), width)
	}
	lines := []string{"name: " + m.diffText(c.Name), "type: " + m.diffText(c.Type)}
	for _, f := range m.chanFacts(c)[2:] {
		lines = append(lines, rawKey(f[0])+": "+f[1])
	}
	return append(lines, "credential: "+credential, "state: "+m.diffText(c.State))
}

// rawKey is the key a label is drawn as in the raw tab: the service's own field name.
func rawKey(label string) string {
	switch label {
	case tr(tLblURL):
		return "url"
	case tr(tLblHost):
		return "host"
	case tr(tLblPort):
		return "port"
	case tr(tLblFrom):
		return "from"
	case tr(tLblTo):
		return "to"
	case tr(tLblSecurity):
		return "security"
	case tr(tLblChatID):
		return "chat_id"
	}
	return label
}

func (m *Model) chanKeys() []string {
	if m.lib.chans.page.open {
		return []string{"left/right tab", "up/down scroll", "t test", "d remove", "{back} close"}
	}
	return []string{"left/right page", "up/down move", "enter details", "n new", "t test", "d remove", "/ filter"}
}

// chanKey handles the letters of the Channels page.
func (m *Model) chanKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	switch {
	case keyIs(key, actNew):
		return m.openChannelForm(), true
	case keyIs(key, actTest):
		return m.channelTest(), true
	case keyIs(key, actDelete):
		return m.channelRemove(), true
	}
	return nil, false
}

// channelTest sends one test message to the marked channel. It needs no admin session; the service sends to
// approved channels only and answers with a fixed code, which is all the screen says about a failure.
func (m *Model) channelTest() tea.Cmd {
	name, ok := m.lib.chans.page.current()
	if !ok || m.backend == nil {
		return nil
	}
	m.clearMessages()
	m.busy = tr(tChanTesting)
	b := m.backend
	return func() tea.Msg {
		_, err := b.TestChannel(name)
		return channelTestMsg{name, err}
	}
}

func (m *Model) takeChannelTest(msg channelTestMsg) {
	m.busy = ""
	switch {
	case msg.err == nil:
		m.status = tr(tChanTestSent, m.diffText(msg.name))
	case isCode(msg.err, control.CodeSendFailed):
		m.fail = tr(tChanTestFailed, m.diffText(msg.name))
	case isCode(msg.err, control.CodeNotApproved), isCode(msg.err, control.CodeNotFound):
		m.fail = tr(tChanTestNotApproved)
	default:
		m.fail = errorText(msg.err)
	}
}

// channelRemove asks, then removes the marked channel.
func (m *Model) channelRemove() tea.Cmd {
	name, ok := m.lib.chans.page.current()
	if !ok {
		return nil
	}
	m.askConfirm(question{title: tr(tChanRemoveTitle, m.diffText(name)), consequence: tr(tChanRemoveConsequence), warn: true,
		danger: true, keys: "y remove · n/{back} cancel"}, func() tea.Cmd {
		var res control.ChangeResult
		return m.requireAdmin(change{
			call: func(b Backend) (err error) { res, err = b.RemoveChannel(name); return err },
			done: func(m *Model, err error) tea.Cmd {
				if err == nil {
					m.status = tr(tChanRemoved)
					if res.State != control.StateApproved {
						m.status = tr(tChanRemovedPending)
					}
				}
				return tea.Batch(m.statusCmd(), m.channelsCmd(), m.accessCmd())
			},
		})
	})
	return nil
}

// credentialChoices are the credentials a channel of the type can name: the mail login for smtp, a plain
// secret for every other type. The first choice is none.
func (m *Model) credentialChoices(channelType string) (values, shown []string) {
	kind := "secret"
	if channelType == "smtp" {
		kind = "smtp"
	}
	var names []string
	for name, c := range m.lib.cred.items {
		if c.Kind == kind {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	values = append([]string{""}, names...)
	shown = []string{tr(tChanNoCredential)}
	for _, n := range names {
		shown = append(shown, m.diffText(n))
	}
	return values, shown
}

// applyChannelType shows the rows the type takes and offers the credentials that fit it.
func (m *Model) applyChannelType(f *formDlg) {
	t := f.row("type").value()
	for i := range f.rows {
		key := f.rows[i].key
		if key == "name" || key == "type" {
			continue
		}
		f.rows[i].hidden = !slices.Contains(channelFields[t], key)
	}
	values, shown := m.credentialChoices(t)
	f.row("credential").setChoices(values, shown)
	if f.rows[f.focus].hidden {
		f.step(1)
	}
}

// openChannelForm opens the form for a new channel.
func (m *Model) openChannelForm() tea.Cmd {
	f := &formDlg{title: tr(tChanNewTitle), hint: tr(tChanFormHint), saveText: "{save} save", submit: submitChannel}
	f.rows = []formRow{
		textRow("name", tr(tColName), ""),
		choiceRow("type", tr(tColType), channelTypes, nil, 0),
		textRow("url", tr(tLblURL), ""),
		textRow("host", tr(tLblHost), ""),
		textRow("port", tr(tLblPort), ""),
		textRow("from", tr(tLblFrom), ""),
		textRow("to", tr(tLblTo), ""),
		choiceRow("security", tr(tLblSecurity), smtpSecurities, []string{tr(tChanSecurityDefault), "starttls", "tls"}, 0),
		textRow("chat_id", tr(tLblChatID), ""),
		choiceRow("credential", tr(tColCredential), []string{""}, nil, 0),
	}
	f.changed = func(m *Model, f *formDlg, row int) {
		if f.rows[row].key == "type" {
			m.applyChannelType(f)
		}
	}
	f.refresh = func(m *Model, f *formDlg) { m.applyChannelType(f) }
	m.applyChannelType(f)
	m.openForm(f)
	if !m.lib.cred.have {
		return m.credentialsCmd()
	}
	return nil
}

// submitChannel hands the typed channel to the service. The form is left first, so the passphrase overlay
// that may follow stands alone; a refusal brings the form back with its problems under the fields.
func submitChannel(m *Model, f *formDlg) tea.Cmd {
	p := control.ChannelParams{Name: f.row("name").value(), Type: f.row("type").value()}
	if p.Name == "" {
		f.row("name").problem = tr(tChanNameEmpty)
		return nil
	}
	for _, key := range channelFields[p.Type] {
		v := f.row(key).value()
		switch {
		case key == "url":
			p.URL = v
		case key == "host":
			p.Host = v
		case key == "port":
			if v == "" {
				continue
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				f.row("port").problem = tr(tChanPortBad)
				return nil
			}
			p.Port = n
		case key == "from":
			p.From = v
		case key == "to":
			for _, a := range strings.Split(v, ",") {
				if a = strings.TrimSpace(a); a != "" {
					p.To = append(p.To, a)
				}
			}
		case key == "security":
			p.Security = v
		case key == "chat_id":
			p.ChatID = v
		case key == "credential":
			p.Credential = v
		}
	}
	m.closeForm()
	var res control.ChangeResult
	return m.requireAdmin(change{
		call: func(b Backend) (err error) { res, err = b.AddChannel(p); return err },
		done: func(m *Model, err error) tea.Cmd {
			switch {
			case err == nil:
				m.status = tr(tChanAdded)
				if res.State != control.StateApproved {
					m.status = tr(tChanAddedPending)
				}
			case isCode(err, control.CodeInvalid):
				if m.formProblems(f, err) {
					m.reopenForm(f)
				}
			case isCode(err, control.CodeExists):
				f.row("name").problem = tr(tChanExists)
				m.reopenForm(f)
			}
			return tea.Batch(m.statusCmd(), m.channelsCmd(), m.accessCmd())
		},
	})
}
