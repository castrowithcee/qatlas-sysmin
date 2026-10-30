package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

func commandNames(list []command) []string {
	var names []string
	for _, c := range list {
		names = append(names, c.name)
	}
	return names
}

func commandByName(t *testing.T, m *Model, name string) command {
	t.Helper()
	for _, c := range m.commands() {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("no command %q", name)
	return command{}
}

// paletteRows are the commands the palette shows now, found by the name that opens each row.
func paletteRows(m *Model) []string {
	var got []string
	for _, line := range strings.Split(plain(m), "\n") {
		line = strings.TrimLeft(line, "│║ >")
		for _, c := range m.commands() {
			if strings.HasPrefix(line, c.name+"  ") {
				got = append(got, c.name)
				break
			}
		}
	}
	return got
}

func TestColonStaysTextWhereTextIsTyped(t *testing.T) {
	m := projectsModel(t, projectsFake(), 100, 30)
	send(m, "/")
	typed(m, ":a")
	if m.pop != nil || m.prj.list.query() != ":a" {
		t.Errorf(": in the filter: palette %v, query %q, want text", m.pop != nil, m.prj.list.query())
	}
}

func TestCommandSuggestionsMatchPrefixWordAliasAndCase(t *testing.T) {
	m := projectsModel(t, projectsFake(), 100, 30)
	list := m.commands()
	cases := []struct{ query, first string }{
		{"", "go overview"},
		{"QU", "quit"},
		{"pau", "pause"},
		{"run", "run now"},
		{"restore", "restore backup"},
		{"exit", "quit"},
		{"access discard-all", "discard all"},
		{"lock", "admin session"},
		{"unlock", "admin session"},
		{"refresh", "reload"},
		{"upgrade", "update"},
		{"adopt", "adopt stack"},
		{"new from project", "duplicate project"},
		{"new", "new project"},
		{"settings", "go settings"},
		{"Settings   Update", "go settings update"},
		{"  activity   LOG ", "go activity log"},
		{"library templates", "go library templates"},
	}
	for _, c := range cases {
		got := commandNames(matchCommands(list, c.query))
		if len(got) == 0 || got[0] != c.first {
			t.Errorf("%q matches %v, want %q first", c.query, got, c.first)
		}
	}
	if got := matchCommands(list, "zzz"); len(got) != 0 {
		t.Errorf("zzz matches %v, want none", commandNames(got))
	}
}

func TestMatchRanksExactBeforePrefixBeforeWordBeforeMidWord(t *testing.T) {
	list := []command{{name: "xpause"}, {name: "do not pause"}, {name: "pause later"}, {name: "pau"}, {name: "pause"}}
	got := commandNames(matchCommands(list, "pau"))
	want := []string{"pau", "pause later", "pause", "do not pause", "xpause"}
	if !slices.Equal(got, want) {
		t.Errorf("ranked %v, want %v", got, want)
	}
	if got := commandNames(matchCommands(list, "pause")); got[0] != "pause" {
		t.Errorf("the exact name is not first: %v", got)
	}
}

func TestEmptyLineListsVisibleCommandsWithDescriptionAndKey(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 40)
	send(m, ":")
	view := plain(m)
	rows := paletteRows(m)
	if len(rows) != paletteMaxRows {
		t.Fatalf("the palette shows %d commands, want %d:\n%s", len(rows), paletteMaxRows, view)
	}
	for _, line := range strings.Split(view, "\n") {
		switch {
		case strings.Contains(line, "go overview"):
			if !strings.Contains(line, "Show Overview") || !strings.Contains(line, " 1 │") {
				t.Errorf("row lacks description or key: %q", line)
			}
		case strings.Contains(line, "go settings vault"):
			t.Errorf("a hidden page is listed: %q", line)
		}
	}
	if !strings.Contains(view, "1-12 of ") {
		t.Errorf("no position of the list:\n%s", view)
	}
	assertViewFits(t, view, 120, 40)
}

func TestEmptyLineKeysFollowTheBindingAndDescriptionsNameNoKey(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 40)
	keys := map[string]string{"go overview": "1", "go settings": "6", "new project": keyLabel(actNew), "edit project": keyLabel(actEdit),
		"duplicate project": keyLabel(actDuplicate), "admin session": keyLabel(actLock), "reload": keyLabel(actReload),
		"update": keyLabel(actSelfUpdate), "help": keyLabel(actHelp), "info": keyLabel(actInfo), "quit": keyLabel(actQuit)}
	for _, c := range matchCommands(m.commands(), "") {
		want, has := keys[c.name]
		got := ""
		if c.shortcut != nil {
			got = c.shortcut()
		}
		if has && got != want {
			t.Errorf("%s: key %q, want %q", c.name, got, want)
		}
		if !has && got != "" && c.name != "setup" && !strings.HasPrefix(c.name, "go ") {
			t.Errorf("%s: unexpected key %q", c.name, got)
		}
		for _, word := range strings.Fields(strings.ToLower(c.desc)) {
			if len(word) > 1 && (word == "esc" || word == "enter" || word == "tab" || strings.HasPrefix(word, "ctrl+")) {
				t.Errorf("%s: the description names a key: %q", c.name, c.desc)
			}
		}
	}
}

func TestPaletteScrollKeepsTheChoiceVisible(t *testing.T) {
	m := projectsModel(t, projectsFake(), 120, 40)
	send(m, ":")
	all := matchCommands(m.commands(), "")
	if len(all) <= paletteMaxRows {
		t.Fatalf("only %d commands: nothing to scroll", len(all))
	}
	for range len(all) - 1 {
		send(m, "down")
	}
	last := all[len(all)-1].name
	found := false
	for _, line := range strings.Split(plain(m), "\n") {
		found = found || (strings.Contains(line, "> "+last+"  "))
	}
	if !found || len(paletteRows(m)) > paletteMaxRows {
		t.Errorf("the last command is not the visible choice (rows %v):\n%s", paletteRows(m), plain(m))
	}
	send(m, "down")
	if m.pop.cursor != 0 {
		t.Errorf("down from the last does not wrap, cursor %d", m.pop.cursor)
	}
	send(m, "shift+tab")
	if m.pop.cursor != len(all)-1 {
		t.Errorf("shift+tab from the first does not wrap, cursor %d", m.pop.cursor)
	}
	send(m, "tab")
	if m.pop.cursor != 0 {
		t.Errorf("tab from the last does not wrap, cursor %d", m.pop.cursor)
	}
}

func TestEnterRunsTheChosenMatchAndEscClosesWithoutEffect(t *testing.T) {
	m := appModel(t, overviewFake(), 100, 30)
	send(m, ":")
	typed(m, "go")
	send(m, "down", "enter")
	if m.pop != nil || m.area != areaProjects {
		t.Errorf("the second match did not run: area %v", m.area)
	}
	send(m, ":")
	typed(m, "go access")
	send(m, "esc")
	if m.pop != nil || m.area != areaProjects {
		t.Errorf("esc had an effect: area %v", m.area)
	}
}

func TestAreaCommandsActLikeTheDigits(t *testing.T) {
	for a := area(0); a < areaCount; a++ {
		x, y := appModel(t, overviewFake(), 100, 30), appModel(t, overviewFake(), 100, 30)
		send(x, string(rune('1'+a)))
		send(y, ":")
		typed(y, "go "+strings.ToLower(a.key()))
		send(y, "enter")
		if y.area != x.area || y.zone != x.zone || y.pop != nil {
			t.Errorf("%v: command area %v zone %v, key area %v zone %v", a, y.area, y.zone, x.area, x.zone)
		}
	}
}

func TestPageAliasesKeepTheOldNamesValid(t *testing.T) {
	cases := map[string]func(m *Model) bool{
		"settings vault":         func(m *Model) bool { return m.area == areaSettings && m.set.sub == subVault },
		"Settings   Update":      func(m *Model) bool { return m.area == areaSettings && m.set.sub == subUpdate },
		"settings general":       func(m *Model) bool { return m.area == areaSettings && m.set.sub == subGeneral },
		"activity runs":          func(m *Model) bool { return m.area == areaActivity && m.act.sub == actRuns },
		"activity log":           func(m *Model) bool { return m.area == areaActivity && m.act.sub == actLog },
		"library channels":       func(m *Model) bool { return m.area == areaLibrary && m.lib.sub == libChannels },
		"go library credentials": func(m *Model) bool { return m.area == areaLibrary && m.lib.sub == libCredentials },
	}
	for text, check := range cases {
		m := appModel(t, overviewFake(), 100, 30)
		runCommand(m, text)
		if m.pop != nil || !check(m) {
			t.Errorf("%q: area %v, panel open %v", text, m.area, m.pop != nil)
		}
	}
}

func TestProjectControlsAreAliasesAndReachableOnlyOnAProject(t *testing.T) {
	fb := projectsFake()
	m := projectsModel(t, fb, 100, 30)
	runCommand(m, "pause")
	runCommand(m, "pin")
	runCommand(m, "unpin")
	runCommand(m, "resume")
	runCommand(m, "run")
	send(m, "y")
	for _, want := range []string{"deploy.pause", "deploy.pin", "deploy.unpin", "deploy.resume"} {
		found := false
		for _, c := range fb.log() {
			found = found || strings.Contains(c, want)
		}
		if !found {
			t.Errorf("no %s call in %v", want, fb.log())
		}
	}
	if m.pop != nil {
		t.Error("a command that ran left the palette open")
	}
}

func TestPauseOutsideProjectsAndWithoutAProjectStaysAndSaysWhy(t *testing.T) {
	m := appModel(t, overviewFake(), 100, 30)
	runCommand(m, "pause")
	if m.pop == nil || m.pop.in.Value() != "pause" || !strings.Contains(plain(m), "pause: Works in Projects only.") {
		t.Errorf("outside Projects:\n%s", plain(m))
	}
	m = projectsModel(t, newFake(control.VaultUnlocked), 100, 30)
	runCommand(m, "pause")
	if m.pop == nil || m.pop.in.Value() != "pause" || !strings.Contains(plain(m), "pause: No project selected.") {
		t.Errorf("without a project:\n%s", plain(m))
	}
	m = projectsModel(t, projectsFake(), 100, 30)
	send(m, "o")
	runCommand(m, "pause")
	if m.pop == nil || !strings.Contains(plain(m), "Close the open log or update first.") {
		t.Errorf("with a log open:\n%s", plain(m))
	}
}

func TestUnknownCommandNamesTheInputAndTheNextStep(t *testing.T) {
	m := appModel(t, overviewFake(), 100, 30)
	send(m, ":")
	typed(m, "Frob   nicate")
	send(m, "enter")
	view := plain(m)
	if m.pop == nil || !strings.Contains(view, `No command "Frob nicate".`) || !strings.Contains(view, "Clear the line") {
		t.Errorf("unknown command:\n%s", view)
	}
	send(m, "backspace")
	if strings.Contains(plain(m), "No command \"") && strings.Contains(plain(m), "Clear the line") {
		t.Error("the note stays after the input changed")
	}
	useLanguage(t, "de")
	typed(m, "x")
	send(m, "enter")
	if view := plain(m); !strings.Contains(view, `Kein Befehl "`) || !strings.Contains(view, "Leere die Zeile") {
		t.Errorf("German note:\n%s", view)
	}
}

func TestCommandsActOnlyWhereTheirKeyActs(t *testing.T) {
	flow := projectsModel(t, projectsFake(), 100, 30)
	send(flow, "n", "enter")
	logOpen := projectsModel(t, projectsFake(), 100, 30)
	send(logOpen, "o")
	onTabs := projectsModel(t, projectsFake(), 100, 30)
	onTabs.toBar()
	access := appModel(t, accessFake(), 100, 30)
	send(access, "4")
	overview := appModel(t, overviewFake(), 100, 30)
	noVault := appModel(t, newFake(control.VaultAbsent), 100, 30)
	vault := projectsModel(t, projectsFake(), 100, 30)
	empty := projectsModel(t, newFake(control.VaultUnlocked), 100, 30)

	cases := []struct {
		name   string
		model  *Model
		reason string
	}{
		{"go access", flow, "guided setup"}, {"quit", flow, "guided setup"}, {"update", flow, "guided setup"},
		{"reload", flow, "guided setup"}, {"new project", flow, "guided setup"}, {"pause", flow, "guided setup"},
		{"info", flow, ""}, {"help", flow, ""}, {"admin session", flow, ""},
		{"pause", logOpen, "log or update"}, {"new project", logOpen, "log or update"}, {"adopt stack", logOpen, "log or update"},
		{"new project", onTabs, "focus"}, {"edit project", onTabs, "focus"}, {"pause", onTabs, ""}, {"adopt stack", onTabs, ""},
		{"restore backup", overview, "Projects only"}, {"edit project", overview, "Projects only"},
		{"duplicate project", overview, "Projects only"}, {"adopt stack", overview, "Projects only"},
		{"resume", overview, "Projects only"}, {"run now", overview, "Projects only"}, {"unpin", overview, "Projects only"},
		{"edit project", empty, "No project selected"}, {"duplicate project", empty, "No project selected"},
		{"restore backup", empty, "No project selected"}, {"new project", empty, ""},
		{"discard all", overview, "Access only"}, {"discard all", vault, "Access only"}, {"discard all", access, ""},
		{"setup", vault, "A vault exists already."}, {"setup", noVault, ""},
		{"go overview", overview, ""}, {"reload", overview, ""}, {"regroup", overview, ""}, {"quit", overview, ""},
	}
	for _, c := range cases {
		got := commandByName(t, c.model, c.name).reason(c.model)
		if (c.reason == "") != (got == "") || !strings.Contains(got, c.reason) {
			t.Errorf("%s: reason %q, want %q", c.name, got, c.reason)
		}
	}
}

func TestCommandsRunTheFunctionOfTheirKey(t *testing.T) {
	// reload is ctrl+r of the area shown, here the project list.
	byKey, byCommand := projectsFake(), projectsFake()
	x, y := projectsModel(t, byKey, 100, 30), projectsModel(t, byCommand, 100, 30)
	send(x, "ctrl+r")
	runCommand(y, "reload")
	if !slices.Equal(byKey.log(), byCommand.log()) {
		t.Errorf("reload calls %v, key calls %v", byCommand.log(), byKey.log())
	}

	x, y = projectsModel(t, editFake(), 100, 30), projectsModel(t, editFake(), 100, 30)
	send(x, "n")
	runCommand(y, "new project")
	if x.prj.dialog == nil || y.prj.dialog == nil || x.prj.dialog.kind != projDlgPick || y.prj.dialog.kind != projDlgPick {
		t.Errorf("new project: dialog %+v, key n: dialog %+v", y.prj.dialog, x.prj.dialog)
	}
	for command, key := range map[string]string{"edit project": "e", "duplicate project": "p"} {
		x, y := projectsModel(t, editFake(), 100, 30), projectsModel(t, editFake(), 100, 30)
		send(x, key)
		runCommand(y, command)
		if x.prj.flow == nil || y.prj.flow == nil || x.prj.flow.draft.name != y.prj.flow.draft.name {
			t.Errorf("%s: flow %+v, key %s: flow %+v", command, y.prj.flow, key, x.prj.flow)
		}
	}

	x, y = projectsModel(t, projectsFake(), 100, 30), projectsModel(t, projectsFake(), 100, 30)
	send(x, "i")
	runCommand(y, "info")
	if x.pop == nil || y.pop == nil || y.pop.kind != popInfo || x.pop.kind != y.pop.kind {
		t.Errorf("info: %v, key: %v", y.pop, x.pop)
	}

	y = projectsModel(t, projectsFake(), 100, 30)
	runCommand(y, "exit")
	if !y.quitting {
		t.Error("exit does not quit")
	}
}

func TestPaletteClickRunsTheRow(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {50, 24}} {
		m := projectsModel(t, projectsFake(), size[0], size[1])
		send(m, ":")
		clickOn(t, m, "go access")
		if m.pop != nil || m.area != areaAccess {
			t.Errorf("%dx%d: a click on the row left area %v, panel open %v\n%s", size[0], size[1], m.area, m.pop != nil, plain(m))
		}
	}
	// A click on a command that cannot run keeps the line and names the reason.
	m := appModel(t, overviewFake(), 120, 40)
	send(m, ":")
	clickOn(t, m, "pause")
	if m.pop == nil || !strings.Contains(plain(m), "pause: Works in Projects only.") {
		t.Errorf("click on an unavailable row:\n%s", plain(m))
	}
}

func TestPaletteIsAnOverlayAndTakesTheWorkspaceWhereTheTerminalIsSmall(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 20}, {59, 19}, {40, 14}} {
			m := projectsModel(t, projectsFake(), size[0], size[1])
			send(m, ":")
			view := plain(m)
			assertViewFits(t, view, size[0], size[1])
			if modal := strings.Contains(view, "╭"); modal != (size[0] >= modalMinWidth && size[1] >= modalMinHeight) {
				t.Errorf("%dx%d: overlay = %v", size[0], size[1], modal)
			}
			if len(paletteRows(m)) == 0 {
				t.Errorf("%dx%d: no command row:\n%s", size[0], size[1], view)
			}
			typed(m, "help")
			send(m, "enter")
			if m.pop == nil || m.pop.kind != popHelp {
				t.Errorf("%dx%d: enter did not run help", size[0], size[1])
			}
		}
	})
}

func TestPaletteReadsWithoutColour(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := projectsModel(t, projectsFake(), 120, 40)
	send(m, ":")
	if view := plain(m); !strings.Contains(view, "> go overview") {
		t.Errorf("the choice is not marked:\n%s", view)
	}
}

func TestProjectsFooterHasNoCommandHint(t *testing.T) {
	m := projectsModel(t, projectsFake(), 200, 30)
	for _, line := range []string{m.footerLine(), plain(m)} {
		if strings.Contains(line, "via :") || strings.Contains(line, "run/pause") {
			t.Errorf("the footer hints at the commands: %q", line)
		}
	}
}
