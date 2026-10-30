// Package helptopics holds the short guides that ship inside the binary. The command line prints them as
// help topics. The package imports nothing of this module, so any surface can use it.
package helptopics

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Topic is one guide: the name it is asked for by, one line on what it covers, and its text. The text is plain
// ASCII with one line per paragraph, list item, or command; Wrap fits it to the width where it is shown.
type Topic struct {
	Name  string
	Short string
	Text  string
}

// All returns the topics in the order a newcomer reads them.
func All() []Topic {
	return []Topic{
		{Name: "sysmin", Short: "What sysmin is and what it is for", Text: sysmin},
	}
}

// listMarker is the start of a numbered or bulleted item; a wrapped item continues under its text.
var listMarker = regexp.MustCompile(`^ *(\d+\. |- )`)

// Wrap breaks every line of text wider than width at its spaces. A continuation keeps the indentation of
// its line, and under a list item the column of the item's text, so items and commands stay recognisable.
// An indentation deeper than half the width is given up, and a word wider than the width is cut, so no
// line of the result is wider than width.
func Wrap(text string, width int) string {
	width = max(width, 1)
	var out []string
	for _, line := range strings.Split(text, "\n") {
		out = append(out, wrapLine(line, width)...)
	}
	return strings.Join(out, "\n")
}

func wrapLine(line string, width int) []string {
	if utf8.RuneCountInString(line) <= width {
		return []string{line}
	}
	lead := len(line) - len(strings.TrimLeft(line, " "))
	hang := lead
	if marker := listMarker.FindString(line); marker != "" {
		hang = len(marker)
	}
	if lead > width/2 {
		lead = 0
	}
	if hang > width/2 {
		hang = lead
	}
	var lines []string
	current, empty := strings.Repeat(" ", lead), true
	for _, word := range strings.Fields(line) {
		if !empty && utf8.RuneCountInString(current)+1+utf8.RuneCountInString(word) <= width {
			current += " " + word
			continue
		}
		if !empty {
			lines = append(lines, current)
			current = strings.Repeat(" ", hang)
		}
		for utf8.RuneCountInString(current)+utf8.RuneCountInString(word) > width {
			room := width - utf8.RuneCountInString(current)
			runes := []rune(word)
			lines = append(lines, current+string(runes[:room]))
			current, word = strings.Repeat(" ", hang), string(runes[room:])
		}
		current, empty = current+word, false
	}
	return append(lines, current)
}

const sysmin = `sysmin is a Go tool that runs as a service on a Linux host. It takes over the setup and operation of a server through guided, repeatable scenarios.

'sysmin --help' lists the commands; 'sysmin tui' opens the terminal interface.`
