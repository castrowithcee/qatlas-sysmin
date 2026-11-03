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
		{Name: "host-admin", Short: "Read and change users, groups and folder permissions of the host", Text: hostAdmin},
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

const sysmin = `sysmin is a Go tool that runs as a service on a Linux host and helps look after that host. It has the areas Overview, Host, Activity and Settings.

Host reads the local users, groups and folder permissions. Changes run through 'sudo sysmin host ...' after a confirmation, are recorded in a journal with the previous state, and can be undone. After a change to folder permissions, the containers it affects are watched. Activity shows a log kept as a hash chain without a signature.

Admins, members of the group sudo, reach the running service through its socket. The installation requires the group docker.

A further area, Containers, is a possible direction; nothing is promised for it.

'sysmin --help' lists the commands; 'sysmin tui' opens the terminal interface; 'sysmin host-admin' covers the host commands.`

const hostAdmin = `The host commands read the local users, groups and folder permissions. Changes (host apply, host undo) run through sudo after a confirmation, are recorded in a root-only journal with the previous state, and can be undone.

Commands:
  sysmin host users
  sysmin host user <name>
  sysmin host groups
  sysmin host group <name>
  sysmin host folder <path>

Only root and members of the group sudo may use them. Everyone else gets one line and exit code 1.

A user or group is a system user or group when its ID lies outside 1000 to 60000, so root and nobody count as system accounts. A user is an admin in the group sudo, a docker user in the group docker, and enabled in the group sysmin. A group lists the users whose primary group it is as members.

Whether an account is locked or expired and which SSH keys it has are only readable as root. Otherwise they are unknown: null in JSON and 'unknown' in the other formats. Keys are shown as type, comment and SHA256 fingerprint, never as the key itself.

A folder or file shows owner, group and the level of owner, group and others: nothing, read, or read and write. For a directory read and write both need the right to enter it; write without read counts as nothing. It also shows whether ACLs are set, the special bits setuid, setgid and sticky, and whether the path is protected: /, /etc, /usr, /boot, /proc, /sys and everything below the last five. Links and .. are resolved before that check.`
