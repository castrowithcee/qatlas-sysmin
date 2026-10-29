package execute

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// inspectFake answers compose ps with the ids and inspect from states(ref, callNumber).
func inspectFake(ids string, states func(ref string, n int) string) *fakeRunner {
	counts := map[string]int{}
	fr := &fakeRunner{}
	fr.handle = func(c Command) (Result, error) {
		if len(c.Args) > 0 && c.Args[0] == "compose" {
			return Result{Output: []byte(ids)}, nil
		}
		ref := c.Args[len(c.Args)-1]
		counts[ref]++
		return Result{Output: []byte(states(ref, counts[ref]) + "\n")}, nil
	}
	return fr
}

func checkStep(typ config.StepType, d time.Duration) config.ResolvedStep {
	return config.ResolvedStep{Type: typ, Timeout: d, Dir: "/srv/example/shop"}
}

func runCheck(ex *Executor, st config.ResolvedStep) (StepResult, error) {
	ex.PollInterval = time.Millisecond
	rep, err := ex.Execute(context.Background(), config.Plan{Project: "shop", Steps: []config.ResolvedStep{st}})
	return rep.Steps[0], err
}

func TestCheckContainerStates(t *testing.T) {
	cases := []struct {
		name    string
		states  func(ref string, n int) string
		timeout time.Duration
		errHas  string
	}{
		{"healthy", func(string, int) string { return "/a running healthy" }, time.Second, ""},
		{"running without healthcheck", func(string, int) string { return "/a running none" }, time.Second, ""},
		{"starting then healthy", func(_ string, n int) string {
			if n < 3 {
				return "/a running starting"
			}
			return "/a running healthy"
		}, time.Second, ""},
		{"unhealthy until timeout", func(string, int) string { return "/a running unhealthy" }, 50 * time.Millisecond, "a=running/unhealthy"},
		{"exited at once", func(string, int) string { return "/a exited none" }, time.Minute, "a=exited"},
		{"dead at once", func(string, int) string { return "/a dead none" }, time.Minute, "a=dead"},
	}
	for _, c := range cases {
		start := time.Now()
		res, err := runCheck(&Executor{Runner: inspectFake("id1\n", c.states)}, checkStep(config.StepCheckContainer, c.timeout))
		if c.errHas == "" {
			if err != nil || res.Status != StatusOK {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.errHas) {
			t.Errorf("%s: err=%v", c.name, err)
		}
		if time.Since(start) > 10*time.Second {
			t.Errorf("%s: waited too long", c.name)
		}
	}
	if _, err := runCheck(&Executor{Runner: inspectFake("id1\n", func(string, int) string { return "/a running unhealthy" })}, checkStep(config.StepCheckContainer, 30*time.Millisecond)); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("timeout text: %v", err)
	}
}

func TestCheckContainerNoneFound(t *testing.T) {
	fr := inspectFake("\n", nil)
	_, err := runCheck(&Executor{Runner: fr}, checkStep(config.StepCheckContainer, time.Minute))
	if err == nil || !strings.Contains(err.Error(), "no containers") || len(fr.calls) != 1 {
		t.Errorf("err=%v calls=%d", err, len(fr.calls))
	}
}

func TestCheckContainerComposeArgsAndAllContainers(t *testing.T) {
	fr := inspectFake("id1\n", func(string, int) string { return "/x running healthy" })
	st := checkStep(config.StepCheckContainer, time.Second)
	st.ComposeFiles = []string{"/a.yml", "/b.yml"}
	st.ComposeProject = "shop"
	st.Services = []string{"app", "db"}
	if _, err := runCheck(&Executor{Runner: fr}, st); err != nil {
		t.Fatal(err)
	}
	for i, svc := range []string{"app", "db"} {
		want := []string{"compose", "-f", "/a.yml", "-f", "/b.yml", "-p", "shop", "ps", "-a", "-q", svc}
		if !reflect.DeepEqual(fr.calls[i].Args, want) || fr.calls[i].Dir != "/srv/example/shop" {
			t.Errorf("call %d: %+v", i, fr.calls[i])
		}
	}
	if len(fr.calls) != 3 { // two ps calls, one inspect: both services share one container
		t.Errorf("calls %+v", fr.calls)
	}
	// Without services the compose call names none, which means all of the project.
	fr = inspectFake("id1\n", func(string, int) string { return "/x running none" })
	if _, err := runCheck(&Executor{Runner: fr}, checkStep(config.StepCheckContainer, time.Second)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fr.calls[0].Args, []string{"compose", "ps", "-a", "-q"}) {
		t.Errorf("calls %+v", fr.calls[0])
	}
}

func TestCheckContainerServiceWithoutContainer(t *testing.T) {
	st := checkStep(config.StepCheckContainer, time.Second)
	st.Services = []string{"app", "db"}
	fr := &fakeRunner{handle: func(c Command) (Result, error) {
		if c.Args[0] == "compose" {
			if c.Args[len(c.Args)-1] == "app" {
				return Result{Output: []byte("id1\n")}, nil
			}
			return Result{}, nil
		}
		return Result{Output: []byte("/app running healthy\n")}, nil
	}}
	_, err := runCheck(&Executor{Runner: fr}, st)
	if err == nil || !strings.Contains(err.Error(), "service db") {
		t.Errorf("err=%v", err)
	}
	for _, c := range fr.calls {
		if c.Args[0] != "compose" {
			t.Errorf("inspect ran before all services were found: %+v", c)
		}
	}
	fr2 := inspectFake("id1\n", func(string, int) string { return "/x running healthy" })
	fr2.handle = func(c Command) (Result, error) {
		if c.Args[0] == "compose" {
			return Result{Output: []byte("id-" + c.Args[len(c.Args)-1] + "\n")}, nil
		}
		return Result{Output: []byte("/x running healthy\n")}, nil
	}
	if _, err := runCheck(&Executor{Runner: fr2}, st); err != nil || len(fr2.calls) != 4 {
		t.Errorf("err=%v calls=%d", err, len(fr2.calls))
	}
}

func TestCheckContainerNamed(t *testing.T) {
	fr := inspectFake("", func(string, int) string { return "/web running healthy" })
	st := checkStep(config.StepCheckContainer, time.Second)
	st.Containers = []string{"web"}
	if _, err := runCheck(&Executor{Runner: fr}, st); err != nil {
		t.Fatal(err)
	}
	if len(fr.calls) != 1 || fr.calls[0].Args[len(fr.calls[0].Args)-1] != "web" {
		t.Errorf("calls %+v", fr.calls)
	}
	st.Containers = []string{"--all"}
	if _, err := runCheck(&Executor{Runner: fr}, st); err == nil {
		t.Error("option-like name accepted")
	}
}

func TestCheckContainerDryRun(t *testing.T) {
	st := checkStep(config.StepCheckContainer, time.Second)
	st.Services = []string{"app"}
	acts, err := describeCheckContainer(describeCtx{}, st)
	if err != nil || !strings.HasPrefix(acts[0], "docker compose ps -a -q app") || len(acts) != 2 {
		t.Errorf("%v %v", acts, err)
	}
}

func httpStep(url string, status int, contains string, d time.Duration) config.ResolvedStep {
	return config.ResolvedStep{Type: config.StepCheckHTTP, Timeout: d, URL: url, Status: status, Contains: contains}
}

func TestCheckHTTP(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("hello example"))
		case "/created":
			w.WriteHeader(201)
		case "/late":
			if hits.Add(1) < 3 {
				w.WriteHeader(503)
				return
			}
			_, _ = w.Write([]byte("up"))
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		default:
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	ex := &Executor{Runner: &fakeRunner{}, HTTPClient: srv.Client()}

	for _, c := range []struct {
		name string
		st   config.ResolvedStep
		err  string
	}{
		{"default status", httpStep(srv.URL+"/ok", 0, "", time.Second), ""},
		{"contains", httpStep(srv.URL+"/ok", 200, "example", time.Second), ""},
		{"other status", httpStep(srv.URL+"/created", 201, "", time.Second), ""},
		{"waits for success", httpStep(srv.URL+"/late", 200, "up", 5*time.Second), ""},
		{"wrong status", httpStep(srv.URL+"/created", 200, "", 40*time.Millisecond), "status 201, want 200"},
		{"missing text", httpStep(srv.URL+"/ok", 200, "absent", 40*time.Millisecond), `does not contain "absent"`},
		{"last status 500", httpStep(srv.URL+"/boom", 200, "", 40*time.Millisecond), "status 500"},
		{"redirect not followed", httpStep(srv.URL+"/redirect", 200, "", 40*time.Millisecond), "status 302"},
		{"redirect expected", httpStep(srv.URL+"/redirect", 302, "", time.Second), ""},
		{"bad url", httpStep("ftp://example.com", 200, "", time.Second), "not an http"},
	} {
		_, err := runCheck(ex, c.st)
		if c.err == "" && err != nil || c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s: err=%v", c.name, err)
		}
	}
	if hits.Load() != 3 {
		t.Errorf("late endpoint hit %d times", hits.Load())
	}
}

func TestCheckHTTPUnreachableAndDryRun(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := runCheck(&Executor{Runner: &fakeRunner{}}, httpStep(url, 200, "", 50*time.Millisecond))
	if err == nil || !strings.Contains(err.Error(), "request failed") {
		t.Errorf("err=%v", err)
	}
	acts, err := describeCheckHTTP(describeCtx{}, httpStep("https://example.com/health", 0, "ok", time.Second))
	if err != nil || acts[0] != `GET https://example.com/health expect 200 containing "ok"` {
		t.Errorf("%v %v", acts, err)
	}
}
