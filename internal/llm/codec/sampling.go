package codec

import (
	"fmt"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
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
	limit, ok := model.Integer(value)
	if !ok || limit <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer", key)
	}
	return &limit, nil
}

// DecodeScalars fills the fields shared by the OpenAI and Anthropic request shapes; limitKey names
// the format's output token limit.
func DecodeScalars(request *model.Request, root map[string]any, limitKey string) error {
	stream, err := BooleanField(root, "stream")
	if err != nil {
		return err
	}
	request.Stream = stream != nil && *stream
	if request.MaxOutputTokens, err = positiveLimit(root, limitKey); err != nil {
		return err
	}
	request.Temperature, request.TopP = OptionalFloat(root, "temperature"), OptionalFloat(root, "top_p")
	if request.ParallelToolCalls, err = BooleanField(root, "parallel_tool_calls"); err != nil {
		return err
	}
	return nil
}

func DecodeOpenAISampling(request *model.Request, root map[string]any) {
	request.Seed = model.OptionalInteger(root["seed"])
	request.FrequencyPenalty = OptionalFloat(root, "frequency_penalty")
	request.PresencePenalty = OptionalFloat(root, "presence_penalty")
	request.User = model.String(root["user"])
	request.ServiceTier = model.String(root["service_tier"])
	request.PromptCacheKey = model.String(root["prompt_cache_key"])
	request.TopK = model.OptionalInteger(root["top_k"])
	if metadata := model.Object(root["metadata"]); metadata != nil {
		request.Metadata = make(map[string]string, len(metadata))
		for key, value := range metadata {
			request.Metadata[key] = model.String(value)
		}
	}
}
