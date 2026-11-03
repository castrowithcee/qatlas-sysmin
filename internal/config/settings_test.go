package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const oldKeys = "schema: 1\npoll_interval: 10m\nwebhook:\n  listen: 127.0.0.1:9000\nchannels:\n  - name: ops\n    type: slack\n"

func TestReadSettingsIgnoresUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte(oldKeys+"vault:\n  admin_timeout: 5m\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	s, err := ReadSettings(dir)
	if err != nil || s.EffectiveAdminTimeout() != 5*time.Minute {
		t.Fatalf("settings = %+v, %v", s, err)
	}
}

func TestReadSettingsDefaultsAndSchema(t *testing.T) {
	dir := t.TempDir()
	s, err := ReadSettings(dir)
	if err != nil || s.EffectiveAdminTimeout() != DefaultAdminTimeout {
		t.Fatalf("missing file: %+v, %v", s, err)
	}
	for _, body := range []string{"poll_interval: 1h\n", "schema: 2\n"} {
		if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte(body), 0o640); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadSettings(dir); err == nil {
			t.Fatalf("%q was accepted", body)
		}
	}
}

func TestSetAdminTimeoutKeepsOtherKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SettingsFile)
	if err := os.WriteFile(path, []byte(oldKeys), 0o640); err != nil {
		t.Fatal(err)
	}
	st := NewStore(dir)
	for _, d := range []Duration{Duration(5 * time.Minute), 0} {
		if err := st.SetAdminTimeout(d); err != nil {
			t.Fatal(err)
		}
		s, err := ReadSettings(dir)
		if err != nil || s.Vault.AdminTimeout == nil || *s.Vault.AdminTimeout != d {
			t.Fatalf("settings = %+v, %v", s, err)
		}
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{"poll_interval: 10m", "127.0.0.1:9000", "name: ops", "admin_timeout: 0"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("missing %q in %s", want, data)
		}
	}
}

func TestSetAdminTimeoutCreatesFileAndRefusesOtherSchema(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "conf")
	st := NewStore(dir)
	if err := st.SetAdminTimeout(Duration(90 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if s, err := ReadSettings(dir); err != nil || s.EffectiveAdminTimeout() != 90*time.Second {
		t.Fatalf("settings = %+v, %v", s, err)
	}
	path := filepath.Join(dir, SettingsFile)
	if err := os.WriteFile(path, []byte("schema: 2\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAdminTimeout(Duration(time.Minute)); err == nil {
		t.Fatal("newer schema was overwritten")
	}
	if data, _ := os.ReadFile(path); string(data) != "schema: 2\n" {
		t.Fatalf("file changed: %s", data)
	}
}

func TestValidateAdminTimeout(t *testing.T) {
	for _, ok := range []time.Duration{0, time.Second, 24 * time.Hour} {
		if p := ValidateAdminTimeout(ok); p != nil {
			t.Fatalf("%s: %+v", ok, p)
		}
	}
	for _, bad := range []time.Duration{500 * time.Millisecond, 48 * time.Hour, -time.Second} {
		p := ValidateAdminTimeout(bad)
		if len(p) != 1 || p[0].Field != "vault.admin_timeout" {
			t.Fatalf("%s: %+v", bad, p)
		}
	}
}
