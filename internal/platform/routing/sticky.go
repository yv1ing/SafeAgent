package routing

import (
	"container/list"
	"sync"
	"time"
)

const (
	stickyCapacity = 100_000
	stickyLifetime = time.Hour
)

type stickyEntry struct {
	key       string
	backend   Backend
	expiresAt time.Time
}

type Sticky struct {
	mu      sync.Mutex
	items   map[string]*list.Element
	order   *list.List
	clock   func() time.Time
	maxSize int
}

func NewSticky(clock func() time.Time) *Sticky {
	if clock == nil {
		clock = time.Now
	}
	return &Sticky{items: map[string]*list.Element{}, order: list.New(), clock: clock, maxSize: stickyCapacity}
}

func (sticky *Sticky) Get(key string) (Backend, bool) {
	sticky.mu.Lock()
	defer sticky.mu.Unlock()
	element, found := sticky.items[key]
	if !found {
		return Backend{}, false
	}
	entry := element.Value.(*stickyEntry)
	if sticky.clock().After(entry.expiresAt) {
		sticky.order.Remove(element)
		delete(sticky.items, key)
		return Backend{}, false
	}
	sticky.order.MoveToFront(element)
	return entry.backend, true
}

func (sticky *Sticky) Set(key string, backend Backend) {
	sticky.mu.Lock()
	defer sticky.mu.Unlock()
	expires := sticky.clock().Add(stickyLifetime)
	if element, found := sticky.items[key]; found {
		entry := element.Value.(*stickyEntry)
		entry.backend = backend
		entry.expiresAt = expires
		sticky.order.MoveToFront(element)
		return
	}
	element := sticky.order.PushFront(&stickyEntry{key: key, backend: backend, expiresAt: expires})
	sticky.items[key] = element
	for sticky.order.Len() > sticky.maxSize {
		oldest := sticky.order.Back()
		if oldest == nil {
			break
		}
		sticky.order.Remove(oldest)
		delete(sticky.items, oldest.Value.(*stickyEntry).key)
	}
}
