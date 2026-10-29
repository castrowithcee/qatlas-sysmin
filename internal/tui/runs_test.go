package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

const (
	runOK     = "20261028T110000.000000000Z"
	runBack   = "20261028T120000.000000000Z"
	runOther  = "20261027T090000.000000000Z"
	runLogOne = "2026-10-28T12:00:00Z step 1/2 set-env result=ok\n2026-10-28T12:00:01Z step 2/2 command output:\n  | login failed with [redacted]\n"
)

// runsFake serves two projects; demo-app has a successful and a rolled-back run, demo-two one run.
func runsFake() *fakeBackend {
	fb := libFake()
	fb.projects = []control.ProjectSummary{{Name: "demo-two", State: control.StateApproved}, {Name: "demo-app", State: control.StateApproved}}
	at := func(h, m int) time.Time { return time.Date(2026, 10, 28, h, m, 0, 0, time.UTC) }
	fb.lib.history = map[string][]control.DeployRun{
		"demo-app": {
			{ID: runOK, Action: "manual", Started: at(11, 0), DurationMS: 4200, Result: "succeeded",
				Changes: []control.DeployChange{{Kind: "image", Name: "app", From: "1.0.0", To: "1.1.0"}}, Backup: "20261028T105959Z"},
			{ID: runBack, Action: "auto", Started: at(12, 0), DurationMS: 9100, Result: "rolled-back", ErrorCode: "step-failed",
				FailedStep: "health-check", Error: "health check failed",
				Changes: []control.DeployChange{{Kind: "image", Name: "app", From: "1.1.0", To: "1.2.0"}}},
		},
		"demo-two": {{ID: runOther, Action: "restore", Started: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC), DurationMS: 800, Result: "succeeded"}},
	}
	fb.lib.status = map[string]control.DeployStatusResult{
		"demo-app": {Project: "demo-app", Paused: true, Sources: []control.DeploySource{}},
	}
	fb.lib.logs = map[string]string{"demo-app/" + runBack: runLogOne, "demo-two/" + runOther: "restore done\n"}
	return fb
}

func TestRunsLoadsStatusAndHistoryNewestFirst(t *testing.T) {
	utcClock(t)
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		fb := runsFake()
		m := appModel(t, fb, size[0], size[1])
		send(m, "3", "right")
		view := plain(m)
		for _, want := range []string{"Log", "Runs", "Runs · demo-app (2)", "‖ paused", "↩ rolled-back", "✓ succeeded", "app 1.1.0→1.2.0"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: lacks %q:\n%s", size[0], size[1], want, view)
			}
		}
		if strings.Index(view, "rolled-back") > strings.Index(view, "succeeded") {
			t.Errorf("%dx%d: the oldest run is first:\n%s", size[0], size[1], view)
		}
		if fb.count("deploy-status demo-app") != 1 || fb.count("deploy-history demo-app") != 1 {
			t.Errorf("calls = %v", fb.log())
		}
		assertViewFits(t, view, size[0], size[1])
	}
}

func TestRunsProjectChangeLoadsAgain(t *testing.T) {
	utcClock(t)
	fb := runsFake()
	m := appModel(t, fb, 80, 24)
	send(m, "3", "right", "f")
	if m.form == nil {
		t.Fatal("no project form")
	}
	send(m, "left")
	f2(m)
	if fb.count("deploy-history demo-two") != 1 || m.act.runs.project != "demo-two" {
		t.Errorf("project %q, calls = %v", m.act.runs.project, fb.log())
	}
	if view := plain(m); !strings.Contains(view, "Runs · demo-two (1)") || !strings.Contains(view, "● idle") {
		t.Errorf("view:\n%s", view)
	}
	send(m, "ctrl+r")
	if fb.count("deploy-history demo-two") != 2 {
		t.Errorf("reload did not read again: %v", fb.log())
	}
}

func TestRunLogOpensScrollsAndClosesAndNothingPolls(t *testing.T) {
	utcClock(t)
	fb := runsFake()
	m := appModel(t, fb, 80, 24)
	send(m, "3", "right", "o")
	if fb.count("deploy-log demo-app "+runBack) != 1 {
		t.Fatalf("calls = %v", fb.log())
	}
	view := plain(m)
	for _, want := range []string{"Run log › demo-app " + runBack, "login failed with [redacted]"} {
		if !strings.Contains(view, want) {
			t.Errorf("lacks %q:\n%s", want, view)
		}
	}
	assertViewFits(t, view, 80, 24)
	send(m, "down", "pgdown", "home", "end")
	send(m, "esc")
	if m.act.runs.log.open || !strings.Contains(plain(m), "Runs · demo-app (2)") {
		t.Errorf("the log did not close:\n%s", plain(m))
	}
	calls := len(fb.log())
	clock := clockModel(m, time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC))
	for range 5 {
		*clock = clock.Add(2 * pollEvery)
		_, cmd := m.Update(tickMsg{})
		drain(m, cmd)
	}
	for _, c := range fb.log()[calls:] {
		if strings.HasPrefix(c, "deploy-") || c == "projects" {
			t.Errorf("read on a timer: %v", fb.log()[calls:])
		}
	}
	send(m, "o", "q")
	if !m.quitting {
		t.Error("q does not quit in the log")
	}
}

func TestRunLogErrorsAreShownWithoutPath(t *testing.T) {
	fb := runsFake()
	m := appModel(t, fb, 80, 24)
	send(m, "3", "right", "down", "o")
	if view := plain(m); !strings.Contains(view, "Not found.") {
		t.Errorf("no error shown:\n%s", view)
	}
	send(m, "esc")
	if m.act.runs.log.open {
		t.Error("esc does not close")
	}
}

func TestRunsDrawNoSecretNoControlSequence(t *testing.T) {
	fb := runsFake()
	fb.lib.logs["demo-app/"+runBack] = "start\x1b]52;c;ZXZpbA==\x07 \x1b[31mred\x1b[0m\ntoken " + secretValue + " leaked\nbell\x07 end\n"
	h := fb.lib.history["demo-app"]
	h[1].Error = "failed with " + secretValue + "\x1b[2J"
	h[1].FailedStep = "step\x1b[31m"
	m := appModel(t, fb, 120, 40)
	m.redactor.Add(secretValue)
	send(m, "3", "right")
	check := func() {
		t.Helper()
		raw := m.render()
		for _, bad := range []string{"\x1b]52", "\x1b[2J", "\x1b[31m", "\x07", secretValue} {
			if strings.Contains(raw, bad) {
				t.Fatalf("%q is drawn:\n%q", bad, raw)
			}
		}
	}
	check()
	send(m, "enter", "right", "right")
	check()
	send(m, "esc", "o")
	check()
	if !strings.Contains(plain(m), "red") {
		t.Errorf("the log text is gone:\n%s", plain(m))
	}
}

func TestRunsCopyHandsOverOnlyTheRunID(t *testing.T) {
	fb := runsFake()
	m := appModel(t, fb, 120, 40)
	send(m, "3", "right")
	if src := m.copySource(); src == nil || src() != runBack {
		t.Error("copy does not give the ID of the marked run")
	}
	send(m, "o")
	if src := m.copySource(); src == nil || strings.Contains(src(), "login") {
		t.Error("copy in the log hands over log text")
	}
}

func TestRunsPageIsReachedByCommandAndHasTexts(t *testing.T) {
	utcClock(t)
	eachLanguage(t, func(t *testing.T) {
		fb := runsFake()
		m := appModel(t, fb, 120, 40)
		send(m, ":")
		typed(m, "activity runs")
		send(m, "enter")
		if m.area != areaActivity || m.act.sub != actRuns {
			t.Fatalf("area %v sub %d", m.area, m.act.sub)
		}
		for _, keys := range [][]string{{"i"}, {"?"}, {"f"}, {"enter"}, {"o"}} {
			m2 := appModel(t, fb, 120, 40)
			send(m2, "3", "right")
			send(m2, keys...)
			assertViewFits(t, plain(m2), 120, 40)
		}
		for _, group := range strings.Split(m.areaKeys(), " · ") {
			if strings.HasPrefix(group, "{") || group == "1-6 area" {
				continue
			}
			if _, ok := textDE[footerID(group)]; !ok {
				t.Errorf("footer group %q has no text", group)
			}
		}
	})
}
