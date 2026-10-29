// Package selfupdate lists and installs signed sysmin releases. A release is installed only after the
// signature over its checksums.txt and the archive checksum have been verified; the installed program is then
// replaced atomically.
package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/release"
)

const (
	// RepositoryAPI is the GitHub API base of the release repository.
	RepositoryAPI = "https://api.github.com/repos/castrowithcee/qatlas-sysmin"
	// DefaultTarget is the installed program the update replaces.
	DefaultTarget = "/usr/local/bin/sysmin"

	maxMetadataBytes  = 1 << 20
	maxArchiveBytes   = 128 << 20
	maxExecutableSize = 64 << 20
	// requestTimeout only bounds a request that keeps trickling; a slow download that moves goes on.
	requestTimeout = 30 * time.Minute
	// stallTimeout ends a request that receives no byte for this long, waiting for the answer included.
	stallTimeout = 30 * time.Second

	checksumsAsset  = "checksums.txt"
	signatureAsset  = "checksums.txt.sig"
	executableEntry = "bin/sysmin"
)

// statusError reports a non-2xx answer of the release source.
type statusError struct {
	Code   int
	Status string
}

func (e *statusError) Error() string { return "release source returned " + e.Status }

// stallTimer is the part of *time.Timer the stall watchdog uses.
type stallTimer interface {
	Reset(d time.Duration) bool
	Stop() bool
}

// newStallTimer is a seam so a test can fire the watchdog without waiting on the wall clock.
var newStallTimer = func(d time.Duration, f func()) stallTimer { return time.AfterFunc(d, f) }

// ErrDevelopmentBuild reports an update attempt by a build without a release version.
var ErrDevelopmentBuild = errors.New("self-update is unavailable in a dev build; install a tagged release first")

// UnsupportedInstallationError reports an installation that cannot be replaced safely in place.
type UnsupportedInstallationError struct{ Reason string }

func (e *UnsupportedInstallationError) Error() string {
	return "cannot self-update this installation: " + e.Reason
}

// Result is the outcome of an installation.
type Result struct {
	Previous string
	Version  string
	Updated  bool
}

// Client reads GitHub Releases and replaces one installed program. Zero values of the exported fields
// select the production repository, platform, target, and HTTP client.
type Client struct {
	// BaseURL replaces the GitHub API base; empty means the production repository. It never changes
	// which keys are trusted.
	BaseURL    string
	HTTPClient *http.Client
	// Version is the running version.
	Version string
	// GOARCH overrides the platform architecture; tests only.
	GOARCH string
	// Target is the program to replace; empty means DefaultTarget.
	Target string
	// SigningKeys replaces the compiled-in release keys; tests only.
	SigningKeys []ssh.PublicKey
	// Progress, when set, is called while the archive downloads with the bytes received and the archive's
	// size, or -1 when the server does not say.
	Progress func(done, total int64)
}

// New returns the production updater for version, reading releases from baseURL or, when it is empty,
// from GitHub.
func New(version, baseURL string) *Client { return &Client{Version: version, BaseURL: baseURL} }

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name       string `json:"name"`
		APIURL     string `json:"url"`
		BrowserURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func (c *Client) currentVersion() (semver, error) {
	if c.Version == "" || c.Version == "dev" {
		return semver{}, ErrDevelopmentBuild
	}
	parsed, err := parseSemver(c.Version)
	if err != nil {
		return semver{}, fmt.Errorf("running build has an invalid version: %w", err)
	}
	return parsed, nil
}

func (c *Client) baseURL() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = RepositoryAPI
	}
	return base
}

func (c *Client) target() string {
	if c.Target != "" {
		return c.Target
	}
	return DefaultTarget
}

func (c *Client) arch() (string, error) {
	arch := c.GOARCH
	if arch == "" {
		arch = runtime.GOARCH
	}
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("no release is published for architecture %s", arch)
	}
	return arch, nil
}

func assetName(version, arch string) string {
	return fmt.Sprintf("sysmin_%s_linux_%s.tar.gz", version, arch)
}

func (c *Client) getJSON(ctx context.Context, endpoint string, out any) error {
	body, err := c.request(ctx, endpoint, maxMetadataBytes, "application/vnd.github+json", nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// latestStable returns the newest stable release, which GitHub reports without drafts and prereleases.
func (c *Client) latestStable(ctx context.Context) (githubRelease, error) {
	var r githubRelease
	if err := c.getJSON(ctx, c.baseURL()+"/releases/latest", &r); err != nil {
		return githubRelease{}, fmt.Errorf("check latest release: %w", err)
	}
	if r.TagName == "" {
		return githubRelease{}, errors.New("latest release does not declare a tag")
	}
	return r, nil
}

func (c *Client) download(ctx context.Context, rawURL string, limit int64, progress func(done, total int64)) ([]byte, error) {
	if err := c.validateAssetURL(rawURL); err != nil {
		return nil, err
	}
	return c.request(ctx, rawURL, limit, "application/octet-stream", progress)
}

func (c *Client) request(ctx context.Context, rawURL string, limit int64, accept string, progress func(done, total int64)) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var stalled atomic.Bool
	stall := newStallTimer(stallTimeout, func() {
		stalled.Store(true)
		cancel()
	})
	defer stall.Stop()
	body, err := c.fetch(ctx, rawURL, limit, accept, func(done, total int64) {
		stall.Reset(stallTimeout)
		if progress != nil {
			progress(done, total)
		}
	})
	if err != nil && stalled.Load() {
		return nil, fmt.Errorf("no data received for %s", stallTimeout)
	}
	return body, err
}

func (c *Client) fetch(ctx context.Context, rawURL string, limit int64, accept string, received func(done, total int64)) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "sysmin/"+c.Version)
	client := c.HTTPClient
	if client == nil {
		// No client timeout: it would also cut off a slow download that still moves; request bounds it.
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &statusError{Code: resp.StatusCode, Status: resp.Status}
	}
	reader := &progressReader{r: resp.Body, total: resp.ContentLength, received: received}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return body, nil
}

// progressReader reports every read that brings bytes, with the running count and the announced total.
type progressReader struct {
	r        io.Reader
	done     int64
	total    int64
	received func(done, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.done += int64(n)
		p.received(p.done, p.total)
	}
	return n, err
}

// validateAssetURL accepts GitHub's HTTPS hosts for the production repository and, for any other base, only
// the base's own origin.
func (c *Client) validateAssetURL(rawURL string) error {
	asset, err := url.Parse(rawURL)
	if err != nil || asset.Host == "" {
		return errors.New("release contains an invalid asset URL")
	}
	base, err := url.Parse(c.baseURL())
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") {
		return errors.New("updater has an invalid release source URL")
	}
	if c.baseURL() == RepositoryAPI {
		if asset.Scheme != "https" || !(asset.Host == "github.com" || strings.HasSuffix(asset.Host, ".github.com") || strings.HasSuffix(asset.Host, ".githubusercontent.com")) {
			return errors.New("release asset URL is outside trusted GitHub HTTPS hosts")
		}
		return nil
	}
	if asset.Scheme != base.Scheme || asset.Host != base.Host {
		return errors.New("release asset URL is outside the configured release source")
	}
	return nil
}

func releaseURLs(r githubRelease, archiveName string) (archiveURL, checksumURL, signatureURL string, err error) {
	for _, asset := range r.Assets {
		assetURL := asset.APIURL
		if assetURL == "" {
			assetURL = asset.BrowserURL
		}
		switch asset.Name {
		case archiveName:
			archiveURL = assetURL
		case checksumsAsset:
			checksumURL = assetURL
		case signatureAsset:
			signatureURL = assetURL
		}
	}
	if archiveURL == "" || checksumURL == "" || signatureURL == "" {
		return "", "", "", fmt.Errorf("release %s does not contain %s, %s, and %s", r.TagName, archiveName, checksumsAsset, signatureAsset)
	}
	return archiveURL, checksumURL, signatureURL, nil
}

// verifiedExecutable downloads the release assets of t and returns the program inside the archive. The
// signature over the checksums is verified before the archive checksum is used, and both before the archive
// is unpacked.
func (c *Client) verifiedExecutable(ctx context.Context, t Target) ([]byte, error) {
	arch, err := c.arch()
	if err != nil {
		return nil, err
	}
	archiveName := assetName(t.Version, arch)
	archiveURL, checksumURL, signatureURL, err := releaseURLs(t.rel, archiveName)
	if err != nil {
		return nil, err
	}
	checksums, err := c.download(ctx, checksumURL, maxMetadataBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("download checksums: %w", err)
	}
	signature, err := c.download(ctx, signatureURL, maxMetadataBytes, nil)
	if err != nil {
		return nil, fmt.Errorf("download checksums signature: %w", err)
	}
	keys := c.SigningKeys
	if keys == nil {
		keys = release.TrustedKeys()
	}
	if err := release.VerifyChecksums(checksums, signature, keys); err != nil {
		return nil, fmt.Errorf("release signature is invalid, nothing was changed: %w", err)
	}
	want, err := release.ChecksumFor(checksums, archiveName)
	if err != nil {
		return nil, err
	}
	archive, err := c.download(ctx, archiveURL, maxArchiveBytes, c.Progress)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", archiveName, err)
	}
	got := sha256.Sum256(archive)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return nil, fmt.Errorf("checksum mismatch for %s, nothing was changed", archiveName)
	}
	return extractExecutable(archive)
}

func extractExecutable(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open release archive: %w", err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("release archive does not contain " + executableEntry)
		}
		if err != nil {
			return nil, fmt.Errorf("read release archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg || strings.TrimPrefix(header.Name, "./") != executableEntry {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(reader, maxExecutableSize+1))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", executableEntry, err)
		}
		if len(body) > maxExecutableSize {
			return nil, fmt.Errorf("%s exceeds %d bytes", executableEntry, maxExecutableSize)
		}
		if len(body) == 0 {
			return nil, errors.New(executableEntry + " in the release archive is empty")
		}
		return body, nil
	}
}

// Install downloads and verifies the release t names and replaces the target program with the program in
// it. Any failure before the final rename leaves the target untouched. Installing the running version
// changes nothing.
func (c *Client) Install(ctx context.Context, t Target) (Result, error) {
	result := Result{Previous: c.Version, Version: t.Version}
	if t.Current {
		return result, nil
	}
	target, err := filepath.Abs(c.target())
	if err != nil {
		return Result{}, fmt.Errorf("resolve target path: %w", err)
	}
	info, err := os.Lstat(target)
	if err != nil {
		return Result{}, fmt.Errorf("inspect installed program: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Result{}, &UnsupportedInstallationError{Reason: target + " must be a regular file, not a symlink"}
	}
	executable, err := c.verifiedExecutable(ctx, t)
	if err != nil {
		return Result{}, err
	}
	staged, err := stageFile(filepath.Dir(target), ".sysmin-update-*", executable, 0o755)
	if err != nil {
		return Result{}, fmt.Errorf("stage new program: %w", err)
	}
	defer os.Remove(staged)
	if err := os.Rename(staged, target); err != nil {
		return Result{}, fmt.Errorf("replace program: %w", err)
	}
	syncDir(filepath.Dir(target))
	result.Updated = true
	return result, nil
}

func stageFile(dir, pattern string, body []byte, mode os.FileMode) (path string, err error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path = file.Name()
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(path)
		}
	}()
	if err = file.Chmod(mode); err != nil {
		return "", err
	}
	if _, err = file.Write(body); err != nil {
		return "", err
	}
	return path, file.Sync()
}

// syncDir makes the rename durable; a failure leaves a correct installation, so it is not reported.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
}
