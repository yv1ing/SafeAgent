package limit

import (
	"strconv"
	"testing"
	"time"
)

func bucketKey(index int) string { return "key-" + strconv.Itoa(index) }

func newTestBucket(maximum int, window time.Duration, now *time.Time) *Bucket {
	bucket := NewBucket(maximum, window)
	bucket.clock = func() time.Time { return *now }
	return bucket
}

func TestBucketEvictsInOrderWithoutScanning(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bucket := newTestBucket(2, time.Minute, &now)
	for index := 0; index < maximumEntries; index++ {
		bucket.Record(bucketKey(index))
	}
	if len(bucket.entries) != maximumEntries || bucket.order.Len() != maximumEntries {
		t.Fatalf("accounting is not capped: %d entries, %d ordered", len(bucket.entries), bucket.order.Len())
	}
	front := bucket.order.Front()
	bucket.Record(bucketKey(0))
	if bucket.order.Front() != front {
		t.Fatal("counting a known key must not reorder or evict")
	}
	bucket.Record("another")
	if len(bucket.entries) != maximumEntries || bucket.order.Len() != maximumEntries {
		t.Fatalf("eviction did not keep the cap: %d entries, %d ordered", len(bucket.entries), bucket.order.Len())
	}
	if _, found := bucket.entries[bucketKey(0)]; found {
		t.Fatal("the oldest entry must be the one evicted")
	}
	if _, found := bucket.entries["another"]; !found {
		t.Fatal("the new key must be counted")
	}
	if bucket.order.Front().Value.(*bucketNode).key != bucketKey(1) {
		t.Fatalf("the ordered head is not the next oldest: %+v", bucket.order.Front().Value)
	}
}

func TestBucketDropsAnExpiredEntryOnAccess(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	bucket := newTestBucket(1, time.Minute, &now)
	bucket.Record(bucketKey(0))
	if !bucket.Blocked(bucketKey(0)) || bucket.Blocked(bucketKey(1)) {
		t.Fatal("only the recorded key of a full window is blocked")
	}
	now = now.Add(time.Minute)
	if bucket.Blocked(bucketKey(0)) {
		t.Fatal("an expired window must not stay blocked")
	}
	if _, found := bucket.entries[bucketKey(0)]; found {
		t.Fatal("an expired key must be dropped when it is read")
	}
	bucket.Record(bucketKey(0))
	if bucket.order.Back().Value.(*bucketNode).key != bucketKey(0) {
		t.Fatal("a re-counted key must move to the newest position")
	}
	if len(bucket.entries) != bucket.order.Len() {
		t.Fatalf("entries and order diverged: %d != %d", len(bucket.entries), bucket.order.Len())
	}
}
