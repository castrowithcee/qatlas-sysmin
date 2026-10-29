package config

import (
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

var testCredentials = []string{"registry-example", "git-example", "hook-example", "mail-example"}

// baseModel is a valid model: one image project and one git project.
func baseModel() *Model {
	return &Model{
		Settings: Settings{Schema: 1, Channels: []Channel{
			{Name: "mail", Type: ChannelSMTP, Host: "smtp.example.com", From: "sysmin@example.com", To: []string{"ops@example.com"}, Credential: "mail-example"},
		}},
		Projects: []Project{
			{
				Schema: 1, Name: "shop", Dir: "/srv/example/shop", Template: "image-tag", EnvFile: "/srv/example/shop/.env",
				Images: []Image{
					{Name: "app", Ref: "registry.example.com/example/app", EnvVar: "APP_TAG", Services: []string{"app"}, Credential: "registry-example"},
					{Name: "db", Ref: "registry.example.com/example/db", EnvVar: "DB_TAG", Services: []string{"db"}},
				},
				Notify:  &Notify{Channels: []string{"mail"}, Events: []string{EventUpdateFailed}},
				Webhook: &ProjectWebhook{Kind: WebhookGitHub, Secret: testCredentials[2]},
			},
			{
				Schema: 1, Name: "site", Dir: "/srv/example/site", Template: "git-tag",
				Git: &GitSource{URL: "https://git.example.com/example/site.git", Credential: "git-example"},
			},
		},
	}
}

func TestBaseModelIsValid(t *testing.T) {
	if probs := Validate(baseModel(), testCredentials); len(probs) != 0 {
		t.Fatalf("unexpected problems: %v", probs)
	}
}

func TestValidateProblems(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(m *Model)
		code   string
		field  string
	}{
		{"unknown template", func(m *Model) { m.Projects[0].Template = "nope" }, CodeUnknownTemplate, "template"},
		{"no steps", func(m *Model) { m.Projects[0].Template = "" }, CodeStepsMissing, "steps"},
		{"unknown step type", func(m *Model) { m.Projects[0].Steps = []Step{{Type: "reboot"}} }, CodeUnknownStepType, "steps[0].type"},
		{"unknown credential", func(m *Model) { m.Projects[0].Images[0].Credential = "missing" }, CodeUnknownCredential, "images[0].credential"},
		{"unknown git credential", func(m *Model) { m.Projects[1].Git.Credential = "missing" }, CodeUnknownCredential, "git.credential"},
		{"unknown webhook credential", func(m *Model) { m.Projects[0].Webhook.Secret = "missing" }, CodeUnknownCredential, "webhook.secret"},
		{"unknown channel credential", func(m *Model) { m.Settings.Channels[0].Credential = "missing" }, CodeUnknownCredential, "channels[0].credential"},
		{"unknown placeholder", func(m *Model) {
			m.Projects[0].Steps = []Step{{Type: StepCommand, Run: "echo {nothing}"}}
		}, CodeUnknownPlaceholder, "steps[0].run"},
		{"var in git project", func(m *Model) {
			m.Projects[1].Steps = []Step{{Type: StepCommand, Run: "echo {var}"}}
		}, CodeUnknownPlaceholder, "steps[0].run"},
		{"broken placeholder", func(m *Model) {
			m.Projects[0].Steps = []Step{{Type: StepCommand, Run: "echo ${HOME}"}}
		}, CodeInvalidPlaceholder, "steps[0].run"},
		{"relative dir", func(m *Model) { m.Projects[0].Dir = "srv/shop" }, CodeRelativePath, "dir"},
		{"relative env file", func(m *Model) { m.Projects[0].EnvFile = ".env" }, CodeRelativePath, "env_file"},
		{"relative compose file", func(m *Model) { m.Projects[0].ComposeFiles = []string{"compose.yaml"} }, CodeRelativePath, "compose_files[0]"},
		{"dotdot dir", func(m *Model) { m.Projects[0].Dir = "/srv/../etc" }, CodeInvalidPath, "dir"},
		{"bad mode", func(m *Model) { m.Projects[0].Images[0].VersionRule.Mode = "sometimes" }, CodeInvalidVersionRule, "images[0].version_rule.mode"},
		{"bad max update", func(m *Model) { m.Projects[0].Images[0].VersionRule.MaxUpdate = "huge" }, CodeInvalidVersionRule, "images[0].version_rule.max_update"},
		{"bad major steps", func(m *Model) { m.Projects[0].Images[0].VersionRule.MajorSteps = "jump" }, CodeInvalidVersionRule, "images[0].version_rule.major_steps"},
		{"bad changelog repo", func(m *Model) { m.Projects[0].Images[0].VersionRule.ChangelogRepo = "just-a-name" }, CodeInvalidVersionRule, "images[0].version_rule.changelog_repo"},
		{"bad tag filter", func(m *Model) { m.Projects[1].Git.VersionRule.TagFilter = "([" }, CodeInvalidTagFilter, "git.version_rule.tag_filter"},
		{"no source", func(m *Model) { m.Projects[0].Images = nil }, CodeSourceMissing, "images"},
		{"two sources", func(m *Model) { m.Projects[0].Git = &GitSource{URL: "https://git.example.com/a.git"} }, CodeSourceConflict, "git"},
		{"duplicate env var", func(m *Model) { m.Projects[0].Images[1].EnvVar = "APP_TAG" }, CodeDuplicateEnvVar, "images[1].env_var"},
		{"duplicate project", func(m *Model) { m.Projects = append(m.Projects, m.Projects[0]) }, CodeDuplicateName, "name"},
		{"duplicate image", func(m *Model) { m.Projects[0].Images[1].Name = "app" }, CodeDuplicateName, "images[1].name"},
		{"duplicate channel", func(m *Model) { m.Settings.Channels = append(m.Settings.Channels, m.Settings.Channels[0]) }, CodeDuplicateName, "channels[1].name"},
		{"duplicate template", func(m *Model) {
			m.Templates = []Template{{Schema: 1, Name: "image-tag", Steps: []Step{{Type: StepComposeUp}}}}
		}, CodeDuplicateName, "name"},
		{"duplicate custom template", func(m *Model) {
			tpl := Template{Schema: 1, Name: "mine", Steps: []Step{{Type: StepComposeUp}}}
			m.Templates = []Template{tpl, tpl}
		}, CodeDuplicateName, "name"},
		{"unknown channel", func(m *Model) { m.Projects[0].Notify.Channels = []string{"pager"} }, CodeUnknownChannel, "notify.channels[0]"},
		{"unknown event", func(m *Model) { m.Projects[0].Notify.Events = []string{"boom"} }, CodeInvalidValue, "notify.events[0]"},
		{"bad webhook kind", func(m *Model) { m.Projects[0].Webhook.Kind = "svn" }, CodeInvalidWebhook, "webhook.kind"},
		{"reserved value name", func(m *Model) { m.Projects[0].Values = map[string]string{"version": "x"} }, CodeReservedValueName, "values.version"},
		{"token in git url", func(m *Model) {
			m.Projects[1].Git.URL = (&url.URL{Scheme: "https", User: url.User("token"), Host: "git.example.com", Path: "/a.git"}).String()
		}, CodeSecretInURL, "git.url"},
		{"image ref with tag", func(m *Model) { m.Projects[0].Images[0].Ref = "registry.example.com/app:1.0" }, CodeInvalidValue, "images[0].ref"},
		{"bad listen", func(m *Model) { m.Settings.Webhook.Listen = ":8787" }, CodeInvalidValue, "webhook.listen"},
		{"poll too small", func(m *Model) { m.Settings.PollInterval = Duration(1) }, CodeInvalidDuration, "poll_interval"},
		{"set-env without env file", func(m *Model) { m.Projects[0].EnvFile = "" }, CodeMissingEnvFile, "template.steps[1]"},
		{"var without env_var", func(m *Model) { m.Projects[0].Images[1].EnvVar = "" }, CodeMissingEnvVar, "template.steps[1].assign[0]"},
		{"field not allowed", func(m *Model) {
			m.Projects[0].Steps = []Step{{Type: StepComposeUp, Tag: "x"}}
		}, CodeFieldNotAllowed, "steps[0].tag"},
		{"backup both modes", func(m *Model) {
			m.Projects[0].Backup = &Step{Command: "true", Volume: "data"}
		}, CodeInvalidValue, "backup"},
		{"bare project check", func(m *Model) {
			m.Projects[0].Checks = []Step{{Type: StepCheckHTTP}}
		}, CodeMissingField, "checks[0]"},
		{"channel type", func(m *Model) { m.Settings.Channels[0].Type = "fax" }, CodeInvalidChannel, "channels[0].type"},
		{"channel field", func(m *Model) { m.Settings.Channels[0].ChatID = "1" }, CodeFieldNotAllowed, "channels[0].chat_id"},
		{"slack without credential", func(m *Model) {
			m.Settings.Channels = append(m.Settings.Channels, Channel{Name: "chat", Type: ChannelSlack})
		}, CodeMissingField, "channels[1].credential"},
		{"secret in webhook url", func(m *Model) {
			m.Settings.Channels = append(m.Settings.Channels, Channel{Name: "hook", Type: ChannelWebhook, URL: "https://user:pw@example.com/x"})
		}, CodeSecretInURL, "channels[1].url"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := baseModel()
			tc.mutate(m)
			probs := Validate(m, testCredentials)
			for _, p := range probs {
				if p.Code == tc.code && p.Field == tc.field {
					if p.Hint == "" || p.Message == "" || p.File == "" {
						t.Errorf("problem lacks location, message or hint: %+v", p)
					}
					return
				}
			}
			t.Errorf("want %s at %s, got %v", tc.code, tc.field, probs)
		})
	}
}

func TestProblemLocation(t *testing.T) {
	m := baseModel()
	m.Projects[0].Dir = "relative"
	probs := Validate(m, testCredentials)
	if len(probs) != 1 || probs[0].File != "projects/shop.yaml" || probs[0].Project != "shop" || !strings.Contains(probs[0].Error(), "dir") {
		t.Errorf("got %+v", probs)
	}
}

func TestBuiltinTemplatesAreValid(t *testing.T) {
	var probs []Problem
	for _, tpl := range BuiltinTemplates() {
		validateTemplate(scope{out: &probs, file: TemplatePath(tpl.Name), template: tpl.Name}, tpl)
	}
	if len(probs) != 0 {
		t.Errorf("%v", probs)
	}
	names := ""
	for _, tpl := range BuiltinTemplates() {
		names += tpl.Name + " "
	}
	if names != "git-tag image-tag " {
		t.Errorf("embedded templates: %s", names)
	}
}

func TestCustomTemplateProblems(t *testing.T) {
	m := baseModel()
	m.Templates = []Template{{Schema: 1, Name: "Bad Name", Steps: []Step{{Type: StepGitCheckout}, {Type: StepSetEnv, Assign: []string{"nokey"}}}}}
	var codes []string
	for _, p := range Validate(m, testCredentials) {
		codes = append(codes, p.Code+"@"+p.Field)
	}
	got := strings.Join(codes, " ")
	for _, want := range []string{CodeInvalidName + "@name", CodeMissingField + "@steps[0].tag", CodeInvalidValue + "@steps[1].assign[0]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

// Path rule: dir, env_file and compose files must be absolute without "..".
func TestPathRule(t *testing.T) {
	for p, ok := range map[string]bool{"/srv/example": true, "/srv/example/": true, "/": true, "srv": false, "./srv": false, "": false, "/a/../b": false, "/a\nb": false} {
		var probs []Problem
		checkPath(scope{out: &probs}, "x", p)
		if (len(probs) == 0) != ok {
			t.Errorf("%q: ok=%v problems=%v", p, ok, probs)
		}
	}
}

// Several problems at once come out in the same order on every run.
func TestValidateOrderIsStable(t *testing.T) {
	m := baseModel()
	m.Settings.Channels[0].ChatID = "1"
	m.Settings.Channels[0].URL = "https://example.com"
	m.Settings.Channels[0].Port = 1
	m.Projects[0].Values = map[string]string{"version": "x", "dir": "y", "Bad": "z", "var": "w", "services": "q"}
	m.Projects[0].Steps = []Step{{Type: StepGitCheckout, Tag: "{a", Volume: "{b", URL: "{c", Contains: "{d", Command: "{e", Run: "{f", RestoreCommand: "{g"}}
	first := Validate(m, testCredentials)
	if len(first) < 10 {
		t.Fatalf("expected many problems, got %d", len(first))
	}
	for i := 0; i < 50; i++ {
		if got := Validate(m, testCredentials); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs:\n%v\n%v", i, got, first)
		}
	}
}

func TestQuotedPlaceholders(t *testing.T) {
	cases := map[string]bool{
		`cp '{dir}' /tmp`:     true,
		`cp "{dir}" /tmp`:     true,
		`echo "a $x {dir}"`:   true,
		`echo \{dir}`:         true,
		`echo 'a b' {dir}`:    false,
		`echo "a \" b" {dir}`: false,
		`echo 'it\' {dir}`:    false,
		`echo {dir} 'a b'`:    false,
		`echo {dir}`:          false,
	}
	for run, bad := range cases {
		m := baseModel()
		m.Projects[0].Steps = []Step{{Type: StepCommand, Run: run}}
		found := false
		for _, p := range Validate(m, testCredentials) {
			if p.Code == CodeQuotedPlaceholder && p.Field == "steps[0].run" {
				found = true
				if p.Hint != "Placeholders are quoted automatically; remove the quotes around them." {
					t.Errorf("hint: %q", p.Hint)
				}
			}
		}
		if found != bad {
			t.Errorf("%q: reported=%v want %v", run, found, bad)
		}
		_, err := m.Resolve("shop", []Target{{Name: "app", Version: "1", PreviousVersion: "0"}})
		if (err != nil) != bad {
			t.Errorf("%q: Resolve error=%v want failure=%v", run, err, bad)
		}
	}
	m := baseModel()
	m.Projects[0].Backup = &Step{Command: "tar '{dir}'", RestoreCommand: `tar "{dir}"`}
	n := 0
	for _, p := range Validate(m, testCredentials) {
		if p.Code == CodeQuotedPlaceholder {
			n++
		}
	}
	if n != 2 {
		t.Errorf("backup quoted problems: %d", n)
	}
}

func TestAdminTimeoutValidation(t *testing.T) {
	for _, c := range []struct {
		d  time.Duration
		ok bool
	}{{0, true}, {time.Second, true}, {24 * time.Hour, true}, {500 * time.Millisecond, false}, {25 * time.Hour, false}} {
		m := baseModel()
		d := Duration(c.d)
		m.Settings.Vault.AdminTimeout = &d
		var found bool
		for _, p := range Validate(m, testCredentials) {
			if p.Code == CodeInvalidDuration && p.Field == "vault.admin_timeout" {
				found = true
			}
		}
		if found == c.ok {
			t.Errorf("%v: reported = %v, want %v", c.d, found, !c.ok)
		}
	}
}
