package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mustResolve(t *testing.T, m *Model, project string, targets []Target) Plan {
	t.Helper()
	plan, err := m.Resolve(project, targets)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestResolveMultipleImagesGolden(t *testing.T) {
	m := baseModel()
	m.Projects[0].ComposeFiles = []string{"/srv/example/shop/compose.yaml", "/srv/example/shop/compose.override.yaml"}
	m.Projects[0].Backup = &Step{Volume: "shop-data", Keep: 2}
	m.Projects[0].Checks = []Step{{Type: StepCheckHTTP, URL: "https://shop.example.com/health", Contains: "ok"}}
	t.Run("both", func(t *testing.T) {
		golden(t, "resolve-image-tag-both", render(mustResolve(t, m, "shop", []Target{
			{Name: "db", Version: "16.4", PreviousVersion: "16.3"}, // input order does not matter
			{Name: "app", Version: "2.0.0", PreviousVersion: "1.9.0"},
		})))
	})
	t.Run("one", func(t *testing.T) {
		golden(t, "resolve-image-tag-one", render(mustResolve(t, m, "shop", []Target{{Name: "db", Version: "16.4", PreviousVersion: "16.3"}})))
	})
	t.Run("per-image custom step", func(t *testing.T) {
		m := baseModel()
		m.Projects[0].Steps = []Step{
			{Type: StepCommand, Run: "echo {var} {previous_version} to {version} in {dir}; svc={services}"},
			{Type: StepSetEnv, Assign: []string{"FIXED=1", "{var}={version}"}},
			{Type: StepComposeUp, Services: []string{"{services}", "extra"}},
		}
		golden(t, "resolve-per-image", render(mustResolve(t, m, "shop", []Target{
			{Name: "app", Version: "2.0.0", PreviousVersion: "1.9.0"},
			{Name: "db", Version: "16.4", PreviousVersion: "16.3"},
		})))
	})
}

func TestResolveGitTag(t *testing.T) {
	m := baseModel()
	plan := mustResolve(t, m, "site", []Target{{Version: "v1.2.3", PreviousVersion: "v1.2.2"}})
	golden(t, "resolve-git-tag", render(plan))
	if len(plan.Steps) != 3 || plan.Steps[0].Tag != "v1.2.3" {
		t.Errorf("steps: %+v", plan.Steps)
	}
}

func TestResolveOwnStepsReplaceTemplate(t *testing.T) {
	m := baseModel()
	m.Projects[0].Steps = []Step{{Type: StepComposeUp}}
	plan := mustResolve(t, m, "shop", []Target{{Name: "app", Version: "1", PreviousVersion: "0"}})
	if len(plan.Steps) != 1 || plan.Steps[0].Type != StepComposeUp {
		t.Errorf("%+v", plan.Steps)
	}
}

func TestResolveRejectsBadInput(t *testing.T) {
	good := Target{Name: "app", Version: "2.0.0", PreviousVersion: "1.9.0"}
	cases := map[string][]Target{
		"no targets":        nil,
		"unknown image":     {{Name: "ghost", Version: "1", PreviousVersion: "0"}},
		"duplicate image":   {good, good},
		"flag-like version": {{Name: "app", Version: "-rf", PreviousVersion: "1.9.0"}},
		"space in version":  {{Name: "app", Version: "1 2", PreviousVersion: "1.9.0"}},
		"missing previous":  {{Name: "app", Version: "2.0.0"}},
	}
	for name, ts := range cases {
		if _, err := baseModel().Resolve("shop", ts); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := baseModel().Resolve("site", []Target{{Name: "x", Version: "1", PreviousVersion: "0"}}); err == nil {
		t.Error("git target with a name must fail")
	}
	if _, err := baseModel().Resolve("ghost", nil); err == nil {
		t.Error("unknown project must fail")
	}
}

func TestResolveTimeoutsAndFree(t *testing.T) {
	m := baseModel()
	m.Projects[0].Steps = []Step{
		{Type: StepComposeUp, Timeout: Duration(90e9)},
		{Type: StepComposePull},
		{Type: StepCommand, Run: "true"},
		{Type: StepBackup, Volume: "data"},
		{Type: StepBackup, Command: "true"},
	}
	plan := mustResolve(t, m, "shop", []Target{{Name: "app", Version: "1", PreviousVersion: "0"}})
	if plan.Steps[0].Timeout.Seconds() != 90 || plan.Steps[1].Timeout != DefaultTimeout(StepComposePull) {
		t.Errorf("timeouts: %v %v", plan.Steps[0].Timeout, plan.Steps[1].Timeout)
	}
	free := []bool{false, false, true, false, true}
	for i, s := range plan.Steps {
		if s.IsFree() != free[i] {
			t.Errorf("step %d (%s) IsFree = %v", i, s.Type, s.IsFree())
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("it's"); got != `'it'\''s'` {
		t.Errorf("got %s", got)
	}
}

// A path full of shell metacharacters reaches /bin/sh -c as one unchanged argument, and nothing it contains
// is executed.
func TestCommandPlaceholdersAreShellSafe(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "executed")
	dir := root + "/a b'c\"d $(touch " + marker + ");`touch " + marker + "`;e"
	m := baseModel()
	m.Projects[0].Dir = dir
	m.Projects[0].Values = map[string]string{"note": `x' ; touch ` + marker + ` ; '`}
	m.Projects[0].Steps = []Step{{Type: StepCommand, Run: `printf '%s\n' {dir} {note} {version} {services}`}}
	plan := mustResolve(t, m, "shop", []Target{{Name: "app", Version: "1.0.0", PreviousVersion: "0.9.0"}})
	out, err := exec.Command("/bin/sh", "-c", plan.Steps[0].Shell).Output()
	if err != nil {
		t.Fatal(err)
	}
	want := dir + "\n" + m.Projects[0].Values["note"] + "\n1.0.0\napp\n"
	if string(out) != want {
		t.Errorf("shell saw\n%q\nwant\n%q", out, want)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a placeholder value was executed")
	}
}

// Typed steps take the value as one argument, with no quoting at all.
func TestTypedStepsAreNotQuoted(t *testing.T) {
	m := baseModel()
	m.Projects[0].Dir = "/srv/example/it's here"
	m.Projects[0].Values = map[string]string{"host": "shop.example.com"}
	m.Projects[0].Steps = []Step{{Type: StepCheckHTTP, URL: "https://{host}/v/{version}"}}
	plan := mustResolve(t, m, "shop", []Target{{Name: "app", Version: "1.0.0", PreviousVersion: "0.9.0"}})
	if plan.Steps[0].URL != "https://shop.example.com/v/1.0.0" || plan.Steps[0].Dir != "/srv/example/it's here" || strings.Contains(plan.Steps[0].URL, "'") {
		t.Errorf("%+v", plan.Steps[0])
	}
}

func TestListPlaceholderInSingleArgument(t *testing.T) {
	m := baseModel()
	m.Projects[0].Steps = []Step{{Type: StepGitCheckout, Tag: "x-{services}"}}
	if _, err := m.Resolve("shop", []Target{{Name: "app", Version: "1", PreviousVersion: "0"}}); err == nil || !strings.Contains(err.Error(), CodeListPlaceholder) {
		t.Errorf("got %v", err)
	}
}

func TestSetEnvRejectsLineBreaks(t *testing.T) {
	m := baseModel()
	m.Projects[0].Values = map[string]string{"x": "a\nINJECT=1"}
	m.Projects[0].Steps = []Step{{Type: StepSetEnv, Assign: []string{"K={x}"}}}
	if _, err := m.Resolve("shop", []Target{{Name: "app", Version: "1", PreviousVersion: "0"}}); err == nil {
		t.Error("a line break in an env value must be rejected")
	}
}
