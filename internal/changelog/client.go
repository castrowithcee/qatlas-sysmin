// Package changelog reads the release texts of a GitHub repository between two versions. It caches the
// release list per repository and falls back to an expired cache when GitHub is rate limiting or
// unreachable. Release texts are untrusted and reduced to plain text. The token lives only in the request
// header: it is never part of errors, results or cache files.
package changelog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/source/version"
)

// Limits and defaults.
const (
	DefaultBaseURL        = "https://api.github.com"
	DefaultRequestTimeout = 30 * time.Second
	DefaultMaxPages       = 5
	DefaultCacheTTL       = time.Hour
	DefaultMaxResponse    = 16 << 20
	maxBodyBytes          = 8 << 10
	maxNameBytes          = 256
	maxRedirects          = 3
	pageSize              = 100
	apiVersion            = "2022-11-28"
)

// Errors wrapped by the errors of Changelogs.
var (
	ErrRateLimited = errors.New("GitHub rate limit reached")
	ErrNotFound    = errors.New("repository not found on GitHub")
	ErrDenied      = errors.New("GitHub denied access")
	// ErrUnavailable covers server errors, network errors and unusable answers.
	ErrUnavailable = errors.New("GitHub is not reachable")
)

// Options configure a Client. The zero value is usable but caches nothing.
type Options struct {
	// Token is an optional GitHub token, sent only to the host of BaseURL.
	Token string
	// BaseURL is the API address; tests point it at a local server. Default DefaultBaseURL.
	BaseURL string
	// HTTPClient is copied; its redirect policy is replaced. The default has no overall timeout.
	HTTPClient *http.Client
	// CacheDir holds the per-repository cache files. Empty disables the cache.
	CacheDir string
	// CacheTTL is how long a cache file counts as fresh. Default DefaultCacheTTL.
	CacheTTL time.Duration
	// RequestTimeout limits each request including reading the answer. Default DefaultRequestTimeout.
	RequestTimeout time.Duration
	// MaxPages caps the pages read per repository; older releases beyond it are not listed.
	MaxPages int
	// MaxResponseBytes caps one answer; a larger one is an error. Default DefaultMaxResponse.
	MaxResponseBytes int64
	// Now is the clock; default time.Now.
	Now func() time.Time
}

// Client reads changelogs. It is safe for concurrent use.
type Client struct {
	opts    Options
	hc      *http.Client
	base    *url.URL
	baseErr error
}

// Release is one published (not draft, not pre-release) GitHub release as plain text.
type Release struct {
	// Version is the core version of the tag without prefix or variant, such as "1.4.0".
	Version   string    `json:"version"`
	Tag       string    `json:"tag"`
	Name      string    `json:"name"`
	Body      string    `json:"body"`
	URL       string    `json:"url,omitempty"`
	Published time.Time `json:"published,omitzero"`
}

// Result is the answer of Changelogs.
type Result struct {
	// Releases are the releases above the current and up to the target version, newest first.
	Releases []Release
	// Link is the repository address to show when Releases is empty.
	Link string
	// Stale reports that Releases come from the cache because GitHub could not be asked.
	Stale bool
	// Notice explains a stale result.
	Notice string
	// FetchedAt is when the underlying release list was read from GitHub.
	FetchedAt time.Time
}

// New returns a client with defaults applied.
func New(opts Options) *Client {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = DefaultCacheTTL
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = DefaultMaxPages
	}
	if opts.MaxResponseBytes <= 0 {
		opts.MaxResponseBytes = DefaultMaxResponse
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	opts.Token = strings.TrimSpace(opts.Token)
	c := &Client{opts: opts}
	c.base, c.baseErr = parseBase(opts.BaseURL)
	hc := &http.Client{}
	if opts.HTTPClient != nil {
		cp := *opts.HTTPClient
		hc = &cp
	}
	// A redirect off the API host would carry the token along, so only same-host redirects are followed.
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects || c.base == nil || req.URL.Host != c.base.Host || req.URL.Scheme != c.base.Scheme {
			return http.ErrUseLastResponse
		}
		return nil
	}
	c.hc = hc
	return c
}

func parseBase(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil, errors.New("invalid GitHub API base URL")
	}
	return u, nil
}

// Changelogs returns the releases of repo above current and up to target (both inclusive of target only),
// matched by core version so that "29.0.2-apache" finds the release "v29.0.2". With an unknown current
// version only the release of target is returned. An unusable target yields an empty result. The release list
// comes from a fresh cache or from GitHub; when GitHub is rate limiting or unreachable an existing cache is
// used even if expired and the result is marked Stale. Otherwise the error wraps one of the Err values.
func (c *Client) Changelogs(ctx context.Context, repo Repo, current, target string) (Result, error) {
	res := Result{Link: repo.URL()}
	if !repo.valid() {
		return res, fmt.Errorf("invalid GitHub repository %q", repo.String())
	}
	tc, ok := version.ParseCore(target)
	if !ok {
		return res, nil
	}
	cc, haveCur := version.ParseCore(current)
	if haveCur && cc.Compare(tc) >= 0 {
		return res, nil
	}
	list, err := c.list(ctx, repo)
	if err != nil {
		return res, err
	}
	res.Stale, res.Notice, res.FetchedAt = list.stale, list.notice, list.fetched
	res.Releases = pick(list.releases, cc, haveCur, tc)
	return res, nil
}

// pick selects the releases in (cur, target], newest first, one per version.
func pick(all []Release, cur version.Core, haveCur bool, target version.Core) []Release {
	type item struct {
		core version.Core
		rel  Release
	}
	var items []item
	for _, r := range all {
		core, ok := version.ParseCore(r.Tag)
		if !ok || core.Compare(target) > 0 {
			continue
		}
		if haveCur && core.Compare(cur) <= 0 || !haveCur && core.Compare(target) != 0 {
			continue
		}
		items = append(items, item{core, r})
	}
	// Equal versions: the plain tag beats a variant tag, then list order decides.
	sort.SliceStable(items, func(i, j int) bool {
		if c := items[i].core.Compare(items[j].core); c != 0 {
			return c > 0
		}
		return !strings.Contains(items[i].rel.Tag, "-") && strings.Contains(items[j].rel.Tag, "-")
	})
	var out []Release
	for _, it := range items {
		if len(out) > 0 && out[len(out)-1].Version == it.core.String() {
			continue
		}
		out = append(out, it.rel)
	}
	return out
}

type releaseList struct {
	releases []Release
	fetched  time.Time
	stale    bool
	notice   string
}

func (c *Client) list(ctx context.Context, repo Repo) (releaseList, error) {
	cached, haveCache := c.readCache(repo)
	if haveCache && c.opts.Now().Sub(cached.FetchedAt) < c.opts.CacheTTL {
		return releaseList{releases: cached.Releases, fetched: cached.FetchedAt}, nil
	}
	rels, err := c.fetch(ctx, repo)
	if err == nil {
		now := c.opts.Now()
		c.writeCache(repo, cacheFile{Version: cacheVersion, Repo: repo.String(), FetchedAt: now, Releases: rels})
		return releaseList{releases: rels, fetched: now}, nil
	}
	if ctx.Err() == nil && haveCache {
		cause := "GitHub is not reachable"
		if errors.Is(err, ErrRateLimited) {
			cause = "GitHub rate limit reached"
		}
		if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrUnavailable) {
			return releaseList{
				releases: cached.Releases,
				fetched:  cached.FetchedAt,
				stale:    true,
				notice:   fmt.Sprintf("%s; showing cached changelogs from %s", cause, cached.FetchedAt.UTC().Format("2006-01-02 15:04 UTC")),
			}, nil
		}
	}
	return releaseList{}, err
}

type rawRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

// fetch reads the release list page by page. Beyond MaxPages older releases are left out.
func (c *Client) fetch(ctx context.Context, repo Repo) ([]Release, error) {
	if c.baseErr != nil {
		return nil, c.baseErr
	}
	next := c.base.String() + "/repos/" + repo.Owner + "/" + repo.Name + "/releases?per_page=" + strconv.Itoa(pageSize)
	var out []Release
	for page := 0; next != "" && page < c.opts.MaxPages; page++ {
		body, header, err := c.get(ctx, next)
		if err != nil {
			return nil, err
		}
		var raw []rawRelease
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("release list of %s: invalid response: %w", repo, ErrUnavailable)
		}
		for _, r := range raw {
			if r.Draft || r.Prerelease {
				continue
			}
			core, ok := version.ParseCore(r.TagName)
			if !ok {
				continue
			}
			out = append(out, Release{
				Version:   core.String(),
				Tag:       oneLine(Sanitize(r.TagName)),
				Name:      truncate(oneLine(Sanitize(r.Name)), maxNameBytes),
				Body:      truncate(strings.TrimSpace(Sanitize(r.Body)), maxBodyBytes),
				URL:       safeLink(r.HTMLURL),
				Published: r.PublishedAt,
			})
		}
		if next, err = nextLink(header.Values("Link"), next, c.base); err != nil {
			return nil, fmt.Errorf("release list of %s: %v: %w", repo, err, ErrUnavailable)
		}
	}
	return out, nil
}

// safeLink keeps only plain http(s) addresses without user info.
func safeLink(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Scheme != "https" && u.Scheme != "http" || strings.ContainsFunc(raw, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return ""
	}
	return u.String()
}

// get performs one request inside the request timeout and maps the status to an error.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, http.Header, error) {
	if strings.ContainsFunc(c.opts.Token, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return nil, nil, errors.New("the GitHub token contains invalid characters")
	}
	rctx, cancel := context.WithTimeout(ctx, c.opts.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, errors.New("invalid request URL")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "sysmin")
	if c.opts.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.opts.Token)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, nil, transportError(req.URL, err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
	case res.StatusCode == http.StatusTooManyRequests,
		res.StatusCode == http.StatusForbidden && (res.Header.Get("X-RateLimit-Remaining") == "0" || res.Header.Get("Retry-After") != ""):
		return nil, nil, rateLimitError(res.Header)
	case res.StatusCode == http.StatusUnauthorized, res.StatusCode == http.StatusForbidden:
		hint := "anonymous access was refused"
		if c.opts.Token != "" {
			hint = "the configured token was not accepted or lacks access"
		}
		return nil, nil, fmt.Errorf("GitHub answered HTTP %d: %s: %w", res.StatusCode, hint, ErrDenied)
	case res.StatusCode == http.StatusNotFound:
		return nil, nil, fmt.Errorf("GitHub answered HTTP 404: %w", ErrNotFound)
	case res.StatusCode >= 500:
		return nil, nil, fmt.Errorf("GitHub answered HTTP %d: %w", res.StatusCode, ErrUnavailable)
	default:
		return nil, nil, fmt.Errorf("unexpected HTTP status %d from GitHub", res.StatusCode)
	}
	limit := c.opts.MaxResponseBytes
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, nil, transportError(req.URL, err)
	}
	if int64(len(body)) > limit {
		return nil, nil, fmt.Errorf("GitHub response larger than %d bytes: %w", limit, ErrUnavailable)
	}
	return body, res.Header, nil
}

func rateLimitError(h http.Header) error {
	if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil && reset > 0 {
		return fmt.Errorf("%w (resets at %s)", ErrRateLimited, time.Unix(reset, 0).UTC().Format("15:04 UTC"))
	}
	return ErrRateLimited
}

func transportError(u *url.URL, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("GET %s: %v: %w", u.Host, err, ErrUnavailable)
}

// nextLink returns the absolute URL of the rel="next" link, or "" if there is none. Only links on the host
// and scheme of the API are followed, so a hostile response cannot send the token elsewhere.
func nextLink(values []string, from string, base *url.URL) (string, error) {
	cur, err := url.Parse(from)
	if err != nil {
		return "", err
	}
	for _, v := range values {
		for _, part := range splitLinks(v) {
			target, params, ok := strings.Cut(part, ">")
			target = strings.TrimSpace(target)
			if !ok || !strings.HasPrefix(target, "<") || !hasRelNext(params) {
				continue
			}
			ref, err := url.Parse(strings.TrimPrefix(target, "<"))
			if err != nil {
				return "", errors.New("invalid pagination link")
			}
			abs := cur.ResolveReference(ref)
			if abs.Host != base.Host || abs.Scheme != base.Scheme || abs.User != nil {
				return "", errors.New("pagination link leaves the API host")
			}
			if abs.String() == from {
				return "", errors.New("pagination link repeats the current page")
			}
			return abs.String(), nil
		}
	}
	return "", nil
}

// splitLinks splits a Link header value on the commas that start a new "<url>" entry.
func splitLinks(v string) []string {
	var parts []string
	for {
		i := strings.Index(v, ", <")
		if i < 0 {
			return append(parts, v)
		}
		parts = append(parts, v[:i])
		v = v[i+2:]
	}
}

func hasRelNext(params string) bool {
	for _, p := range strings.Split(params, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok && strings.EqualFold(k, "rel") {
			for _, rel := range strings.Fields(strings.Trim(v, `"`)) {
				if strings.EqualFold(rel, "next") {
					return true
				}
			}
		}
	}
	return false
}
