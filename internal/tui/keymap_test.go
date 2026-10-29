package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// rebind replaces the key of an action for one test.
func rebind(t *testing.T, a action, key string) {
	t.Helper()
	old := keymap[a]
	setKeys(a, key)
	t.Cleanup(func() { keymap[a] = old })
}

func TestDefaultKeymapKeepsTheKnownKeys(t *testing.T) {
	want := map[action]string{
		actInfo: "i", actSave: "f2", actHelp: "?", actCommand: ":", actLock: "ctrl+l", actBack: "esc", actDelete: "d",
		actForceQuit: "ctrl+c", actUp: "up", actDown: "down", actLeft: "left", actRight: "right", actHome: "home",
		actEnd: "end", actPageUp: "pgup", actPageDown: "pgdown", actNext: "tab", actPrev: "shift+tab",
		actVimUp: "k", actVimDown: "j", actVimLeft: "h", actVimRight: "l",
		actTopicNext: "tab", actTopicPrev: "shift+tab",
		actEnter: "enter", actSpace: "space", actSearch: "/", actQuit: "q", actYes: "y", actNo: "n", actDiscard: "d",
		actSetup: "g", actCopy: "c", actNew: "n", actApproveAll: "a", actEdit: "e", actRename: "r", actReveal: "v",
		actSummary: "s", actFilter: "f", actRevert: "b", actLogs: "o", actDuplicate: "p",
		actTest: "t", actUpdate: "u", actWrite: "w",
		actRemoveField: "ctrl+d", actSuggestPrev: "ctrl+p", actSuggestNext: "ctrl+n", actNextKind: "ctrl+t",
		actReload: "ctrl+r",
	}
	if len(keymap) != len(want) {
		t.Fatalf("keymap has %d actions, want %d", len(keymap), len(want))
	}
	for a, key := range want {
		if keyOf(a) != key {
			t.Errorf("action %q is bound to %q, want %q", a, keyOf(a), key)
		}
	}
	if !keyIs(tea.KeyPressMsg{Code: 'i', Text: "i"}, actInfo) || keyIs(tea.KeyPressMsg{}, actInfo) {
		t.Error("the info action does not match exactly its key")
	}
	if keyLabel(actSave) != "F2" || keyLabel(actHelp) != "?" {
		t.Errorf("labels = %q, %q", keyLabel(actSave), keyLabel(actHelp))
	}
}

// Every key a footer names for a global action comes from the keymap: no screen file spells one as a
// literal in a footer text.
func TestFooterTextsNameGlobalKeysOnlyByPlaceholder(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{"? help", ": cmds", "F2 save", "F2 next", "F2 write", "F2 or enter", "esc back", "esc cancel", "esc close", "esc leave", "n/esc"}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "keymap.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") || !strings.Contains(line, `"`) {
				continue
			}
			for _, b := range banned {
				if strings.Contains(line, b) && strings.Contains(line, " · ") {
					t.Errorf("%s:%d spells %q in a footer text; use the key placeholder", f, i+1, b)
				}
			}
		}
	}
}

// assertNoCollisions fails when two actions of one level share a key.
func assertNoCollisions(t *testing.T) {
	t.Helper()
	seen := map[keyLevel]map[string]action{}
	for _, d := range keyDefs {
		if seen[d.level] == nil {
			seen[d.level] = map[string]action{}
		}
		for _, k := range keymap[d.act].Keys() {
			if other, ok := seen[d.level][k]; ok {
				t.Errorf("%s and %s share %q on level %s", other, d.act, k, d.level)
			}
			seen[d.level][k] = d.act
		}
	}
}

// keyLiteral finds the lines that spell a key value: a case item that is a string literal, or a comparison
// of a String() with one.
var keyLiteral = regexp.MustCompile(`^\s*case\s+(?:(?:"[^"]*"|[\w.]+\([^()]*\)|[\w.]+)\s*,\s*)*"[^"]*"\s*[,:]|\.String\(\)\s*[!=]=\s*"`)

// nonKeySwitches are files whose string cases are no keys (names, colour codes, footer groups, filter
// states). TestTableKeysAreNeverLiterals still covers them for every key of the table.
var nonKeySwitches = []string{"canvas.go", "footer.go", "logs.go", "mouse.go", "template.go", "tui.go"}

// tableKeys are the key names the keymap owns: every default key of every action. No file but keymap.go
// spells one as a key value.
func tableKeys() []string {
	var keys []string
	for _, d := range keyDefs {
		for _, k := range d.keys {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

// Key values live in the keymap: outside it, only the files of nonKeySwitches may switch on strings, and
// even those never on a key of the table.
func TestKeyLiteralsStayInTheKeymap(t *testing.T) {
	for _, f := range keyLiteralFindings(t, "*.go") {
		t.Errorf("%s", f)
	}
}

// The keys of the table have no literal anywhere: a case item or a comparison that spells one fails, in a
// file of nonKeySwitches as much as in any other.
func TestTableKeysAreNeverLiterals(t *testing.T) {
	for _, f := range tableKeyFindings(t, "*.go") {
		t.Errorf("%s", f)
	}
}

// tableKeyFindings lists the lines of the files matching glob that spell a key of the table as a literal.
func tableKeyFindings(t *testing.T, glob string) []string {
	t.Helper()
	files, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	type literal struct {
		key string
		re  *regexp.Regexp
	}
	var literals []literal
	for _, k := range tableKeys() {
		q := regexp.QuoteMeta(`"` + k + `"`)
		item := `^\s*case\s+(?:(?:"[^"]*"|[\w.]+\([^()]*\)|[\w.]+)\s*,\s*)*` + q + `\s*[,:]`
		literals = append(literals, literal{k, regexp.MustCompile(item + `|[!=]=\s*` + q + `|` + q + `\s*[!=]=`)})
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "keymap.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			for _, l := range literals {
				if l.re.MatchString(line) {
					found = append(found, f+":"+strconv.Itoa(i+1)+" spells the table key "+l.key+"; use the keymap: "+strings.TrimSpace(line))
				}
			}
		}
	}
	return found
}

// keyLiteralFindings lists the key literals of the files matching glob, except the files of nonKeySwitches.
func keyLiteralFindings(t *testing.T, glob string) []string {
	t.Helper()
	files, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "keymap.go" || slices.Contains(nonKeySwitches, f) {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if keyLiteral.MatchString(line) {
				found = append(found, f+":"+strconv.Itoa(i+1)+" spells a key value; use the keymap: "+strings.TrimSpace(line))
			}
		}
	}
	return found
}
