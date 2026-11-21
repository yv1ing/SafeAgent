package codec

import (
	"errors"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

func NormalizeGeminiSchema(schema map[string]any) (map[string]any, error) {
	return normalizeSchema(schema, 0)
}

func normalizeSchema(schema map[string]any, depth int) (map[string]any, error) {
	if depth > 64 {
		return nil, errors.New("schema nesting exceeds supported limit")
	}
	result := make(map[string]any, len(schema))
	for key, value := range schema {
		switch key {
		case "type":
			if kind, ok := value.(string); ok {
				value = strings.ToLower(kind)
			} else if alternatives, ok := value.([]any); ok {
				types := make([]any, len(alternatives))
				for index, alternative := range alternatives {
					kind, ok := alternative.(string)
					if !ok {
						return nil, errors.New("schema types must be strings")
					}
					types[index] = strings.ToLower(kind)
				}
				value = types
			} else {
				return nil, errors.New("invalid schema type")
			}
		case "properties", "$defs", "definitions", "patternProperties":
			entries := model.Object(value)
			if entries == nil {
				return nil, errors.New("invalid schema property map")
			}
			children := make(map[string]any, len(entries))
			for name, child := range entries {
				normalized, err := normalizeChild(child, depth)
				if err != nil {
					return nil, err
				}
				children[name] = normalized
			}
			value = children
		case "items", "additionalProperties", "not", "if", "then", "else", "contains":
			normalized, err := normalizeChild(value, depth)
			if err != nil {
				return nil, err
			}
			value = normalized
		case "anyOf", "allOf", "oneOf", "prefixItems":
			alternatives, ok := value.([]any)
			if !ok {
				return nil, errors.New("schema alternatives must be an array")
			}
			children := make([]any, len(alternatives))
			for index, child := range alternatives {
				normalized, err := normalizeChild(child, depth)
				if err != nil {
					return nil, err
				}
				children[index] = normalized
			}
			value = children
		case "nullable":
			if _, ok := value.(bool); !ok {
				return nil, errors.New("schema nullable must be boolean")
			}
			continue
		}
		result[key] = value
	}
	if schema["nullable"] == true {
		return map[string]any{"anyOf": []any{result, map[string]any{"type": "null"}}}, nil
	}
	return result, nil
}

func normalizeChild(child any, depth int) (any, error) {
	nested := model.Object(child)
	if nested == nil {
		return child, nil
	}
	return normalizeSchema(nested, depth+1)
}
