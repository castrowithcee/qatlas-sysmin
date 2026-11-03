package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	kitcli "github.com/castrowithcee/qatlas-kit/cli"
	kithelp "github.com/castrowithcee/qatlas-kit/helptopics"
	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/output"

	"github.com/castrowithcee/qatlas-sysmin/internal/helptopics"
)

const (
	exitOK      = kitcli.ExitSuccess
	exitRuntime = kitcli.ExitRuntime
	exitUsage   = kitcli.ExitUsage
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
	if !strings.HasPrefix(stderr, "qsysmin: usage: ") || !strings.Contains(stderr, "bogus") {
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
	if code != exitUsage || !strings.HasPrefix(stderr, "qsysmin: usage: ") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestHelpTopic(t *testing.T) {
	code, stdout, _ := runCLI(t, "help", "qsysmin")
	if code != exitOK || !strings.Contains(stdout, "Linux host") || !strings.Contains(stdout, "qsysmin --help") ||
		strings.Contains(stdout, "exist so far") {
		t.Errorf("code = %d, stdout = %q", code, stdout)
	}
}

func TestTUIIsRegisteredAndRejectsArguments(t *testing.T) {
	code, _, stderr := runCLI(t, "tui", "extra")
	if code != exitUsage || !strings.Contains(stderr, "qsysmin: usage: unexpected argument \"extra\"") ||
		!strings.Contains(stderr, "Usage:") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestTUIWithoutTerminalFailsCleanly(t *testing.T) {
	code, stdout, stderr := runCLI(t, "tui")
	if code != exitUsage || stdout != "" {
		t.Fatalf("code = %d, stdout = %q", code, stdout)
	}
	if !strings.Contains(stderr, "qsysmin: usage: the terminal interface needs a terminal") ||
		strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestTUIAcceptsTheSocketFlag(t *testing.T) {
	code, _, stderr := runCLI(t, "tui", "--socket", "/nonexistent/qsysmin.sock")
	if code != exitUsage || !strings.Contains(stderr, "the terminal interface needs a terminal") {
		t.Errorf("code = %d, stderr = %q", code, stderr)
	}
}

func TestVersionFlag(t *testing.T) {
	code, stdout, stderr := runCLI(t, "--version")
	if code != exitOK || stderr != "" || stdout != "qsysmin "+version+"\n" {
		t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestFormatFlagIsGone(t *testing.T) {
	code, stdout, stderr := runCLI(t, "version", "--format", "json")
	if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "qsysmin: usage: unknown flag: --format\n") {
		t.Errorf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestAgentFlagSelectsCompact(t *testing.T) {
	_, want, _ := runCLI(t, "version", "--output", "compact")
	code, got, stderr := runCLI(t, "version", "--agent")
	if code != exitOK || stderr != "" || got != want {
		t.Errorf("code = %d, stdout = %q, want %q, stderr = %q", code, got, want, stderr)
	}
}

// A failure is two lines: the code with the message, then the next step, which differs for an agent.
func TestFailureHasCodeAndNextStep(t *testing.T) {
	h := newFakeHost(t, nil)
	_, _, human := h.run(t, "", "uninstall")
	_, _, agent := h.run(t, "", "uninstall", "--agent")
	for _, stderr := range []string{human, agent} {
		if lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n"); len(lines) != 2 ||
			lines[0] != "qsysmin: confirmation-required: this request needs --confirm" {
			t.Errorf("stderr = %q", stderr)
		}
	}
	h.root = false
	_, _, human = h.run(t, "", "uninstall", "--confirm")
	_, _, agent = h.run(t, "", "uninstall", "--confirm", "--agent")
	if !strings.Contains(human, "Repeat the command with sudo.") || !strings.Contains(agent, "Do not run this yourself") {
		t.Errorf("human = %q, agent = %q", human, agent)
	}
	_, _, agent = runCLI(t, "tui", "--agent")
	_, _, human = runCLI(t, "tui")
	if !strings.Contains(human, "Run 'qsysmin tui' in a terminal.") || !strings.Contains(agent, "Do not run the terminal interface") {
		t.Errorf("human = %q, agent = %q", human, agent)
	}
}

// Every command checks its arguments and the tree follows the conventions, or no command runs at all.
func TestEveryCommandRejectsUnexpectedArguments(t *testing.T) {
	root, err := newRootCommand(&Options{})
	if err != nil {
		t.Fatal(err)
	}
	var visit func(c *cobra.Command, path []string)
	visit = func(c *cobra.Command, path []string) {
		for _, sub := range c.Commands() {
			visit(sub, append(path[:len(path):len(path)], sub.Name()))
		}
		if !c.Runnable() {
			return // help topics print their text and take no argument check of their own
		}
		switch c.Name() {
		case "qsysmin", "help", "completion", "user", "group", "folder", "apply", "undo":
			return // the root, help and the commands with their own argument check
		}
		code, stdout, stderr := runCLI(t, append(append([]string{}, path...), "unexpected", "--agent")...)
		if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, "qsysmin: usage: ") {
			t.Errorf("%v: code = %d, stdout = %q, stderr = %q", path, code, stdout, stderr)
		}
	}
	visit(root, nil)
}

func TestAgentsGuide(t *testing.T) {
	code, stdout, stderr := runCLI(t, "agents")
	if code != exitOK || stderr != "" || stdout != agentsGuide {
		t.Fatalf("code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	for _, want := range []string{"--agent", "qsysmin host users", "qsysmin help <topic>", "Never run: host apply, host undo",
		"qsysmin tui", "Exit code 0", "usage", "confirmation-required", "admin-required", "runtime"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the guide lacks %q", want)
		}
	}
	if code, _, _ := runCLI(t, "agents", "extra"); code != exitUsage {
		t.Errorf("agents with an argument: code = %d", code)
	}
}

// The guide names each code qsysmin can emit exactly once, and the codes are valid and distinct.
func TestAgentsGuideNamesEveryCode(t *testing.T) {
	shared := []output.Code{output.CodeUsage, output.CodeConfirmationRequired, output.CodeConfirmationDeclined,
		output.CodeAdminRequired, output.CodeUnreachable, output.CodeRuntime}
	codes, err := output.AllCodes(ownCodes...)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range append(shared, ownCodes...) {
		if !slices.Contains(codes, code) {
			t.Errorf("code %q is not a valid code", code)
		}
		if n := strings.Count(agentsGuide, "\n  "+string(code)+" "); n != 1 {
			t.Errorf("the guide names %q %d times, want once", code, n)
		}
	}
}

func TestOutputUnknownValueIsUsageError(t *testing.T) {
	code, _, _ := runCLI(t, "version", "--output", "yaml")
	if code != exitUsage {
		t.Errorf("code = %d, want %d", code, exitUsage)
	}
}

// The command line serves the topics of the shared list, the one the help of the terminal interface shows.
func TestHelpTopicsAreTheSharedList(t *testing.T) {
	shared := helptopics.Topics(i18n.English)
	if !slices.Equal(helpTopics, shared) {
		t.Fatalf("the command line has topics %v, the shared list %v", helpTopics, shared)
	}
	for _, topic := range shared {
		code, out, _ := runCLI(t, topic.Name)
		if code != exitOK || !strings.Contains(out, kithelp.CLIText(topic)) {
			t.Errorf("qsysmin %s: code %d, output does not hold the topic:\n%s", topic.Name, code, out)
		}
	}
}

// The topics are plain ASCII, so a rune is a cell wherever they are shown, and each has a unique name.
func TestHelpTopicsArePlainAndDistinct(t *testing.T) {
	names := map[string]bool{}
	for _, topic := range helpTopics {
		if names[topic.Name] || topic.Name == "" || topic.Short == "" || topic.Text == "" {
			t.Errorf("topic %q is unnamed, repeated, or empty", topic.Name)
		}
		names[topic.Name] = true
		for i, r := range topic.Text {
			if r > '~' || (r < ' ' && r != '\n') {
				t.Errorf("topic %s has the character %q at byte %d", topic.Name, r, i)
			}
		}
	}
}
