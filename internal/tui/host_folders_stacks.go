package tui

import (
	"bytes"
	"encoding/json"
	"path"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// A stack folder is set up from Host > Folders with s. What the setup would change is read as root first
// (`sudo sysmin host preview …`, which changes nothing); the confirmation names every stack, the entries that
// stay as they are, and that other users lose access. The setup itself runs as any host change, on y.

type stackAccess struct {
	Owner string `json:"owner"`
	Group string `json:"group"`
	Mode  string `json:"mode"`
}

func (a stackAccess) String() string { return a.Owner + ":" + a.Group + " " + a.Mode }

type stackChange struct {
	Path   string      `json:"path"`
	Role   string      `json:"role"`
	Before stackAccess `json:"before"`
	After  stackAccess `json:"after"`
}

type stackKept struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type stackInfo struct {
	Name    string        `json:"name"`
	Path    string        `json:"path"`
	Running bool          `json:"running"`
	Changes []stackChange `json:"changes"`
	Kept    []stackKept   `json:"kept"`
}

// stackPreview is the answer of `host preview folder.setup-stacks`.
type stackPreview struct {
	Sentence string   `json:"sentence"`
	Commands []string `json:"commands"`
	Preview  *struct {
		Path             string       `json:"path"`
		Folder           *stackChange `json:"folder"`
		Stacks           []stackInfo  `json:"stacks"`
		Kept             []stackKept  `json:"kept"`
		OthersLoseAccess bool         `json:"others_lose_access"`
		NothingToDo      bool         `json:"nothing_to_do"`
	} `json:"preview"`
}

// stackDetailRoom is the number of detail lines the confirmation keeps free at a terminal height h: the rest
// of a modal is the heading, the consequence and the buttons. A longer list ends in a count.
func stackDetailRoom(h int) int { return max(h-14, 4) }

// askStackSetup reads the preview of the folder shown as root and, from its answer, asks to set it up.
func (m *Model) askStackSetup() tea.Cmd {
	folder, ok := m.changeableFolder()
	if !ok {
		m.status = tr(tHostFolderOnlyDir)
		return nil
	}
	target := realPath(folder)
	args := []string{"preview", "--output", "json", "--", hostchange.ActionFolderSetupStacks}
	args = append(args, hostchange.StackFolderArgs(target)...)
	return m.runHost(HostRequest{Args: args}, func(out []byte, err error) tea.Cmd {
		var p stackPreview
		if json.Unmarshal(bytes.TrimSpace(out), &p) == nil && p.Preview != nil {
			m.confirmStackSetup(target, p)
			return nil
		}
		// A refusal comes as the report of a refused change.
		if report, ok := parseReport(out); ok {
			m.hostResult(report, "")
			return nil
		}
		kind, exit := classifyRun(out, err)
		m.hostFailNotice(kind, exit, err, false)
		return nil
	})
}

func (m *Model) confirmStackSetup(target string, p stackPreview) {
	v := p.Preview
	if v.NothingToDo {
		m.hostNotice(question{title: tr(tHostStacksNothing, m.diffText(target)), calm: true,
			consequence: tr(tHostStacksNothingNote), keys: "{back} close"})
		return
	}
	q := question{title: tr(tHostStacksTitle, m.diffText(target)), keys: "y apply · n/{back} cancel"}
	q.consequence = tr(tHostReversible)
	if v.OthersLoseAccess {
		q.consequence = tr(tHostStacksOthers) + " " + q.consequence
		q.warn = true
	}
	lines := m.stackLines(target, v.Folder, v.Stacks, v.Kept, len(p.Commands) > 0)
	lines = append(lines, m.commandLines(p.Commands, stackDetailRoom(m.termHeight)-len(lines))...)
	q.details = []string{"\n" + strings.Join(lines, "\n")}
	argv := append([]string{"apply", "--yes", "--output", "json", "--", hostchange.ActionFolderSetupStacks},
		hostchange.StackFolderArgs(target)...)
	m.askConfirm(q, func() tea.Cmd { return m.runHostChange(argv, p.Sentence, nil, nil) })
}

// stackChangeText is the change of one entry in plain words. Owner and group are named only where they differ
// from the entry before, so a stack with three files reads as one line; prev is that entry (nil for the first).
func stackChangeText(c stackChange, prev *stackChange) string {
	after, before := c.After.String(), c.Before.String()
	if prev != nil && prev.After.Owner == c.After.Owner && prev.After.Group == c.After.Group {
		after = c.After.Mode
	}
	if prev != nil && prev.Before.Owner == c.Before.Owner && prev.Before.Group == c.Before.Group {
		before = c.Before.Mode
	}
	return after + " (was " + before + ")"
}

// entryName is how a change names its entry within a stack: the folder as the word, a file by its name.
func entryName(c stackChange) string {
	if c.Role == hostchange.RoleFolder || c.Role == hostchange.RoleStackFolder {
		return tr(tHostStacksFolder)
	}
	return path.Base(c.Path)
}

func keptText(base string, k stackKept) string {
	name := strings.TrimPrefix(k.Path, strings.TrimSuffix(base, "/")+"/")
	return tr(tHostStacksStays, name, k.Reason)
}

// stackLines are the lines of the confirmation: the folder, one block per stack, and what stays. When the
// room of a modal is used up, the stacks that do not fit are counted.
func (m *Model) stackLines(target string, folder *stackChange, stacks []stackInfo, kept []stackKept, commands bool) []string {
	var blocks [][]string
	if folder != nil {
		blocks = append(blocks, hostLines(styles.cell, "", m.diffText(target)+": "+stackChangeText(*folder, nil)))
	}
	for _, s := range stacks {
		name := s.Name
		if s.Running {
			name += " " + tr(tHostStacksRunning)
		}
		var parts []string
		var prev *stackChange
		for i, c := range s.Changes {
			parts = append(parts, entryName(c)+" "+stackChangeText(c, prev))
			prev = &s.Changes[i]
		}
		text := name + ": " + strings.Join(parts, ", ")
		if len(parts) == 0 {
			text = name + ": " + tr(tHostStacksSetUpAlready)
		}
		block := hostLines(styles.cell, "", m.diffText(text))
		for _, k := range s.Kept {
			block = append(block, hostLines(styles.hint, "  ", m.diffText(keptText(s.Path, k)))...)
		}
		blocks = append(blocks, block)
	}
	for _, k := range kept {
		blocks = append(blocks, hostLines(styles.hint, "", m.diffText(keptText(target, k))))
	}
	room := stackDetailRoom(m.termHeight)
	if commands {
		room-- // the commands keep at least one line
	}
	var lines []string
	for i, b := range blocks {
		limit := room
		if i < len(blocks)-1 {
			limit-- // keeps a line for the count
		}
		if len(lines)+len(b) > limit {
			return append(lines, hostLines(styles.hint, "", tr(tHostStacksMore, len(blocks)-i))...)
		}
		lines = append(lines, b...)
	}
	return lines
}

// commandLines are the exact commands, dimmed, as far as room lines hold whole commands; the rest is counted.
func (m *Model) commandLines(commands []string, room int) []string {
	var lines []string
	for i, c := range commands {
		block := hostLines(styles.hint, "$ ", m.diffText(c))
		if len(lines)+len(block) > room-1 && (i < len(commands)-1 || len(lines)+len(block) > room) {
			return append(lines, hostLines(styles.hint, "", tr(tHostStacksMoreCommands, len(commands)-i))...)
		}
		lines = append(lines, block...)
	}
	return lines
}
