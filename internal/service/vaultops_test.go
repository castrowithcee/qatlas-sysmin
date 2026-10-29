package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

const (
	newPass   = "example-new-pass-2"
	otherPass = "example-other-pass-3"
)

func TestCreateVaultOnFirstSetup(t *testing.T) {
	e := newEnv(t, false, false)
	e.start(t)
	c := e.dial(t)
	if err := c.CreateVault(testPass); err != nil {
		t.Fatal(err)
	}
	if st, _ := c.Status(); st.Vault != "unlocked" {
		t.Fatalf("status = %+v", st)
	}
	// The service is in the state after an unlock: signing works and the passphrase opens a session.
	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.dial(t).CreateVault(otherPass), control.CodeExists)
	// The created vault was not replaced.
	if err := e.dial(t).OpenSession(otherPass); err == nil {
		t.Fatal("the second create replaced the vault")
	}
	if err := e.dial(t).OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	var ce *control.Error
	if err := c.Call(control.OpVaultCreate, nil, nil); err == nil {
		t.Fatal("create without passphrase succeeded")
	} else if ce = err.(*control.Error); ce.Code != control.CodeBadRequest && ce.Code != control.CodeExists {
		t.Fatalf("code = %q", ce.Code)
	}
	e.cancel()
	<-e.done
	e.done <- nil
	entries := readEntries(t, filepath.Join(e.cfg.StateDir, "logs"))
	found := false
	for _, en := range entries {
		if en.Action == "vault.create" && en.Result == "success" && en.MAC != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no signed vault.create entry: %+v", entries)
	}
	assertNoPassphrase(t, e.cfg.StateDir, testPass, otherPass)
}

func TestCreateVaultExistsWhenLocked(t *testing.T) {
	e := newEnv(t, true, false)
	e.start(t)
	wantCode(t, e.dial(t).CreateVault(otherPass), control.CodeExists)
	if err := e.dial(t).Unlock(testPass); err != nil {
		t.Fatal(err)
	}
}

func TestCreateVaultRace(t *testing.T) {
	e := newEnv(t, false, false)
	e.start(t)
	const n = 6
	res := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { res <- e.dial(t).CreateVault(testPass) }()
	}
	ok := 0
	for i := 0; i < n; i++ {
		if err := <-res; err == nil {
			ok++
		} else {
			wantCode(t, err, control.CodeExists)
		}
	}
	if ok != 1 {
		t.Fatalf("%d creates succeeded, want 1", ok)
	}
}

func TestChangePassphrase(t *testing.T) {
	e := newEnv(t, true, false)
	e.start(t)
	c := e.dial(t)
	wantCode(t, c.ChangePassphrase(testPass, newPass), control.CodeLocked)
	if err := c.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	wantCode(t, c.ChangePassphrase(testPass, newPass), control.CodeAdminRequired)
	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	wantCode(t, c.ChangePassphrase(wrongPass, newPass), control.CodeWrongPassphrase)
	wantCode(t, c.ChangePassphrase(testPass, ""), control.CodeBadRequest)
	if err := c.ChangePassphrase(testPass, newPass); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.dial(t).OpenSession(testPass), control.CodeWrongPassphrase)
	if err := e.dial(t).OpenSession(newPass); err != nil {
		t.Fatal(err)
	}
	e.cancel()
	<-e.done
	e.done <- nil
	var rekeys int
	for _, en := range readEntries(t, filepath.Join(e.cfg.StateDir, "logs")) {
		if en.Action == "vault.rekey" {
			rekeys++
		}
	}
	if rekeys < 3 {
		t.Fatalf("rekey entries = %d", rekeys)
	}
	assertNoPassphrase(t, e.cfg.StateDir, testPass, newPass, wrongPass)
}

func TestChangePassphraseAbsentVault(t *testing.T) {
	e := newEnv(t, false, false)
	e.start(t)
	wantCode(t, e.dial(t).ChangePassphrase(testPass, newPass), control.CodeLocked)
}

func TestAdminTimeoutSettings(t *testing.T) {
	e := newEnv(t, true, false)
	e.start(t)
	c := e.dial(t)
	if err := c.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	got, err := c.Settings()
	if err != nil || got.AdminTimeout != "15m0s" || got.Explicit {
		t.Fatalf("settings = %+v, %v", got, err)
	}
	if _, err := c.SetAdminTimeout("5m"); err == nil {
		t.Fatal("set without session succeeded")
	} else {
		wantCode(t, err, control.CodeAdminRequired)
	}
	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	// Other settings stay.
	path := filepath.Join(e.cfg.ConfigDir, "sysmin.yaml")
	if err := os.WriteFile(path, []byte("schema: 1\npoll_interval: 10m\nwebhook:\n  listen: 127.0.0.1:9000\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"500ms", "48h", "-1s"} {
		_, err := c.SetAdminTimeout(bad)
		wantCode(t, err, control.CodeInvalid)
		if err.(*control.Error).Problems[0].Field != "vault.admin_timeout" {
			t.Fatalf("problems = %+v", err.(*control.Error).Problems)
		}
	}
	_, err = c.SetAdminTimeout("soon")
	wantCode(t, err, control.CodeBadRequest)
	res, err := c.SetAdminTimeout("5m")
	if err != nil || res.AdminTimeout != "5m0s" || !res.Explicit {
		t.Fatalf("set = %+v, %v", res, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "admin_timeout: 5m") || !strings.Contains(string(data), "poll_interval: 10m") ||
		!strings.Contains(string(data), "127.0.0.1:9000") {
		t.Fatalf("sysmin.yaml = %s", data)
	}
	got, _ = e.dial(t).Settings()
	if got.AdminTimeout != "5m0s" || !got.Explicit {
		t.Fatalf("settings = %+v", got)
	}
	// 0 and the range limits are allowed; 0 makes every change ask again.
	for _, ok := range []string{"1s", "24h", "0"} {
		if _, err := c.SetAdminTimeout(ok); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
	got, _ = c.Settings()
	if got.AdminTimeout != "0s" || !got.Explicit {
		t.Fatalf("settings = %+v", got)
	}
}

func TestAdminTimeoutNeedsUnlockedVault(t *testing.T) {
	e := newEnv(t, true, false)
	e.start(t)
	_, err := e.dial(t).SetAdminTimeout("5m")
	wantCode(t, err, control.CodeLocked)
}

func TestStatusAdminSession(t *testing.T) {
	e := newEnv(t, true, false)
	e.start(t)
	c := e.dial(t)
	st, _ := c.Status()
	if st.ConfigDir != e.cfg.ConfigDir || st.Admin || st.AdminRemaining != 0 || st.AdminTimeout != "15m0s" {
		t.Fatalf("status = %+v", st)
	}
	if err := c.Unlock(testPass); err != nil {
		t.Fatal(err)
	}
	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	st, _ = c.Status()
	if !st.Admin || st.AdminRemaining != 900 {
		t.Fatalf("status = %+v", st)
	}
	// Reading does not renew: the remaining time follows the clock only.
	e.clock.add(5 * time.Minute)
	for i := 0; i < 3; i++ {
		st, _ = c.Status()
	}
	if st.AdminRemaining != 600 {
		t.Fatalf("remaining = %d, want 600", st.AdminRemaining)
	}
	// Another connection has no session.
	if o, _ := e.dial(t).Status(); o.Admin || o.AdminRemaining != 0 {
		t.Fatalf("other connection = %+v", o)
	}
	// The session was not used up by reading.
	if _, err := c.SetAdminTimeout("15m"); err != nil {
		t.Fatal(err)
	}
	if err := c.EndSession(); err != nil {
		t.Fatal(err)
	}
	st, _ = c.Status()
	if st.Admin || st.AdminRemaining != 0 {
		t.Fatalf("after end = %+v", st)
	}
	// With timeout 0 the passphrase covers one change.
	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetAdminTimeout("0"); err != nil {
		t.Fatal(err)
	}
	if err := c.OpenSession(testPass); err != nil {
		t.Fatal(err)
	}
	st, _ = c.Status()
	if !st.Admin || st.AdminRemaining != 0 || st.AdminTimeout != "0s" {
		t.Fatalf("zero timeout = %+v", st)
	}
	if _, err := c.SetAdminTimeout("1m"); err != nil {
		t.Fatal(err)
	}
	st, _ = c.Status()
	if st.Admin {
		t.Fatalf("after the covered change = %+v", st)
	}
}

func assertNoPassphrase(t *testing.T, dir string, pass ...string) {
	t.Helper()
	filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
		if strings.Contains(p, "vault") || d.IsDir() {
			return nil
		}
		data, _ := os.ReadFile(p)
		for _, s := range pass {
			if strings.Contains(string(data), s) {
				t.Errorf("%s contains a passphrase", p)
			}
		}
		return nil
	})
}
