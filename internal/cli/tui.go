package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	kitcli "github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/selfupdate"
	kitprefs "github.com/castrowithcee/qatlas-kit/uiprefs"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/tui"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// loadPrefs resolves ~/.qatlas/qsysmin/tui.yaml and loads it under the schema of the editor.
func loadPrefs() (prefs kitprefs.Prefs, path string, warnings []string) {
	schema := tui.PrefsSchema()
	dir, err := uiprefs.UserDir()
	if err != nil {
		return kitprefs.Default(schema), "", []string{fmt.Sprintf("no home directory, using the defaults: %v", err)}
	}
	path = kitprefs.Path(dir)
	prefs, warnings = kitprefs.Load(path, schema)
	return prefs, path, warnings
}

func newTUICommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal interface",
		Long: "Open the terminal interface. It needs a terminal on both standard input and standard output.\n" +
			"It talks to the running service; without the service it still opens and says what to check.\n" +
			"Device-local preferences are read from ~/.qatlas/qsysmin/tui.yaml; a missing file means\n" +
			"the defaults.",
		RunE: func(c *cobra.Command, _ []string) error {
			in, out, ok := terminalFiles(c)
			if !ok {
				return &kitcli.Failure{
					Code: output.CodeUsage, Message: "the terminal interface needs a terminal on standard input and standard output",
					NextStep:      "Run 'qsysmin tui' in a terminal.",
					AgentNextStep: "Do not run the terminal interface; read with the commands in 'qsysmin agents'.",
					Usage:         true,
				}
			}
			prefs, prefsPath, warnings := loadPrefs()
			backend := tui.NewBackend(socket)
			err := tui.Run(tui.Options{Prefs: prefs, PrefsWarnings: warnings, PrefsPath: prefsPath, Redactor: opts.Redactor,
				Backend: backend, Version: version, Releases: releaseCheck(opts), In: in, Out: out,
				HostRun: hostRunner})
			backend.Close()
			if denied := accessError(err); denied != err {
				return denied
			}
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

// hostRunner builds `sudo <this program> host …` for the interface. sudo prompts on the terminal, so the
// password never reaches this program. Standard input belongs to that prompt, so a request with a secret runs
// in two steps: the interface first runs the Validate request (sudo -v) on the terminal, then this one with
// sudo -n, which never prompts, and the secret on the standard input of the command. The secret goes into no
// argument and no environment variable; the C locale is for the interface to recognise the answer of sudo
// when it holds no password.
func hostRunner(req tui.HostRequest) (*exec.Cmd, error) {
	sudo, err := lookPath("sudo")
	if err != nil {
		return nil, tui.ErrNoSudo
	}
	if req.Validate {
		return exec.Command(sudo, "-v"), nil
	}
	exe, err := executable()
	if err != nil {
		return nil, fmt.Errorf("cannot find this program: %w", err)
	}
	if len(req.Secret) == 0 {
		return exec.Command(sudo, append([]string{exe, "host"}, req.Args...)...), nil
	}
	cmd := exec.Command(sudo, append([]string{"-n", exe, "host"}, req.Args...)...)
	cmd.Stdin = bytes.NewReader(req.Secret)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	return cmd, nil
}

// execWithGroup replaces this process by the same command under sudo -g qsysmin, which carries the group without
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

// updateAndRestart installs version through sudo qsysmin update on the terminal, then starts the interface
// again: the new program after an update, the old one after a failure, once enter was pressed so the failure
// can be read. It returns only when neither can start.
func updateAndRestart(version string, args []string) error {
	sudo, err := lookPath("sudo")
	if err != nil {
		return errors.New("sudo is not installed; run qsysmin update as root")
	}
	exe, err := executable()
	if err != nil {
		return fmt.Errorf("cannot find this program: %w", err)
	}
	update := exec.Command(sudo, exe, "update", "--to", version, "--yes", "--confirm")
	update.Stdin, update.Stdout, update.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := runCommand(update); err != nil {
		fmt.Fprintf(os.Stderr, "\nqsysmin update did not finish (%v). Press enter to return to the interface.", err)
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
