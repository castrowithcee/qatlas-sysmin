package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
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
	WorkDir    string
	Mounts     []fakeMount
}

func healthy(id, name string) fakeContainer {
	return fakeContainer{ID: id, Name: "/" + name, Status: "running", Running: true}
}

func (c fakeContainer) line() string {
	b, _ := json.Marshal(map[string]any{
		"id": c.ID, "name": c.Name, "restarts": c.Restarts, "status": c.Status, "running": c.Running,
		"restarting": c.Restarting, "health": c.Health, "workdir": c.WorkDir, "mounts": c.Mounts,
	})
	return string(b) + "\n"
}

// fakeDocker answers docker ps with the ids of the first scripted state and docker inspect with the next
// scripted state; the last state stays.
type fakeDocker struct {
	mu       sync.Mutex
	states   [][]fakeContainer
	pses     int
	inspects int
	err      error // fails docker ps
}

func (f *fakeDocker) Run(_ context.Context, c Command) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(c.Argv) < 2 || c.Argv[0] != "docker" {
		return nil, errors.New("unexpected command")
	}
	switch c.Argv[1] {
	case "ps":
		f.pses++
		if f.err != nil {
			return nil, f.err
		}
		var out strings.Builder
		for _, ct := range f.states[0] {
			out.WriteString(ct.ID + "\n")
		}
		return []byte(out.String()), nil
	case "inspect":
		i := min(f.inspects, len(f.states)-1)
		f.inspects++
		var out strings.Builder
		for _, ct := range f.states[i] {
			out.WriteString(ct.line())
		}
		return []byte(out.String()), nil
	}
	return nil, errors.New("unexpected docker call")
}

func newCheck(f *fakeDocker) ContainerCheck {
	return ContainerCheck{Exec: f, Window: 40 * time.Millisecond, Interval: 5 * time.Millisecond}
}

func TestAffectedContainersByEachKindOfUse(t *testing.T) {
	const changed = "/srv/demo-app"
	cases := []struct {
		name string
		ct   func(*fakeContainer)
	}{
		{"volume source", func(c *fakeContainer) {
			c.Mounts = []fakeMount{{"volume", "/srv/demo-app/volumes/x"}}
		}},
		{"bind mount", func(c *fakeContainer) {
			c.Mounts = []fakeMount{{"volume", "/var/lib/docker/volumes/x"}, {"bind", "/srv/demo-app/data"}}
		}},
		{"the changed folder itself", func(c *fakeContainer) { c.Mounts = []fakeMount{{"bind", changed}} }},
		{"compose working dir", func(c *fakeContainer) { c.WorkDir = "/srv/demo-app/deploy" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ct := healthy(idWeb, "demo-web-1")
			tc.ct(&ct)
			f := &fakeDocker{states: [][]fakeContainer{{ct}}}
			got, err := newCheck(f).Affected(context.Background(), changed)
			if err != nil || len(got) != 1 || got[0].ID != idWeb || got[0].Name != "demo-web-1" {
				t.Fatalf("%v %+v", err, got)
			}
		})
	}
}

func TestMountOfAParentFolderIsNotAffected(t *testing.T) {
	ct := healthy(idWeb, "demo-web-1")
	ct.Mounts = []fakeMount{{"bind", "/srv"}}
	f := &fakeDocker{states: [][]fakeContainer{{ct}}}
	got, err := newCheck(f).Affected(context.Background(), "/srv/demo-app")
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestUnrelatedContainersAreNotAffected(t *testing.T) {
	web := healthy(idWeb, "demo-web-1")
	web.WorkDir = "/srv/demo-app-other"
	web.Mounts = []fakeMount{{"bind", "/srv/demo-app-other/data"}, {"bind", "/srv/demo"}, {"bind", "/srv"}, {"bind", "/srv/other"}, {"tmpfs", ""}, {"bind", "relative/demo-app"}}
	db := healthy(idDB, "demo-db-1")
	f := &fakeDocker{states: [][]fakeContainer{{web, db}}}
	got, err := newCheck(f).Affected(context.Background(), "/srv/demo-app")
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestOnlyTheReachedContainerIsRemembered(t *testing.T) {
	web := healthy(idWeb, "demo-web-1")
	web.Mounts = []fakeMount{{"bind", "/srv/demo-app/data"}}
	web.Restarts = 3
	web.Health = "unhealthy"
	db := healthy(idDB, "demo-db-1")
	f := &fakeDocker{states: [][]fakeContainer{{web, db}}}
	got, err := newCheck(f).Affected(context.Background(), "/srv/demo-app")
	if err != nil || len(got) != 1 || got[0].ID != idWeb || got[0].RestartCount != 3 || !got[0].Unhealthy {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestNamesFromDockerAreStripped(t *testing.T) {
	ct := healthy(idWeb, "demo\x1b[31m-web‮-1")
	ct.WorkDir = "/srv/demo-app"
	f := &fakeDocker{states: [][]fakeContainer{{ct}}}
	got, err := newCheck(f).Affected(context.Background(), "/srv/demo-app")
	if err != nil || len(got) != 1 || got[0].Name != "demo[31m-web-1" {
		t.Fatalf("%v %+v", err, got)
	}
}

func affectedWeb(t *testing.T, f *fakeDocker) (ContainerCheck, []ContainerState) {
	t.Helper()
	c := newCheck(f)
	got, err := c.Affected(context.Background(), "/srv/demo-app")
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	return c, got
}

func webAt(workDir string, mod func(*fakeContainer)) fakeContainer {
	ct := healthy(idWeb, "demo-web-1")
	ct.WorkDir = workDir
	mod(&ct)
	return ct
}

func TestWatchFindsWhatWentWrong(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*fakeContainer)
		want string
	}{
		{"restart", func(c *fakeContainer) { c.Restarts = 2 }, "container demo-web-1: the container restarted (restart count 2, was 0)"},
		{"stop", func(c *fakeContainer) { c.Status, c.Running = "exited", false }, "container demo-web-1: the container is not running (exited)"},
		{"restarting", func(c *fakeContainer) { c.Restarting = true }, "container demo-web-1: the container is restarting"},
		{"unhealthy", func(c *fakeContainer) { c.Health = "unhealthy" }, "container demo-web-1: the container became unhealthy"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeDocker{states: [][]fakeContainer{{webAt("/srv/demo-app", func(*fakeContainer) {})}, {webAt("/srv/demo-app", func(*fakeContainer) {})}, {webAt("/srv/demo-app", tc.mod)}}}
			c, affected := affectedWeb(t, f)
			err := c.Watch(context.Background(), affected)
			var finding *ContainerFinding
			if !errors.As(err, &finding) || err.Error() != tc.want {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestVanishedContainerIsAFinding(t *testing.T) {
	f := &fakeDocker{states: [][]fakeContainer{{webAt("/srv/demo-app", func(*fakeContainer) {})}, {}}}
	c, affected := affectedWeb(t, f)
	if err := c.Watch(context.Background(), affected); err == nil || err.Error() != "container demo-web-1: the container is gone" {
		t.Fatalf("%v", err)
	}
}

func TestContainerUnhealthyBeforeTheChangeIsNotBlamed(t *testing.T) {
	sick := webAt("/srv/demo-app", func(c *fakeContainer) { c.Health = "unhealthy" })
	f := &fakeDocker{states: [][]fakeContainer{{sick}}}
	c, affected := affectedWeb(t, f)
	if err := c.Watch(context.Background(), affected); err != nil {
		t.Fatalf("%v", err)
	}
}

func TestHealthyContainersPassTheWholeWindow(t *testing.T) {
	f := &fakeDocker{states: [][]fakeContainer{{webAt("/srv/demo-app", func(*fakeContainer) {})}}}
	c, affected := affectedWeb(t, f)
	if err := c.Watch(context.Background(), affected); err != nil {
		t.Fatal(err)
	}
	if f.inspects < 3 {
		t.Errorf("looked %d times", f.inspects)
	}
}

func TestWithoutContainersThereIsNothingToObserve(t *testing.T) {
	f := &fakeDocker{states: [][]fakeContainer{{}}}
	c := ContainerCheck{Exec: f, Window: time.Hour, Interval: time.Hour}
	got, err := c.Affected(context.Background(), "/srv/demo-app")
	if err != nil || len(got) != 0 || f.inspects != 0 {
		t.Fatalf("%v %+v %d", err, got, f.inspects)
	}
	start := time.Now()
	if err := c.Watch(context.Background(), got); err != nil || time.Since(start) > time.Second || f.inspects != 0 || f.pses != 1 {
		t.Fatalf("%v inspects %d pses %d", err, f.inspects, f.pses)
	}
}

func TestPathRelations(t *testing.T) {
	for p, want := range map[string]bool{
		"/srv/demo-app": true, "/srv/demo-app/a/b": true, "/srv/demo-app2": false, "/srv/demo-app-other": false,
		"/srv": false, "relative": false, "": false,
	} {
		if got := pathUnder("/srv/demo-app", p); got != want {
			t.Errorf("pathUnder %q = %v", p, got)
		}
	}
}
