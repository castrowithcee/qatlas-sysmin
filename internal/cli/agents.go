package cli

import (
	"github.com/spf13/cobra"
)

func newAgentsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "agents",
		Short: "Print the guide for AI agents",
		Long:  "agents prints what an AI agent may run, how to read the output, and what each error code asks it to do.",
		RunE: func(c *cobra.Command, _ []string) error {
			_, err := c.OutOrStdout().Write([]byte(agentsGuide))
			return err
		},
	}
}

// agentsGuide is the whole answer of 'qsysmin agents'. It names every error code the program can emit.
const agentsGuide = `qsysmin: guide for AI agents

Read only. Always pass --agent: compact output, no progress, no questions.
  qsysmin version --agent
  qsysmin status --agent
  qsysmin log verify --agent
  qsysmin host users --agent
  qsysmin host user <name> --agent
  qsysmin host groups --agent
  qsysmin host group <name> --agent
  qsysmin host folder <path> --agent
  qsysmin help <topic>        topics: qsysmin, host-admin

Never run: host apply, host undo, host journal, update, uninstall, run, tui.
Changes to the host are made by a person in 'qsysmin tui'. If a change is wanted,
tell the user what to do there. Do not pass --yes or --confirm for the user.

Errors: stderr has two lines, "qsysmin: <code>: <message>" and the next step. With --agent the next step
is the one for an agent. Exit code 0 is success, 1 a failure, 2 a usage problem.

Codes:
  usage                  the command line is wrong; fix it as the usage says
  confirmation-required  the request needs --yes or --confirm; ask the user, do not add them
  confirmation-declined  the user said no; nothing changed
  admin-required         the caller is no administrator (sudo); tell the user
  not-enabled            the account is not in the group qsysmin; tell the user
  root-required          the command needs root; tell the user
  unreachable            the service does not answer; tell the user, do not restart it
  session-group          the login session lacks the group qsysmin; tell the user
  peer-mismatch          another process answers at the socket; tell the user
  host-read-failed       a host read failed; check the name or path, then tell the user
  chain-broken           the action log chain is broken; tell the user, do not repair it
  change-refused         a rule refused a change; nothing ran
  change-failed          a change did not stand; tell the user
  update-failed          an update did not finish; tell the user
  uninstall-failed       an uninstall did not finish; tell the user
  runtime                any other failure; read the message, then tell the user
`
