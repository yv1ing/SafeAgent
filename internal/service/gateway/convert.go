package gateway

import (
	"math"
	"net/http"

	"github.com/yv1ing/SafeAgent/internal/protocol"
	"github.com/yv1ing/SafeAgent/internal/protocol/format"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func tokens(usage *protocol.Usage) int64 {
	if usage == nil {
		return 0
	}
	return usage.Input + usage.Output
}

// cost prices the uncached input separately: Input is gross and already contains the cached tokens.
// Charging the full input price plus the cache prices would bill the same tokens twice.
func cost(channel *entity.Channel, upstreamModel string, usage *protocol.Usage) *int64 {
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

func convertLosses(losses []protocol.Loss) []entity.Loss {
	converted := make([]entity.Loss, 0, len(losses))
	for _, item := range losses {
		converted = append(converted, entity.Loss{Path: item.Path, Code: string(item.Code), Detail: item.Detail, From: string(item.From), To: string(item.To)})
	}
	return converted
}

func writeError(writer Writer, codecs format.Registry, kind protocol.Format, failure *protocol.Error) {
	status := failure.Status
	body := protocol.Marshal(map[string]any{"error": map[string]any{"message": failure.Message}})
	if codec, found := codecs.Get(kind); found {
		status, body = codec.EncodeError(failure, false)
	}
	if status == 0 {
		status = http.StatusInternalServerError
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

// WriteError renders one protocol-shaped failure; the HTTP layer uses it for errors that occur
// before the pipeline is entered.
func (pipeline *Pipeline) WriteError(writer Writer, kind protocol.Format, failure *protocol.Error) {
	writeError(writer, pipeline.codecs, kind, failure)
}
