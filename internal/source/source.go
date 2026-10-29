// Package source determines, for one version source, which versions exist and which one a project should
// move to. Image sources are read from an OCI registry; the version choice itself is shared with other
// source kinds through the version package.
package source

import (
	"context"
	"errors"
	"fmt"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/gitrepo"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/version"
)

// Problem codes added by this package.
const (
	CodeDigestUnavailable = "digest-unavailable"
	CodeLabelsUnavailable = "labels-unavailable"
)

// Credentials hands out secret values by credential name. The caller owns the vault; this package only
// receives values.
type Credentials interface {
	// Registry returns the login stored under name.
	Registry(name string) (registry.Credential, error)
	// Git returns the git-https or ssh-key login stored under name, with Kind set accordingly.
	Git(name string) (gitrepo.Credential, error)
}

// ImageResult is the outcome for one image source.
type ImageResult struct {
	version.Selection
	// Digest is the manifest digest of Target; empty without a target or when it could not be read.
	Digest string
	// SourceRepo is the value of the image label org.opencontainers.image.source of Target, if present.
	SourceRepo string
}

// Checker evaluates image sources.
type Checker struct {
	// Credentials resolves Image.Credential names; it may be nil if no image uses a credential.
	Credentials Credentials
	// Registry configures the registry client; its Credential field is set per image.
	Registry registry.Options
	// Git configures the git tag lister.
	Git gitrepo.Options
}

// GitResult is the outcome for a git source.
type GitResult struct {
	version.Selection
}

// selectVersions chooses the target for current; an empty current means it is unknown, which lists the
// versions and chooses none.
func selectVersions(current string, tags []string, rule config.VersionRule) version.Selection {
	if current == "" {
		return version.List(tags, rule)
	}
	return version.Select(current, tags, rule)
}

// CheckGit lists the tags of the repository, then selects the target for the current tag (empty if unknown)
// under the source's version rule. A repository that cannot be reached or refuses access is an error.
func (c *Checker) CheckGit(ctx context.Context, g config.GitSource, current string) (GitResult, error) {
	var cred gitrepo.Credential
	if g.Credential != "" {
		if c.Credentials == nil {
			return GitResult{}, fmt.Errorf("credential %q is configured but no credential store is available", g.Credential)
		}
		var err error
		if cred, err = c.Credentials.Git(g.Credential); err != nil {
			return GitResult{}, fmt.Errorf("read git credential %q: %w", g.Credential, err)
		}
	}
	tags, err := gitrepo.New(c.Git).Tags(ctx, g.URL, cred)
	if err != nil {
		return GitResult{}, err
	}
	return GitResult{Selection: selectVersions(current, tags, g.VersionRule)}, nil
}

// CheckImage lists the tags of img.Ref, selects the target for the current tag under the image's version
// rule and reads digest and labels of the target. A registry that cannot be reached or refuses access is an
// error; a missing digest or label set is reported as a problem in the result.
func (c *Checker) CheckImage(ctx context.Context, img config.Image, current string) (ImageResult, error) {
	repo, err := registry.ParseRepo(img.Ref)
	if err != nil {
		return ImageResult{}, err
	}
	opts := c.Registry
	if img.Credential != "" {
		if c.Credentials == nil {
			return ImageResult{}, fmt.Errorf("credential %q is configured but no credential store is available", img.Credential)
		}
		if opts.Credential, err = c.Credentials.Registry(img.Credential); err != nil {
			return ImageResult{}, fmt.Errorf("read registry credential %q: %w", img.Credential, err)
		}
	}
	client := registry.New(opts)

	tags, err := client.Tags(ctx, repo)
	if err != nil {
		return ImageResult{}, err
	}
	res := ImageResult{Selection: selectVersions(current, tags, img.VersionRule)}
	if res.Target == "" {
		return res, nil
	}
	if res.Digest, err = client.Digest(ctx, repo, res.Target); err != nil {
		if ctx.Err() != nil || errors.Is(err, registry.ErrDenied) {
			return ImageResult{}, err
		}
		res.Problems = append(res.Problems, version.Problem{
			Code:    CodeDigestUnavailable,
			Message: fmt.Sprintf("digest of %s could not be read: %v", res.Target, err),
			Hint:    "Check that the registry serves manifests for this tag.",
		})
	}
	labels, err := client.Labels(ctx, repo, res.Target)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, registry.ErrDenied) {
			return ImageResult{}, err
		}
		res.Problems = append(res.Problems, version.Problem{
			Code:    CodeLabelsUnavailable,
			Message: fmt.Sprintf("labels of %s could not be read: %v", res.Target, err),
			Hint:    "Set changelog_repo in the version rule to show changelogs without image labels.",
		})
		return res, nil
	}
	res.SourceRepo = labels[registry.LabelSource]
	return res, nil
}
