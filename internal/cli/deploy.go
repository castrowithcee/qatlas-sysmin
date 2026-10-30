package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

func newDeployCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Show and steer the deploys of a project",
		Long: "These commands read the deploy state of a project and hold it back or let it go on. None of them needs\n" +
			"an admin session, and none deploys a version that the version rules of the approved project do not\n" +
			"allow without a human. Updating to a version a person chooses and restoring a backup are done in the\n" +
			"terminal interface (sysmin tui), which asks for the vault passphrase.",
		Args: noArgs,
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newDeployStatusCommand(opts), newDeployHistoryCommand(opts), newDeployRunNowCommand(opts))
	for _, a := range []struct {
		name, short, long, op, done string
	}{
		{"pause", "Stop automatic and manual deploys of a project",
			"pause keeps the project from deploying until it is resumed. Restoring a backup stays possible.",
			control.OpDeployPause, "paused"},
		{"resume", "Let a paused project deploy again and clear its error state",
			"resume lifts a pause, including the one a failed rollback caused, and clears the error state of the\nproject. Check the project and the run log before you resume after a failed rollback.",
			control.OpDeployResume, "resumed"},
		{"pin", "Hold a project on the versions it runs now",
			"pin keeps the project on its current versions: no check deploys anything until it is unpinned.",
			control.OpDeployPin, "pinned"},
		{"unpin", "Release a pinned project",
			"unpin lets the version rules of the project apply again at the next check.",
			control.OpDeployUnpin, "unpinned"},
	} {
		cmd.AddCommand(newDeployControlCommand(opts, a.name, a.short, a.long, a.op, a.done))
	}
	return cmd
}

// deployError turns the codes of a deploy request into messages that say what to do.
func deployError(what string, err error) error {
	var ce *control.Error
	if errors.As(err, &ce) {
		switch ce.Code {
		case control.CodeLocked:
			return fmt.Errorf("%s: the vault is locked; unlock it in the terminal interface first", what)
		case control.CodeNotFound:
			return fmt.Errorf("%s: the project does not exist", what)
		case control.CodeNotApproved:
			return fmt.Errorf("%s: the project has no approved state; a person must approve it in the terminal interface (sysmin tui) first", what)
		case control.CodeBusy:
			return fmt.Errorf("%s: a run of the project is active; try again when it has finished", what)
		case control.CodePaused:
			return fmt.Errorf("%s: the project is paused; resume it first", what)
		case control.CodePinned:
			return fmt.Errorf("%s: the project is pinned; unpin it first", what)
		case control.CodeBadRequest:
			return fmt.Errorf("%s: the service did not accept the request; check the project name", what)
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

func timeText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func changesText(changes []control.DeployChange) string {
	var parts []string
	for _, c := range changes {
		name := c.Name
		if name == "" {
			name = "git"
		}
		parts = append(parts, fmt.Sprintf("%s %s -> %s", name, c.From, c.To))
	}
	return strings.Join(parts, "; ")
}

func newDeployStatusCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "status PROJECT",
		Short: "Show the deploy state of a project",
		Long: "status shows whether a run is active, whether the project is paused or pinned, the error state after a\n" +
			"failed rollback, the versions of every source with the newer ones that wait for a person, and the\n" +
			"latest run. Nothing is changed, and the vault may be locked.",
		Example: "  sysmin deploy status demo-app --output json",
		Args:    oneName("status"),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.DeployStatusResult
			err := withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.DeployStatus(args[0])
				return err
			})
			if err != nil {
				return deployError("deploy status", err)
			}
			var sources []string
			for _, s := range res.Sources {
				name := s.Name
				if name == "" {
					name = "git"
				}
				text := name + " " + orDash(s.Current)
				if len(s.Available) > 0 {
					text += " (newer: " + strings.Join(s.Available, ",") + ")"
				}
				if s.Failed != "" {
					text += " (rolled back: " + s.Failed + ")"
				}
				if s.Error != "" {
					text += " (error: " + s.Error + ")"
				}
				sources = append(sources, text)
			}
			fault, lastRun := "", ""
			if res.Fault != nil {
				fault = res.Fault.Error
				if fault == "" {
					fault = "the rollback failed"
				}
			}
			if r := res.LastRun; r != nil {
				lastRun = fmt.Sprintf("%s %s %s", timeText(r.Started), r.Action, r.Result)
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "project", Value: res.Project}, {Name: "running", Value: res.Running},
				{Name: "paused", Value: res.Paused}, {Name: "pinned", Value: res.Pinned},
				{Name: "locked", Value: res.Locked}, {Name: "fault", Value: fault},
				{Name: "last_check", Value: timeText(res.LastCheck)}, {Name: "check_error", Value: res.CheckError},
				{Name: "sources", Value: strings.Join(sources, "; ")}, {Name: "last_run", Value: lastRun},
			}})
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func newDeployHistoryCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "history PROJECT",
		Short: "List the latest runs of a project",
		Long: "history lists the latest runs of a project, the oldest first: automatic deploys, updates a person\n" +
			"started and restores, with their result and the version changes. Error texts have secret values\n" +
			"removed. Nothing is changed, and the vault may be locked.",
		Example: "  sysmin deploy history demo-app --output json",
		Args:    oneName("history"),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.DeployHistoryResult
			err := withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.DeployHistory(args[0])
				return err
			})
			if err != nil {
				return deployError("deploy history", err)
			}
			result := output.Collection{Columns: []string{"id", "action", "started", "duration_ms", "result", "changes", "backup", "error_code", "failed_step", "error"}}
			for _, r := range res.Runs {
				result.Rows = append(result.Rows, output.Row{
					"id": r.ID, "action": r.Action, "started": timeText(r.Started), "duration_ms": r.DurationMS,
					"result": r.Result, "changes": changesText(r.Changes), "backup": r.Backup,
					"error_code": r.ErrorCode, "failed_step": r.FailedStep, "error": r.Error,
				})
			}
			return emit(c, opts, result)
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newDeployRunNowCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "run-now PROJECT",
		Short: "Check the sources of a project now and deploy what its rules allow",
		Long: "run-now reads the version sources of the project at once and deploys the versions that its version\n" +
			"rules allow without a human, exactly as the periodic check does. A version above the update cap or\n" +
			"under the manual mode is only reported. The vault must be unlocked for a deploy. The command waits\n" +
			"up to 20 seconds; a longer run is not stopped, the command then says it is still running, and\n" +
			"'sysmin deploy status' and 'sysmin deploy history' show the end. It exits with an error when the run\n" +
			"failed or was rolled back.",
		Example: "  sysmin deploy run-now demo-app",
		Args:    oneName("run-now"),
		RunE: func(c *cobra.Command, args []string) error {
			var res control.DeployRunNowResult
			err := withService(socket, func(cl *client.Client) (err error) {
				res, err = cl.DeployRunNow(args[0])
				return err
			})
			if err != nil {
				return deployError("deploy run-now", err)
			}
			status := "nothing-to-deploy"
			switch {
			case res.Pending:
				status = "pending"
			case res.Skipped != "":
				status = "skipped"
			case res.Run != nil:
				status = "deployed"
			}
			fields := []output.Field{{Name: "project", Value: res.Project}, {Name: "status", Value: status},
				{Name: "skipped", Value: res.Skipped}}
			var failure error
			if r := res.Run; r != nil {
				fields = append(fields, output.Field{Name: "run", Value: r.ID}, output.Field{Name: "result", Value: r.Result},
					output.Field{Name: "changes", Value: changesText(r.Changes)}, output.Field{Name: "backup", Value: r.Backup},
					output.Field{Name: "failed_step", Value: r.FailedStep}, output.Field{Name: "error_code", Value: r.ErrorCode})
				if r.Result != "succeeded" {
					failure = fmt.Errorf("deploy run-now: the run ended with %s; see 'sysmin deploy history %s'", r.Result, res.Project)
				}
			}
			if err := emit(c, opts, output.Object{Fields: fields}); err != nil {
				return err
			}
			return failure
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

func newDeployControlCommand(opts *Options, name, short, long, op, done string) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:     name + " PROJECT",
		Short:   short,
		Long:    long + " No admin session is needed, and the vault may be locked.",
		Example: "  sysmin deploy " + name + " demo-app",
		Args:    oneName(name),
		RunE: func(c *cobra.Command, args []string) error {
			err := withService(socket, func(cl *client.Client) error { return cl.DeployControl(op, args[0]) })
			if err != nil {
				return deployError("deploy "+name, err)
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "project", Value: args[0]}, {Name: "state", Value: done},
			}})
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}
