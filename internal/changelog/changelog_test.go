package changelog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "test-token-not-real"

type fakeRelease struct {
	tag        string
	draft, pre bool
	body       string
}

// fakeGitHub serves releases of example-org/example-app in pages of three and counts requests.
type fakeGitHub struct {
	*httptest.Server
	mu       sync.Mutex
	releases []fakeRelease
	requests atomic.Int32
	tokens   []string
	status   int // when set, every request answers with it
	headers  map[string]string
}

func newFake(t *testing.T, rels []fakeRelease) *fakeGitHub {
	f := &fakeGitHub{releases: rels}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeGitHub) setStatus(status int, headers map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.headers = status, headers
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = append(f.tokens, r.Header.Get("Authorization"))
	if f.status != 0 {
		for k, v := range f.headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(f.status)
		fmt.Fprint(w, `{"message":"boom"}`)
		return
	}
	if r.URL.Path != "/repos/example-org/example-app/releases" {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") == "" || r.URL.Query().Get("per_page") != "100" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	const per = 3
	page := 1
	fmt.Sscan(r.URL.Query().Get("page"), &page)
	lo, hi := (page-1)*per, page*per
	if hi > len(f.releases) {
		hi = len(f.releases)
	}
	if hi < len(f.releases) {
		w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?per_page=100&page=%d>; rel="next", <http://%s%s?page=9>; rel="last"`, r.Host, r.URL.Path, page+1, r.Host, r.URL.Path))
	}
	var out []map[string]any
	for _, rel := range f.releases[lo:hi] {
		out = append(out, map[string]any{
			"tag_name": rel.tag, "name": "Release " + rel.tag, "body": rel.body, "draft": rel.draft, "prerelease": rel.pre,
			"html_url":     "https://github.com/example-org/example-app/releases/tag/" + rel.tag,
			"published_at": "2026-01-02T03:04:05Z",
		})
	}
	data, _ := json.Marshal(out)
	w.Write(data)
}

func standardReleases() []fakeRelease {
	return []fakeRelease{
		{tag: "v1.5.0"}, {tag: "v1.4.0", body: "four"}, {tag: "v1.4.1-rc1", pre: true},
		{tag: "v1.3.5", draft: true}, {tag: "v1.3.1"}, {tag: "v1.3.0"},
		{tag: "v1.2.5"}, {tag: "v1.2.4"}, {tag: "v1.2.3"}, {tag: "v1.2.0"},
	}
}

var testRepo = Repo{Owner: "example-org", Name: "example-app"}

func versions(rs []Release) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Version)
	}
	return out
}

func TestChangelogsRangeAcrossPages(t *testing.T) {
	f := newFake(t, standardReleases())
	c := New(Options{BaseURL: f.URL, Token: testToken})
	res, err := c.Changelogs(context.Background(), testRepo, "1.2.3", "1.4.0")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.4.0", "1.3.1", "1.3.0", "1.2.5", "1.2.4"}
	if got := versions(res.Releases); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("versions = %v, want %v", got, want)
	}
	if f.requests.Load() != 4 {
		t.Errorf("requests = %d, want 4 pages", f.requests.Load())
	}
	for _, a := range f.tokens {
		if a != "Bearer "+testToken {
			t.Errorf("Authorization = %q", a)
		}
	}
	if res.Releases[0].Body != "four" || res.Releases[0].Name != "Release v1.4.0" || res.Stale {
		t.Errorf("unexpected first release %+v", res.Releases[0])
	}
}

func TestChangelogsVariantAndUnknownCurrent(t *testing.T) {
	f := newFake(t, []fakeRelease{{tag: "v29.0.2"}, {tag: "v29.0.1"}, {tag: "v28.0.0"}})
	c := New(Options{BaseURL: f.URL})
	repo := testRepo
	res, err := c.Changelogs(context.Background(), repo, "29.0.1-apache", "29.0.2-apache")
	if err != nil || len(res.Releases) != 1 || res.Releases[0].Tag != "v29.0.2" {
		t.Fatalf("variant: %+v, %v", res, err)
	}
	res, err = c.Changelogs(context.Background(), repo, "", "29.0.2-apache")
	if err != nil || len(res.Releases) != 1 || res.Releases[0].Tag != "v29.0.2" {
		t.Fatalf("unknown current: %+v, %v", res, err)
	}
	res, err = c.Changelogs(context.Background(), repo, "latest", "29.0.1")
	if err != nil || len(res.Releases) != 1 || res.Releases[0].Tag != "v29.0.1" {
		t.Fatalf("floating current: %+v, %v", res, err)
	}
}

func TestChangelogsNoRangeNoRequest(t *testing.T) {
	f := newFake(t, standardReleases())
	c := New(Options{BaseURL: f.URL})
	for _, tc := range [][2]string{{"1.4.0", "1.4.0"}, {"1.5.0", "1.4.0"}, {"1.2.3", "latest"}, {"1.2.3", ""}} {
		res, err := c.Changelogs(context.Background(), testRepo, tc[0], tc[1])
		if err != nil || len(res.Releases) != 0 || res.Link != "https://github.com/example-org/example-app" {
			t.Errorf("%v: %+v, %v", tc, res, err)
		}
	}
	if f.requests.Load() != 0 {
		t.Errorf("requests = %d, want 0", f.requests.Load())
	}
}

func TestEmptyRangeKeepsLink(t *testing.T) {
	f := newFake(t, standardReleases())
	res, err := New(Options{BaseURL: f.URL}).Changelogs(context.Background(), testRepo, "1.5.0", "1.6.0")
	if err != nil || len(res.Releases) != 0 || res.Link != testRepo.URL() {
		t.Fatalf("%+v, %v", res, err)
	}
}

func TestFreshCacheSkipsRequest(t *testing.T) {
	f := newFake(t, standardReleases())
	dir := filepath.Join(t.TempDir(), "changelogs")
	c := New(Options{BaseURL: f.URL, CacheDir: dir})
	if _, err := c.Changelogs(context.Background(), testRepo, "1.2.3", "1.4.0"); err != nil {
		t.Fatal(err)
	}
	n := f.requests.Load()
	res, err := c.Changelogs(context.Background(), testRepo, "1.3.0", "1.3.1")
	if err != nil || f.requests.Load() != n || len(res.Releases) != 1 || res.Stale {
		t.Fatalf("fresh cache: requests %d->%d, %+v, %v", n, f.requests.Load(), res, err)
	}
	st, err := os.Stat(dir)
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("cache dir: %v, %v", st, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("cache dir entries = %d, want 1 (no temp files)", len(entries))
	}
	if fi, _ := entries[0].Info(); fi.Mode().Perm() != 0o600 {
		t.Errorf("cache file mode = %v", fi.Mode().Perm())
	}
}

func TestStaleCacheOnRateLimitAndServerError(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		notice  string
	}{
		{"403 rate limit", 403, map[string]string{"X-RateLimit-Remaining": "0"}, "GitHub rate limit reached; showing cached changelogs from 2026-10-29 10:00 UTC"},
		{"429", 429, nil, "GitHub rate limit reached; showing cached changelogs from 2026-10-29 10:00 UTC"},
		{"403 retry-after", 403, map[string]string{"Retry-After": "60"}, "GitHub rate limit reached"},
		{"502", 502, nil, "GitHub is not reachable; showing cached changelogs from 2026-10-29 10:00 UTC"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t, standardReleases())
			now := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
			c := New(Options{BaseURL: f.URL, CacheDir: t.TempDir(), Now: func() time.Time { return now }})
			if _, err := c.Changelogs(context.Background(), testRepo, "1.2.3", "1.4.0"); err != nil {
				t.Fatal(err)
			}
			now = now.Add(3 * time.Hour)
			f.setStatus(tc.status, tc.headers)
			res, err := c.Changelogs(context.Background(), testRepo, "1.2.3", "1.4.0")
			if err != nil {
				t.Fatalf("want cached result, got %v", err)
			}
			if !res.Stale || !strings.HasPrefix(res.Notice, strings.SplitN(tc.notice, ";", 2)[0]) || len(res.Releases) != 5 {
				t.Errorf("stale=%v notice=%q releases=%d", res.Stale, res.Notice, len(res.Releases))
			}
			if tc.name != "403 retry-after" && res.Notice != tc.notice {
				t.Errorf("notice = %q, want %q", res.Notice, tc.notice)
			}
			if res.FetchedAt.IsZero() {
				t.Error("FetchedAt not set")
			}
		})
	}
}

func TestErrorsWithoutCache(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		want    error
	}{
		{"rate limit", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1790000000"}, ErrRateLimited},
		{"not found", 404, nil, ErrNotFound},
		{"denied", 401, nil, ErrDenied},
		{"forbidden", 403, nil, ErrDenied},
		{"server", 503, nil, ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t, nil)
			f.setStatus(tc.status, tc.headers)
			_, err := New(Options{BaseURL: f.URL, Token: testToken}).Changelogs(context.Background(), testRepo, "1.0.0", "1.1.0")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("token in error: %v", err)
			}
		})
	}
}

func TestNotFoundDoesNotUseCache(t *testing.T) {
	f := newFake(t, standardReleases())
	now := time.Now()
	c := New(Options{BaseURL: f.URL, CacheDir: t.TempDir(), Now: func() time.Time { return now }})
	if _, err := c.Changelogs(context.Background(), testRepo, "1.2.3", "1.4.0"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	f.setStatus(404, nil)
	if _, err := c.Changelogs(context.Background(), testRepo, "1.2.3", "1.4.0"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenNeverLeaks(t *testing.T) {
	dir := t.TempDir()
	f := newFake(t, standardReleases())
	c := New(Options{BaseURL: f.URL, Token: testToken, CacheDir: dir})
	res, err := c.Changelogs(context.Background(), testRepo, "1.2.3", "1.4.0")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprintf("%+v", res), testToken) {
		t.Error("token in result")
	}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			data, _ := os.ReadFile(p)
			if strings.Contains(string(data), testToken) || strings.Contains(p, testToken) {
				t.Errorf("token in cache %s", p)
			}
		}
		return nil
	})
	// Transport failure: the closed server must not echo the token either.
	f.Close()
	_, err = New(Options{BaseURL: f.URL, Token: testToken}).Changelogs(context.Background(), testRepo, "1.0.0", "1.1.0")
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), testToken) {
		t.Errorf("transport error = %v", err)
	}
	// Invalid token characters are rejected without echoing the token.
	bad := testToken + "\nX-Evil: 1"
	_, err = New(Options{BaseURL: "http://127.0.0.1:1", Token: bad}).Changelogs(context.Background(), testRepo, "1.0.0", "1.1.0")
	if err == nil || strings.Contains(err.Error(), "test-token") {
		t.Errorf("invalid token error = %v", err)
	}
	// The cache key does not depend on the token.
	if a, b := New(Options{CacheDir: "/x", Token: "a"}).cachePath(testRepo), New(Options{CacheDir: "/x"}).cachePath(testRepo); a != b {
		t.Errorf("cache path depends on token: %s vs %s", a, b)
	}
}

func TestResponseSizeLimit(t *testing.T) {
	f := newFake(t, standardReleases())
	_, err := New(Options{BaseURL: f.URL, MaxResponseBytes: 100}).Changelogs(context.Background(), testRepo, "1.2.3", "1.4.0")
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "larger than 100 bytes") {
		t.Fatalf("err = %v", err)
	}
}

func TestPageCap(t *testing.T) {
	f := newFake(t, standardReleases())
	res, err := New(Options{BaseURL: f.URL, MaxPages: 2}).Changelogs(context.Background(), testRepo, "1.2.3", "1.4.0")
	if err != nil || f.requests.Load() != 2 {
		t.Fatalf("requests %d, %v", f.requests.Load(), err)
	}
	if got := versions(res.Releases); strings.Join(got, ",") != "1.4.0,1.3.1,1.3.0" {
		t.Errorf("versions = %v", got)
	}
}

func TestRedirectToOtherHostIsNotFollowed(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("token reached foreign host")
		}
		fmt.Fprint(w, "[]")
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/x", http.StatusFound)
	}))
	defer srv.Close()
	_, err := New(Options{BaseURL: srv.URL, Token: testToken}).Changelogs(context.Background(), testRepo, "1.0.0", "1.1.0")
	if err == nil || hits.Load() != 0 || strings.Contains(err.Error(), testToken) {
		t.Fatalf("err = %v, foreign hits = %d", err, hits.Load())
	}
}

func TestPaginationLinkOffHostIsRejected(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/next>; rel="next"`, other.URL))
		fmt.Fprint(w, "[]")
	}))
	defer srv.Close()
	_, err := New(Options{BaseURL: srv.URL, Token: testToken}).Changelogs(context.Background(), testRepo, "1.0.0", "1.1.0")
	if !errors.Is(err, ErrUnavailable) || hits.Load() != 0 {
		t.Fatalf("err = %v, hits = %d", err, hits.Load())
	}
}

func TestReleaseTextIsSanitizedAndTruncated(t *testing.T) {
	long := strings.Repeat("ä", maxBodyBytes) // multi-byte to test the rune boundary
	f := newFake(t, []fakeRelease{{tag: "v2.0.0", body: "\x1b[31mred\x1b[0m\r\nnext\x07 line\x1b]0;title\x07"}, {tag: "v1.9.0", body: long}})
	res, err := New(Options{BaseURL: f.URL}).Changelogs(context.Background(), testRepo, "1.8.0", "2.0.0")
	if err != nil || len(res.Releases) != 2 {
		t.Fatalf("%+v, %v", res, err)
	}
	if got := res.Releases[0].Body; got != "red\nnext line" {
		t.Errorf("body = %q", got)
	}
	b := res.Releases[1].Body
	if !strings.HasSuffix(b, "[truncated; see the full release notes on GitHub]") || len(b) > maxBodyBytes+100 {
		t.Errorf("truncation: len %d, tail %q", len(b), b[len(b)-60:])
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"a\x1b[1;38;5;196mb\x1b[K", "ab"},
		{"x\x1b]0;title\x07y", "xy"},
		{"x\x1b]8;;http://example.invalid/\x1b\\link\x1b]8;;\x1b\\y", "xlinky"},
		{"x\x1b]0;unterminated\nnext", "x\nnext"},
		{"x\x1bPdcs data\x1b\\y", "xy"},
		{"a\x1bcb\x1b(Bc", "abc"},
		{"a\x00b\x07c\x08d\x7fe", "abcde"},
		{"keep\ttab\nline", "keep\ttab\nline"},
		{"win\r\nmac\rend", "win\nmac\nend"},
		{"c1\u009b31mred\u009d0;t\u009cz", "c1redz"},
		{"a‮b⁦c", "abc"},
		{"bad\xffbyte", "badbyte"},
		{"trailing\x1b", "trailing"},
		{"umlaut äöü €", "umlaut äöü €"},
	}
	for _, tc := range cases {
		if got := Sanitize(tc.in); got != tc.want {
			t.Errorf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseSource(t *testing.T) {
	gh := Source{Repo: Repo{"example-org", "example-app"}, GitHub: true, Link: "https://github.com/example-org/example-app"}
	// Built from parts so the SSH user does not read as an e-mail address.
	sshHost := "git" + "@" + "github.com"
	for _, in := range []string{
		"example-org/example-app",
		"example-org/example-app.git",
		"https://github.com/example-org/example-app",
		"https://github.com/example-org/example-app.git",
		"https://github.com/example-org/example-app/tree/main/docs",
		"http://www.github.com/example-org/example-app/",
		sshHost + ":example-org/example-app.git",
		"ssh://" + sshHost + "/example-org/example-app",
		"git://github.com/example-org/example-app.git",
		"https://" + "user:pass" + "@" + "github.com/example-org/example-app",
		"github.com/example-org/example-app",
		" https://GitHub.com/example-org/example-app\n",
	} {
		if got := ParseSource(in); got != gh {
			t.Errorf("ParseSource(%q) = %+v", in, got)
		}
	}
	for _, in := range []string{"", "example-app", "a/b/c", "example.com/foo", "github.com/only", "https://github.com/only", "../x", "owner/..", "git@example.invalid:o/r.git", "ssh://git@example.invalid/o/r", "ftp://example.invalid/o/r", "o/r with space"} {
		if got := ParseSource(in); got.GitHub || got.Link != "" {
			t.Errorf("ParseSource(%q) = %+v, want unrecognized", in, got)
		}
	}
	got := ParseSource("https://user:secret@example.invalid/team/app?token=x#frag")
	if got.GitHub || got.Link != "https://example.invalid/team/app" {
		t.Errorf("non-GitHub link = %+v", got)
	}
}
