package changelog

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	cacheVersion  = 1
	maxCacheBytes = 32 << 20
)

type cacheFile struct {
	Version   int       `json:"version"`
	Repo      string    `json:"repo"`
	FetchedAt time.Time `json:"fetched_at"`
	Releases  []Release `json:"releases"`
}

// cachePath is keyed by repository only. Owner names cannot contain "_", so the key is unambiguous.
func (c *Client) cachePath(repo Repo) string {
	return filepath.Join(c.opts.CacheDir, strings.ToLower(repo.Owner)+"_"+strings.ToLower(repo.Name)+".json")
}

// readCache returns the cache of repo; an unreadable or foreign file counts as no cache.
func (c *Client) readCache(repo Repo) (cacheFile, bool) {
	if c.opts.CacheDir == "" {
		return cacheFile{}, false
	}
	f, err := os.Open(c.cachePath(repo))
	if err != nil {
		return cacheFile{}, false
	}
	defer f.Close()
	var cf cacheFile
	if err := json.NewDecoder(io.LimitReader(f, maxCacheBytes)).Decode(&cf); err != nil {
		return cacheFile{}, false
	}
	if cf.Version != cacheVersion || !strings.EqualFold(cf.Repo, repo.String()) {
		return cacheFile{}, false
	}
	// The cache is local but not trusted more than the network.
	for i, r := range cf.Releases {
		cf.Releases[i].Tag = oneLine(Sanitize(r.Tag))
		cf.Releases[i].Name = oneLine(Sanitize(r.Name))
		cf.Releases[i].Body = Sanitize(r.Body)
		cf.Releases[i].URL = safeLink(r.URL)
	}
	return cf, true
}

// writeCache stores the cache atomically. A failure only costs the cache, so it is not reported.
func (c *Client) writeCache(repo Repo, cf cacheFile) {
	if c.opts.CacheDir == "" {
		return
	}
	data, err := json.Marshal(cf)
	if err != nil || os.MkdirAll(c.opts.CacheDir, 0o700) != nil {
		return
	}
	tmp, err := os.CreateTemp(c.opts.CacheDir, ".tmp-*")
	if err != nil {
		return
	}
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if tmp.Chmod(0o600) != nil {
		return
	}
	if _, err := tmp.Write(data); err != nil || tmp.Sync() != nil || tmp.Close() != nil {
		return
	}
	if os.Rename(tmp.Name(), c.cachePath(repo)) == nil {
		ok = true
	}
}
