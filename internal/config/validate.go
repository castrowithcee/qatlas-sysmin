package config

import (
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/source/registry"
)

var (
	nameRe         = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	credentialRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	envNameRe      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	serviceRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	composeNameRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	changelogRe    = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	versionRe      = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.+/-]{0,127}$`)
	scpGitRe       = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^\s]+$`)
	imageRefRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)
	minStepTimeout = time.Second
	maxStepTimeout = 24 * time.Hour
)

// Validate checks the whole model and returns every problem it finds, in a stable order. credentials is the
// set of credential names the vault holds; the model refers to credentials by name only.
//
// Path rule: every path in the configuration (project dir, env_file, compose_files) must be absolute and
// free of ".." elements. Nothing is resolved against the working directory or the project directory.
func Validate(m *Model, credentials []string) []Problem {
	var out []Problem
	creds := make(map[string]bool, len(credentials))
	for _, c := range credentials {
		creds[c] = true
	}

	validateSettings(scope{out: &out, file: SettingsPath()}, m.Settings, creds)

	seen := map[string]bool{}
	for _, t := range BuiltinTemplates() {
		seen[t.Name] = true
	}
	for _, t := range m.Templates {
		sc := scope{out: &out, file: TemplatePath(t.Name), template: t.Name}
		if seen[t.Name] {
			sc.add("name", CodeDuplicateName, "template name %q is already taken (embedded and custom templates share one namespace)", t.Name)
		}
		seen[t.Name] = true
		validateTemplate(sc, t)
	}

	projects := map[string]bool{}
	for _, p := range m.Projects {
		sc := scope{out: &out, file: ProjectPath(p.Name), project: p.Name}
		if projects[p.Name] {
			sc.add("name", CodeDuplicateName, "project name %q is used more than once", p.Name)
		}
		projects[p.Name] = true
		validateProject(sc, m, p, creds)
	}
	return dedupe(out)
}

func dedupe(in []Problem) []Problem {
	seen := map[string]bool{}
	var out []Problem
	for _, p := range in {
		k := strings.Join([]string{p.File, p.Project, p.Template, p.Field, p.Code, p.Message}, "\x00")
		if !seen[k] {
			seen[k] = true
			out = append(out, p)
		}
	}
	return out
}

func checkSchema(sc scope, schema int) {
	if schema != CurrentSchema {
		sc.add("schema", CodeInvalidValue, "schema version is %d, expected %d", schema, CurrentSchema)
	}
}

func checkRange(sc scope, field string, d Duration, lo, hi time.Duration) {
	if d == 0 {
		return
	}
	if v := time.Duration(d); v < lo || v > hi {
		sc.add(field, CodeInvalidDuration, "duration %s is outside the allowed range %s to %s", v, lo, hi)
	}
}

func checkName(sc scope, field, name string) bool {
	if !nameRe.MatchString(name) {
		sc.add(field, CodeInvalidName, "name %q is not valid", name)
		return false
	}
	return true
}

func checkCredential(sc scope, field, name string, creds map[string]bool) {
	if !credentialRe.MatchString(name) {
		sc.add(field, CodeInvalidName, "credential name %q is not valid", name)
		return
	}
	if !creds[name] {
		sc.add(field, CodeUnknownCredential, "credential %q is not known to the vault", name)
	}
}

// checkPath enforces the path rule.
func checkPath(sc scope, field, p string) {
	switch {
	case strings.ContainsAny(p, "\x00\n\r"):
		sc.add(field, CodeInvalidPath, "path contains a control character")
	case !filepath.IsAbs(p):
		sc.add(field, CodeRelativePath, "path %q is relative", p)
	default:
		for _, el := range strings.Split(p, "/") {
			if el == ".." {
				sc.add(field, CodeInvalidPath, "path %q contains a \"..\" element", p)
				return
			}
		}
	}
}

func checkHTTPURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "must be an http or https URL with a host", false
	}
	if u.User != nil {
		return "must not contain user information", true
	}
	return "", false
}

func validateSettings(sc scope, s Settings, creds map[string]bool) {
	checkSchema(sc, s.Schema)
	checkRange(sc, "poll_interval", s.PollInterval, time.Minute, 30*24*time.Hour)
	if at := s.Vault.AdminTimeout; at != nil && *at != 0 {
		if v := time.Duration(*at); v < time.Second || v > 24*time.Hour {
			sc.add("vault.admin_timeout", CodeInvalidDuration, "duration %s is outside the allowed range %s to %s, or 0 for a passphrase on every change", v, time.Second, 24*time.Hour)
		}
	}
	if s.Webhook.Listen != "" {
		host, port, err := net.SplitHostPort(s.Webhook.Listen)
		n, perr := strconv.Atoi(port)
		if err != nil || host == "" || perr != nil || n < 1 || n > 65535 {
			sc.add("webhook.listen", CodeInvalidValue, "listen address %q must be host:port with an explicit host and a port from 1 to 65535", s.Webhook.Listen)
		}
	}
	names := map[string]bool{}
	for i, ch := range s.Channels {
		f := fmt.Sprintf("channels[%d]", i)
		if checkName(sc, f+".name", ch.Name) && names[ch.Name] {
			sc.add(f+".name", CodeDuplicateName, "channel name %q is used more than once", ch.Name)
		}
		names[ch.Name] = true
		validateChannel(sc, f, ch, creds)
	}
}

// channelFields lists the fields each channel type accepts besides name and type.
var channelFields = map[string][]string{
	ChannelWebhook:  {"url", "credential"},
	ChannelSMTP:     {"host", "port", "from", "to", "security", "credential"},
	ChannelTelegram: {"chat_id", "credential"},
	ChannelSlack:    {"credential"},
}

func validateChannel(sc scope, f string, ch Channel, creds map[string]bool) {
	allowed, ok := channelFields[ch.Type]
	if !ok {
		sc.add(f+".type", CodeInvalidChannel, "channel type %q is unknown (webhook, smtp, telegram, slack)", ch.Type)
		return
	}
	set := []struct {
		name  string
		isSet bool
	}{
		{"url", ch.URL != ""}, {"host", ch.Host != ""}, {"port", ch.Port != 0}, {"from", ch.From != ""},
		{"to", len(ch.To) > 0}, {"security", ch.Security != ""}, {"chat_id", ch.ChatID != ""}, {"credential", ch.Credential != ""},
	}
	for _, e := range set {
		if e.isSet && !contains(allowed, e.name) {
			sc.add(f+"."+e.name, CodeFieldNotAllowed, "field %q does not apply to a %s channel", e.name, ch.Type)
		}
	}
	need := func(name string, present bool) {
		if !present {
			sc.add(f+"."+name, CodeMissingField, "a %s channel needs %q", ch.Type, name)
		}
	}
	if ch.Credential != "" {
		checkCredential(sc, f+".credential", ch.Credential, creds)
	}
	switch ch.Type {
	case ChannelWebhook:
		need("url", ch.URL != "")
		if ch.URL != "" {
			if msg, secret := checkHTTPURL(ch.URL); msg != "" {
				code := CodeInvalidChannel
				if secret {
					code = CodeSecretInURL
				}
				sc.add(f+".url", code, "url %s", msg)
			}
		}
	case ChannelSMTP:
		need("host", ch.Host != "")
		need("from", ch.From != "")
		need("to", len(ch.To) > 0)
		if ch.Port < 0 || ch.Port > 65535 {
			sc.add(f+".port", CodeInvalidChannel, "port %d is out of range", ch.Port)
		}
		if ch.Security != "" && ch.Security != SecurityStartTLS && ch.Security != SecurityTLS {
			sc.add(f+".security", CodeInvalidChannel, "security %q must be starttls or tls", ch.Security)
		}
		if ch.From != "" {
			if _, err := mail.ParseAddress(ch.From); err != nil {
				sc.add(f+".from", CodeInvalidChannel, "from %q is not an email address", ch.From)
			}
		}
		for j, to := range ch.To {
			if _, err := mail.ParseAddress(to); err != nil {
				sc.add(fmt.Sprintf("%s.to[%d]", f, j), CodeInvalidChannel, "%q is not an email address", to)
			}
		}
	case ChannelTelegram:
		need("chat_id", ch.ChatID != "")
		need("credential", ch.Credential != "")
	case ChannelSlack:
		need("credential", ch.Credential != "")
	}
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func validateTemplate(sc scope, t Template) {
	checkSchema(sc, t.Schema)
	checkName(sc, "name", t.Name)
	if len(t.Steps) == 0 {
		sc.add("steps", CodeStepsMissing, "template has no steps")
	}
	for i, s := range t.Steps {
		validateStep(sc, s, fmt.Sprintf("steps[%d]", i), true)
	}
}

// validateStep checks one step without knowing the project. allowBare permits a step without parameters
// of its own (backup and check steps that use the project's configuration).
func validateStep(sc scope, s Step, f string, allowBare bool) {
	if s.Type == "" || !s.Type.Known() {
		sc.add(f+".type", CodeUnknownStepType, "step type %q is not a building block", s.Type)
		return
	}
	allowed := stepFields[s.Type]
	for _, name := range s.setFields() {
		if !contains(allowed, name) {
			sc.add(f+"."+name, CodeFieldNotAllowed, "field %q does not apply to %s", name, s.Type)
		}
	}
	if s.Timeout != 0 && (time.Duration(s.Timeout) < minStepTimeout || time.Duration(s.Timeout) > maxStepTimeout) {
		sc.add(f+".timeout", CodeInvalidDuration, "timeout %s is outside the allowed range %s to %s", time.Duration(s.Timeout), minStepTimeout, maxStepTimeout)
	}
	checkTexts(sc, s, f)

	need := func(name string, present bool) {
		if !present {
			sc.add(f+"."+name, CodeMissingField, "%s needs %q", s.Type, name)
		}
	}
	bare := s.bare()
	switch s.Type {
	case StepSetEnv:
		need("assign", len(s.Assign) > 0)
		for j, a := range s.Assign {
			if i := strings.IndexByte(a, '='); i <= 0 {
				sc.add(fmt.Sprintf("%s.assign[%d]", f, j), CodeInvalidValue, "assignment %q must have the form KEY=value", a)
			}
		}
	case StepGitCheckout:
		need("tag", s.Tag != "")
	case StepBackup:
		switch {
		case s.Command != "" && s.Volume != "":
			sc.add(f, CodeInvalidValue, "backup uses either command or volume, not both")
		case bare && !allowBare:
			sc.add(f, CodeMissingField, "backup needs command or volume")
		case !bare && s.Command == "" && s.Volume == "":
			sc.add(f, CodeMissingField, "backup needs command or volume to go with its other fields")
		}
		if s.Keep < 0 {
			sc.add(f+".keep", CodeInvalidValue, "keep must not be negative")
		}
	case StepCheckContainer:
		if bare && !allowBare {
			sc.add(f, CodeMissingField, "check-container needs services or containers")
		}
	case StepCheckHTTP:
		if bare && !allowBare {
			sc.add(f, CodeMissingField, "check-http needs url")
		} else if !bare {
			need("url", s.URL != "")
		}
		if s.Status != 0 && (s.Status < 100 || s.Status > 599) {
			sc.add(f+".status", CodeInvalidValue, "status %d is not an HTTP status code", s.Status)
		}
	case StepCommand:
		need("run", s.Run != "")
	}
}

// checkTexts verifies the placeholder syntax of every text field.
func checkTexts(sc scope, s Step, f string) {
	check := func(field, text string) {
		if _, err := parseText(text); err != nil {
			sc.add(field, CodeInvalidPlaceholder, "%v", err)
		}
	}
	for j, v := range s.Assign {
		check(fmt.Sprintf("%s.assign[%d]", f, j), v)
	}
	for j, v := range s.Services {
		check(fmt.Sprintf("%s.services[%d]", f, j), v)
	}
	for j, v := range s.Containers {
		check(fmt.Sprintf("%s.containers[%d]", f, j), v)
	}
	for _, e := range []struct {
		name, text string
		shell      bool
	}{{"tag", s.Tag, false}, {"command", s.Command, true}, {"volume", s.Volume, false},
		{"restore_command", s.RestoreCommand, true}, {"url", s.URL, false}, {"contains", s.Contains, false}, {"run", s.Run, true}} {
		check(f+"."+e.name, e.text)
		if e.shell {
			if name, bad := quotedPlaceholder(e.text); bad {
				sc.add(f+"."+e.name, CodeQuotedPlaceholder, "%s", quotedMessage(name))
			}
		}
	}
}

func validateProject(sc scope, m *Model, p Project, creds map[string]bool) {
	checkSchema(sc, p.Schema)
	checkName(sc, "name", p.Name)

	if p.Dir == "" {
		sc.add("dir", CodeMissingField, "project needs a directory")
	} else {
		checkPath(sc, "dir", p.Dir)
	}
	if p.EnvFile != "" {
		checkPath(sc, "env_file", p.EnvFile)
	}
	files := map[string]bool{}
	for i, c := range p.ComposeFiles {
		f := fmt.Sprintf("compose_files[%d]", i)
		checkPath(sc, f, c)
		if files[c] {
			sc.add(f, CodeDuplicateName, "compose file %q is listed more than once", c)
		}
		files[c] = true
	}
	if p.ComposeProject != "" && !composeNameRe.MatchString(p.ComposeProject) {
		sc.add("compose_project", CodeInvalidName, "compose project name %q is not valid", p.ComposeProject)
	}
	checkRange(sc, "poll_interval", p.PollInterval, time.Minute, 30*24*time.Hour)

	keys := make([]string, 0, len(p.Values))
	for k := range p.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch {
		case !placeholderName.MatchString(k):
			sc.add("values."+k, CodeInvalidName, "value name %q is not a valid placeholder name", k)
		case isBuiltin(k):
			sc.add("values."+k, CodeReservedValueName, "value name %q is a built-in placeholder", k)
		}
	}

	// Steps.
	haveSteps := false
	if p.Template != "" {
		if t, ok := m.Template(p.Template); ok {
			haveSteps = len(t.Steps) > 0
		} else {
			sc.add("template", CodeUnknownTemplate, "template %q does not exist", p.Template)
		}
	}
	if len(p.Steps) > 0 {
		haveSteps = true
	} else if p.Template == "" {
		sc.add("steps", CodeStepsMissing, "project has neither a template nor its own steps")
	}
	validateProjectSteps(sc, p)

	validateSource(sc, p, creds)

	if n := p.Notify; n != nil {
		if len(n.Channels) == 0 {
			sc.add("notify.channels", CodeMissingField, "notify needs at least one channel")
		}
		if len(n.Events) == 0 {
			sc.add("notify.events", CodeMissingField, "notify needs at least one event")
		}
		known := map[string]bool{}
		for _, ch := range m.Settings.Channels {
			known[ch.Name] = true
		}
		for i, c := range n.Channels {
			if !known[c] {
				sc.add(fmt.Sprintf("notify.channels[%d]", i), CodeUnknownChannel, "channel %q is not defined in %s", c, SettingsFile)
			}
		}
		for i, e := range n.Events {
			if !contains(events, e) {
				sc.add(fmt.Sprintf("notify.events[%d]", i), CodeInvalidValue, "event %q is unknown (%s)", e, strings.Join(events, ", "))
			}
		}
	}
	if w := p.Webhook; w != nil {
		if !contains([]string{WebhookGitHub, WebhookGitLab, WebhookGitea, WebhookHMAC}, w.Kind) {
			sc.add("webhook.kind", CodeInvalidWebhook, "webhook kind %q is unknown", w.Kind)
		}
		if w.Secret == "" {
			sc.add("webhook.secret", CodeMissingField, "webhook needs the name of its secret credential")
		} else {
			checkCredential(sc, "webhook.secret", w.Secret, creds)
		}
	}

	// A trial run with sample versions finds placeholders and values that cannot be resolved.
	if haveSteps && (len(p.Images) > 0) != (p.Git != nil) {
		var tmp []Problem
		tsc := scope{out: &tmp, file: sc.file, project: sc.project}
		_, _ = resolveProject(tsc, m, p, sampleTargets(p))
		for _, pr := range tmp {
			if strings.HasPrefix(pr.Field, "template.") && (pr.Code == CodeInvalidPlaceholder || pr.Code == CodeUnknownStepType || pr.Code == CodeQuotedPlaceholder) {
				continue // reported with the template
			}
			*sc.out = append(*sc.out, pr)
		}
	}
}

// validateProjectSteps checks the steps the project itself carries.
func validateProjectSteps(sc scope, p Project) {
	for i, s := range p.Steps {
		validateStep(sc, s, fmt.Sprintf("steps[%d]", i), true)
	}
	if b := p.Backup; b != nil {
		if b.Type != "" && b.Type != StepBackup {
			sc.add("backup.type", CodeInvalidValue, "the project backup is a backup step, not %q", b.Type)
		}
		spec := *b
		spec.Type = StepBackup
		validateStep(sc, spec, "backup", false)
	}
	for i, c := range p.Checks {
		f := fmt.Sprintf("checks[%d]", i)
		if c.Type != StepCheckContainer && c.Type != StepCheckHTTP {
			sc.add(f+".type", CodeInvalidValue, "a check is check-container or check-http, not %q", c.Type)
			continue
		}
		validateStep(sc, c, f, false)
	}
}

func validateSource(sc scope, p Project, creds map[string]bool) {
	switch {
	case len(p.Images) > 0 && p.Git != nil:
		sc.add("git", CodeSourceConflict, "project has images and a git source")
	case len(p.Images) == 0 && p.Git == nil:
		sc.add("images", CodeSourceMissing, "project has neither images nor a git source")
	}
	names := map[string]bool{}
	envs := map[string]bool{}
	hostCred := map[string]string{}
	for i, im := range p.Images {
		f := fmt.Sprintf("images[%d]", i)
		if checkName(sc, f+".name", im.Name) && names[im.Name] {
			sc.add(f+".name", CodeDuplicateName, "image name %q is used more than once", im.Name)
		}
		names[im.Name] = true
		switch {
		case im.Ref == "":
			sc.add(f+".ref", CodeMissingField, "image needs a registry reference")
		case !imageRefRe.MatchString(im.Ref) || strings.Contains(im.Ref, "@") || strings.Contains(im.Ref[strings.LastIndexByte(im.Ref, '/')+1:], ":"):
			sc.add(f+".ref", CodeInvalidValue, "reference %q must be a repository such as registry.example.com/team/app without tag or digest", im.Ref)
		}
		if im.EnvVar != "" {
			if !envNameRe.MatchString(im.EnvVar) {
				sc.add(f+".env_var", CodeInvalidValue, "%q is not an environment variable name", im.EnvVar)
			}
			if envs[im.EnvVar] {
				sc.add(f+".env_var", CodeDuplicateEnvVar, "environment variable %q is used by more than one image", im.EnvVar)
			}
			envs[im.EnvVar] = true
		}
		for j, s := range im.Services {
			if !serviceRe.MatchString(s) {
				sc.add(fmt.Sprintf("%s.services[%d]", f, j), CodeInvalidValue, "%q is not a service name", s)
			}
		}
		if im.Credential != "" {
			checkCredential(sc, f+".credential", im.Credential, creds)
			// One docker login per registry host applies to a run, so images cannot differ in it.
			if repo, err := registry.ParseRepo(im.Ref); err == nil {
				if prev, ok := hostCred[repo.Host]; ok && prev != im.Credential {
					sc.add(f+".credential", CodeRegistryCredential, "image uses credential %q on %s, another image of the project uses %q", im.Credential, repo.Host, prev)
				} else if !ok {
					hostCred[repo.Host] = im.Credential
				}
			}
		}
		validateRule(sc, f+".version_rule", im.VersionRule)
		if c := im.VersionRule.ChangelogCredential; c != "" {
			checkCredential(sc, f+".version_rule.changelog_credential", c, creds)
		}
	}
	if g := p.Git; g != nil {
		validateGitURL(sc, g.URL)
		if g.Credential != "" {
			checkCredential(sc, "git.credential", g.Credential, creds)
		}
		validateRule(sc, "git.version_rule", g.VersionRule)
		if c := g.VersionRule.ChangelogCredential; c != "" {
			checkCredential(sc, "git.version_rule.changelog_credential", c, creds)
		}
	}
}

func validateGitURL(sc scope, raw string) {
	switch {
	case raw == "":
		sc.add("git.url", CodeMissingField, "git source needs a url")
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		switch {
		case err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "ssh"):
			sc.add("git.url", CodeInvalidValue, "url must use https:// or ssh:// with a host, or have the form user@host:path")
		case u.Scheme == "https" && u.User != nil, u.Scheme == "ssh" && u.User != nil && func() bool { _, has := u.User.Password(); return has }():
			sc.add("git.url", CodeSecretInURL, "url must not contain a password or token")
		}
	case !scpGitRe.MatchString(raw):
		sc.add("git.url", CodeInvalidValue, "url must use https:// or ssh:// with a host, or have the form user@host:path")
	}
}

func validateRule(sc scope, f string, r VersionRule) {
	enum := func(field, v string, allowed ...string) {
		if v != "" && !contains(allowed, v) {
			sc.add(f+"."+field, CodeInvalidVersionRule, "%s %q must be one of %s", field, v, strings.Join(allowed, ", "))
		}
	}
	enum("mode", r.Mode, ModeAuto, ModeManual)
	enum("max_update", r.MaxUpdate, LevelPatch, LevelMinor, LevelMajor)
	enum("major_steps", r.MajorSteps, MajorDirect, MajorStep)
	if r.TagFilter != "" {
		if _, err := regexp.Compile(r.TagFilter); err != nil {
			sc.add(f+".tag_filter", CodeInvalidTagFilter, "tag filter does not compile: %v", err)
		}
	}
	if r.ChangelogRepo != "" && !changelogRe.MatchString(r.ChangelogRepo) {
		sc.add(f+".changelog_repo", CodeInvalidVersionRule, "changelog_repo %q must have the form owner/repo", r.ChangelogRepo)
	}
}
