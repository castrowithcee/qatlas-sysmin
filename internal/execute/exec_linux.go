package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// DefaultMaxOutput is the number of output bytes a step keeps; the head is kept, the rest is dropped.
const DefaultMaxOutput = 64 << 10

// defaultKillGrace is the time between SIGTERM and SIGKILL for a program that outlives its context.
const defaultKillGrace = 2 * time.Second

// ExecRunner runs programs with os/exec. Each program gets its own process group, and the whole group ends
// when the context does, so a hanging program does not leave children behind.
type ExecRunner struct {
	// MaxOutput caps the captured output; zero means DefaultMaxOutput.
	MaxOutput int
	// KillGrace is the time between SIGTERM and SIGKILL; zero means two seconds.
	KillGrace time.Duration
}

// Run implements Runner.
func (r *ExecRunner) Run(ctx context.Context, c Command) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	limit, grace := r.MaxOutput, r.KillGrace
	if limit <= 0 {
		limit = DefaultMaxOutput
	}
	if grace <= 0 {
		grace = defaultKillGrace
	}

	out := &limitedWriter{limit: limit}
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var mu sync.Mutex
	var killer *time.Timer
	cmd.Cancel = func() error {
		pid := cmd.Process.Pid
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		mu.Lock()
		killer = time.AfterFunc(grace, func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
		mu.Unlock()
		return nil
	}
	// A backstop for children that keep the output pipe open after the group was signalled.
	cmd.WaitDelay = grace + time.Second

	err := cmd.Run()
	if cmd.Process != nil {
		mu.Lock()
		if killer != nil {
			killer.Stop()
		}
		mu.Unlock()
		if ctx.Err() != nil {
			// The leader may have exited on SIGTERM while a child ignored it.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}

	res := Result{Output: out.buf, Omitted: out.omitted}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctxErr := ctx.Err(); ctxErr != nil && err != nil {
		return res, fmt.Errorf("%s: %w", c.Name, ctxErr)
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
		return res, nil
	case errors.As(err, &ee):
		return res, &ExitError{Code: res.ExitCode}
	}
	return res, fmt.Errorf("%s: %w", c.Name, err)
}

// limitedWriter keeps the first limit bytes and counts the rest. It accepts every write so the program is
// never blocked or failed by a full buffer.
type limitedWriter struct {
	mu      sync.Mutex
	limit   int
	buf     []byte
	omitted int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	room := w.limit - len(w.buf)
	if room > len(p) {
		room = len(p)
	}
	if room > 0 {
		w.buf = append(w.buf, p[:room]...)
	}
	w.omitted += int64(len(p) - max(room, 0))
	return len(p), nil
}
