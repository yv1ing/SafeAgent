package limit

import (
	"container/list"
	"sync"
	"time"
)

const maximumEntries = 10_000

type bucketEntry struct {
	started time.Time
	count   int
}

type bucketNode struct {
	key   string
	entry bucketEntry
}

// Bucket is a fixed-window counter keyed by an arbitrary string. Entries are kept in insertion
// order so the oldest one is evicted in constant time when the map is full, and an expired entry is
// dropped when it is next read instead of being found by a scan on every operation.
type Bucket struct {
	mu      sync.Mutex
	maximum int
	window  time.Duration
	entries map[string]*list.Element
	order   *list.List
	clock   func() time.Time
}

func NewBucket(maximum int, window time.Duration) *Bucket {
	return &Bucket{maximum: maximum, window: window, entries: map[string]*list.Element{}, order: list.New(), clock: time.Now}
}

func (bucket *Bucket) entry(key string, now time.Time) *bucketNode {
	element, found := bucket.entries[key]
	if !found {
		return nil
	}
	node := element.Value.(*bucketNode)
	if now.Sub(node.entry.started) >= bucket.window {
		bucket.remove(element)
		return nil
	}
	return node
}

func (bucket *Bucket) remove(element *list.Element) {
	bucket.order.Remove(element)
	delete(bucket.entries, element.Value.(*bucketNode).key)
}

// makeRoom bounds the map even when every request uses a different key. The evicted entry is the
// front of the insertion order, so a rotating attacker cannot push out a recently blocked key.
func (bucket *Bucket) makeRoom() {
	if len(bucket.entries) < maximumEntries {
		return
	}
	if oldest := bucket.order.Front(); oldest != nil {
		bucket.remove(oldest)
	}
}

func (bucket *Bucket) create(key string, now time.Time) *bucketNode {
	bucket.makeRoom()
	node := &bucketNode{key: key, entry: bucketEntry{started: now}}
	bucket.entries[key] = bucket.order.PushBack(node)
	return node
}

// Allow counts one use of key and reports whether it stayed within the maximum.
func (bucket *Bucket) Allow(key string) bool {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	now := bucket.clock()
	node := bucket.entry(key, now)
	if node == nil {
		node = bucket.create(key, now)
	}
	if node.entry.count >= bucket.maximum {
		return false
	}
	node.entry.count++
	return true
}

// Blocked reports whether key is over the maximum without counting a use.
func (bucket *Bucket) Blocked(key string) bool {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	node := bucket.entry(key, bucket.clock())
	return node != nil && node.entry.count >= bucket.maximum
}

// Record counts one use of key; paired with Blocked it limits only the events worth counting,
// such as failed logins.
func (bucket *Bucket) Record(key string) {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	now := bucket.clock()
	node := bucket.entry(key, now)
	if node == nil {
		node = bucket.create(key, now)
	}
	node.entry.count++
}

func (bucket *Bucket) Reset(key string) {
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	if element, found := bucket.entries[key]; found {
		bucket.remove(element)
	}
}
