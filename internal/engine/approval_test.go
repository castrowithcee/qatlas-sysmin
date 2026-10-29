package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const approvedProject = `schema: 1
name: demo-shop
dir: %DIR%
env_file: %DIR%/.env
steps:
  - type: set-env
    assign:
      - "{var}={version}"
  - type: command
    run: verify approved
images:
  - name: app
    ref: registry.example.com/demo/app
    env_var: APP_TAG
    services: [app]
    version_rule:
      mode: auto
`

// TestUnapprovedChangeIsNotRun uses the real approval manager: a project file changed by hand after its
// approval runs as approved, and a project that was never approved does not run.
func TestUnapprovedChangeIsNotRun(t *testing.T) {
	f := newFixture(t)
	vault.LowerWorkFactorForTests()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	project := func(run string) string {
		s := approvedProject
		s = strings.ReplaceAll(s, "%DIR%", f.projDir)
		return strings.ReplaceAll(s, "verify approved", run)
	}
	write("sysmin.yaml", "schema: 1\n")
	write("projects/demo-shop.yaml", project("verify approved"))
	v := vault.New(filepath.Join(t.TempDir(), "vault"))
	if err := v.Create("demo-passphrase-1"); err != nil {
		t.Fatal(err)
	}
	mgr := approval.New(config.NewStore(root), v, f.stateDir)
	if err := mgr.Approve(demoProject, ""); err != nil {
		t.Fatal(err)
	}
	write("projects/demo-shop.yaml", project("verify changed by hand"))
	write("projects/demo-new.yaml", strings.ReplaceAll(project("verify new"), "name: demo-shop", "name: demo-new"))

	f.eng.cfg.Approvals, f.eng.cfg.Vault = mgr, v
	f.sources.setTags("app", "1.0.0", "1.1.0")

	out := f.runNow()
	if out.Run == nil || out.Run.Result != ResultSucceeded {
		t.Fatalf("outcome = %+v", out.Run)
	}
	if calls := f.runner.calls(); !slices.Equal(calls, []string{"verify approved"}) {
		t.Errorf("calls = %v", calls)
	}
	if _, err := f.eng.RunNow(context.Background(), "demo-new"); !errors.Is(err, approval.ErrNotApproved) {
		t.Errorf("unapproved project: %v", err)
	}
	if st, err := f.eng.Status("demo-new"); err != nil || st.CheckError == "" {
		t.Errorf("status = %+v %v", st, err)
	}
	if calls := f.runner.calls(); len(calls) != 1 {
		t.Errorf("unapproved project ran: %v", calls)
	}
}
