package execute

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
)

// Step statuses.
const (
	StatusOK      = "ok"
	StatusFailed  = "failed"
	StatusSkipped = "skipped" // not run because an earlier step failed
)

// Executor runs the steps of a plan. Runner is required.
type Executor struct {
	Runner Runner
	// Log receives the run log; nil discards it.
	Log io.Writer
	// Redactor, when set, removes secrets from the log, the results and error texts.
	Redactor *redact.Redactor
	// GitEnv holds extra KEY=value entries for every git call. This is the extension point for credentials.
	GitEnv []string
	// MaxOutput caps the output kept per step; zero means DefaultMaxOutput.
	MaxOutput int
	// Now supplies the log time stamps; nil means time.Now.
	Now func() time.Time
}

// EnvChange is the value a set-env step replaced. WasSet tells an unset variable from an empty one.
type EnvChange struct {
	Key      string
	Previous string
	WasSet   bool
}

// StepResult is the outcome of one step.
type StepResult struct {
	Index    int
	Type     config.StepType
	Status   string
	Duration time.Duration
	// Output is the capped output of the step, with a visible marker where it was cut.
	Output string
	// Error is the failure cause; empty unless Status is StatusFailed.
	Error string
	// Previous lists the values a set-env step replaced, for a later rollback.
	Previous []EnvChange
}

// Report is the outcome of a run: one entry per planned step.
type Report struct {
	Project string
	Steps   []StepResult
}

// StepError reports the step that stopped a run.
type StepError struct {
	Index int // zero-based
	Type  config.StepType
	Err   error
}

func (e *StepError) Error() string {
	return fmt.Sprintf("step %d (%s) failed: %v", e.Index+1, e.Type, e.Err)
}

func (e *StepError) Unwrap() error { return e.Err }

// redactedError shows the redacted text and still unwraps to the cause.
type redactedError struct {
	msg string
	err error
}

func (e redactedError) Error() string { return e.msg }
func (e redactedError) Unwrap() error { return e.err }

// block is one building block.
type block struct {
	run      func(ctx context.Context, s *stepRun, st config.ResolvedStep) error
	describe func(st config.ResolvedStep) ([]string, error)
}

var errNotImplemented = errors.New("not implemented")

func unimplemented(t config.StepType) block {
	return block{
		run: func(context.Context, *stepRun, config.ResolvedStep) error {
			return fmt.Errorf("step type %s is %w", t, errNotImplemented)
		},
		describe: func(config.ResolvedStep) ([]string, error) {
			return []string{string(t) + " (not implemented)"}, nil
		},
	}
}

// blocks maps every step type to its implementation.
var blocks = map[config.StepType]block{
	config.StepSetEnv:         {run: runSetEnv, describe: describeSetEnv},
	config.StepComposePull:    {run: runCompose("pull"), describe: describeCompose("pull")},
	config.StepComposeUp:      {run: runCompose("up", "-d"), describe: describeCompose("up", "-d")},
	config.StepGitCheckout:    {run: runGitCheckout, describe: describeGitCheckout},
	config.StepCommand:        {run: runCommand, describe: describeCommand},
	config.StepBackup:         unimplemented(config.StepBackup),
	config.StepCheckContainer: unimplemented(config.StepCheckContainer),
	config.StepCheckHTTP:      unimplemented(config.StepCheckHTTP),
}

// Execute runs the steps in order and stops at the first failure. The report lists every step, those after
// a failure as skipped. The error is a *StepError for a failed step.
func (e *Executor) Execute(ctx context.Context, plan config.Plan) (Report, error) {
	rep := Report{Project: plan.Project, Steps: make([]StepResult, len(plan.Steps))}
	for i, st := range plan.Steps {
		rep.Steps[i] = StepResult{Index: i, Type: st.Type, Status: StatusSkipped}
	}
	if e.Runner == nil {
		return rep, errors.New("executor has no runner")
	}
	for i, st := range plan.Steps {
		res, err := e.runStep(ctx, plan, i, st)
		rep.Steps[i] = res
		if err != nil {
			return rep, &StepError{Index: i, Type: st.Type, Err: redactedError{msg: e.redactErr(err), err: err}}
		}
	}
	return rep, nil
}

func (e *Executor) runStep(ctx context.Context, plan config.Plan, i int, st config.ResolvedStep) (StepResult, error) {
	res := StepResult{Index: i, Type: st.Type, Status: StatusFailed}
	s := &stepRun{e: e, label: fmt.Sprintf("project=%s step=%d/%d type=%s", plan.Project, i+1, len(plan.Steps), st.Type)}
	s.out.limit = e.MaxOutput
	if s.out.limit <= 0 {
		s.out.limit = DefaultMaxOutput
	}
	start := time.Now()

	var err error
	b, known := blocks[st.Type]
	switch {
	case !known:
		err = fmt.Errorf("unknown step type %q", st.Type)
	case st.Timeout <= 0:
		err = errors.New("step has no time limit")
	default:
		s.logf("start timeout=%s", st.Timeout)
		sctx, cancel := context.WithTimeout(ctx, st.Timeout)
		err = b.run(sctx, s, st)
		if err != nil && sctx.Err() != nil {
			if errors.Is(sctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
				err = fmt.Errorf("timed out after %s: %w", st.Timeout, err)
			} else {
				err = fmt.Errorf("canceled: %w", err)
			}
		}
		cancel()
	}

	res.Duration = time.Since(start)
	res.Previous = s.prev
	if s.out.omitted > 0 {
		s.logf("%s", s.out.marker())
	}
	res.Output = e.redact(s.out.String())
	if err != nil {
		res.Error = e.redactErr(err)
		s.logf("result=failed duration=%s error=%s", res.Duration.Round(time.Millisecond), res.Error)
		return res, err
	}
	res.Status = StatusOK
	s.logf("result=ok duration=%s", res.Duration.Round(time.Millisecond))
	return res, nil
}

func (e *Executor) redact(s string) string {
	if e.Redactor == nil {
		return s
	}
	return e.Redactor.Apply(s)
}

func (e *Executor) redactErr(err error) string {
	return e.redact(err.Error())
}

func (e *Executor) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// stepRun is the view of the executor one step gets: it runs commands and logs them.
type stepRun struct {
	e     *Executor
	label string
	out   capBuf
	prev  []EnvChange
}

// logf writes one log line: time stamp, project, step, message.
func (s *stepRun) logf(format string, args ...any) {
	if s.e.Log == nil {
		return
	}
	line := fmt.Sprintf("%s %s %s\n", s.e.now().UTC().Format(time.RFC3339), s.label, fmt.Sprintf(format, args...))
	_, _ = io.WriteString(s.e.Log, s.e.redact(line))
}

// run starts one command through the runner, logs it and collects its capped output.
func (s *stepRun) run(ctx context.Context, c Command) (Result, error) {
	s.logf("run %s", displayCommand(c))
	start := time.Now()
	res, err := s.e.Runner.Run(ctx, c)
	if shown := s.out.add(res.Output, res.Omitted); len(shown) > 0 {
		s.logf("output:\n%s", indent(string(shown)))
	}
	var ee *ExitError
	code := res.ExitCode
	if errors.As(err, &ee) {
		code = ee.Code
	}
	s.logf("exit=%d duration=%s", code, time.Since(start).Round(time.Millisecond))
	if err != nil {
		return res, fmt.Errorf("%s: %w", c.Name, err)
	}
	return res, nil
}

// note adds a line the step itself produced to its output.
func (s *stepRun) note(format string, args ...any) {
	text := fmt.Sprintf(format, args...) + "\n"
	if shown := s.out.add([]byte(text), 0); len(shown) > 0 {
		s.logf("note: %s", strings.TrimSuffix(text, "\n"))
	}
}

func indent(s string) string {
	s = strings.TrimSuffix(s, "\n")
	return "  | " + strings.ReplaceAll(s, "\n", "\n  | ")
}

// capBuf keeps the head of what is added to it and counts the rest.
type capBuf struct {
	limit   int
	buf     []byte
	omitted int64
}

// add appends p up to the limit, counts the bytes it had to drop plus those the caller dropped already, and
// returns the part that was kept.
func (c *capBuf) add(p []byte, alreadyOmitted int64) []byte {
	c.omitted += alreadyOmitted
	room := c.limit - len(c.buf)
	if room >= len(p) {
		c.buf = append(c.buf, p...)
		return p
	}
	cut := max(room, 0)
	// Do not split a UTF-8 sequence at the cut.
	for cut > 0 && cut < len(p) && !utf8.RuneStart(p[cut]) {
		cut--
	}
	c.buf = append(c.buf, p[:cut]...)
	c.omitted += int64(len(p) - cut)
	return p[:cut]
}

func (c *capBuf) marker() string {
	return fmt.Sprintf("[output truncated: %d bytes omitted]", c.omitted)
}

func (c *capBuf) String() string {
	if c.omitted == 0 {
		return string(c.buf)
	}
	s := string(c.buf)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s + c.marker() + "\n"
}
