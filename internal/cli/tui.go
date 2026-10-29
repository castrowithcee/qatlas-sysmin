package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/selfupdate"
	"github.com/castrowithcee/qatlas-sysmin/internal/tui"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// configDirName is the folder of sysmin inside the user configuration directory.
const configDirName = "sysmin"

func newTUICommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal interface",
		Long: "Open the terminal interface. It needs a terminal on both standard input and standard output.\n" +
			"It talks to the running service; without the service it still opens and says what to check.\n" +
			"Device-local preferences are read from tui.yaml in the sysmin folder of the user configuration\n" +
			"directory; a missing file means the defaults.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return newSyntaxError(fmt.Errorf("unexpected argument %q", args[0]))
			}
			return nil
		},
		RunE: func(c *cobra.Command, _ []string) error {
			in, out, ok := terminalFiles(c)
			if !ok {
				return &UsageError{errors.New("the terminal interface needs a terminal on standard input and standard output")}
			}
			var prefs uiprefs.Prefs
			var warnings []string
			prefsPath := ""
			if dir, err := os.UserConfigDir(); err != nil {
				prefs = uiprefs.Default()
				warnings = []string{fmt.Sprintf("no configuration directory, using the defaults: %v", err)}
			} else {
				prefsPath = uiprefs.Path(filepath.Join(dir, configDirName))
				prefs, warnings = uiprefs.Load(prefsPath)
			}
			backend := tui.NewBackend(socket)
			err := tui.Run(tui.Options{Prefs: prefs, PrefsWarnings: warnings, PrefsPath: prefsPath, Redactor: opts.Redactor,
				Backend: backend, Version: version, Releases: releaseCheck(opts), In: in, Out: out})
			backend.Close()
			var upd *tui.UpdateRequest
			switch {
			case errors.Is(err, tui.ErrRegroup):
				return execWithGroup(os.Args)
			case errors.As(err, &upd):
				return updateAndRestart(upd.Version, os.Args)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

// The calls execWithGroup and updateAndRestart make to the host; variables so a test can stand in for them.
var (
	lookPath   = exec.LookPath
	executable = os.Executable
	execv      = unix.Exec
	runCommand = func(c *exec.Cmd) error { return c.Run() }
)

// execWithGroup replaces this process by the same command under sudo -g sysmin, which carries the group without
// a new login. It returns only on failure.
func execWithGroup(args []string) error {
	sudo, err := lookPath("sudo")
	if err != nil {
		return fmt.Errorf("sudo is not installed; log in again to use the group %s", control.ServiceGroup)
	}
	exe, err := executable()
	if err != nil {
		return fmt.Errorf("cannot find this program: %w", err)
	}
	argv := append([]string{"sudo", "-g", control.ServiceGroup, exe}, args[1:]...)
	if err := execv(sudo, argv, os.Environ()); err != nil {
		return fmt.Errorf("cannot run sudo: %w", err)
	}
	return nil
}

// updateAndRestart installs version through sudo sysmin update on the terminal, then starts the interface
// again: the new program after an update, the old one after a failure, once enter was pressed so the failure
// can be read. It returns only when neither can start.
func updateAndRestart(version string, args []string) error {
	sudo, err := lookPath("sudo")
	if err != nil {
		return errors.New("sudo is not installed; run sysmin update as root")
	}
	exe, err := executable()
	if err != nil {
		return fmt.Errorf("cannot find this program: %w", err)
	}
	update := exec.Command(sudo, exe, "update", "--to", version, "--yes")
	update.Stdin, update.Stdout, update.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := runCommand(update); err != nil {
		fmt.Fprintf(os.Stderr, "\nsysmin update did not finish (%v). Press enter to return to the interface.", err)
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
	if err := execv(exe, args, os.Environ()); err != nil {
		return fmt.Errorf("cannot start the interface again: %w", err)
	}
	return nil
}

// terminalFiles returns the standard streams of c when both are terminals. A buffer or a pipe is no terminal.
func terminalFiles(c *cobra.Command) (in, out *os.File, ok bool) {
	in, inOK := c.InOrStdin().(*os.File)
	out, outOK := c.OutOrStdout().(*os.File)
	if !inOK || !outOK || !isTerminal(in) || !isTerminal(out) {
		return nil, nil, false
	}
	return in, out, true
}

func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	return err == nil
}

// releaseCheck looks up the newest stable release for the update page of the terminal interface, the way
// update --list does. A development build has no release version to compare, which the page shows as such.
func releaseCheck(opts *Options) tui.ReleaseCheck {
	return func(ctx context.Context) (tui.ReleaseInfo, error) {
		entries, err := opts.system().updater(version, "").List(ctx)
		if errors.Is(err, selfupdate.ErrDevelopmentBuild) {
			return tui.ReleaseInfo{Dev: true}, nil
		}
		if err != nil {
			return tui.ReleaseInfo{}, err
		}
		// The entries come newest first, so the first stable one that is newer than this program is the newest.
		for _, e := range entries {
			if !e.Prerelease && !e.Current && !e.Older {
				return tui.ReleaseInfo{Latest: e.Version}, nil
			}
		}
		return tui.ReleaseInfo{}, nil
	}
}
