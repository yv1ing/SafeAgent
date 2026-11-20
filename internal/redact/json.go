package redact

import (
	"encoding/json"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

// redactJSON redacts the string values of a JSON document in place; a value that is not JSON is
// redacted as text.
func (applier *applier) redactJSON(value []byte, path string) ([]byte, error) {
	if len(value) == 0 {
		return value, nil
	}
	decoded, err := protocol.DecodeJSON(value)
	if err != nil {
		return []byte(applier.text(string(value), path)), nil
	}
	transformed, err := applier.redactValue(decoded, path)
	if err != nil || applier.dry {
		return value, err
	}
	return protocol.Marshal(transformed), nil
}

func (applier *applier) redactValue(value any, path string) (any, error) {
	switch typed := value.(type) {
	case string:
		return applier.text(typed, path), nil
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			transformed, err := applier.redactValue(item, path+"."+strconv.Itoa(index))
			if err != nil {
				return nil, err
			}
			result[index] = transformed
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if err := applier.immutable(key); err != nil {
				return nil, err
			}
			transformed, err := applier.redactValue(item, path+"."+key)
			if err != nil {
				return nil, err
			}
			result[key] = transformed
		}
		return result, nil
	case json.Number:
		return value, applier.immutable(typed.String())
	}
	return value, nil
}
