package codec

import (
	"fmt"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
)

var effortTiers = []struct {
	limit  int64
	effort string
}{{5000, "low"}, {15000, "medium"}, {30000, "high"}}

func effortForBudget(budget int64) string {
	switch {
	case budget == 0:
		return "none"
	case budget < 0:
		return "auto"
	}
	for _, tier := range effortTiers {
		if budget <= tier.limit {
			return tier.effort
		}
	}
	return "xhigh"
}

func encodeReasoningConfig(reasoning *model.ReasoningConfig, target model.Format, modelName string, root map[string]any, compat model.Compat) ([]model.Loss, error) {
	var losses []model.Loss
	loss := func(path, detail string) { losses = append(losses, dropped(path, detail, target)) }
	effort := reasoning.Effort
	switch effort {
	case "", "none", "auto", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return nil, fmt.Errorf("unsupported reasoning effort %q", effort)
	}
	budget := reasoning.BudgetTokens
	if budget != nil && compat.IgnoreBudgetTokens {
		loss("reasoning.budget_tokens", "the target endpoint ignores an explicit thinking budget")
		budget = nil
	}
	if budget != nil && effort != "" {
		loss("reasoning.budget_tokens", "explicit effort takes precedence over token budget")
		budget = nil
	}
	if budget != nil && (target == model.Chat || target == model.Responses) {
		effort = effortForBudget(*budget)
		loss("reasoning.budget_tokens", "token budget represented as an approximate effort tier")
		budget = nil
	}
	if target != model.Gemini && reasoning.IncludeThoughts != nil {
		loss("reasoning.include_thoughts", "target has no equivalent visibility control")
	}
	if target != model.Responses && reasoning.Summary != "" {
		loss("reasoning.summary", "target has no reasoning summary selection")
	}
	switch target {
	case model.Chat, model.Responses:
		if effort == "auto" {
			loss("reasoning.effort", "automatic thinking budget has no OpenAI effort equivalent")
			effort = ""
		}
		if effort == "max" {
			effort = "xhigh"
			loss("reasoning.effort", "maximum effort represented as xhigh")
		}
		if target == model.Chat {
			if effort != "" {
				root["reasoning_effort"] = effort
			}
			break
		}
		entry := map[string]any{}
		if effort != "" {
			entry["effort"] = effort
		}
		if reasoning.Summary != "" {
			entry["summary"] = reasoning.Summary
		}
		if len(entry) > 0 {
			root["reasoning"] = entry
		}
	case model.Messages:
		losses = append(losses, encodeMessagesThinking(reasoning, effort, budget, root)...)
	case model.Gemini:
		nested, err := encodeGeminiThinking(reasoning, effort, budget, modelName, root)
		if err != nil {
			return nil, err
		}
		losses = append(losses, nested...)
	}
	return losses, nil
}

func encodeMessagesThinking(reasoning *model.ReasoningConfig, effort string, budget *int64, root map[string]any) []model.Loss {
	var losses []model.Loss
	if budget != nil {
		switch {
		case *budget == 0:
			root["thinking"] = map[string]any{"type": "disabled"}
		case *budget < 0:
			root["thinking"] = map[string]any{"type": "adaptive"}
		default:
			tokens := *budget
			if tokens < 1024 {
				tokens = 1024
				losses = append(losses, dropped("reasoning.budget_tokens", "Messages thinking budget was raised to the 1024 minimum", model.Messages))
			}
			if limit, has := model.Integer(root["max_tokens"]); !has || limit <= tokens {
				root["max_tokens"] = tokens + 1024
				losses = append(losses, dropped("max_output_tokens", "max_tokens was raised above the thinking budget", model.Messages))
			}
			root["thinking"] = map[string]any{"type": "enabled", "budget_tokens": tokens}
		}
		applyThinkingDisplay(reasoning, root)
		return losses
	}
	switch effort {
	case "none":
		root["thinking"] = map[string]any{"type": "disabled"}
	case "auto", "":
		root["thinking"] = map[string]any{"type": "adaptive"}
	default:
		if effort == "minimal" {
			effort = "low"
			losses = append(losses, dropped("reasoning.effort", "minimal effort represented as low", model.Messages))
		}
		root["thinking"] = map[string]any{"type": "adaptive"}
		config := model.Object(root["output_config"])
		if config == nil {
			config = map[string]any{}
			root["output_config"] = config
		}
		config["effort"] = effort
	}
	applyThinkingDisplay(reasoning, root)
	return losses
}

func applyThinkingDisplay(reasoning *model.ReasoningConfig, root map[string]any) {
	thinking := model.Object(root["thinking"])
	if thinking == nil || reasoning == nil {
		return
	}
	if reasoning.Display != "" {
		thinking["display"] = reasoning.Display
	}
	if reasoning.BetweenTools {
		thinking["between_tools"] = true
	}
}

func encodeGeminiThinking(reasoning *model.ReasoningConfig, effort string, budget *int64, modelName string, root map[string]any) ([]model.Loss, error) {
	var losses []model.Loss
	thinking := map[string]any{}
	switch {
	case budget != nil:
		thinking["thinkingBudget"] = *budget
	case effort != "":
		if strings.HasPrefix(modelName, "gemini-3") && (effort == "minimal" || effort == "low" || effort == "medium" || effort == "high") {
			thinking["thinkingLevel"] = effort
			break
		}
		tokens, known := map[string]int64{"none": 0, "auto": -1, "minimal": 512, "low": 1024, "medium": 8192, "high": 32768, "xhigh": 32768, "max": 32768}[effort]
		if !known {
			return nil, fmt.Errorf("gemini cannot represent reasoning effort %q", effort)
		}
		thinking["thinkingBudget"] = tokens
		if effort != "none" && effort != "auto" {
			losses = append(losses, dropped("reasoning.effort", "effort represented as a model-dependent token budget", model.Gemini))
		}
	}
	if reasoning.IncludeThoughts != nil {
		thinking["includeThoughts"] = *reasoning.IncludeThoughts
	}
	if len(thinking) > 0 {
		generationConfig(root)["thinkingConfig"] = thinking
	}
	return losses, nil
}
