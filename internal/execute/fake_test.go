package execute

import (
	"context"
	"sync"
)

// fakeRunner records calls and answers them from handle, or with an empty success.
type fakeRunner struct {
	mu     sync.Mutex
	calls  []Command
	handle func(c Command) (Result, error)
}

func (f *fakeRunner) Run(ctx context.Context, c Command) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	if f.handle == nil {
		return Result{}, nil
	}
	res, err := f.handle(c)
	if err == nil && res.ExitCode != 0 {
		err = &ExitError{Code: res.ExitCode}
	}
	return res, err
}
