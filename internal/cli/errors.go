package cli

import (
	"errors"

	kitcli "github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
)

// Codes qsysmin adds to the shared ones; agentsGuide names each of them.
const (
	codeRootRequired  output.Code = "root-required"
	codeSessionGroup  output.Code = "session-group"
	codePeerMismatch  output.Code = "peer-mismatch"
	codeChangeRefused output.Code = "change-refused"
	codeChangeFailed  output.Code = "change-failed"
	codeUpdateFailed  output.Code = "update-failed"
	codeUninstallFail output.Code = "uninstall-failed"
	codeHostRead      output.Code = "host-read-failed"
	codeChainBroken   output.Code = "chain-broken"
)

// ownCodes are the codes qsysmin adds to the shared ones of the kit.
var ownCodes = []output.Code{
	client.CodeNotEnabled, codeRootRequired, codeSessionGroup, codePeerMismatch, codeChangeRefused,
	codeChangeFailed, codeUpdateFailed, codeUninstallFail, codeHostRead, codeChainBroken,
}

// askUserReport is the next step for an agent where only a person may act.
const askUserReport = "Tell the user what failed; do not retry on your own."

// failure is a runtime failure with the exit code 1 that keeps err as its cause.
func failure(code output.Code, err error, next, agentNext string) error {
	return &kitcli.Failure{Code: code, Message: err.Error(), NextStep: next, AgentNextStep: agentNext, Cause: err}
}

// usageFailure is a well-formed request that is refused with the exit code 2. No usage block follows.
func usageFailure(err error, next string) error {
	return &kitcli.Failure{Code: output.CodeUsage, Message: err.Error(), NextStep: next, Usage: true, Cause: err}
}

// syntaxFailure marks err as a mistake in the command line itself, which adds the usage of the command.
func syntaxFailure(err error) error { return cobracli.Syntax(err) }

func rootRequired(action string) error {
	return failure(codeRootRequired, errors.New(action+" must run as root"),
		"Repeat the command with sudo.",
		"Do not run this yourself; ask the user, who can do it in 'qsysmin tui'.")
}
