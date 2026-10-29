package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

// maxProjectFile keeps a project file clear of the request size limit, which also carries the other fields.
const maxProjectFile = control.MaxRequestBytes - 8<<10

func newProjectCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Add, change, show, check and remove projects",
		Long: "A project is one file that describes what to update and how. These commands change it through the\n" +
			"service and never ask for the vault passphrase: a change is stored, but it counts as open until a\n" +
			"person approves it in the terminal interface, and the project keeps running in its approved state\n" +
			"until then. Every answer to a change says whether it is approved or pending approval. The vault\n" +
			"must be unlocked for a change.",
		Args: noVaultArgs,
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newProjectAddCommand(opts), newProjectEditCommand(opts), newProjectListCommand(opts),
		newProjectShowCommand(opts), newProjectRemoveCommand(opts), newProjectCheckCommand(opts), newProjectChangelogCommand(opts),
		newProjectAdoptCommand(opts))
	return cmd
}

func oneName(verb string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return newSyntaxError(fmt.Errorf("%s takes exactly one project name, got %d arguments", verb, len(args)))
		}
		return nil
	}
}

// projectError turns the codes of a project request into messages that say what to do.
func projectError(what string, err error) error {
	var ce *control.Error
	if errors.As(err, &ce) {
		switch ce.Code {
		case control.CodeLocked:
			return fmt.Errorf("%s: the vault is locked; unlock it in the terminal interface first", what)
		case control.CodeVaultAbsent:
			return fmt.Errorf("%s: there is no vault yet", what)
		case control.CodeNotFound:
			return fmt.Errorf("%s: the project does not exist", what)
		case control.CodeExists:
			return fmt.Errorf("%s: the project exists already; use edit to change it", what)
		case control.CodeChanged:
			return fmt.Errorf("%s: the project changed since it was read; nothing was changed, read it again and repeat the command", what)
		case control.CodeBadRequest:
			return fmt.Errorf("%s: the service did not accept the request; check the project name and the combination of options", what)
		case control.CodeTooLarge:
			return fmt.Errorf("%s: the request is too large for the service", what)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

// problemsResult lists the problems of an invalid request, one per row.
func problemsResult(probs []control.Problem) output.Collection {
	result := output.Collection{Columns: []string{"field", "code", "message", "hint"}}
	for _, p := range probs {
		result.Rows = append(result.Rows, output.Row{"field": p.Field, "code": p.Code, "message": p.Message, "hint": p.Hint})
	}
	return result
}

// finish returns nil for a successful call. For invalid input it prints the problem list and returns a
// usage error, which exits with code 2; every other failure is a runtime error.
func finish(c *cobra.Command, opts *Options, what string, changed bool, err error) error {
	return finishWith(c, opts, what, changed, err, projectError)
}

// finishWith is finish with its own wording for the failures that are no list of problems.
func finishWith(c *cobra.Command, opts *Options, what string, changed bool, err error, describe func(string, error) error) error {
	if err == nil {
		return nil
	}
	var ce *control.Error
	if errors.As(err, &ce) && ce.Code == control.CodeInvalid && len(ce.Problems) > 0 {
		if eerr := emit(c, opts, problemsResult(ce.Problems)); eerr != nil {
			return eerr
		}
		msg := fmt.Sprintf("%s: %d problems found", what, len(ce.Problems))
		if len(ce.Problems) == 1 {
			msg = fmt.Sprintf("%s: 1 problem found", what)
		}
		if changed {
			msg += "; nothing was changed"
		}
		return &UsageError{errors.New(msg)}
	}
	return describe(what, err)
}

// changeResult prints the outcome of a change and what it means.
func changeResult(c *cobra.Command, opts *Options, action string, res control.ChangeResult) error {
	message := "stored; not approved yet: a person must approve it in the terminal interface (sysmin tui) before it takes effect"
	switch {
	case res.State == control.StateApproved && action == "removed":
		message = "removed; no approval is open for it"
	case res.State == control.StateApproved:
		message = "stored and approved"
	case action == "removed":
		message = "removed; the removal is not approved yet: a person must approve it in the terminal interface (sysmin tui), and until then the project keeps running as approved"
	}
	return emit(c, opts, output.Object{Fields: []output.Field{
		{Name: "project", Value: res.Project},
		{Name: "action", Value: action},
		{Name: "state", Value: res.State},
		{Name: "revision", Value: res.Revision},
		{Name: "message", Value: message},
	}})
}

// projectFlags are the options that describe a project on add and edit.
type projectFlags struct {
	file, template, dir, envFile, composeProject, pollInterval, gitURL, gitCredential string
	composeFiles, set, unset                                                          []string
	images, imageEnv, imageService, imageCredential, removeImages                     []string
	revision                                                                          string
}

func (f *projectFlags) register(cmd *cobra.Command, edit bool) {
	fl := cmd.Flags()
	fl.StringVar(&f.file, "file", "", "read the complete project from this YAML file (- for standard input); excludes the other options")
	fl.StringVar(&f.template, "template", "", "scenario template (default: image-tag, or git-tag with --git)")
	fl.StringVar(&f.dir, "dir", "", "absolute directory of the project")
	fl.StringVar(&f.envFile, "env-file", "", "absolute path of the environment file the update writes")
	fl.StringArrayVar(&f.composeFiles, "compose-file", nil, "absolute path of a compose file; repeat for several (replaces the list)")
	fl.StringVar(&f.composeProject, "compose-project", "", "compose project name")
	fl.StringVar(&f.pollInterval, "poll-interval", "", "how often to look for new versions, such as 30m or 6h")
	fl.StringArrayVar(&f.set, "set", nil, "template value as key=value; repeat for several")
	fl.StringArrayVar(&f.images, "image", nil, "container image as NAME=REGISTRY/REPOSITORY (no tag); repeat for several")
	fl.StringArrayVar(&f.imageEnv, "image-env", nil, "environment variable that carries the tag of an image, as NAME=VARIABLE")
	fl.StringArrayVar(&f.imageService, "image-service", nil, "compose service that runs an image, as NAME=SERVICE; repeat for several")
	fl.StringArrayVar(&f.imageCredential, "image-credential", nil, "name of the vault credential for an image's registry, as NAME=CREDENTIAL")
	fl.StringVar(&f.gitURL, "git", "", "git repository whose tags are the versions, instead of images")
	fl.StringVar(&f.gitCredential, "git-credential", "", "name of the vault credential for the git repository")
	if edit {
		fl.StringArrayVar(&f.unset, "unset", nil, "remove a template value by key; repeat for several")
		fl.StringArrayVar(&f.removeImages, "remove-image", nil, "remove an image by name; repeat for several")
		fl.StringVar(&f.revision, "revision", "", "only change the project if its stored revision (see show) is this one")
	}
}

// pairs splits key=value options; the first = separates.
func pairs(flag string, in []string) ([][2]string, error) {
	out := make([][2]string, 0, len(in))
	for _, s := range in {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return nil, newSyntaxError(fmt.Errorf("--%s wants NAME=VALUE, got %q", flag, s))
		}
		out = append(out, [2]string{k, v})
	}
	return out, nil
}

// params builds the request for project name. A file named "-" is read from in.
func (f *projectFlags) params(name string, in io.Reader) (control.ProjectParams, error) {
	p := control.ProjectParams{
		Name: name, Revision: f.revision, Template: f.template, Dir: f.dir, EnvFile: f.envFile,
		ComposeFiles: f.composeFiles, ComposeProject: f.composeProject, PollInterval: f.pollInterval,
		UnsetValues: f.unset, RemoveImages: f.removeImages, GitURL: f.gitURL, GitCredential: f.gitCredential,
	}
	if f.file != "" {
		if p.HasFields() {
			return p, newSyntaxError(errors.New("--file excludes the options that describe the project"))
		}
		var data []byte
		var err error
		if f.file == "-" {
			data, err = io.ReadAll(io.LimitReader(in, maxProjectFile+1))
		} else {
			data, err = os.ReadFile(f.file)
		}
		if err != nil {
			return p, fmt.Errorf("cannot read the project file: %w", err)
		}
		if len(data) > maxProjectFile {
			return p, &UsageError{errors.New("the project file is too large")}
		}
		p.File = string(data)
		return p, nil
	}
	sets, err := pairs("set", f.set)
	if err != nil {
		return p, err
	}
	if len(sets) > 0 {
		p.Values = map[string]string{}
		for _, kv := range sets {
			p.Values[kv[0]] = kv[1]
		}
	}
	byName := map[string]*control.ProjectImage{}
	var order []string
	image := func(name string) *control.ProjectImage {
		if byName[name] == nil {
			byName[name] = &control.ProjectImage{Name: name}
			order = append(order, name)
		}
		return byName[name]
	}
	for _, o := range []struct {
		flag string
		in   []string
		set  func(*control.ProjectImage, string)
	}{
		{"image", f.images, func(im *control.ProjectImage, v string) { im.Ref = v }},
		{"image-env", f.imageEnv, func(im *control.ProjectImage, v string) { im.EnvVar = v }},
		{"image-service", f.imageService, func(im *control.ProjectImage, v string) { im.Services = append(im.Services, v) }},
		{"image-credential", f.imageCredential, func(im *control.ProjectImage, v string) { im.Credential = v }},
	} {
		kvs, err := pairs(o.flag, o.in)
		if err != nil {
			return p, err
		}
		for _, kv := range kvs {
			o.set(image(kv[0]), kv[1])
		}
	}
	for _, n := range order {
		p.Images = append(p.Images, *byName[n])
	}
	return p, nil
}

func newProjectAddCommand(opts *Options) *cobra.Command {
	var socket string
	var f projectFlags
	cmd := &cobra.Command{
		Use:   "add NAME",
		Short: "Add a project from a scenario template",
		Long: "add stores a new project that starts from a scenario template (image-tag unless --git is given) and\n" +
			"the values you pass, or from a complete project file with --file. The result is validated first; a\n" +
			"project with problems is not stored. The new project is pending approval until a person approves\n" +
			"it in the terminal interface.",
		Example: "  sysmin project add demo-app --dir /srv/demo/app --env-file /srv/demo/app/.env \\\n" +
			"      --image app=registry.example.com/demo/app --image-env app=DEMO_APP_TAG --image-service app=app",
		Args: oneName("add"),
		RunE: func(c *cobra.Command, args []string) error {
			p, err := f.params(args[0], c.InOrStdin())
			if err != nil {
				return err
			}
			var res control.ChangeResult
			err = withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.AddProject(p)
				return err
			})
			if err != nil {
				return finish(c, opts, "project add", true, err)
			}
			return changeResult(c, opts, "added", res)
		},
	}
	f.register(cmd, false)
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newProjectEditCommand(opts *Options) *cobra.Command {
	var socket string
	var f projectFlags
	cmd := &cobra.Command{
		Use:   "edit NAME",
		Short: "Change a project",
		Long: "edit applies the options you pass to the stored project and leaves everything else as it is; --image\n" +
			"and the --image-* options change the image of that name or add it. With --file the complete project\n" +
			"is replaced. The result is validated first; a project with problems is not stored. The change is\n" +
			"pending approval until a person approves it in the terminal interface.",
		Args: oneName("edit"),
		RunE: func(c *cobra.Command, args []string) error {
			p, err := f.params(args[0], c.InOrStdin())
			if err != nil {
				return err
			}
			var res control.ChangeResult
			err = withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.EditProject(p)
				return err
			})
			if err != nil {
				return finish(c, opts, "project edit", true, err)
			}
			return changeResult(c, opts, "changed", res)
		},
	}
	f.register(cmd, true)
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newProjectRemoveCommand(opts *Options) *cobra.Command {
	var socket, revision string
	cmd := &cobra.Command{
		Use:   "remove NAME",
		Short: "Remove a project",
		Long: "remove deletes the project file. Until a person approves the removal in the terminal interface,\n" +
			"the project keeps running in its approved state.",
		Args: oneName("remove"),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.ChangeResult
			err := withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.RemoveProject(args[0], revision)
				return err
			})
			if err != nil {
				return finish(c, opts, "project remove", true, err)
			}
			return changeResult(c, opts, "removed", res)
		},
	}
	cmd.Flags().StringVar(&revision, "revision", "", "only remove the project if its stored revision (see show) is this one")
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newProjectListCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the stored projects",
		Long: "list shows the stored project files. The state is approved when the file is the state a person\n" +
			"approved, pending approval when it differs, and unknown while the vault is locked.",
		Args: noVaultArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var list []control.ProjectSummary
			err := withService(socket, func(cl *client.Client) (err error) {
				list, err = cl.Projects()
				return err
			})
			if err != nil {
				return projectError("project list", err)
			}
			result := output.Collection{Columns: []string{"name", "template", "source", "dir", "state"}}
			for _, p := range list {
				result.Rows = append(result.Rows, output.Row{
					"name": p.Name, "template": p.Template, "source": p.Source, "dir": p.Dir, "state": p.State,
				})
			}
			return emit(c, opts, result)
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newProjectShowCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "show NAME",
		Short: "Show the stored file of a project",
		Long: "show prints the project file as stored, which may differ from the approved state: the state says\n" +
			"whether it is approved or pending approval ('sysmin approvals show NAME' lists the differences). The\n" +
			"revision identifies the stored file for the --revision option of edit and remove.",
		Args: oneName("show"),
		RunE: func(c *cobra.Command, args []string) error {
			var d control.ProjectDetail
			err := withService(socket, func(cl *client.Client) (err error) {
				d, err = cl.Project(args[0])
				return err
			})
			if err != nil {
				return projectError("project show", err)
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "name", Value: d.Name},
				{Name: "state", Value: d.State},
				{Name: "revision", Value: d.Revision},
				{Name: "template", Value: d.Template},
				{Name: "source", Value: d.Source},
				{Name: "dir", Value: d.Dir},
				{Name: "file", Value: d.File},
			}})
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newProjectCheckCommand(opts *Options) *cobra.Command {
	var socket, version string
	cmd := &cobra.Command{
		Use:   "check NAME",
		Short: "Validate a project and show the steps an update would run",
		Long: "check validates the stored project and resolves its steps for an example version, 1.0.0 unless\n" +
			"--version names another. It runs nothing. Free command lines, which no typed building block\n" +
			"constrains, are marked in the free column. The vault must be unlocked, since credential names are\n" +
			"checked against it.",
		Args: oneName("check"),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.CheckResult
			err := withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.CheckProject(args[0], version)
				return err
			})
			if err != nil {
				return finish(c, opts, "project check", false, err)
			}
			result := output.Collection{Columns: []string{"step", "type", "timeout", "free", "detail"}}
			for i, st := range res.Steps {
				result.Rows = append(result.Rows, output.Row{
					"step": int64(i + 1), "type": st.Type, "timeout": st.Timeout, "free": st.Free, "detail": st.Detail,
				})
			}
			return emit(c, opts, result)
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "example version to resolve the steps with (default 1.0.0)")
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}
