package service

import (
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

// A fresh installation has no sysmin.yaml; every service operation works and the first change creates it.
func TestFreshInstallWithoutSettingsFile(t *testing.T) {
	e := newEnv(t, true, true)
	if _, err := e.svc.vault.Add("demo-registry", vault.KindRegistry, map[string]string{
		vault.FieldUser: "demo-user", vault.FieldToken: demoSecret}); err != nil {
		t.Fatal(err)
	}
	e.start(t)
	c := e.dial(t)
	if e.exists("sysmin.yaml") {
		t.Fatal("sysmin.yaml exists before any change")
	}

	if list, err := c.Approvals(); err != nil || len(list) != 0 {
		t.Fatalf("approvals = %+v, %v", list, err)
	}
	if res, err := c.AddChannel(opsHook()); err != nil || res.State != control.StatePending {
		t.Fatalf("channel add = %+v, %v", res, err)
	}
	if !e.exists("sysmin.yaml") {
		t.Fatal("the first channel did not create sysmin.yaml")
	}
	if res, err := c.AddProject(control.ProjectParams{Name: "demo-app", File: demoProject}); err != nil || res.State != control.StatePending {
		t.Fatalf("project add = %+v, %v", res, err)
	}
	if list, err := c.Approvals(); err != nil || len(list) == 0 {
		t.Fatalf("open approvals = %+v, %v", list, err)
	}
	if _, err := c.DeployStatus("demo-app"); err != nil {
		t.Fatalf("deploy status: %v", err)
	}
	res, err := c.SetCredential(registryParams(newSecret))
	if err != nil || res.Action != "replaced" || !slices.Equal(res.Pending, []string{"demo-app"}) {
		t.Fatalf("credential set = %+v, %v", res, err)
	}
}

// A credential can be stored before sysmin.yaml exists.
func TestFreshInstallCredentialSetWithoutSettingsFile(t *testing.T) {
	e := newEnv(t, true, true)
	e.start(t)
	c := e.dial(t)
	res, err := c.SetCredential(registryParams(newSecret))
	if err != nil || res.Action != "created" {
		t.Fatalf("credential set = %+v, %v", res, err)
	}
}
