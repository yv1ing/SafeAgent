package format

import (
	"strings"

	"github.com/yv1ing/SafeAgent/internal/protocol"
)

const defaultMessagesMaxTokens = 8192

func generationConfig(root map[string]any) map[string]any {
	config := protocol.Object(root["generationConfig"])
	if config == nil {
		config = map[string]any{}
		root["generationConfig"] = config
	}
	return config
}

// EncodeParams writes the sampling, limit, tool-choice, response-format and reasoning settings of
// request into root, the JSON object of the target format.
func EncodeParams(request *protocol.Request, target protocol.Format, root map[string]any) ([]protocol.Loss, error) {
	var losses []protocol.Loss
	loss := func(path, detail string) { losses = append(losses, dropped(path, detail, target)) }
	if request.MaxOutputTokens != nil {
		switch target {
		case protocol.Chat:
			root["max_completion_tokens"] = *request.MaxOutputTokens
		case protocol.Responses:
			root["max_output_tokens"] = *request.MaxOutputTokens
		case protocol.Messages:
			root["max_tokens"] = *request.MaxOutputTokens
		case protocol.Gemini:
			generationConfig(root)["maxOutputTokens"] = *request.MaxOutputTokens
		}
	} else if target == protocol.Messages {
		root["max_tokens"] = defaultMessagesMaxTokens
		losses = append(losses, protocol.Loss{Path: "max_output_tokens", Code: protocol.LossDefaultTokenLimit, Detail: "Messages requires a token limit; 8192 was applied", To: target})
	}
	setSampling := func(chatKey, geminiKey string, value any) {
		if target == protocol.Gemini {
			generationConfig(root)[geminiKey] = value
		} else {
			root[chatKey] = value
		}
	}
	if request.Temperature != nil {
		setSampling("temperature", "temperature", *request.Temperature)
	}
	if request.TopP != nil {
		setSampling("top_p", "topP", *request.TopP)
	}
	if request.TopK != nil {
		switch target {
		case protocol.Gemini:
			generationConfig(root)["topK"] = *request.TopK
		case protocol.Messages:
			root["top_k"] = *request.TopK
		default:
			loss("top_k", "target has no top-k sampling")
		}
	}
	if len(request.Stop) > 0 {
		switch target {
		case protocol.Messages:
			root["stop_sequences"] = request.Stop
		case protocol.Gemini:
			generationConfig(root)["stopSequences"] = request.Stop
		case protocol.Responses:
			loss("stop", "Responses has no stop sequence field")
		default:
			root["stop"] = request.Stop
		}
	}
	losses = append(losses, encodeIdentity(request, target, root)...)
	if request.Seed != nil {
		switch target {
		case protocol.Chat:
			root["seed"] = *request.Seed
		case protocol.Gemini:
			generationConfig(root)["seed"] = *request.Seed
		default:
			loss("seed", "target cannot fix the sampling seed")
		}
	}
	penalties := []struct {
		path        string
		value       *float64
		geminiKey   string
		chatDefault bool
	}{{"frequency_penalty", request.FrequencyPenalty, "frequencyPenalty", true}, {"presence_penalty", request.PresencePenalty, "presencePenalty", true}}
	for _, penalty := range penalties {
		if penalty.value == nil {
			continue
		}
		switch target {
		case protocol.Chat:
			root[penalty.path] = *penalty.value
		case protocol.Gemini:
			generationConfig(root)[penalty.geminiKey] = *penalty.value
		default:
			loss(penalty.path, "target has no "+strings.ReplaceAll(penalty.path, "_", " "))
		}
	}
	if request.ServiceTier != "" {
		if target == protocol.Chat || target == protocol.Responses || target == protocol.Messages {
			root["service_tier"] = request.ServiceTier
		} else {
			loss("service_tier", "target has no OpenAI service tier")
		}
	}
	if request.Logprobs != nil || request.TopLogprobs != nil {
		loss("logprobs", "token logprobs are not first-class and are not requested upstream")
	}
	if len(request.LogitBias) > 0 {
		if target == protocol.Chat {
			root["logit_bias"] = request.LogitBias
		} else {
			loss("logit_bias", "target has no logit bias")
		}
	}
	if len(request.Modalities) > 0 {
		unsupported := false
		for _, modality := range request.Modalities {
			if modality != "text" {
				unsupported = true
			}
		}
		if unsupported {
			loss("modalities", "non-text output modalities are not first-class")
		}
		if (target == protocol.Chat || target == protocol.Responses) && !unsupported {
			root["modalities"] = request.Modalities
		} else if target != protocol.Chat && target != protocol.Responses {
			loss("modalities", "target has no output modality selection")
		}
	}
	if request.Verbosity != "" {
		if target == protocol.Responses {
			text := protocol.Object(root["text"])
			if text == nil {
				text = map[string]any{}
				root["text"] = text
			}
			text["verbosity"] = request.Verbosity
		} else {
			loss("verbosity", "only Responses has output verbosity")
		}
	}
	if request.PromptCacheKey != "" {
		if target == protocol.Chat || target == protocol.Responses {
			root["prompt_cache_key"] = request.PromptCacheKey
		} else {
			loss("prompt_cache_key", "target has no prompt cache key")
		}
	}
	if request.PromptCacheRetention != "" && target != protocol.Responses {
		loss("prompt_cache_retention", "only Responses controls prompt cache retention")
	}
	if request.SafetyIdentifier != "" {
		if target == protocol.Chat {
			root["safety_identifier"] = request.SafetyIdentifier
		} else if target == protocol.Gemini || target == protocol.Messages {
			loss("safety_identifier", "target has no safety identifier")
		}
	}
	nested, err := encodeToolPolicy(request, target, root)
	if err != nil {
		return nil, err
	}
	losses = append(losses, nested...)
	if request.ResponseFormat != nil {
		losses = append(losses, encodeResponseFormat(request.ResponseFormat, target, root)...)
	}
	if request.Reasoning != nil {
		nested, err := encodeReasoningConfig(request.Reasoning, target, request.Model, root)
		if err != nil {
			return nil, err
		}
		losses = append(losses, nested...)
	}
	return losses, nil
}

func encodeIdentity(request *protocol.Request, target protocol.Format, root map[string]any) []protocol.Loss {
	var losses []protocol.Loss
	metadata := request.Metadata
	switch target {
	case protocol.Chat, protocol.Responses:
		if request.User != "" {
			root["user"] = request.User
		}
		if len(metadata) > 0 {
			root["metadata"] = metadata
		}
	case protocol.Messages:
		identity := map[string]any{}
		if request.User != "" {
			identity["user_id"] = request.User
		}
		for key, value := range metadata {
			if key == "user_id" && request.User == "" {
				identity[key] = value
				continue
			}
			losses = append(losses, dropped("metadata."+key, "Messages metadata only carries user_id", target))
		}
		if len(identity) > 0 {
			root["metadata"] = identity
		}
	case protocol.Gemini:
		if request.User != "" {
			losses = append(losses, dropped("user", "target has no user identifier", target))
		}
		if len(metadata) > 0 {
			losses = append(losses, dropped("metadata", "target has no request metadata", target))
		}
	}
	return losses
}

func encodeToolPolicy(request *protocol.Request, target protocol.Format, root map[string]any) ([]protocol.Loss, error) {
	var losses []protocol.Loss
	var choice any
	if request.ToolChoice != nil && root["tools"] != nil {
		encoded, nested, err := encodeToolChoice(request.ToolChoice, request.Tools, target)
		if err != nil {
			return nil, err
		}
		losses = append(losses, nested...)
		choice = encoded
		if target == protocol.Gemini {
			root["toolConfig"] = encoded
		} else {
			root["tool_choice"] = encoded
		}
	} else if request.ToolChoice != nil && request.ToolChoice.Mode != "auto" && request.ToolChoice.Mode != "none" {
		losses = append(losses, dropped("tool_choice", "a tool choice without tools is meaningless", target))
	}
	if request.ParallelToolCalls == nil {
		return losses, nil
	}
	switch target {
	case protocol.Chat, protocol.Responses:
		if root["tools"] != nil {
			root["parallel_tool_calls"] = *request.ParallelToolCalls
		}
	case protocol.Messages:
		if root["tools"] == nil || request.ToolChoice != nil && request.ToolChoice.Mode == "none" {
			break
		}
		object := protocol.Object(choice)
		if object == nil {
			object = map[string]any{"type": "auto"}
			root["tool_choice"] = object
		}
		object["disable_parallel_tool_use"] = !*request.ParallelToolCalls
	case protocol.Gemini:
		losses = append(losses, dropped("parallel_tool_calls", "target cannot enforce parallel tool policy", target))
	}
	return losses, nil
}
