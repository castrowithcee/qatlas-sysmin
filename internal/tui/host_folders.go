package tui

import (
	"errors"
	"io/fs"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// hostFoldersState is the Folders page: a path line that completes on the file system, and what the host says
// about the path last asked for. The line takes letters only while editing is set (enter or /), so help, info
// and the command line stay reachable; esc stops the editing, a second esc goes on to the bar.
type hostFoldersState struct {
	in      pathInput
	ready   bool
	editing bool
	asked   string
	folder  *hostadmin.Folder
	err     string
}

type hostFolderMsg struct {
	path   string
	folder hostadmin.Folder
	err    error
}

// foldersReady makes the path line on first use; the line starts empty, not yet editing, and suggests the root.
func (m *Model) foldersReady() *hostFoldersState {
	f := &m.hst.fold
	if !f.ready {
		f.in, f.ready = newPathInput(), true
		f.in.open("", pathFile, "/")
	}
	return f
}

// enterFolders is the page being shown: the directories are listed afresh and the path asked last is read again.
func (m *Model) enterFolders() tea.Cmd {
	f := m.foldersReady()
	f.editing = false
	f.in.open(f.in.Value(), pathFile, "/")
	return m.folderCmd(f.asked)
}

// hostTyping reports whether the path line takes the letters now.
func (m *Model) hostTyping() bool {
	return m.area == areaHost && m.hst.sub == hostFolders && m.zone == zoneContent && m.hst.fold.editing
}

func (m *Model) folderCmd(path string) tea.Cmd {
	if path == "" {
		return nil
	}
	src := m.hostSrc
	return func() tea.Msg {
		if src == nil {
			return hostFolderMsg{path: path, err: errNoHostSource}
		}
		folder, err := src.Folder(path)
		return hostFolderMsg{path, folder, err}
	}
}

func (m *Model) takeHostFolder(msg hostFolderMsg) {
	f := &m.hst.fold
	if msg.path != f.asked {
		return
	}
	f.folder, f.err = nil, ""
	switch {
	case errors.Is(msg.err, hostadmin.ErrNotFound):
		f.err = tr(tHostFolderNotFound, m.diffText(msg.path))
	case errors.Is(msg.err, fs.ErrPermission):
		f.err = tr(tHostFolderDenied, m.diffText(msg.path))
	case msg.err != nil:
		f.err = m.diffText(msg.err.Error())
	default:
		f.folder = &msg.folder
	}
}

// foldersKey handles a key while the path line takes the letters. tab completes, the arrows and ctrl+n and
// ctrl+p move the suggestion, enter takes a marked suggestion first and reads the path otherwise.
func (m *Model) foldersKey(key tea.KeyPressMsg) tea.Cmd {
	f := m.foldersReady()
	in := &f.in
	switch {
	case m.keyIs(key, actBack):
		f.editing = false
		in.Blur()
	case m.keyIs(key, actNext):
		in.complete()
	case m.keyIs(key, actEnter):
		if in.marked && in.complete() {
			return nil
		}
		f.asked = strings.TrimSpace(in.Value())
		f.folder, f.err = nil, ""
		return m.folderCmd(f.asked)
	case m.keyIs(key, actSuggestNext), m.keyIs(key, actDown):
		in.move(1)
	case m.keyIs(key, actSuggestPrev), m.keyIs(key, actUp):
		in.move(-1)
	case m.keyIsTyped(key, actInfo):
		m.openPopup(popInfo)
	default:
		cmd, _ := in.edit(key)
		return cmd
	}
	return nil
}

func hostAccessText(a hostadmin.Access) string {
	switch a {
	case hostadmin.AccessReadWrite:
		return tr(tHostAccReadWrite)
	case hostadmin.AccessRead:
		return tr(tHostAccRead)
	}
	return tr(tHostAccNothing)
}

func hostFolderKind(kind string) string {
	switch {
	case kind == "directory":
		return tr(tHostKindFolder)
	case kind == "file":
		return tr(tHostKindFile)
	}
	return tr(tHostKindOther)
}

// folderLines are the facts of a path in plain words.
func (m *Model) folderLines(f hostadmin.Folder, width int) []string {
	dir := f.Kind == "directory"
	pairs := [][2]string{
		{tr(tColType), hostFolderKind(f.Kind)},
		{tr(tHostOwnerOf, m.diffText(f.Owner)), hostAccessText(f.OwnerAcc)},
		{tr(tHostGroupOf, m.diffText(f.Group)), hostAccessText(f.GroupAcc)},
		{tr(tHostOthers), hostAccessText(f.OthersAcc)},
		{tr(tHostMode), m.diffText(f.Mode)},
	}
	if f.Resolved != "" && f.Resolved != f.Path {
		pairs = append([][2]string{{tr(tHostResolved), m.diffText(f.Resolved)}}, pairs...)
	}
	switch {
	case f.ACL == nil:
		pairs = append(pairs, [2]string{tr(tHostACL), tr(tHostUnknown)})
	case *f.ACL:
		pairs = append(pairs, [2]string{tr(tHostACL), tr(tHostACLSet)})
	}
	special := func(on bool, folder, other textID) {
		if !on {
			return
		}
		id := other
		if dir {
			id = folder
		}
		pairs = append(pairs, [2]string{tr(tHostSpecial), tr(id)})
	}
	special(f.Setuid, tHostSetuidDir, tHostSetuid)
	special(f.Setgid, tHostSetgidDir, tHostSetgid)
	special(f.Sticky, tHostStickyDir, tHostSticky)
	if f.Protected {
		pairs = append(pairs, [2]string{tr(tHostProtected), tr(tHostProtectedNote)})
	}
	return factLines(pairs, width)
}

// hostFoldersBody is the Folders page under the bars of the frame.
func (m *Model) hostFoldersBody() string {
	f := m.foldersReady()
	width := m.usable(0)
	prefix := tr(tHostPathLabel) + ": "
	room := max(width-len([]rune(prefix))-1, 1)
	in := f.in.Model
	in.Placeholder = tr(tHostPathPlaceholder)
	if !f.editing {
		in.Blur()
	}
	lines := []string{styles.title.Render(tr(tSubFolders)), "", prefix + inputView(fitInput(in, room))}
	if f.editing {
		suggestions, mark := f.in.shown()
		for j, s := range suggestions {
			if f.in.marked && j == mark {
				lines = append(lines, styles.title.Render(truncateCells("  > "+s, width-1)))
			} else {
				lines = append(lines, styles.hint.Render(truncateCells("    "+s, width-1)))
			}
		}
	}
	lines = append(lines, "")
	switch {
	case f.err != "":
		lines = append(lines, strings.Split(render(styles.fail, width, f.err), "\n")...)
	case f.folder != nil:
		lines = append(lines, m.folderLines(*f.folder, width)...)
	default:
		lines = append(lines, strings.Split(render(styles.hint, width, tr(tHostFolderHint)), "\n")...)
	}
	return strings.Join(lines, "\n")
}

// hostFoldersKeys is the key line of Folders while the line takes letters.
func (m *Model) hostFoldersKeys() string {
	keys := "enter read · {back} stop"
	if len(m.foldersReady().in.matches) > 0 {
		keys = "enter read · tab complete · ctrl+n/ctrl+p suggestion · {back} stop"
	}
	return keys
}
