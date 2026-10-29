package engine

import (
	"context"
	"errors"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

const (
	// refreshEvery bounds how long the planner sleeps, so a new project or interval is picked up soon.
	refreshEvery = time.Minute
	// maxSpread caps the random delay added to every poll, so projects with the same interval do not all
	// poll at the same moment.
	maxSpread = 5 * time.Minute
	// minInterval keeps a misconfigured interval from turning the planner into a busy loop.
	minInterval = time.Minute
)

// Trigger starts a check of the project in the background, as the planner does, and reports whether it
// started. It is dropped, not queued, while a run of the project is active or while Run is not active.
func (e *Engine) Trigger(project string) bool {
	if e.acquire(project) != nil {
		return false
	}
	e.plannerMu.Lock()
	ctx := e.base
	if ctx == nil {
		e.plannerMu.Unlock()
		e.release(project)
		return false
	}
	e.wg.Add(1)
	e.plannerMu.Unlock()
	go func() {
		defer e.wg.Done()
		defer e.release(project)
		_, _ = e.check(ctx, project)
	}()
	return true
}

// Run polls every project of Config.Projects in its interval until ctx ends, and returns after every run
// that it or Trigger started has finished. Each poll waits a random delay of up to a tenth of the interval,
// at most five minutes, also the first one after the start.
func (e *Engine) Run(ctx context.Context) error {
	e.plannerMu.Lock()
	if e.base != nil {
		e.plannerMu.Unlock()
		return errors.New("the planner is already running")
	}
	e.base = ctx
	e.plannerMu.Unlock()
	defer func() {
		e.plannerMu.Lock()
		e.base = nil
		e.plannerMu.Unlock()
		e.wg.Wait()
	}()

	clock := e.cfg.Clock
	next := map[string]time.Time{}
	for {
		now := clock.Now()
		var names []string
		if e.cfg.Projects != nil {
			names = e.cfg.Projects()
		}
		seen := map[string]bool{}
		for _, name := range names {
			if !config.ValidName(name) {
				continue
			}
			seen[name] = true
			if _, ok := next[name]; !ok {
				next[name] = now.Add(e.spread(e.interval(name)))
			}
		}
		for name, due := range next {
			if !seen[name] {
				delete(next, name)
				continue
			}
			if !due.After(now) {
				e.Trigger(name)
				iv := e.interval(name)
				next[name] = now.Add(iv + e.spread(iv))
			}
		}
		wait := refreshEvery
		for _, due := range next {
			wait = min(wait, due.Sub(now))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-clock.After(wait):
		}
	}
}

// interval is the poll interval of the approved project last seen, or the default one, never below the
// minimum the configuration accepts.
func (e *Engine) interval(project string) time.Duration {
	e.mu.Lock()
	ap, ok := e.approved[project]
	e.mu.Unlock()
	d := e.cfg.PollInterval()
	if ok && ap.Project.PollInterval > 0 {
		d = time.Duration(ap.Project.PollInterval)
	}
	if d <= 0 {
		d = config.DefaultPollInterval
	}
	return max(d, minInterval)
}

func (e *Engine) spread(interval time.Duration) time.Duration {
	return e.cfg.Jitter(min(interval/10, maxSpread))
}
