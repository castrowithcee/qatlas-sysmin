package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/output"
	"github.com/castrowithcee/qatlas-sysmin/internal/selfupdate"
)

const lockedNotice = "The service restarts and the vault is locked afterwards unless automatic unlock is set up."

func newUpdateCommand(opts *Options) *cobra.Command {
	var to, releaseURL string
	var list, yes bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update sysmin to the latest stable release or to a chosen one",
		Long: "update replaces the installed program and its systemd unit with those of a signed release and\n" +
			"restarts the service; drop-ins of the unit stay as they are. Without arguments it installs the\n" +
			"newest stable release; --to installs exactly the given version, an older one included; --list\n" +
			"shows the signed releases that can be chosen, prereleases included.\n" +
			"The signature over the release's checksums.txt is verified against the release keys built into\n" +
			"sysmin, then the archive's SHA-256 checksum; any failure leaves the installation unchanged.\n" +
			"Installing needs root. The service restarts afterwards and the vault is locked again unless\n" +
			"automatic unlock is set up, so update asks for confirmation on a terminal; --yes confirms.",
		Args: noVaultArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runUpdate(c, opts, updateRequest{to: to, list: list, yes: yes, releaseURL: releaseURL})
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "install this release version, for example v1.2.3")
	cmd.Flags().BoolVar(&list, "list", false, "list the releases that can be installed")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm without asking")
	cmd.Flags().StringVar(&releaseURL, "release-url", "",
		"read releases from this GitHub-API-compatible base URL instead of GitHub; signatures are still\n"+
			"checked against the built-in release keys")
	return cmd
}

type updateRequest struct {
	to, releaseURL string
	list, yes      bool
}

func runUpdate(c *cobra.Command, opts *Options, req updateRequest) error {
	if req.list && (req.to != "" || req.yes) {
		return newSyntaxError(errors.New("--list cannot be combined with --to or --yes"))
	}
	s := opts.system()
	updater := s.updater(version, req.releaseURL)
	updater.Target = s.binaryPath
	updater.UnitTarget = s.unitPath
	ctx := c.Context()

	if req.list {
		entries, err := updater.List(ctx)
		if err != nil {
			return fmt.Errorf("update: %w", err)
		}
		rows := make([]output.Row, len(entries))
		for i, e := range entries {
			rows[i] = output.Row{"version": e.Version, "channel": channel(e.Prerelease), "state": entryState(e)}
		}
		return emit(c, opts, output.Collection{Columns: []string{"version", "channel", "state"}, Rows: rows})
	}

	if err := requireRoot(s, "update"); err != nil {
		return err
	}
	if version == "" || version == "dev" {
		return selfupdate.ErrDevelopmentBuild
	}
	if err := requireConfirmation(s, c.InOrStdin(), req.yes, "update"); err != nil {
		return err
	}
	target, err := updater.Resolve(ctx, req.to)
	if err != nil {
		var te *selfupdate.TargetError
		if errors.As(err, &te) && req.to != "" {
			return &UsageError{err}
		}
		return fmt.Errorf("update: %w", err)
	}
	// Without --to the newest stable release never replaces a newer running build.
	if target.Current || (req.to == "" && target.Older) {
		return emitUpdate(c, opts, version, version, false, false, false)
	}

	action := "Update"
	if target.Older {
		action = "Roll back"
	}
	ok, err := confirm(c, req.yes, fmt.Sprintf("%s sysmin from %s to %s. %s", action, version, target.Version, lockedNotice))
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("update cancelled; nothing was changed")
	}

	if f, ok := c.ErrOrStderr().(*os.File); ok && isTerminal(f) {
		updater.Progress = progressPrinter(c.ErrOrStderr())
	}
	result, err := updater.Install(ctx, target)
	if err != nil {
		if result.UnitUpdated {
			// The unit is already new; let the service manager know even though the program is not.
			_ = s.command(ctx, "systemctl", "daemon-reload")
			return fmt.Errorf("update: the unit was replaced, but installing the program failed: %w", err)
		}
		return fmt.Errorf("update: %w", err)
	}
	// The service manager reads a replaced unit before the restart, which happens once the program is in place.
	if result.UnitUpdated {
		if err := s.command(ctx, "systemctl", "daemon-reload"); err != nil {
			return fmt.Errorf("update: sysmin %s is installed, but reloading the service manager failed: %w", result.Version, err)
		}
	}
	if err := s.command(ctx, "systemctl", "restart", serviceName); err != nil {
		return fmt.Errorf("update: sysmin %s is installed, but restarting the service failed: %w", result.Version, err)
	}
	return emitUpdate(c, opts, result.Previous, result.Version, true, true, result.UnitUpdated)
}

func emitUpdate(c *cobra.Command, opts *Options, previous, current string, updated, restarted, unitUpdated bool) error {
	return emit(c, opts, output.Object{Fields: []output.Field{
		{Name: "previous", Value: previous},
		{Name: "version", Value: current},
		{Name: "updated", Value: updated},
		{Name: "restarted", Value: restarted},
		{Name: "unit_updated", Value: unitUpdated},
	}})
}

func channel(prerelease bool) string {
	if prerelease {
		return "prerelease"
	}
	return "stable"
}

func entryState(e selfupdate.Entry) string {
	switch {
	case e.Current:
		return "current"
	case e.Older:
		return "older"
	}
	return "newer"
}

func progressPrinter(w io.Writer) func(done, total int64) {
	last := int64(-1)
	return func(done, total int64) {
		pct := int64(-1)
		if total > 0 {
			pct = done * 100 / total
		}
		if pct == last {
			return
		}
		last = pct
		if pct >= 0 {
			fmt.Fprintf(w, "\rdownloading %d%%", pct)
			if pct == 100 {
				fmt.Fprintln(w)
			}
		}
	}
}
