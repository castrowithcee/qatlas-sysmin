package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

func newSourceCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "source",
		Short: "Check the version sources of a project",
		Args:  noArgs,
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newSourceTestCommand(opts))
	return cmd
}

// sourceError turns the codes of a source test into messages that say what to do.
func sourceError(what string, err error) error {
	var ce *control.Error
	if errors.As(err, &ce) {
		switch ce.Code {
		case control.CodeLocked:
			return fmt.Errorf("%s: the vault is locked; unlock it in the terminal interface first", what)
		case control.CodeNotApproved:
			return fmt.Errorf("%s: the project has no approved state; a person must approve it in the terminal interface (sysmin tui) first", what)
		case control.CodeBadRequest:
			return fmt.Errorf("%s: the service did not accept the request; check the project name", what)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

func newSourceTestCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "test PROJECT",
		Short: "Check that the sources of a project can be reached and show their versions",
		Long: "test reads the registries and repositories that the approved state of the project names, with the\n" +
			"credentials of that approved state, and shows for each image or git source the current version, the\n" +
			"available newer versions and the target version under the version rule, together with problems that\n" +
			"keep a target from being chosen. A change to the project that is not approved yet is not used. The\n" +
			"current version of an image is the value of its env_var in the env file of the project; that of a\n" +
			"git source is the tag checked out in the project directory. Nothing is changed, and no admin\n" +
			"session is needed, but the vault must be unlocked. The command exits with an error when a source\n" +
			"cannot be reached or refuses access; each source is tried regardless of the others.",
		Example: "  sysmin source test demo-app --format json",
		Args:    oneName("test"),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.SourceTestResult
			err := withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.SourceTest(args[0])
				return err
			})
			if err != nil {
				return sourceError("source test", err)
			}
			result := output.Collection{Columns: []string{"source", "kind", "ref", "status", "current", "available", "target", "digest", "source_repo", "problems", "message"}}
			failed := 0
			for _, s := range res.Sources {
				status := "ok"
				if s.Error != "" {
					status = s.Error
					failed++
				}
				var problems []string
				for _, p := range s.Problems {
					problems = append(problems, p.Code+": "+p.Message)
				}
				available := strings.Join(s.Available, ",")
				if s.AvailableTotal > len(s.Available) {
					available = fmt.Sprintf("...,%s (%d in total)", available, s.AvailableTotal)
				}
				result.Rows = append(result.Rows, output.Row{
					"source": s.Name, "kind": s.Kind, "ref": s.Ref, "status": status, "current": s.Current,
					"available": available, "target": s.Target, "digest": s.Digest, "source_repo": s.SourceRepo,
					"problems": strings.Join(problems, "; "), "message": s.Message,
				})
			}
			if err := emit(c, opts, result); err != nil {
				return err
			}
			if failed > 0 {
				return fmt.Errorf("source test: %d of %d sources could not be read", failed, len(res.Sources))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}
