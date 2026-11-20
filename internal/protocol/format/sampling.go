package format

import (
	"fmt"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

func ParseStop(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	if text, ok := value.(string); ok {
		return []string{text}, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("stop sequences must be strings")
	}
	stop := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("stop sequences must be strings")
		}
		stop = append(stop, text)
	}
	return stop, nil
}

func positiveLimit(root map[string]any, key string) (*int64, error) {
	value, exists := root[key]
	if !exists || value == nil {
		return nil, nil
	}
	limit, ok := protocol.Integer(value)
	if !ok || limit <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer", key)
	}
	return &limit, nil
}

func booleanField(root map[string]any, key string) (*bool, error) {
	value, exists := root[key]
	if !exists || value == nil {
		return nil, nil
	}
	flag, ok := value.(bool)
	if !ok {
		return nil, fmt.Errorf("%s must be boolean", key)
	}
	return &flag, nil
}

func optionalFloat(root map[string]any, key string) *float64 {
	if value, ok := protocol.Float(root[key]); ok {
		return &value
	}
	return nil
}

// DecodeScalars fills the fields shared by the OpenAI and Anthropic request shapes; limitKey names
// the format's output token limit.
func DecodeScalars(request *protocol.Request, root map[string]any, limitKey string) error {
	stream, err := booleanField(root, "stream")
	if err != nil {
		return err
	}
	request.Stream = stream != nil && *stream
	if request.MaxOutputTokens, err = positiveLimit(root, limitKey); err != nil {
		return err
	}
	request.Temperature, request.TopP = optionalFloat(root, "temperature"), optionalFloat(root, "top_p")
	if request.ParallelToolCalls, err = booleanField(root, "parallel_tool_calls"); err != nil {
		return err
	}
	return nil
}

func DecodeOpenAISampling(request *protocol.Request, root map[string]any) {
	request.Seed = protocol.OptionalInteger(root["seed"])
	request.FrequencyPenalty = optionalFloat(root, "frequency_penalty")
	request.PresencePenalty = optionalFloat(root, "presence_penalty")
	request.User = protocol.String(root["user"])
	request.ServiceTier = protocol.String(root["service_tier"])
	request.PromptCacheKey = protocol.String(root["prompt_cache_key"])
	if metadata := protocol.Object(root["metadata"]); metadata != nil {
		request.Metadata = make(map[string]string, len(metadata))
		for key, value := range metadata {
			request.Metadata[key] = protocol.String(value)
		}
	}
}
