package cli

import (
	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

func newBackupCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "List the backups of a project",
		Long: "A deploy makes a backup before it changes anything. Restoring one is done in the terminal interface\n" +
			"(sysmin tui), which asks for the vault passphrase.",
		Args: noArgs,
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newBackupListCommand(opts))
	return cmd
}

func newBackupListCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "list PROJECT",
		Short: "List the backups of a project",
		Long: "list shows the names of the backups of a project, the oldest first. Nothing is changed, no admin\n" +
			"session is needed, and the vault may be locked.",
		Example: "  sysmin backup list demo-app --output json",
		Args:    oneName("list"),
		RunE: func(c *cobra.Command, args []string) error {
			var names []string
			err := withService(socket, func(cl *client.Client) (err error) {
				names, err = cl.Backups(args[0])
				return err
			})
			if err != nil {
				return deployError("backup list", err)
			}
			result := output.Collection{Columns: []string{"backup"}}
			for _, n := range names {
				result.Rows = append(result.Rows, output.Row{"backup": n})
			}
			return emit(c, opts, result)
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}
