package pipeline

import (
	"context"
	"sync"
)

// inflight tracks the calls a client may cancel by response id. A call registers before it opens
// an upstream and removes itself when it ends.
type inflight struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func newInflight() *inflight {
	return &inflight{cancels: map[string]context.CancelFunc{}}
}

func (registry *inflight) register(callID string, cancel context.CancelFunc) {
	registry.mu.Lock()
	registry.cancels[callID] = cancel
	registry.mu.Unlock()
}

func (registry *inflight) remove(callID string) {
	registry.mu.Lock()
	delete(registry.cancels, callID)
	registry.mu.Unlock()
}

// Cancel aborts one running call; it reports whether the call was still running.
func (registry *inflight) Cancel(callID string) bool {
	registry.mu.Lock()
	cancel, found := registry.cancels[callID]
	registry.mu.Unlock()
	if found {
		cancel()
	}
	return found
}
