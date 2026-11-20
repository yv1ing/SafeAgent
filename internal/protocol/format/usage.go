package format

import "github.com/yv1ing/SafeAgent/internal/protocol"

func ParseChatUsage(root map[string]any) *protocol.Usage {
	usage := protocol.Object(root["usage"])
	if usage == nil {
		return nil
	}
	result := &protocol.Usage{Input: count(usage["prompt_tokens"]), Output: count(usage["completion_tokens"])}
	if details := protocol.Object(usage["prompt_tokens_details"]); details != nil {
		result.CacheRead = count(details["cached_tokens"])
	}
	if details := protocol.Object(usage["completion_tokens_details"]); details != nil {
		result.Reasoning = count(details["reasoning_tokens"])
	}
	return result
}

func EncodeChatUsage(usage *protocol.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	return map[string]any{
		"prompt_tokens":             usage.Input,
		"completion_tokens":         usage.Output,
		"total_tokens":              usage.Input + usage.Output,
		"prompt_tokens_details":     map[string]any{"cached_tokens": usage.CacheRead},
		"completion_tokens_details": map[string]any{"reasoning_tokens": usage.Reasoning},
	}
}

func ParseResponsesUsage(root map[string]any) *protocol.Usage {
	usage := protocol.Object(root["usage"])
	if usage == nil {
		return nil
	}
	result := &protocol.Usage{Input: count(usage["input_tokens"]), Output: count(usage["output_tokens"])}
	if details := protocol.Object(usage["input_tokens_details"]); details != nil {
		result.CacheRead = count(details["cached_tokens"])
	}
	if details := protocol.Object(usage["output_tokens_details"]); details != nil {
		result.Reasoning = count(details["reasoning_tokens"])
	}
	return result
}

func EncodeResponsesUsage(usage *protocol.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	return map[string]any{
		"input_tokens":          usage.Input,
		"output_tokens":         usage.Output,
		"total_tokens":          usage.Input + usage.Output,
		"input_tokens_details":  map[string]any{"cached_tokens": usage.CacheRead},
		"output_tokens_details": map[string]any{"reasoning_tokens": usage.Reasoning},
	}
}

func ParseMessagesUsage(usage map[string]any) *protocol.Usage {
	if usage == nil {
		return nil
	}
	result := &protocol.Usage{
		Input:        count(usage["input_tokens"]) + count(usage["cache_read_input_tokens"]) + count(usage["cache_creation_input_tokens"]),
		Output:       count(usage["output_tokens"]),
		CacheRead:    count(usage["cache_read_input_tokens"]),
		CacheWrite:   count(usage["cache_creation_input_tokens"]),
		CacheWrite1h: count(protocol.Object(usage["cache_creation"])["ephemeral_1h_input_tokens"]),
		Reasoning:    count(protocol.Object(usage["output_tokens_details"])["reasoning_tokens"]),
	}
	return result
}

func EncodeMessagesUsage(usage *protocol.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	encoded := map[string]any{
		"input_tokens":                max(usage.Input-usage.CacheRead-usage.CacheWrite, 0),
		"output_tokens":               usage.Output,
		"cache_read_input_tokens":     usage.CacheRead,
		"cache_creation_input_tokens": usage.CacheWrite,
	}
	if usage.CacheWrite1h > 0 {
		encoded["cache_creation"] = map[string]any{"ephemeral_1h_input_tokens": usage.CacheWrite1h, "ephemeral_5m_input_tokens": usage.CacheWrite - usage.CacheWrite1h}
	}
	return encoded
}

func ParseGeminiUsage(root map[string]any) *protocol.Usage {
	usage := protocol.Object(root["usageMetadata"])
	if usage == nil {
		return nil
	}
	return &protocol.Usage{
		Input:     count(usage["promptTokenCount"]),
		Output:    count(usage["candidatesTokenCount"]) + count(usage["thoughtsTokenCount"]),
		CacheRead: count(usage["cachedContentTokenCount"]),
		Reasoning: count(usage["thoughtsTokenCount"]),
	}
}

func EncodeGeminiUsage(usage *protocol.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	encoded := map[string]any{
		"promptTokenCount":     usage.Input,
		"candidatesTokenCount": max(usage.Output-usage.Reasoning, 0),
		"totalTokenCount":      usage.Input + usage.Output,
	}
	if usage.Reasoning > 0 {
		encoded["thoughtsTokenCount"] = usage.Reasoning
	}
	if usage.CacheRead > 0 {
		encoded["cachedContentTokenCount"] = usage.CacheRead
	}
	return encoded
}

func count(value any) int64 {
	number, _ := protocol.Integer(value)
	return max(number, 0)
}
