package codec

import (
	"encoding/json"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
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

func dropped(path, detail string, target model.Format) model.Loss {
	return model.Loss{Path: path, Code: model.LossFieldDropped, Class: model.LossDegraded, Detail: detail, To: target}
}

func Degraded(path, detail string, target model.Format) model.Loss {
	return dropped(path, detail, target)
}

func DecodeCacheControl(value any) *model.CacheControl {
	object := model.Object(value)
	if object == nil {
		return nil
	}
	kind := model.String(object["type"])
	if kind == "" {
		kind = "ephemeral"
	}
	return &model.CacheControl{Type: kind, TTL: model.String(object["ttl"])}
}

func EncodeCacheControl(cache *model.CacheControl) map[string]any {
	encoded := map[string]any{"type": cache.Type}
	if cache.TTL != "" {
		encoded["ttl"] = cache.TTL
	}
	return encoded
}

// nativeRaw keeps the complete provider object of a node with no canonical form. It is replayed
// only towards the same format.
func nativeRaw(kind model.Format, entry map[string]any) *model.Raw {
	return &model.Raw{Format: kind, Fields: map[string]json.RawMessage{"native": model.Marshal(entry)}}
}

func RawObject(raw *model.Raw, target model.Format) map[string]any {
	return model.Object(raw.For(target)["native"])
}
