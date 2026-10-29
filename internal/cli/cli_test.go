package cli

import (
	"bytes"
	"encoding/json"
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
	code, stdout, stderr := runCLI(t, "version", "--format", "json")
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
	code, stdout, _ := runCLI(t, "version", "--format", "compact")
	if code != exitOK || !strings.Contains(stdout, version) {
		t.Errorf("code = %d, stdout = %q", code, stdout)
	}
}

func TestUnknownFormatIsUsageError(t *testing.T) {
	code, stdout, stderr := runCLI(t, "version", "--format", "bogus")
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
	if code != exitOK || !strings.Contains(stdout, "Linux host") {
		t.Errorf("code = %d, stdout = %q", code, stdout)
	}
}
