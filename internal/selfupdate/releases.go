package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
)

const (
	// MinVersion is the oldest release that can be listed or installed.
	MinVersion = "v0.1.0"
	// listedStable is how many of the newest stable releases the list shows.
	listedStable = 10
	// maxReleasePages bounds the pagination so a misbehaving server cannot keep the reader looping.
	maxReleasePages = 20
)

// releasePageSize is a variable so a test can paginate with few releases.
var releasePageSize = 100

// TargetError reports a release that cannot be chosen, with the reason in words for the person.
type TargetError struct{ Reason string }

func (e *TargetError) Error() string { return e.Reason }

// Entry is one release a person can choose.
type Entry struct {
	Version    string
	Prerelease bool
	// Current marks the running version.
	Current bool
	// Older marks a version older than the running one.
	Older bool
}

// Target is a release chosen for installation, resolved and checked but not yet downloaded.
type Target struct {
	Version    string
	Prerelease bool
	// Current reports that Version is the running version; installing it changes nothing.
	Current bool
	// Older reports that Version is older than the running version: installing it is a rollback.
	Older bool
	rel   githubRelease
}

func minimum() semver {
	v, err := parseSemver(MinVersion)
	if err != nil {
		panic(err)
	}
	return v
}

// eligible reports whether r is a release a person may choose: published, signed, a valid version, and not
// older than MinVersion.
func eligible(r githubRelease) (semver, bool) {
	if r.Draft {
		return semver{}, false
	}
	v, err := parseSemver(r.TagName)
	if err != nil || v.compare(minimum()) < 0 {
		return semver{}, false
	}
	return v, hasSignature(r)
}

func hasSignature(r githubRelease) bool {
	for _, a := range r.Assets {
		if a.Name == signatureAsset {
			return true
		}
	}
	return false
}

// List returns the releases a person can choose, newest first: the ten newest stable releases, every
// prerelease newer than the newest stable one, and the running version when it is such a release. Only
// signed releases from MinVersion on appear.
func (c *Client) List(ctx context.Context) ([]Entry, error) {
	current, err := c.currentVersion()
	if err != nil {
		return nil, err
	}
	all, err := c.releases(ctx)
	if err != nil {
		return nil, err
	}
	type item struct {
		entry Entry
		v     semver
	}
	var items []item
	for _, r := range all {
		v, ok := eligible(r)
		if !ok {
			continue
		}
		cmp := v.compare(current)
		items = append(items, item{Entry{Version: r.TagName, Prerelease: r.Prerelease, Current: cmp == 0, Older: cmp < 0}, v})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].v.compare(items[j].v) > 0 })
	var out []Entry
	stable := 0
	for _, it := range items {
		switch {
		case it.entry.Current:
		case it.entry.Prerelease:
			// A prerelease is only offered while it is newer than the newest stable release.
			if stable > 0 {
				continue
			}
		default:
			if stable >= listedStable {
				continue
			}
		}
		if !it.entry.Prerelease {
			stable++
		}
		out = append(out, it.entry)
	}
	return out, nil
}

// releases reads every page of the repository's release list.
func (c *Client) releases(ctx context.Context) ([]githubRelease, error) {
	var all []githubRelease
	for page := 1; page <= maxReleasePages; page++ {
		endpoint := c.baseURL() + "/releases?per_page=" + strconv.Itoa(releasePageSize) + "&page=" + strconv.Itoa(page)
		var batch []githubRelease
		if err := c.getJSON(ctx, endpoint, &batch); err != nil {
			return nil, fmt.Errorf("list releases: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < releasePageSize {
			break
		}
	}
	return all, nil
}

// Resolve chooses the release to install. An empty tag selects the newest stable release; otherwise the tag
// must name a signed release from MinVersion on. A version older than the running one is allowed and marked
// Older.
func (c *Client) Resolve(ctx context.Context, tag string) (Target, error) {
	current, err := c.currentVersion()
	if err != nil {
		return Target{}, err
	}
	var r githubRelease
	if tag == "" {
		if r, err = c.latestStable(ctx); err != nil {
			return Target{}, err
		}
	} else {
		if _, err := parseSemver(tag); err != nil {
			return Target{}, &TargetError{fmt.Sprintf("%q is not a release version: %v", tag, err)}
		}
		if err := c.getJSON(ctx, c.baseURL()+"/releases/tags/"+url.PathEscape(tag), &r); err != nil {
			var se *statusError
			if errors.As(err, &se) && se.Code == http.StatusNotFound {
				return Target{}, &TargetError{fmt.Sprintf("release %s does not exist", tag)}
			}
			return Target{}, fmt.Errorf("look up release %s: %w", tag, err)
		}
		if r.TagName != tag || r.Draft {
			return Target{}, &TargetError{fmt.Sprintf("release %s does not exist", tag)}
		}
	}
	v, err := parseSemver(r.TagName)
	if err != nil {
		return Target{}, &TargetError{fmt.Sprintf("release %q has an invalid version: %v", r.TagName, err)}
	}
	if v.compare(minimum()) < 0 {
		return Target{}, &TargetError{fmt.Sprintf("%s is older than %s, the oldest version sysmin installs", r.TagName, MinVersion)}
	}
	if !hasSignature(r) {
		return Target{}, &TargetError{fmt.Sprintf("release %s is not signed and cannot be chosen", r.TagName)}
	}
	cmp := v.compare(current)
	return Target{Version: r.TagName, Prerelease: r.Prerelease, Current: cmp == 0, Older: cmp < 0, rel: r}, nil
}
