package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestVersionTable(t *testing.T) {
	code, stdout, stderr := runCLI(t, "version")
	if code != exitOK || stderr != "" {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, version) {
		t.Errorf("stdout = %q, want it to contain %q", stdout, version)
	}
}

func TestVersionJSON(t *testing.T) {
	code, stdout, stderr := runCLI(t, "version", "--output", "json")
	if code != exitOK || stderr != "" {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout %q is no JSON object: %v", stdout, err)
	}
	if got["version"] != version || len(got) != 1 {
		t.Errorf("got %v, want only version %q", got, version)
	}
}

func TestVersionCompact(t *testing.T) {
	code, stdout, _ := runCLI(t, "version", "--output", "compact")
	if code != exitOK || !strings.Contains(stdout, version) {
		t.Errorf("code = %d, stdout = %q", code, stdout)
	}
}

func TestUnknownFormatIsUsageError(t *testing.T) {
	code, stdout, stderr := runCLI(t, "version", "--output", "bogus")
	if code != exitUsage {
		t.Errorf("code = %d, want %d", code, exitUsage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.HasPrefix(stderr, "sysmin: usage: ") || !strings.Contains(stderr, "bogus") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	code, _, stderr := runCLI(t, "nothing")
	if code != exitUsage || !strings.Contains(stderr, `unknown command "nothing"`) {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestUnknownFlagIsUsageError(t *testing.T) {
	code, _, stderr := runCLI(t, "version", "--nothing")
	if code != exitUsage || !strings.HasPrefix(stderr, "sysmin: usage: ") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestHelpTopic(t *testing.T) {
	code, stdout, _ := runCLI(t, "help", "sysmin")
	if code != exitOK || !strings.Contains(stdout, "Linux host") || !strings.Contains(stdout, "sysmin --help") ||
		strings.Contains(stdout, "exist so far") {
		t.Errorf("code = %d, stdout = %q", code, stdout)
	}
}

func TestTUIIsRegisteredAndRejectsArguments(t *testing.T) {
	code, _, stderr := runCLI(t, "tui", "extra")
	if code != exitUsage || !strings.Contains(stderr, "sysmin: usage: unexpected argument \"extra\"") ||
		!strings.Contains(stderr, "Usage:") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestTUIWithoutTerminalFailsCleanly(t *testing.T) {
	code, stdout, stderr := runCLI(t, "tui")
	if code != exitUsage || stdout != "" {
		t.Fatalf("code = %d, stdout = %q", code, stdout)
	}
	if !strings.Contains(stderr, "sysmin: usage: the terminal interface needs a terminal") ||
		strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestTUIAcceptsTheSocketFlag(t *testing.T) {
	code, _, stderr := runCLI(t, "tui", "--socket", "/nonexistent/sysmin.sock")
	if code != exitUsage || !strings.Contains(stderr, "the terminal interface needs a terminal") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestVersionFlag(t *testing.T) {
	code, stdout, stderr := runCLI(t, "--version")
	if code != exitOK || stderr != "" || stdout != "sysmin "+version+"\n" {
		t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestFormatAliasWarnsAndMatchesOutput(t *testing.T) {
	code, want, _ := runCLI(t, "version", "--output", "json")
	if code != exitOK {
		t.Fatalf("code = %d", code)
	}
	code, got, stderr := runCLI(t, "version", "--format", "json")
	if code != exitOK || got != want {
		t.Errorf("code = %d, stdout = %q, want %q", code, got, want)
	}
	if !strings.Contains(stderr, "--format is deprecated") {
		t.Errorf("stderr = %q, want a notice", stderr)
	}
}

func TestOutputAndFormatTogetherIsUsageError(t *testing.T) {
	code, stdout, stderr := runCLI(t, "version", "--output", "json", "--format", "json")
	if code != exitUsage || stdout != "" || !strings.Contains(stderr, "cannot be used together") {
		t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestOutputUnknownValueIsUsageError(t *testing.T) {
	code, _, _ := runCLI(t, "version", "--output", "yaml")
	if code != exitUsage {
		t.Errorf("code = %d, want %d", code, exitUsage)
	}
}

func TestLoadPrefsMigratesOldFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	oldPath := filepath.Join(home, ".config", "sysmin", "tui.yaml")
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("theme: terminal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, path, warnings := loadPrefs()
	if want := filepath.Join(home, ".qatlas", "sysmin", "tui.yaml"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "moved") {
		t.Errorf("warnings = %q", warnings)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("new file missing: %v", err)
	}
	// With the new file present, a second old file stays untouched.
	if err := os.WriteFile(oldPath, []byte("theme: other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, w := loadPrefs(); len(w) != 0 {
		t.Errorf("warnings = %q, want none", w)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Errorf("old file touched: %v", err)
	}
}
