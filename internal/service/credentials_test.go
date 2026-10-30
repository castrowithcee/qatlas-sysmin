package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const (
	newSecret    = "demo-token-not-real-0002"
	secretValue  = "demo-secret-not-real-0003"
	registryUser = "demo-user"
)

func registryParams(token string) control.CredentialSetParams {
	return control.CredentialSetParams{
		Name: "demo-registry", Kind: "registry",
		Fields: map[string]string{vault.FieldUser: registryUser, vault.FieldToken: token},
	}
}

// approvedEnv is approvalEnv with demo-app approved.
func approvedEnv(t *testing.T) *env {
	t.Helper()
	e := approvalEnv(t)
	if _, err := approveAll(t, e.admin(t)); err != nil {
		t.Fatal(err)
	}
	return e
}

func credentialOf(t *testing.T, c *client.Client, name string) control.CredentialSummary {
	t.Helper()
	list, err := c.Credentials()
	if err != nil {
		t.Fatal(err)
	}
	for _, cr := range list {
		if cr.Name == name {
			return cr
		}
	}
	t.Fatalf("no credential %s in %+v", name, list)
	return control.CredentialSummary{}
}

func TestCredentialKindTableMatchesTheVault(t *testing.T) {
	kinds := vault.Kinds()
	if len(kinds) != len(control.CredentialKinds) {
		t.Fatalf("kinds = %v, table = %v", kinds, control.CredentialKinds)
	}
	for i, k := range kinds {
		tk := control.CredentialKinds[i]
		got := append(append([]string(nil), tk.Plain...), tk.Secret)
		want := vault.FieldsOf(k)
		slices.Sort(got)
		slices.Sort(want)
		if tk.Name != string(k) || !slices.Equal(got, want) {
			t.Errorf("table entry %+v differs from the vault kind %s with fields %v", tk, k, want)
		}
	}
}

func TestCredentialSetCreatesListsAndNeverShowsAValue(t *testing.T) {
	e := approvalEnv(t)
	c := e.dial(t)
	res, err := c.SetCredential(control.CredentialSetParams{Name: "demo-hook", Kind: "secret", Fields: map[string]string{vault.FieldValue: secretValue}})
	if err != nil || res.Action != "created" || res.State != control.StateApproved || res.ID == "" || len(res.Pending) != 0 {
		t.Fatalf("set = %+v, %v", res, err)
	}
	got := credentialOf(t, c, "demo-hook")
	if got.Kind != "secret" || got.ID != res.ID || len(got.Uses) != 0 {
		t.Fatalf("list entry = %+v", got)
	}
	reg := credentialOf(t, c, "demo-registry")
	if len(reg.Uses) != 1 || reg.Uses[0].Kind != "project" || reg.Uses[0].Name != "demo-app" || !reg.Uses[0].Current || reg.Uses[0].Approved {
		t.Fatalf("uses = %+v", reg.Uses)
	}
	list, _ := c.Credentials()
	data, _ := json.Marshal(list)
	out, _ := json.Marshal(res)
	for _, s := range []string{string(data), string(out)} {
		if strings.Contains(s, secretValue) || strings.Contains(s, demoSecret) {
			t.Fatalf("a value is in the answer: %s", s)
		}
	}
	en, err := e.svc.vault.GetByName("demo-hook")
	if err != nil || en.Fields[vault.FieldValue].Reveal() != secretValue {
		t.Fatalf("stored entry = %+v, %v", en, err)
	}
	// The audit log names the credential and holds no value, in any file.
	found := false
	for _, en := range readEntries(t, filepath.Join(e.cfg.StateDir, "logs")) {
		if en.Action == auditlog.OperationCredentialChange && en.Project == "demo-hook" && en.Result == auditlog.ResultSuccess {
			found = true
		}
	}
	if !found {
		t.Fatal("the change left no audit entry")
	}
	files, _ := filepath.Glob(filepath.Join(e.cfg.StateDir, "logs", "*"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		if strings.Contains(string(data), secretValue) {
			t.Fatalf("%s holds a secret value", f)
		}
	}
}

func TestCredentialReplaceOpensWhatUsesItAndKeepsTheApprovedValue(t *testing.T) {
	e := approvedEnv(t)
	c := e.dial(t)
	oldID := credentialOf(t, c, "demo-registry").ID

	res, err := c.SetCredential(registryParams(newSecret))
	if err != nil || res.Action != "replaced" || res.State != control.StatePending || !slices.Equal(res.Pending, []string{"demo-app"}) || len(res.Approved) != 0 {
		t.Fatalf("replace = %+v, %v", res, err)
	}
	if res.ID == oldID || credentialOf(t, c, "demo-registry").ID != res.ID {
		t.Fatalf("the id did not change: %s, %+v", oldID, res)
	}
	if got := openOf(t, c, "demo-app"); got.Kind != "credential" {
		t.Fatalf("summary = %+v", got)
	}
	// The approved state still resolves to the old entry and its value.
	ap, err := e.svc.approvals.Approved("demo-app")
	if err != nil || ap.Credentials["demo-registry"] != oldID {
		t.Fatalf("approved = %+v, %v", ap.Credentials, err)
	}
	if en, err := e.svc.vault.Get(oldID); err != nil || en.Fields[vault.FieldToken].Reveal() != demoSecret {
		t.Fatalf("old entry = %v", err)
	}
	// Approving the credential change lets the old entry go.
	if _, err := approveAll(t, e.admin(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.vault.Get(oldID); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("the old entry stayed after the approval: %v", err)
	}
}

func TestCredentialReplaceInAdminSessionApprovesOnlyWhatItAloneOpens(t *testing.T) {
	e := approvedEnv(t)
	adm := e.admin(t)
	res, err := adm.SetCredential(registryParams(newSecret))
	if err != nil || res.State != control.StateApproved || !slices.Equal(res.Approved, []string{"demo-app"}) || len(res.Pending) != 0 {
		t.Fatalf("replace in a session = %+v, %v", res, err)
	}
	if items, _ := adm.Approvals(); len(items) != 0 {
		t.Fatalf("open after an approved replace: %+v", items)
	}

	// A foreign open change of the project is not approved along with the credential.
	other := e.dial(t)
	if r, err := other.EditProject(control.ProjectParams{Name: "demo-app", Dir: "/srv/demo/moved"}); err != nil || r.State != control.StatePending {
		t.Fatalf("edit = %+v, %v", r, err)
	}
	res, err = adm.SetCredential(registryParams(secretValue))
	if err != nil || res.State != control.StatePending || !slices.Equal(res.Pending, []string{"demo-app"}) || len(res.Approved) != 0 {
		t.Fatalf("replace beside a foreign change = %+v, %v", res, err)
	}
	if got := openOf(t, adm, "demo-app"); got.Kind != "changed" {
		t.Fatalf("summary = %+v", got)
	}
	ap, _ := e.svc.approvals.Approved("demo-app")
	if ap.Project.Dir != "/srv/demo/app" {
		t.Fatalf("the foreign change was approved: %s", ap.Project.Dir)
	}

	// A connection without a session does not benefit from the session of another.
	res, err = other.SetCredential(registryParams(newSecret))
	if err != nil || res.State != control.StatePending {
		t.Fatalf("replace without a session = %+v, %v", res, err)
	}
}

func TestCredentialChangeUsesUpTheSingleChangeOfAPassphrase(t *testing.T) {
	e := approvalEnv(t)
	e.write(t, "sysmin.yaml", "schema: 1\nvault:\n  admin_timeout: 0\n")
	if _, err := approveAll(t, e.admin(t)); err != nil {
		t.Fatal(err)
	}
	c := e.dial(t)
	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	res, err := c.SetCredential(registryParams(newSecret))
	if err != nil || res.State != control.StateApproved {
		t.Fatalf("first = %+v, %v", res, err)
	}
	res, err = c.SetCredential(registryParams(secretValue))
	if err != nil || res.State != control.StatePending {
		t.Fatalf("second = %+v, %v", res, err)
	}
}

func TestCredentialSetRefusals(t *testing.T) {
	e := approvedEnv(t)
	c := e.dial(t)
	before, _ := c.Credentials()

	_, err := c.SetCredential(control.CredentialSetParams{Name: "demo-registry", Kind: "secret", Fields: map[string]string{vault.FieldValue: secretValue}})
	wantCode(t, err, control.CodeKindMismatch)

	for name, p := range map[string]control.CredentialSetParams{
		"bad name":      {Name: "Bad Name", Kind: "secret", Fields: map[string]string{vault.FieldValue: secretValue}},
		"unknown kind":  {Name: "demo-x", Kind: "nope", Fields: map[string]string{vault.FieldValue: secretValue}},
		"missing field": {Name: "demo-x", Kind: "registry", Fields: map[string]string{vault.FieldToken: secretValue}},
		"empty secret":  {Name: "demo-x", Kind: "secret", Fields: map[string]string{vault.FieldValue: ""}},
		"extra field":   {Name: "demo-x", Kind: "secret", Fields: map[string]string{vault.FieldValue: secretValue, "other": "x"}},
	} {
		_, err := c.SetCredential(p)
		probs := problemsOfErr(t, err)
		data, _ := json.Marshal(probs)
		if len(probs) == 0 || strings.Contains(string(data), secretValue) {
			t.Errorf("%s: problems = %s", name, data)
		}
	}
	after, _ := c.Credentials()
	if len(after) != len(before) {
		t.Fatalf("a refused set stored something: %+v", after)
	}
}

func TestCredentialOperationsNeedAnUnlockedVault(t *testing.T) {
	e := newEnv(t, true, false)
	e.start(t)
	c := e.dial(t)
	_, err := c.SetCredential(control.CredentialSetParams{Name: "demo-x", Kind: "secret", Fields: map[string]string{vault.FieldValue: secretValue}})
	wantCode(t, err, control.CodeLocked)
	_, err = c.RemoveCredential("demo-x")
	wantCode(t, err, control.CodeLocked)
	_, err = c.Credentials()
	wantCode(t, err, control.CodeLocked)
	// Nothing was queued as a pending entry either.
	if err := c.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	if list, err := c.Credentials(); err != nil || len(list) != 0 {
		t.Fatalf("credentials = %+v, %v", list, err)
	}
}

func TestCredentialRemove(t *testing.T) {
	e := approvedEnv(t)
	c := e.dial(t)
	if _, err := c.SetCredential(control.CredentialSetParams{Name: "demo-hook", Kind: "secret", Fields: map[string]string{vault.FieldValue: secretValue}}); err != nil {
		t.Fatal(err)
	}

	// Used in the current configuration: refused without a session, with the uses listed.
	_, err := c.RemoveCredential("demo-registry")
	wantCode(t, err, control.CodeInUse)
	var ce *control.Error
	errors.As(err, &ce)
	if len(ce.Problems) != 1 || ce.Problems[0].Field != "project:demo-app" || !strings.Contains(ce.Problems[0].Message, "current configuration and the approved state") {
		t.Fatalf("problems = %+v", ce.Problems)
	}
	if _, err := e.svc.vault.GetByName("demo-registry"); err != nil {
		t.Fatalf("a refused removal deleted the credential: %v", err)
	}

	// Used only by the approved state: the project file is gone, its approval still names the credential.
	if r, err := c.RemoveProject("demo-app", ""); err != nil || r.State != control.StatePending {
		t.Fatalf("remove project = %+v, %v", r, err)
	}
	_, err = c.RemoveCredential("demo-registry")
	wantCode(t, err, control.CodeInUse)
	errors.As(err, &ce)
	if len(ce.Problems) != 1 || !strings.Contains(ce.Problems[0].Message, "in the approved state") || strings.Contains(ce.Problems[0].Message, "current") {
		t.Fatalf("problems = %+v", ce.Problems)
	}

	// Not used: removed at once; unknown: not found.
	res, err := c.RemoveCredential("demo-hook")
	if err != nil || res.Action != "removed" || res.State != control.StateApproved {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	_, err = c.RemoveCredential("demo-hook")
	wantCode(t, err, control.CodeNotFound)
	_, err = c.RemoveCredential("Bad Name")
	if err == nil {
		t.Fatal("a name with space was accepted for a missing credential")
	}
}

func TestCredentialRemoveInAdminSessionLeavesTheUsesOpen(t *testing.T) {
	e := approvedEnv(t)
	adm := e.admin(t)
	res, err := adm.RemoveCredential("demo-registry")
	if err != nil || res.Action != "removed" || res.State != control.StatePending || !slices.Equal(res.Pending, []string{"demo-app"}) || len(res.Approved) != 0 {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	if _, err := e.svc.vault.GetByName("demo-registry"); !errors.Is(err, vault.ErrNotFound) {
		t.Fatalf("the credential is still there: %v", err)
	}
	if got := openOf(t, adm, "demo-app"); got.Kind != "credential" {
		t.Fatalf("summary = %+v", got)
	}
}

func TestCredentialRemoveDropsSupersededValues(t *testing.T) {
	e := approvedEnv(t)
	adm := e.admin(t)
	if _, err := adm.SetCredential(registryParams(newSecret)); err != nil {
		t.Fatal(err)
	}
	// Remove the project and approve, then nothing names the credential any more.
	if _, err := adm.RemoveProject("demo-app", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := adm.RemoveCredential("demo-registry"); err != nil {
		t.Fatal(err)
	}
	infos, _ := e.svc.vault.List()
	if len(infos) != 0 {
		t.Fatalf("live entries = %+v", infos)
	}
	// No superseded entry carries the old value: PurgeSuperseded with nothing to keep finds nothing.
	if n, err := e.svc.vault.PurgeSuperseded(nil); err != nil || n != 0 {
		t.Fatalf("superseded entries left = %d, %v", n, err)
	}
}

func TestCredentialChangesAreLoggedWithoutValues(t *testing.T) {
	e := approvedEnv(t)
	c := e.dial(t)
	_, _ = c.SetCredential(registryParams(newSecret))
	_, _ = c.SetCredential(control.CredentialSetParams{Name: "demo-registry", Kind: "secret", Fields: map[string]string{vault.FieldValue: secretValue}})
	_, _ = c.RemoveCredential("demo-registry")
	got := map[string]bool{}
	for _, en := range readEntries(t, filepath.Join(e.cfg.StateDir, "logs")) {
		got[en.Action+"/"+en.Project+"/"+en.Result+"/"+en.ErrorCode] = true
	}
	for _, key := range []string{
		"credential.change/demo-registry/success/", "credential.change/demo-registry/failure/kind-mismatch",
		"credential.delete/demo-registry/failure/in-use",
	} {
		if !got[key] {
			t.Errorf("no audit entry %s in %v", key, got)
		}
	}
	files, _ := filepath.Glob(filepath.Join(e.cfg.StateDir, "logs", "*"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		if strings.Contains(string(data), newSecret) || strings.Contains(string(data), secretValue) {
			t.Fatalf("%s holds a secret value", f)
		}
	}
}
