package execute

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// TestDockerIntegration runs compose-up, check-container and a volume backup against a real Docker daemon. It
// only runs when SYSMIN_DOCKER_TEST=1, because it pulls images and creates containers and volumes.
func TestDockerIntegration(t *testing.T) {
	if os.Getenv("SYSMIN_DOCKER_TEST") != "1" {
		t.Skip("set SYSMIN_DOCKER_TEST=1 to run the test against a real Docker daemon")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("docker is not reachable: %v", err)
	}
	rnd := make([]byte, 4)
	if _, err := rand.Read(rnd); err != nil {
		t.Fatal(err)
	}
	name := "sysmin-test-" + hex.EncodeToString(rnd)
	volume := name + "-data"
	dir := t.TempDir()
	compose := "services:\n  web:\n    image: " + DefaultBackupImage + "\n" +
		"    command: [\"sleep\", \"600\"]\n" +
		"    healthcheck:\n      test: [\"CMD\", \"true\"]\n      interval: 1s\n      retries: 3\n" +
		"    volumes:\n      - data:/data\nvolumes:\n  data:\n    name: " + volume + "\n"
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		down := exec.Command("docker", "compose", "-p", name, "down", "-v", "--remove-orphans")
		down.Dir = dir
		_ = down.Run()
		_ = exec.Command("docker", "volume", "rm", "-f", volume).Run()
	})

	step := func(typ config.StepType) config.ResolvedStep {
		return config.ResolvedStep{Type: typ, Timeout: 2 * time.Minute, Dir: dir, ComposeProject: name}
	}
	backup := step(config.StepBackup)
	backup.Backup = &config.BackupParams{Volume: volume, Keep: 1}
	root := t.TempDir()
	ex := &Executor{Runner: &ExecRunner{}, BackupRoot: root, PollInterval: time.Second}
	plan := config.Plan{Project: name, Steps: []config.ResolvedStep{
		step(config.StepComposeUp), step(config.StepCheckContainer), backup}}
	rep, err := ex.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("%v\n%+v", err, rep)
	}
	archive := filepath.Join(rep.Steps[2].BackupDir, volume+".tar.gz")
	if fi, err := os.Stat(archive); err != nil || fi.Size() == 0 {
		t.Errorf("archive %s: %v", archive, err)
	}
	if !strings.HasPrefix(filepath.Base(rep.Steps[2].BackupDir), "20") {
		t.Errorf("backup dir %q", rep.Steps[2].BackupDir)
	}
}
