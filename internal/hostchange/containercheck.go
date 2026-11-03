package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Defaults of the observation after a change that touches running containers.
const (
	DefaultContainerWindow   = 60 * time.Second
	DefaultContainerInterval = 2 * time.Second

	// maxContainers bounds how many running containers the check looks at; more is an error, not a silent gap.
	maxContainers = 500
	maxNameLen    = 64
)

const composeWorkingDirLabel = "com.docker.compose.project.working_dir"

var containerIDRe = regexp.MustCompile(`^[0-9a-f]{12,64}$`)

// ContainerCheck is the reusable health check for changes that may disturb running containers: Affected
// finds the containers a change reaches before it is made, Watch observes them afterwards.
type ContainerCheck struct {
	// Exec runs docker; nil means the host's SystemExecutor.
	Exec Executor
	// Window is how long the containers are observed after the change; zero means DefaultContainerWindow.
	Window time.Duration
	// Interval is the time between two looks at the containers; zero means DefaultContainerInterval.
	Interval time.Duration
}

// ContainerState is what Watch compares later observations with.
type ContainerState struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	RestartCount int    `json:"restart_count"`
	// Unhealthy says the container was unhealthy before the change, which is then not blamed on it.
	Unhealthy bool `json:"unhealthy"`
}

// ContainerFinding is the first thing Watch found wrong. Its text names container and finding.
type ContainerFinding struct {
	Container string
	Finding   string
}

func (f *ContainerFinding) Error() string {
	return fmt.Sprintf("container %s: %s", f.Container, f.Finding)
}

func (c ContainerCheck) exec() Executor {
	if c.Exec != nil {
		return c.Exec
	}
	return SystemExecutor{}
}

func (c ContainerCheck) window() time.Duration {
	if c.Window > 0 {
		return c.Window
	}
	return DefaultContainerWindow
}

func (c ContainerCheck) interval() time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return DefaultContainerInterval
}

// inspectFormat prints one JSON object per container with only what the check needs; no environment.
const inspectFormat = `{"id":{{json .Id}},"name":{{json .Name}},"restarts":{{json .RestartCount}},` +
	`"status":{{json .State.Status}},"running":{{json .State.Running}},"restarting":{{json .State.Restarting}},` +
	`"health":{{with index .State "Health"}}{{json .Status}}{{else}}""{{end}},` +
	`"workdir":{{json (index .Config.Labels "` + composeWorkingDirLabel + `")}},` +
	`"mounts":{{json .Mounts}}}`

type inspected struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Restarts   int    `json:"restarts"`
	Status     string `json:"status"`
	Running    bool   `json:"running"`
	Restarting bool   `json:"restarting"`
	Health     string `json:"health"`
	WorkDir    string `json:"workdir"`
	Mounts     []struct {
		Source string `json:"Source"`
	} `json:"mounts"`
}

func (c ContainerCheck) docker(ctx context.Context, args ...string) ([]byte, error) {
	return c.exec().Run(ctx, Command{Argv: append([]string{"docker"}, args...)})
}

// inspect reads the containers; one that no longer exists is missing from the result.
func (c ContainerCheck) inspect(ctx context.Context, ids []string) (map[string]inspected, error) {
	out := map[string]inspected{}
	if len(ids) == 0 {
		return out, nil
	}
	raw, err := c.docker(ctx, append([]string{"inspect", "--format", inspectFormat}, ids...)...)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		var ct inspected
		if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &ct) == nil && containerIDRe.MatchString(ct.ID) {
			ct.Name = plainText(strings.TrimPrefix(ct.Name, "/"))
			ct.Status = plainText(ct.Status)
			out[ct.ID] = ct
		}
	}
	switch {
	case err == nil, len(out) > 0, strings.Contains(string(raw), "No such object"):
		return out, nil
	case ctx.Err() != nil:
		return nil, ctx.Err()
	}
	return nil, fmt.Errorf("docker inspect failed: %w", err)
}

// listRunning reads the ids of the running containers. A host without docker has none.
func (c ContainerCheck) listRunning(ctx context.Context) ([]string, error) {
	raw, err := c.docker(ctx, "ps", "--no-trunc", "--format", "{{.ID}}")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("the running containers cannot be listed: %w", err)
	}
	var ids []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); containerIDRe.MatchString(line) {
			ids = append(ids, line)
		}
	}
	if len(ids) > maxContainers {
		return nil, errors.New("too many running containers to check them reliably")
	}
	return ids, nil
}

// Affected returns the running containers that use changed or a folder below it as the source of a volume or
// bind mount, or as the working directory of their Compose project. Without such a container there is
// nothing to observe. A host without docker has none.
func (c ContainerCheck) Affected(ctx context.Context, changed string) ([]ContainerState, error) {
	ids, err := c.listRunning(ctx)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	details, err := c.inspect(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []ContainerState
	for _, id := range ids {
		ct, ok := details[id]
		if !ok || !ct.reaches(changed) {
			continue
		}
		out = append(out, ContainerState{ID: ct.ID, Name: ct.Name, RestartCount: ct.Restarts, Unhealthy: ct.Health == "unhealthy"})
	}
	return out, nil
}

func (ct inspected) reaches(changed string) bool {
	if pathUnder(changed, ct.WorkDir) {
		return true
	}
	for _, m := range ct.Mounts {
		if pathUnder(changed, m.Source) {
			return true
		}
	}
	return false
}

// Watch observes the containers for the window. It returns a *ContainerFinding as soon as a container is
// gone, stopped, restarting, restarted since Affected or became unhealthy. After the window without a
// finding it returns nil. Without containers it returns at once and starts no program.
func (c ContainerCheck) Watch(ctx context.Context, containers []ContainerState) error {
	if len(containers) == 0 {
		return nil
	}
	start := time.Now()
	for {
		if err := c.lookOnce(ctx, containers); err != nil {
			return err
		}
		remaining := c.window() - time.Since(start)
		if remaining <= 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(c.interval(), remaining)):
		}
	}
}

func (c ContainerCheck) lookOnce(ctx context.Context, containers []ContainerState) error {
	ids := make([]string, len(containers))
	for i, before := range containers {
		ids[i] = before.ID
	}
	now, err := c.inspect(ctx, ids)
	if err != nil {
		return err
	}
	for _, before := range containers {
		cur, ok := now[before.ID]
		switch {
		case !ok:
			return &ContainerFinding{before.Name, "the container is gone"}
		case !cur.Running || cur.Status != "running":
			return &ContainerFinding{before.Name, "the container is not running (" + cur.Status + ")"}
		case cur.Restarting:
			return &ContainerFinding{before.Name, "the container is restarting"}
		case cur.Restarts != before.RestartCount:
			return &ContainerFinding{before.Name,
				fmt.Sprintf("the container restarted (restart count %d, was %d)", cur.Restarts, before.RestartCount)}
		case cur.Health == "unhealthy" && !before.Unhealthy:
			return &ContainerFinding{before.Name, "the container became unhealthy"}
		}
	}
	return nil
}

// plainText drops control and format characters from text that docker reported and bounds its length.
func plainText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > maxNameLen {
		s = string(r[:maxNameLen])
	}
	return s
}

// pathUnder reports whether p is root or below it, compared as written and with links resolved, so that a
// link into the changed folder does not hide a container.
func pathUnder(root, p string) bool {
	return related(p, func(q string) bool { return lexicallyUnder(root, q) })
}

func related(p string, match func(string) bool) bool {
	if p == "" || !filepath.IsAbs(p) {
		return false
	}
	p = filepath.Clean(p)
	if match(p) {
		return true
	}
	real, err := filepath.EvalSymlinks(p)
	return err == nil && match(real)
}

func lexicallyUnder(root, p string) bool {
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}
