package codec

import (
	"errors"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// BooleanField reads an optional boolean, rejecting a value of another type.
func BooleanField(root map[string]any, key string) (*bool, error) {
	value, exists := root[key]
	if !exists || value == nil {
		return nil, nil
	}
	flag, ok := value.(bool)
	if !ok {
		return nil, errors.New(key + " must be boolean")
	}
	return &flag, nil
}

// OptionalFloat reads an optional number from a named field.
func OptionalFloat(root map[string]any, key string) *float64 {
	if value, ok := model.Float(root[key]); ok {
		return &value
	}
	return nil
}

func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func ValidJSON(value string) bool {
	_, err := model.DecodeJSON([]byte(value))
	return err == nil
}

func InputJSON(value any) string {
	if value == nil {
		return ""
	}
	return string(model.Marshal(value))
}

// PrefixLosses prepends a path to every loss, keeping the report attached to its node.
func PrefixLosses(losses []model.Loss, path string) []model.Loss {
	for index := range losses {
		losses[index].Path = joinPath(path, losses[index].Path)
	}
	return losses
}
