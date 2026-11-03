package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cobracli"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
)

// maxSecretInput bounds what apply reads from standard input.
const maxSecretInput = 64 << 10

// changeEnv is everything apply, undo and journal take from the host, so that tests run without root.
type changeEnv struct {
	Registry *hostchange.Registry
	Exec     hostchange.Executor
	// JournalDir and OwnerUID name the journal and the user that must own it.
	JournalDir string
	OwnerUID   int
	Now        func() time.Time
	// SudoUID is the human behind the sudo call, 0 when unknown.
	SudoUID func() int
	// Record hands a finished change to the audit log.
	Record hostchange.Recorder
}

func (o *Options) changeEnv() *changeEnv {
	if o.change != nil {
		return o.change
	}
	return &changeEnv{
		Registry: hostchange.Default(), Exec: hostchange.SystemExecutor{},
		JournalDir: hostchange.DefaultJournalDir, OwnerUID: 0, Now: time.Now,
		SudoUID: sudoUID,
		Record:  recordChange,
	}
}

// sudoUID reads SUDO_UID strictly: plain decimal digits without sign, space or leading zero. Anything else,
// and a missing variable, counts as 0, the narrow reading that root itself made the change; the journal
// and the audit entry then name root rather than a guessed user.
func sudoUID() int {
	text := os.Getenv("SUDO_UID")
	if text == "" || (len(text) > 1 && text[0] == '0') {
		return 0
	}
	uid, err := strconv.ParseUint(text, 10, 31)
	if err != nil {
		return 0
	}
	return int(uid)
}

// recordChange hands a finished change to the service's audit log. Only a final result is recorded; the
// sentence and commands stay in the journal.
func recordChange(a hostchange.AuditEntry) error {
	return withService("", func(c *client.Client) error {
		return c.RecordHostChange(control.HostRecordParams{
			Action: a.Action, Result: a.Result, SudoUID: uint32(a.SudoUID), JournalID: a.ID, UndoOf: a.UndoOf,
		})
	})
}

func (e *changeEnv) runner(j *hostchange.Journal) *hostchange.Runner {
	return &hostchange.Runner{
		Registry: e.Registry, Journal: j, Exec: e.Exec, Now: e.Now, SudoUID: e.SudoUID(), Record: e.Record,
	}
}

func (e *changeEnv) openJournal() (*hostchange.Journal, error) {
	return hostchange.OpenJournal(e.JournalDir, e.OwnerUID)
}

// changeFailed is a change that did not stand, or an error around it.
func changeFailed(err error) error {
	return failure(codeChangeFailed, err,
		"Read the report and the journal entry for what ran, then fix the cause before repeating the change.",
		"The change did not stand. Do not repeat it; tell the user what failed.")
}

func newHostApplyCommand(opts *Options) *cobra.Command {
	var gate *cobracli.Gate
	cmd := &cobra.Command{
		Use:   "apply <action-id> [args...]",
		Short: "Change the host with a registered action (run by the TUI via sudo)",
		Long: "Runs one registered action as root: records the previous state in the journal, runs the commands,\n" +
			"checks the result and restores the previous state when the change or its check fails. A secret input\n" +
			"is read from standard input only. It needs --confirm. There is no question here; the TUI confirms\n" +
			"before it calls sudo.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("an action id is needed")
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			if err := requireRoot(opts.system(), "host apply"); err != nil {
				return err
			}
			return gate.Run(cobracli.Request{Changes: true}, func() error { return applyChange(c, opts, args) })
		},
	}
	gate = opts.prog.AddGate(cmd, cobracli.Gates{Change: true})
	return cmd
}

func applyChange(c *cobra.Command, opts *Options, args []string) error {
	env := opts.changeEnv()
	def, ok := env.Registry.Lookup(args[0])
	if !ok {
		return usageFailure(fmt.Errorf("unknown action %q", args[0]), "Use an action id that 'qsysmin help host-admin' or the TUI offers.")
	}
	var secret []byte
	if def.WantsSecret {
		var err error
		if secret, err = readChangeSecret(c.InOrStdin()); err != nil {
			return err
		}
		opts.Redactor.Add(string(secret))
	}
	journal, err := env.openJournal()
	if err != nil {
		return changeFailed(fmt.Errorf("host apply: %w", err))
	}
	res, err := env.runner(journal).Apply(c.Context(), args[0], args[1:], secret)
	if err != nil {
		if refused, ok := refusedChange(c, opts, args[0], err); ok {
			return refused
		}
		var arg *hostchange.ArgumentError
		if errors.As(err, &arg) {
			return usageFailure(err, "Correct the arguments of the action.")
		}
		return changeFailed(fmt.Errorf("host apply: %w", err))
	}
	return finishChange(c, opts, res)
}

func newHostUndoCommand(opts *Options) *cobra.Command {
	var gate *cobracli.Gate
	cmd := &cobra.Command{
		Use:   "undo <journal-id>",
		Short: "Undo a journaled change of the host (run by the TUI via sudo)",
		Long: "Runs the inverse of a reversible change as root, with the same check and restore as 'host apply', and\n" +
			"records the undo as a new journal entry. It needs --confirm. There is no question here; the TUI\n" +
			"confirms first.",
		Args: exactlyOne("journal id"),
		RunE: func(c *cobra.Command, args []string) error {
			if err := requireRoot(opts.system(), "host undo"); err != nil {
				return err
			}
			return gate.Run(cobracli.Request{Changes: true}, func() error {
				env := opts.changeEnv()
				journal, err := env.openJournal()
				if err != nil {
					return changeFailed(fmt.Errorf("host undo: %w", err))
				}
				res, err := env.runner(journal).Undo(c.Context(), args[0])
				if err != nil {
					if refused, ok := refusedChange(c, opts, "", err); ok {
						return refused
					}
					return changeFailed(fmt.Errorf("host undo: %w", err))
				}
				return finishChange(c, opts, res)
			})
		},
	}
	gate = opts.prog.AddGate(cmd, cobracli.Gates{Change: true})
	return cmd
}

func newHostJournalCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "journal",
		Short: "List the latest changes of the host (run by the TUI via sudo)",
		Long:  "Lists the latest changes, newest first. The journal is readable by root only.",
		RunE: func(c *cobra.Command, _ []string) error {
			if err := requireRoot(opts.system(), "host journal"); err != nil {
				return err
			}
			journal, err := opts.changeEnv().openJournal()
			if err != nil {
				return hostError("host journal", err)
			}
			entries, err := journal.List()
			if err != nil {
				return hostError("host journal", err)
			}
			registry := opts.changeEnv().Registry
			result := output.Collection{Columns: []string{"id", "time", "action", "sentence", "result", "reversible",
				"sudo_uid", "undo_sentence", "undo_commands"}}
			for _, e := range entries {
				reversible := e.Reversible && e.Result == hostchange.ResultApplied && e.UndoneBy == ""
				undoSentence, undoCommands := undoPlan(registry, e, reversible)
				result.Rows = append(result.Rows, output.Row{
					"id": e.ID, "time": e.Time.UTC().Format(time.RFC3339), "action": e.Action, "sentence": e.Sentence,
					"result": e.Result, "reversible": reversible, "sudo_uid": int64(e.SudoUID),
					"undo_sentence": undoSentence, "undo_commands": undoCommands,
				})
			}
			return emit(c, opts, result)
		},
	}
}

// undoPlan is the confirmation text of the undo of a reversible entry: the sentence and the displayed commands of
// its inverse, secrets as placeholders. An inverse that cannot be built leaves both empty, so no undo is offered.
func undoPlan(registry *hostchange.Registry, e hostchange.Entry, reversible bool) (string, []string) {
	def, ok := registry.Lookup(e.Action)
	if !reversible || !ok || def.Inverse == nil {
		return "", []string{}
	}
	plan, err := def.Inverse(e.Previous)
	if err != nil {
		return "", []string{}
	}
	commands := make([]string, len(plan.Commands))
	for i, c := range plan.Commands {
		commands[i] = c.Display()
	}
	return plan.Sentence, commands
}

// refusedChange reports a change that a rule refused before anything ran: the report object with the result
// "refused" and the reason, then an error for the exit code. No journal entry exists for it.
func refusedChange(c *cobra.Command, opts *Options, action string, err error) (error, bool) {
	var refusal *hostchange.RefusedError
	if !errors.As(err, &refusal) {
		return nil, false
	}
	if emitErr := emit(c, opts, output.Object{Fields: []output.Field{
		{Name: "journal_id", Value: ""},
		{Name: "action", Value: action},
		{Name: "sentence", Value: ""},
		{Name: "commands", Value: []string{}},
		{Name: "result", Value: hostchange.ResultRefused},
		{Name: "check", Value: ""},
		{Name: "check_error", Value: ""},
		{Name: "restored", Value: false},
		{Name: "reversible", Value: false},
		{Name: "error", Value: refusal.Msg},
		{Name: "warning", Value: ""},
	}}); emitErr != nil {
		return emitErr, true
	}
	return failure(codeChangeRefused, fmt.Errorf("refused: %s", refusal.Msg),
		"Nothing was changed. Choose another target or action, or fix what the reason names.",
		"Nothing ran. Do not try to work around the rule; tell the user the reason."), true
}

// finishChange prints the report. A change that did not stand is an error after the report, so that the
// exit code tells the caller and the report still arrives on standard output.
func finishChange(c *cobra.Command, opts *Options, res hostchange.Result) error {
	e := res.Entry
	if err := emit(c, opts, output.Object{Fields: []output.Field{
		{Name: "journal_id", Value: e.ID},
		{Name: "action", Value: e.Action},
		{Name: "sentence", Value: e.Sentence},
		{Name: "commands", Value: e.Commands},
		{Name: "result", Value: e.Result},
		{Name: "check", Value: e.Check},
		{Name: "check_error", Value: e.CheckError},
		{Name: "restored", Value: e.Restored},
		{Name: "reversible", Value: e.Reversible},
		{Name: "error", Value: e.Error},
		{Name: "warning", Value: res.Warning},
	}}); err != nil {
		return err
	}
	switch e.Result {
	case hostchange.ResultApplied:
		return nil
	case hostchange.ResultRestored:
		return changeFailed(fmt.Errorf("the change did not succeed and the previous state is back: %s", e.Error))
	}
	return changeFailed(fmt.Errorf("the change did not succeed: %s", e.Error))
}

func readChangeSecret(in io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(in, maxSecretInput+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSecretInput {
		return nil, usageFailure(errors.New("the secret input on standard input is too long"), "Provide a shorter secret on standard input.")
	}
	data = bytes.TrimSuffix(data, []byte("\n"))
	if len(data) == 0 {
		return nil, usageFailure(errors.New("this action reads its secret from standard input, which is empty"), "Provide the secret on standard input.")
	}
	return data, nil
}
