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
func cost(channel *entity.Channel, upstreamModel string, usage *model.Usage) *int64 {
	if usage == nil {
		return nil
	}
	for _, entry := range channel.Models {
		if entry.Name != upstreamModel {
			continue
		}
		uncached := max(usage.Input-usage.CacheRead-usage.CacheWrite, 0)
		total := saturatingAdd(
			saturatingAdd(saturatingMul(entry.InputPrice, uncached), saturatingMul(entry.OutputPrice, usage.Output)),
			saturatingAdd(saturatingMul(entry.CacheReadPrice, usage.CacheRead), saturatingMul(entry.CacheWritePrice, usage.CacheWrite)),
		)
		value := total / 1_000_000
		return &value
	}
	return nil
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
