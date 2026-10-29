package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
)

func TestDeployLogOfARunKeepsSecretsOut(t *testing.T) {
	f := newDeployFixture(t, true)
	admin, reader := f.approved(t)
	f.runner.mu.Lock()
	f.runner.handle = func(line string) (execute.Result, error) {
		if strings.HasPrefix(line, "deploy") {
			return execute.Result{Output: []byte("login failed with " + demoSecret + "\n"), ExitCode: 1}, &execute.ExitError{Code: 1}
		}
		return execute.Result{}, nil
	}
	f.runner.mu.Unlock()
	if _, err := admin.DeployUpdate([]control.ProjectUpdate{appUpdate("demo-app", "1.1.0")}); err != nil {
		t.Fatal(err)
	}
	h, err := reader.DeployHistory("demo-app")
	if err != nil || len(h.Runs) != 1 {
		t.Fatalf("history = %+v %v", h, err)
	}
	res, err := reader.DeployLog("demo-app", h.Runs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Project != "demo-app" || res.Run != h.Runs[0].ID || res.Truncated || !strings.Contains(res.Text, "[redacted]") {
		t.Errorf("result = %+v", res)
	}
	if strings.Contains(res.Text, demoSecret) {
		t.Errorf("secret in the log: %s", res.Text)
	}
}

func TestDeployLogRefusesWhatIsNotARun(t *testing.T) {
	f := newDeployFixture(t, true)
	_, reader := f.approved(t)
	const id = "20260101T000000.000000000Z"
	for _, run := range []string{"", "../state", "../../etc/passwd", "a/b", "demo.log", id + "/..", strings.Repeat("a", 300)} {
		_, err := reader.DeployLog("demo-app", run)
		wantCode(t, err, control.CodeBadRequest)
	}
	for _, project := range []string{"", "../x", "Bad Name", "a/b"} {
		_, err := reader.DeployLog(project, id)
		wantCode(t, err, control.CodeBadRequest)
	}
	_, err := reader.DeployLog("demo-app", id)
	wantCode(t, err, control.CodeNotFound)
	_, err = reader.DeployLog("demo-none", id)
	wantCode(t, err, control.CodeNotFound)
}

func TestDeployLogIsCutAndCleaned(t *testing.T) {
	f := newDeployFixture(t, true)
	_, reader := f.approved(t)
	const id = "20260101T000000.000000000Z"
	dir := filepath.Join(f.cfg.StateDir, "deploy", "demo-app", "runs")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	line := "2026-01-01T00:00:00Z <step> \"quoted\" & more\n"
	big := strings.Repeat(line, (control.MaxResponseBytes*2)/len(line)) + "last \x1b[31mred\x1b[0m line\x07\x00\n"
	if err := os.WriteFile(filepath.Join(dir, id+".log"), []byte(big), 0o640); err != nil {
		t.Fatal(err)
	}
	res, err := reader.DeployLog("demo-app", id)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Text) > control.RunLogMaxBytes || !strings.HasPrefix(res.Text, line) || !strings.HasSuffix(res.Text, "last red line\n") {
		t.Errorf("truncated=%v len=%d head=%.60q tail=%.40q", res.Truncated, len(res.Text), res.Text, res.Text[max(len(res.Text)-40, 0):])
	}
	if strings.ContainsAny(res.Text, "\x1b\x07\x00") {
		t.Error("control characters in the log")
	}
}
