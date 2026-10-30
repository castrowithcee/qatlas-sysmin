package service

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// realCheckEnv is a service that asks the kernel about the paths of a project.
func realCheckEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, true, true, func(c *Config) { c.WriteCheck = nil })
	if _, err := e.svc.vault.Add("demo-registry", vault.KindRegistry, map[string]string{
		vault.FieldUser: "demo-user", vault.FieldToken: demoSecret}); err != nil {
		t.Fatal(err)
	}
	e.write(t, "sysmin.yaml", "schema: 1\n")
	e.start(t)
	return e
}

func lockedProject(t *testing.T) (params control.ProjectParams, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root can write to any directory")
	}
	dir = t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	p := newWeb()
	p.Dir, p.EnvFile = dir, filepath.Join(dir, ".env")
	return p, dir
}

func wantPaths(t *testing.T, err error, want ...string) {
	t.Helper()
	var ce *control.Error
	if !errors.As(err, &ce) || ce.Code != control.CodeNotWritable || !slices.Equal(ce.Paths, want) {
		t.Fatalf("err = %v (%+v), want not-writable %v", err, ce, want)
	}
}

func TestUnwritableProjectIsStoredReportedAndNotApproved(t *testing.T) {
	e := realCheckEnv(t)
	adm := e.admin(t)
	params, dir := lockedProject(t)
	envFile := params.EnvFile

	res, err := adm.AddProject(params)
	if err != nil || !slices.Equal(res.NotWritable, []string{envFile}) || res.Revision == "" {
		t.Fatalf("add = %+v, %v", res, err)
	}
	// Even an admin session leaves the project open.
	if res.State != control.StatePending {
		t.Fatalf("state = %q, want pending", res.State)
	}

	fp := openOf(t, adm, "demo-web").Fingerprint
	wantPaths(t, adm.Approve("demo-web", fp), envFile)
	all, err := adm.ApproveAll()
	if err != nil || len(all.Done) != 0 || len(all.Failed) != 1 ||
		all.Failed[0].Code != control.CodeNotWritable || !slices.Equal(all.Failed[0].Paths, []string{envFile}) {
		t.Fatalf("approve-all = %+v, %v", all, err)
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := adm.Approve("demo-web", fp); err != nil {
		t.Fatalf("approve after the fix: %v", err)
	}
}

func TestBatchApprovalRefusesOnlyTheUnwritableProject(t *testing.T) {
	e := realCheckEnv(t)
	adm := e.admin(t)
	params, _ := lockedProject(t)
	good := newWeb()
	good.Name, good.Dir = "demo-ok", t.TempDir()
	good.EnvFile = filepath.Join(good.Dir, ".env")
	// Without a session of its own, the change of the connection stays open.
	plain := e.dial(t)
	for _, p := range []control.ProjectParams{params, good} {
		if _, err := plain.AddProject(p); err != nil {
			t.Fatal(err)
		}
	}
	res, err := adm.ApproveAll()
	if err != nil || !slices.Equal(res.Done, []string{"demo-ok"}) || len(res.Failed) != 1 || res.Failed[0].Project != "demo-web" {
		t.Fatalf("approve-all = %+v, %v", res, err)
	}
}

func TestUnwritableGitDirectory(t *testing.T) {
	e := realCheckEnv(t)
	adm := e.admin(t)
	_, dir := lockedProject(t)
	res, err := adm.AddProject(control.ProjectParams{Name: "demo-site", Dir: dir, GitURL: "https://example.invalid/demo/site.git"})
	if err != nil || !slices.Equal(res.NotWritable, []string{dir}) {
		t.Fatalf("add = %+v, %v", res, err)
	}
}
