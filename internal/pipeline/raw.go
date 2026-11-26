package pipeline

import (
	"strconv"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

// rawSafe carries the format rules that need no endpoint capability. Responses always takes the
// canonical request path because the gateway owns store, include and previous_response_id.
func rawSafe(request *model.Request, target model.Format) bool {
	if target == model.Responses {
		return false
	}
	if request.ChoiceCount > 1 && target == model.Chat {
		return true
	}
	return target != model.Messages || request.MaxOutputTokens != nil
}

// rawRequestSafe rejects any modelled value the target codec would drop or rewrite. Raw bytes are
// only safe when canonical encoding would preserve the request unchanged.
func rawRequestSafe(request *model.Request, endpoint endpoint) bool {
	capability, compat := endpoint.endpoint.Capability, endpoint.endpoint.Compat
	if request.Store != nil {
		return false
	}
	if request.TopK != nil && (!capability.TopK || compat.IgnoreTopK) {
		return false
	}
	if (request.Logprobs != nil || request.TopLogprobs != nil) && !capability.Logprobs {
		return false
	}
	if request.TopLogprobs != nil && (request.Logprobs == nil || !*request.Logprobs) {
		return false
	}
	if request.ServiceTier != "" && compat.IgnoreServiceTier {
		return false
	}
	if request.Reasoning != nil && request.Reasoning.BudgetTokens != nil && compat.IgnoreBudgetTokens {
		return false
	}
	if request.StreamOptions != nil && !capability.StreamUsage {
		return false
	}
	for _, modality := range request.Modalities {
		if modality != "text" {
			return false
		}
	}
	if !rawReasoningSafe(request, endpoint.format) {
		return false
	}
	if !rawItemsSafe(request, endpoint.format) {
		return false
	}
	if endpoint.format == model.Gemini && !strings.Contains(endpoint.upstreamModel, "gemini-3") {
		for _, item := range request.Items {
			if item.ToolCall != nil && item.ToolCall.ID != "" || item.CallID != "" {
				return false
			}
		}
	}
	if !rawToolsSafe(request, endpoint) {
		return false
	}
	return rawResponseFormatSafe(request.ResponseFormat, endpoint.format)
}

// rawBodySafe catches wire-level shapes that the canonical model normalizes for the same format.
func rawBodySafe(body []byte, target model.Format) bool {
	if target != model.Chat && target != model.Messages && target != model.Gemini {
		return true
	}
	root, err := model.DecodeObject(body)
	if err != nil {
		return false
	}
	switch target {
	case model.Chat:
		if root["max_tokens"] != nil || root["functions"] != nil || root["thinking"] != nil {
			return false
		}
		if choice, ok := root["tool_choice"].(string); ok && choice == "any" {
			return false
		}
		for _, value := range model.Array(root["messages"]) {
			message := model.Object(value)
			if message["function_call"] != nil || message["reasoning"] != nil {
				return false
			}
		}
	case model.Messages:
		if root["parallel_tool_calls"] != nil {
			return false
		}
		if value, found := root["metadata"]; found && value != nil {
			metadata := model.Object(value)
			if metadata == nil {
				return false
			}
			for key, entry := range metadata {
				if key != "user_id" {
					return false
				}
				if _, ok := entry.(string); !ok {
					return false
				}
			}
		}
		for _, value := range model.Array(root["messages"]) {
			switch model.String(model.Object(value)["role"]) {
			case "system", "developer":
				return false
			}
		}
	case model.Gemini:
		if config := model.Object(root["generationConfig"]); config["responseSchema"] != nil {
			return false
		}
	}
	return true
}

func rawItemsSafe(request *model.Request, target model.Format) bool {
	if target != model.Messages {
		return true
	}
	for _, item := range request.Items {
		if item.Role == model.RoleDeveloper {
			return false
		}
		if item.Role != model.RoleSystem {
			continue
		}
		for _, part := range item.Parts {
			if part.Kind != model.PartText {
				return false
			}
		}
	}
	return true
}

func rawReasoningSafe(request *model.Request, target model.Format) bool {
	reasoning := request.Reasoning
	if reasoning == nil {
		return true
	}
	if reasoning.IncludeThoughts != nil && target != model.Gemini {
		return false
	}
	if reasoning.Summary != "" && target != model.Responses {
		return false
	}
	switch target {
	case model.Chat:
		return reasoning.BudgetTokens == nil && reasoning.Effort != "auto" && reasoning.Effort != "max"
	case model.Messages:
		if reasoning.Effort == "minimal" || reasoning.BudgetTokens != nil && reasoning.Effort != "" {
			return false
		}
		if reasoning.BudgetTokens == nil {
			return true
		}
		budget := *reasoning.BudgetTokens
		return budget <= 0 || budget >= 1024 && (request.MaxOutputTokens == nil || *request.MaxOutputTokens > budget)
	case model.Gemini:
		if reasoning.BudgetTokens != nil && reasoning.Effort != "" {
			return false
		}
		if reasoning.Effort == "" {
			return true
		}
		switch reasoning.Effort {
		case "minimal", "low", "medium", "high":
			return strings.HasPrefix(request.Model, "gemini-3")
		}
		return false
	}
	return true
}

func rawToolsSafe(request *model.Request, endpoint endpoint) bool {
	for _, tool := range request.Tools {
		if tool.Strict != nil && !endpoint.endpoint.Capability.Tools.Strict {
			return false
		}
	}
	if request.ParallelToolCalls != nil && len(request.Tools) == 0 {
		return false
	}
	if request.ToolChoice == nil {
		return true
	}
	if len(request.Tools) == 0 && request.ToolChoice.Mode != "auto" && request.ToolChoice.Mode != "none" {
		return false
	}
	return endpoint.format != model.Messages || request.ToolChoice.Mode != "allowed"
}

func rawResponseFormatSafe(responseFormat *model.ResponseFormat, target model.Format) bool {
	if responseFormat == nil {
		return true
	}
	extra := responseFormat.Name != "" || responseFormat.Description != "" || responseFormat.Strict != nil
	switch target {
	case model.Messages:
		return responseFormat.Kind != "json_object" && (responseFormat.Kind != "json_schema" || !extra)
	case model.Gemini:
		return responseFormat.Kind != "json_schema" || !extra
	}
	return true
}

func downgradeCustomTools(request *model.Request) []model.Loss {
	var losses []model.Loss
	for index := range request.Tools {
		tool := &request.Tools[index]
		if tool.Kind != model.ToolCustom {
			continue
		}
		tool.Kind = model.ToolFunction
		tool.Parameters = customToolSchema()
		tool.Format = nil
		losses = append(losses, model.Loss{Path: "tools[" + strconv.Itoa(index) + "]", Code: model.LossToolKindMapped, Class: model.LossDegraded, Detail: "the endpoint has no custom tool type; the tool became a function taking one string input", To: request.Source})
	}
	return losses
}
