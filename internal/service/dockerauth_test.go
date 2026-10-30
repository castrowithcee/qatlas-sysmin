package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
)

type dockerCall struct {
	env   []string
	dir   string
	mode  os.FileMode
	file  os.FileMode
	conf  []byte
	found bool
}

func captureDocker(calls *[]dockerCall) capture {
	return capture{&deployRunner{}, func(c execute.Command) {
		var call dockerCall
		call.env = c.Env
		for _, e := range c.Env {
			if v, ok := strings.CutPrefix(e, "DOCKER_CONFIG="); ok {
				call.found, call.dir = true, v
				if st, err := os.Stat(v); err == nil {
					call.mode = st.Mode().Perm()
				}
				if st, err := os.Stat(filepath.Join(v, "config.json")); err == nil {
					call.file = st.Mode().Perm()
				}
				call.conf, _ = os.ReadFile(filepath.Join(v, "config.json"))
			}
		}
		*calls = append(*calls, call)
	}}
}

func demoAuth() string { return base64.StdEncoding.EncodeToString([]byte("demo-user:" + demoSecret)) }

func TestDockerAuthRunnerGivesADockerCallItsLoginForOneCall(t *testing.T) {
	var calls []dockerCall
	runner := &dockerAuthRunner{
		inner: captureDocker(&calls),
		auths: map[string]string{"registry.example.invalid": demoAuth(), dockerHubAuthKey: demoAuth()},
	}
	pre := []string{"A=b", "DOCKER_CONFIG=/home/demo/.docker"}
	if _, err := runner.Run(context.Background(), execute.Command{Name: "docker", Args: []string{"compose", "pull"}, Env: pre}); err != nil {
		t.Fatal(err)
	}
	c := calls[0]
	if !c.found || c.mode != 0o700 || c.file != 0o600 {
		t.Fatalf("call = %+v", c)
	}
	var conf struct {
		Auths map[string]struct{ Auth string }
	}
	if err := json.Unmarshal(c.conf, &conf); err != nil || len(conf.Auths) != 2 ||
		conf.Auths["registry.example.invalid"].Auth != demoAuth() || conf.Auths["https://index.docker.io/v1/"].Auth != demoAuth() {
		t.Fatalf("config = %s %v", c.conf, err)
	}
	if _, err := os.Stat(c.dir); !os.IsNotExist(err) {
		t.Errorf("the config outlives the call: %v", err)
	}
	// The login wins over a DOCKER_CONFIG that the call already carried.
	if last := c.env[len(c.env)-1]; last != "DOCKER_CONFIG="+c.dir {
		t.Errorf("env = %v", c.env)
	}
	if strings.Contains(string(c.conf), demoSecret) {
		t.Errorf("the raw token is in the config")
	}

	// Other programs stay unchanged.
	calls = nil
	_, _ = runner.Run(context.Background(), execute.Command{Name: "git", Env: []string{"A=b"}})
	if len(calls) != 1 || len(calls[0].env) != 1 || calls[0].found {
		t.Errorf("git call = %+v", calls)
	}
}

func TestDockerAuthRunnerWithoutCredentialSetsNothing(t *testing.T) {
	var calls []dockerCall
	runner := &dockerAuthRunner{inner: captureDocker(&calls)}
	_, _ = runner.Run(context.Background(), execute.Command{Name: "docker", Env: []string{"DOCKER_CONFIG=/home/demo/.docker"}})
	if len(calls) != 1 || len(calls[0].env) != 1 || calls[0].env[0] != "DOCKER_CONFIG=/home/demo/.docker" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestExecutorDockerLoginKeepsSecretsOutOfTheLog(t *testing.T) {
	f := newDeployFixture(t, true)
	var calls []dockerCall
	f.svc.cfg.DeployRunner = captureDocker(&calls)
	e, err := f.svc.vault.GetByName("demo-registry")
	if err != nil {
		t.Fatal(err)
	}
	ap := approval.Approved{
		Credentials: map[string]string{"demo-registry": e.ID},
		Project: config.Project{Images: []config.Image{
			{Name: "app", Ref: "registry.example.invalid/demo/app", Credential: "demo-registry"},
			{Name: "db", Ref: "demo/db", Credential: "demo-registry"},
			{Name: "cache", Ref: "registry.other.invalid/demo/cache"},
		}},
	}
	ex, err := f.svc.executor(ap)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	ex.Log = &log
	ex.Runner = printing{ex.Runner, demoSecret + " " + demoAuth()}
	plan := config.Plan{Project: "demo-app", Steps: []config.ResolvedStep{{Type: config.StepComposePull, Dir: t.TempDir(), Timeout: time.Minute}}}
	rep, err := ex.Execute(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || !calls[0].found {
		t.Fatalf("calls = %+v", calls)
	}
	var conf struct{ Auths map[string]any }
	if err := json.Unmarshal(calls[0].conf, &conf); err != nil || len(conf.Auths) != 2 {
		t.Fatalf("hosts = %s %v", calls[0].conf, err)
	}
	for _, k := range []string{"registry.example.invalid", dockerHubAuthKey} {
		if _, ok := conf.Auths[k]; !ok {
			t.Errorf("host %q is missing in %s", k, calls[0].conf)
		}
	}
	text := log.String() + rep.Steps[0].Output
	if strings.Contains(text, demoSecret) || strings.Contains(text, demoAuth()) || !strings.Contains(text, "[redacted]") {
		t.Errorf("log = %s", text)
	}
}

// printing makes every call print the given text, as a failing docker call might.
type printing struct {
	execute.Runner
	out string
}

func (p printing) Run(ctx context.Context, c execute.Command) (execute.Result, error) {
	res, err := p.Runner.Run(ctx, c)
	res.Output = []byte(p.out)
	return res, err
}

func TestDockerAuthsRefuseTwoCredentialsForOneRegistry(t *testing.T) {
	f := newDeployFixture(t, true)
	e, _ := f.svc.vault.GetByName("demo-registry")
	creds := &approvedCredentials{vault: f.svc.vault, ids: map[string]string{"one": e.ID, "two": e.ID}}
	_, err := dockerAuths([]config.Image{
		{Ref: "registry.example.invalid/a", Credential: "one"}, {Ref: "registry.example.invalid/b", Credential: "two"}}, creds)
	if err == nil || strings.Contains(err.Error(), demoSecret) {
		t.Errorf("err = %v", err)
	}
}
