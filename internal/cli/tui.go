package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-sysmin/internal/tui"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// configDirName is the folder of sysmin inside the user configuration directory.
const configDirName = "sysmin"

func newTUICommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the terminal interface",
		Long: "Open the terminal interface. It needs a terminal on both standard input and standard output.\n" +
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
			if dir, err := os.UserConfigDir(); err != nil {
				prefs = uiprefs.Default()
				warnings = []string{fmt.Sprintf("no configuration directory, using the defaults: %v", err)}
			} else {
				prefs, warnings = uiprefs.Load(uiprefs.Path(filepath.Join(dir, configDirName)))
			}
			return tui.Run(tui.Options{Prefs: prefs, PrefsWarnings: warnings, Redactor: opts.Redactor, In: in, Out: out})
		},
	}
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
