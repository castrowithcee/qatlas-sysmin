package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

// adoptError turns the codes of an adoption into messages that say what to do.
func adoptError(what string, err error) error {
	var ce *control.Error
	if errors.As(err, &ce) {
		switch ce.Code {
		case control.CodeNotFound:
			return fmt.Errorf("%s: the directory does not exist or is not a directory", what)
		case control.CodeDockerUnavailable:
			return fmt.Errorf("%s: the service cannot ask Docker for the running containers; check that Docker runs and that the service may use it", what)
		case control.CodeTimeout:
			return fmt.Errorf("%s: inspecting the stack took too long; nothing was stored", what)
		case control.CodeBadRequest:
			return fmt.Errorf("%s: the service did not accept the request; check the directory and the project name", what)
		case control.CodeExists:
			return fmt.Errorf("%s: the directory or the name is managed by a project already; nothing was stored", what)
		}
	}
	return projectError(what, err)
}

// adoptRows lists the proposal, its problems and the outcome as the rows of one collection, so that every
// format shows all of it.
func adoptRows(res control.AdoptResult, dryRun bool) output.Collection {
	result := output.Collection{Columns: []string{"type", "name", "ref", "services", "env_var", "current", "blocking", "code", "message", "hint"}}
	add := func(r output.Row) { result.Rows = append(result.Rows, r) }
	p := res.Project
	add(output.Row{"type": "project", "name": p.Name, "ref": p.Dir, "message": adoptDetails(p)})
	for _, im := range p.Images {
		add(output.Row{
			"type": "image", "name": im.Name, "ref": im.Ref, "services": strings.Join(im.Services, ","),
			"env_var": im.EnvVar, "current": im.Current,
		})
	}
	if g := p.Git; g != nil {
		add(output.Row{"type": "git", "name": "git", "ref": g.URL, "current": g.Current})
	}
	for _, pr := range res.Problems {
		add(output.Row{"type": "problem", "blocking": pr.Blocking, "code": pr.Code, "message": pr.Message, "hint": pr.Hint})
	}
	status, message := "dry-run", "dry run; nothing was stored"
	switch {
	case res.Saved && res.State == control.StateApproved:
		status, message = res.State, "stored and approved; the versions found are the current ones"
	case res.Saved:
		status, message = res.State, "stored; not approved yet: a person must approve it in the terminal interface (sysmin tui), and nothing is deployed"
	case !dryRun:
		status, message = "not stored", "a blocking problem keeps the project from being stored"
	}
	add(output.Row{"type": "status", "name": status, "current": res.Revision, "message": message})
	return result
}

func adoptDetails(p control.AdoptProject) string {
	var parts []string
	for _, kv := range [][2]string{
		{"template", p.Template}, {"compose_project", p.ComposeProject},
		{"compose_files", strings.Join(p.ComposeFiles, ",")}, {"env_file", p.EnvFile},
	} {
		if kv[1] != "" {
			parts = append(parts, kv[0]+"="+quote(kv[1]))
		}
	}
	return strings.Join(parts, " ")
}

func quote(v string) string {
	if strings.ContainsAny(v, " \t\"") {
		return fmt.Sprintf("%q", v)
	}
	return v
}

func newProjectAdoptCommand(opts *Options) *cobra.Command {
	var socket, name string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "adopt DIR",
		Short: "Take over a running Docker Compose stack as a project",
		Long: "adopt asks the service to read the Compose stack that runs from DIR and to propose a project for it:\n" +
			"the Compose project name and files, the env file, and the images whose tag is an environment\n" +
			"variable (or, for a git working copy, the checked-out tag), each with the version it runs now. Problems\n" +
			"that keep sysmin from managing the stack come with a hint; a blocking problem keeps the project from\n" +
			"being stored, and the command then exits with an error. Without --dry-run the project is stored in\n" +
			"manual mode and counts as open until a person approves it in the terminal interface; after the approval\n" +
			"the versions found are the current ones. Nothing is deployed, and compose files, env files and tags\n" +
			"are not changed. The vault must be unlocked, and the service must be able to use Docker.",
		Example: "  sysmin project adopt /srv/shop --dry-run\n" +
			"  sysmin project adopt /srv/shop --name shop-prod",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 {
				return newSyntaxError(fmt.Errorf("adopt takes exactly one directory, got %d arguments", len(args)))
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			dir, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("project adopt: cannot resolve the directory: %w", err)
			}
			var res control.AdoptResult
			err = withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.AdoptProject(control.AdoptParams{Dir: dir, Name: name, DryRun: dryRun})
				return err
			})
			if err != nil {
				return finishWith(c, opts, "project adopt", true, err, adoptError)
			}
			if err := emit(c, opts, adoptRows(res, dryRun)); err != nil {
				return err
			}
			if !dryRun && !res.Saved {
				return errors.New("project adopt: the project was not stored because of blocking problems")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "project name (default: the Compose project name)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "only show the proposal and its problems; store nothing")
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}
