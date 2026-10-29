package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/changelog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/source"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/version"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const maxToLength = 64

var toRe = regexp.MustCompile(`^v?[0-9A-Za-z][0-9A-Za-z.+_-]*$`)

func (s *Service) registerChangelog() error {
	// The request is logged under the credential use action: it may read a token of the vault.
	return s.server.Register(control.OpProjectsChangelog, daemon.Handler{Action: auditlog.OperationCredentialUse, Func: s.handleProjectChangelog})
}

func (s *Service) handleProjectChangelog(ctx context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	p, err := decodeParams(params, func(p control.ChangelogParams) string { return p.Project })
	if err != nil {
		return nil, err
	}
	fail := func(code string) (any, error) {
		c.Audit(p.Project, code)
		return nil, &control.Error{Code: code}
	}
	if p.Image != "" && !config.ValidName(p.Image) {
		return fail(control.CodeBadRequest)
	}
	if p.To != "" {
		if _, ok := version.ParseCore(p.To); !ok || len(p.To) > maxToLength || !toRe.MatchString(p.To) {
			return fail(control.CodeBadRequest)
		}
	}
	if s.vaultState() != control.VaultUnlocked {
		return fail(control.CodeLocked)
	}
	ap, err := s.approvals.Approved(p.Project)
	switch {
	case errors.Is(err, vault.ErrLocked):
		return fail(control.CodeLocked)
	case errors.Is(err, approval.ErrNotApproved), errors.Is(err, approval.ErrIntegrity):
		return fail(control.CodeNotApproved)
	case err != nil:
		c.Audit(p.Project, control.CodeInternal)
		return nil, internalError("cannot read the approved state", err)
	}
	proj := ap.Project
	if p.Image != "" && !hasImage(proj, p.Image) {
		return fail(control.CodeBadRequest)
	}

	ctx, cancel := context.WithTimeout(ctx, control.ChangelogTimeout)
	defer cancel()
	creds := &approvedCredentials{vault: s.vault, ids: ap.Credentials}
	r := &changelogRun{
		checker: &source.Checker{Credentials: creds, Registry: s.cfg.SourceRegistry, Git: s.cfg.SourceGit},
		creds:   creds,
		opts:    s.cfg.Changelog,
		to:      p.To,
	}
	r.opts.CacheDir = filepath.Join(s.cfg.StateDir, "changelogs")

	out := control.ChangelogResult{Project: p.Project, Sources: []control.ChangelogSource{}}
	for _, img := range proj.Images {
		if p.Image == "" || p.Image == img.Name {
			out.Sources = append(out.Sources, r.image(ctx, proj, img))
		}
	}
	if proj.Git != nil && p.Image == "" {
		out.Sources = append(out.Sources, r.git(ctx, proj))
	}
	limitChangelog(out.Sources)
	code := ""
	for _, src := range out.Sources {
		if src.Error != "" {
			code = src.Error
			break
		}
	}
	c.Audit(p.Project, code)
	return out, nil
}

func hasImage(proj config.Project, name string) bool {
	for _, img := range proj.Images {
		if img.Name == name {
			return true
		}
	}
	return false
}

type changelogRun struct {
	checker *source.Checker
	creds   *approvedCredentials
	opts    changelog.Options
	to      string
}

func (r *changelogRun) image(ctx context.Context, proj config.Project, img config.Image) control.ChangelogSource {
	res := control.ChangelogSource{Kind: control.SourceImage, Name: img.Name, Ref: img.Ref, Releases: []control.ChangelogRelease{}}
	if _, err := registry.ParseRepo(img.Ref); err != nil {
		res.Error, res.Message = control.SourceInvalid, "the image reference is not valid"
		return res
	}
	current, problem := currentImageTag(proj, img)
	res.Current = current
	if problem != nil {
		res.Problems = append(res.Problems, *problem)
	}
	ctx, cancel := context.WithTimeout(ctx, perSourceTimeout)
	defer cancel()

	repo := img.VersionRule.ChangelogRepo
	target := r.to
	// The registry is only asked for what is still missing: the target or the repository label.
	if target == "" || repo == "" {
		checked, err := r.checker.CheckImage(ctx, img, current)
		if err != nil {
			res.Error, res.Message = classify(ctx, err, r.creds.secrets)
			return res
		}
		for _, pr := range checked.Problems {
			res.Problems = append(res.Problems, control.Problem{Code: pr.Code, Message: pr.Message, Hint: pr.Hint})
		}
		if target == "" {
			target = checked.Target
		}
		if repo == "" {
			repo = checked.SourceRepo
		}
	}
	r.releases(ctx, &res, img.VersionRule, repo, target)
	return res
}

func (r *changelogRun) git(ctx context.Context, proj config.Project) control.ChangelogSource {
	g := proj.Git
	res := control.ChangelogSource{Kind: control.SourceGit, Name: "git", Ref: displayURL(g.URL), Releases: []control.ChangelogRelease{}}
	ctx, cancel := context.WithTimeout(ctx, perSourceTimeout)
	defer cancel()
	current, problem := currentGitTag(ctx, r.checker, proj)
	res.Current = current
	if problem != nil {
		res.Problems = append(res.Problems, *problem)
	}

	repo := g.VersionRule.ChangelogRepo
	if repo == "" {
		repo = g.URL
	}
	target := r.to
	if target == "" {
		checked, err := r.checker.CheckGit(ctx, *g, current)
		if err != nil {
			res.Error, res.Message = classify(ctx, err, r.creds.secrets)
			return res
		}
		for _, pr := range checked.Problems {
			res.Problems = append(res.Problems, control.Problem{Code: pr.Code, Message: pr.Message, Hint: pr.Hint})
		}
		target = checked.Target
	}
	r.releases(ctx, &res, g.VersionRule, repo, target)
	return res
}

// releases fills res with the releases of repo, when it is a GitHub repository. Any other address only
// yields a link.
func (r *changelogRun) releases(ctx context.Context, res *control.ChangelogSource, rule config.VersionRule, repo, target string) {
	res.Target = target
	src := changelog.ParseSource(repo)
	res.Link = src.Link
	if !src.GitHub {
		return
	}
	res.Repo = src.Repo.String()
	opts := r.opts
	if name := rule.ChangelogCredential; name != "" {
		token, err := r.creds.Secret(name)
		if err != nil {
			res.Error, res.Message = classify(ctx, err, r.creds.secrets)
			return
		}
		opts.Token = token
	}
	got, err := changelog.New(opts).Changelogs(ctx, src.Repo, res.Current, target)
	if err != nil {
		res.Error, res.Message = changelogError(ctx, err, r.creds.secrets)
		return
	}
	res.Link, res.Stale, res.Notice = got.Link, got.Stale, scrub(got.Notice, r.creds.secrets)
	for _, rel := range got.Releases {
		res.Releases = append(res.Releases, control.ChangelogRelease{Version: rel.Version, Tag: rel.Tag, Name: rel.Name,
			Body: rel.Body, URL: rel.URL, Published: rel.Published})
	}
	res.ReleasesTotal = len(res.Releases)
}

// changelogError turns the error of the release lookup into a fixed code and a message without secrets.
func changelogError(ctx context.Context, err error, secrets []string) (code, message string) {
	switch {
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded):
		return control.SourceTimeout, "GitHub did not answer in time"
	case errors.Is(err, changelog.ErrRateLimited):
		return control.ChangelogRateLimited, "GitHub rate limit reached and no earlier answer is cached; set changelog_credential to raise the limit"
	case errors.Is(err, changelog.ErrDenied):
		return control.SourceDenied, "access was denied; check the credential and its permissions"
	case errors.Is(err, changelog.ErrNotFound):
		return control.SourceNotFound, "the repository does not exist or is not visible to the credential"
	}
	return control.SourceUnreachable, "GitHub could not be read: " + scrub(err.Error(), secrets)
}

// limitChangelog keeps the newest releases of each source within the count and size limits of a result, so
// the answer fits the response limit.
func limitChangelog(sources []control.ChangelogSource) {
	budget := control.MaxChangelogBytes
	for i := range sources {
		s := &sources[i]
		keep := 0
		for _, rel := range s.Releases {
			size := len(rel.Body) + len(rel.Name) + len(rel.URL) + len(rel.Tag) + len(rel.Version) + 128
			if keep == control.MaxChangelogReleases || size > budget {
				break
			}
			budget -= size
			keep++
		}
		s.Releases = s.Releases[:keep]
	}
}
