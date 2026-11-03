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

	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// withTableKeys gives m a key table in which the action a has keys as its default keys, for a test of a table
// the code does not hold.
func withTableKeys(t *testing.T, m *Model, a action, keys ...string) {
	t.Helper()
	table := keyTable()
	for i := range table {
		if table[i].ID == a {
			table[i].DefaultKeys = keys
		}
	}
	km, err := kittui.NewKeymap(table, kittui.CharsetUnicode)
	if err != nil {
		t.Fatal(err)
	}
	m.ui.keys = km
}

func TestDefaultKeymapKeepsTheKnownKeys(t *testing.T) {
	want := map[action]string{
		actInfo: "i", actSave: "f2", actHelp: "?", actCommand: ":", actBack: "esc", actDelete: "d",
		actForceQuit: "ctrl+c", actUp: "up", actDown: "down", actLeft: "left", actRight: "right", actHome: "home",
		actEnd: "end", actPageUp: "pgup", actPageDown: "pgdown", actNext: "tab", actPrev: "shift+tab",
		actVimUp: "k", actVimDown: "j", actVimLeft: "h", actVimRight: "l",
		actTopicNext: "tab", actTopicPrev: "shift+tab",
		actEnter: "enter", actSpace: "space", actSearch: "/", actQuit: "q", actYes: "y", actNo: "n", actDiscard: "d",
		actCopy: "c", actNew: "n", actEdit: "e", actRename: "r",
		actFilter: "f",
		actTest:   "t", actSelfUpdate: "U",
		actSuggestPrev: "ctrl+p", actSuggestNext: "ctrl+n",
		actReload: "ctrl+r",
	}
	table := testKeys().Actions()
	if len(table) != len(want) {
		t.Fatalf("the key table has %d actions, want %d", len(table), len(want))
	}
	for _, a := range table {
		if len(a.DefaultKeys) != 1 || a.DefaultKeys[0] != want[a.ID] {
			t.Errorf("action %q is bound to %q, want %q", a.ID, a.DefaultKeys, want[a.ID])
		}
	}
	m := New(uiprefs.Default(), nil)
	if !m.keyIs(tea.KeyPressMsg{Code: 'i', Text: "i"}, actInfo) || m.keyIs(tea.KeyPressMsg{}, actInfo) {
		t.Error("the info action does not match exactly its key")
	}
	if m.keyLabel(actSave) != "F2" || m.keyLabel(actHelp) != "?" {
		t.Errorf("labels = %q, %q", m.keyLabel(actSave), m.keyLabel(actHelp))
	}
}

// The rebindable actions keep the names tui.yaml stores their keys under.
func TestRebindableActionsAreThoseOfThePreferences(t *testing.T) {
	var got []string
	for _, a := range testKeys().Actions() {
		if a.Rebind != kittui.RebindNone {
			got = append(got, string(a.ID))
		}
	}
	slices.Sort(got)
	want := slices.Sorted(slices.Values(uiprefs.Actions))
	if !slices.Equal(got, want) {
		t.Errorf("rebindable actions = %v, want %v", got, want)
	}
}

// The keys of the preferences act, and the extra info key works on text lines too.
func TestPreferenceKeysAreBound(t *testing.T) {
	prefs := uiprefs.Default()
	prefs.Keys = uiprefs.Keys{Save: "f3", Help: "ctrl+l", Command: ":", Info: "f4"}
	m := New(prefs, nil)
	if !m.keyIs(keyMsg("f3"), actSave) || m.keyIs(keyMsg("f2"), actSave) {
		t.Error("save is not on f3 alone")
	}
	if !m.keyIs(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}, actHelp) {
		t.Error("help is not on ctrl+l")
	}
	if !m.keyIsTyped(tea.KeyPressMsg{Code: tea.KeyF4}, actInfo) || m.keyIsTyped(tea.KeyPressMsg{Code: 'i', Text: "i"}, actInfo) {
		t.Error("on a text line only the extra key asks for the info")
	}
	if m.keyLabel(actInfo) != "i/F4" || m.firstKeyLabel(actInfo) != "i" {
		t.Errorf("info label = %q, first %q", m.keyLabel(actInfo), m.firstKeyLabel(actInfo))
	}
}

// A key the table refuses keeps the standard key of its action, with a warning, and the preferences name the
// key that acts.
func TestRefusedPreferenceKeysFallBackToTheStandard(t *testing.T) {
	prefs := uiprefs.Default()
	prefs.Keys = uiprefs.Keys{Save: "f3", Help: "ctrl+g", Command: ":"}
	m := newModel(prefs, nil, kittui.CharsetUnicode)
	warnings := m.bindKeys()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "keys.help ctrl+g") || !strings.Contains(warnings[0], "using ?") {
		t.Fatalf("warnings = %v, want one for help", warnings)
	}
	if m.keyLabel(actHelp) != "?" || m.keyLabel(actSave) != "F3" || m.prefs.Keys.Help != "?" {
		t.Errorf("help %q, save %q, preferences %+v", m.keyLabel(actHelp), m.keyLabel(actSave), m.prefs.Keys)
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

// keyLiteral finds the lines that spell a key value: a case item that is a string literal, or a comparison
// of a String() with one.
var keyLiteral = regexp.MustCompile(`^\s*case\s+(?:(?:"[^"]*"|[\w.]+\([^()]*\)|[\w.]+)\s*,\s*)*"[^"]*"\s*[,:]|\.String\(\)\s*[!=]=\s*"`)

// nonKeySwitches are files whose string cases are no keys (names, colour codes, footer groups, filter
// states). TestTableKeysAreNeverLiterals still covers them for every key of the table.
var nonKeySwitches = []string{"canvas.go", "footer.go", "logs.go", "mouse.go", "tui.go"}

// tableKeys are the key names the keymap owns: every default key of every action. No file but keymap.go
// spells one as a key value.
func tableKeys() []string {
	var keys []string
	for _, a := range keyTable() {
		for _, k := range a.DefaultKeys {
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
