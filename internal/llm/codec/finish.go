package codec

import "github.com/yv1ing/SafeAgent/internal/llm/model"

// FinishLoss reports whether encoding the reason for the target loses its exact meaning, so the
// caller can record finish_mapped instead of silently approximating.
func FinishLoss(reason model.FinishReason, target model.Format) bool {
	if reason == "" || reason == model.FinishStop {
		return false
	}
	switch target {
	case model.Chat:
		return reason != model.FinishLength && reason != model.FinishToolCalls && reason != model.FinishContentFilter
	case model.Messages:
		return reason != model.FinishLength && reason != model.FinishToolCalls && reason != model.FinishRefusal && reason != model.FinishPauseTurn
	case model.Gemini:
		return reason == model.FinishRefusal || reason == model.FinishPauseTurn
	case model.Responses:
		return reason == model.FinishRefusal || reason == model.FinishPauseTurn
	}
	return true
}

// FinishMappedLoss describes an unknown provider finish reason that was decoded as a plain stop.
func FinishMappedLoss(value string, from model.Format) model.Loss {
	return model.Loss{Path: "finish", Code: model.LossFinishMapped, Class: model.LossDegraded, Detail: "unknown finish reason " + value + " treated as stop", From: from}
}

func ParseChatFinish(value string) (model.FinishReason, bool) {
	switch value {
	case "stop", "":
		return model.FinishStop, true
	case "length":
		return model.FinishLength, true
	case "tool_calls", "function_call":
		return model.FinishToolCalls, true
	case "content_filter":
		return model.FinishContentFilter, true
	}
	return model.FinishStop, false
}

func ChatFinish(reason model.FinishReason) string {
	switch reason {
	case "":
		return ""
	case model.FinishLength:
		return "length"
	case model.FinishToolCalls:
		return "tool_calls"
	case model.FinishContentFilter, model.FinishRefusal:
		return "content_filter"
	}
	return "stop"
}

func ParseMessagesFinish(value string) (model.FinishReason, bool) {
	switch value {
	case "end_turn", "stop_sequence":
		return model.FinishStop, true
	case "max_tokens", "model_context_window_exceeded":
		return model.FinishLength, true
	case "tool_use":
		return model.FinishToolCalls, true
	case "refusal":
		return model.FinishRefusal, true
	case "pause_turn":
		return model.FinishPauseTurn, true
	}
	return model.FinishStop, false
}

func MessagesFinish(reason model.FinishReason, stopSequence string) string {
	switch reason {
	case model.FinishLength:
		return "max_tokens"
	case model.FinishToolCalls:
		return "tool_use"
	case model.FinishRefusal, model.FinishContentFilter:
		return "refusal"
	case model.FinishPauseTurn:
		return "pause_turn"
	}
	if stopSequence != "" {
		return "stop_sequence"
	}
	return "end_turn"
}

func ResponsesStatus(reason model.FinishReason) string {
	switch reason {
	case model.FinishLength, model.FinishContentFilter:
		return "incomplete"
	case model.FinishError:
		return "failed"
	}
	return "completed"
}

func GeminiFinish(reason model.FinishReason) string {
	switch reason {
	case model.FinishLength:
		return "MAX_TOKENS"
	case model.FinishContentFilter, model.FinishRefusal:
		return "SAFETY"
	case model.FinishError:
		return "OTHER"
	}
	return "STOP"
}

func ParseGeminiFinish(value string) (model.FinishReason, bool) {
	switch value {
	case "STOP", "FINISH_REASON_UNSPECIFIED":
		return model.FinishStop, true
	case "MAX_TOKENS":
		return model.FinishLength, true
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "LANGUAGE", "SPII", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION":
		return model.FinishContentFilter, true
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL", "TOO_MANY_TOOL_CALLS", "OTHER":
		return model.FinishError, true
	}
	return model.FinishStop, false
}
