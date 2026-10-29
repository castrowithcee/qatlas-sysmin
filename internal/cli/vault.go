package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/autounlock"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

func newVaultCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Manage how the vault is unlocked",
		Args:  noVaultArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newVaultBindCommand(opts), newVaultUnbindCommand(opts), newVaultStatusCommand(opts))
	return cmd
}

// newBinder returns the binder for dir; tests replace opts.binder so they never reach the real tools.
func (o *Options) newBinder(dir string) *autounlock.Binder {
	if o.binder != nil {
		return o.binder(dir)
	}
	return autounlock.NewBinder(dir)
}

func newVaultBindCommand(opts *Options) *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "bind",
		Short: "Let the service open the vault after every start without the passphrase",
		Long: "bind asks for the vault passphrase and seals a copy of the vault key to this machine with\n" +
			"systemd-creds, using the TPM2 when there is one and the host key otherwise. Anyone who can run\n" +
			"code as root on this machine can then open the vault. Changing the passphrase later needs no new\n" +
			"binding. Run it as root; the passphrase is read from the terminal, or from standard input when\n" +
			"there is no terminal.",
		Args: noVaultArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			mode, err := opts.newBinder(dir).Bind(c.Context(), c.InOrStdin())
			if err != nil {
				return err
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "unlock", Value: string(mode)},
				{Name: "credential", Value: autounlock.CredentialPath(dir)},
				{Name: "warning", Value: mode.Warning()},
			}})
		},
	}
	cmd.Flags().StringVar(&dir, "vault-dir", autounlock.DefaultVaultDir, "vault directory")
	return cmd
}

func newVaultUnbindCommand(opts *Options) *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "unbind",
		Short: "Remove the machine binding so the vault needs the passphrase after a start again",
		Args:  noVaultArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			removed, err := opts.newBinder(dir).Unbind()
			if err != nil {
				return err
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "unlock", Value: string(autounlock.ModeManual)},
				{Name: "removed", Value: removed},
			}})
		},
	}
	cmd.Flags().StringVar(&dir, "vault-dir", autounlock.DefaultVaultDir, "vault directory")
	return cmd
}

func noVaultArgs(_ *cobra.Command, args []string) error {
	if len(args) > 0 {
		return newSyntaxError(fmt.Errorf("unexpected argument %q", args[0]))
	}
	return nil
}
