package tui

import (
	"strings"
	"testing"

	kithelp "github.com/castrowithcee/qatlas-kit/helptopics"
	"github.com/castrowithcee/qatlas-kit/i18n"

	"github.com/castrowithcee/qatlas-sysmin/internal/helptopics"
)

// kind is which part is open.
func (p *popup) kind() popKind {
	switch {
	case p.info != nil:
		return popInfo
	case p.cmd != nil:
		return popCommand
	}
	return popHelp
}

func TestHelpInfoAndCommandsOpenAndCloseWithEsc(t *testing.T) {
	for key, kind := range map[string]popKind{"?": popHelp, "i": popInfo, ":": popCommand} {
		for _, size := range [][2]int{{80, 24}, {40, 14}} {
			m := appModel(t, overviewFake(), size[0], size[1])
			send(m, key)
			if m.pop == nil || m.pop.kind() != kind || m.screen != screenPopup {
				t.Fatalf("%s opens no part of kind %v", key, kind)
			}
			assertViewFits(t, plain(m), size[0], size[1])
			send(m, "esc")
			if m.pop != nil || m.screen != screenArea {
				t.Errorf("%dx%d: esc leaves the %s part open", size[0], size[1], key)
			}
		}
	}
}

func TestHelpTogglesWithItsOwnKeyAndNamesTheKeysOfTheScreen(t *testing.T) {
	m := appModel(t, overviewFake(), 120, 40)
	send(m, "?")
	// The help learns where its text ends when it is drawn.
	for range 40 {
		plain(m)
		send(m, "down")
	}
	view := plain(m)
	for _, want := range []string{"[qsysmin]", "host-admin", "host-tui", "←/→ area", "Everywhere", "F2 save"} {
		if !strings.Contains(view, want) {
			t.Errorf("the help lacks %q:\n%s", want, view)
		}
	}
	send(m, "right")
	if view := plain(m); !strings.Contains(view, "[host-admin]") || !strings.Contains(view, "The host commands read") {
		t.Errorf("the topic host-admin is not shown:\n%s", view)
	}
	send(m, "?")
	if m.pop != nil {
		t.Error("? does not close the help")
	}
}

// The help of the interface shows the topics of the command line, in each language under the same names.
func TestHelpShowsTheTopicsOfTheCommandLine(t *testing.T) {
	english := helptopics.Topics(i18n.English)
	eachLanguage(t, func(t *testing.T) {
		topics := helptopics.Topics(testLang)
		if len(topics) != len(english) {
			t.Fatalf("%d topics, English has %d", len(topics), len(english))
		}
		m := appModel(t, overviewFake(), 120, 40)
		send(m, "?")
		for i, topic := range topics {
			if topic.Name != english[i].Name {
				t.Errorf("topic %d is %q, English has %q", i, topic.Name, english[i].Name)
			}
			if m.pop.help.Topic() != topic.Name {
				t.Errorf("the help shows %q, want %q", m.pop.help.Topic(), topic.Name)
			}
			first, _, _ := strings.Cut(kithelp.Wrap(topic.Text, 40), "\n")
			if view := flatText(strings.ReplaceAll(plain(m), "║", " ")); !strings.Contains(view, "["+topic.Name+"]") || !strings.Contains(view, flatText(first)) {
				t.Errorf("the help does not show the topic %q:\n%s", topic.Name, plain(m))
			}
			send(m, "right")
		}
		if m.pop.help.Topic() != topics[0].Name {
			t.Errorf("right after the last topic shows %q, want the first again", m.pop.help.Topic())
		}
	})
}

func TestInfoDescribesTheFocusedEntry(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	focusCard(t, m, "Not enabled")
	send(m, "i")
	if view := flat(m); !strings.Contains(view, "Not enabled") || !strings.Contains(view, "not in the qsysmin group") {
		t.Errorf("no info for the focused row:\n%s", plain(m))
	}
	send(m, "esc", "4", "down")
	send(m, "i")
	if view := flat(m); !strings.Contains(view, "Theme") || !strings.Contains(view, "Colors of this interface") {
		t.Errorf("no info for the focused field:\n%s", plain(m))
	}
}

func TestInfoOfTheLogExplainsTheAuditLogTerm(t *testing.T) {
	eachLanguage(t, func(t *testing.T) {
		m := appModel(t, activityFake(), 120, 40)
		send(m, "3", "i")
		term, ok := kithelp.LookupTerm(helptopics.Terms(testLang), helptopics.TermAuditLog)
		if !ok {
			t.Fatal("no term audit-log")
		}
		words := strings.Join(strings.Fields(term.Text)[:3], " ")
		if view := plain(m); !strings.Contains(view, term.Title+": "+words) {
			t.Errorf("the info lacks the term:\n%s", plain(m))
		}
	})
}

func TestTermsKeepTheirContract(t *testing.T) {
	for _, lang := range []i18n.Lang{i18n.English, i18n.German} {
		if err := kithelp.CheckTerms(helptopics.Terms(lang)); err != nil {
			t.Errorf("%s: %v", lang, err)
		}
	}
}

func TestInfoCopiesItsText(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	focusCard(t, m, "Not enabled")
	send(m, "i")
	_, cmd := m.Update(keyMsg("c"))
	if cmd == nil || m.pop == nil || m.status != m.tr(tCopyHandedOver) {
		t.Errorf("c in the info copied nothing: status %q, open %v", m.status, m.pop != nil)
	}
}

func TestCommandLineChangesAreasAndPages(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	for text, check := range map[string]func() bool{
		"host":              func() bool { return m.area == areaHost },
		"Settings   Update": func() bool { return m.area == areaSettings && m.set.sub == subUpdate },
		"settings":          func() bool { return m.area == areaSettings && m.set.sub == subGeneral },
		"overview":          func() bool { return m.area == areaOverview },
	} {
		runCommand(m, text)
		if m.pop != nil || !check() {
			t.Errorf("%q: area %v page %d, panel open %v", text, m.area, m.set.sub, m.pop != nil)
		}
	}
}

func TestCommandLineRunsTheActionsOfTheKeys(t *testing.T) {
	m := appModel(t, newFake(), 80, 24)
	runCommand(m, "help")
	if m.pop == nil || m.pop.kind() != popHelp {
		t.Error("help opens no help")
	}
	send(m, "esc", ":")
	typed(m, "quit")
	_, cmd := m.Update(keyMsg("enter"))
	if cmd == nil || !m.quitting {
		t.Error("quit does not end the program")
	}
}

func TestPanelKeysDoNotActWhereTextIsTyped(t *testing.T) {
	m := appModel(t, overviewFake(), 80, 24)
	send(m, ":")
	typed(m, "?i:q")
	if m.pop == nil || m.pop.kind() != popCommand || m.pop.cmd.Query() != "?i:q" || m.quitting {
		t.Errorf("the command line did not take its text: %v", m.pop)
	}
}

func TestFooterNamesTheKeysOfTheScreenAndKeepsHelpVisible(t *testing.T) {
	for _, size := range [][2]int{{40, 14}, {60, 20}, {80, 24}, {120, 40}} {
		for _, page := range []string{"1", "2", "4"} {
			m := appModel(t, overviewFake(), size[0], size[1])
			send(m, page)
			rows := strings.Split(plain(m), "\n")
			foot := rows[len(rows)-1]
			if !strings.Contains(foot, "? help") {
				t.Errorf("%dx%d area %s: footer %q hides the help key", size[0], size[1], page, foot)
			}
		}
	}
	m := appModel(t, overviewFake(), 120, 40)
	rows := strings.Split(plain(m), "\n")
	for _, want := range []string{"←/→ area", "↓ open", "i info", ": cmds", "q quit"} {
		if !strings.Contains(rows[len(rows)-1], want) {
			t.Errorf("the footer of the overview lacks %q: %q", want, rows[len(rows)-1])
		}
	}
}
