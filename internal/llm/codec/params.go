package codec

import (
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

const defaultMessagesMaxTokens = 8192

func generationConfig(root map[string]any) map[string]any {
	config := model.Object(root["generationConfig"])
	if config == nil {
		config = map[string]any{}
		root["generationConfig"] = config
	}
	return config
}

// EncodeParams writes the sampling, limit, tool-choice, response-format and reasoning settings of
// request into root, the JSON object of the target format.
func EncodeParams(request *model.Request, target model.Format, root map[string]any, profile model.Profile) ([]model.Loss, error) {
	var losses []model.Loss
	loss := func(path, detail string) { losses = append(losses, dropped(path, detail, target)) }
	if request.MaxOutputTokens != nil {
		switch target {
		case model.Chat:
			root["max_completion_tokens"] = *request.MaxOutputTokens
		case model.Responses:
			root["max_output_tokens"] = *request.MaxOutputTokens
		case model.Messages:
			root["max_tokens"] = *request.MaxOutputTokens
		case model.Gemini:
			generationConfig(root)["maxOutputTokens"] = *request.MaxOutputTokens
		}
	} else if target == model.Messages {
		root["max_tokens"] = defaultMessagesMaxTokens
		losses = append(losses, model.Loss{Path: "max_output_tokens", Code: model.LossDefaultTokenLimit, Detail: "Messages requires a token limit; 8192 was applied", To: target})
	}
	compat := profile.Compat
	setSampling := func(chatKey, geminiKey string, value any) {
		if target == model.Gemini {
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
		switch {
		case compat.IgnoreTopK:
			loss("top_k", "the target endpoint ignores top-k sampling")
		case target == model.Gemini:
			generationConfig(root)["topK"] = *request.TopK
		case target == model.Messages:
			root["top_k"] = *request.TopK
		default:
			loss("top_k", "target has no top-k sampling")
		}
	}
	if len(request.Stop) > 0 {
		switch target {
		case model.Messages:
			root["stop_sequences"] = request.Stop
		case model.Gemini:
			generationConfig(root)["stopSequences"] = request.Stop
		case model.Responses:
			loss("stop", "Responses has no stop sequence field")
		default:
			root["stop"] = request.Stop
		}
	}
	losses = append(losses, encodeIdentity(request, target, root)...)
	if request.Seed != nil {
		switch target {
		case model.Chat:
			root["seed"] = *request.Seed
		case model.Gemini:
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
		case model.Chat:
			root[penalty.path] = *penalty.value
		case model.Gemini:
			generationConfig(root)[penalty.geminiKey] = *penalty.value
		default:
			loss(penalty.path, "target has no "+strings.ReplaceAll(penalty.path, "_", " "))
		}
	}
	if request.ServiceTier != "" {
		if compat.IgnoreServiceTier {
			loss("service_tier", "the target endpoint ignores service tiers")
		} else if target == model.Chat || target == model.Responses || target == model.Messages {
			root["service_tier"] = request.ServiceTier
		} else {
			loss("service_tier", "target has no OpenAI service tier")
		}
	}
	if request.Logprobs != nil || request.TopLogprobs != nil {
		if target == model.Chat && profile.Capability.Logprobs {
			root["logprobs"] = true
			if request.TopLogprobs != nil {
				root["top_logprobs"] = *request.TopLogprobs
			}
		} else {
			loss("logprobs", "the target endpoint does not expose token logprobs")
		}
	}
	if len(request.LogitBias) > 0 {
		if target == model.Chat {
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
		if (target == model.Chat || target == model.Responses) && !unsupported {
			root["modalities"] = request.Modalities
		} else if target != model.Chat && target != model.Responses {
			loss("modalities", "target has no output modality selection")
		}
	}
	if request.Verbosity != "" {
		if target == model.Responses {
			text := model.Object(root["text"])
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
		if target == model.Chat || target == model.Responses {
			root["prompt_cache_key"] = request.PromptCacheKey
		} else {
			loss("prompt_cache_key", "target has no prompt cache key")
		}
	}
	if request.PromptCacheRetention != "" && target != model.Responses {
		loss("prompt_cache_retention", "only Responses controls prompt cache retention")
	}
	if len(request.SafetySettings) > 0 && target != model.Gemini {
		loss("safety_settings", "only Gemini carries content safety thresholds")
	}
	if request.SafetyIdentifier != "" {
		if target == model.Chat {
			root["safety_identifier"] = request.SafetyIdentifier
		} else if target == model.Gemini || target == model.Messages {
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
		nested, err := encodeReasoningConfig(request.Reasoning, target, request.Model, root, compat)
		if err != nil {
			return nil, err
		}
		losses = append(losses, nested...)
	}
	return losses, nil
}

func encodeIdentity(request *model.Request, target model.Format, root map[string]any) []model.Loss {
	var losses []model.Loss
	metadata := request.Metadata
	switch target {
	case model.Chat, model.Responses:
		if request.User != "" {
			root["user"] = request.User
		}
		if len(metadata) > 0 {
			root["metadata"] = metadata
		}
	case model.Messages:
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
	case model.Gemini:
		if request.User != "" {
			losses = append(losses, dropped("user", "target has no user identifier", target))
		}
		if len(metadata) > 0 {
			losses = append(losses, dropped("metadata", "target has no request metadata", target))
		}
	}
	return losses
}

func encodeToolPolicy(request *model.Request, target model.Format, root map[string]any) ([]model.Loss, error) {
	var losses []model.Loss
	var choice any
	if request.ToolChoice != nil && root["tools"] != nil {
		encoded, nested, err := encodeToolChoice(request.ToolChoice, request.Tools, target)
		if err != nil {
			return nil, err
		}
		losses = append(losses, nested...)
		choice = encoded
		if target == model.Gemini {
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
	case model.Chat, model.Responses:
		if root["tools"] != nil {
			root["parallel_tool_calls"] = *request.ParallelToolCalls
		}
	case model.Messages:
		if root["tools"] == nil || request.ToolChoice != nil && request.ToolChoice.Mode == "none" {
			break
		}
		object := model.Object(choice)
		if object == nil {
			object = map[string]any{"type": "auto"}
			root["tool_choice"] = object
		}
		object["disable_parallel_tool_use"] = !*request.ParallelToolCalls
	case model.Gemini:
		losses = append(losses, dropped("parallel_tool_calls", "target cannot enforce parallel tool policy", target))
	}
	return losses, nil
}
