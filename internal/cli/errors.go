package cli

import (
	"errors"

	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

// Exit codes are part of the public contract.
const (
	exitOK      = 0
	exitRuntime = 1
	exitUsage   = 2
)

// UsageError marks a usage or validation problem, which maps to exit code 2. Every other error is a
// runtime error and maps to exit code 1.
type UsageError struct{ Err error }

func (e *UsageError) Error() string { return e.Err.Error() }

func (e *UsageError) Unwrap() error { return e.Err }

// syntaxError is a usage error in the shape of the command line itself: an unknown command or flag, a flag
// value that is not accepted, or the wrong number of positional arguments. Only such an error is followed by
// the usage block.
type syntaxError struct{ usage *UsageError }

func (e *syntaxError) Error() string { return e.usage.Error() }

func (e *syntaxError) Unwrap() error { return e.usage }

// newSyntaxError marks err as a syntax error, which keeps exit code 2 and adds the usage block.
func newSyntaxError(err error) error { return &syntaxError{&UsageError{err}} }

// codeFor maps an error to its code. Callers branch on the code instead of parsing the message.
func codeFor(err error) output.Code {
	var usage *UsageError
	if errors.As(err, &usage) {
		return output.CodeUsage
	}
	return output.CodeRuntime
}

// exitCode maps an error from the command layer to the documented process exit code.
func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var usage *UsageError
	if errors.As(err, &usage) {
		return exitUsage
	}
	return exitRuntime
}
