package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/selfupdate"
	"github.com/castrowithcee/qatlas-sysmin/internal/release"
)

const (
	// releaseAPI is the GitHub API base of the public repository the releases come from.
	releaseAPI = "https://api.github.com/repos/castrowithcee/qatlas-sysmin"
	// minimumVersion is the oldest release that can be listed or installed.
	minimumVersion = "v0.7.0"
	// unitEntry is the systemd unit inside the release archive.
	unitEntry = "lib/systemd/system/qsysmin.service"
	// manEntry is the manpage inside the release archive.
	manEntry = "share/man/man1/qsysmin.1"
)

// errDevelopmentBuild reports an update attempt by a build without a release version.
var errDevelopmentBuild = errors.New("self-update is unavailable in a dev build; install a tagged release first")

// archiveNamer returns the archive name of a release version for arch, or "" when no release is published
// for arch.
func archiveNamer(arch string) func(version string) string {
	return func(version string) string {
		if arch != "amd64" && arch != "arm64" {
			return ""
		}
		return fmt.Sprintf("qsysmin_%s_linux_%s.tar.gz", version, arch)
	}
}

// newUpdateClient returns the release client for the running program. apiURL replaces the GitHub API base;
// it never changes which keys are trusted.
func newUpdateClient(version, apiURL string) *selfupdate.Client {
	api := strings.TrimRight(apiURL, "/")
	if api == "" {
		api = releaseAPI
	}
	return &selfupdate.Client{
		RepositoryAPI:    api,
		CurrentVersion:   version,
		MinimumVersion:   minimumVersion,
		ArchiveName:      archiveNamer(runtime.GOARCH),
		UserAgent:        "qsysmin/" + version,
		Namespace:        release.Namespace,
		TrustedKeys:      release.TrustedKeys(),
		ProgramName:      "qsysmin",
		ValidateRollback: validateRollback,
	}
}

// validateRollback lets an older release in only if its program starts on this host. qsysmin reads no
// configuration file, so the release's own run, isolated from the host's files and environment, is the whole
// check; data an older release cannot read is kept out by minimumVersion.
func validateRollback(ctx context.Context, p *selfupdate.Prepared) error {
	ok, err := p.ReadsConfig(ctx, selfupdate.ConfigCheck{
		FileName: "check.conf",
		Args:     func(string) []string { return []string{"--help"} },
	})
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("the older program does not start on this host")
	}
	return nil
}

func newUpdateCommand(opts *Options) *cobra.Command {
	var to, apiURL string
	var list bool
	var gate *cobracli.Gate
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update qsysmin to the latest stable release or to a chosen one",
		Long: "update replaces the installed program, its systemd unit and its manpage with those of a signed release and\n" +
			"restarts the service; drop-ins of the unit stay as they are. Without arguments it installs the\n" +
			"newest stable release; --to installs exactly the given version, an older one included; --list\n" +
			"shows the signed releases that can be chosen, prereleases included.\n" +
			"The signature over the release's checksums.txt is verified against the release keys built into\n" +
			"qsysmin, then the archive's SHA-256 checksum; any failure leaves the installation unchanged.\n" +
			"Installing needs root. The service restarts afterwards, so installing needs --confirm and asks for\n" +
			"confirmation on a terminal; --yes answers the question.",
		RunE: func(c *cobra.Command, _ []string) error {
			return runUpdate(c, opts, gate, updateRequest{to: to, list: list, apiURL: apiURL})
		},
	}
	gate = opts.prog.AddGate(cmd, cobracli.Gates{Question: true, Change: true})
	cmd.Flags().StringVar(&to, "to", "", "install this release version, for example v1.2.3")
	cmd.Flags().BoolVar(&list, "list", false, "list the releases that can be installed")
	cmd.Flags().StringVar(&apiURL, "api-url", "",
		"read releases from this GitHub-API-compatible base URL instead of GitHub, in the form\n"+
			"https://api.github.com/repos/<owner>/<repo>; signatures are still checked against the\n"+
			"built-in release keys")
	return cmd
}

type updateRequest struct {
	to, apiURL string
	list       bool
}

func runUpdate(c *cobra.Command, opts *Options, gate *cobracli.Gate, req updateRequest) error {
	if req.list && (req.to != "" || c.Flags().Changed("yes") || c.Flags().Changed("confirm")) {
		return syntaxFailure(errors.New("--list cannot be combined with --to, --yes or --confirm"))
	}
	s := opts.system()
	updater := s.updater(version, req.apiURL)
	updater.Executable = s.binaryPath
	updater.Companions = []selfupdate.Companion{{Entry: unitEntry, Path: s.unitPath}, {Entry: manEntry, Path: s.manPath}}
	if updater.ArchiveName("v0.0.0") == "" {
		return updateError(fmt.Errorf("no release is published for architecture %s", runtime.GOARCH))
	}
	ctx := c.Context()

	if req.list {
		entries, err := updater.List(ctx)
		if err != nil {
			return updateError(err)
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
		return updateError(errDevelopmentBuild)
	}
	// A request without --confirm fails before the release server is asked.
	if err := gate.Run(cobracli.Request{Changes: true}, func() error { return nil }); err != nil {
		return err
	}
	tag := req.to
	if tag == "" {
		// Without --to the newest stable release never replaces a newer running build.
		check, err := updater.Check(ctx)
		if err != nil {
			return updateError(err)
		}
		if !check.UpdateAvailable {
			return emitUpdate(c, opts, version, version, false, false, false)
		}
		tag = check.Latest
	}
	target, err := updater.Target(ctx, tag)
	if err != nil {
		var te *selfupdate.TargetError
		if errors.As(err, &te) && req.to != "" {
			return usageFailure(err, "Choose a version from 'qsysmin update --list'.")
		}
		return updateError(err)
	}
	if target.Current {
		return emitUpdate(c, opts, version, version, false, false, false)
	}

	action := "Update"
	if target.Older {
		action = "Roll back"
	}
	question := fmt.Sprintf("%s qsysmin from %s to %s. The service restarts.", action, version, target.Version)
	return gate.Run(cobracli.Request{Question: question, Changes: true}, func() error {
		return install(c, opts, s, updater, target)
	})
}

func install(c *cobra.Command, opts *Options, s system, updater *selfupdate.Client, target selfupdate.Target) error {
	ctx := c.Context()
	if f, ok := c.ErrOrStderr().(*os.File); ok && isTerminal(f) {
		updater.Progress = progressPrinter(c.ErrOrStderr())
	}
	prepared, err := updater.Prepare(ctx, target)
	if err != nil {
		return updateError(err)
	}
	result, err := updater.InstallPrepared(ctx, prepared)
	if err != nil {
		return updateError(err)
	}
	// The service manager reads a replaced unit before the restart, which happens once the program is in place.
	unitUpdated := slices.Contains(result.Companions, s.unitPath)
	if unitUpdated {
		if err := s.command(ctx, "systemctl", "daemon-reload"); err != nil {
			return updateError(fmt.Errorf("qsysmin %s is installed, but reloading the service manager failed: %w", result.Latest, err))
		}
	}
	if err := s.command(ctx, "systemctl", "restart", serviceName); err != nil {
		return updateError(fmt.Errorf("qsysmin %s is installed, but restarting the service failed: %w", result.Latest, err))
	}
	return emitUpdate(c, opts, result.Current, result.Latest, true, true, unitUpdated)
}

// updateError is a failed update or release lookup; the message says whether the installation changed.
func updateError(err error) error {
	return failure(codeUpdateFailed, fmt.Errorf("update: %w", err),
		"Read the message: the installation is unchanged unless it says otherwise. Fix the cause, then repeat the update.",
		askUserReport)
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
