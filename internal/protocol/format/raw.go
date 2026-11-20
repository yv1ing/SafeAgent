package format

import (
	"encoding/json"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

func joinPath(path, key string) string {
	switch {
	case path == "":
		return key
	case key == "":
		return path
	}
	return path + "." + key
}

func dropped(path, detail string, target protocol.Format) protocol.Loss {
	return protocol.Loss{Path: path, Code: protocol.LossFieldDropped, Class: protocol.LossDegraded, Detail: detail, To: target}
}

func Degraded(path, detail string, target protocol.Format) protocol.Loss {
	return dropped(path, detail, target)
}

func prefixLosses(losses []protocol.Loss, path string) []protocol.Loss {
	for index := range losses {
		losses[index].Path = joinPath(path, losses[index].Path)
	}
	return losses
}

func DecodeCacheControl(value any) *protocol.CacheControl {
	object := protocol.Object(value)
	if object == nil {
		return nil
	}
	kind := protocol.String(object["type"])
	if kind == "" {
		kind = "ephemeral"
	}
	return &protocol.CacheControl{Type: kind, TTL: protocol.String(object["ttl"])}
}

func EncodeCacheControl(cache *protocol.CacheControl) map[string]any {
	encoded := map[string]any{"type": cache.Type}
	if cache.TTL != "" {
		encoded["ttl"] = cache.TTL
	}
	return encoded
}

// nativeRaw keeps the complete provider object of a node with no canonical form. It is replayed
// only towards the same format.
func nativeRaw(kind protocol.Format, entry map[string]any) *protocol.Raw {
	return &protocol.Raw{Format: kind, Fields: map[string]json.RawMessage{"native": protocol.Marshal(entry)}}
}

func RawObject(raw *protocol.Raw, target protocol.Format) map[string]any {
	return protocol.Object(raw.For(target)["native"])
}
