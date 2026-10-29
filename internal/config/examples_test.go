package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var exampleCredentials = []string{"registry-example", "git-example", "smtp-example", "slack-example", "webhook-shop-example"}

// golden compares got with testdata/<name>.golden, or rewrites the file with -update.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the golden file; run with -update to review.\n--- got ---\n%s", name, got)
	}
}

// render prints a plan in a stable text form that shows every resolved parameter.
func render(p Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "project %s\n", p.Project)
	for i, s := range p.Steps {
		fmt.Fprintf(&b, "%d. %s timeout=%s free=%v\n", i+1, s.Type, s.Timeout, s.IsFree())
		line := func(k string, v any) { fmt.Fprintf(&b, "   %s: %v\n", k, v) }
		line("dir", s.Dir)
		if s.EnvFile != "" {
			line("env_file", s.EnvFile)
		}
		if len(s.ComposeFiles) > 0 {
			line("compose_files", fmt.Sprintf("%q", s.ComposeFiles))
		}
		if s.ComposeProject != "" {
			line("compose_project", s.ComposeProject)
		}
		switch s.Type {
		case StepSetEnv:
			for _, a := range s.Assignments {
				line("assign", a.Key+"="+a.Value)
			}
		case StepComposePull, StepComposeUp:
			line("services", fmt.Sprintf("%q", s.Services))
		case StepGitCheckout:
			line("tag", s.Tag)
		case StepBackup:
			line("backup", fmt.Sprintf("command=%q volume=%q keep=%d restore=%q", s.Backup.Command, s.Backup.Volume, s.Backup.Keep, s.Backup.RestoreCommand))
		case StepCheckContainer:
			line("services", fmt.Sprintf("%q", s.Services))
			line("containers", fmt.Sprintf("%q", s.Containers))
		case StepCheckHTTP:
			line("url", s.URL)
			line("status", s.Status)
			line("contains", fmt.Sprintf("%q", s.Contains))
		case StepCommand:
			line("shell", s.Shell)
		}
	}
	return b.String()
}

func TestExamples(t *testing.T) {
	runs := []struct {
		dir, project string
		targets      []Target
	}{
		{"image-project", "notes-app", []Target{{Name: "app", Version: "2.4.1", PreviousVersion: "2.4.0"}}},
		{"stack-override", "shop-stack", []Target{
			{Name: "app", Version: "5.1.0", PreviousVersion: "5.0.2"},
			{Name: "db", Version: "16.4", PreviousVersion: "16.3"},
		}},
		{"stack-override-db-only", "shop-stack", []Target{{Name: "db", Version: "16.4", PreviousVersion: "16.3"}}},
		{"website-git", "example-site", []Target{{Version: "v1.3.0", PreviousVersion: "v1.2.0"}}},
	}
	for _, r := range runs {
		t.Run(r.dir, func(t *testing.T) {
			dir := strings.TrimSuffix(r.dir, "-db-only")
			m, err := LoadPath(filepath.Join("..", "..", "examples", dir))
			if err != nil {
				t.Fatal(err)
			}
			if probs := Validate(m, exampleCredentials); len(probs) > 0 {
				t.Fatalf("example is not valid: %v", probs)
			}
			plan, err := m.Resolve(r.project, r.targets)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, "plan-"+r.dir, render(plan))
		})
	}
}

// Every example survives decode, encode and decode again without change.
func TestExamplesRoundTrip(t *testing.T) {
	for _, dir := range []string{"image-project", "stack-override", "website-git"} {
		m, err := LoadPath(filepath.Join("..", "..", "examples", dir))
		if err != nil {
			t.Fatal(err)
		}
		data, err := EncodeSettings(m.Settings)
		if err != nil {
			t.Fatal(err)
		}
		back, err := DecodeSettings(data)
		if err != nil || !reflect.DeepEqual(back, m.Settings) {
			t.Errorf("%s settings: %v", dir, err)
		}
		for _, p := range m.Projects {
			data, err := EncodeProject(p)
			if err != nil {
				t.Fatal(err)
			}
			back, err := DecodeProject(data)
			if err != nil || !reflect.DeepEqual(back, p) {
				t.Errorf("%s/%s: round trip changed the project (err %v)", dir, p.Name, err)
			}
		}
	}
}

func TestEffectiveDefaults(t *testing.T) {
	var s Settings
	if s.EffectivePollInterval() != time.Hour || s.EffectiveAdminTimeout() != 15*time.Minute || s.EffectiveListen() != "127.0.0.1:8787" {
		t.Errorf("defaults: %v %v %v", s.EffectivePollInterval(), s.EffectiveAdminTimeout(), s.EffectiveListen())
	}
}
