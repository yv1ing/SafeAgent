package routing

import (
	"sync"
	"time"
)

const rateCooldown = 60 * time.Second

type keyState struct {
	position int
	cooldown map[string]time.Time
}

type KeyPool struct {
	mu    sync.Mutex
	pools map[string]*keyState
	clock func() time.Time
}

func NewKeyPool(clock func() time.Time) *KeyPool {
	if clock == nil {
		clock = time.Now
	}
	return &KeyPool{pools: map[string]*keyState{}, clock: clock}
}

type Key struct {
	ID     string
	Secret string
}

func (pool *KeyPool) Available(channelID string, keys []Key) []Key {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	state := pool.state(channelID)
	now := pool.clock()
	available := make([]Key, 0, len(keys))
	start := state.position % max(1, len(keys))
	for offset := 0; offset < len(keys); offset++ {
		key := keys[(start+offset)%len(keys)]
		if until, cooling := state.cooldown[key.ID]; cooling && now.Before(until) {
			continue
		}
		available = append(available, key)
	}
	if len(available) > 0 {
		state.position = (start + 1) % len(keys)
	}
	return available
}

func (pool *KeyPool) Cool(channelID, keyID string, retryAfter time.Duration) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if retryAfter <= 0 {
		retryAfter = rateCooldown
	}
	pool.state(channelID).cooldown[keyID] = pool.clock().Add(retryAfter)
}

func (pool *KeyPool) state(channelID string) *keyState {
	value, found := pool.pools[channelID]
	if !found {
		value = &keyState{cooldown: map[string]time.Time{}}
		pool.pools[channelID] = value
	}
	return value
}
