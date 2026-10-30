package engine

import (
	"context"
	"errors"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

const (
	// refreshEvery bounds how long the planner sleeps, so a new project or a changed approval is picked up
	// within that time.
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
	plan := map[string]*pollPlan{}
	for {
		now := clock.Now()
		var names []string
		if e.cfg.Projects != nil {
			names = e.cfg.Projects()
		}
		seen := map[string]bool{}
		wait := refreshEvery
		for _, name := range names {
			if !config.ValidName(name) || seen[name] {
				continue
			}
			seen[name] = true
			iv, ok := e.interval(name)
			if !ok {
				// Nothing verified to plan from: no poll, and the plan is kept for later.
				continue
			}
			p := plan[name]
			if p == nil {
				p = &pollPlan{anchor: now, spread: e.spread(iv)}
				plan[name] = p
			}
			due := p.anchor.Add(p.spread)
			if p.polled {
				due = p.anchor.Add(iv + p.spread)
			}
			if !due.After(now) {
				e.Trigger(name)
				// The spread is rolled once per poll, so a later round does not move the due time again.
				p.anchor, p.polled, p.spread = now, true, e.spread(iv)
				due = now.Add(iv + p.spread)
			}
			wait = min(wait, due.Sub(now))
		}
		for name := range plan {
			if !seen[name] {
				delete(plan, name)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-e.wake:
		case <-clock.After(wait):
		}
	}
}

// pollPlan is when a project was last polled and the delay rolled for its next poll. Before the first poll,
// anchor is when the planner first had a verified state of the project.
type pollPlan struct {
	anchor time.Time
	polled bool
	spread time.Duration
}

// Wake makes a running planner look at its projects again at once, for example after an unlock. It never
// blocks and does nothing without a planner. A project is still polled only when its interval has elapsed.
func (e *Engine) Wake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// interval is the poll interval of the approved, verified state of the project, never below the minimum
// the configuration accepts. ok is false while there is no verified state, for example while the vault is
// locked; the remembered state of an earlier check is not used for that.
func (e *Engine) interval(project string) (d time.Duration, ok bool) {
	ap, fresh, err := e.approvedState(project)
	if err != nil || !fresh {
		return 0, false
	}
	d = e.cfg.PollInterval()
	if ap.Project.PollInterval > 0 {
		d = time.Duration(ap.Project.PollInterval)
	}
	if d <= 0 {
		d = config.DefaultPollInterval
	}
	return max(d, minInterval), true
}

func (e *Engine) spread(interval time.Duration) time.Duration {
	return e.cfg.Jitter(min(interval/10, maxSpread))
}
