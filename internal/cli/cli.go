// Package cli defines the command surface of the qsysmin binary. Commands stay thin adapters: they parse
// global options, hand typed results to the encoders, and translate errors into exit codes.
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	kitcli "github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

// version is overridden at build time with
// -ldflags "-X github.com/castrowithcee/qatlas-sysmin/internal/cli.version=<v>".
var version = "dev"

// Options carries the program and the test seams to the commands.
type Options struct {
	// Redactor removes secret values from anything the process prints.
	Redactor *redact.Redactor

	// prog is the command tree's program; it resolves the output format and holds the confirmation gates.
	prog *cobracli.Program
	// resolver answers the service's peer check; nil means the system's groups. Tests set it.
	resolver daemon.Resolver
	// sys supplies the host access of update and uninstall; nil means the real host. Tests set it.
	sys *system
	// host supplies the caller and the databases of the host commands; nil means the real host. Tests set it.
	host *hostEnv
	// change supplies the registry, executor, journal and clock of the host changes; nil means the real host.
	// Tests set it.
	change *changeEnv
}

// Run executes the root command against the given streams and returns the process exit code. It never
// terminates the process, so callers and tests share the same path.
func Run(args []string, stdout, stderr io.Writer) int {
	return run(&Options{Redactor: &redact.Redactor{}}, nil, args, stdout, stderr)
}

// run builds the command tree for opts and executes it; a nil stdin means the process's.
func run(opts *Options, stdin io.Reader, args []string, stdout, stderr io.Writer) int {
	root, err := newRootCommand(opts)
	if err != nil {
		fmt.Fprintf(stderr, "qsysmin: %s: %v\n", output.CodeRuntime, err)
		return kitcli.ExitRuntime
	}
	if stdin != nil {
		root.SetIn(stdin)
	}
	return opts.prog.Execute(args, stdout, stderr)
}

func newRootCommand(opts *Options) (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "qsysmin",
		Short: "Look after a Linux host: read it, change it with a journal, and undo changes",
		Long: "qsysmin runs as a service on a Linux host. Its area Host reads users, groups and folder permissions\n" +
			"and changes them with a journal that allows undoing. It is at an early stage of development.\n" +
			"AI agents: run 'qsysmin agents' first.",
		// Without a subcommand there is nothing to do yet, so the root command prints its own help.
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	prog, err := cobracli.New(cmd, cobracli.Options{
		Version: version, Topics: helpTopics, Redactor: opts.Redactor,
		OutputHelp: "output format: table (default), json, compact (default with --agent), or toon",
	})
	if err != nil {
		return nil, err
	}
	opts.prog = prog

	cmd.AddCommand(newVersionCommand(opts), newAgentsCommand())
	cmd.AddCommand(newLogCommand(opts))
	cmd.AddCommand(newRunCommand(opts), newStatusCommand(opts), newHostCommand(opts))
	cmd.AddCommand(newTUICommand(opts))
	cmd.AddCommand(newUpdateCommand(opts), newUninstallCommand(opts))
	return cmd, nil
}

func newVersionCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of qsysmin",
		RunE: func(c *cobra.Command, _ []string) error {
			return emit(c, opts, output.Object{Fields: []output.Field{{Name: "version", Value: version}}})
		},
	}
}

// emit redacts and encodes a result to stdout. Only payload data reaches stdout.
func emit(c *cobra.Command, opts *Options, result output.Result) error {
	return output.Encode(c.OutOrStdout(), opts.prog.Format(), joinLists(opts.prog.Format(), redactResult(opts.Redactor, result)))
}

// joinLists writes the lists of a result as comma-separated text for the text formats, which take only
// scalars. JSON keeps them as lists.
func joinLists(format output.Format, result output.Result) output.Result {
	if format == output.FormatJSON {
		return result
	}
	join := func(value any) any {
		if list, ok := value.([]string); ok {
			return strings.Join(list, ",")
		}
		return value
	}
	switch r := result.(type) {
	case output.Collection:
		rows := make([]output.Row, len(r.Rows))
		for i, row := range r.Rows {
			rows[i] = make(output.Row, len(row))
			for name, value := range row {
				rows[i][name] = join(value)
			}
		}
		return output.Collection{Columns: r.Columns, Rows: rows}
	case output.Object:
		fields := make([]output.Field, len(r.Fields))
		for i, field := range r.Fields {
			fields[i] = output.Field{Name: field.Name, Value: join(field.Value)}
		}
		return output.Object{Fields: fields}
	}
	return result
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
