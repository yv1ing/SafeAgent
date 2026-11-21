// Package routing selects a channel target for a model. It combines ordered route targets,
// per-key cooling, circuit breakers and sticky sessions; it does not call upstreams itself.
package routing

import (
	"math/rand/v2"
	"slices"
	"time"
)

const (
	initialCooldown = 30 * time.Second
	maximumCooldown = 5 * time.Minute
	probeLifetime   = 15 * time.Second
)

type State string

const (
	StateClosed   State = "closed"
	StateOpen     State = "open"
	StateHalfOpen State = "half_open"
)

type Target struct {
	ChannelID       string
	UpstreamModel   string
	Format          string
	Priority        int
	Weight          int
	MaxOutputTokens int
}

func (target Target) Backend() Backend {
	return Backend{ChannelID: target.ChannelID, UpstreamModel: target.UpstreamModel, Format: target.Format}
}

type Candidate struct {
	Target Target
	Keys   []Key
}

// Admission is a candidate cleared by its breaker for one attempt; its outcome must be reported
// exactly once through RecordSuccess, RecordFailure or RecordNeutral.
type Admission struct {
	Candidate
	ticket Ticket
}

type Health struct {
	State    State `json:"state"`
	Failures int   `json:"consecutive_failures"`
	Cooldown int64 `json:"cooldown_seconds"`
}

type Selector struct {
	breakers *Breakers
	keys     *KeyPool
	sticky   *Sticky
	maxTries int
}

func NewSelector(breakers *Breakers, keys *KeyPool, sticky *Sticky, maxTries int) *Selector {
	if maxTries < 1 {
		maxTries = 3
	}
	return &Selector{breakers: breakers, keys: keys, sticky: sticky, maxTries: maxTries}
}

// Plan orders the candidates that look usable right now. It reserves nothing: the caller must
// Admit a candidate right before trying it and report the outcome exactly once.
func (selector *Selector) Plan(targets []Target, keysByChannel map[string][]Key, stickyKey, preferredFormat string) []Candidate {
	available := make([]Candidate, 0, len(targets))
	for _, target := range targets {
		if !selector.breakers.Allows(target.Backend()) {
			continue
		}
		keys := selector.keys.Available(target.ChannelID, keysByChannel[target.ChannelID])
		if len(keys) == 0 {
			continue
		}
		available = append(available, Candidate{Target: target, Keys: keys})
	}
	if len(available) == 0 {
		return nil
	}
	ordered := selector.ordered(available, preferredFormat)
	if preferred, found := selector.sticky.Get(stickyKey); found && stickyKey != "" && (preferredFormat == "" || preferred.Format == preferredFormat) {
		if index := slices.IndexFunc(ordered, func(candidate Candidate) bool { return candidate.Target.Backend() == preferred }); index > 0 {
			pinned := ordered[index]
			ordered = append([]Candidate{pinned}, slices.Delete(ordered, index, index+1)...)
		}
	}
	return ordered[:min(len(ordered), selector.maxTries)]
}

// Admit asks the candidate's breaker for one attempt; false means it is no longer usable.
func (selector *Selector) Admit(candidate Candidate) (Admission, bool) {
	ticket, admitted := selector.breakers.Take(candidate.Target.Backend())
	return Admission{Candidate: candidate, ticket: ticket}, admitted
}

func (selector *Selector) ordered(candidates []Candidate, preferredFormat string) []Candidate {
	byPriority := make([]int, 0, len(candidates))
	groups := map[int][]Candidate{}
	for _, candidate := range candidates {
		priority := candidate.Target.Priority
		if _, found := groups[priority]; !found {
			byPriority = append(byPriority, priority)
		}
		groups[priority] = append(groups[priority], candidate)
	}
	slices.Sort(byPriority)
	ordered := make([]Candidate, 0, len(candidates))
	for _, priority := range byPriority {
		group := groups[priority]
		if preferredFormat != "" {
			preferred := make([]Candidate, 0, len(group))
			rest := make([]Candidate, 0, len(group))
			for _, candidate := range group {
				if candidate.Target.Format == preferredFormat {
					preferred = append(preferred, candidate)
				} else {
					rest = append(rest, candidate)
				}
			}
			ordered = append(ordered, selector.weighted(preferred)...)
			ordered = append(ordered, selector.weighted(rest)...)
			continue
		}
		ordered = append(ordered, selector.weighted(group)...)
	}
	return ordered
}

func (selector *Selector) weighted(group []Candidate) []Candidate {
	pool := slices.Clone(group)
	result := make([]Candidate, 0, len(pool))
	for len(pool) > 0 {
		total := 0
		for _, candidate := range pool {
			total += weightOf(candidate.Target)
		}
		choice := rand.IntN(total)
		selected := len(pool) - 1
		for index, candidate := range pool {
			choice -= weightOf(candidate.Target)
			if choice < 0 {
				selected = index
				break
			}
		}
		result = append(result, pool[selected])
		pool = slices.Delete(pool, selected, selected+1)
	}
	return result
}

func weightOf(target Target) int {
	if target.Weight > 0 {
		return target.Weight
	}
	return 1
}

func (selector *Selector) RecordSuccess(stickyKey string, admission Admission) {
	selector.breakers.Success(admission.ticket)
	if stickyKey != "" {
		selector.sticky.Set(stickyKey, admission.Target.Backend())
	}
}

func (selector *Selector) RecordFailure(admission Admission) {
	selector.breakers.Failure(admission.ticket)
}

// RecordNeutral settles an attempt whose outcome says nothing about upstream health.
func (selector *Selector) RecordNeutral(admission Admission) {
	selector.breakers.Release(admission.ticket)
}

func (selector *Selector) CoolKey(channelID, keyID string, duration time.Duration) {
	selector.keys.Cool(channelID, keyID, duration)
}

func (selector *Selector) Health(channelID, upstreamModel string) Health {
	return selector.breakers.Health(Backend{ChannelID: channelID, UpstreamModel: upstreamModel})
}
