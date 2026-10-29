// Package registry reads tags, digests and image labels from an OCI registry through the distribution API.
// It authenticates anonymously, with Basic or with a Bearer token obtained from the realm the registry names.
// Secrets live only in request headers: they are never part of errors, URLs or log records.
package registry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// LabelSource is the image label that names the source repository.
const LabelSource = "org.opencontainers.image.source"

// Limits and defaults.
const (
	DefaultRequestTimeout = 30 * time.Second
	DefaultMaxPages       = 50
	maxTagPageBytes       = 4 << 20
	maxManifestBytes      = 1 << 20
	maxConfigBytes        = 1 << 20
	maxTokenBytes         = 64 << 10
	maxBlobRedirects      = 3
	pageSize              = 1000
)

const (
	mediaOCIIndex       = "application/vnd.oci.image.index.v1+json"
	mediaOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	mediaDockerList     = "application/vnd.docker.distribution.manifest.list.v2+json"
	mediaDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	manifestAccept      = mediaOCIIndex + ", " + mediaOCIManifest + ", " + mediaDockerList + ", " + mediaDockerManifest
)

var digestRe = regexp.MustCompile(`^[a-z0-9]+(?:[.+_-][a-z0-9]+)*:[a-zA-Z0-9=_-]+$`)

// Credential is the login for a registry. The zero value is anonymous access. With a token but no user, the
// token alone is sent as the password of an empty user name.
type Credential struct {
	User  string
	Token string
}

func (c Credential) set() bool { return c.User != "" || c.Token != "" }

// Platform selects the manifest of a multi-platform image.
type Platform struct {
	OS, Arch string
}

// Options configure a Client. The zero value is usable.
type Options struct {
	Credential Credential
	// HTTPClient is copied; its redirect policy is replaced. The default has no overall timeout.
	HTTPClient *http.Client
	// BaseURL maps a registry host to its base URL, such as "https://host". The default is HTTPS on the
	// host; tests point it at a local server.
	BaseURL func(host string) string
	// Platform defaults to linux and the architecture of this process.
	Platform       Platform
	RequestTimeout time.Duration
	MaxPages       int
	// Logger receives one debug record per request with method, URL without query and status.
	Logger *slog.Logger
}

// Client talks to registries. It is safe for concurrent use.
type Client struct {
	opts Options
	hc   *http.Client

	mu     sync.Mutex
	tokens map[string]string // "host\x00scope" -> bearer token
}

// ErrDenied is wrapped by errors for 401 and 403 answers.
var ErrDenied = errors.New("access denied")

// ErrNotFound is wrapped by errors for 404 answers.
var ErrNotFound = errors.New("not found")

// New returns a client with defaults applied.
func New(opts Options) *Client {
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = DefaultRequestTimeout
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = DefaultMaxPages
	}
	if opts.Platform.OS == "" {
		opts.Platform.OS = "linux"
	}
	if opts.Platform.Arch == "" {
		opts.Platform.Arch = runtime.GOARCH
	}
	if opts.BaseURL == nil {
		opts.BaseURL = func(host string) string { return "https://" + host }
	}
	hc := &http.Client{}
	if opts.HTTPClient != nil {
		c := *opts.HTTPClient
		hc = &c
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{opts: opts, hc: hc, tokens: map[string]string{}}
}

// Tags lists every tag of the repository, following the pagination links of the registry.
func (c *Client) Tags(ctx context.Context, repo Repo) ([]string, error) {
	base, err := c.baseURL(repo)
	if err != nil {
		return nil, err
	}
	next := base.String() + "/v2/" + repo.Name + "/tags/list?n=" + fmt.Sprint(pageSize)
	var tags []string
	for page := 0; next != ""; page++ {
		if page >= c.opts.MaxPages {
			return nil, fmt.Errorf("tag list of %s exceeds %d pages", repo, c.opts.MaxPages)
		}
		resp, err := c.do(ctx, repo, http.MethodGet, next, "application/json", maxTagPageBytes, false)
		if err != nil {
			return nil, err
		}
		var body struct {
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(resp.body, &body); err != nil {
			return nil, fmt.Errorf("tag list of %s: invalid response", repo)
		}
		tags = append(tags, body.Tags...)
		if next, err = nextLink(resp.header.Values("Link"), next, base); err != nil {
			return nil, fmt.Errorf("tag list of %s: %w", repo, err)
		}
	}
	return tags, nil
}

// Digest returns the manifest digest of a tag, read from a HEAD request.
func (c *Client) Digest(ctx context.Context, repo Repo, tag string) (string, error) {
	if err := checkReference(tag); err != nil {
		return "", err
	}
	base, err := c.baseURL(repo)
	if err != nil {
		return "", err
	}
	resp, err := c.do(ctx, repo, http.MethodHead, base.String()+"/v2/"+repo.Name+"/manifests/"+tag, manifestAccept, 0, false)
	if err != nil {
		return "", err
	}
	d := resp.header.Get("Docker-Content-Digest")
	if !digestRe.MatchString(d) {
		return "", fmt.Errorf("registry %s did not report a digest for %s:%s", repo.Host, repo.Name, tag)
	}
	return d, nil
}

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Platform  *struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
	} `json:"platform"`
}

type manifestDoc struct {
	MediaType string       `json:"mediaType"`
	Config    *descriptor  `json:"config"`
	Manifests []descriptor `json:"manifests"`
}

// Labels returns the labels of the image configuration for the tag. For a multi-platform image the manifest
// of the configured platform is used.
func (c *Client) Labels(ctx context.Context, repo Repo, tag string) (map[string]string, error) {
	if err := checkReference(tag); err != nil {
		return nil, err
	}
	base, err := c.baseURL(repo)
	if err != nil {
		return nil, err
	}
	manifestURL := func(ref string) string { return base.String() + "/v2/" + repo.Name + "/manifests/" + ref }
	doc, err := c.manifest(ctx, repo, manifestURL(tag))
	if err != nil {
		return nil, err
	}
	if len(doc.Manifests) > 0 || doc.MediaType == mediaOCIIndex || doc.MediaType == mediaDockerList {
		digest := ""
		for _, m := range doc.Manifests {
			if m.Platform != nil && m.Platform.OS == c.opts.Platform.OS && m.Platform.Architecture == c.opts.Platform.Arch {
				digest = m.Digest
				break
			}
		}
		if digest == "" {
			return nil, fmt.Errorf("%s:%s has no manifest for %s/%s", repo.Name, tag, c.opts.Platform.OS, c.opts.Platform.Arch)
		}
		if !digestRe.MatchString(digest) {
			return nil, fmt.Errorf("%s:%s: invalid manifest digest in index", repo.Name, tag)
		}
		if doc, err = c.manifest(ctx, repo, manifestURL(digest)); err != nil {
			return nil, err
		}
	}
	if doc.Config == nil || !digestRe.MatchString(doc.Config.Digest) {
		return nil, fmt.Errorf("%s:%s: manifest has no usable image configuration", repo.Name, tag)
	}
	resp, err := c.do(ctx, repo, http.MethodGet, base.String()+"/v2/"+repo.Name+"/blobs/"+doc.Config.Digest, "application/json", maxConfigBytes, true)
	if err != nil {
		return nil, err
	}
	if algo, hexSum, _ := strings.Cut(doc.Config.Digest, ":"); algo == "sha256" {
		sum := sha256.Sum256(resp.body)
		if hex.EncodeToString(sum[:]) != strings.ToLower(hexSum) {
			return nil, fmt.Errorf("%s:%s: image configuration does not match its digest", repo.Name, tag)
		}
	}
	var cfg struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := json.Unmarshal(resp.body, &cfg); err != nil {
		return nil, fmt.Errorf("%s:%s: invalid image configuration", repo.Name, tag)
	}
	return cfg.Config.Labels, nil
}

func (c *Client) manifest(ctx context.Context, repo Repo, u string) (manifestDoc, error) {
	resp, err := c.do(ctx, repo, http.MethodGet, u, manifestAccept, maxManifestBytes, false)
	if err != nil {
		return manifestDoc{}, err
	}
	var doc manifestDoc
	if err := json.Unmarshal(resp.body, &doc); err != nil {
		return manifestDoc{}, fmt.Errorf("manifest of %s: invalid response", repo)
	}
	if doc.MediaType == "" {
		doc.MediaType = strings.TrimSpace(strings.SplitN(resp.header.Get("Content-Type"), ";", 2)[0])
	}
	return doc, nil
}

func checkReference(ref string) error {
	if ref == "" || strings.ContainsAny(ref, "/?#\\ ") {
		return fmt.Errorf("invalid tag %q", ref)
	}
	return nil
}

func (c *Client) baseURL(repo Repo) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(c.opts.BaseURL(repo.Host), "/"))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return nil, fmt.Errorf("invalid base URL for registry %s", repo.Host)
	}
	return u, nil
}

// nextLink returns the absolute URL of the rel="next" link, or "" if there is none. Only links on the host
// and scheme of the registry are followed, so a hostile response cannot redirect credentials elsewhere.
func nextLink(values []string, from string, base *url.URL) (string, error) {
	cur, err := url.Parse(from)
	if err != nil {
		return "", err
	}
	for _, v := range values {
		for _, part := range splitLinks(v) {
			target, params, ok := strings.Cut(part, ">")
			if !ok || !strings.HasPrefix(strings.TrimSpace(target), "<") {
				continue
			}
			if !hasRelNext(params) {
				continue
			}
			ref, err := url.Parse(strings.TrimPrefix(strings.TrimSpace(target), "<"))
			if err != nil {
				return "", errors.New("invalid pagination link")
			}
			abs := cur.ResolveReference(ref)
			if abs.Host != base.Host || abs.Scheme != base.Scheme || abs.User != nil {
				return "", errors.New("pagination link leaves the registry host")
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

type response struct {
	header http.Header
	body   []byte
}

// do performs one request with authentication. A 401 triggers one retry with the credentials the challenge
// asks for. With blob set, redirects are followed (registries hand blobs to a CDN) and the authorization
// header is dropped when the target host differs.
func (c *Client) do(ctx context.Context, repo Repo, method, rawURL, accept string, limit int64, blob bool) (response, error) {
	base, err := c.baseURL(repo)
	if err != nil {
		return response{}, err
	}
	scope := "repository:" + repo.Name + ":pull"
	auth := c.cachedAuth(repo.Host, scope)
	for attempt := 0; ; attempt++ {
		resp, status, err := c.roundTrip(ctx, method, rawURL, accept, auth, limit, blob, base)
		if err != nil {
			return response{}, err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			challenge := resp.header.Values("Www-Authenticate")
			if auth, err = c.authorize(ctx, repo, base, challenge, scope); err != nil {
				return response{}, err
			}
			if auth == "" {
				return response{}, deniedError(repo, status, c.opts.Credential.set())
			}
			continue
		}
		switch {
		case status >= 200 && status < 300:
			return resp, nil
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			return response{}, deniedError(repo, status, c.opts.Credential.set())
		case status == http.StatusNotFound:
			return response{}, fmt.Errorf("%s %s: %w (HTTP 404)", method, repo, ErrNotFound)
		default:
			return response{}, fmt.Errorf("%s %s: unexpected HTTP status %d", method, repo, status)
		}
	}
}

func deniedError(repo Repo, status int, haveCredential bool) error {
	hint := "the registry requires a login; configure a registry credential for this image"
	if haveCredential {
		hint = "the configured registry credential was not accepted or lacks pull access"
	}
	return fmt.Errorf("registry %s denied access to %s (HTTP %d): %s: %w", repo.Host, repo.Name, status, hint, ErrDenied)
}

func (c *Client) cachedAuth(host, scope string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.tokens[host+"\x00"+scope]; ok {
		return "Bearer " + t
	}
	return ""
}

// roundTrip sends the request and reads the limited body inside the request timeout.
func (c *Client) roundTrip(ctx context.Context, method, rawURL, accept, auth string, limit int64, blob bool, base *url.URL) (response, int, error) {
	ctx, cancel := context.WithTimeout(ctx, c.opts.RequestTimeout)
	defer cancel()
	cur := rawURL
	for hops := 0; ; hops++ {
		req, err := http.NewRequestWithContext(ctx, method, cur, nil)
		if err != nil {
			return response{}, 0, errors.New("invalid request URL")
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("User-Agent", "sysmin")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		res, err := c.hc.Do(req)
		if err != nil {
			return response{}, 0, transportError(method, req.URL, err)
		}
		c.logRequest(method, req.URL, res.StatusCode)
		if res.StatusCode >= 300 && res.StatusCode < 400 && res.StatusCode != http.StatusNotModified {
			loc := res.Header.Get("Location")
			res.Body.Close()
			if !blob || loc == "" || hops >= maxBlobRedirects {
				return response{}, 0, fmt.Errorf("%s %s: unexpected redirect", method, req.URL.Host)
			}
			next, err := req.URL.Parse(loc)
			if err != nil || next.User != nil || (next.Scheme != "https" && next.Scheme != base.Scheme) {
				return response{}, 0, fmt.Errorf("%s %s: unsafe redirect", method, req.URL.Host)
			}
			if next.Host != base.Host {
				auth = ""
			}
			cur = next.String()
			continue
		}
		defer res.Body.Close()
		out := response{header: res.Header}
		if method != http.MethodHead && res.StatusCode >= 200 && res.StatusCode < 300 {
			body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
			if err != nil {
				return response{}, 0, transportError(method, req.URL, err)
			}
			if int64(len(body)) > limit {
				return response{}, 0, fmt.Errorf("%s %s: response larger than %d bytes", method, req.URL.Host, limit)
			}
			out.body = body
		}
		return out, res.StatusCode, nil
	}
}

func transportError(method string, u *url.URL, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Errorf("%s %s: %w", method, u.Host, err)
}

func (c *Client) logRequest(method string, u *url.URL, status int) {
	if c.opts.Logger == nil {
		return
	}
	clean := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}
	c.opts.Logger.Debug("registry request", "method", method, "url", clean.String(), "status", status)
}

// authorize turns a WWW-Authenticate challenge into an Authorization header value and remembers bearer
// tokens. It returns "" when no credential can answer the challenge.
func (c *Client) authorize(ctx context.Context, repo Repo, base *url.URL, challenges []string, scope string) (string, error) {
	for _, ch := range challenges {
		scheme, params := parseChallenge(ch)
		switch scheme {
		case "bearer":
			return c.bearer(ctx, repo, base, params, scope)
		case "basic":
			if c.opts.Credential.set() {
				return basicHeader(c.opts.Credential), nil
			}
		}
	}
	return "", nil
}

func basicHeader(cred Credential) string {
	r := &http.Request{Header: http.Header{}}
	r.SetBasicAuth(cred.User, cred.Token)
	return r.Header.Get("Authorization")
}

func (c *Client) bearer(ctx context.Context, repo Repo, base *url.URL, params map[string]string, scope string) (string, error) {
	realm, err := url.Parse(params["realm"])
	if err != nil || realm.Host == "" || realm.User != nil {
		return "", fmt.Errorf("registry %s sent an unusable authentication challenge", repo.Host)
	}
	if realm.Scheme != "https" && (realm.Scheme != base.Scheme || realm.Host != base.Host) {
		return "", fmt.Errorf("registry %s sent an insecure authentication realm", repo.Host)
	}
	if s := params["scope"]; s != "" {
		scope = s
	}
	q := realm.Query()
	if s := params["service"]; s != "" {
		q.Set("service", s)
	}
	q.Set("scope", scope)
	realm.RawQuery = q.Encode()

	auth := ""
	if c.opts.Credential.set() {
		auth = basicHeader(c.opts.Credential)
	}
	resp, status, err := c.roundTrip(ctx, http.MethodGet, realm.String(), "application/json", auth, maxTokenBytes, false, &url.URL{Scheme: realm.Scheme, Host: realm.Host})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return "", deniedError(repo, status, c.opts.Credential.set())
		}
		return "", fmt.Errorf("token service for %s answered HTTP %d", repo.Host, status)
	}
	var tok struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(resp.body, &tok); err != nil {
		return "", fmt.Errorf("token service for %s sent an invalid response", repo.Host)
	}
	t := tok.Token
	if t == "" {
		t = tok.AccessToken
	}
	if t == "" {
		return "", fmt.Errorf("token service for %s sent no token", repo.Host)
	}
	c.mu.Lock()
	c.tokens[repo.Host+"\x00"+repo.scopeKey()] = t
	c.mu.Unlock()
	return "Bearer " + t, nil
}

func (r Repo) scopeKey() string { return "repository:" + r.Name + ":pull" }

// parseChallenge reads one WWW-Authenticate value: a scheme followed by key="value" pairs.
func parseChallenge(v string) (string, map[string]string) {
	scheme, rest, _ := strings.Cut(strings.TrimSpace(v), " ")
	params := map[string]string{}
	for rest = strings.TrimSpace(rest); rest != ""; {
		k, after, ok := strings.Cut(rest, "=")
		if !ok {
			break
		}
		k = strings.ToLower(strings.TrimSpace(k))
		after = strings.TrimLeft(after, " ")
		var val string
		if strings.HasPrefix(after, `"`) {
			end := strings.Index(after[1:], `"`)
			if end < 0 {
				break
			}
			val, rest = after[1:1+end], after[2+end:]
		} else {
			val, rest, _ = strings.Cut(after, ",")
			val = strings.TrimSpace(val)
			rest = "," + rest
		}
		params[k] = val
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), ","))
	}
	return strings.ToLower(scheme), params
}
