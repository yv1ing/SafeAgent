package analysis

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/yv1ing/SafeAgent/internal/observation"
)

// Detector is the extension point. Each detector reads the protocol-neutral trace and returns
// findings; it never sees storage, the gateway or the console.
type Detector interface {
	ID() string
	Version() string
	Evaluate(ctx context.Context, trace *observation.Trace) []Finding
}

// Registry is the process-wide detector set. Registration is explicit and idempotent for the same
// id and version.
type Registry struct {
	mu        sync.RWMutex
	detectors map[string]Detector
}

func NewRegistry() *Registry {
	return &Registry{detectors: map[string]Detector{}}
}

var defaultRegistry = NewRegistry()

func DefaultRegistry() *Registry { return defaultRegistry }

func (registry *Registry) Register(detector Detector) error {
	if detector == nil {
		return fmt.Errorf("detector is nil")
	}
	if detector.ID() == "" || detector.Version() == "" {
		return fmt.Errorf("detector needs an id and a version")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if existing, found := registry.detectors[detector.ID()]; found {
		if existing.Version() == detector.Version() {
			return nil
		}
		return fmt.Errorf("detector %s version %s is already registered", detector.ID(), existing.Version())
	}
	registry.detectors[detector.ID()] = detector
	return nil
}

func MustRegister(detector Detector) {
	if err := DefaultRegistry().Register(detector); err != nil {
		panic(err)
	}
}

func (registry *Registry) Detectors() []Detector {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	detectors := make([]Detector, 0, len(registry.detectors))
	for _, detector := range registry.detectors {
		detectors = append(detectors, detector)
	}
	sort.Slice(detectors, func(left, right int) bool { return detectors[left].ID() < detectors[right].ID() })
	return detectors
}

func (registry *Registry) Version() string {
	parts := make([]string, 0)
	for _, detector := range registry.Detectors() {
		parts = append(parts, detector.ID()+"@"+detector.Version())
	}
	return joinVersion(parts)
}
