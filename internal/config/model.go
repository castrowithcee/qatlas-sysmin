// Package config is the validated in-memory configuration model of sysmin: settings, projects, scenario
// templates and their building blocks. The model holds credential names only, never secret values.
// Files are decoded and encoded one by one as pure functions; Load assembles the directory layout, Validate
// reports every problem it finds, and Resolve turns a project plus target versions into concrete steps.
package config

import (
	"fmt"
	"time"

	"go.yaml.in/yaml/v3"
)

// CurrentSchema is the schema version every file is written with and the newest one this package reads.
const CurrentSchema = 1

// Layout of a configuration directory, relative to its root (normally /etc/sysmin).
const (
	SettingsFile  = "sysmin.yaml"
	ProjectsDir   = "projects"
	TemplatesDir  = "templates"
	fileExtension = ".yaml"
)

// Defaults applied where a field is left out.
const (
	DefaultPollInterval = time.Hour
	DefaultAdminTimeout = 15 * time.Minute
	DefaultListen       = "127.0.0.1:8787"
	DefaultBackupKeep   = 5
	DefaultHTTPStatus   = 200
	DefaultSMTPPort     = 587
)

// Duration is a time.Duration written as text such as "30s" or "5m" in YAML.
type Duration time.Duration

// UnmarshalYAML reads a duration string.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: a duration must be a string such as \"30s\"", node.Line)
	}
	v, err := time.ParseDuration(node.Value)
	if err != nil {
		return fmt.Errorf("line %d: invalid duration %q: use a form such as \"30s\" or \"5m\"", node.Line, node.Value)
	}
	*d = Duration(v)
	return nil
}

// MarshalYAML writes the shortest exact form; zero is written as a bare 0.
func (d Duration) MarshalYAML() (any, error) {
	v := time.Duration(d)
	switch {
	case v == 0:
		return 0, nil
	case v != 0 && v%time.Hour == 0:
		return fmt.Sprintf("%dh", v/time.Hour), nil
	case v != 0 && v%time.Minute == 0:
		return fmt.Sprintf("%dm", v/time.Minute), nil
	case v != 0 && v%time.Second == 0:
		return fmt.Sprintf("%ds", v/time.Second), nil
	}
	return v.String(), nil
}

// Settings is sysmin.yaml.
type Settings struct {
	Schema       int             `yaml:"schema"`
	PollInterval Duration        `yaml:"poll_interval,omitempty"`
	Vault        VaultSettings   `yaml:"vault,omitempty"`
	Webhook      WebhookListener `yaml:"webhook,omitempty"`
	Channels     []Channel       `yaml:"channels,omitempty"`
}

// VaultSettings configures the credential vault.
type VaultSettings struct {
	// AdminTimeout is a pointer so that an explicit 0 (every change asks for the passphrase) differs from
	// a missing field (the default).
	AdminTimeout *Duration `yaml:"admin_timeout,omitempty"`
}

// WebhookListener configures the one listener that receives project webhooks.
type WebhookListener struct {
	Listen string `yaml:"listen,omitempty"`
}

// EffectivePollInterval is the default poll interval with the built-in default applied.
func (s Settings) EffectivePollInterval() time.Duration {
	return orDuration(s.PollInterval, DefaultPollInterval)
}

// EffectiveAdminTimeout is the vault admin timeout with the built-in default applied; an explicit 0 stays 0.
func (s Settings) EffectiveAdminTimeout() time.Duration {
	if s.Vault.AdminTimeout == nil {
		return DefaultAdminTimeout
	}
	return time.Duration(*s.Vault.AdminTimeout)
}

// EffectiveListen is the webhook listen address with the loopback default applied.
func (s Settings) EffectiveListen() string {
	if s.Webhook.Listen == "" {
		return DefaultListen
	}
	return s.Webhook.Listen
}

func orDuration(d Duration, def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return time.Duration(d)
}

// Notification channel types.
const (
	ChannelWebhook  = "webhook"
	ChannelSMTP     = "smtp"
	ChannelTelegram = "telegram"
	ChannelSlack    = "slack"
)

// SMTP connection security.
const (
	SecurityStartTLS = "starttls"
	SecurityTLS      = "tls"
)

// Channel is one notification channel. Which fields apply depends on Type; Credential names the secret
// (login, bot token or hook URL) that lives in the vault.
type Channel struct {
	Name       string   `yaml:"name"`
	Type       string   `yaml:"type"`
	URL        string   `yaml:"url,omitempty"`
	Host       string   `yaml:"host,omitempty"`
	Port       int      `yaml:"port,omitempty"`
	From       string   `yaml:"from,omitempty"`
	To         []string `yaml:"to,omitempty"`
	Security   string   `yaml:"security,omitempty"`
	ChatID     string   `yaml:"chat_id,omitempty"`
	Credential string   `yaml:"credential,omitempty"`
}

// Notification events a project can subscribe to.
const (
	EventUpdateAvailable = "update-available"
	EventUpdateSucceeded = "update-succeeded"
	EventUpdateFailed    = "update-failed"
	EventRestoreSucceed  = "restore-succeeded"
	EventRestoreFailed   = "restore-failed"
)

var events = []string{EventUpdateAvailable, EventUpdateSucceeded, EventUpdateFailed, EventRestoreSucceed, EventRestoreFailed}

// Version rule values.
const (
	ModeAuto    = "auto"
	ModeManual  = "manual"
	LevelPatch  = "patch"
	LevelMinor  = "minor"
	LevelMajor  = "major"
	MajorDirect = "direct"
	MajorStep   = "stepwise"
)

// VersionRule says which new versions are offered and which are applied without a human.
type VersionRule struct {
	// Mode defaults to manual.
	Mode string `yaml:"mode,omitempty"`
	// MaxUpdate caps automatic updates; it defaults to major.
	MaxUpdate string `yaml:"max_update,omitempty"`
	// MajorSteps defaults to direct.
	MajorSteps string `yaml:"major_steps,omitempty"`
	// TagFilter is an RE2 regular expression a tag must match to be considered.
	TagFilter string `yaml:"tag_filter,omitempty"`
	// ChangelogRepo is a GitHub repository, owner/repo.
	ChangelogRepo string `yaml:"changelog_repo,omitempty"`
}

// Image is one container image of a project.
type Image struct {
	Name        string      `yaml:"name"`
	Ref         string      `yaml:"ref"`
	EnvVar      string      `yaml:"env_var,omitempty"`
	Services    []string    `yaml:"services,omitempty"`
	Credential  string      `yaml:"credential,omitempty"`
	VersionRule VersionRule `yaml:"version_rule,omitempty"`
}

// GitSource is a git repository whose tags are the versions.
type GitSource struct {
	URL         string      `yaml:"url"`
	Credential  string      `yaml:"credential,omitempty"`
	VersionRule VersionRule `yaml:"version_rule,omitempty"`
}

// Webhook kinds.
const (
	WebhookGitHub = "github"
	WebhookGitLab = "gitlab"
	WebhookGitea  = "gitea"
	WebhookHMAC   = "hmac"
)

// ProjectWebhook lets a push from outside trigger a check; Secret names the vault credential that signs it.
type ProjectWebhook struct {
	Kind   string `yaml:"kind"`
	Secret string `yaml:"secret"`
}

// Notify routes events of a project to channels by name.
type Notify struct {
	Channels []string `yaml:"channels,omitempty"`
	Events   []string `yaml:"events,omitempty"`
}

// Project is one file under projects/.
type Project struct {
	Schema   int    `yaml:"schema"`
	Name     string `yaml:"name"`
	Dir      string `yaml:"dir"`
	Template string `yaml:"template,omitempty"`
	// Steps replace the steps of the template completely.
	Steps  []Step            `yaml:"steps,omitempty"`
	Values map[string]string `yaml:"values,omitempty"`
	// EnvFile is the file set-env writes to.
	EnvFile string `yaml:"env_file,omitempty"`
	// ComposeFiles are passed in this order; empty means the default search of Docker Compose.
	ComposeFiles   []string `yaml:"compose_files,omitempty"`
	ComposeProject string   `yaml:"compose_project,omitempty"`
	PollInterval   Duration `yaml:"poll_interval,omitempty"`
	// Backup is what a bare backup step uses; its type is backup.
	Backup *Step `yaml:"backup,omitempty"`
	// Checks are what bare check-container and check-http steps expand to.
	Checks  []Step          `yaml:"checks,omitempty"`
	Notify  *Notify         `yaml:"notify,omitempty"`
	Webhook *ProjectWebhook `yaml:"webhook,omitempty"`
	// Source: exactly one of Images and Git.
	Images []Image    `yaml:"images,omitempty"`
	Git    *GitSource `yaml:"git,omitempty"`
}

// Template is a scenario template: a named step sequence with placeholders. Templates are starting points.
type Template struct {
	Schema      int    `yaml:"schema"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Steps       []Step `yaml:"steps"`
}

// Model is the whole configuration read from one directory.
type Model struct {
	Settings  Settings
	Projects  []Project
	Templates []Template // custom templates only
}

// Project returns the project with the given name.
func (m *Model) Project(name string) (*Project, bool) {
	for i := range m.Projects {
		if m.Projects[i].Name == name {
			return &m.Projects[i], true
		}
	}
	return nil, false
}

// Template returns an embedded or custom template by name.
func (m *Model) Template(name string) (Template, bool) {
	for _, t := range BuiltinTemplates() {
		if t.Name == name {
			return t, true
		}
	}
	for _, t := range m.Templates {
		if t.Name == name {
			return t, true
		}
	}
	return Template{}, false
}

// SettingsPath, ProjectPath and TemplatePath are the file locations under the configuration root.
func SettingsPath() string           { return SettingsFile }
func ProjectPath(name string) string { return ProjectsDir + "/" + name + fileExtension }
func TemplatePath(name string) string {
	return TemplatesDir + "/" + name + fileExtension
}
