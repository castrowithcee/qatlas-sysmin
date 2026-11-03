package hostchange

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestContainerCheckAgainstRealDocker checks the docker ps and inspect parsing for containers with and
// without a healthcheck and labels. It only runs when QSYSMIN_DOCKER_TEST=1 and uses an image that must exist
// locally.
func TestContainerCheckAgainstRealDocker(t *testing.T) {
	if os.Getenv("QSYSMIN_DOCKER_TEST") != "1" {
		t.Skip("set QSYSMIN_DOCKER_TEST=1 to run the test against a real Docker daemon")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("docker is not reachable: %v", err)
	}
	const image = "redis:7-alpine"
	if err := exec.Command("docker", "image", "inspect", image).Run(); err != nil {
		t.Skipf("image %s is not available locally", image)
	}
	rnd := make([]byte, 4)
	if _, err := rand.Read(rnd); err != nil {
		t.Fatal(err)
	}
	prefix := "qsysmin-test-inspect-" + hex.EncodeToString(rnd) + "-"
	specs := map[string][]string{
		"plain":       {},
		"healthcheck": {"--health-cmd", "true", "--health-interval", "1s"},
		"labeled":     {"--label", composeWorkingDirLabel + "=/srv/demo-app/deploy"},
	}
	ids := map[string]string{}
	for name, extra := range specs {
		cname := prefix + name
		t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", cname).Run() })
		args := append([]string{"run", "-d", "--name", cname}, extra...)
		args = append(args, image)
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("start %s: %v: %s", name, err, out)
		}
		ids[name] = strings.TrimSpace(string(out))
	}
	time.Sleep(3 * time.Second) // let the first health probe run

	c := ContainerCheck{}
	running, err := c.listRunning(context.Background())
	if err != nil {
		t.Fatalf("listRunning: %v", err)
	}
	for name, id := range ids {
		found := false
		for _, r := range running {
			found = found || r == id
		}
		if !found {
			t.Errorf("%s is missing from the running containers", name)
		}
	}
	got, err := c.inspect(context.Background(), []string{ids["plain"], ids["healthcheck"], ids["labeled"]})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d containers, want 3: %+v", len(got), got)
	}
	for _, name := range []string{"plain", "labeled"} {
		if h := got[ids[name]].Health; h != "" {
			t.Errorf("%s: health %q, want empty", name, h)
		}
	}
	if h := got[ids["healthcheck"]].Health; h == "" {
		t.Error("healthcheck: health is empty, want a status")
	}
	if w := got[ids["labeled"]].WorkDir; w != "/srv/demo-app/deploy" {
		t.Errorf("labeled: workdir %q", w)
	}
	if w := got[ids["plain"]].WorkDir; w != "" {
		t.Errorf("plain: workdir %q, want empty", w)
	}
}
