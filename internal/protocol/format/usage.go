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
		putDetail(result, "prompt.audio", details["audio_tokens"])
	}
	if result.CacheRead == 0 {
		result.CacheRead = count(usage["prompt_cache_hit_tokens"])
	}
	if details := protocol.Object(usage["completion_tokens_details"]); details != nil {
		result.Reasoning = count(details["reasoning_tokens"])
		putDetail(result, "completion.audio", details["audio_tokens"])
		putDetail(result, "completion.accepted_prediction", details["accepted_prediction_tokens"])
		putDetail(result, "completion.rejected_prediction", details["rejected_prediction_tokens"])
	}
	result.Raw = CaptureRaw(protocol.Chat, usage, "prompt_tokens", "completion_tokens", "total_tokens", "prompt_tokens_details", "completion_tokens_details")
	return result
}

func EncodeChatUsage(usage *protocol.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	prompt := map[string]any{"cached_tokens": usage.CacheRead}
	completion := map[string]any{"reasoning_tokens": usage.Reasoning}
	copyDetail(prompt, usage, "prompt.audio", "audio_tokens")
	copyDetail(completion, usage, "completion.audio", "audio_tokens")
	copyDetail(completion, usage, "completion.accepted_prediction", "accepted_prediction_tokens")
	copyDetail(completion, usage, "completion.rejected_prediction", "rejected_prediction_tokens")
	encoded := map[string]any{
		"prompt_tokens":             usage.Input,
		"completion_tokens":         usage.Output,
		"total_tokens":              usage.Input + usage.Output,
		"prompt_tokens_details":     prompt,
		"completion_tokens_details": completion,
	}
	RawFields(encoded, usage.Raw, protocol.Chat, "usage")
	return encoded
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
	result.Raw = CaptureRaw(protocol.Responses, usage, "input_tokens", "output_tokens", "total_tokens", "input_tokens_details", "output_tokens_details")
	return result
}

func EncodeResponsesUsage(usage *protocol.Usage) map[string]any {
	if usage == nil {
		return nil
	}
	encoded := map[string]any{
		"input_tokens":          usage.Input,
		"output_tokens":         usage.Output,
		"total_tokens":          usage.Input + usage.Output,
		"input_tokens_details":  map[string]any{"cached_tokens": usage.CacheRead},
		"output_tokens_details": map[string]any{"reasoning_tokens": usage.Reasoning},
	}
	RawFields(encoded, usage.Raw, protocol.Responses, "usage")
	return encoded
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
	if result.Reasoning == 0 {
		result.Reasoning = count(protocol.Object(usage["output_tokens_details"])["thinking_tokens"])
	}
	result.Raw = CaptureRaw(protocol.Messages, usage, "input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens", "cache_creation", "output_tokens_details")
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
	if usage.Reasoning > 0 {
		encoded["output_tokens_details"] = map[string]any{"reasoning_tokens": usage.Reasoning}
	}
	RawFields(encoded, usage.Raw, protocol.Messages, "usage")
	return encoded
}

func ParseGeminiUsage(root map[string]any) *protocol.Usage {
	usage := protocol.Object(root["usageMetadata"])
	if usage == nil {
		return nil
	}
	result := &protocol.Usage{
		Input:     count(usage["promptTokenCount"]),
		Output:    count(usage["candidatesTokenCount"]) + count(usage["thoughtsTokenCount"]),
		CacheRead: count(usage["cachedContentTokenCount"]),
		Reasoning: count(usage["thoughtsTokenCount"]),
	}
	result.Raw = CaptureRaw(protocol.Gemini, usage, "promptTokenCount", "candidatesTokenCount", "totalTokenCount", "thoughtsTokenCount", "cachedContentTokenCount")
	return result
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
	RawFields(encoded, usage.Raw, protocol.Gemini, "usageMetadata")
	return encoded
}

func count(value any) int64 {
	number, _ := protocol.Integer(value)
	return max(number, 0)
}

func putDetail(usage *protocol.Usage, key string, value any) {
	number, ok := protocol.Integer(value)
	if !ok || number <= 0 {
		return
	}
	if usage.Details == nil {
		usage.Details = map[string]int64{}
	}
	usage.Details[key] = number
}

func copyDetail(target map[string]any, usage *protocol.Usage, key, field string) {
	if value := usage.Details[key]; value > 0 {
		target[field] = value
	}
}

// UsageLosses reports the usage buckets a target cannot carry. Cache and reasoning details are
// accounting data, so dropping them is degraded rather than fatal.
func UsageLosses(usage *protocol.Usage, target protocol.Format) []protocol.Loss {
	if usage == nil {
		return nil
	}
	var dropped []string
	if target != protocol.Messages {
		if usage.CacheWrite > 0 {
			dropped = append(dropped, "cache_write_tokens")
		}
		if usage.CacheWrite1h > 0 {
			dropped = append(dropped, "cache_write_1h_tokens")
		}
	}
	for key := range usage.Details {
		if !usageDetailSupported(target, key) {
			dropped = append(dropped, "details."+key)
		}
	}
	if len(dropped) == 0 {
		return nil
	}
	return []protocol.Loss{{Path: "usage", Code: protocol.LossFieldDropped, Class: protocol.LossDegraded, Detail: "the target cannot carry: " + joinKeys(dropped), To: target}}
}

func usageDetailSupported(target protocol.Format, key string) bool {
	if target != protocol.Chat {
		return false
	}
	switch key {
	case "prompt.audio", "completion.audio", "completion.accepted_prediction", "completion.rejected_prediction":
		return true
	}
	return false
}

func joinKeys(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += ", "
		}
		result += value
	}
	return result
}
