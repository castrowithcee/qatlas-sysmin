package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/approval"
	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/execute"
	"github.com/castrowithcee/qatlas-sysmin/internal/source"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/gitrepo"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry"
	"github.com/castrowithcee/qatlas-sysmin/internal/source/version"
	"github.com/castrowithcee/qatlas-sysmin/internal/vault"
)

const (
	// perSourceTimeout bounds one source, so a slow one leaves time for the others.
	perSourceTimeout = time.Minute
	maxCurrentLength = 128
	maxMessageLength = 300
)

// errCredentialUnavailable marks a credential that the approved state does not provide.
var errCredentialUnavailable = errors.New("the credential is not available in the approved state")

func (s *Service) registerSources() error {
	// The request is logged under the credential use action: it reads secrets of the vault.
	return s.server.Register(control.OpSourceTest, daemon.Handler{Action: auditlog.OperationCredentialUse, Func: s.handleSourceTest})
}

// approvedCredentials hands out the vault entries that the approved state of a project names, by the vault
// id that was approved. A credential name is never looked up in the vault, so a credential or a project
// changed afterwards cannot send a secret anywhere the approved state did not.
type approvedCredentials struct {
	vault *vault.Vault
	ids   map[string]string
	// secrets collects every secret value handed out, so messages can be scrubbed.
	secrets []string
}

func (a *approvedCredentials) entry(name string, kinds ...vault.Kind) (vault.Entry, error) {
	id, ok := a.ids[name]
	if !ok {
		return vault.Entry{}, fmt.Errorf("%w: %q was not part of the approval", errCredentialUnavailable, name)
	}
	e, err := a.vault.Get(id)
	if err != nil {
		return vault.Entry{}, fmt.Errorf("%w: %q cannot be read", errCredentialUnavailable, name)
	}
	for _, k := range kinds {
		if e.Kind == k {
			return e, nil
		}
	}
	return vault.Entry{}, fmt.Errorf("%w: %q is of kind %s", errCredentialUnavailable, name, e.Kind)
}

func (a *approvedCredentials) value(e vault.Entry, field string) string {
	v := e.Fields[field].Reveal()
	a.secrets = append(a.secrets, v)
	return v
}

func (a *approvedCredentials) Registry(name string) (registry.Credential, error) {
	e, err := a.entry(name, vault.KindRegistry)
	if err != nil {
		return registry.Credential{}, err
	}
	return registry.Credential{User: a.value(e, vault.FieldUser), Token: a.value(e, vault.FieldToken)}, nil
}

func (a *approvedCredentials) Git(name string) (gitrepo.Credential, error) {
	e, err := a.entry(name, vault.KindGitHTTPS, vault.KindSSHKey)
	if err != nil {
		return gitrepo.Credential{}, err
	}
	if e.Kind == vault.KindSSHKey {
		return gitrepo.Credential{Kind: gitrepo.KindSSH, PrivateKey: a.value(e, vault.FieldPrivateKey), HostKey: a.value(e, vault.FieldHostKey)}, nil
	}
	return gitrepo.Credential{Kind: gitrepo.KindHTTPS, User: a.value(e, vault.FieldUser), Token: a.value(e, vault.FieldToken)}, nil
}

// Secret hands out the value of a credential of kind secret.
func (a *approvedCredentials) Secret(name string) (string, error) {
	e, err := a.entry(name, vault.KindSecret)
	if err != nil {
		return "", err
	}
	return a.value(e, vault.FieldValue), nil
}

func (s *Service) handleSourceTest(ctx context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	p, err := decodeParams(params, func(p control.SourceTestParams) string { return p.Project })
	if err != nil {
		return nil, err
	}
	fail := func(code string) (any, error) {
		c.Audit(p.Project, code)
		return nil, &control.Error{Code: code}
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

	ctx, cancel := context.WithTimeout(ctx, control.SourceTestTimeout)
	defer cancel()
	creds := &approvedCredentials{vault: s.vault, ids: ap.Credentials}
	checker := &source.Checker{Credentials: creds, Registry: s.cfg.SourceRegistry, Git: s.cfg.SourceGit}
	proj := ap.Project

	out := control.SourceTestResult{Project: p.Project, Sources: []control.SourceResult{}}
	for _, img := range proj.Images {
		out.Sources = append(out.Sources, testImage(ctx, checker, creds, proj, img))
	}
	if proj.Git != nil {
		out.Sources = append(out.Sources, testGit(ctx, checker, creds, proj))
	}
	code := ""
	for _, r := range out.Sources {
		if r.Error != "" {
			code = r.Error
			break
		}
	}
	c.Audit(p.Project, code)
	return out, nil
}

func testImage(ctx context.Context, checker *source.Checker, creds *approvedCredentials, proj config.Project, img config.Image) control.SourceResult {
	res := control.SourceResult{Kind: control.SourceImage, Name: img.Name, Ref: img.Ref}
	if _, err := registry.ParseRepo(img.Ref); err != nil {
		res.Error, res.Message = control.SourceInvalid, "the image reference is not valid"
		return res
	}
	current, problem := currentImageTag(proj, img)
	ctx, cancel := context.WithTimeout(ctx, perSourceTimeout)
	defer cancel()
	r, err := checker.CheckImage(ctx, img, current)
	if err != nil {
		res.Error, res.Message = classify(ctx, err, creds.secrets)
		return res
	}
	fillSelection(&res, r.Selection, problem)
	res.Digest, res.SourceRepo = r.Digest, r.SourceRepo
	return res
}

func testGit(ctx context.Context, checker *source.Checker, creds *approvedCredentials, proj config.Project) control.SourceResult {
	g := proj.Git
	res := control.SourceResult{Kind: control.SourceGit, Name: "git", Ref: displayURL(g.URL)}
	ctx, cancel := context.WithTimeout(ctx, perSourceTimeout)
	defer cancel()
	current, problem := currentGitTag(ctx, checker, proj)
	r, err := checker.CheckGit(ctx, *g, current)
	if err != nil {
		res.Error, res.Message = classify(ctx, err, creds.secrets)
		return res
	}
	fillSelection(&res, r.Selection, problem)
	return res
}

func fillSelection(res *control.SourceResult, sel version.Selection, unknown *control.Problem) {
	res.Current, res.Target, res.AvailableTotal = sel.Current, sel.Target, len(sel.Available)
	res.Available = sel.Available
	if n := len(res.Available); n > control.MaxAvailable {
		res.Available = res.Available[n-control.MaxAvailable:]
	}
	if unknown != nil {
		res.Problems = append(res.Problems, *unknown)
	}
	for _, p := range sel.Problems {
		res.Problems = append(res.Problems, control.Problem{Code: p.Code, Message: p.Message, Hint: p.Hint})
	}
}

// currentImageTag reads the tag an image is on from the project's env file, which is where the update steps
// set it. The file is only read, never through a link, and not above 1 MiB.
func currentImageTag(proj config.Project, img config.Image) (string, *control.Problem) {
	unknown := func(msg, hint string) (string, *control.Problem) {
		return "", &control.Problem{Code: control.CodeCurrentUnknown, Message: msg, Hint: hint}
	}
	switch {
	case img.EnvVar == "":
		return unknown("the image sets no env_var, so its current tag is not known", "Set env_var on the image.")
	case proj.EnvFile == "":
		return unknown("the project sets no env_file, so the current tag is not known", "Set env_file to the file that holds "+img.EnvVar+".")
	}
	v, found, err := execute.ReadEnvValue(proj.EnvFile, img.EnvVar)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return unknown("the env file does not exist", "Create the env file with "+img.EnvVar+" set to the running tag.")
	case err != nil:
		return unknown("the env file cannot be read", "Check that env_file is a regular file the service can read.")
	case !found || v == "":
		return unknown(img.EnvVar+" is not set in the env file", "Set "+img.EnvVar+" to the running tag.")
	case len(v) > maxCurrentLength:
		return unknown(img.EnvVar+" does not hold a tag", "Set "+img.EnvVar+" to the running tag.")
	}
	return v, nil
}

// currentGitTag reads the tag the working copy in the project directory has checked out exactly.
func currentGitTag(ctx context.Context, checker *source.Checker, proj config.Project) (string, *control.Problem) {
	unknown := func(msg string) (string, *control.Problem) {
		return "", &control.Problem{Code: control.CodeCurrentUnknown, Message: msg,
			Hint: "Check out a release tag in the project directory."}
	}
	tag, err := gitrepo.New(checker.Git).CurrentTag(ctx, proj.Dir)
	switch {
	case err != nil:
		return unknown("the checked-out tag cannot be read: " + scrub(strings.ReplaceAll(err.Error(), proj.Dir, "<dir>"), nil))
	case tag == "":
		return unknown("the working copy is not on a tag")
	}
	return tag, nil
}

// classify turns the error of a source into a fixed code and a message without secrets. Only the text of an
// unreachable source is passed on, after the secret values handed out are removed from it.
func classify(ctx context.Context, err error, secrets []string) (code, message string) {
	text := strings.ToLower(err.Error())
	switch {
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded):
		return control.SourceTimeout, "the source did not answer in time"
	case errors.Is(err, errCredentialUnavailable), errors.Is(err, gitrepo.ErrAuth):
		return control.SourceCredential, "the credential of the source cannot be used: " + scrub(err.Error(), secrets)
	case errors.Is(err, registry.ErrDenied), containsAny(text, "authentication failed", "could not read username", "could not read password", "permission denied", "access denied"):
		return control.SourceDenied, "access was denied; check the credential and its permissions"
	case errors.Is(err, registry.ErrNotFound), containsAny(text, "repository not found", "does not appear to be a git repository"):
		return control.SourceNotFound, "the repository does not exist or is not visible to the credential"
	}
	return control.SourceUnreachable, "the source could not be read: " + scrub(err.Error(), secrets)
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

var userinfoRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/@\s'"]*@`)

// scrub removes the secret values and URL user information from text and shortens it.
func scrub(text string, secrets []string) string {
	text = userinfoRe.ReplaceAllString(text, "${1}")
	for _, secret := range secrets {
		if len(secret) >= 3 {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > maxMessageLength {
		text = text[:maxMessageLength] + "..."
	}
	return text
}

// displayURL drops the user information of a repository URL, including the user of an scp-like address.
func displayURL(raw string) string {
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil {
			u.User = nil
			return u.String()
		}
		return userinfoRe.ReplaceAllString(raw, "${1}")
	}
	if _, rest, ok := strings.Cut(raw, "@"); ok {
		return rest
	}
	return raw
}
