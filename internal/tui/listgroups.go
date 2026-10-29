package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// noGroup titles the group of entries that belong to none. It sorts last.
const noGroup = "no group"

// isGroupRow reports whether a list name is a group row rather than an entry. Group rows reuse the NUL
// prefix of the tool picker, which no entry name contains.
func isGroupRow(name string) bool { return strings.HasPrefix(name, "\x00") }

// groupRowPrefix starts the name of a group row, followed by the key of the group.
const groupRowPrefix = "\x00g:"

// listGroups is the grouping of one list: presentation only, the entries keep their names and
// their order inside a group. The fold state outlives the grouping, which is rebuilt with every list.
type listGroups struct {
	keys      []string
	members   map[string][]string
	of        map[string]string
	collapsed map[string]bool
	// hits caches, for one refilter, whether an entry matches the query, so a group row and its entries
	// look at each entry once.
	hits map[string]bool
}

// newListGroups groups the sorted names by their group key ("" for none). Groups are ordered
// alphabetically by group, the group without group last.
func newListGroups(names []string, group func(name string) string, collapsed map[string]bool) *listGroups {
	g := &listGroups{members: map[string][]string{}, of: make(map[string]string, len(names)), collapsed: collapsed}
	for _, name := range names {
		key := group(name)
		if _, ok := g.members[key]; !ok {
			g.keys = append(g.keys, key)
		}
		g.members[key] = append(g.members[key], name)
		g.of[name] = key
	}
	sort.Slice(g.keys, func(i, j int) bool {
		a, b := g.keys[i], g.keys[j]
		if (a == "") != (b == "") {
			return b == ""
		}
		return a < b
	})
	return g
}

func groupRow(key string) string { return groupRowPrefix + key }

// items is every name of the list in display order: each group row followed by its entries.
func (g *listGroups) items() []string {
	items := make([]string, 0, len(g.of)+len(g.keys))
	for _, key := range g.keys {
		items = append(items, groupRow(key))
		items = append(items, g.members[key]...)
	}
	return items
}

// rowText is how a group row reads: the fold marker, the mark of the group, the group and the number
// of its entries.
func (g *listGroups) rowText(row string, open bool) string {
	key, title, arrow := g.rowParts(row, open)
	return fmt.Sprintf("%s■ %s (%d)", arrow, title, len(g.members[key]))
}

// styledRow is rowText drawn for the screen: the mark in the colour of the group, its name in the accent
// colour and the count dimmed. Without colour it reads exactly as rowText does.
func (g *listGroups) styledRow(row string, open bool) string {
	key, title, arrow := g.rowParts(row, open)
	return styles.hint.Render(arrow) + groupMark(key) + " " + styles.active.Render(title) +
		styles.hint.Render(fmt.Sprintf(" (%d)", len(g.members[key])))
}

func (g *listGroups) rowParts(row string, open bool) (key, title, arrow string) {
	key = strings.TrimPrefix(row, groupRowPrefix)
	title = key
	if key == "" {
		title = wordText(noGroup)
	}
	arrow = "▸ "
	if open {
		arrow = "▾ "
	}
	return key, title, arrow
}

// open reports whether the group of the row is shown unfolded.
func (g *listGroups) open(row string) bool {
	return !g.collapsed[strings.TrimPrefix(row, groupRowPrefix)]
}

// begin starts a refilter.
func (g *listGroups) begin() { g.hits = map[string]bool{} }

// matches is the filter of a grouped list. Without a query the group rows show and their entries only
// while the group is open; with one, every entry whose text matches shows under its group, folded or not,
// and a group row shows when one of its entries does. The fold state itself is not touched.
func (g *listGroups) matches(text func(string) string) func(name, query string) bool {
	hit := func(name, query string) bool {
		if v, ok := g.hits[name]; ok {
			return v
		}
		v := strings.Contains(strings.ToLower(text(name)), query)
		if g.hits != nil {
			g.hits[name] = v
		}
		return v
	}
	return func(name, query string) bool {
		switch {
		case isGroupRow(name):
			if query == "" {
				return true
			}
			for _, member := range g.members[strings.TrimPrefix(name, groupRowPrefix)] {
				if hit(member, query) {
					return true
				}
			}
			return false
		case query == "":
			return !g.collapsed[g.of[name]]
		}
		return hit(name, query)
	}
}

// fold opens or closes the group of the row (a group row or one of its entries) and returns the row the
// cursor belongs on, or "" when nothing changed. An entry only folds, and the cursor follows to its group.
func (g *listGroups) fold(row string, collapse bool) string {
	key, ok := g.of[row]
	if isGroupRow(row) {
		key, ok = strings.TrimPrefix(row, groupRowPrefix), true
	} else if !collapse {
		return ""
	}
	if !ok || g.collapsed[key] == collapse {
		return ""
	}
	g.collapsed[key] = collapse
	return groupRow(key)
}

// toggle flips the fold state of the group row.
func (g *listGroups) toggle(row string) {
	key := strings.TrimPrefix(row, groupRowPrefix)
	g.collapsed[key] = !g.collapsed[key]
}

// reveal unfolds the group of an entry and reports whether that changed anything.
func (g *listGroups) reveal(name string) bool {
	key, ok := g.of[name]
	if !ok || !g.collapsed[key] {
		return false
	}
	g.collapsed[key] = false
	return true
}

// setEntries fills the list with the entries of the active view: names in display order, grouped by the key
// groupOf gives each ("" for none) when it is not nil. text is what the filter and the copy key read of an
// entry. Groups are ordered alphabetically, the group without key last; their fold state outlives the list.
// It is the one way to fill m.list, because it also installs the matching filter.
func (m *Model) setEntries(names []string, groupOf func(name string) string, text func(name string) string) {
	if text != nil {
		m.list.text = text
	}
	if groupOf == nil {
		m.groups, m.list.match, m.list.begin = nil, nil, nil
		m.list.setItems(names)
		return
	}
	if m.folds == nil {
		m.folds = map[string]bool{}
	}
	g := newListGroups(names, groupOf, m.folds)
	m.groups, m.list.match, m.list.begin = g, g.matches(m.list.text), g.begin
	m.list.setItems(g.items())
}

// entryTotal is the number of entries of the list, group rows not counted.
func (m *Model) entryTotal() int {
	if m.groups == nil {
		return len(m.list.all)
	}
	return len(m.list.all) - len(m.groups.keys)
}

// entryPosition is the number of entries shown, and the place of the cursor among them (a group row counts
// as the entry above it, at least the first).
func (m *Model) entryPosition() (shown, pos int) {
	if m.groups == nil {
		return len(m.list.matches), m.list.cursor + 1
	}
	for i, name := range m.list.matches {
		if isGroupRow(name) {
			continue
		}
		shown++
		if i <= m.list.cursor {
			pos++
		}
	}
	return shown, max(pos, min(shown, 1))
}

// foldKey handles enter, left and right on a group row or, for left, on an entry. It reports whether the
// key was a fold key of this list. While a filter is set the groups show unfolded and stay as they are.
func (m *Model) foldKey(key tea.KeyPressMsg) bool {
	if m.groups == nil || m.list.query() != "" {
		return false
	}
	row, ok := m.list.selected()
	if !ok {
		return false
	}
	var target string
	switch {
	case keyIs(key, actEnter) && isGroupRow(row):
		m.groups.toggle(row)
		target = row
	case keyIs(key, actLeft) || keyIs(key, actRight):
		target = m.groups.fold(row, keyIs(key, actLeft))
	default:
		return false
	}
	if target != "" {
		m.list.refilter()
		m.list.selectName(target)
	}
	return true
}

// selectEntry selects the named entry, unfolding its group when it is folded.
func (m *Model) selectEntry(name string) {
	if m.list.selectName(name) || m.groups == nil || !m.groups.reveal(name) {
		return
	}
	m.list.refilter()
	m.list.selectName(name)
}
