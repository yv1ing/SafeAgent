package adapter

import "github.com/yv1ing/SafeAgent/internal/llm/model"

const (
	// MaximumHistoryBytes bounds the decoded ancestor conversation one request or replay expands.
	MaximumHistoryBytes = 32 << 20
	// MaximumHistoryDepth bounds the number of ancestor calls; an older chain is truncated rather
	// than replayed as if it were complete.
	MaximumHistoryDepth = 200
)

// TrimAncestors keeps the nearest ancestors that fit the shared depth and byte budget. Entries are
// oldest first; a non-empty result means everything kept, and the flag reports whether the oldest
// turns were dropped. The online gate and the replay path call it so both see the same history.
func TrimAncestors(entries []Ancestor) ([]Ancestor, bool) {
	start := 0
	if len(entries) > MaximumHistoryDepth {
		start = len(entries) - MaximumHistoryDepth
	}
	total := 0
	for index := len(entries) - 1; index >= start; index-- {
		total += ancestorSize(entries[index])
		if total > MaximumHistoryBytes {
			start = index + 1
			break
		}
	}
	if start == 0 {
		return entries, false
	}
	return entries[start:], true
}

func ancestorSize(entry Ancestor) int {
	size := len(model.Marshal(entry.Request.Items))
	if entry.Response != nil {
		size += len(model.Marshal(entry.Response.Response.Items))
	}
	return size
}
