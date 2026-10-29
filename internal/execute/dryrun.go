package execute

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// PlannedStep describes what one step would do.
type PlannedStep struct {
	Index   int
	Type    config.StepType
	Timeout time.Duration
	// Actions are the commands or file changes in order.
	Actions []string
	// Err is set when the step could not be carried out as resolved.
	Err error
}

// DryRun lists what each step of the plan would do. It starts no program and reads or writes no file.
func DryRun(plan config.Plan) []PlannedStep { return dryRun(plan, DefaultBackupRoot) }

func dryRun(plan config.Plan, backupRoot string) []PlannedStep {
	d := describeCtx{project: plan.Project, backupRoot: backupRoot}
	out := make([]PlannedStep, len(plan.Steps))
	for i, st := range plan.Steps {
		p := PlannedStep{Index: i, Type: st.Type, Timeout: st.Timeout}
		if b, ok := blocks[st.Type]; ok {
			p.Actions, p.Err = b.describe(d, st)
		} else {
			p.Err = fmt.Errorf("unknown step type %q", st.Type)
		}
		out[i] = p
	}
	return out
}

// WriteDryRun writes the dry run as text, one block per step, through the executor's redactor.
func (e *Executor) WriteDryRun(w io.Writer, plan config.Plan) error {
	var b strings.Builder
	fmt.Fprintf(&b, "project %s: %d steps (dry run, nothing is executed)\n", plan.Project, len(plan.Steps))
	for _, p := range dryRun(plan, e.backupRoot()) {
		fmt.Fprintf(&b, "step %d/%d %s (timeout %s)\n", p.Index+1, len(plan.Steps), p.Type, p.Timeout)
		for _, a := range p.Actions {
			fmt.Fprintf(&b, "  %s\n", a)
		}
		if p.Err != nil {
			fmt.Fprintf(&b, "  invalid: %v\n", p.Err)
		}
	}
	_, err := io.WriteString(w, e.redact(b.String()))
	return err
}
