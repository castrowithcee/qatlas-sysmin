package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/gitrepo"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry"
)

type fakeCreds map[string]registry.Credential

func (f fakeCreds) Registry(name string) (registry.Credential, error) {
	c, ok := f[name]
	if !ok {
		return registry.Credential{}, errors.New("no such credential")
	}
	return c, nil
}

func (f fakeCreds) Git(string) (gitrepo.Credential, error) {
	return gitrepo.Credential{}, errors.New("no such credential")
}

func registryServer(t *testing.T, wantAuth string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, _ := r.BasicAuth(); wantAuth != "" && user+":"+pass != wantAuth {
			w.Header().Set("WWW-Authenticate", `Basic realm="demo"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/tags/list") {
			json.NewEncoder(w).Encode(map[string]any{"tags": []string{"1.0.0", "1.1.0", "2.0.0", "latest"}})
			return
		}
		// Manifests without a digest header and no config: digest and labels are reported as problems.
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// demoToken is a made-up registry token.
const demoToken = "demo-registry-token-not-real"

func TestCheckImage(t *testing.T) {
	srv := registryServer(t, "demo-user:demo-registry-token-not-real")
	c := &Checker{
		Credentials: fakeCreds{"hub": {User: "demo-user", Token: demoToken}},
		Registry:    registry.Options{BaseURL: func(string) string { return srv.URL }},
	}
	img := config.Image{Name: "app", Ref: "registry.example.invalid/team/app", Credential: "hub",
		VersionRule: config.VersionRule{MaxUpdate: "minor"}}
	res, err := c.CheckImage(context.Background(), img, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != "1.1.0" || fmt.Sprint(res.Available) != "[1.1.0 2.0.0]" {
		t.Errorf("target %q available %v", res.Target, res.Available)
	}
	var codes []string
	for _, p := range res.Problems {
		codes = append(codes, p.Code)
	}
	if fmt.Sprint(codes) != fmt.Sprint([]string{CodeDigestUnavailable, CodeLabelsUnavailable}) {
		t.Errorf("problem codes = %v", codes)
	}
}

func TestCheckImageErrors(t *testing.T) {
	srv := registryServer(t, "demo-user:demo-registry-token-not-real")
	opts := registry.Options{BaseURL: func(string) string { return srv.URL }}
	img := config.Image{Ref: "registry.example.invalid/team/app"}

	if _, err := (&Checker{Registry: opts}).CheckImage(context.Background(), img, "1.0.0"); !errors.Is(err, registry.ErrDenied) {
		t.Errorf("anonymous: err = %v, want ErrDenied", err)
	}
	img.Credential = "missing"
	if _, err := (&Checker{Registry: opts}).CheckImage(context.Background(), img, "1.0.0"); err == nil {
		t.Error("credential without store: want an error")
	}
	if _, err := (&Checker{Credentials: fakeCreds{}, Registry: opts}).CheckImage(context.Background(), img, "1.0.0"); err == nil {
		t.Error("unknown credential: want an error")
	}
	if _, err := (&Checker{}).CheckImage(context.Background(), config.Image{Ref: "bad ref"}, "1.0.0"); err == nil {
		t.Error("invalid ref: want an error")
	}
}

func TestCheckImageFloatingTag(t *testing.T) {
	srv := registryServer(t, "")
	c := &Checker{Registry: registry.Options{BaseURL: func(string) string { return srv.URL }}}
	res, err := c.CheckImage(context.Background(), config.Image{Ref: "registry.example.invalid/team/app"}, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 || res.Problems[0].Code != "floating-tag" || res.Target != "" {
		t.Errorf("got %+v", res)
	}
}

func TestCheckGitSelectsVersion(t *testing.T) {
	const out = "aaa\trefs/tags/v1.0.0\nbbb\trefs/tags/v1.1.0\nccc\trefs/tags/v2.0.0\nddd\trefs/tags/nightly\n"
	c := &Checker{Git: gitrepo.Options{Run: func(cmd *exec.Cmd) error {
		_, err := cmd.Stdout.Write([]byte(out))
		return err
	}}}
	g := config.GitSource{URL: "https://git.example.invalid/team/app.git", VersionRule: config.VersionRule{MaxUpdate: "minor"}}
	res, err := c.CheckGit(context.Background(), g, "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != "v1.1.0" || fmt.Sprint(res.Available) != "[v1.1.0 v2.0.0]" {
		t.Errorf("target %q available %v", res.Target, res.Available)
	}
	g.Credential = "missing"
	if _, err := c.CheckGit(context.Background(), g, "v1.0.0"); err == nil {
		t.Error("a credential without a store was accepted")
	}
}
