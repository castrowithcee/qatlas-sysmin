package tui

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files")

// colourSGR matches a select-graphic-rendition sequence that sets a colour (foreground, background,
// bright, or an extended colour), which NO_COLOR must keep out of the frame.
var colourSGR = regexp.MustCompile(`\x1b\[[0-9;:]*(?:3[0-9]|4[0-9]|9[0-9]|10[0-7])[0-9;:]*m`)

// goldenFrame is the frame at 80x24 as it is drawn with NO_COLOR set: the profile comes from detectProfile.
func goldenFrame(t *testing.T) *Model {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	restoreRenderer(t)
	setProfile(detectProfile(os.Stdout))
	if currentProfile() != profileNone {
		t.Fatal("NO_COLOR does not switch the colour profile off")
	}
	applyTheme(uiprefs.ThemeTerminal)
	useLanguage(t, "en")
	m := New(uiprefs.Default(), nil)
	m.title = "Services"
	m.table = table{
		columns: []column{{title: "Name"}, {title: "State"}, {title: "Note", flex: true}},
		rows: map[string][]string{
			"example-app": {"example-app", "running", "demo web service"},
			"demo-db":     {"demo-db", "running", "demo database"},
			"sample-job":  {"sample-job", "stopped", "demo nightly job"},
			"test-proxy":  {"test-proxy", "failed", "demo reverse proxy"},
		},
	}
	m.setEntries([]string{"example-app", "demo-db", "sample-job", "test-proxy"}, nil, nil)
	sizedModel(m, 80, 24)
	press(t, m, "down")
	return m
}

// assertGolden compares got with testdata/name.golden. The comparison is on the raw render, so an
// attribute such as bold or reverse stays part of the contract; only colour is forbidden.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	if loc := colourSGR.FindStringIndex(got); loc != nil {
		t.Errorf("frame carries a colour sequence under NO_COLOR: %q", got[loc[0]:loc[1]])
	}
	path := filepath.Join("testdata", name+".golden")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if got != string(want) {
		t.Errorf("render differs from %s:\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func TestGoldenList(t *testing.T) {
	m := goldenFrame(t)
	got := m.render()
	if !strings.Contains(got, "> ") {
		t.Errorf("the marked row is not shown by a mark without colour:\n%s", got)
	}
	assertGolden(t, "list_80x24", got)
}

func TestGoldenModal(t *testing.T) {
	m := goldenFrame(t)
	m.screen = screenConfirm
	m.ask = &question{title: "Stop the example service?", consequence: "The demo service stops serving requests.",
		keys: "y stop · n/{back} cancel"}
	assertGolden(t, "modal_80x24", m.render())
}
