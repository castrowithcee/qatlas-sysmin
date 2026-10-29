package tui

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// projectPathInput is the text line of the guided project setup: a plain text input, or for a path a text
// input with what completes the typed path on the local file system. A project lives on this host and its
// paths are absolute, so only an absolute path is completed. tab takes the suggestion marked, ctrl+n and
// ctrl+p move the mark; every other key goes to the text.
type projectPathInput struct {
	textinput.Model
	mode projectPathMode
	// start is the directory suggested while nothing is typed.
	start string
	// listed holds each directory read since the line was focused, so a keystroke never reads one again.
	listed  map[string][]projectPathEntry
	matches []string
	mark    int
	marked  bool
}

// projectPathMode says what a line suggests.
type projectPathMode int

const (
	projectPathPlain projectPathMode = iota
	// projectPathDir suggests directories only.
	projectPathDir
	// projectPathFile suggests directories and files; the file itself may not exist yet.
	projectPathFile
)

const (
	// projectPathShown is how many suggestions stand under the typed path at once.
	projectPathShown = 3
	// projectPathLimit bounds the suggestions kept for one typed text.
	projectPathLimit = 100
)

// projectPathEntry is one name of a listed directory.
type projectPathEntry struct {
	name string
	dir  bool
}

func newProjectPathInput() projectPathInput {
	in := newTextInput()
	in.Prompt = ""
	return projectPathInput{Model: in}
}

// open starts typing value with the cursor at its end; start is the suggestion for an empty line.
func (p *projectPathInput) open(value string, mode projectPathMode, start string) {
	p.mode, p.start, p.listed = mode, start, nil
	p.SetValue(value)
	p.CursorEnd()
	p.Focus()
	p.refresh()
}

// close ends the typing; the value stays where the line wrote it.
func (p *projectPathInput) close() {
	p.Blur()
	p.matches, p.mark, p.marked = nil, 0, false
}

// edit hands key to the text and reports whether the text changed.
func (p *projectPathInput) edit(key tea.KeyPressMsg) (tea.Cmd, bool) {
	before := p.Value()
	var cmd tea.Cmd
	p.Model, cmd = p.Model.Update(inputMsg(key))
	if p.Value() == before {
		return cmd, false
	}
	p.refresh()
	return cmd, true
}

// complete puts the marked suggestion into the text and reports whether that changed it.
func (p *projectPathInput) complete() bool {
	if p.mark >= len(p.matches) || p.matches[p.mark] == p.Value() {
		return false
	}
	p.SetValue(p.matches[p.mark])
	p.CursorEnd()
	p.refresh()
	return true
}

// move moves the mark over the suggestions.
func (p *projectPathInput) move(step int) {
	if len(p.matches) == 0 {
		return
	}
	p.mark = (p.mark + step + len(p.matches)) % len(p.matches)
	p.marked = true
}

func (p *projectPathInput) refresh() {
	p.matches, p.mark, p.marked = p.suggest(p.Value()), 0, false
}

// shown are the suggestions that stand under the line and the position of the marked one among them.
func (p *projectPathInput) shown() ([]string, int) {
	start := max(0, min(p.mark-projectPathShown+1, len(p.matches)-projectPathShown))
	end := min(len(p.matches), start+projectPathShown)
	return p.matches[start:end], p.mark - start
}

// suggest are the entries inside the directory typed up to its last separator whose names start with the
// rest, each written as that directory followed by the name, with a separator after a directory so the next
// tab goes on into it. A hidden entry is offered once its dot is typed. Directories come first.
func (p *projectPathInput) suggest(typed string) []string {
	if p.mode == projectPathPlain {
		return nil
	}
	if typed == "" {
		if p.start == "" || !filepath.IsAbs(p.start) {
			return nil
		}
		return []string{projectWithSeparator(p.start)}
	}
	cut := strings.LastIndexFunc(typed, projectIsSeparator) + 1
	dir, rest := typed[:cut], typed[cut:]
	if dir == "" || !filepath.IsAbs(dir) {
		return nil
	}
	var dirs, files []string
	for _, entry := range p.list(dir) {
		name := entry.name
		if !strings.HasPrefix(name, rest) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(rest, ".")) {
			continue
		}
		switch {
		case entry.dir:
			dirs = append(dirs, dir+name+string(filepath.Separator))
		case p.mode == projectPathFile:
			files = append(files, dir+name)
		}
	}
	all := append(dirs, files...)
	return all[:min(len(all), projectPathLimit)]
}

// list reads the entries of a directory once per focus of the line. One that cannot be read has none.
func (p *projectPathInput) list(dir string) []projectPathEntry {
	if entries, ok := p.listed[dir]; ok {
		return entries
	}
	var entries []projectPathEntry
	if read, err := os.ReadDir(dir); err == nil {
		for _, e := range read {
			isDir := e.IsDir()
			if e.Type()&fs.ModeSymlink != 0 {
				// A link to a directory is offered like the directory it points to.
				info, err := os.Stat(filepath.Join(dir, e.Name()))
				isDir = err == nil && info.IsDir()
			}
			entries = append(entries, projectPathEntry{e.Name(), isDir})
		}
	}
	if p.listed == nil {
		p.listed = map[string][]projectPathEntry{}
	}
	p.listed[dir] = entries
	return entries
}

func projectIsSeparator(r rune) bool { return r < 0x80 && os.IsPathSeparator(uint8(r)) }

// projectWithSeparator is dir with one separator at its end.
func projectWithSeparator(dir string) string {
	if dir != "" && os.IsPathSeparator(dir[len(dir)-1]) {
		return dir
	}
	return dir + string(filepath.Separator)
}
