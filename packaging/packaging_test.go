package packaging

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const unitFile = "sysmin.service"

func readUnit(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(unitFile)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestUnitHasRequiredLines(t *testing.T) {
	lines := map[string]bool{}
	for _, l := range strings.Split(readUnit(t), "\n") {
		lines[strings.TrimSpace(l)] = true
	}
	required := []string{
		"[Unit]", "[Service]", "[Install]",
		"After=network-online.target docker.service",
		"Wants=network-online.target",
		"Type=simple",
		"ExecStart=/usr/local/bin/sysmin run",
		"User=sysmin",
		"Group=sysmin",
		"SupplementaryGroups=docker",
		"RuntimeDirectory=sysmin",
		"RuntimeDirectoryMode=0750",
		"StateDirectory=sysmin",
		"StateDirectoryMode=0700",
		"ConfigurationDirectory=sysmin",
		"ConfigurationDirectoryMode=0750",
		"LoadCredentialEncrypted=vault-key:/var/lib/sysmin/vault/key.cred",
		"SetCredential=vault-key:\\n",
		"NoNewPrivileges=yes",
		"ProtectSystem=full",
		"ReadWritePaths=/etc/sysmin",
		"PrivateTmp=yes",
		"Restart=on-failure",
		"WantedBy=multi-user.target",
	}
	for _, want := range required {
		if !lines[want] {
			t.Errorf("unit lacks line %q", want)
		}
	}
}

func TestUnitVerifies(t *testing.T) {
	analyze, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze not in PATH")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unit := strings.Replace(readUnit(t), "ExecStart=/usr/local/bin/sysmin run", "ExecStart="+exe+" run", 1)
	path := filepath.Join(t.TempDir(), unitFile)
	if err := os.WriteFile(path, []byte(unit), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(analyze, "verify", path).CombinedOutput()
	if err == nil {
		return
	}
	// Missing user, group or docker.service on the build host is not a unit error.
	var problems []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(l) == "" || strings.Contains(l, "docker.service") ||
			strings.Contains(l, "sysmin") && (strings.Contains(l, "user") || strings.Contains(l, "group") ||
				strings.Contains(l, "User") || strings.Contains(l, "Group")) {
			continue
		}
		problems = append(problems, l)
	}
	if len(problems) > 0 {
		t.Errorf("systemd-analyze verify: %s", strings.Join(problems, "\n"))
	}
}
