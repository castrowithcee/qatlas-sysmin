package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// Templates is the Library page of the scenario templates. The builtin ones are part of the program and the
// custom ones are files of the configuration; the page lists and shows them and derives new custom ones, and
// changes or deletes none.

// tmplState is the Templates page: the list, and the YAML of the templates looked at.
type tmplState struct {
	page   dataPage
	items  map[string]control.TemplateSummary
	err    string
	have   bool
	detail map[string]*tmplDetail
	// pick is a template to mark as soon as the list arrives, such as the one just derived.
	pick string
}

type tmplDetail struct {
	file    string
	loading bool
	err     string
}

type templatesMsg struct {
	list []control.TemplateSummary
	err  error
}

type templateDetailMsg struct {
	name string
	d    control.TemplateDetail
	err  error
}

func originText(origin string) string {
	switch origin {
	case control.TemplateBuiltin:
		return tr(tTmplBuiltin)
	case control.TemplateCustom:
		return tr(tTmplCustom)
	}
	return origin
}

func tmplColumns() []column {
	return []column{
		{title: tr(tColName)},
		{title: tr(tColOrigin), shown: originText},
		{title: tr(tColSteps), right: true},
		{title: tr(tColDescription), flex: true, optional: true},
	}
}

func (m *Model) templatesCmd() tea.Cmd {
	if m.backend == nil || !m.serviceUp() {
		return nil
	}
	b := m.backend
	return func() tea.Msg {
		list, err := b.Templates()
		return templatesMsg{list, err}
	}
}

func (m *Model) takeTemplates(msg templatesMsg) {
	t := &m.lib.tmpls
	t.err, t.have = "", msg.err == nil
	if msg.err != nil {
		t.err, msg.list = errorText(msg.err), nil
	}
	t.items = map[string]control.TemplateSummary{}
	rows := map[string][]string{}
	var names []string
	for _, s := range msg.list {
		t.items[s.Name] = s
		names = append(names, s.Name)
		rows[s.Name] = []string{m.diffText(s.Name), s.Origin, strconv.Itoa(s.Steps), m.diffText(s.Description)}
	}
	t.page.cols = tmplColumns()
	t.page.setRows(names, rows)
	if t.pick != "" {
		t.page.list.selectName(t.pick)
		t.pick = ""
	}
	for name, d := range t.detail {
		if _, ok := t.items[name]; !ok || d.err != "" {
			delete(t.detail, name)
		}
	}
}

// tmplWant asks for the YAML of the marked template where its detail is on the screen and not there yet.
func (m *Model) tmplWant() tea.Cmd {
	t := &m.lib.tmpls
	name, ok := t.page.current()
	if !ok || !m.detailSeen() || m.backend == nil {
		return nil
	}
	if t.detail == nil {
		t.detail = map[string]*tmplDetail{}
	}
	if t.detail[name] != nil {
		return nil
	}
	t.detail[name] = &tmplDetail{loading: true}
	b := m.backend
	return func() tea.Msg {
		d, err := b.Template(name)
		return templateDetailMsg{name, d, err}
	}
}

func (m *Model) takeTemplateDetail(msg templateDetailMsg) {
	d := m.lib.tmpls.detail[msg.name]
	if d == nil {
		return
	}
	d.loading = false
	if msg.err != nil {
		d.err = errorText(msg.err)
		return
	}
	d.file = msg.d.File
}

func (m *Model) tmplSpec() dataSpec {
	empty := tr(tTmplEmpty)
	if m.lib.tmpls.err != "" {
		empty = m.lib.tmpls.err
	}
	return dataSpec{title: tr(tSubTemplates), empty: empty, lines: m.tmplLines}
}

// tmplLines are the lines of one tab of a template. The YAML is the file of the template as the service reads
// it; every line is made safe and cut into the width, indentation kept.
func (m *Model) tmplLines(name string, tab, width int) []string {
	t, ok := m.lib.tmpls.items[name]
	if !ok {
		return nil
	}
	switch tab {
	case tabOverview:
		return factLines([][2]string{
			{tr(tColName), m.diffText(t.Name)},
			{tr(tColOrigin), originText(t.Origin)},
			{tr(tColSteps), strconv.Itoa(t.Steps)},
			{tr(tColDescription), m.diffText(t.Description)},
		}, width)
	case tabDetails:
		d := m.lib.tmpls.detail[name]
		switch {
		case d == nil || d.loading:
			return []string{styles.hint.Render(tr(tAccLoading))}
		case d.err != "":
			return []string{styles.hint.Render(d.err)}
		}
		var lines []string
		for _, line := range strings.Split(strings.TrimRight(d.file, "\n"), "\n") {
			lines = append(lines, strings.Split(ansi.Hardwrap(m.diffText(line), max(width, 1), true), "\n")...)
		}
		return lines
	}
	return []string{"name: " + m.diffText(t.Name), "origin: " + t.Origin, "source: " + t.Source,
		"steps: " + strconv.Itoa(t.Steps), "description: " + m.diffText(t.Description)}
}

func (m *Model) tmplKeys() []string {
	if m.lib.tmpls.page.open {
		return []string{"left/right tab", "up/down scroll", "p derive", "{back} close"}
	}
	return []string{"left/right page", "up/down move", "enter details", "p derive", "/ filter"}
}

// tmplKey handles the letters of the Templates page.
func (m *Model) tmplKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	if keyIs(key, actDuplicate) {
		return m.openDeriveForm(), true
	}
	return nil, false
}

// openDeriveForm opens the form for a new custom template with the steps of the marked one.
func (m *Model) openDeriveForm() tea.Cmd {
	from, ok := m.lib.tmpls.page.current()
	if !ok {
		return nil
	}
	t := m.lib.tmpls.items[from]
	f := &formDlg{title: tr(tTmplDeriveTitle, m.diffText(from)), hint: tr(tTmplDeriveHint), saveText: "{save} save",
		submit: func(m *Model, f *formDlg) tea.Cmd { return submitDerive(m, f, from, t.Description) }}
	f.rows = []formRow{
		textRow("name", tr(tColName), ""),
		textRow("description", tr(tColDescription), t.Description),
	}
	m.openForm(f)
	return nil
}

// submitDerive hands the new name to the service. An unchanged description is not sent, so the new template
// keeps the one it is copied from.
func submitDerive(m *Model, f *formDlg, from, original string) tea.Cmd {
	name, description := f.row("name").value(), f.row("description").value()
	if name == "" {
		f.row("name").problem = tr(tTmplNameEmpty)
		return nil
	}
	if description == original {
		description = ""
	}
	p := control.TemplateDeriveParams{From: from, Name: name, Description: description}
	m.closeForm()
	return m.requireAdmin(change{
		call: func(b Backend) error { _, err := b.DeriveTemplate(p); return err },
		done: func(m *Model, err error) tea.Cmd {
			switch {
			case err == nil:
				m.status = tr(tTmplDerived, m.diffText(name))
				m.lib.tmpls.pick = name
			case isCode(err, control.CodeInvalid):
				if m.formProblems(f, err) {
					m.reopenForm(f)
				}
			case isCode(err, control.CodeExists):
				f.row("name").problem = tr(tTmplExists)
				m.reopenForm(f)
			}
			return tea.Batch(m.statusCmd(), m.templatesCmd())
		},
	})
}
