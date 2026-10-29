package approval

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const testPass = "demo-passphrase-1"

const demoProject = `schema: 1
name: demo-app
dir: /srv/demo/app
template: image-tag
env_file: /srv/demo/app/.env
images:
  - name: app
    ref: registry.example.com/demo/app
    env_var: DEMO_APP_TAG
    services: [app]
    credential: demo-registry
`

type env struct {
	t     *testing.T
	root  string
	store *config.Store
	vault *vault.Vault
	mgr   *Manager
	state string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	vault.LowerWorkFactorForTests()
	e := &env{t: t, root: t.TempDir(), state: filepath.Join(t.TempDir(), "state")}
	e.store = config.NewStore(e.root)
	e.vault = vault.New(filepath.Join(t.TempDir(), "vault"))
	if err := e.vault.Create(testPass); err != nil {
		t.Fatal(err)
	}
	e.credential("demo-registry")
	e.write("sysmin.yaml", "schema: 1\n")
	e.write("projects/demo-app.yaml", demoProject)
	e.mgr = New(e.store, e.vault, e.state)
	return e
}

func (e *env) credential(name string) string {
	e.t.Helper()
	id, err := e.vault.Add(name, vault.KindRegistry, map[string]string{
		vault.FieldUser: "demo-user", vault.FieldToken: "demo-token-value-0001"})
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) write(rel, content string) {
	e.t.Helper()
	path := filepath.Join(e.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) read(rel string) string {
	e.t.Helper()
	data, err := os.ReadFile(filepath.Join(e.root, filepath.FromSlash(rel)))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(data)
}

func (e *env) open() []Item {
	e.t.Helper()
	items, err := e.mgr.Open()
	if err != nil {
		e.t.Fatal(err)
	}
	return items
}

func (e *env) approveDemo() {
	e.t.Helper()
	if err := e.mgr.Approve("demo-app", ""); err != nil {
		e.t.Fatal(err)
	}
}

func hasChange(it Item, path string) (Change, bool) {
	for _, c := range it.Changes {
		if c.Path == path {
			return c, true
		}
	}
	return Change{}, false
}

func TestNewProjectIsOpenUntilApproved(t *testing.T) {
	e := newEnv(t)
	items := e.open()
	if len(items) != 1 || items[0].Kind != KindNew || items[0].Project != "demo-app" || items[0].Fingerprint == "" {
		t.Fatalf("Open() = %+v, want one new demo-app", items)
	}
	if _, err := e.mgr.Approved("demo-app"); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("Approved() = %v, want ErrNotApproved", err)
	}
	e.approveDemo()
	if items := e.open(); len(items) != 0 {
		t.Fatalf("Open() after approval = %+v", items)
	}
	a, err := e.mgr.Approved("demo-app")
	if err != nil || a.Project.Dir != "/srv/demo/app" || len(a.Steps) == 0 {
		t.Fatalf("Approved() = %+v, %v", a, err)
	}
	if a.Credentials["demo-registry"] == "" {
		t.Fatal("approved state does not bind the credential id")
	}
}

func TestHandEditStaysOpenAndApprovedStateStaysOld(t *testing.T) {
	e := newEnv(t)
	e.approveDemo()
	e.write("projects/demo-app.yaml", strings.Replace(demoProject, "/srv/demo/app\ntemplate", "/srv/demo/other\ntemplate", 1))

	items := e.open()
	if len(items) != 1 || items[0].Kind != KindChanged {
		t.Fatalf("Open() = %+v, want one changed", items)
	}
	if c, ok := hasChange(items[0], "project.dir"); !ok || c.Old != "/srv/demo/app" || c.New != "/srv/demo/other" {
		t.Fatalf("changes = %+v", items[0].Changes)
	}
	a, err := e.mgr.Approved("demo-app")
	if err != nil || a.Project.Dir != "/srv/demo/app" {
		t.Fatalf("Approved() = %+v, %v; want the old dir", a, err)
	}
	model, _, err := e.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	am, skipped, err := e.mgr.ApprovedModel(model)
	if err != nil || len(skipped) != 0 || len(am.Projects) != 1 || am.Projects[0].Dir != "/srv/demo/app" {
		t.Fatalf("ApprovedModel() = %+v, %v, %v", am, skipped, err)
	}

	if err := e.mgr.Approve("demo-app", items[0].Fingerprint); err != nil {
		t.Fatal(err)
	}
	if a, _ := e.mgr.Approved("demo-app"); a.Project.Dir != "/srv/demo/other" {
		t.Fatalf("after approval dir = %q", a.Project.Dir)
	}
	if items := e.open(); len(items) != 0 {
		t.Fatalf("Open() = %+v", items)
	}
}

func TestApproveRefusesWhenChangedSinceShown(t *testing.T) {
	e := newEnv(t)
	shown := e.open()[0].Fingerprint
	e.write("projects/demo-app.yaml", demoProject+"values:\n  note: later\n")
	if err := e.mgr.Approve("demo-app", shown); !errors.Is(err, ErrChanged) {
		t.Fatalf("Approve() = %v, want ErrChanged", err)
	}
	if _, err := e.mgr.Approved("demo-app"); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("Approved() = %v after refused approval", err)
	}
}

func TestDiscardRestoresFile(t *testing.T) {
	e := newEnv(t)
	e.approveDemo()
	e.write("projects/demo-app.yaml", strings.Replace(demoProject, "/srv/demo/app\ntemplate", "/srv/demo/other\ntemplate", 1))
	if err := e.mgr.Discard("demo-app"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.read("projects/demo-app.yaml"), "dir: /srv/demo/app\n") {
		t.Fatalf("file not restored:\n%s", e.read("projects/demo-app.yaml"))
	}
	if items := e.open(); len(items) != 0 {
		t.Fatalf("Open() after discard = %+v", items)
	}
}

func TestNewProjectDiscardedAndDeletedProjectRestored(t *testing.T) {
	e := newEnv(t)
	e.approveDemo()
	e.write("projects/fresh-app.yaml", strings.Replace(demoProject, "demo-app", "fresh-app", 1))
	if items := e.open(); len(items) != 1 || items[0].Project != "fresh-app" || items[0].Kind != KindNew {
		t.Fatalf("Open() = %+v", items)
	}
	if err := e.mgr.Discard("fresh-app"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.root, "projects", "fresh-app.yaml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("new project still exists: %v", err)
	}

	if err := os.Remove(filepath.Join(e.root, "projects", "demo-app.yaml")); err != nil {
		t.Fatal(err)
	}
	items := e.open()
	if len(items) != 1 || items[0].Kind != KindDeleted || items[0].Project != "demo-app" || len(items[0].Changes) == 0 {
		t.Fatalf("Open() = %+v, want deleted demo-app", items)
	}
	if err := e.mgr.Discard("demo-app"); err != nil {
		t.Fatal(err)
	}
	if items := e.open(); len(items) != 0 {
		t.Fatalf("Open() after restore = %+v", items)
	}
}

func TestDeletedProjectApprovedRemovesApproval(t *testing.T) {
	e := newEnv(t)
	e.approveDemo()
	if err := os.Remove(filepath.Join(e.root, "projects", "demo-app.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Approve("demo-app", ""); err != nil {
		t.Fatal(err)
	}
	if items := e.open(); len(items) != 0 {
		t.Fatalf("Open() = %+v", items)
	}
	if _, err := os.Stat(e.mgr.snapshotPath("demo-app")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("snapshot remains: %v", err)
	}
}

func TestChangedFreeCommandIsMarked(t *testing.T) {
	e := newEnv(t)
	e.write("projects/demo-app.yaml", demoProject+"steps:\n  - type: command\n    run: echo one\n  - type: compose-up\n")
	e.approveDemo()
	e.write("projects/demo-app.yaml", demoProject+"steps:\n  - type: command\n    run: echo two\n  - type: compose-up\n")
	items := e.open()
	if len(items) != 1 {
		t.Fatalf("Open() = %+v", items)
	}
	c, ok := hasChange(items[0], "steps[0].run")
	if !ok || !c.Free || c.Old != "echo one" || c.New != "echo two" || !items[0].FreeCommands() {
		t.Fatalf("changes = %+v", items[0].Changes)
	}
	for _, c := range items[0].Changes {
		if c.Path != "steps[0].run" && c.Free {
			t.Fatalf("unrelated change marked free: %+v", c)
		}
	}
}

func TestRecreatedCredentialReopensProject(t *testing.T) {
	e := newEnv(t)
	e.approveDemo()
	old, err := e.vault.GetByName("demo-registry")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.vault.Delete(old.ID); err != nil {
		t.Fatal(err)
	}
	newID := e.credential("demo-registry")
	items := e.open()
	if len(items) != 1 || items[0].Kind != KindCredential {
		t.Fatalf("Open() = %+v, want one credential item", items)
	}
	c, ok := hasChange(items[0], "credentials.demo-registry")
	if !ok || c.Old != old.ID || c.New != newID {
		t.Fatalf("changes = %+v", items[0].Changes)
	}
	// A run still gets the approved id, which no longer exists.
	if a, _ := e.mgr.Approved("demo-app"); a.Credentials["demo-registry"] != old.ID {
		t.Fatalf("approved credentials = %v", a.Credentials)
	}
	e.approveDemo()
	if items := e.open(); len(items) != 0 {
		t.Fatalf("Open() = %+v", items)
	}
}

func TestFingerprintIgnoresFormatting(t *testing.T) {
	e := newEnv(t)
	e.approveDemo()
	reordered := `# a comment
images:
  - credential: demo-registry
    services:
      - app
    env_var: DEMO_APP_TAG
    ref: registry.example.com/demo/app
    name: app
env_file: /srv/demo/app/.env
template: image-tag
dir: /srv/demo/app
name: demo-app
schema: 1
`
	e.write("projects/demo-app.yaml", reordered)
	if items := e.open(); len(items) != 0 {
		t.Fatalf("formatting change opened an approval: %+v", items)
	}
}

func TestTamperedSnapshotIsNotApproved(t *testing.T) {
	e := newEnv(t)
	e.approveDemo()
	path := e.mgr.snapshotPath("demo-app")
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "/srv/demo/app", "/srv/evil", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.mgr.Approved("demo-app"); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("Approved() = %v, want ErrIntegrity", err)
	}
	model, _, _ := e.store.Load()
	am, skipped, _ := e.mgr.ApprovedModel(model)
	if len(am.Projects) != 0 || skipped["demo-app"] == nil {
		t.Fatalf("ApprovedModel() = %+v, %v", am.Projects, skipped)
	}
	if items := e.open(); len(items) != 0 {
		// The file still equals the vault fingerprint; the broken snapshot is what blocks runs.
		t.Fatalf("Open() = %+v", items)
	}
}

func TestLockedVaultChangesNothing(t *testing.T) {
	e := newEnv(t)
	e.approveDemo()
	e.write("projects/demo-app.yaml", demoProject+"values:\n  note: x\n")
	e.vault.Forget()
	before := e.read("projects/demo-app.yaml")
	checks := map[string]error{
		"Approve":        e.mgr.Approve("demo-app", ""),
		"Discard":        e.mgr.Discard("demo-app"),
		"ApproveCurrent": e.mgr.ApproveCurrent("demo-app", config.RevisionAbsent),
	}
	_, checks["ApproveAll"] = e.mgr.ApproveAll()
	_, checks["DiscardAll"] = e.mgr.DiscardAll()
	_, checks["Open"] = e.mgr.Open()
	_, checks["Approved"] = e.mgr.Approved("demo-app")
	for name, err := range checks {
		if !errors.Is(err, vault.ErrLocked) {
			t.Errorf("%s locked = %v, want ErrLocked", name, err)
		}
	}
	if e.read("projects/demo-app.yaml") != before {
		t.Fatal("a locked vault let a file change")
	}
}

func TestApproveCurrentAfterWrite(t *testing.T) {
	e := newEnv(t)
	e.approveDemo()
	model, revs, err := e.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	p, _ := model.Project("demo-app")
	next := *p
	next.Dir = "/srv/demo/moved"
	rev, err := e.store.SaveProject(next, revs[config.ProjectPath("demo-app")])
	if err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.ApproveCurrent("demo-app", rev); err != nil {
		t.Fatal(err)
	}
	if items := e.open(); len(items) != 0 {
		t.Fatalf("Open() = %+v", items)
	}

	rev, err = e.store.SaveProject(next, rev)
	if err != nil {
		t.Fatal(err)
	}
	e.write("projects/demo-app.yaml", demoProject) // someone edits in between
	if err := e.mgr.ApproveCurrent("demo-app", rev); !errors.Is(err, ErrChanged) {
		t.Fatalf("ApproveCurrent() = %v, want ErrChanged", err)
	}
}

func TestApproveAllAndDiscardAll(t *testing.T) {
	e := newEnv(t)
	e.write("projects/second-app.yaml", strings.Replace(demoProject, "demo-app", "second-app", 1))
	done, err := e.mgr.ApproveAll()
	if err != nil || len(done) != 2 {
		t.Fatalf("ApproveAll() = %v, %v", done, err)
	}
	e.write("projects/demo-app.yaml", demoProject+"values:\n  a: b\n")
	e.write("projects/third-app.yaml", strings.Replace(demoProject, "demo-app", "third-app", 1))
	done, err = e.mgr.DiscardAll()
	if err != nil || len(done) != 2 {
		t.Fatalf("DiscardAll() = %v, %v", done, err)
	}
	if items := e.open(); len(items) != 0 {
		t.Fatalf("Open() = %+v", items)
	}
}

func TestSnapshotPermissionsAndNoSecrets(t *testing.T) {
	e := newEnv(t)
	e.approveDemo()
	path := e.mgr.snapshotPath("demo-app")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %v, %v", info, err)
	}
	if d, _ := os.Stat(filepath.Dir(path)); d.Mode().Perm() != 0o700 {
		t.Fatalf("snapshot dir mode = %v", d.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "demo-token-value-0001") {
		t.Fatal("snapshot holds a secret value")
	}
}

func TestChangelogCredentialIsBoundToTheApproval(t *testing.T) {
	e := newEnv(t)
	id, err := e.vault.Add("demo-changelog", vault.KindSecret, map[string]string{vault.FieldValue: "test-token-not-real"})
	if err != nil {
		t.Fatal(err)
	}
	e.write("projects/demo-app.yaml", demoProject+"    version_rule:\n      changelog_credential: demo-changelog\n")
	e.approveDemo()
	if a, err := e.mgr.Approved("demo-app"); err != nil || a.Credentials["demo-changelog"] != id {
		t.Fatalf("approved credentials = %v, %v", a.Credentials, err)
	}

	// Another credential name, and a recreated credential, both reopen the project.
	e.write("projects/demo-app.yaml", demoProject+"    version_rule:\n      changelog_credential: demo-registry\n")
	if items := e.open(); len(items) != 1 || items[0].Project != "demo-app" {
		t.Fatalf("Open() after renaming the credential = %+v", items)
	}
	e.write("projects/demo-app.yaml", demoProject+"    version_rule:\n      changelog_credential: demo-changelog\n")
	if items := e.open(); len(items) != 0 {
		t.Fatalf("Open() after restoring = %+v", items)
	}
	if err := e.vault.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.vault.Add("demo-changelog", vault.KindSecret, map[string]string{vault.FieldValue: "test-token-not-real"}); err != nil {
		t.Fatal(err)
	}
	if items := e.open(); len(items) != 1 {
		t.Fatalf("Open() after recreating the credential = %+v", items)
	}
}
