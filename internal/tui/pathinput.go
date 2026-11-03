package tui

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// pathInput is a text line: a plain text line, or for a path a text line with what completes the typed path on
// the local file system. Only an absolute path is completed. tab takes the suggestion marked, the arrows and
// ctrl+n and ctrl+p move the mark; every other key goes to the text.
type pathInput struct {
	line textLine
	mode pathMode
	// start is the directory suggested while nothing is typed.
	start string
	// listed holds each directory read since the line was focused, so a keystroke never reads one again.
	listed  map[string][]pathEntry
	matches []string
	mark    int
	marked  bool
}

// pathMode says what a line suggests.
type pathMode int

const (
	pathPlain pathMode = iota
	// pathDir suggests directories only.
	pathDir
	// pathFile suggests directories and files; the file itself may not exist yet.
	pathFile
)

const (
	// pathShown is how many suggestions stand under the typed path at once.
	pathShown = 3
	// pathLimit bounds the suggestions kept for one typed text.
	pathLimit = 100
)

// pathEntry is one name of a listed directory.
type pathEntry struct {
	name string
	dir  bool
}

// value is the text of the line.
func (p *pathInput) value() string { return p.line.value() }

// open starts typing value with the cursor at its end; start is the suggestion for an empty line.
func (p *pathInput) open(value string, mode pathMode, start string) {
	p.mode, p.start, p.listed = mode, start, nil
	p.line.set(value)
	p.refresh()
}

// edit hands key to the text and reports whether the text changed.
func (p *pathInput) edit(key tea.KeyPressMsg) bool {
	before := p.value()
	p.line.edit(key)
	if p.value() == before {
		return false
	}
	p.refresh()
	return true
}

// complete puts the marked suggestion into the text and reports whether that changed it.
func (p *pathInput) complete() bool {
	if p.mark >= len(p.matches) || p.matches[p.mark] == p.value() {
		return false
	}
	p.line.set(p.matches[p.mark])
	p.refresh()
	return true
}

// move moves the mark over the suggestions.
func (p *pathInput) move(step int) {
	if len(p.matches) == 0 {
		return
	}
	p.mark = (p.mark + step + len(p.matches)) % len(p.matches)
	p.marked = true
}

func (p *pathInput) refresh() {
	p.matches, p.mark, p.marked = p.suggest(p.value()), 0, false
}

// shown are the suggestions that stand under the line and the position of the marked one among them.
func (p *pathInput) shown() ([]string, int) {
	start := max(0, min(p.mark-pathShown+1, len(p.matches)-pathShown))
	end := min(len(p.matches), start+pathShown)
	return p.matches[start:end], p.mark - start
}

// suggest are the entries inside the directory typed up to its last separator whose names start with the
// rest, each written as that directory followed by the name, with a separator after a directory so the next
// tab goes on into it. A hidden entry is offered once its dot is typed. Directories come first.
func (p *pathInput) suggest(typed string) []string {
	if p.mode == pathPlain {
		return nil
	}
	if typed == "" {
		if p.start == "" || !filepath.IsAbs(p.start) {
			return nil
		}
		return []string{withSeparator(p.start)}
	}
	cut := strings.LastIndexFunc(typed, isSeparator) + 1
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
		case p.mode == pathFile:
			files = append(files, dir+name)
		}
	}
	all := append(dirs, files...)
	return all[:min(len(all), pathLimit)]
}

// list reads the entries of a directory once per focus of the line. One that cannot be read has none.
func (p *pathInput) list(dir string) []pathEntry {
	if entries, ok := p.listed[dir]; ok {
		return entries
	}
	var entries []pathEntry
	if read, err := os.ReadDir(dir); err == nil {
		for _, e := range read {
			isDir := e.IsDir()
			if e.Type()&fs.ModeSymlink != 0 {
				// A link to a directory is offered like the directory it points to.
				info, err := os.Stat(filepath.Join(dir, e.Name()))
				isDir = err == nil && info.IsDir()
			}
			entries = append(entries, pathEntry{e.Name(), isDir})
		}
	}
	if p.listed == nil {
		p.listed = map[string][]pathEntry{}
	}
	p.listed[dir] = entries
	return entries
}

func isSeparator(r rune) bool { return r < 0x80 && os.IsPathSeparator(uint8(r)) }

// withSeparator is dir with one separator at its end.
func withSeparator(dir string) string {
	if dir != "" && os.IsPathSeparator(dir[len(dir)-1]) {
		return dir
	}
	return dir + string(filepath.Separator)
}
