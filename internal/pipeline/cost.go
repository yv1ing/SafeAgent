package pipeline

import (
	"math"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func tokens(usage *model.Usage) int64 {
	if usage == nil {
		return 0
	}
	return usage.Input + usage.Output
}

// cost prices the uncached input separately: Input is gross and already contains the cached tokens.
// Charging the full input price plus the cache prices would bill the same tokens twice.
func cost(modelEntry *entity.Model, usage *model.Usage) *int64 {
	if usage == nil || modelEntry == nil {
		return nil
	}
	uncached := max(usage.Input-usage.CacheRead-usage.CacheWrite, 0)
	total := saturatingAdd(
		saturatingAdd(saturatingMul(modelEntry.InputPrice, uncached), saturatingMul(modelEntry.OutputPrice, usage.Output)),
		saturatingAdd(saturatingMul(modelEntry.CacheReadPrice, usage.CacheRead), saturatingMul(modelEntry.CacheWritePrice, usage.CacheWrite)),
	)
	value := total / 1_000_000
	return &value
}

func saturatingMul(left, right int64) int64 {
	if left <= 0 || right <= 0 {
		return 0
	}
	if left > math.MaxInt64/right {
		return math.MaxInt64
	}
	return left * right
}

func saturatingAdd(left, right int64) int64 {
	if left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}
