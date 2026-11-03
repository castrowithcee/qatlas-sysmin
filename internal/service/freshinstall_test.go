package service

import (
	"testing"
)

// A fresh installation has no sysmin.yaml; the service answers and the first settings change creates it.
func TestFreshInstallWithoutSettingsFile(t *testing.T) {
	e := newEnv(t, true, true)
	e.start(t)
	c := e.dial(t)
	if e.exists("sysmin.yaml") {
		t.Fatal("sysmin.yaml exists before any change")
	}
	res, err := c.Settings()
	if err != nil || res.Explicit {
		t.Fatalf("settings = %+v, %v", res, err)
	}
	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	if res, err := c.SetAdminTimeout("5m"); err != nil || res.AdminTimeout != "5m0s" || !res.Explicit {
		t.Fatalf("set = %+v, %v", res, err)
	}
	if !e.exists("sysmin.yaml") {
		t.Fatal("the first change did not create sysmin.yaml")
	}
}
