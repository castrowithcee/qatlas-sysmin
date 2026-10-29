package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// updates are the calls of fb that started an update.
func updates(fb *fakeBackend) []string {
	var out []string
	for _, c := range fb.log() {
		if strings.HasPrefix(c, "update ") {
			out = append(out, c)
		}
	}
	return out
}

func TestProjectsUpdateAsksBeforeItStartsAnything(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := projectsModel(t, fb, 120, 30)
	drain(m, m.statusCmd())
	send(m, "u")
	view := plain(m)
	if m.prj.upd == nil || !strings.Contains(view, "Update 1 project(s)") || !strings.Contains(view, "demo-app · app") ||
		!strings.Contains(view, "1.0.0 → 2.0.0") || !strings.Contains(view, "y update") {
		t.Fatalf("no update panel:\n%s", view)
	}
	if got := updates(fb); len(got) != 0 {
		t.Fatalf("an update started without the answer: %v", got)
	}
	send(m, "n")
	if m.prj.upd != nil || len(updates(fb)) != 0 {
		t.Fatalf("n did not cancel: %v", fb.log())
	}
	send(m, "u", "esc")
	if m.prj.upd != nil || len(updates(fb)) != 0 {
		t.Fatalf("esc did not cancel: %v", fb.log())
	}
	send(m, "u", "y")
	if got := updates(fb); len(got) != 1 || got[0] != "update demo-app=app@2.0.0" {
		t.Fatalf("updates = %v", got)
	}
}

func TestProjectsUpdateOfTwoMarkedProjectsIsOneCall(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	fb.proj.updateRuns = map[string]control.ProjectUpdateResult{
		"demo-db": {Project: "demo-db", Pending: true},
	}
	m := projectsModel(t, fb, 120, 30)
	drain(m, m.statusCmd())
	send(m, "space", "space", "u")
	view := plain(m)
	if !strings.Contains(view, "Update 2 project(s)") || !strings.Contains(view, "demo-app · app") || !strings.Contains(view, "demo-db · app") {
		t.Fatalf("both projects are not listed:\n%s", view)
	}
	send(m, "y")
	got := updates(fb)
	if len(got) != 1 || got[0] != "update demo-app=app@2.0.0 demo-db=app@2.0.0" {
		t.Fatalf("updates = %v", got)
	}
	view = plain(m)
	for _, want := range []string{"Update result", "demo-app: run succeeded", "app 1.0.0 → 2.0.0", "demo-db: still running"} {
		if !strings.Contains(view, want) {
			t.Errorf("result lacks %q:\n%s", want, view)
		}
	}
	if fb.count("session") != 0 {
		t.Error("the update asked for a new session")
	}
	if n := len(m.prj.list.all); n != 3 || m.prj.gen < 2 {
		t.Errorf("the list was not reloaded (gen %d)", m.prj.gen)
	}
	send(m, "esc")
	if m.prj.upd != nil || !strings.Contains(plain(m), "Projects (3)") {
		t.Error("esc does not leave the result")
	}
}

func TestProjectsUpdateShowsReleaseNotesOfTheTargetAndPicksAnother(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := projectsModel(t, fb, 120, 30)
	drain(m, m.statusCmd())
	send(m, "u")
	if fb.count("changelog demo-app app 2.0.0") != 1 {
		t.Fatalf("calls = %v", fb.log())
	}
	view := plain(m)
	for _, want := range []string{"Release notes demo-app · app · 1.0.0 → 2.0.0", "Release 2.0.0", "Notes for 2.0.0", "Second line"} {
		if !strings.Contains(view, want) {
			t.Errorf("notes lack %q:\n%s", want, view)
		}
	}
	send(m, "enter")
	if m.screen != screenConfirm || m.prj.dialog == nil || m.prj.dialog.picker == nil {
		t.Fatalf("no picker:\n%s", plain(m))
	}
	view = plain(m)
	if !strings.Contains(view, "Version of demo-app · app") || strings.Index(view, "2.0.0") > strings.Index(view, "1.1.0") {
		t.Errorf("the picker lacks the versions or the newest is not first:\n%s", view)
	}
	send(m, "down", "enter")
	if m.prj.upd.items[0].target != "1.1.0" || fb.count("changelog demo-app app 1.1.0") != 1 {
		t.Fatalf("target = %q, calls = %v", m.prj.upd.items[0].target, fb.log())
	}
	if view := plain(m); !strings.Contains(view, "Notes for 1.1.0") || !strings.Contains(view, "1.0.0 → 1.1.0") {
		t.Errorf("the notes do not follow the target:\n%s", view)
	}
	send(m, "y")
	if got := updates(fb); len(got) != 1 || got[0] != "update demo-app=app@1.1.0" {
		t.Fatalf("updates = %v", got)
	}
}

func TestProjectsUpdatePickerFiltersAndEscKeepsTheTarget(t *testing.T) {
	fb := projectsFake()
	st := fb.proj.status["demo-app"]
	st.Sources[0].Available = []string{"1.1.0", "1.2.0", "2.0.0"}
	fb.proj.status["demo-app"] = st
	m := projectsModel(t, fb, 120, 30)
	send(m, "u", "enter", "/")
	if !m.typing() {
		t.Fatal("the filter is not typed into")
	}
	typed(m, "1.2")
	if got := len(m.prj.dialog.picker.list.matches); got != 1 {
		t.Fatalf("matches = %d", got)
	}
	send(m, "enter", "enter")
	if m.prj.upd.items[0].target != "1.2.0" {
		t.Errorf("target = %q", m.prj.upd.items[0].target)
	}
	send(m, "enter", "esc")
	if m.prj.dialog != nil || m.prj.upd.items[0].target != "1.2.0" {
		t.Error("esc changed the target")
	}
}

func TestProjectsUpdateWithoutASessionAsksOnceAndStartsAtOnce(t *testing.T) {
	fb := projectsFake()
	m := projectsModel(t, fb, 120, 30)
	send(m, "u", "y")
	if m.ov == nil || m.screen != screenPrompt {
		t.Fatalf("no passphrase asked (screen %v)", m.screen)
	}
	if len(updates(fb)) != 0 {
		t.Fatal("the update ran before the passphrase")
	}
	send(m, "esc")
	if len(updates(fb)) != 0 || m.prj.upd == nil {
		t.Fatalf("esc did not cancel the entry only: %v", fb.log())
	}
	send(m, "y")
	typed(m, testPass)
	send(m, "enter")
	if got := updates(fb); len(got) != 1 {
		t.Fatalf("updates = %v", got)
	}
	if fb.count("session") != 1 || m.ov != nil || m.prj.upd == nil || m.prj.upd.results == nil {
		t.Errorf("calls = %v\n%s", fb.log(), plain(m))
	}
}

func TestProjectsUpdateWithNothingNewerStartsNothing(t *testing.T) {
	fb := projectsFake()
	st := fb.proj.status["demo-app"]
	st.Sources[0].Available = nil
	fb.proj.status["demo-app"] = st
	m := projectsModel(t, fb, 120, 30)
	send(m, "u")
	if m.prj.upd != nil || !strings.Contains(plain(m), "Nothing newer to update.") || len(fb.log()) > 0 && strings.Contains(strings.Join(fb.log(), "|"), "update ") {
		t.Errorf("view:\n%s", plain(m))
	}
}

func TestProjectsUpdateSkipsProjectsWithNothingNewer(t *testing.T) {
	fb := projectsFake()
	st := fb.proj.status["demo-db"]
	st.Sources[0].Available = nil
	fb.proj.status["demo-db"] = st
	fb.session = true
	m := projectsModel(t, fb, 120, 30)
	drain(m, m.statusCmd())
	send(m, "space", "space", "u")
	if view := plain(m); !strings.Contains(view, "Nothing newer: demo-db") || !strings.Contains(view, "Update 1 project(s)") {
		t.Errorf("view:\n%s", view)
	}
	send(m, "y")
	if got := updates(fb); len(got) != 1 || got[0] != "update demo-app=app@2.0.0" {
		t.Errorf("updates = %v", got)
	}
}

func TestProjectsUpdateReleaseNotesAreUntrustedAndFailuresFixed(t *testing.T) {
	fb := projectsFake()
	fb.proj.notes["demo-app"] = control.ChangelogResult{Project: "demo-app", Sources: []control.ChangelogSource{{
		Kind: control.SourceImage, Name: "app", ReleasesTotal: 30, Stale: true,
		Releases: []control.ChangelogRelease{{Version: "2.0.0", Name: "\x1b]0;owned\x07Big \x1b[31mrelease", Body: "ok\x1b[2J line\x00\rend"}}}}}
	m := projectsModel(t, fb, 120, 30)
	send(m, "u")
	view := plain(m)
	if strings.ContainsAny(view, "\x1b\x00\r\a") || !strings.Contains(view, "Big release") || !strings.Contains(view, "ok line") ||
		!strings.Contains(view, "29 older release(s) not shown.") || !strings.Contains(view, "Cached notes") {
		t.Errorf("view:\n%s", view)
	}
	for _, c := range []struct {
		src  control.ChangelogSource
		want string
	}{
		{control.ChangelogSource{Kind: control.SourceImage, Name: "app", Error: control.ChangelogRateLimited, Message: "secret detail"}, "GitHub limits requests"},
		{control.ChangelogSource{Kind: control.SourceImage, Name: "app", Error: control.SourceUnreachable, Message: "secret detail"}, "unreachable"},
	} {
		fb.proj.notes["demo-app"] = control.ChangelogResult{Project: "demo-app", Sources: []control.ChangelogSource{c.src}}
		m = projectsModel(t, fb, 120, 30)
		send(m, "u")
		if view := plain(m); !strings.Contains(view, c.want) || strings.Contains(view, "secret detail") {
			t.Errorf("want %q:\n%s", c.want, view)
		}
	}
	fb.proj.notesErr = &control.Error{Code: control.CodeTimeout}
	m = projectsModel(t, fb, 120, 30)
	send(m, "u")
	if view := plain(m); !strings.Contains(view, "timed out") && !strings.Contains(view, "too long") && !strings.Contains(view, "Timed out") {
		t.Errorf("view:\n%s", view)
	}
}

func TestProjectsUpdateNotesScroll(t *testing.T) {
	fb := projectsFake()
	var b strings.Builder
	for i := range 80 {
		fmt.Fprintf(&b, "change %02d\n", i)
	}
	fb.proj.notes["demo-app"] = control.ChangelogResult{Project: "demo-app", Sources: []control.ChangelogSource{{
		Kind: control.SourceImage, Name: "app", ReleasesTotal: 1,
		Releases: []control.ChangelogRelease{{Version: "2.0.0", Body: b.String()}}}}}
	m := projectsModel(t, fb, 80, 24)
	send(m, "u")
	view := plain(m)
	assertViewFits(t, view, 80, 24)
	if !strings.Contains(view, "change 00") || !strings.Contains(view, "lines 1-") {
		t.Fatalf("no first page:\n%s", view)
	}
	send(m, "end")
	if view := plain(m); !strings.Contains(view, "change 79") || strings.Contains(view, "change 00") {
		t.Errorf("end does not show the last notes:\n%s", view)
	}
}

func TestProjectsUpdateErrorKeepsThePanel(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	fb.proj.updateRuns = map[string]control.ProjectUpdateResult{"demo-app": {Project: "demo-app", Error: control.CodeVersionRefused}}
	m := projectsModel(t, fb, 120, 30)
	drain(m, m.statusCmd())
	send(m, "u", "y")
	if view := plain(m); !strings.Contains(view, "demo-app: That version is no available update any more.") {
		t.Errorf("view:\n%s", view)
	}
}

func TestProjectsRestoreAsksThenRestores(t *testing.T) {
	fb := projectsFake()
	fb.session = true
	m := projectsModel(t, fb, 120, 30)
	drain(m, m.statusCmd())
	command(m, "restore")
	if fb.count("backups demo-app") != 1 || m.prj.dialog == nil || m.prj.dialog.picker == nil {
		t.Fatalf("no picker: %v\n%s", fb.log(), plain(m))
	}
	view := plain(m)
	if !strings.Contains(view, "Restore demo-app from") || strings.Index(view, "backup-20261029") > strings.Index(view, "backup-20261028") {
		t.Errorf("the backups are not listed newest first:\n%s", view)
	}
	send(m, "down", "enter")
	view = plain(m)
	if m.prj.dialog == nil || m.prj.dialog.kind != projDlgRestore || !strings.Contains(view, "Restore demo-app?") || !strings.Contains(view, "backup-20261028") {
		t.Fatalf("no question:\n%s", view)
	}
	if fb.count("restore demo-app backup-20261028") != 0 {
		t.Fatal("the restore ran before the answer")
	}
	send(m, "n")
	if fb.count("restore demo-app backup-20261028") != 0 || m.prj.dialog != nil {
		t.Fatal("n did not cancel")
	}
	command(m, "restore")
	send(m, "esc")
	if m.prj.dialog != nil || fb.count("restore demo-app backup-20261029") != 0 {
		t.Fatal("esc did not leave the picker")
	}
	command(m, "restore")
	send(m, "enter", "esc")
	if fb.count("restore demo-app backup-20261029") != 0 {
		t.Fatal("esc did not cancel the question")
	}
	command(m, "restore")
	send(m, "enter", "y")
	if fb.count("restore demo-app backup-20261029") != 1 || !strings.Contains(plain(m), "demo-app: run succeeded.") {
		t.Errorf("calls = %v\n%s", fb.log(), plain(m))
	}
}

func TestProjectsRestoreNeedsTheAdminSession(t *testing.T) {
	fb := projectsFake()
	m := projectsModel(t, fb, 120, 30)
	command(m, "restore")
	send(m, "enter", "y")
	if m.ov == nil || fb.count("restore demo-app backup-20261029") != 0 {
		t.Fatalf("no passphrase asked: %v", fb.log())
	}
	typed(m, testPass)
	send(m, "enter")
	if fb.count("restore demo-app backup-20261029") != 1 {
		t.Errorf("calls = %v", fb.log())
	}
}

func TestProjectsRestoreWithoutBackups(t *testing.T) {
	fb := projectsFake()
	fb.proj.backups["demo-app"] = nil
	m := projectsModel(t, fb, 120, 30)
	command(m, "restore")
	if m.prj.dialog != nil || !strings.Contains(plain(m), "demo-app has no backups.") {
		t.Errorf("view:\n%s", plain(m))
	}
}

func TestProjectsRestoreDangerButtonOnly(t *testing.T) {
	fb := projectsFake()
	m := projectsModel(t, fb, 120, 40)
	command(m, "restore")
	send(m, "enter")
	q := m.ask
	if q == nil || !q.danger || !q.warn || !strings.Contains(q.keys, "y restore") {
		t.Fatalf("question = %+v", q)
	}
}

func TestProjectsUpdateGerman(t *testing.T) {
	fb := projectsFake()
	m := projectsModel(t, fb, 120, 30)
	useLanguage(t, "de")
	send(m, "u")
	view := plain(m)
	for _, want := range []string{"1 Projekt(e) aktualisieren", "Release Notes demo-app", "y aktualisieren", "Version"} {
		if !strings.Contains(view, want) {
			t.Errorf("German view lacks %q:\n%s", want, view)
		}
	}
}

func TestGoldenProjectsUpdate(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			fb := projectsFake()
			zone := projectZone
			projectZone = time.UTC
			defer func() { projectZone = zone }()
			m := goldenApp(t, fb, size[0], size[1])
			send(m, "2", "space", "space", "u")
			got := m.render()
			assertViewFits(t, plain(m), size[0], size[1])
			assertGolden(t, fmt.Sprintf("projects_update_%dx%d", size[0], size[1]), got)
		})
	}
}
