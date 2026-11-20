package limit

import (
	"sync"
	"time"
)

const maximumEntries = 10_000

type bucketEntry struct {
	started time.Time
	count   int
}

// Bucket is a fixed-window counter keyed by an arbitrary string. Expired keys are dropped on every
// call so a scan of unique keys cannot grow the map without bound.
type Bucket struct {
	mu      sync.Mutex
	maximum int
	window  time.Duration
	entries map[string]bucketEntry
	clock   func() time.Time
}

func NewBucket(maximum int, window time.Duration) *Bucket {
	return &Bucket{maximum: maximum, window: window, entries: map[string]bucketEntry{}, clock: time.Now}
}

func (bucket *Bucket) expire(now time.Time) {
	for name, entry := range bucket.entries {
		if now.Sub(entry.started) >= bucket.window {
			delete(bucket.entries, name)
		}
	}
}

// makeRoom bounds the map even when every request uses a different key. One arbitrary entry is
// evicted; the caller holds the lock.
func (bucket *Bucket) makeRoom() {
	if len(bucket.entries) < maximumEntries {
		return
	}
	for name := range bucket.entries {
		delete(bucket.entries, name)
		return
	}
}

// Allow counts one use of key and reports whether it stayed within the maximum.
func (bucket *Bucket) Allow(key string) bool {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	now := bucket.clock()
	bucket.expire(now)
	entry, found := bucket.entries[key]
	if !found {
		bucket.makeRoom()
		entry.started = now
	}
	if entry.count >= bucket.maximum {
		return false
	}
	entry.count++
	bucket.entries[key] = entry
	return true
}

// Blocked reports whether key is over the maximum without counting a use.
func (bucket *Bucket) Blocked(key string) bool {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	bucket.expire(bucket.clock())
	return bucket.entries[key].count >= bucket.maximum
}

// Record counts one use of key; paired with Blocked it limits only the events worth counting,
// such as failed logins.
func (bucket *Bucket) Record(key string) {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	now := bucket.clock()
	bucket.expire(now)
	entry, found := bucket.entries[key]
	if !found {
		bucket.makeRoom()
		entry.started = now
	}
	entry.count++
	bucket.entries[key] = entry
}

func (bucket *Bucket) Reset(key string) {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	delete(bucket.entries, key)
}
