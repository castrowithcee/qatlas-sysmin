package hostchange

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Executor runs commands. Tests replace it.
type Executor interface {
	// Run runs the command and returns its combined output, which is used for error text only.
	Run(ctx context.Context, c Command) ([]byte, error)
}

// SystemExecutor runs the commands on the host with a fixed environment.
type SystemExecutor struct{}

func (SystemExecutor) Run(ctx context.Context, c Command) ([]byte, error) {
	if err := validateCommand(c); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	if len(c.Secret) > 0 {
		cmd.Stdin = bytes.NewReader(c.Secret)
	}
	return cmd.CombinedOutput()
}

func validateCommand(c Command) error {
	if len(c.Argv) == 0 || c.Argv[0] == "" {
		return fmt.Errorf("a command has no program")
	}
	for _, a := range c.Argv {
		if bytes.IndexByte([]byte(a), 0) >= 0 {
			return fmt.Errorf("an argument of %s contains a NUL byte", c.Argv[0])
		}
		if len(c.Secret) > 0 && bytes.Contains([]byte(a), c.Secret) {
			return fmt.Errorf("a secret must not be part of the arguments of %s", c.Argv[0])
		}
	}
	if len(c.Secret) > 0 && bytes.Contains([]byte(c.label()), c.Secret) {
		return fmt.Errorf("the placeholder of %s contains the secret", c.Argv[0])
	}
	return nil
}
