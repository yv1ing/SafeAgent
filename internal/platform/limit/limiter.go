// Package limit provides request-rate and concurrency limits keyed by scope. A limiter is
// process-local; the gateway applies one scope to a project and one to an agent key.
package limit

import (
	"errors"
	"sync"
	"time"
)

var ErrExceeded = errors.New("rate or concurrency limit exceeded")

// Limit is a per-minute request ceiling and a concurrency ceiling; zero disables either.
type Limit struct {
	RequestsPerMinute int
	Concurrency       int
}

// Scoped binds a limit to the identity it counts against, e.g. "project:<id>" or "key:<id>".
type Scoped struct {
	Scope string
	Limit
}

type window struct {
	started  time.Time
	requests int
	active   int
}

type Limiter struct {
	mu      sync.Mutex
	windows map[string]*window
	clock   func() time.Time
}

func New(clock func() time.Time) *Limiter {
	if clock == nil {
		clock = time.Now
	}
	return &Limiter{windows: map[string]*window{}, clock: clock}
}

// Acquire admits one request against every scope or none of them. The returned release must be
// called when the request finishes, not when it is admitted.
func (limiter *Limiter) Acquire(limits ...Scoped) (func(), error) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := limiter.clock()
	for scope, entry := range limiter.windows {
		if entry.active == 0 && now.Sub(entry.started) > 2*time.Minute {
			delete(limiter.windows, scope)
		}
	}
	for _, scoped := range limits {
		entry := limiter.entry(scoped.Scope, now)
		if scoped.RequestsPerMinute > 0 && entry.requests >= scoped.RequestsPerMinute {
			return nil, ErrExceeded
		}
		if scoped.Concurrency > 0 && entry.active >= scoped.Concurrency {
			return nil, ErrExceeded
		}
	}
	for _, scoped := range limits {
		entry := limiter.entry(scoped.Scope, now)
		entry.requests++
		entry.active++
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			limiter.mu.Lock()
			defer limiter.mu.Unlock()
			for _, scoped := range limits {
				limiter.entry(scoped.Scope, limiter.clock()).active--
			}
		})
	}, nil
}

func (limiter *Limiter) entry(scope string, now time.Time) *window {
	entry, found := limiter.windows[scope]
	if !found {
		entry = &window{started: now}
		limiter.windows[scope] = entry
	}
	if now.Sub(entry.started) >= time.Minute {
		entry.started = now
		entry.requests = 0
	}
	return entry
}
