package routing

import (
	"sync"
	"time"
)

const failureThreshold = 5

type breaker struct {
	state      State
	failures   int
	openedAt   time.Time
	cooldown   time.Duration
	probing    bool
	probeStart time.Time
	generation uint64
}

// Ticket is one admitted attempt. Only the ticket of the current half-open probe decides the
// probe's outcome; a ticket admitted while the breaker was closed only counts toward opening it,
// so a slow attempt that started before the breaker opened cannot clear someone else's probe.
type Ticket struct {
	backend    Backend
	probe      bool
	generation uint64
}

// Backend identifies what a breaker protects: one catalog model on one channel in one format.
type Backend struct {
	ChannelID string
	ModelID   string
	Format    string
}

type Breakers struct {
	mu      sync.Mutex
	entries map[Backend]*breaker
	clock   func() time.Time
}

func NewBreakers(clock func() time.Time) *Breakers {
	if clock == nil {
		clock = time.Now
	}
	return &Breakers{entries: map[Backend]*breaker{}, clock: clock}
}

func (breakers *Breakers) entry(backend Backend) *breaker {
	value, found := breakers.entries[backend]
	if !found {
		value = &breaker{state: StateClosed, cooldown: initialCooldown}
		breakers.entries[backend] = value
	}
	return value
}

// probeAvailable reports whether a half-open probe may start. A probe that outlived probeLifetime
// is presumed lost, so a caller that never reported back cannot block the backend forever.
func (value *breaker) probeAvailable(now time.Time) bool {
	if value.probing && now.Sub(value.probeStart) < probeLifetime {
		return false
	}
	return now.Sub(value.openedAt) >= value.cooldown
}

// Allows is a read-only check used when planning; Take reserves the probe.
func (breakers *Breakers) Allows(backend Backend) bool {
	breakers.mu.Lock()
	defer breakers.mu.Unlock()
	value := breakers.entry(backend)
	if value.state == StateClosed {
		return true
	}
	return value.probeAvailable(breakers.clock())
}

// Take admits one attempt. Every ticket must be answered by exactly one of Success, Failure or
// Release; a probe ticket that never is expires after probeLifetime.
func (breakers *Breakers) Take(backend Backend) (Ticket, bool) {
	breakers.mu.Lock()
	defer breakers.mu.Unlock()
	value := breakers.entry(backend)
	if value.state == StateClosed {
		return Ticket{backend: backend}, true
	}
	now := breakers.clock()
	if !value.probeAvailable(now) {
		return Ticket{}, false
	}
	value.state = StateHalfOpen
	value.probing = true
	value.probeStart = now
	value.generation++
	return Ticket{backend: backend, probe: true, generation: value.generation}, true
}

// current returns the breaker when ticket still speaks for it: a probe ticket of the running probe,
// or an ordinary ticket while the breaker is closed.
func (breakers *Breakers) current(ticket Ticket) (*breaker, bool) {
	value := breakers.entry(ticket.backend)
	if ticket.probe {
		return value, value.state == StateHalfOpen && value.probing && value.generation == ticket.generation
	}
	return value, value.state == StateClosed
}

// Release hands a probe back without judging the backend.
func (breakers *Breakers) Release(ticket Ticket) {
	breakers.mu.Lock()
	defer breakers.mu.Unlock()
	if value, ok := breakers.current(ticket); ok && ticket.probe {
		value.state = StateOpen
		value.probing = false
	}
}

func (breakers *Breakers) Success(ticket Ticket) {
	breakers.mu.Lock()
	defer breakers.mu.Unlock()
	value, ok := breakers.current(ticket)
	if !ok {
		return
	}
	value.state = StateClosed
	value.failures = 0
	value.probing = false
	if ticket.probe {
		value.cooldown = initialCooldown
	}
}

func (breakers *Breakers) Failure(ticket Ticket) {
	breakers.mu.Lock()
	defer breakers.mu.Unlock()
	value, ok := breakers.current(ticket)
	if !ok {
		return
	}
	if ticket.probe {
		value.state = StateOpen
		value.openedAt = breakers.clock()
		value.probing = false
		value.failures = 0
		value.cooldown = min(value.cooldown*2, maximumCooldown)
		return
	}
	value.failures++
	if value.failures >= failureThreshold {
		value.state = StateOpen
		value.openedAt = breakers.clock()
		value.failures = 0
	}
}

func (breakers *Breakers) Health(backend Backend) Health {
	breakers.mu.Lock()
	defer breakers.mu.Unlock()
	value := breakers.entry(backend)
	return Health{State: value.state, Failures: value.failures, Cooldown: int64(value.cooldown / time.Second)}
}
