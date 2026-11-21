package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

func DecodeJSON(payload []byte) (any, error) {
	if !utf8.Valid(payload) {
		return nil, errors.New("JSON must use valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("expected exactly one JSON value")
	}
	return value, nil
}

func DecodeObject(payload []byte) (map[string]any, error) {
	value, err := DecodeJSON(payload)
	if err != nil {
		return nil, err
	}
	object, valid := value.(map[string]any)
	if !valid {
		return nil, errors.New("expected JSON object")
	}
	return object, nil
}

func Object(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case json.RawMessage:
		result, _ := DecodeObject(typed)
		return result
	}
	return nil
}

// DecodeJSONOrNil is for payloads the codec itself produced and therefore knows to be valid.
func DecodeJSONOrNil(payload []byte) any {
	decoded, _ := DecodeJSON(payload)
	return decoded
}

func Array(value any) []any {
	result, _ := value.([]any)
	return result
}

func String(value any) string {
	result, _ := value.(string)
	return result
}

func Integer(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		result, err := typed.Int64()
		return result, err == nil
	case float64:
		return int64(typed), typed == float64(int64(typed))
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	}
	return 0, false
}

// OptionalInteger is Integer for fields that may be absent: it returns nil unless value is an integer.
func OptionalInteger(value any) *int64 {
	if number, ok := Integer(value); ok {
		return &number
	}
	return nil
}

// IntegerOrZero is Integer for fields where an absent or malformed value means zero.
func IntegerOrZero(value any) int64 {
	number, _ := Integer(value)
	return number
}

func Float(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		result, err := typed.Float64()
		return result, err == nil
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	}
	return 0, false
}

func Marshal(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}
