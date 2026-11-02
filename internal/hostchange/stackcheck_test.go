package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
)

const (
	idWeb = "0123456789ab"
	idDB  = "ba9876543210"
)

type fakeMount struct{ Type, Source string }

type fakeContainer struct {
	ID         string
	Name       string
	Restarts   int
	Status     string
	Running    bool
	Restarting bool
	Health     string
	EnvFiles   string
	Mounts     []fakeMount
}

func healthy(id, name string) fakeContainer {
	return fakeContainer{ID: id, Name: "/" + name, Status: "running", Running: true}
}

func (c fakeContainer) line() string {
	b, _ := json.Marshal(map[string]any{
		"id": c.ID, "name": c.Name, "restarts": c.Restarts, "status": c.Status, "running": c.Running,
		"restarting": c.Restarting, "health": c.Health, "envfiles": c.EnvFiles, "mounts": c.Mounts,
	})
	return string(b) + "\n"
}

// fakeDocker answers docker ps with the stacks and docker inspect with the next scripted state; the last
// state stays.
type fakeDocker struct {
	mu       sync.Mutex
	stacks   string
	states   [][]fakeContainer
	inspects int
	missing  bool
}

func stackRow(id, project, dir, files string) string {
	b, _ := json.Marshal(map[string]string{"id": id, "project": project, "dir": dir, "files": files, "oneoff": "False"})
	return string(b) + "\n"
}

func (f *fakeDocker) Run(_ context.Context, c execute.Command) (execute.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch c.Args[0] {
	case "ps":
		return execute.Result{Output: []byte(f.stacks)}, nil
	case "inspect":
		i := min(f.inspects, len(f.states)-1)
		f.inspects++
		var out strings.Builder
		for _, ct := range f.states[i] {
			out.WriteString(ct.line())
		}
		if f.missing {
			return execute.Result{Output: []byte(out.String() + "Error: No such object: x\n")}, &execute.ExitError{Code: 1}
		}
		return execute.Result{Output: []byte(out.String())}, nil
	}
	return execute.Result{}, errors.New("unexpected docker call")
}

func newCheck(f *fakeDocker) StackCheck {
	return StackCheck{Docker: f, Window: 40 * time.Millisecond, Interval: 5 * time.Millisecond, CanRead: func(string) error { return nil }}
}

func TestAffectedStacksByEachKindOfPath(t *testing.T) {
	cases := []struct {
		name   string
		dir    string
		files  string
		env    string
		mounts []fakeMount
		want   string
	}{
		{"working directory", "/srv/demo-stack", "/srv/elsewhere/compose.yml", "/srv/elsewhere/.env", nil, "working directory /srv/demo-stack"},
		{"compose file", "/srv/elsewhere", "/srv/demo-stack/compose.yml", "/srv/elsewhere/.env", nil, "compose file /srv/demo-stack/compose.yml"},
		{"env file", "/srv/elsewhere", "/srv/elsewhere/compose.yml", "/srv/demo-stack/.env", nil, "env file /srv/demo-stack/.env"},
		{"bind mount", "/srv/elsewhere", "/srv/elsewhere/compose.yml", "", []fakeMount{{"volume", "/var/lib/docker/volumes/x"}, {"bind", "/srv/demo-stack/data"}}, "bind mount /srv/demo-stack/data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ct := healthy(idWeb, "demo-web-1")
			ct.EnvFiles, ct.Mounts = tc.env, tc.mounts
			f := &fakeDocker{stacks: stackRow(idWeb, "demo", tc.dir, tc.files), states: [][]fakeContainer{{ct}}}
			got, err := newCheck(f).Affected(context.Background(), "/srv/demo-stack")
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Project != "demo" || got[0].Reason != tc.want || len(got[0].Containers) != 1 ||
				got[0].Containers[0].Name != "demo-web-1" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestUnrelatedStackIsNotAffected(t *testing.T) {
	ct := healthy(idWeb, "demo-web-1")
	ct.Mounts = []fakeMount{{"bind", "/srv/demo-stack-other/data"}, {"bind", "/srv"}}
	f := &fakeDocker{stacks: stackRow(idWeb, "demo", "/srv/demo-stack-other", "/srv/demo-stack-other/compose.yml"), states: [][]fakeContainer{{ct}}}
	got, err := newCheck(f).Affected(context.Background(), "/srv/demo-stack")
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestWatchHealthyPasses(t *testing.T) {
	f := &fakeDocker{
		stacks: stackRow(idWeb, "demo", "/srv/demo-stack", "/srv/demo-stack/compose.yml"),
		states: [][]fakeContainer{{healthy(idWeb, "demo-web-1")}},
	}
	c := newCheck(f)
	affected, err := c.Affected(context.Background(), "/srv/demo-stack")
	if err != nil || len(affected) != 1 {
		t.Fatalf("%v %v", affected, err)
	}
	start := time.Now()
	if err := c.Watch(context.Background(), affected); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < c.Window || f.inspects < 3 {
		t.Errorf("observed %v with %d looks, want the whole window", time.Since(start), f.inspects)
	}
}

func TestWatchNamesTheFinding(t *testing.T) {
	ok := healthy(idWeb, "demo-web-1")
	restarted := ok
	restarted.Restarts = 2
	stopped := ok
	stopped.Status, stopped.Running = "exited", false
	restarting := ok
	restarting.Restarting = true
	unhealthy := ok
	unhealthy.Health = "unhealthy"
	cases := []struct {
		name  string
		after fakeContainer
		want  string
	}{
		{"restart", restarted, "stack demo, container demo-web-1: the container restarted (restart count 2, was 0)"},
		{"stop", stopped, "stack demo, container demo-web-1: the container is not running (exited)"},
		{"restarting", restarting, "stack demo, container demo-web-1: the container is restarting"},
		{"unhealthy", unhealthy, "stack demo, container demo-web-1: the container became unhealthy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeDocker{
				stacks: stackRow(idWeb, "demo", "/srv/demo-stack", ""),
				states: [][]fakeContainer{{ok}, {ok}, {tc.after}},
			}
			c := newCheck(f)
			affected, err := c.Affected(context.Background(), "/srv/demo-stack")
			if err != nil {
				t.Fatal(err)
			}
			var finding *StackFinding
			err = c.Watch(context.Background(), affected)
			if !errors.As(err, &finding) || err.Error() != tc.want {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestWatchGoneContainerAndUnreadableFile(t *testing.T) {
	f := &fakeDocker{
		stacks: stackRow(idWeb, "demo", "/srv/demo-stack", "/srv/demo-stack/compose.yml"),
		states: [][]fakeContainer{{healthy(idWeb, "demo-web-1")}, {}},
	}
	c := newCheck(f)
	affected, _ := c.Affected(context.Background(), "/srv/demo-stack")
	f.missing = true
	if err := c.Watch(context.Background(), affected); err == nil || err.Error() != "stack demo, container demo-web-1: the container is gone" {
		t.Fatalf("got %v", err)
	}

	f2 := &fakeDocker{
		stacks: stackRow(idWeb, "demo", "/srv/demo-stack", "/srv/demo-stack/compose.yml"),
		states: [][]fakeContainer{{healthy(idWeb, "demo-web-1")}},
	}
	c2 := newCheck(f2)
	c2.CanRead = func(p string) error {
		if p == "/srv/demo-stack/compose.yml" {
			return errors.New("permission denied")
		}
		return nil
	}
	affected, _ = c2.Affected(context.Background(), "/srv/demo-stack")
	want := "stack demo: the service cannot read /srv/demo-stack/compose.yml: permission denied"
	if err := c2.Watch(context.Background(), affected); err == nil || err.Error() != want {
		t.Fatalf("got %v", err)
	}
}

func TestAlreadyUnhealthyContainerIsNotBlamedOnTheChange(t *testing.T) {
	sick := healthy(idWeb, "demo-web-1")
	sick.Health = "unhealthy"
	f := &fakeDocker{stacks: stackRow(idWeb, "demo", "/srv/demo-stack", ""), states: [][]fakeContainer{{sick}}}
	c := newCheck(f)
	affected, _ := c.Affected(context.Background(), "/srv/demo-stack")
	if err := c.Watch(context.Background(), affected); err != nil {
		t.Fatal(err)
	}
}

func TestWatchEndsWithTheContext(t *testing.T) {
	f := &fakeDocker{stacks: stackRow(idWeb, "demo", "/srv/demo-stack", ""), states: [][]fakeContainer{{healthy(idWeb, "demo-web-1")}}}
	c := newCheck(f)
	c.Window = time.Minute
	affected, _ := c.Affected(context.Background(), "/srv/demo-stack")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := c.Watch(ctx, affected); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestPathUnder(t *testing.T) {
	for p, want := range map[string]bool{
		"/srv/demo-stack": true, "/srv/demo-stack/a/b": true, "/srv/demo-stack2": false, "/srv": false, "relative": false, "": false,
	} {
		if got := pathUnder("/srv/demo-stack", p); got != want {
			t.Errorf("pathUnder(%q) = %v", p, got)
		}
	}
	if !slices.Equal(splitPaths("/a/b, /c/./d,rel,"), []string{"/a/b", "/c/d"}) {
		t.Error("splitPaths")
	}
}
