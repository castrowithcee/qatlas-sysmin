package autounlock

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// Runner runs one of the systemd tools. stdin carries secret material, so it must never end up in an error,
// a log, or an argument. It is an interface so that tests never touch the host's real tools.
type Runner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) (stdout []byte, err error)
}

// The only programs the real runner starts.
const (
	credsProgram   = "systemd-creds"
	analyzeProgram = "systemd-analyze"
)

// ExecRunner returns the Runner that starts the real programs through os/exec. It refuses to run inside a
// test binary, so no test can reach the host's systemd tools, whoever it runs as.
func ExecRunner() Runner { return execRunner{} }

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	if testing.Testing() {
		return nil, errors.New("the real command runner is disabled in test binaries")
	}
	if name != credsProgram && name != analyzeProgram {
		return nil, fmt.Errorf("refusing to run %q", name)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return stdout.Bytes(), fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return stdout.Bytes(), fmt.Errorf("%s: %w", name, err)
	}
	return stdout.Bytes(), nil
}
