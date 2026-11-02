package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// A data page may split its rows into titled sections that fold. A section is drawn as a heading row, which is
// part of the list like any row: it can be marked and moved over, but it has no detail. Pages without sections
// never see any of this.

// sectionRowPrefix starts the name of a heading row; it holds the NUL byte, which no entry name contains.
const sectionRowPrefix = "\x00s:"

func isSectionRow(name string) bool { return strings.HasPrefix(name, sectionRowPrefix) }

// dataSection is one section of a page: its key, the text of its title, the entries it holds in display order,
// and whether it starts folded.
type dataSection struct {
	key       string
	title     textID
	names     []string
	collapsed bool
}

// sectionState is what a page keeps of its sections. The fold state outlives the rows, which are rebuilt each
// time the data is read again.
type sectionState struct {
	list   []dataSection
	of     map[string]string
	folded map[string]bool
	// queried is the filter text the sections were last opened for.
	queried string
}

// setSections replaces the rows of the page by sections. A section without entries has no heading.
func (p *dataPage) setSections(secs []dataSection, rows map[string][]string) {
	s := &p.sec
	if s.folded == nil {
		s.folded = map[string]bool{}
	}
	s.list, s.of = nil, map[string]string{}
	var names []string
	for _, sec := range secs {
		if len(sec.names) == 0 {
			continue
		}
		if _, ok := s.folded[sec.key]; !ok {
			s.folded[sec.key] = sec.collapsed
		}
		s.list = append(s.list, sec)
		names = append(names, sectionRow(sec.key))
		for _, name := range sec.names {
			s.of[name] = sec.key
			names = append(names, name)
		}
	}
	_, had := p.list.selected()
	p.setRows(names, rows)
	p.list.begin = p.openMatching
	p.list.match = p.sectionShows
	p.list.refilter()
	// A page opens on its first entry, not on the heading above it.
	if !had && len(p.list.matches) > 1 && isSectionRow(p.list.matches[0]) {
		p.list.cursor = 1
	}
}

func sectionRow(key string) string { return sectionRowPrefix + key }

func (p *dataPage) sectioned() bool { return len(p.sec.list) > 0 }

// section is the section a heading row stands for.
func (p *dataPage) section(row string) (dataSection, bool) {
	for _, sec := range p.sec.list {
		if sectionRow(sec.key) == row {
			return sec, true
		}
	}
	return dataSection{}, false
}

// onHeading reports whether the marked row is a heading.
func (p *dataPage) onHeading() bool {
	name, ok := p.list.selected()
	return ok && isSectionRow(name)
}

// headingFolded reports whether the section of the marked heading is folded.
func (p *dataPage) headingFolded() bool {
	name, _ := p.list.selected()
	sec, _ := p.section(name)
	return p.sec.folded[sec.key]
}

// entries is the number of entries of the page, the headings not counted.
func (p *dataPage) entries() int {
	if !p.sectioned() {
		return len(p.list.all)
	}
	return len(p.sec.of)
}

// sectionPosition is the place of the marked row among the entries on view, headings not counted, and their
// number; on a heading it is the place of the entry above it.
func (p *dataPage) sectionPosition() (at, shown int) {
	for i, name := range p.list.matches {
		if isSectionRow(name) {
			continue
		}
		shown++
		if i <= p.list.cursor {
			at++
		}
	}
	return at, shown
}

// textHas reports whether the filter text of an entry holds the lower-case query.
func (p *dataPage) textHas(name, query string) bool {
	return strings.Contains(strings.ToLower(p.list.text(name)), query)
}

// sectionShows is the filter of a sectioned page: a heading is shown while its section has an entry that
// matches, an entry while it matches and its section is open.
func (p *dataPage) sectionShows(name, query string) bool {
	if isSectionRow(name) {
		if query == "" {
			return true
		}
		sec, _ := p.section(name)
		for _, n := range sec.names {
			if p.textHas(n, query) {
				return true
			}
		}
		return false
	}
	return !p.sec.folded[p.sec.of[name]] && (query == "" || p.textHas(name, query))
}

// openMatching opens the sections that hold a match when the filter text has changed, so a match inside a
// folded section is seen. Folding it again by hand sticks until the text changes.
func (p *dataPage) openMatching() {
	query := strings.ToLower(p.list.query())
	if query == p.sec.queried {
		return
	}
	p.sec.queried = query
	if query == "" {
		return
	}
	for _, sec := range p.sec.list {
		for _, n := range sec.names {
			if p.textHas(n, query) {
				p.sec.folded[sec.key] = false
				break
			}
		}
	}
}

// toggleHeading folds or opens the section of the marked heading and keeps the heading marked.
func (p *dataPage) toggleHeading() {
	name, ok := p.list.selected()
	if !ok {
		return
	}
	sec, ok := p.section(name)
	if !ok {
		return
	}
	p.sec.folded[sec.key] = !p.sec.folded[sec.key]
	p.scroll = 0
	p.list.refilter()
}

// headingLine draws a heading: the fold mark, the title and the number of its entries.
func (p *dataPage) headingLine(row string) string {
	sec, _ := p.section(row)
	arrow := "▾"
	if p.sec.folded[sec.key] {
		arrow = "▸"
	}
	return styles.hint.Render(arrow) + " " + styles.active.Render(tr(sec.title)) +
		styles.hint.Render(fmt.Sprintf(" (%d)", len(sec.names)))
}

// headingKey handles the keys that act on a marked heading.
func (p *dataPage) headingKey(key tea.KeyPressMsg) bool {
	if p.onHeading() && (keyIs(key, actEnter) || keyIs(key, actSpace)) {
		p.toggleHeading()
		return true
	}
	return false
}
