package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry/registrytest"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const (
	changelogToken = "test-token-not-real"
	githubLabel    = "https://github.com/example-org/example-app"
)

// fakeGitHub serves the releases of example-org/example-app and records the Authorization headers.
type fakeGitHub struct {
	*httptest.Server
	mu     sync.Mutex
	status int
	auth   []string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	g := &fakeGitHub{}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.auth = append(g.auth, r.Header.Get("Authorization"))
		if g.status != 0 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(g.status)
			return
		}
		if r.URL.Path != "/repos/example-org/example-app/releases" {
			http.NotFound(w, r)
			return
		}
		var out []map[string]any
		for _, tag := range []string{"v1.5.0", "v1.4.0", "v1.3.0", "v1.2.4", "v1.2.3"} {
			out = append(out, map[string]any{"tag_name": tag, "name": "Release " + tag, "body": "notes of " + tag,
				"html_url":     "https://github.com/example-org/example-app/releases/tag/" + tag,
				"published_at": "2026-01-02T03:04:05Z"})
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGitHub) setStatus(status int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.status = status
}

func (g *fakeGitHub) authHeaders() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.auth...)
}

// newChangelogFixture starts a service with the project demo-app, whose image app runs 1.2.3 and has the
// registry versions 1.2.3, 1.3.0 and 1.4.0. rule is appended to the image as its version rule.
func newChangelogFixture(t *testing.T, label, rule string) (*sourceFixture, *fakeGitHub) {
	t.Helper()
	gh := newFakeGitHub(t)
	reg := registrytest.New(t, registrytest.Config{User: "demo-user", Token: demoSecret,
		Labels: map[string]string{registry.LabelSource: label},
		Repos:  map[string][]string{"demo/app": {"1.2.3", "1.3.0", "1.4.0"}}})
	f := newSourceFixture(t, demoSecret, func(cfg *Config) {
		cfg.SourceRegistry.BaseURL = func(string) string { return reg.URL }
		cfg.Changelog.BaseURL = gh.URL
		cfg.Changelog.CacheTTL = time.Nanosecond
	})
	if _, err := f.svc.vault.Add("gh-token", vault.KindSecret, map[string]string{vault.FieldValue: changelogToken}); err != nil {
		t.Fatal(err)
	}
	f.write(t, "projects/demo-app.yaml", imageProject(f.dir, "registry.example.com/demo/app")+rule)
	if err := os.WriteFile(filepath.Join(f.dir, ".env"), []byte("DEMO_APP_TAG=1.2.3\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	f.approve(t)
	return f, gh
}

func releaseVersions(s control.ChangelogSource) string {
	var v []string
	for _, r := range s.Releases {
		v = append(v, r.Version)
	}
	return fmt.Sprint(v)
}

func noChangelogToken(t *testing.T, what, text string) {
	t.Helper()
	if strings.Contains(text, changelogToken) {
		t.Errorf("%s contains the token: %s", what, text)
	}
}

func TestChangelogUsesTheImageLabelAndTheToken(t *testing.T) {
	f, gh := newChangelogFixture(t, githubLabel, "    version_rule:\n      changelog_credential: gh-token\n")
	res, err := f.dial(t).Changelog("demo-app", "", "")
	if err != nil || len(res.Sources) != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	s := res.Sources[0]
	if s.Error != "" || s.Repo != "example-org/example-app" || s.Current != "1.2.3" || s.Target != "1.4.0" ||
		releaseVersions(s) != "[1.4.0 1.3.0 1.2.4]" || s.Releases[0].Body != "notes of v1.4.0" || s.Stale {
		t.Fatalf("source = %+v", s)
	}
	for _, h := range gh.authHeaders() {
		if h != "Bearer "+changelogToken {
			t.Errorf("Authorization = %q", h)
		}
	}
	if len(gh.authHeaders()) == 0 {
		t.Fatal("GitHub was not asked")
	}
	noChangelogToken(t, "result", fmt.Sprintf("%+v", res))
	log := f.logText(t)
	noChangelogToken(t, "log", log)
	if !strings.Contains(log, `"action":"credential.use"`) {
		t.Errorf("log lacks the entry: %s", log)
	}
}

func TestChangelogUsesTheRegisteredRepository(t *testing.T) {
	f, gh := newChangelogFixture(t, "https://example.invalid/other/app", "    version_rule:\n      changelog_repo: example-org/example-app\n")
	res, err := f.dial(t).Changelog("demo-app", "", "")
	if err != nil || len(res.Sources) != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if s := res.Sources[0]; s.Error != "" || s.Repo != "example-org/example-app" || releaseVersions(s) != "[1.4.0 1.3.0 1.2.4]" {
		t.Fatalf("source = %+v", s)
	}
	for _, h := range gh.authHeaders() {
		if h != "" {
			t.Errorf("a token was sent without a changelog_credential: %q", h)
		}
	}
}

func TestChangelogOtherHostYieldsOnlyALink(t *testing.T) {
	f, gh := newChangelogFixture(t, "https://example.invalid/other/app", "")
	res, err := f.dial(t).Changelog("demo-app", "", "")
	if err != nil || len(res.Sources) != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if s := res.Sources[0]; s.Error != "" || len(s.Releases) != 0 || s.Link != "https://example.invalid/other/app" || s.Repo != "" {
		t.Fatalf("source = %+v", s)
	}
	if n := len(gh.authHeaders()); n != 0 {
		t.Fatalf("GitHub was asked %d times", n)
	}
}

func TestChangelogImageFilterAndTarget(t *testing.T) {
	f, _ := newChangelogFixture(t, githubLabel, "")
	two := imageProject(f.dir, "registry.example.com/demo/app") +
		"  - name: second\n    ref: registry.example.com/demo/app\n    env_var: DEMO_SECOND_TAG\n    services: [second]\n    credential: demo-registry\n"
	f.write(t, "projects/demo-app.yaml", two)
	if err := os.WriteFile(filepath.Join(f.dir, ".env"), []byte("DEMO_APP_TAG=1.2.3\nDEMO_SECOND_TAG=1.3.0\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	f.approve(t)

	res, err := f.dial(t).Changelog("demo-app", "", "")
	if err != nil || len(res.Sources) != 2 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	res, err = f.dial(t).Changelog("demo-app", "second", "")
	if err != nil || len(res.Sources) != 1 || res.Sources[0].Name != "second" || releaseVersions(res.Sources[0]) != "[1.4.0]" {
		t.Fatalf("filtered = %+v, %v", res, err)
	}
	res, err = f.dial(t).Changelog("demo-app", "app", "1.3.0")
	if err != nil || len(res.Sources) != 1 || res.Sources[0].Target != "1.3.0" || releaseVersions(res.Sources[0]) != "[1.3.0 1.2.4]" {
		t.Fatalf("target = %+v, %v", res, err)
	}
	for _, bad := range [][2]string{{"nope", ""}, {"app", "../x"}, {"app", "latest"}, {"bad name", ""}, {"app", strings.Repeat("1", 80) + ".0.0"}} {
		_, err := f.dial(t).Changelog("demo-app", bad[0], bad[1])
		wantCode(t, err, control.CodeBadRequest)
	}
}

func TestChangelogRateLimitUsesTheCache(t *testing.T) {
	f, gh := newChangelogFixture(t, githubLabel, "")
	if res, err := f.dial(t).Changelog("demo-app", "", ""); err != nil || res.Sources[0].Error != "" {
		t.Fatalf("first = %+v, %v", res, err)
	}
	gh.setStatus(http.StatusTooManyRequests)
	res, err := f.dial(t).Changelog("demo-app", "", "")
	if err != nil || len(res.Sources) != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if s := res.Sources[0]; s.Error != "" || !s.Stale || s.Notice == "" || releaseVersions(s) != "[1.4.0 1.3.0 1.2.4]" {
		t.Fatalf("source = %+v", s)
	}
}

func TestChangelogRateLimitWithoutCacheIsACode(t *testing.T) {
	f, gh := newChangelogFixture(t, githubLabel, "    version_rule:\n      changelog_credential: gh-token\n")
	gh.setStatus(http.StatusTooManyRequests)
	res, err := f.dial(t).Changelog("demo-app", "", "")
	if err != nil || len(res.Sources) != 1 {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if s := res.Sources[0]; s.Error != control.ChangelogRateLimited || len(s.Releases) != 0 || s.Message == "" {
		t.Fatalf("source = %+v", s)
	}
	noChangelogToken(t, "result", fmt.Sprintf("%+v", res))
	noChangelogToken(t, "log", f.logText(t))
}

func TestChangelogCredentialMustBeApprovedAndASecret(t *testing.T) {
	// A credential of the wrong kind.
	f, gh := newChangelogFixture(t, githubLabel, "    version_rule:\n      changelog_credential: demo-registry\n")
	res, err := f.dial(t).Changelog("demo-app", "", "")
	if err != nil || len(res.Sources) != 1 || res.Sources[0].Error != control.SourceCredential {
		t.Fatalf("wrong kind: %+v, %v", res, err)
	}
	noSecret(t, "result", fmt.Sprintf("%+v", res))

	// A credential replaced after the approval is not the approved one.
	f, gh = newChangelogFixture(t, githubLabel, "    version_rule:\n      changelog_credential: gh-token\n")
	old, err := f.svc.vault.GetByName("gh-token")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.vault.Delete(old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.vault.Add("gh-token", vault.KindSecret, map[string]string{vault.FieldValue: "replaced-token-not-real"}); err != nil {
		t.Fatal(err)
	}
	res, err = f.dial(t).Changelog("demo-app", "", "")
	if err != nil || len(res.Sources) != 1 || res.Sources[0].Error != control.SourceCredential {
		t.Fatalf("replaced: %+v, %v", res, err)
	}
	for _, h := range gh.authHeaders() {
		if strings.Contains(h, "replaced-token-not-real") || strings.Contains(h, changelogToken) {
			t.Errorf("a token was sent: %q", h)
		}
	}
	noChangelogToken(t, "result", fmt.Sprintf("%+v", res))
	noChangelogToken(t, "log", f.logText(t))
}

func TestChangelogNeedsAnApprovedState(t *testing.T) {
	f := newSourceFixture(t, demoSecret)
	wantCode(t, func() error { _, err := f.dial(t).Changelog("demo-app", "", ""); return err }(), control.CodeNotApproved)
	wantCode(t, func() error { _, err := f.dial(t).Changelog("../x", "", ""); return err }(), control.CodeBadRequest)
}
