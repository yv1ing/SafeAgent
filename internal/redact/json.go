package redact

import (
	"encoding/json"
	"strconv"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// redactJSON redacts the string values of a JSON document in place; a value that is not JSON is
// redacted as text.
func (applier *applier) redactJSON(value []byte, path string) ([]byte, error) {
	if len(value) == 0 {
		return value, nil
	}
	decoded, err := model.DecodeJSON(value)
	if err != nil {
		return []byte(applier.text(string(value), path)), nil
	}
	transformed, err := applier.redactValue(decoded, path)
	if err != nil || applier.dry {
		return value, err
	}
	return model.Marshal(transformed), nil
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

// ApplyAuxiliaryBody redacts an auxiliary request body (embeddings, moderations). The top-level
// model value is structural and is checked but preserved for routing; every other key and value is
// walked exactly like the canonical request path.
func (scope *Scope) ApplyAuxiliaryBody(body []byte) ([]byte, *Result, error) {
	if scope.engine.Empty() {
		return body, &Result{}, nil
	}
	root, err := model.DecodeObject(body)
	if err != nil {
		return nil, nil, err
	}
	modelName, hasModel := root["model"].(string)
	if hasModel {
		delete(root, "model")
	}
	content := model.Marshal(root)
	dry := &applier{scope: scope, result: &Result{}, dry: true}
	if _, err := dry.redactJSON(content, "body"); err != nil {
		return nil, nil, err
	}
	if hasModel {
		if err := dry.immutable(modelName); err != nil {
			return nil, nil, err
		}
	}
	real := &applier{scope: scope, result: &Result{}}
	out, err := real.redactJSON(content, "body")
	if err != nil {
		return nil, nil, err
	}
	if hasModel {
		object := model.Object(model.DecodeJSONOrNil(out))
		if object == nil {
			return nil, nil, ErrImmutablePosition
		}
		object["model"] = modelName
		out = model.Marshal(object)
	}
	return out, real.result, nil
}
