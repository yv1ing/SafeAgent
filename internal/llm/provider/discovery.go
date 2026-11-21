package provider

import (
	"encoding/json"
	"errors"
	"strings"
)

func (endpoint EndpointSpec) DiscoveryURL(baseURL string) string {
	if endpoint.Discovery.Path == "" {
		return ""
	}
	base := strings.TrimRight(firstNonEmpty(baseURL, endpoint.BaseURL), "/")
	return base + "/" + strings.TrimLeft(endpoint.Discovery.Path, "/")
}

// ParseModels reads a model-listing shape into plain upstream model names. OpenAI and Anthropic
// both return a data array; Gemini returns a models array.
func ParseModels(shape DiscoveryShape, payload []byte) ([]string, error) {
	var root struct {
		Data   []map[string]any `json:"data"`
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, err
	}
	var names []string
	add := func(value string) {
		value = strings.TrimPrefix(value, "models/")
		if value != "" {
			names = append(names, value)
		}
	}
	switch shape {
	case DiscoveryOpenAI, DiscoveryAnthropic:
		if root.Data == nil {
			return nil, errors.New("the model listing has no data array")
		}
		for _, entry := range root.Data {
			add(text(entry["id"]))
		}
	case DiscoveryGemini:
		if root.Models == nil {
			return nil, errors.New("the model listing has no models array")
		}
		for _, entry := range root.Models {
			add(text(entry["name"]))
		}
	default:
		return nil, errors.New("unknown model listing shape")
	}
	return names, nil
}

func text(value any) string {
	result, _ := value.(string)
	return result
}
