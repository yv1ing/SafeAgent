package routing_test

import (
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/routing"
)

type fixture struct {
	now      time.Time
	selector *routing.Selector
	breakers *routing.Breakers
	keys     *routing.KeyPool
}

func newFixture() *fixture {
	value := &fixture{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	clock := func() time.Time { return value.now }
	value.breakers = routing.NewBreakers(clock)
	value.keys = routing.NewKeyPool(clock)
	value.selector = routing.NewSelector(value.breakers, value.keys, routing.NewSticky(clock), 3)
	return value
}

// trip opens the breaker of backend with consecutive failures of attempts admitted while closed.
func trip(breakers *routing.Breakers, backend routing.Backend) {
	for range 5 {
		ticket, _ := breakers.Take(backend)
		breakers.Failure(ticket)
	}
}

func admit(t *testing.T, selector *routing.Selector, candidate routing.Candidate) routing.Admission {
	t.Helper()
	admission, admitted := selector.Admit(candidate)
	if !admitted {
		t.Fatalf("candidate was not admitted: %+v", candidate)
	}
	return admission
}

func keysFor(channels ...string) map[string][]routing.Key {
	result := map[string][]routing.Key{}
	for _, channel := range channels {
		result[channel] = []routing.Key{{ID: channel + "-key", Secret: "s"}}
	}
	return result
}

func TestPlanOrdersByPriorityThenSticky(t *testing.T) {
	f := newFixture()
	targets := []routing.Target{{ChannelID: "b", Priority: 2, Weight: 1}, {ChannelID: "a", Priority: 1, Weight: 1}}
	keys := keysFor("a", "b")
	plan := f.selector.Plan(targets, keys, "")
	if len(plan) != 2 || plan[0].Target.ChannelID != "a" {
		t.Fatalf("priority order was ignored: %+v", plan)
	}
	f.selector.RecordSuccess("session", admit(t, f.selector, plan[1]))
	if pinned := f.selector.Plan(targets, keys, "session"); pinned[0].Target.ChannelID != "b" {
		t.Fatalf("sticky target was not preferred over priority: %+v", pinned)
	}
}

func TestStickyTargetKeepsTheRestInPriorityOrder(t *testing.T) {
	f := newFixture()
	targets := []routing.Target{{ChannelID: "a", Priority: 1}, {ChannelID: "b", Priority: 2}, {ChannelID: "c", Priority: 3}}
	keys := keysFor("a", "b", "c")
	f.selector.RecordSuccess("session", admit(t, f.selector, routing.Candidate{Target: targets[2]}))
	plan := f.selector.Plan(targets, keys, "session")
	if len(plan) != 3 || plan[0].Target.ChannelID != "c" || plan[1].Target.ChannelID != "a" || plan[2].Target.ChannelID != "b" {
		t.Fatalf("the sticky target goes first and the rest keep their priority: %+v", plan)
	}
}

func TestStickyDistinguishesUpstreamModels(t *testing.T) {
	f := newFixture()
	targets := []routing.Target{{ChannelID: "a", UpstreamModel: "one", Priority: 1}, {ChannelID: "a", UpstreamModel: "two", Priority: 1}}
	f.selector.RecordSuccess("session", admit(t, f.selector, routing.Candidate{Target: targets[1]}))
	plan := f.selector.Plan(targets, keysFor("a"), "session")
	if len(plan) != 2 || plan[0].Target.UpstreamModel != "two" {
		t.Fatalf("sticky target was not preferred: %+v", plan)
	}
}

func TestPlanReservesNothing(t *testing.T) {
	f := newFixture()
	target := routing.Target{ChannelID: "a", UpstreamModel: "m"}
	trip(f.breakers, target.Backend())
	f.now = f.now.Add(31 * time.Second)
	for range 3 {
		if plan := f.selector.Plan([]routing.Target{target}, keysFor("a"), ""); len(plan) != 1 {
			t.Fatalf("a planned-but-unused probe must not hide the target: %+v", plan)
		}
	}
}

func TestBreakerProbeLifecycle(t *testing.T) {
	f := newFixture()
	backend := routing.Backend{ChannelID: "c", UpstreamModel: "m"}
	trip(f.breakers, backend)
	if _, admitted := f.breakers.Take(backend); admitted {
		t.Fatal("breaker should be open")
	}
	f.now = f.now.Add(31 * time.Second)
	probe, admitted := f.breakers.Take(backend)
	if !admitted {
		t.Fatal("breaker should admit a probe after the cooldown")
	}
	if _, admitted := f.breakers.Take(backend); admitted {
		t.Fatal("only one probe may run at a time")
	}
	f.breakers.Release(probe)
	probe, admitted = f.breakers.Take(backend)
	if !admitted {
		t.Fatal("a released probe must be available again")
	}
	f.breakers.Success(probe)
	if f.breakers.Health(backend).State != routing.StateClosed {
		t.Fatal("success must close the breaker")
	}
}

func TestStaleTicketsCannotSettleTheProbe(t *testing.T) {
	f := newFixture()
	backend := routing.Backend{ChannelID: "c", UpstreamModel: "m"}
	early, _ := f.breakers.Take(backend)
	trip(f.breakers, backend)
	f.now = f.now.Add(31 * time.Second)
	if _, admitted := f.breakers.Take(backend); !admitted {
		t.Fatal("probe expected")
	}
	f.breakers.Release(early)
	f.breakers.Success(early)
	if health := f.breakers.Health(backend); health.State != routing.StateHalfOpen {
		t.Fatalf("an attempt admitted before the breaker opened must not settle the probe: %+v", health)
	}
	if _, admitted := f.breakers.Take(backend); admitted {
		t.Fatal("the probe must still be reserved")
	}
}

func TestLostProbeExpires(t *testing.T) {
	f := newFixture()
	backend := routing.Backend{ChannelID: "c", UpstreamModel: "m"}
	trip(f.breakers, backend)
	f.now = f.now.Add(31 * time.Second)
	lost, admitted := f.breakers.Take(backend)
	if !admitted {
		t.Fatal("probe expected")
	}
	f.now = f.now.Add(16 * time.Second)
	if _, admitted := f.breakers.Take(backend); !admitted {
		t.Fatal("a probe that never reported back must expire")
	}
	f.breakers.Failure(lost)
	if health := f.breakers.Health(backend); health.State != routing.StateHalfOpen {
		t.Fatalf("an expired probe must not judge its successor: %+v", health)
	}
}

func TestHealthUsesTheBreakerKey(t *testing.T) {
	f := newFixture()
	target := routing.Target{ChannelID: "c", UpstreamModel: "m", Priority: 7, Weight: 3}
	for range 5 {
		f.selector.RecordFailure(admit(t, f.selector, routing.Candidate{Target: target}))
	}
	if health := f.selector.Health("c", "m"); health.State != routing.StateOpen {
		t.Fatalf("health must not depend on priority or weight: %+v", health)
	}
}

func TestKeyPoolCoolsRejectedKeys(t *testing.T) {
	f := newFixture()
	keys := []routing.Key{{ID: "one", Secret: "a"}, {ID: "two", Secret: "b"}}
	f.keys.Cool("channel", "one", time.Minute)
	if available := f.keys.Available("channel", keys); len(available) != 1 || available[0].ID != "two" {
		t.Fatalf("cooled key was still offered: %+v", available)
	}
}

func TestPlanSkipsChannelsWithoutKeys(t *testing.T) {
	f := newFixture()
	if plan := f.selector.Plan([]routing.Target{{ChannelID: "empty"}}, map[string][]routing.Key{}, ""); len(plan) != 0 {
		t.Fatalf("expected no candidates, got %+v", plan)
	}
}
