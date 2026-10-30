package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

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

const demoSecret = "demo-token-value-0001"

// approvalEnv is a service with an unlocked vault, one credential and a configuration holding demo-app.
func approvalEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, true, true)
	if _, err := e.svc.vault.Add("demo-registry", vault.KindRegistry, map[string]string{
		vault.FieldUser: "demo-user", vault.FieldToken: demoSecret}); err != nil {
		t.Fatal(err)
	}
	e.write(t, "sysmin.yaml", "schema: 1\n")
	e.write(t, "projects/demo-app.yaml", demoProject)
	e.start(t)
	return e
}

func (e *env) write(t *testing.T, rel, content string) {
	t.Helper()
	path := filepath.Join(e.cfg.ConfigDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func (e *env) read(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.cfg.ConfigDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (e *env) admin(t *testing.T) *client.Client {
	t.Helper()
	c := e.dial(t)
	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	return c
}

// shownAll lists the open approvals as a client shows them before an all-at-once request.
func shownAll(t *testing.T, c *client.Client) []control.ShownApproval {
	t.Helper()
	items, err := c.Approvals()
	if err != nil {
		t.Fatal(err)
	}
	shown := make([]control.ShownApproval, 0, len(items))
	for _, it := range items {
		shown = append(shown, it.Shown())
	}
	return shown
}

// approveAll approves what c lists now, as a client does after it showed the list.
func approveAll(t *testing.T, c *client.Client) (control.ApprovalsAllResult, error) {
	t.Helper()
	return c.ApproveAll(shownAll(t, c))
}

func openOf(t *testing.T, c *client.Client, project string) control.ApprovalSummary {
	t.Helper()
	items, err := c.Approvals()
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Project == project {
			return it
		}
	}
	t.Fatalf("no open approval for %s in %+v", project, items)
	return control.ApprovalSummary{}
}

func TestApprovalsListShowApproveDiscard(t *testing.T) {
	e := approvalEnv(t)
	reader := e.dial(t) // reading needs no admin session
	sum := openOf(t, reader, "demo-app")
	if sum.Kind != "new" || sum.Fingerprint == "" {
		t.Fatalf("summary = %+v", sum)
	}
	if _, err := e.svc.approvals.Approved("demo-app"); !errors.Is(err, approval.ErrNotApproved) {
		t.Fatalf("approved before approval: %v", err)
	}

	// Without an admin session nothing changes.
	vaultBefore, _ := e.svc.vault.Approvals()
	wantCode(t, reader.Approve("demo-app", sum.Fingerprint), control.CodeAdminRequired)
	wantCode(t, reader.Discard("demo-app", sum.Fingerprint), control.CodeAdminRequired)
	_, err := approveAll(t, reader)
	wantCode(t, err, control.CodeAdminRequired)
	_, err = reader.DiscardAll(shownAll(t, reader))
	wantCode(t, err, control.CodeAdminRequired)
	if e.read(t, "projects/demo-app.yaml") != demoProject {
		t.Fatal("the project file changed without a session")
	}
	if after, _ := e.svc.vault.Approvals(); len(after) != len(vaultBefore) {
		t.Fatalf("the vault changed without a session: %v", after)
	}
	if _, err := e.svc.approvals.Approved("demo-app"); !errors.Is(err, approval.ErrNotApproved) {
		t.Fatalf("approved without a session: %v", err)
	}
	openOf(t, reader, "demo-app")

	adm := e.admin(t)
	wantCode(t, adm.Approve("demo-app", "0000"), control.CodeChanged)
	wantCode(t, adm.Approve("other-app", ""), control.CodeNothingOpen)
	wantCode(t, adm.Approve("../x", ""), control.CodeBadRequest)
	wantCode(t, adm.Approve("demo-app", ""), control.CodeChanged) // an empty fingerprint stands for a deletion
	if err := adm.Approve("demo-app", sum.Fingerprint); err != nil {
		t.Fatal(err)
	}
	old, err := e.svc.approvals.Approved("demo-app")
	if err != nil || old.Fingerprint != sum.Fingerprint {
		t.Fatalf("approved = %+v, %v", old, err)
	}
	if items, _ := reader.Approvals(); len(items) != 0 {
		t.Fatalf("open after approval: %+v", items)
	}

	// A hand change with a free command is open and marked; the approved state stays the old one.
	changed := demoProject + "steps:\n  - type: command\n    run: echo hello\n  - type: compose-up\n"
	e.write(t, "projects/demo-app.yaml", changed)
	sum = openOf(t, reader, "demo-app")
	if sum.Kind != "changed" || !sum.FreeCommands || sum.Changes == 0 {
		t.Fatalf("summary = %+v", sum)
	}
	detail, err := reader.Approval("demo-app")
	if err != nil {
		t.Fatal(err)
	}
	free := false
	for _, c := range detail.Details {
		if c.Free && c.New == "echo hello" {
			free = true
		}
	}
	if !free || detail.Fingerprint != sum.Fingerprint {
		t.Fatalf("detail = %+v", detail)
	}
	still, _ := e.svc.approvals.Approved("demo-app")
	if still.Fingerprint != old.Fingerprint {
		t.Fatal("the approved state moved before approval")
	}
	wantCode(t, mustFail(reader.Approval("other-app")), control.CodeNothingOpen)

	// Discard restores the file; the approved state is untouched.
	if err := adm.Discard("demo-app", sum.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if items, _ := reader.Approvals(); len(items) != 0 {
		t.Fatalf("open after discard: %+v", items)
	}
	if strings.Contains(e.read(t, "projects/demo-app.yaml"), "echo hello") {
		t.Fatal("the hand change survived the discard")
	}

	// Approval takes the new state.
	e.write(t, "projects/demo-app.yaml", changed)
	sum = openOf(t, reader, "demo-app")
	if err := adm.Approve("demo-app", sum.Fingerprint); err != nil {
		t.Fatal(err)
	}
	now, _ := e.svc.approvals.Approved("demo-app")
	if now.Fingerprint != sum.Fingerprint || now.Fingerprint == old.Fingerprint {
		t.Fatalf("approved fingerprint = %s", now.Fingerprint)
	}

	// A discard of a stale view is refused; a deleted project is approved without a fingerprint.
	e.write(t, "projects/demo-app.yaml", demoProject)
	wantCode(t, adm.Call(control.OpApprovalsDiscard, control.ApprovalParams{Project: "demo-app", Fingerprint: "0000"}, nil), control.CodeChanged)
	if err := os.Remove(filepath.Join(e.cfg.ConfigDir, "projects", "demo-app.yaml")); err != nil {
		t.Fatal(err)
	}
	if got := openOf(t, reader, "demo-app"); got.Kind != "deleted" {
		t.Fatalf("summary = %+v", got)
	}
	if err := adm.Approve("demo-app", ""); err != nil {
		t.Fatal(err)
	}
	if items, _ := reader.Approvals(); len(items) != 0 {
		t.Fatalf("open after approving the deletion: %+v", items)
	}
}

func mustFail[T any](_ T, err error) error { return err }

func TestApprovalsLockedVault(t *testing.T) {
	e := newEnv(t, true, false)
	e.write(t, "sysmin.yaml", "schema: 1\n")
	e.write(t, "projects/demo-app.yaml", demoProject)
	e.start(t)
	c := e.dial(t)
	wantCode(t, mustFail(c.Approvals()), control.CodeLocked)
	wantCode(t, mustFail(c.Approval("demo-app")), control.CodeLocked)
	wantCode(t, c.Approve("demo-app", "x"), control.CodeLocked)
}

func TestApprovalsAllAndAuditLog(t *testing.T) {
	e := approvalEnv(t)
	e.write(t, "projects/demo-two.yaml", strings.ReplaceAll(demoProject, "demo-app", "demo-two"))
	reader := e.dial(t)
	wantCode(t, mustFail(approveAll(t, reader)), control.CodeAdminRequired)

	adm := e.admin(t)
	res, err := approveAll(t, adm)
	if err != nil || len(res.Done) != 2 || len(res.Failed) != 0 {
		t.Fatalf("approve-all = %+v, %v", res, err)
	}
	if res, err = approveAll(t, adm); err != nil || len(res.Done) != 0 {
		t.Fatalf("approve-all with nothing open = %+v, %v", res, err)
	}
	e.write(t, "projects/demo-app.yaml", demoProject+"# edited\nsteps:\n  - type: compose-up\n")
	if err := os.Remove(filepath.Join(e.cfg.ConfigDir, "projects", "demo-two.yaml")); err != nil {
		t.Fatal(err)
	}
	if res, err = adm.DiscardAll(shownAll(t, adm)); err != nil || len(res.Done) != 2 || len(res.Failed) != 0 {
		t.Fatalf("discard-all = %+v, %v", res, err)
	}
	if items, _ := reader.Approvals(); len(items) != 0 {
		t.Fatalf("open after discard-all: %+v", items)
	}

	got := map[string]auditlog.Entry{}
	for _, en := range readEntries(t, filepath.Join(e.cfg.StateDir, "logs")) {
		if en.Action == auditlog.OperationApprovalGrant || en.Action == auditlog.OperationApprovalDeny {
			key := en.Action + "/" + en.Project + "/" + en.Result + "/" + en.ErrorCode
			got[key] = en
		}
	}
	for _, key := range []string{
		"approval.grant/demo-app/success/", "approval.grant/demo-two/success/",
		"approval.deny/demo-app/success/", "approval.deny/demo-two/success/",
		"approval.grant//failure/admin-required",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("no log entry %q in %v", key, got)
		}
	}
	for _, en := range readEntries(t, filepath.Join(e.cfg.StateDir, "logs")) {
		data, _ := json.Marshal(en)
		if strings.Contains(string(data), demoSecret) || strings.Contains(string(data), testPass) {
			t.Fatalf("entry holds a secret: %s", data)
		}
	}
}

func TestDiscardRefusesCredentialOnlyItem(t *testing.T) {
	e := approvalEnv(t)
	adm := e.admin(t)
	if _, err := approveAll(t, adm); err != nil {
		t.Fatal(err)
	}
	// A credential created anew under the same name is another vault entry.
	infos, err := e.svc.vault.List()
	if err != nil || len(infos) != 1 {
		t.Fatalf("credentials = %+v, %v", infos, err)
	}
	if err := e.svc.vault.Delete(infos[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.vault.Add("demo-registry", vault.KindRegistry, map[string]string{
		vault.FieldUser: "demo-user", vault.FieldToken: "demo-token-value-0002"}); err != nil {
		t.Fatal(err)
	}
	if got := openOf(t, adm, "demo-app"); got.Kind != "credential" {
		t.Fatalf("summary = %+v", got)
	}
	wantCode(t, adm.Discard("demo-app", openOf(t, adm, "demo-app").Fingerprint), control.CodeCannotDiscard)
	res, err := adm.DiscardAll(shownAll(t, adm))
	if err != nil || len(res.Done) != 0 || len(res.Failed) != 0 {
		t.Fatalf("discard-all = %+v, %v", res, err)
	}
}

func TestAllAtOnceRefusesWhatWasNotShown(t *testing.T) {
	e := approvalEnv(t)
	adm := e.admin(t)
	approved := func() string {
		t.Helper()
		a, err := e.svc.approvals.Approved("demo-app")
		if err != nil && !errors.Is(err, approval.ErrNotApproved) {
			t.Fatal(err)
		}
		return a.Fingerprint
	}
	refused := func(what string, res control.ApprovalsAllResult, err error) {
		t.Helper()
		wantCode(t, err, control.CodeChanged)
		if len(res.Done) != 0 || len(res.Failed) != 0 {
			t.Fatalf("%s: result = %+v", what, res)
		}
	}

	// A stale fingerprint.
	shown := shownAll(t, adm)
	e.write(t, "projects/demo-app.yaml", demoProject+"values:\n  note: edited after the list\n")
	res, err := adm.ApproveAll(shown)
	refused("stale fingerprint", res, err)
	if approved() != "" {
		t.Fatal("a stale state was approved")
	}

	// A kind that differs although project and fingerprint match.
	shown = shownAll(t, adm)
	shown[0].Kind = "changed"
	res, err = adm.ApproveAll(shown)
	refused("other kind", res, err)

	// An open approval that was not shown.
	shown = shownAll(t, adm)
	e.write(t, "projects/demo-two.yaml", strings.ReplaceAll(demoProject, "demo-app", "demo-two"))
	res, err = adm.ApproveAll(shown)
	refused("unshown entry", res, err)
	if approved() != "" {
		t.Fatal("approved although another approval was not shown")
	}

	// The shown set is approved as a whole.
	res, err = approveAll(t, adm)
	if err != nil || len(res.Done) != 2 || len(res.Failed) != 0 {
		t.Fatalf("approve-all = %+v, %v", res, err)
	}
	before := approved()

	// A deleted project written again after the list.
	if err := os.Remove(filepath.Join(e.cfg.ConfigDir, "projects", "demo-app.yaml")); err != nil {
		t.Fatal(err)
	}
	shown = shownAll(t, adm)
	if len(shown) != 1 || shown[0].Kind != "deleted" || shown[0].Fingerprint != "" {
		t.Fatalf("shown = %+v", shown)
	}
	e.write(t, "projects/demo-app.yaml", demoProject+"values:\n  note: written again\n")
	res, err = adm.ApproveAll(shown)
	refused("re-created project", res, err)
	wantCode(t, adm.Approve("demo-app", ""), control.CodeChanged)
	if approved() != before {
		t.Fatal("the re-created file was approved")
	}

	// discard-all refuses a difference and restores nothing.
	shown = shownAll(t, adm)
	e.write(t, "projects/demo-app.yaml", demoProject+"values:\n  note: edited after the last list\n")
	res, err = adm.DiscardAll(shown)
	refused("discard-all", res, err)
	if !strings.Contains(e.read(t, "projects/demo-app.yaml"), "note: edited after the last list") {
		t.Fatal("discard-all restored a file that was not shown")
	}
	res, err = adm.DiscardAll(shownAll(t, adm))
	if err != nil || len(res.Done) != 1 || res.Done[0] != "demo-app" {
		t.Fatalf("discard-all = %+v, %v", res, err)
	}
	if items, _ := adm.Approvals(); len(items) != 0 {
		t.Fatalf("open after discard-all: %+v", items)
	}
}

func TestAllAtOnceWithoutShownApprovalsIsABadRequest(t *testing.T) {
	e := approvalEnv(t)
	adm := e.admin(t)
	for _, op := range []string{control.OpApprovalsApproveAll, control.OpApprovalsDiscardAll} {
		for _, params := range []any{nil, struct{}{}, map[string]any{"shown": nil}, "demo-app"} {
			wantCode(t, adm.Call(op, params, nil), control.CodeBadRequest)
		}
	}
	if items, _ := adm.Approvals(); len(items) != 1 || e.read(t, "projects/demo-app.yaml") != demoProject {
		t.Fatalf("a bad request changed something: %+v", items)
	}
	if _, err := e.svc.approvals.Approved("demo-app"); !errors.Is(err, approval.ErrNotApproved) {
		t.Fatalf("approved by a bad request: %v", err)
	}
}
