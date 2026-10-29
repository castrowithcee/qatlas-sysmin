package execute

import (
	"context"
	"fmt"
)

// Command is one external program call. Name and Args are passed as argv; nothing is interpreted by a shell
// unless Name is a shell.
type Command struct {
	Name string
	Args []string
	// Dir is the working directory; empty means the one of the service.
	Dir string
	// Env holds extra KEY=value entries added to the environment the program inherits.
	Env []string
}

// Result is what a finished program produced.
type Result struct {
	// Output is the combined standard output and error, capped by the runner.
	Output []byte
	// Omitted counts the output bytes the runner dropped.
	Omitted  int64
	ExitCode int
}

// Runner starts external programs. Run returns an *ExitError for a non-zero exit status, the context's error
// when the context ended the program, and any other error when the program could not run. The Result is
// filled whenever the program started.
type Runner interface {
	Run(ctx context.Context, c Command) (Result, error)
}

// ExitError reports a program that ended with a non-zero exit status (-1 when a signal ended it).
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit status %d", e.Code) }
