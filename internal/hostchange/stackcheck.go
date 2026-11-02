package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/adopt"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
)

// Defaults of the observation after a change that touches a running stack.
const (
	DefaultStackWindow   = 60 * time.Second
	DefaultStackInterval = 2 * time.Second
)

// StackCheck is the reusable health check for changes that may disturb running Compose stacks: Affected
// finds the stacks a change reaches before it is made, Watch observes them afterwards.
type StackCheck struct {
	// Docker runs docker; nil means the real program.
	Docker execute.Runner
	// Window is how long the containers are observed after the change; zero means DefaultStackWindow.
	Window time.Duration
	// Interval is the time between two looks at the containers; zero means DefaultStackInterval.
	Interval time.Duration
	// CanRead says why the service user cannot read path; nil asks the kernel as the service user.
	CanRead func(path string) error
}

// ContainerState is what Watch compares later observations with.
type ContainerState struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	RestartCount int    `json:"restart_count"`
	// Unhealthy says the container was unhealthy before the change, which is then not blamed on it.
	Unhealthy bool `json:"unhealthy"`
}

// AffectedStack is a running stack that a change reaches, with the state of its containers before the change.
type AffectedStack struct {
	Project string `json:"project"`
	Dir     string `json:"dir"`
	// Reason says which path of the stack lies under the changed path, for example "bind mount /srv/demo-stack/data".
	Reason string `json:"reason"`
	// Files are the compose files and env files the service must keep reading.
	Files      []string         `json:"files"`
	Containers []ContainerState `json:"containers"`
}

// StackFinding is the first thing Watch found wrong. Its text names stack, container and finding.
type StackFinding struct {
	Stack     string
	Container string
	Finding   string
}

func (f *StackFinding) Error() string {
	if f.Container == "" {
		return fmt.Sprintf("stack %s: %s", f.Stack, f.Finding)
	}
	return fmt.Sprintf("stack %s, container %s: %s", f.Stack, f.Container, f.Finding)
}

func (s StackCheck) docker() execute.Runner {
	if s.Docker != nil {
		return s.Docker
	}
	return &execute.ExecRunner{}
}

func (s StackCheck) window() time.Duration {
	if s.Window > 0 {
		return s.Window
	}
	return DefaultStackWindow
}

func (s StackCheck) interval() time.Duration {
	if s.Interval > 0 {
		return s.Interval
	}
	return DefaultStackInterval
}

// inspectFormat prints one JSON object per container with only what the check needs; no environment.
const inspectFormat = `{"id":{{json .Id}},"name":{{json .Name}},"restarts":{{json .RestartCount}},` +
	`"status":{{json .State.Status}},"running":{{json .State.Running}},"restarting":{{json .State.Restarting}},` +
	`"health":{{if .State.Health}}{{json .State.Health.Status}}{{else}}""{{end}},` +
	`"envfiles":{{json (index .Config.Labels "com.docker.compose.project.environment_file")}},` +
	`"mounts":{{json .Mounts}}}`

type inspected struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Restarts   int    `json:"restarts"`
	Status     string `json:"status"`
	Running    bool   `json:"running"`
	Restarting bool   `json:"restarting"`
	Health     string `json:"health"`
	EnvFiles   string `json:"envfiles"`
	Mounts     []struct {
		Type   string `json:"Type"`
		Source string `json:"Source"`
	} `json:"mounts"`
}

// inspect reads the containers; one that no longer exists is missing from the result.
func (s StackCheck) inspect(ctx context.Context, ids []string) (map[string]inspected, error) {
	if len(ids) == 0 {
		return map[string]inspected{}, nil
	}
	args := append([]string{"inspect", "--format", inspectFormat}, ids...)
	res, err := s.docker().Run(ctx, execute.Command{Name: "docker", Args: args})
	out := map[string]inspected{}
	for _, line := range strings.Split(string(res.Output), "\n") {
		line = strings.TrimSpace(line)
		var c inspected
		if strings.HasPrefix(line, "{") && json.Unmarshal([]byte(line), &c) == nil && c.ID != "" {
			out[c.ID] = c
		}
	}
	var exit *execute.ExitError
	switch {
	case err == nil, errors.As(err, &exit) && (len(out) > 0 || strings.Contains(string(res.Output), "No such object")):
		return out, nil
	case ctx.Err() != nil:
		return nil, ctx.Err()
	}
	return nil, fmt.Errorf("docker inspect failed: %w", err)
}

// Affected returns the running stacks that have their working directory, a compose file, an env file or a
// bind mount of a container at or below changed. A host without docker has none. Containers beyond what
// adopt.ListStacks considers cannot be checked, which is an error rather than a silent gap.
func (s StackCheck) Affected(ctx context.Context, changed string) ([]AffectedStack, error) {
	stacks, truncated, err := adopt.ListStacks(ctx, s.docker())
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("the running stacks cannot be listed: %w", err)
	}
	if truncated {
		return nil, errors.New("too many running containers to check the stacks reliably")
	}
	var ids []string
	for _, st := range stacks {
		ids = append(ids, st.ContainerIDs...)
	}
	details, err := s.inspect(ctx, ids)
	if err != nil {
		return nil, err
	}
	var out []AffectedStack
	for _, st := range stacks {
		a := AffectedStack{Project: st.ComposeProject, Dir: st.Dir}
		reason := ""
		note := func(what, p string) {
			if reason == "" && pathUnder(changed, p) {
				reason = what + " " + p
			}
		}
		note("working directory", st.Dir)
		for _, f := range st.ComposeFiles {
			note("compose file", f)
			a.Files = append(a.Files, f)
		}
		for _, id := range st.ContainerIDs {
			c, ok := details[id]
			if !ok {
				continue
			}
			for _, f := range splitPaths(c.EnvFiles) {
				note("env file", f)
				a.Files = appendOnce(a.Files, f)
			}
			for _, m := range c.Mounts {
				if m.Type == "bind" {
					note("bind mount", m.Source)
				}
			}
			a.Containers = append(a.Containers, ContainerState{
				ID: c.ID, Name: strings.TrimPrefix(c.Name, "/"), RestartCount: c.Restarts, Unhealthy: c.Health == "unhealthy",
			})
		}
		if reason != "" {
			a.Reason = reason
			out = append(out, a)
		}
	}
	return out, nil
}

// Watch observes the containers of the stacks for the window. It returns a *StackFinding as soon as a
// container stopped, is restarting, restarted since Affected or became unhealthy, or the service user cannot
// read a compose or env file any longer. After the window without a finding it returns nil.
func (s StackCheck) Watch(ctx context.Context, stacks []AffectedStack) error {
	if len(stacks) == 0 {
		return nil
	}
	start := time.Now()
	for {
		if err := s.lookOnce(ctx, stacks); err != nil {
			return err
		}
		remaining := s.window() - time.Since(start)
		if remaining <= 0 {
			return nil
		}
		wait := min(s.interval(), remaining)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (s StackCheck) lookOnce(ctx context.Context, stacks []AffectedStack) error {
	var ids []string
	for _, st := range stacks {
		for _, c := range st.Containers {
			ids = append(ids, c.ID)
		}
	}
	now, err := s.inspect(ctx, ids)
	if err != nil {
		return err
	}
	canRead := s.CanRead
	if canRead == nil {
		canRead = canReadAsServiceUser
	}
	for _, st := range stacks {
		for _, before := range st.Containers {
			cur, ok := now[before.ID]
			switch {
			case !ok:
				return &StackFinding{st.Project, before.Name, "the container is gone"}
			case !cur.Running || cur.Status != "running":
				return &StackFinding{st.Project, before.Name, "the container is not running (" + cur.Status + ")"}
			case cur.Restarting:
				return &StackFinding{st.Project, before.Name, "the container is restarting"}
			case cur.Restarts != before.RestartCount:
				return &StackFinding{st.Project, before.Name,
					fmt.Sprintf("the container restarted (restart count %d, was %d)", cur.Restarts, before.RestartCount)}
			case cur.Health == "unhealthy" && !before.Unhealthy:
				return &StackFinding{st.Project, before.Name, "the container became unhealthy"}
			}
		}
		for _, f := range st.Files {
			if err := canRead(f); err != nil {
				return &StackFinding{Stack: st.Project, Finding: "the service cannot read " + f + ": " + err.Error()}
			}
		}
	}
	return nil
}

func splitPaths(list string) []string {
	var out []string
	for _, p := range strings.Split(list, ",") {
		if p = strings.TrimSpace(p); filepath.IsAbs(p) && !strings.ContainsAny(p, "\x00\n\r") {
			out = append(out, filepath.Clean(p))
		}
	}
	return out
}

func appendOnce(list []string, p string) []string {
	for _, q := range list {
		if q == p {
			return list
		}
	}
	return append(list, p)
}

// pathUnder reports whether p is root or below it, compared as written and with links resolved, so that a
// link into the changed folder does not hide a stack.
func pathUnder(root, p string) bool {
	if p == "" || !filepath.IsAbs(p) {
		return false
	}
	p = filepath.Clean(p)
	if lexicallyUnder(root, p) {
		return true
	}
	real, err := filepath.EvalSymlinks(p)
	return err == nil && lexicallyUnder(root, real)
}

func lexicallyUnder(root, p string) bool {
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}

// canReadAsServiceUser asks the kernel whether the service user can read path, by running test -r with the
// user's identity and groups.
func canReadAsServiceUser(path string) error {
	u, err := user.Lookup(client.ServiceUser)
	if err != nil {
		return fmt.Errorf("the user %s is unknown: %w", client.ServiceUser, err)
	}
	uid, err1 := strconv.ParseUint(u.Uid, 10, 32)
	gid, err2 := strconv.ParseUint(u.Gid, 10, 32)
	if err1 != nil || err2 != nil {
		return fmt.Errorf("the user %s has no numeric identity", client.ServiceUser)
	}
	var groups []uint32
	if names, err := u.GroupIds(); err == nil {
		for _, g := range names {
			if id, err := strconv.ParseUint(g, 10, 32); err == nil {
				groups = append(groups, uint32(id))
			}
		}
	}
	test, err := exec.LookPath("test")
	if err != nil {
		return err
	}
	cmd := exec.Command(test, "-r", path)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	cmd.Dir = "/"
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: groups}}
	if err := cmd.Run(); err != nil {
		if _, statErr := os.Lstat(path); statErr != nil {
			return statErr
		}
		return errors.New("not readable")
	}
	return nil
}
