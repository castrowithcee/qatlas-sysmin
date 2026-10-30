// Package cli defines the command surface of the sysmin binary. Commands stay thin adapters: they parse
// global options, hand typed results to the encoders, and translate errors into exit codes.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/autounlock"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
	"github.com/castrowithcee/qatlas-sysmin/internal/helptopics"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
)

// version is overridden at build time with
// -ldflags "-X github.com/castrowithcee/qatlas-sysmin/internal/cli.version=<v>".
var version = "dev"

// Options carries the global flags to the commands.
type Options struct {
	// OutputName and FormatName are the raw values of --output and its deprecated alias --format.
	OutputName string
	FormatName string

	// Format is resolved from OutputName before a command runs.
	Format output.Format
	// Redactor removes secret values from anything the process prints.
	Redactor *redact.Redactor

	// binder builds the vault binder; nil means the real one. Tests set it.
	binder func(dir string) *autounlock.Binder
	// resolver answers the service's peer check; nil means the system's groups. Tests set it.
	resolver daemon.Resolver
	// readSecret asks for a secret on the terminal without echoing it; nil means the real terminal. Tests
	// set it.
	readSecret func(prompt string) ([]byte, error)
	// sys supplies the host access of update and uninstall; nil means the real host. Tests set it.
	sys *system
}

// Run executes the root command against the given streams and returns the process exit code. It never
// terminates the process, so callers and tests share the same path.
func Run(args []string, stdout, stderr io.Writer) int {
	opts := &Options{Redactor: &redact.Redactor{}}
	return run(newRootCommand(opts), opts, args, stdout, stderr)
}

func run(cmd *cobra.Command, opts *Options, args []string, stdout, stderr io.Writer) int {
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	executed, err := cmd.ExecuteC()
	if err == nil {
		return exitOK
	}
	// Redaction happens before anything is shown.
	fmt.Fprintf(stderr, "sysmin: %s: %s\n", codeFor(err), opts.Redactor.Error(err))
	var syntax *syntaxError
	if errors.As(err, &syntax) {
		fmt.Fprint(stderr, executed.UsageString())
	}
	return exitCode(err)
}

func newRootCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sysmin",
		Short: "Set up and operate a Linux server through guided, repeatable scenarios",
		Long: "sysmin runs as a service on a Linux host and takes over the setup and operation of a server\n" +
			"through guided, repeatable scenarios. It is at an early stage of development.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return newSyntaxError(fmt.Errorf("unknown command %q", args[0]))
			}
			return nil
		},
		PersistentPreRunE: func(c *cobra.Command, _ []string) error {
			name := opts.OutputName
			if c.Flags().Changed("format") {
				if c.Flags().Changed("output") {
					return newSyntaxError(errors.New("--output and --format cannot be used together"))
				}
				fmt.Fprintln(c.ErrOrStderr(), "sysmin: notice: --format is deprecated, use --output")
				name = opts.FormatName
			}
			if name == "" {
				name = string(output.FormatTable)
			}
			format, err := output.ParseFormat(name)
			if err != nil {
				return newSyntaxError(err)
			}
			opts.Format = format
			return nil
		},
		// Without a subcommand there is nothing to do yet, so the root command prints its own help.
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}

	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return newSyntaxError(err) })

	cmd.Version = version
	cmd.SetVersionTemplate("sysmin {{.Version}}\n")
	cmd.PersistentFlags().StringVar(&opts.OutputName, "output", "", "output format: table, json, or compact (default table)")
	cmd.PersistentFlags().StringVar(&opts.FormatName, "format", "", "")
	_ = cmd.PersistentFlags().MarkHidden("format")

	cmd.AddCommand(newVersionCommand(opts))
	cmd.AddCommand(newLogCommand(opts))
	cmd.AddCommand(newVaultCommand(opts))
	cmd.AddCommand(newRunCommand(opts), newStatusCommand(opts), newApprovalsCommand(opts), newProjectCommand(opts),
		newCredentialCommand(opts), newNotifyCommand(opts), newSourceCommand(opts), newDeployCommand(opts),
		newBackupCommand(opts))
	cmd.AddCommand(newTUICommand(opts))
	cmd.AddCommand(newUpdateCommand(opts), newUninstallCommand(opts))
	// A command without a run function is a help topic: 'sysmin help <topic>' prints its text, and the
	// root help lists it under the additional help topics.
	for _, topic := range helptopics.All() {
		cmd.AddCommand(&cobra.Command{
			Use: topic.Name, Short: topic.Short, Long: helptopics.Wrap(topic.Text, 80),
		})
	}
	return cmd
}

func newVersionCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of sysmin",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return newSyntaxError(fmt.Errorf("unexpected argument %q", args[0]))
			}
			return nil
		},
		RunE: func(c *cobra.Command, _ []string) error {
			return emit(c, opts, output.Object{Fields: []output.Field{{Name: "version", Value: version}}})
		},
	}
}

// emit redacts and encodes a result to stdout. Only payload data reaches stdout.
func emit(c *cobra.Command, opts *Options, result output.Result) error {
	return output.Encode(c.OutOrStdout(), opts.Format, redactResult(opts.Redactor, result))
}

// redactResult removes known secrets from string values before an encoder escapes or serializes them.
// Non-string values keep their types, and the input result stays unchanged.
func redactResult(redactor *redact.Redactor, result output.Result) output.Result {
	if redactor == nil {
		return result
	}
	switch r := result.(type) {
	case output.Collection:
		rows := make([]output.Row, len(r.Rows))
		for i, row := range r.Rows {
			rows[i] = make(output.Row, len(row))
			for name, value := range row {
				if text, ok := value.(string); ok {
					value = redactor.Apply(text)
				}
				rows[i][name] = value
			}
		}
		return output.Collection{Columns: r.Columns, Rows: rows}
	case output.Object:
		fields := make([]output.Field, len(r.Fields))
		for i, field := range r.Fields {
			fields[i] = field
			if text, ok := field.Value.(string); ok {
				fields[i].Value = redactor.Apply(text)
			}
		}
		return output.Object{Fields: fields}
	default:
		return result
	}
}
