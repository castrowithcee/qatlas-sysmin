package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func buttonKeys(buttons []dialogButton) string {
	var out []string
	for _, b := range buttons {
		out = append(out, b.key+"="+b.label)
	}
	return strings.Join(out, ",")
}

func TestDialogButtonsAreReadFromTheKeyLine(t *testing.T) {
	m := frameModel(t, 80, 24)
	for _, c := range []struct {
		keys string
		want string
	}{
		{"y stop · n/{back} cancel", "y=Stop,n=Cancel"},
		{"enter save · {back} cancel", "enter=Save,esc=Cancel"},
		{"y yes · n/{back} no, nothing changes · {help} help", "y=Yes,n=No"},
		{"enter or y confirm · {back} back", "enter=Confirm,esc=Back"},
	} {
		if got := buttonKeys(m.dialogButtons(c.keys, false, nil)); got != c.want {
			t.Errorf("%q gives %q, want %q", c.keys, got, c.want)
		}
	}
	// Groups of the editor around the dialog are no answers.
	for _, b := range m.dialogButtons("y yes · {help} help · {command} cmds · up/down move", false, nil) {
		if b.key != "y" {
			t.Errorf("%q became a button", b.key)
		}
	}
	if got := m.dialogButtons("y remove · n/{back} cancel", false, map[string]string{"y": "Remove it"}); got[0].label != "Remove it" {
		t.Errorf("label = %q, want the given label", got[0].label)
	}
}

func TestEnterGroupIsTheFocusedButton(t *testing.T) {
	m := frameModel(t, 80, 24)
	buttons := m.dialogButtons("enter save · {back} cancel", false, nil)
	if !buttons[0].focused || buttons[1].focused {
		t.Errorf("focus = %v, %v, want the enter answer only", buttons[0].focused, buttons[1].focused)
	}
	for _, b := range m.dialogButtons("y yes · n/{back} no", false, nil) {
		if b.focused {
			t.Errorf("%q is focused although enter answers nothing", b.key)
		}
	}
}

// Only a question that destroys something draws its answer as the danger button.
func TestOnlyDestructiveQuestionsHaveADangerButton(t *testing.T) {
	m := frameModel(t, 80, 24)
	kinds := func(keys string, danger bool) map[string]buttonKind {
		out := map[string]buttonKind{}
		for _, b := range m.dialogButtons(keys, danger, nil) {
			out[b.key] = b.kind
		}
		return out
	}
	if got := kinds("y remove · n/{back} cancel", true); got["y"] != buttonDanger || got["n"] != buttonNormal {
		t.Errorf("danger question: %v", got)
	}
	if got := kinds("d delete · {back} cancel", true); got["d"] != buttonDanger {
		t.Errorf("discard answer: %v", got)
	}
	if got := kinds("y lock · n/{back} cancel", false); got["y"] != buttonPrimary {
		t.Errorf("a harmless question has a danger button: %v", got)
	}
}

func TestButtonLabelIsTheCapitalisedActionWithoutItsExplanation(t *testing.T) {
	for in, want := range map[string]string{
		"n/esc cancel, nothing saved": "Cancel",
		"enter or y confirm":          "Confirm",
		"y yes":                       "Yes",
		"y":                           "",
	} {
		if got := buttonLabel(in); got != want {
			t.Errorf("buttonLabel(%q) = %q, want %q", in, got, want)
		}
	}
	if got := buttonLabel("y " + strings.Repeat("long ", 20)); len([]rune(got)) > 24 {
		t.Errorf("a long label is not cut: %q", got)
	}
}

func TestButtonRowsWrapAtTheWidthAndLogTheirKeysAsSpans(t *testing.T) {
	m := frameModel(t, 80, 24)
	var log []footerLine
	m.footerLog = &log
	buttons := m.dialogButtons("y yes · n no · a all · enter save", false, nil)
	rows := m.buttonRows(buttons)
	lines := strings.Split(ansi.Strip(rows), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "[ Yes ]  [ No ]") {
		t.Fatalf("rows = %q", lines)
	}
	if len(log) != 1 || len(log[0].spans) != len(buttons) {
		t.Fatalf("log = %+v", log)
	}
	for i, span := range log[0].spans {
		if span.key != buttons[i].key || span.x1 <= span.x0 {
			t.Errorf("span %d = %+v for %q", i, span, buttons[i].key)
		}
	}
	if got := log[0].plain[log[0].spans[1].x0:log[0].spans[1].x1]; got != "[ No ]" {
		t.Errorf("the span of the second button covers %q", got)
	}

	// A modal narrower than the buttons puts them on further rows.
	m.width = 20
	log = nil
	if got := strings.Split(m.buttonRows(buttons), "\n"); len(got) < 2 || len(log) != len(got) {
		t.Errorf("narrow: %d rows, %d logged", len(got), len(log))
	}
}

func TestClickOnAModalButtonPressesItsKey(t *testing.T) {
	m := frameModel(t, 80, 24)
	m.screen = screenConfirm
	m.ask = &question{title: "Stop the example service?", consequence: "It stops.", keys: "y stop · n/{back} cancel"}
	view := plain(m)
	if !strings.Contains(view, "[ Stop ]") || !strings.Contains(view, "[ Cancel ]") {
		t.Fatalf("the question shows no buttons:\n%s", view)
	}
	cell := func(text string) (x, y int) {
		for y, line := range strings.Split(plain(m), "\n") {
			if at := strings.Index(line, text); at >= 0 {
				return ansi.StringWidth(line[:at]) + 2, y
			}
		}
		t.Fatalf("no %q on the screen:\n%s", text, plain(m))
		return 0, 0
	}
	x, y := cell("[ Stop ]")
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if m.screen != screenConfirm {
		t.Error("the frame answered y on its own: only the editor around it knows what y does")
	}
	x, y = cell("[ Cancel ]")
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if m.screen != screenList || m.ask != nil {
		t.Errorf("a click on Cancel left screen %v", m.screen)
	}
}

func TestDialogsReadWithoutColourAndFitTheSmallestTerminal(t *testing.T) {
	m := goldenFrame(t)
	m.screen = screenConfirm
	m.ask = &question{title: "Stop the example service?", consequence: "It stops.", danger: true,
		keys: "enter or y stop · n/{back} cancel"}
	view := m.render()
	if colourSGR.MatchString(view) || !strings.Contains(view, "> Stop <") || !strings.Contains(view, "[ Cancel ]") {
		t.Errorf("the focused button does not read without colour:\n%q", view)
	}
	sizedModel(m, minimumWidth, minimumHeight)
	view = m.render()
	assertViewFits(t, view, minimumWidth, minimumHeight)
	if strings.Contains(view, "Resize") || strings.Contains(view, "[ Cancel ]") {
		t.Errorf("the smallest terminal shows a modal or a resize hint:\n%s", view)
	}
	sizedModel(m, minimumWidth, minimumHeight-1)
	if !strings.Contains(m.render(), "Resize") {
		t.Error("a terminal below the minimum does not ask for more room")
	}
}
