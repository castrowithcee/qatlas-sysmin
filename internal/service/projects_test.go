package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

func newWeb() control.ProjectParams {
	return control.ProjectParams{
		Name: "demo-web", Dir: "/srv/demo/web", EnvFile: "/srv/demo/web/.env",
		Images: []control.ProjectImage{{Name: "web", Ref: "registry.example.com/demo/web", EnvVar: "DEMO_WEB_TAG", Services: []string{"web"}}},
	}
}

func problemsOfErr(t *testing.T, err error) []control.Problem {
	t.Helper()
	var ce *control.Error
	if !errors.As(err, &ce) || ce.Code != control.CodeInvalid {
		t.Fatalf("err = %v, want code invalid", err)
	}
	return ce.Problems
}

func (e *env) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(e.cfg.ConfigDir, filepath.FromSlash(rel)))
	return err == nil
}

func TestProjectChangeWithoutSessionStaysPending(t *testing.T) {
	e := approvalEnv(t)
	c := e.dial(t)
	if _, err := e.admin(t).ApproveAll(); err != nil {
		t.Fatal(err)
	}

	res, err := c.AddProject(newWeb())
	if err != nil || res.State != control.StatePending || res.Revision == "" {
		t.Fatalf("add = %+v, %v", res, err)
	}
	if got := openOf(t, c, "demo-web"); got.Kind != "new" {
		t.Fatalf("summary = %+v", got)
	}
	if !strings.Contains(e.read(t, "projects/demo-web.yaml"), "template: image-tag") {
		t.Fatal("the default template is missing")
	}

	// An edit of an approved project is pending as well; the approved state stays.
	res, err = c.EditProject(control.ProjectParams{Name: "demo-app", Dir: "/srv/demo/moved"})
	if err != nil || res.State != control.StatePending {
		t.Fatalf("edit = %+v, %v", res, err)
	}
	if got := openOf(t, c, "demo-app"); got.Kind != "changed" {
		t.Fatalf("summary = %+v", got)
	}
	old, err := e.svc.approvals.Approved("demo-app")
	if err != nil || old.Project.Dir != "/srv/demo/app" {
		t.Fatalf("approved = %+v, %v", old.Project.Dir, err)
	}

	// An edit that changes nothing leaves the project approved.
	two := newWeb()
	two.Name = "demo-two"
	if res, err := c.AddProject(two); err != nil || res.State != control.StatePending {
		t.Fatalf("add = %+v, %v", res, err)
	}
	if res, err := c.RemoveProject("demo-two", ""); err != nil || res.State != control.StateApproved || e.exists("projects/demo-two.yaml") {
		t.Fatalf("remove of a never approved project = %+v, %v", res, err)
	}
	if _, err := e.admin(t).ApproveAll(); err != nil {
		t.Fatal(err)
	}
	res, err = c.EditProject(control.ProjectParams{Name: "demo-app", Dir: "/srv/demo/moved"})
	if err != nil || res.State != control.StateApproved {
		t.Fatalf("no-op edit = %+v, %v", res, err)
	}

	res, err = c.RemoveProject("demo-web", "")
	if err != nil || res.State != control.StatePending || e.exists("projects/demo-web.yaml") {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	res, err = c.RemoveProject("demo-app", "")
	if err != nil || res.State != control.StatePending || e.exists("projects/demo-app.yaml") {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	if got := openOf(t, c, "demo-app"); got.Kind != "deleted" {
		t.Fatalf("summary = %+v", got)
	}
}

func TestProjectChangeInAdminSessionIsApproved(t *testing.T) {
	e := approvalEnv(t)
	adm := e.admin(t)
	res, err := adm.AddProject(newWeb())
	if err != nil || res.State != control.StateApproved {
		t.Fatalf("add = %+v, %v", res, err)
	}
	if _, err := e.svc.approvals.Approved("demo-web"); err != nil {
		t.Fatalf("not approved: %v", err)
	}
	res, err = adm.EditProject(control.ProjectParams{Name: "demo-web", Values: map[string]string{"domain": "web.example.com"}})
	if err != nil || res.State != control.StateApproved {
		t.Fatalf("edit = %+v, %v", res, err)
	}
	res, err = adm.RemoveProject("demo-web", "")
	if err != nil || res.State != control.StateApproved {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	for _, it := range mustItems(t, adm) {
		if it.Project == "demo-web" {
			t.Fatalf("demo-web is open: %+v", it)
		}
	}
	// Another connection has no session and so does not inherit it.
	res, err = e.dial(t).AddProject(newWeb())
	if err != nil || res.State != control.StatePending {
		t.Fatalf("add from another connection = %+v, %v", res, err)
	}
}

func mustItems(t *testing.T, c interface {
	Approvals() ([]control.ApprovalSummary, error)
}) []control.ApprovalSummary {
	t.Helper()
	items, err := c.Approvals()
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestProjectChangeUsesUpTheSingleChangeOfTimeoutZero(t *testing.T) {
	e := newEnv(t, true, true)
	e.write(t, "sysmin.yaml", "schema: 1\nvault:\n  admin_timeout: 0\n")
	e.start(t)
	adm := e.admin(t)
	first, err := adm.AddProject(newWeb())
	if err != nil || first.State != control.StateApproved {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second := newWeb()
	second.Name = "demo-two"
	res, err := adm.AddProject(second)
	if err != nil || res.State != control.StatePending {
		t.Fatalf("second = %+v, %v", res, err)
	}
}

func TestProjectInvalidInputWritesNothing(t *testing.T) {
	e := approvalEnv(t)
	c := e.dial(t)
	bad := newWeb()
	bad.Dir = "relative/dir"
	bad.Images[0].Credential = "demo-missing"
	probs := problemsOfErr(t, mustFail(c.AddProject(bad)))
	codes := map[string]string{}
	for _, p := range probs {
		codes[p.Field] = p.Code
		if p.Message == "" || p.Hint == "" || strings.Contains(p.Message+p.Hint, e.cfg.ConfigDir) {
			t.Errorf("problem = %+v", p)
		}
	}
	if codes["dir"] != "relative-path" || codes["images[0].credential"] != "unknown-credential" {
		t.Fatalf("problems = %+v", probs)
	}
	if e.exists("projects/demo-web.yaml") {
		t.Fatal("an invalid project was written")
	}

	// A problem in another project does not block this change.
	e.write(t, "projects/demo-bad.yaml", strings.Replace(strings.ReplaceAll(demoProject, "demo-app", "demo-bad"), "dir: /srv/demo/app\n", "dir: bad\n", 1))

	probs = problemsOfErr(t, mustFail(c.EditProject(control.ProjectParams{Name: "demo-app", PollInterval: "soon"})))
	if len(probs) != 1 || probs[0].Code != "invalid-duration" {
		t.Fatalf("problems = %+v", probs)
	}
	probs = problemsOfErr(t, mustFail(c.EditProject(control.ProjectParams{Name: "demo-app", RemoveImages: []string{"nope"}})))
	if len(probs) != 1 || probs[0].Field != "images" {
		t.Fatalf("problems = %+v", probs)
	}
	probs = problemsOfErr(t, mustFail(c.AddProject(control.ProjectParams{Name: "demo-file", File: "name: demo-file\n"})))
	if len(probs) != 1 || probs[0].Code != codeInvalidFile {
		t.Fatalf("problems = %+v", probs)
	}
	probs = problemsOfErr(t, mustFail(c.AddProject(control.ProjectParams{Name: "demo-file", File: strings.ReplaceAll(demoProject, "demo-app", "other")})))
	if len(probs) != 1 || probs[0].Field != "name" {
		t.Fatalf("problems = %+v", probs)
	}
	wantCode(t, mustFail(c.AddProject(control.ProjectParams{Name: "demo-file", File: demoProject, Dir: "/x"})), control.CodeBadRequest)
	wantCode(t, mustFail(c.AddProject(control.ProjectParams{Name: "../x"})), control.CodeBadRequest)
}

func TestProjectChangeConflictsAndMissing(t *testing.T) {
	e := approvalEnv(t)
	c := e.dial(t)
	d, err := c.Project("demo-app")
	if err != nil || d.Revision == "" || d.File != demoProject {
		t.Fatalf("show = %+v, %v", d, err)
	}
	e.write(t, "projects/demo-app.yaml", demoProject+"poll_interval: 2h\n")
	wantCode(t, mustFail(c.EditProject(control.ProjectParams{Name: "demo-app", Revision: d.Revision, Dir: "/srv/x"})), control.CodeChanged)
	wantCode(t, mustFail(c.RemoveProject("demo-app", d.Revision)), control.CodeChanged)
	if !e.exists("projects/demo-app.yaml") || strings.Contains(e.read(t, "projects/demo-app.yaml"), "/srv/x") {
		t.Fatal("a stale change was written")
	}
	wantCode(t, mustFail(c.AddProject(control.ProjectParams{Name: "demo-app", File: demoProject})), control.CodeExists)
	wantCode(t, mustFail(c.EditProject(control.ProjectParams{Name: "demo-none", Dir: "/srv/x"})), control.CodeNotFound)
	wantCode(t, mustFail(c.RemoveProject("demo-none", "")), control.CodeNotFound)
	wantCode(t, mustFail(c.Project("demo-none")), control.CodeNotFound)
	wantCode(t, mustFail(c.CheckProject("demo-none", "")), control.CodeNotFound)
}

func TestProjectChangeNeedsUnlockedVault(t *testing.T) {
	e := newEnv(t, true, false)
	e.write(t, "sysmin.yaml", "schema: 1\n")
	e.write(t, "projects/demo-app.yaml", demoProject)
	e.start(t)
	c := e.dial(t)
	wantCode(t, mustFail(c.AddProject(newWeb())), control.CodeLocked)
	wantCode(t, mustFail(c.EditProject(control.ProjectParams{Name: "demo-app", Dir: "/srv/x"})), control.CodeLocked)
	wantCode(t, mustFail(c.RemoveProject("demo-app", "")), control.CodeLocked)
	wantCode(t, mustFail(c.CheckProject("demo-app", "")), control.CodeLocked)
	if e.exists("projects/demo-web.yaml") || e.read(t, "projects/demo-app.yaml") != demoProject {
		t.Fatal("something was written while the vault was locked")
	}
	// Reading the files needs no vault; the approval state is unknown then.
	list, err := c.Projects()
	if err != nil || len(list) != 1 || list[0].State != control.StateUnknown {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestProjectListShowCheck(t *testing.T) {
	e := approvalEnv(t)
	c := e.dial(t)
	if _, err := e.admin(t).ApproveAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddProject(newWeb()); err != nil {
		t.Fatal(err)
	}
	list, err := c.Projects()
	if err != nil || len(list) != 2 || list[0].Name != "demo-app" || list[0].State != control.StateApproved ||
		list[1].Name != "demo-web" || list[1].State != control.StatePending || list[1].Source != "images" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	res, err := c.CheckProject("demo-app", "2.3.4")
	if err != nil || res.Version != "2.3.4" || len(res.Steps) == 0 {
		t.Fatalf("check = %+v, %v", res, err)
	}
	var sawSet, sawPull bool
	for _, st := range res.Steps {
		switch st.Type {
		case "set-env":
			sawSet = strings.Contains(st.Detail, "assign=DEMO_APP_TAG=2.3.4")
		case "compose-pull":
			sawPull = strings.Contains(st.Detail, "services=app")
		}
	}
	if !sawSet || !sawPull {
		t.Fatalf("steps = %+v", res.Steps)
	}
	if def, err := c.CheckProject("demo-app", ""); err != nil || def.Version != "1.0.0" {
		t.Fatalf("default check = %+v, %v", def, err)
	}
	probs := problemsOfErr(t, mustFail(c.CheckProject("demo-app", "bad version")))
	if len(probs) == 0 {
		t.Fatal("no problems for a bad version")
	}
	// A stored project that is invalid is reported by check.
	e.write(t, "projects/demo-app.yaml", strings.Replace(demoProject, "/srv/demo/app\n", "srv\n", 1))
	probs = problemsOfErr(t, mustFail(c.CheckProject("demo-app", "")))
	if len(probs) == 0 || probs[0].Field != "dir" {
		t.Fatalf("problems = %+v", probs)
	}
}

func TestProjectChangesAreLogged(t *testing.T) {
	e := approvalEnv(t)
	c := e.dial(t)
	if _, err := c.AddProject(newWeb()); err != nil {
		t.Fatal(err)
	}
	bad := newWeb()
	bad.Name = "demo-bad"
	bad.Dir = "x"
	_, _ = c.AddProject(bad)
	_, _ = c.Projects()
	got := map[string]bool{}
	for _, en := range readEntries(t, filepath.Join(e.cfg.StateDir, "logs")) {
		got[en.Action+"/"+en.Project+"/"+en.Result+"/"+en.ErrorCode] = true
	}
	for _, key := range []string{
		"config.apply/demo-web/success/", "config.apply/demo-bad/failure/invalid", "config.read//success/",
	} {
		if !got[key] {
			t.Errorf("no log entry %q in %v", key, got)
		}
	}
}
