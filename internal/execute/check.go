package execute

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

const (
	// DefaultPollInterval is the pause between two checks when Executor.PollInterval is zero.
	DefaultPollInterval = 2 * time.Second

	// maxCheckBody caps what check-http reads of a response.
	maxCheckBody = 1 << 20

	defaultCheckStatus = 200
)

func (e *Executor) pollInterval() time.Duration {
	if e.PollInterval > 0 {
		return e.PollInterval
	}
	return DefaultPollInterval
}

// wait pauses for the poll interval and reports false when ctx ended first.
func (e *Executor) wait(ctx context.Context) bool {
	t := time.NewTimer(e.pollInterval())
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// containerRefs returns the containers to check: the named ones, or those of the compose services (all
// services of the project when none are named).
func containerRefs(ctx context.Context, s *stepRun, st config.ResolvedStep) ([]string, error) {
	if len(st.Containers) > 0 {
		for _, n := range st.Containers {
			if n == "" || strings.HasPrefix(n, "-") {
				return nil, fmt.Errorf("container %q cannot be passed as an argument", n)
			}
		}
		return st.Containers, nil
	}
	if len(st.Services) == 0 {
		return psIDs(ctx, s, st)
	}
	var ids []string
	seen := map[string]bool{}
	for _, svc := range st.Services {
		one := st
		one.Services = []string{svc}
		got, err := psIDs(ctx, s, one)
		if err != nil {
			return nil, fmt.Errorf("service %s: %w", svc, err)
		}
		for _, id := range got {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}

// psIDs lists the container IDs of the services in st (all of the project when none); none is an error.
func psIDs(ctx context.Context, s *stepRun, st config.ResolvedStep) ([]string, error) {
	c, err := composeCommand(st, "ps", "-a", "-q")
	if err != nil {
		return nil, err
	}
	res, err := s.run(ctx, c)
	if err != nil {
		return nil, err
	}
	var ids []string
	seen := map[string]bool{}
	for _, id := range strings.Fields(string(res.Output)) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("no containers found")
	}
	return ids, nil
}

type containerState struct {
	name, status, health string
}

func (c containerState) String() string {
	if c.health != "none" {
		return c.name + "=" + c.status + "/" + c.health
	}
	return c.name + "=" + c.status
}

func inspectContainer(ctx context.Context, s *stepRun, ref string) (containerState, error) {
	res, err := s.run(ctx, Command{Name: "docker", Args: []string{"inspect", "--type", "container", "--format",
		"{{.Name}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", ref}})
	if err != nil {
		return containerState{}, err
	}
	f := strings.Fields(string(res.Output))
	if len(f) != 3 {
		return containerState{}, fmt.Errorf("unexpected inspect output for %s", ref)
	}
	return containerState{name: strings.TrimPrefix(f[0], "/"), status: f[1], health: f[2]}, nil
}

func runCheckContainer(ctx context.Context, s *stepRun, st config.ResolvedStep) error {
	refs, err := containerRefs(ctx, s, st)
	if err != nil {
		return err
	}
	var last []string
	for {
		prev := last
		last = nil
		ready := true
		for _, ref := range refs {
			cs, err := inspectContainer(ctx, s, ref)
			if err != nil {
				if ctx.Err() != nil && prev != nil {
					// The time limit cut the query short; report the last complete answer.
					return fmt.Errorf("containers not ready: %s", strings.Join(prev, " "))
				}
				return err
			}
			last = append(last, cs.String())
			if cs.status == "exited" || cs.status == "dead" {
				return fmt.Errorf("container %s", cs)
			}
			if cs.health != "healthy" && (cs.health != "none" || cs.status != "running") {
				ready = false
			}
		}
		if ready {
			s.note("containers ready: %s", strings.Join(last, " "))
			return nil
		}
		if !s.e.wait(ctx) {
			return fmt.Errorf("containers not ready: %s", strings.Join(last, " "))
		}
	}
}

func describeCheckContainer(_ describeCtx, st config.ResolvedStep) ([]string, error) {
	if len(st.Containers) > 0 {
		out := []string{}
		for _, n := range st.Containers {
			out = append(out, "docker inspect "+quoteWord(n)+" until healthy, or running without healthcheck")
		}
		return out, nil
	}
	var out []string
	if len(st.Services) == 0 {
		c, err := composeCommand(st, "ps", "-a", "-q")
		if err != nil {
			return nil, err
		}
		out = append(out, displayCommand(c)+" (in "+st.Dir+")")
	}
	for _, svc := range st.Services {
		one := st
		one.Services = []string{svc}
		c, err := composeCommand(one, "ps", "-a", "-q")
		if err != nil {
			return nil, err
		}
		out = append(out, displayCommand(c)+" (in "+st.Dir+")")
	}
	return append(out, "docker inspect each container until healthy, or running without healthcheck"), nil
}

func (e *Executor) httpClient() *http.Client {
	var c http.Client
	if e.HTTPClient != nil {
		c = *e.HTTPClient
	}
	// The status is checked exactly as the server sends it.
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}

func httpParams(st config.ResolvedStep) (string, int, error) {
	u, err := url.Parse(st.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", 0, fmt.Errorf("url %q is not an http or https url", st.URL)
	}
	status := st.Status
	if status == 0 {
		status = defaultCheckStatus
	}
	return st.URL, status, nil
}

func runCheckHTTP(ctx context.Context, s *stepRun, st config.ResolvedStep) error {
	target, want, err := httpParams(st)
	if err != nil {
		return err
	}
	client := s.e.httpClient()
	var last error
	for {
		err := getOnce(ctx, client, target, want, st.Contains)
		if err == nil {
			s.note("GET %s: status %d", target, want)
			return nil
		}
		// Report the last real outcome, not the context error that cut a request short.
		if last == nil || ctx.Err() == nil {
			last = err
		}
		s.logf("check: %v", err)
		if !s.e.wait(ctx) {
			return fmt.Errorf("GET %s: %w", target, last)
		}
	}
}

// getOnce performs one request. The error says what did not match.
func getOnce(ctx context.Context, client *http.Client, target string, want int, contains string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", urlErrCause(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCheckBody))
	if err != nil {
		return fmt.Errorf("status %d, reading the body failed: %w", resp.StatusCode, err)
	}
	if resp.StatusCode != want {
		return fmt.Errorf("status %d, want %d", resp.StatusCode, want)
	}
	if contains != "" && !bytes.Contains(body, []byte(contains)) {
		return fmt.Errorf("status %d, body does not contain %q", resp.StatusCode, contains)
	}
	return nil
}

// urlErrCause drops the URL that *url.Error repeats.
func urlErrCause(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func describeCheckHTTP(_ describeCtx, st config.ResolvedStep) ([]string, error) {
	target, want, err := httpParams(st)
	if err != nil {
		return nil, err
	}
	s := fmt.Sprintf("GET %s expect %d", target, want)
	if st.Contains != "" {
		s += fmt.Sprintf(" containing %q", st.Contains)
	}
	return []string{s}, nil
}
