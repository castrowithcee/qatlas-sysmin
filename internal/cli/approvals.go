package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

func newApprovalsCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approvals",
		Short: "Show the project changes that wait for approval",
		Long: "A project counts as approved only in the state an administrator confirmed. A project file that was\n" +
			"changed, added or removed by hand, or whose credential is another vault entry now, stays an open\n" +
			"approval and does not run until it is approved or discarded in the terminal interface.\n" +
			"These commands only show them.",
		Args: noVaultArgs,
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newApprovalsListCommand(opts), newApprovalsShowCommand(opts))
	return cmd
}

// approvalsError turns the codes of a read request into messages that say what to do.
func approvalsError(what string, err error) error {
	var ce *control.Error
	if errors.As(err, &ce) {
		switch ce.Code {
		case control.CodeLocked:
			return fmt.Errorf("%s: the vault is locked; unlock it in the terminal interface first", what)
		case control.CodeVaultAbsent:
			return fmt.Errorf("%s: there is no vault yet", what)
		case control.CodeNothingOpen:
			return fmt.Errorf("%s: the project has no open approval", what)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

func newApprovalsListCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the open approvals",
		Args:  noVaultArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var items []control.ApprovalSummary
			err := withService(socket, func(cl *client.Client) (err error) {
				items, err = cl.Approvals()
				return err
			})
			if err != nil {
				return approvalsError("approvals list", err)
			}
			result := output.Collection{Columns: []string{"project", "kind", "changes", "free_commands", "unverified"}}
			for _, it := range items {
				result.Rows = append(result.Rows, output.Row{
					"project": it.Project, "kind": it.Kind, "changes": int64(it.Changes),
					"free_commands": it.FreeCommands, "unverified": it.Unverified,
				})
			}
			return emit(c, opts, result)
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newApprovalsShowCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "show NAME",
		Short: "Show what differs from the approved state of a project",
		Long: "show lists every field of the project that differs from its approved state. A free command line,\n" +
			"which no typed building block constrains, is marked in the free column.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return newSyntaxError(fmt.Errorf("show takes exactly one project name, got %d arguments", len(args)))
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			var d control.ApprovalDetail
			err := withService(socket, func(cl *client.Client) (err error) {
				d, err = cl.Approval(args[0])
				return err
			})
			if err != nil {
				return approvalsError("approvals show", err)
			}
			result := output.Collection{Columns: []string{"project", "kind", "path", "old", "new", "free"}}
			for _, ch := range d.Details {
				result.Rows = append(result.Rows, output.Row{
					"project": d.Project, "kind": d.Kind, "path": ch.Path, "old": ch.Old, "new": ch.New, "free": ch.Free,
				})
			}
			if len(result.Rows) == 0 {
				result.Rows = append(result.Rows, output.Row{"project": d.Project, "kind": d.Kind, "free": false})
			}
			return emit(c, opts, result)
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}
