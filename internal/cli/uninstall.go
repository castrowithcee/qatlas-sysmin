package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

func newUninstallCommand(opts *Options) *cobra.Command {
	var purge, yes bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the sysmin installation from this host",
		Long: "uninstall stops and disables the service and removes its unit, the program, and the user and\n" +
			"group sysmin. The configuration and state directories, which hold the vault, stay in place unless\n" +
			"--purge is given. Parts that are already gone are skipped. It needs root and asks for\n" +
			"confirmation on a terminal; --yes confirms.",
		Args: noVaultArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			s := opts.system()
			if err := requireRoot(s, "uninstall"); err != nil {
				return err
			}
			if err := requireConfirmation(s, c.InOrStdin(), yes, "uninstall"); err != nil {
				return err
			}
			question := "Remove sysmin: the service, its unit, the program, and the user and group sysmin. The vault and configuration stay in place."
			if purge {
				question = "Remove sysmin including its data: the service, its unit, the program, the user and group sysmin, and " +
					"ALL of its configuration and state, the vault included. This cannot be undone."
			}
			ok, err := confirm(c, yes, question)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("uninstall cancelled; nothing was changed")
			}
			if err := uninstall(c, s, purge); err != nil {
				return fmt.Errorf("uninstall: %w", err)
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "uninstalled", Value: true},
				{Name: "purged", Value: purge},
			}})
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also remove the configuration and state directories, the vault included")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm without asking")
	return cmd
}

// uninstall removes the installation step by step; a missing part is skipped, a failing step stops the run
// so that the remaining parts stay available for a retry.
func uninstall(c *cobra.Command, s system, purge bool) error {
	ctx := c.Context()
	unitExisted := false
	if _, err := os.Lstat(s.unitPath); err == nil {
		unitExisted = true
		if err := s.command(ctx, "systemctl", "disable", "--now", serviceName); err != nil {
			return fmt.Errorf("stop and disable the service: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := removeIfPresent(s.unitPath); err != nil {
		return err
	}
	if unitExisted {
		if err := s.command(ctx, "systemctl", "daemon-reload"); err != nil {
			return fmt.Errorf("reload systemd: %w", err)
		}
	}
	if err := removeIfPresent(s.binaryPath); err != nil {
		return err
	}
	// Exit status 6 of userdel and groupdel means the user or group does not exist.
	if err := s.command(ctx, "userdel", serviceName); err != nil && !exitedWith(err, 6) {
		return fmt.Errorf("remove user %s: %w", serviceName, err)
	}
	if err := s.command(ctx, "groupdel", serviceName); err != nil && !exitedWith(err, 6) {
		return fmt.Errorf("remove group %s: %w", serviceName, err)
	}
	if purge {
		for _, dir := range s.dataDirs {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
