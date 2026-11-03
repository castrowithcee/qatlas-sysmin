//go:build e2efakes

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var releaseRoot = "/srv/releases"

// qsysminRepo is the only repository the fake knows; its releases come from the directories under releaseRoot
// (one directory per tag holding the release assets).
const qsysminRepo = "castrowithcee/qatlas-sysmin"

type apiAsset struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	BrowserURL  string `json:"browser_download_url"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

type apiRelease struct {
	TagName     string     `json:"tag_name"`
	Name        string     `json:"name"`
	Draft       bool       `json:"draft"`
	Prerelease  bool       `json:"prerelease"`
	Body        string     `json:"body"`
	HTMLURL     string     `json:"html_url"`
	PublishedAt string     `json:"published_at"`
	Assets      []apiAsset `json:"assets"`
}

var (
	apiPath      = regexp.MustCompile(`^/repos/([^/]+/[^/]+)/releases(?:/(latest)|/tags/([^/]+)|/assets/([0-9]+))?$`)
	downloadPath = regexp.MustCompile(`^(?:/[^/]+/[^/]+)?/releases/(?:download/([^/]+)|latest/download)/([^/]+)$`)
	latestPath   = regexp.MustCompile(`^((?:/[^/]+/[^/]+)?/releases)/latest$`)
	tagPath      = regexp.MustCompile(`^(?:/[^/]+/[^/]+)?/releases/tag/([^/]+)$`)
)

// runReleases serves the GitHub releases API subset qsysmin uses (api.github.com), the release download
// paths of github.com and the install script, all from the same data under /srv/releases.
func runReleases() error {
	return serveTLS(":443", http.HandlerFunc(serveReleases))
}

func serveReleases(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/install.sh":
		serveFile(w, r, filepath.Join(releaseRoot, "install.sh"))
	case apiPath.MatchString(path):
		m := apiPath.FindStringSubmatch(path)
		serveAPI(w, r, m[1], m[2], m[3], m[4])
	case downloadPath.MatchString(path):
		m := downloadPath.FindStringSubmatch(path)
		tag := m[1]
		if tag == "" {
			tag = latestTag()
		}
		serveFile(w, r, filepath.Join(releaseRoot, filepath.Base(tag), filepath.Base(m[2])))
	case latestPath.MatchString(path):
		tag := latestTag()
		if tag == "" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, latestPath.FindStringSubmatch(path)[1]+"/tag/"+tag, http.StatusFound)
	case tagPath.MatchString(path):
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

func serveFile(w http.ResponseWriter, r *http.Request, path string) {
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, path)
}

func serveAPI(w http.ResponseWriter, r *http.Request, repo, latest, tag, assetID string) {
	releases, ok := releasesOf(repo, requestScheme(r), r.Host)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	switch {
	case assetID != "":
		id, _ := strconv.Atoi(assetID)
		for _, a := range assetFiles() {
			if a.id == id {
				serveFile(w, r, filepath.Join(releaseRoot, a.tag, a.name))
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	case latest != "":
		for _, rel := range releases {
			if !rel.Prerelease && !rel.Draft {
				writeJSON(w, http.StatusOK, rel)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	case tag != "":
		for _, rel := range releases {
			if rel.TagName == tag {
				writeJSON(w, http.StatusOK, rel)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
	default:
		writeJSON(w, http.StatusOK, releases)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func requestScheme(r *http.Request) string {
	if r.TLS == nil {
		return "http"
	}
	return "https"
}

// assetFile is one regular file of a release. GitHub addresses assets by a numeric ID, so the fake numbers
// the files in the order of tagDirs and then by name.
type assetFile struct {
	id        int
	tag, name string
	size      int64
}

func assetFiles() []assetFile {
	var out []assetFile
	for _, tag := range tagDirs() {
		entries, _ := os.ReadDir(filepath.Join(releaseRoot, tag))
		for _, e := range entries {
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			out = append(out, assetFile{id: len(out) + 1, tag: tag, name: e.Name(), size: info.Size()})
		}
	}
	return out
}

// releasesOf returns the releases of repo, newest first. Asset URLs point back at host under the numeric
// asset path of the API, so a client that only trusts its configured origin follows them.
func releasesOf(repo, scheme, host string) ([]apiRelease, bool) {
	if repo != qsysminRepo {
		return nil, false
	}
	tags := tagDirs()
	files := assetFiles()
	out := make([]apiRelease, 0, len(tags))
	for _, tag := range tags {
		rel := apiRelease{
			TagName: tag, Name: "qsysmin " + tag, Prerelease: strings.Contains(tag, "-"),
			Body:        "Invented test release " + tag + ".",
			HTMLURL:     scheme + "://" + host + "/" + repo + "/releases/tag/" + tag,
			PublishedAt: "2026-04-01T10:00:00Z",
		}
		for _, f := range files {
			if f.tag != tag {
				continue
			}
			rel.Assets = append(rel.Assets, apiAsset{
				Name:        f.name,
				URL:         scheme + "://" + host + "/repos/" + repo + "/releases/assets/" + strconv.Itoa(f.id),
				BrowserURL:  scheme + "://" + host + "/" + repo + "/releases/download/" + tag + "/" + f.name,
				Size:        f.size,
				ContentType: "application/octet-stream",
			})
		}
		out = append(out, rel)
	}
	return out, true
}

// tagDirs lists the release directories, newest version first.
func tagDirs() []string {
	entries, _ := os.ReadDir(releaseRoot)
	var tags []string
	for _, e := range entries {
		if e.IsDir() {
			if _, ok := parseTag(e.Name()); ok {
				tags = append(tags, e.Name())
			}
		}
	}
	sort.Slice(tags, func(i, j int) bool {
		a, _ := parseTag(tags[i])
		b, _ := parseTag(tags[j])
		for k := range a {
			if a[k] != b[k] {
				return a[k] > b[k]
			}
		}
		// A prerelease sorts below its release.
		preI, preJ := strings.Contains(tags[i], "-"), strings.Contains(tags[j], "-")
		return !preI && preJ || preI == preJ && tags[i] > tags[j]
	})
	return tags
}

func parseTag(tag string) ([3]int, bool) {
	var v [3]int
	core, _, _ := strings.Cut(strings.TrimPrefix(tag, "v"), "-")
	parts := strings.Split(core, ".")
	if !strings.HasPrefix(tag, "v") || len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// latestTag is the newest stable tag.
func latestTag() string {
	for _, t := range tagDirs() {
		if !strings.Contains(t, "-") {
			return t
		}
	}
	return ""
}
