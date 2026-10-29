package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	loginToken  = "demo-registry-token-not-real"
	wrongToken  = "wrong-value-not-real"
	bearerToken = "demo-bearer-value-not-real"
)

// fakeRegistry is a distribution API server with an optional bearer-token flow.
type fakeRegistry struct {
	t        *testing.T
	srv      *httptest.Server
	cdn      *httptest.Server
	blobs    map[string][]byte // path on the CDN -> content
	needAuth bool              // require a bearer token
	basic    bool              // require basic credentials instead
	user     string            // accepted user for the token service and basic
	mu       sync.Mutex
	hosts    []string // Host headers seen on the registry
	tokenReq []url.Values
	paths    []string
	labels   map[string]string
	index    bool // serve a multi-platform index
}

func newFake(t *testing.T) *fakeRegistry {
	f := &fakeRegistry{t: t, user: "demo-user", labels: map[string]string{LabelSource: "https://example.invalid/team/app"}}
	f.blobs = map[string][]byte{}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	// Blobs are served from a second host, as a CDN would.
	f.cdn = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "authorization leaked to CDN", http.StatusBadRequest)
			return
		}
		w.Write(f.blobs[r.URL.Path])
	}))
	t.Cleanup(f.srv.Close)
	t.Cleanup(f.cdn.Close)
	return f
}

func (f *fakeRegistry) client(cred Credential, extra ...func(*Options)) *Client {
	o := Options{
		Credential: cred,
		BaseURL: func(host string) string {
			f.mu.Lock()
			f.hosts = append(f.hosts, host)
			f.mu.Unlock()
			return f.srv.URL
		},
		Platform: Platform{OS: "linux", Arch: "amd64"},
	}
	for _, e := range extra {
		e(&o)
	}
	return New(o)
}

func (f *fakeRegistry) token(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.tokenReq = append(f.tokenReq, r.URL.Query())
	f.mu.Unlock()
	if f.user != "" {
		u, p, ok := r.BasicAuth()
		if !ok || u != f.user || p != loginToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	json.NewEncoder(w).Encode(map[string]string{"token": bearerToken})
}

func (f *fakeRegistry) authorized(w http.ResponseWriter, r *http.Request, scope string) bool {
	switch {
	case f.basic:
		u, p, ok := r.BasicAuth()
		if ok && u == f.user && p == loginToken {
			return true
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="demo"`)
	case f.needAuth:
		if r.Header.Get("Authorization") == "Bearer "+bearerToken {
			return true
		}
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="demo.registry",scope="%s"`, f.srv.URL, scope))
	default:
		return true
	}
	w.WriteHeader(http.StatusUnauthorized)
	return false
}

func sum(b []byte) string { s := sha256.Sum256(b); return fmt.Sprintf("sha256:%x", s[:]) }

func (f *fakeRegistry) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.paths = append(f.paths, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	if r.URL.Path == "/token" {
		f.token(w, r)
		return
	}
	name, rest, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v2/"), "/tags/")
	scope := "repository:" + name + ":pull"
	switch {
	case strings.Contains(r.URL.Path, "/tags/list") && ok && rest == "list":
		if !f.authorized(w, r, scope) {
			return
		}
		f.tagPage(w, r)
	case strings.Contains(r.URL.Path, "/manifests/"):
		n, ref, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v2/"), "/manifests/")
		if !f.authorized(w, r, "repository:"+n+":pull") {
			return
		}
		f.manifest(w, r, ref)
	case strings.Contains(r.URL.Path, "/blobs/"):
		n, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v2/"), "/blobs/")
		if !f.authorized(w, r, "repository:"+n+":pull") {
			return
		}
		http.Redirect(w, r, f.cdn.URL+"/cdn"+strings.TrimPrefix(r.URL.Path, "/v2"), http.StatusTemporaryRedirect)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeRegistry) tagPage(w http.ResponseWriter, r *http.Request) {
	pages := [][]string{{"1.0.0", "1.1.0"}, {"1.2.0", "latest"}, {"2.0.0"}}
	p := 0
	fmt.Sscan(r.URL.Query().Get("page"), &p)
	if p < len(pages)-1 {
		// Alternate relative and absolute links.
		link := fmt.Sprintf("/v2/%s/tags/list?page=%d", strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v2/"), "/tags/list"), p+1)
		if p == 1 {
			link = f.srv.URL + link
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="next"`, link))
	}
	json.NewEncoder(w).Encode(map[string]any{"name": "x", "tags": pages[p]})
}

func (f *fakeRegistry) manifest(w http.ResponseWriter, r *http.Request, ref string) {
	cfg, _ := json.Marshal(map[string]any{"config": map[string]any{"Labels": f.labels}})
	cfgDigest := sum(cfg)
	image, _ := json.Marshal(map[string]any{
		"mediaType": mediaOCIManifest,
		"config":    map[string]any{"digest": cfgDigest},
	})
	f.blobs["/cdn/team/app/blobs/"+cfgDigest] = cfg
	imageDigest := sum(image)
	if f.index && !strings.HasPrefix(ref, "sha256:") {
		idx, _ := json.Marshal(map[string]any{
			"mediaType": mediaOCIIndex,
			"manifests": []map[string]any{
				{"digest": "sha256:aaaa", "platform": map[string]string{"os": "unknown", "architecture": "unknown"}},
				{"digest": "sha256:bbbb", "platform": map[string]string{"os": "linux", "architecture": "arm64"}},
				{"digest": imageDigest, "platform": map[string]string{"os": "linux", "architecture": "amd64"}},
			},
		})
		w.Header().Set("Docker-Content-Digest", sum(idx))
		w.Header().Set("Content-Type", mediaOCIIndex)
		w.Write(idx)
		return
	}
	if ref != "1.2.0" && ref != imageDigest {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Docker-Content-Digest", imageDigest)
	w.Header().Set("Content-Type", mediaOCIManifest)
	if r.Method != http.MethodHead {
		w.Write(image)
	}
}

func repo() Repo { return Repo{Host: "registry.example.invalid", Name: "team/app"} }

func TestTagsPaginationAndBearerFlow(t *testing.T) {
	f := newFake(t)
	f.needAuth = true
	tags, err := f.client(Credential{User: f.user, Token: loginToken}).Tags(context.Background(), repo())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(tags, ","), "1.0.0,1.1.0,1.2.0,latest,2.0.0"; got != want {
		t.Errorf("tags = %s, want %s", got, want)
	}
	if len(f.tokenReq) != 1 {
		t.Fatalf("token requests = %d, want 1 (the token is reused across pages)", len(f.tokenReq))
	}
	q := f.tokenReq[0]
	if q.Get("service") != "demo.registry" || q.Get("scope") != "repository:team/app:pull" {
		t.Errorf("token request lacks service/scope: %v", q)
	}
}

func TestBearerAnonymousToken(t *testing.T) {
	f := newFake(t)
	f.needAuth = true
	f.user = "" // token service hands out anonymous tokens
	tags, err := f.client(Credential{}).Tags(context.Background(), repo())
	if err != nil || len(tags) != 5 {
		t.Fatalf("tags %v err %v", tags, err)
	}
}

func TestBasicAuth(t *testing.T) {
	f := newFake(t)
	f.basic = true
	if _, err := f.client(Credential{User: f.user, Token: loginToken}).Tags(context.Background(), repo()); err != nil {
		t.Fatal(err)
	}
	_, err := f.client(Credential{}).Tags(context.Background(), repo())
	if !errors.Is(err, ErrDenied) {
		t.Errorf("anonymous against basic: err = %v, want ErrDenied", err)
	}
}

func TestUnauthorizedIsClear(t *testing.T) {
	f := newFake(t)
	f.needAuth = true
	_, err := f.client(Credential{User: f.user, Token: wrongToken}).Tags(context.Background(), repo())
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v, want ErrDenied", err)
	}
	for _, want := range []string{"registry.example.invalid", "team/app", "401", "credential"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "wrong-value-not-real") {
		t.Errorf("error leaks the secret: %v", err)
	}
	_, err = f.client(Credential{}).Tags(context.Background(), repo())
	if !errors.Is(err, ErrDenied) || !strings.Contains(err.Error(), "requires a login") {
		t.Errorf("anonymous 401: %v", err)
	}
}

func TestNotFound(t *testing.T) {
	f := newFake(t)
	_, err := f.client(Credential{}).Digest(context.Background(), repo(), "9.9.9")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestPaginationLimits(t *testing.T) {
	f := newFake(t)
	_, err := f.client(Credential{}, func(o *Options) { o.MaxPages = 2 }).Tags(context.Background(), repo())
	if err == nil || !strings.Contains(err.Error(), "pages") {
		t.Errorf("err = %v, want page limit", err)
	}

	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://other.example.invalid/v2/team/app/tags/list?page=1>; rel="next"`)
		w.Write([]byte(`{"tags":["1.0.0"]}`))
	}))
	defer foreign.Close()
	c := New(Options{BaseURL: func(string) string { return foreign.URL }})
	if _, err := c.Tags(context.Background(), repo()); err == nil || !strings.Contains(err.Error(), "leaves the registry host") {
		t.Errorf("foreign link: err = %v", err)
	}
}

func TestResponseSizeAndTimeout(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tags":["` + strings.Repeat("a", maxTagPageBytes) + `"]}`))
	}))
	defer big.Close()
	c := New(Options{BaseURL: func(string) string { return big.URL }})
	if _, err := c.Tags(context.Background(), repo()); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("oversized: err = %v", err)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()
	c = New(Options{BaseURL: func(string) string { return slow.URL }, RequestTimeout: 50 * time.Millisecond})
	start := time.Now()
	if _, err := c.Tags(context.Background(), repo()); err == nil {
		t.Error("slow registry: want a timeout error")
	}
	if time.Since(start) > 3*time.Second {
		t.Error("request timeout was not enforced")
	}
}

func TestNoRedirectOutsideBlobs(t *testing.T) {
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://other.example.invalid/x", http.StatusFound)
	}))
	defer redir.Close()
	c := New(Options{BaseURL: func(string) string { return redir.URL }})
	if _, err := c.Tags(context.Background(), repo()); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("err = %v, want redirect refusal", err)
	}
}

func TestDigestUsesHead(t *testing.T) {
	f := newFake(t)
	d, err := f.client(Credential{}).Digest(context.Background(), repo(), "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d, "sha256:") {
		t.Errorf("digest = %q", d)
	}
	if got := f.paths[len(f.paths)-1]; got != "HEAD /v2/team/app/manifests/1.2.0" {
		t.Errorf("last request = %q, want a HEAD on the manifest", got)
	}
}

func TestLabelsFromConfigViaBlobRedirect(t *testing.T) {
	for _, index := range []bool{false, true} {
		t.Run(fmt.Sprintf("index=%v", index), func(t *testing.T) {
			f := newFake(t)
			f.index = index
			f.needAuth = true
			labels, err := f.client(Credential{User: f.user, Token: loginToken}).Labels(context.Background(), repo(), "1.2.0")
			if err != nil {
				t.Fatal(err)
			}
			if labels[LabelSource] != "https://example.invalid/team/app" {
				t.Errorf("labels = %v", labels)
			}
		})
	}
}

func TestLabelsNoMatchingPlatform(t *testing.T) {
	f := newFake(t)
	f.index = true
	_, err := f.client(Credential{}, func(o *Options) { o.Platform = Platform{OS: "linux", Arch: "riscv64"} }).Labels(context.Background(), repo(), "1.2.0")
	if err == nil || !strings.Contains(err.Error(), "linux/riscv64") {
		t.Errorf("err = %v", err)
	}
}

func TestDockerHubMapping(t *testing.T) {
	tests := []struct {
		ref      string
		host     string
		name     string
		wantFail bool
	}{
		{"nginx", "registry-1.docker.io", "library/nginx", false},
		{"docker.io/nginx", "registry-1.docker.io", "library/nginx", false},
		{"docker.io/team/app", "registry-1.docker.io", "team/app", false},
		{"team/app", "registry-1.docker.io", "team/app", false},
		{"ghcr.io/team/app", "ghcr.io", "team/app", false},
		{"localhost:5000/app", "localhost:5000", "app", false},
		{"registry.example.invalid/a/b/c", "registry.example.invalid", "a/b/c", false},
		{"nginx:1.2.3", "", "", true},
		{"team/app@sha256:abc", "", "", true},
		{"", "", "", true},
	}
	for _, tc := range tests {
		got, err := ParseRepo(tc.ref)
		if (err != nil) != tc.wantFail {
			t.Errorf("%q: err = %v", tc.ref, err)
			continue
		}
		if err == nil && (got.Host != tc.host || got.Name != tc.name) {
			t.Errorf("%q = %+v, want %s %s", tc.ref, got, tc.host, tc.name)
		}
	}
}

func TestDockerHubRequestUsesLibraryPrefix(t *testing.T) {
	f := newFake(t)
	r, err := ParseRepo("nginx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.client(Credential{}).Tags(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if f.hosts[0] != "registry-1.docker.io" {
		t.Errorf("host = %q", f.hosts[0])
	}
	if !strings.HasPrefix(f.paths[0], "GET /v2/library/nginx/tags/list") {
		t.Errorf("first request = %q", f.paths[0])
	}
}

func TestNoSecretInErrorsOrLogs(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cred := Credential{User: "demo-user", Token: loginToken}
	basicLogin := base64.StdEncoding.EncodeToString([]byte("demo-user:" + loginToken))

	var errs []string
	collect := func(err error) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	withLog := func(o *Options) { o.Logger = logger }

	// Success path, then failures at the token service, at the registry and with a bad body.
	f := newFake(t)
	f.needAuth = true
	c := f.client(cred, withLog)
	if _, err := c.Tags(context.Background(), repo()); err != nil {
		t.Fatal(err)
	}
	_, err := c.Digest(context.Background(), repo(), "9.9.9")
	collect(err)
	_, err = c.Labels(context.Background(), repo(), "9.9.9")
	collect(err)

	f.user = "other-user" // the token service now rejects the login
	_, err = f.client(cred, withLog).Tags(context.Background(), repo())
	collect(err)

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Www-Authenticate", `Bearer realm="`+"http://"+r.Host+`/token"`)
		if r.URL.Path == "/token" {
			w.Write([]byte(`{"token":"` + bearerToken + `` + `"`)) // truncated JSON
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer bad.Close()
	_, err = New(Options{Credential: cred, Logger: logger, BaseURL: func(string) string { return bad.URL }}).Tags(context.Background(), repo())
	collect(err)

	if len(errs) < 4 {
		t.Fatalf("expected failures to inspect, got %v", errs)
	}
	haystack := strings.Join(errs, "\n") + "\n" + logs.String()
	for _, secret := range []string{loginToken, bearerToken, basicLogin} {
		if strings.Contains(haystack, secret) {
			t.Errorf("secret %q appears in errors or logs:\n%s", secret, haystack)
		}
	}
	if logs.Len() == 0 {
		t.Error("expected request log records")
	}
}

func TestParseChallenge(t *testing.T) {
	scheme, p := parseChallenge(`Bearer realm="https://auth.example.invalid/token",service="registry.example.invalid",scope="repository:a/b:pull,push"`)
	if scheme != "bearer" || p["realm"] != "https://auth.example.invalid/token" || p["service"] != "registry.example.invalid" || p["scope"] != "repository:a/b:pull,push" {
		t.Errorf("got %q %v", scheme, p)
	}
	scheme, p = parseChallenge(`Basic realm=demo`)
	if scheme != "basic" || p["realm"] != "demo" {
		t.Errorf("got %q %v", scheme, p)
	}
}

func TestRejectsInsecureRealm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Www-Authenticate", `Bearer realm="http://token.example.invalid/token"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := New(Options{Credential: Credential{User: "u", Token: loginToken}, BaseURL: func(string) string { return srv.URL }})
	if _, err := c.Tags(context.Background(), repo()); err == nil || !strings.Contains(err.Error(), "insecure") {
		t.Errorf("err = %v", err)
	}
}
