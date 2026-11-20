package format

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

// UnmodelledFields collects the fields of object that a codec does not model. A dotted name such as
// "generationConfig.topK" marks one child of a nested object as modelled; the remaining children
// are kept under the parent.
func UnmodelledFields(kind protocol.Format, object map[string]any, known ...string) protocol.Extras {
	exact := make(map[string]bool, len(known))
	nested := map[string]map[string]bool{}
	for _, name := range known {
		parent, child, dotted := strings.Cut(name, ".")
		if !dotted {
			exact[name] = true
			continue
		}
		if nested[parent] == nil {
			nested[parent] = map[string]bool{}
		}
		nested[parent][child] = true
	}
	fields := map[string]json.RawMessage{}
	for key, value := range object {
		if exact[key] {
			continue
		}
		if children, partial := nested[key]; partial {
			remaining := map[string]any{}
			for childKey, childValue := range protocol.Object(value) {
				if !children[childKey] {
					remaining[childKey] = childValue
				}
			}
			if len(remaining) == 0 {
				continue
			}
			value = remaining
		}
		fields[key] = json.RawMessage(protocol.Marshal(value))
	}
	if len(fields) == 0 {
		return protocol.Extras{}
	}
	return protocol.Extras{Format: kind, Fields: fields}
}

// ApplyExtras replays extras into an encoded object when the target is the format they came from;
// otherwise it reports every field as dropped. Fields the encoder already set win, and objects are
// merged key by key.
func ApplyExtras(object map[string]any, extras protocol.Extras, target protocol.Format, path string) []protocol.Loss {
	if len(extras.Fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(extras.Fields))
	for key := range extras.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if extras.Format != target {
		losses := make([]protocol.Loss, 0, len(keys))
		for _, key := range keys {
			losses = append(losses, protocol.Loss{Path: joinPath(path, key), Code: protocol.LossExtrasDropped, From: extras.Format, To: target})
		}
		return losses
	}
	for _, key := range keys {
		decoded, err := protocol.DecodeJSON(extras.Fields[key])
		if err != nil {
			continue
		}
		existing, present := object[key]
		if !present {
			object[key] = decoded
			continue
		}
		base, isObject := existing.(map[string]any)
		extra := protocol.Object(decoded)
		if !isObject || extra == nil {
			continue
		}
		for childKey, childValue := range extra {
			if _, has := base[childKey]; !has {
				base[childKey] = childValue
			}
		}
	}
	return nil
}

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
	return protocol.Loss{Path: path, Code: protocol.LossFieldDropped, Detail: detail, To: target}
}

func decodeCacheControl(value any) *protocol.CacheControl {
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

func encodeCacheControl(cache *protocol.CacheControl) map[string]any {
	encoded := map[string]any{"type": cache.Type}
	if cache.TTL != "" {
		encoded["ttl"] = cache.TTL
	}
	return encoded
}

func nativeExtras(kind protocol.Format, entry map[string]any) protocol.Extras {
	return protocol.Extras{Format: kind, Fields: map[string]json.RawMessage{protocol.NativeField: protocol.Marshal(entry)}}
}

func nativeObject(extras protocol.Extras, target protocol.Format) map[string]any {
	return protocol.Object(extras.For(target)[protocol.NativeField])
}
