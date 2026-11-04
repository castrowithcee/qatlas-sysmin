package tui

import (
	"slices"
	"strings"
	"testing"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// openCommands opens the command line and types text into it.
func openCommands(t *testing.T, m *Model, text string) *kittui.CommandLine {
	t.Helper()
	send(m, ":")
	if m.pop == nil || m.pop.cmd == nil {
		t.Fatalf("no command line is open:\n%s", plain(m))
	}
	typed(m, text)
	return m.pop.cmd
}

func commandNames(list []kittui.Command) []string {
	var names []string
	for _, c := range list {
		names = append(names, c.Name)
	}
	return names
}

// paletteRows are the commands the command line shows now, found by the name that opens each row.
func paletteRows(m *Model) []string {
	var got []string
	for _, line := range strings.Split(plain(m), "\n") {
		for _, cell := range strings.FieldsFunc(line, func(r rune) bool { return r == '│' || r == '║' }) {
			cell = strings.TrimLeft(cell, " >▌")
			for _, c := range m.commands() {
				if strings.HasPrefix(cell, c.Name+"  ") {
					got = append(got, c.Name)
					break
				}
			}
		}
	}
	return got
}

func TestColonStaysTextWhereTextIsTyped(t *testing.T) {
	m := appModel(t, overviewFake(), 100, 30)
	send(m, "3", "/")
	typed(m, ":a")
	if m.pop != nil || m.currentPage().list.Query() != ":a" {
		t.Errorf(": in the filter: palette %v, query %q, want text", m.pop != nil, m.currentPage().list.Query())
	}
}

func TestCommandSuggestionsMatchPrefixWordAliasAndCase(t *testing.T) {
	cases := []struct{ query, first string }{
		{"", "go overview"},
		{"QU", "quit"},
		{"exit", "quit"},
		{"refresh", "reload"},
		{"upgrade", "update"},
		{"settings", "go settings"},
		{"Settings   Update", "go settings update"},
		{"  activity   LOG ", "go activity log"},
		{"host groups", "go host groups"},
	}
	for _, c := range cases {
		m := appModel(t, overviewFake(), 100, 30)
		got := commandNames(openCommands(t, m, c.query).Matches())
		if len(got) == 0 || got[0] != c.first {
			t.Errorf("%q matches %v, want %q first", c.query, got, c.first)
		}
	}
	m := appModel(t, overviewFake(), 100, 30)
	if got := openCommands(t, m, "zzz").Matches(); len(got) != 0 {
		t.Errorf("zzz matches %v, want none", commandNames(got))
	}
}

func TestEmptyLineListsVisibleCommandsWithDescriptionAndKey(t *testing.T) {
	m := appModel(t, overviewFake(), 120, 40)
	all := openCommands(t, m, "").Matches()
	view := plain(m)
	if rows := paletteRows(m); len(rows) != min(len(all), 12) {
		t.Fatalf("the command line shows %d commands, want %d:\n%s", len(rows), min(len(all), 12), view)
	}
	for _, line := range strings.Split(view, "\n") {
		switch {
		case strings.Contains(line, "go overview"):
			if !strings.Contains(line, "Show Overview") || !strings.Contains(line, " 1 │") && !strings.Contains(line, " 1 ║") {
				t.Errorf("row lacks description or key: %q", line)
			}
		case strings.Contains(line, "go settings update"):
			t.Errorf("a hidden page is listed: %q", line)
		}
	}
	assertViewFits(t, view, 120, 40)
}

func TestEmptyLineKeysFollowTheBindingAndDescriptionsNameNoKey(t *testing.T) {
	m := appModel(t, overviewFake(), 120, 40)
	keys := map[string]string{"go overview": "1", "go settings": "4", "reload": m.keyLabel(actReload),
		"update": m.keyLabel(actSelfUpdate), "help": m.keyLabel(actHelp), "info": m.keyLabel(actInfo), "quit": m.keyLabel(actQuit)}
	openCommands(t, m, "")
	view := plain(m)
	for name, key := range keys {
		found := false
		for _, line := range strings.Split(view, "\n") {
			cells := strings.FieldsFunc(line, func(r rune) bool { return r == '│' || r == '║' })
			for _, cell := range cells {
				if fields := strings.Fields(strings.TrimLeft(cell, " >▌")); len(fields) > 1 &&
					strings.HasPrefix(strings.TrimLeft(cell, " >▌"), name+"  ") {
					found = true
					if fields[len(fields)-1] != key {
						t.Errorf("%s: row %q ends in %q, want the key %q", name, cell, fields[len(fields)-1], key)
					}
				}
			}
		}
		if !found {
			t.Errorf("%s: no row:\n%s", name, view)
		}
	}
	for _, c := range m.commands() {
		desc := m.tr(c.Description)
		for _, word := range strings.Fields(strings.ToLower(desc)) {
			if word == "esc" || word == "enter" || word == "tab" || strings.HasPrefix(word, "ctrl+") {
				t.Errorf("%s: the description names a key: %q", c.Name, desc)
			}
		}
	}
}

func TestCommandDescriptionsAreTranslated(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		m := appModel(t, overviewFake(), 120, 40)
		for _, c := range m.commands() {
			if text := m.tr(c.Description); text == "" || text == string(c.Description) {
				t.Errorf("%s: no text for %q", c.Name, c.Description)
			}
		}
	})
}

func TestPaletteScrollKeepsTheChoiceVisible(t *testing.T) {
	m := appModel(t, overviewFake(), 60, 14)
	all := openCommands(t, m, "").Matches()
	if len(paletteRows(m)) >= len(all) {
		t.Fatalf("all %d commands are shown: nothing to scroll", len(all))
	}
	for range len(all) - 1 {
		send(m, "down")
	}
	last := all[len(all)-1].Name
	if !slices.Contains(paletteRows(m), last) {
		t.Errorf("the last command is not visible (rows %v):\n%s", paletteRows(m), plain(m))
	}
	send(m, "down")
	if rows := paletteRows(m); len(rows) == 0 || rows[0] != all[0].Name {
		t.Errorf("down from the last does not wrap to the first:\n%s", plain(m))
	}
}

func TestEnterRunsTheChosenMatchAndEscClosesWithoutEffect(t *testing.T) {
	m := appModel(t, overviewFake(), 100, 30)
	openCommands(t, m, "go")
	send(m, "down", "enter")
	if m.pop != nil || m.area != areaHost {
		t.Errorf("the second match did not run: area %v", m.area)
	}
	openCommands(t, m, "go access")
	send(m, "esc")
	if m.pop != nil || m.area != areaHost {
		t.Errorf("esc had an effect: area %v", m.area)
	}
}

func TestAreaCommandsActLikeTheDigits(t *testing.T) {
	for a := area(0); a < areaCount; a++ {
		x, y := appModel(t, overviewFake(), 100, 30), appModel(t, overviewFake(), 100, 30)
		send(x, string(rune('1'+a)))
		runCommand(y, "go "+strings.ToLower(a.key()))
		if y.area != x.area || y.zone != x.zone || y.pop != nil {
			t.Errorf("%v: command area %v zone %v, key area %v zone %v", a, y.area, y.zone, x.area, x.zone)
		}
	}
}

func TestPageAliasesKeepTheOldNamesValid(t *testing.T) {
	cases := map[string]func(m *Model) bool{
		"Settings   Update": func(m *Model) bool { return m.area == areaSettings && m.set.sub == subUpdate },
		"settings general":  func(m *Model) bool { return m.area == areaSettings && m.set.sub == subGeneral },
		"activity log":      func(m *Model) bool { return m.area == areaActivity && m.act.sub == actLog },
		"host groups":       func(m *Model) bool { return m.area == areaHost && m.hst.sub == hostGroups },
		"go host users":     func(m *Model) bool { return m.area == areaHost && m.hst.sub == hostUsers },
	}
	for text, check := range cases {
		m := appModel(t, overviewFake(), 100, 30)
		runCommand(m, text)
		if m.pop != nil || !check(m) {
			t.Errorf("%q: area %v, panel open %v", text, m.area, m.pop != nil)
		}
	}
}

func TestUnknownCommandNamesTheInputAndTheNextStep(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		m := appModel(t, overviewFake(), 120, 30)
		openCommands(t, m, "Frob   nicate")
		send(m, "enter")
		view := flatText(strings.NewReplacer("│", " ", "║", " ").Replace(plain(m)))
		if want := flatText(m.tr(kittui.TextCommandUnknown, "frob nicate")); m.pop == nil || !strings.Contains(view, want) {
			t.Errorf("unknown command lacks %q:\n%s", want, plain(m))
		}
		send(m, "backspace")
		if strings.Contains(plain(m), `"frob nicate"`) {
			t.Error("the note stays after the input changed")
		}
	})
}

// The command runs the action, so it needs no key press: it reloads under any binding of the key table.
func TestReloadCommandNeedsNoKeyPress(t *testing.T) {
	byKey, byCommand := overviewFake(), overviewFake()
	x, y := appModel(t, byKey, 100, 30), appModel(t, byCommand, 100, 30)
	withTableKeys(t, y, actReload, "x")
	send(x, "ctrl+r")
	runCommand(y, "reload")
	if y.pop != nil || !slices.Equal(byKey.log(), byCommand.log()) {
		t.Errorf("reload calls %v, key calls %v, panel open %v", byCommand.log(), byKey.log(), y.pop != nil)
	}
}

func TestCommandsRunTheFunctionOfTheirKey(t *testing.T) {
	byKey, byCommand := overviewFake(), overviewFake()
	x, y := appModel(t, byKey, 100, 30), appModel(t, byCommand, 100, 30)
	send(x, "ctrl+r")
	runCommand(y, "reload")
	if !slices.Equal(byKey.log(), byCommand.log()) {
		t.Errorf("reload calls %v, key calls %v", byCommand.log(), byKey.log())
	}

	x, y = appModel(t, overviewFake(), 100, 30), appModel(t, overviewFake(), 100, 30)
	send(x, "i")
	runCommand(y, "info")
	if x.pop == nil || y.pop == nil || y.pop.kind() != popInfo || x.pop.kind() != y.pop.kind() {
		t.Errorf("info: %v, key: %v", y.pop, x.pop)
	}

	y = appModel(t, overviewFake(), 100, 30)
	runCommand(y, "exit")
	if !y.quitting {
		t.Error("exit does not quit")
	}
}

func TestPaletteClickRunsTheRow(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {50, 24}} {
		m := appModel(t, overviewFake(), size[0], size[1])
		send(m, ":")
		clickOn(t, m, "go host")
		if m.pop != nil || m.area != areaHost {
			t.Errorf("%dx%d: a click on the row left area %v, panel open %v\n%s", size[0], size[1], m.area, m.pop != nil, plain(m))
		}
	}
}

func TestPaletteFillsTheWorkspaceAtEverySize(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 20}, {59, 19}, {40, 14}} {
			m := appModel(t, overviewFake(), size[0], size[1])
			send(m, ":")
			view := plain(m)
			assertViewFits(t, view, size[0], size[1])
			if len(paletteRows(m)) == 0 {
				t.Errorf("%dx%d: no command row:\n%s", size[0], size[1], view)
			}
			typed(m, "help")
			send(m, "enter")
			if m.pop == nil || m.pop.kind() != popHelp {
				t.Errorf("%dx%d: enter did not run help", size[0], size[1])
			}
		}
	})
}

func TestPaletteReadsWithoutColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := appModel(t, overviewFake(), 120, 40)
	send(m, ":")
	if view := plain(m); !strings.Contains(view, "> go overview") {
		t.Errorf("the choice is not marked:\n%s", view)
	}
}

func TestFooterHasNoCommandHint(t *testing.T) {
	m := appModel(t, overviewFake(), 200, 30)
	for _, line := range []string{footerOf(m), plain(m)} {
		if strings.Contains(line, "via :") {
			t.Errorf("the footer hints at the commands: %q", line)
		}
	}
}

func runCommand(m *Model, name string) {
	send(m, ":")
	typed(m, name)
	send(m, "enter")
}
