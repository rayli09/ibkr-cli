package cpapi

import (
	"context"
	"sync"
	"time"
)

// Per-route rate limits from IBKR's Web API documentation (SPEC §2):
// minimum intervals between calls, plus a concurrency cap for
// history. The limiter is deliberately simple — one mutex, last-call
// timestamps, and a semaphore — because the CLI is one-shot and
// mostly sequential.
var routeLimits = map[string]time.Duration{
	"snapshot":      100 * time.Millisecond, // 10 req/s
	"orders":        5 * time.Second,        // 1 req / 5 s
	"trades":        5 * time.Second,        // 1 req / 5 s
	"pnl":           5 * time.Second,        // 1 req / 5 s
	"portfolio":     5 * time.Second,        // 1 req / 5 s
	"tickle":        time.Second,            // 1 req / s
	"validate":      time.Minute,            // 1 req / min
	"scannerrun":    time.Second,            // 1 req / s
	"scannerparams": 15 * time.Minute,       // 1 req / 15 min
	"pa":            15 * time.Minute,       // 1 req / 15 min (/pa/performance, /pa/allperiods, /pa/transactions)
	"default":       0,
}

const historyConcurrency = 5

type rateLimiter struct {
	mu      sync.Mutex
	last    map[string]time.Time
	histSem chan struct{}
	now     func() time.Time
	sleep   func(time.Duration)
	limits  map[string]time.Duration
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		last:    map[string]time.Time{},
		histSem: make(chan struct{}, historyConcurrency),
		now:     time.Now,
		sleep:   time.Sleep,
		limits:  routeLimits,
	}
}

// acquire blocks until the route may be called again and returns the
// release function. For history the release also frees one of the
// concurrency slots, so the caller must hold it for the request's
// duration.
func (r *rateLimiter) acquire(ctx context.Context, route string) (func(), error) {
	release := func() {}
	if route == "history" {
		select {
		case r.histSem <- struct{}{}:
			release = func() { <-r.histSem }
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	min := r.limits[route]
	if min <= 0 {
		return release, nil
	}
	r.mu.Lock()
	last := r.last[route]
	now := r.now()
	if !last.IsZero() && now.Sub(last) < min {
		d := min - now.Sub(last)
		r.last[route] = last.Add(min)
		r.mu.Unlock()
		if err := sleepCtx(ctx, r.sleep, d); err != nil {
			release()
			return nil, err
		}
		return release, nil
	}
	r.last[route] = now
	r.mu.Unlock()
	return release, nil
}

func sleepCtx(ctx context.Context, sleep func(time.Duration), d time.Duration) error {
	done := make(chan struct{})
	go func() {
		sleep(d)
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
