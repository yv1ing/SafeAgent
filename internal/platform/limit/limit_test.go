package limit_test

import (
	"errors"
	"testing"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/limit"
)

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	limiter := limit.New(func() time.Time { return now })
	project := func(l limit.Limit) limit.Scoped { return limit.Scoped{Scope: "project:p", Limit: l} }
	key := func(id string, l limit.Limit) limit.Scoped { return limit.Scoped{Scope: "key:" + id, Limit: l} }

	t.Run("concurrency is held until release", func(t *testing.T) {
		release, err := limiter.Acquire(key("a", limit.Limit{Concurrency: 1}))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := limiter.Acquire(key("a", limit.Limit{Concurrency: 1})); !errors.Is(err, limit.ErrExceeded) {
			t.Fatalf("second request must be rejected while the first runs, got %v", err)
		}
		release()
		release()
		if _, err := limiter.Acquire(key("a", limit.Limit{Concurrency: 1})); err != nil {
			t.Fatalf("slot was not freed: %v", err)
		}
	})

	t.Run("project scope is shared across keys", func(t *testing.T) {
		now = now.Add(time.Hour)
		shared := limit.Limit{Concurrency: 1}
		release, err := limiter.Acquire(project(shared), key("x", limit.Limit{}))
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if _, err := limiter.Acquire(project(shared), key("y", limit.Limit{})); !errors.Is(err, limit.ErrExceeded) {
			t.Fatalf("project limit must apply to a different key, got %v", err)
		}
	})

	t.Run("rejection counts nothing", func(t *testing.T) {
		now = now.Add(time.Hour)
		projectLimit := project(limit.Limit{RequestsPerMinute: 2})
		release, err := limiter.Acquire(projectLimit, key("z", limit.Limit{Concurrency: 1}))
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if _, err := limiter.Acquire(projectLimit, key("z", limit.Limit{Concurrency: 1})); !errors.Is(err, limit.ErrExceeded) {
			t.Fatalf("key concurrency must reject, got %v", err)
		}
		if _, err := limiter.Acquire(projectLimit, key("other", limit.Limit{})); err != nil {
			t.Fatalf("the rejected attempt must not consume the project request budget: %v", err)
		}
	})

	t.Run("window rolls over", func(t *testing.T) {
		now = now.Add(time.Hour)
		spec := key("w", limit.Limit{RequestsPerMinute: 1})
		release, err := limiter.Acquire(spec)
		if err != nil {
			t.Fatal(err)
		}
		release()
		if _, err := limiter.Acquire(spec); !errors.Is(err, limit.ErrExceeded) {
			t.Fatalf("expected the minute window to be full, got %v", err)
		}
		now = now.Add(time.Minute)
		if _, err := limiter.Acquire(spec); err != nil {
			t.Fatalf("window did not reset: %v", err)
		}
	})
}

func TestBucketCountsOnlyRecordedFailures(t *testing.T) {
	bucket := limit.NewBucket(2, time.Minute)
	for range 5 {
		if bucket.Blocked("ip|user") {
			t.Fatal("Blocked must not count")
		}
	}
	bucket.Record("ip|user")
	bucket.Record("ip|user")
	if !bucket.Blocked("ip|user") || bucket.Blocked("ip|other") {
		t.Fatal("limit must apply to the recorded key only")
	}
	bucket.Reset("ip|user")
	if bucket.Blocked("ip|user") {
		t.Fatal("reset did not clear the key")
	}
	if !bucket.Allow("k") {
		t.Fatal("the first request must be admitted")
	}
	if !bucket.Allow("k") {
		t.Fatal("the second request must be admitted")
	}
	if bucket.Allow("k") {
		t.Fatal("Allow must admit exactly the maximum")
	}
}
