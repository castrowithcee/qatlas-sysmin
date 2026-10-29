package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

// withService runs fn on a connection to the service at socket. Every failure is a runtime error with a
// message that says what to check.
func withService(socket string, fn func(*client.Client) error) error {
	c, err := client.Dial(socket)
	if err != nil {
		return fmt.Errorf("%w (is the service running, and are you root or in the group sysmin?)", err)
	}
	defer c.Close()
	err = fn(c)
	var ce *control.Error
	if errors.As(err, &ce) && ce.Code == control.CodeRefused {
		return errors.New("the service does not allow this user; only root and members of the group sysmin may use it")
	}
	return err
}

func newStatusCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the state of the running service",
		Args:  noVaultArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var st control.StatusResult
			err := withService(socket, func(cl *client.Client) (err error) {
				st, err = cl.Status()
				return err
			})
			if err != nil {
				return fmt.Errorf("status: %w", err)
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "version", Value: st.Version},
				{Name: "vault", Value: st.Vault},
				{Name: "unlock", Value: st.Unlock},
				{Name: "warning", Value: st.Warning},
				{Name: "socket", Value: st.Socket},
			}})
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newVaultStatusCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show whether the service's vault is locked and how it was unlocked",
		Args:  noVaultArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var st control.VaultStatusResult
			err := withService(socket, func(cl *client.Client) (err error) {
				st, err = cl.VaultStatus()
				return err
			})
			if err != nil {
				return fmt.Errorf("vault status: %w", err)
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "vault", Value: st.Vault},
				{Name: "unlock", Value: st.Unlock},
				{Name: "warning", Value: st.Warning},
			}})
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}
