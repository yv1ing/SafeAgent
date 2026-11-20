package format

import "github.com/yv1ing/SafeAgent/internal/protocol"

// FinishLoss reports whether encoding the reason for the target loses its exact meaning, so the
// caller can record finish_mapped instead of silently approximating.
func FinishLoss(reason protocol.FinishReason, target protocol.Format) bool {
	if reason == "" || reason == protocol.FinishStop {
		return false
	}
	switch target {
	case protocol.Chat:
		return reason != protocol.FinishLength && reason != protocol.FinishToolCalls && reason != protocol.FinishContentFilter
	case protocol.Messages:
		return reason != protocol.FinishLength && reason != protocol.FinishToolCalls && reason != protocol.FinishRefusal && reason != protocol.FinishPauseTurn
	case protocol.Gemini:
		return reason == protocol.FinishRefusal || reason == protocol.FinishPauseTurn
	case protocol.Responses:
		return reason == protocol.FinishRefusal || reason == protocol.FinishPauseTurn
	}
	return true
}

// FinishMappedLoss describes an unknown provider finish reason that was decoded as a plain stop.
func FinishMappedLoss(value string, from protocol.Format) protocol.Loss {
	return protocol.Loss{Path: "finish", Code: protocol.LossFinishMapped, Class: protocol.LossDegraded, Detail: "unknown finish reason " + value + " treated as stop", From: from}
}

func ParseChatFinish(value string) (protocol.FinishReason, bool) {
	switch value {
	case "stop", "":
		return protocol.FinishStop, true
	case "length":
		return protocol.FinishLength, true
	case "tool_calls", "function_call":
		return protocol.FinishToolCalls, true
	case "content_filter":
		return protocol.FinishContentFilter, true
	}
	return protocol.FinishStop, false
}

func ChatFinish(reason protocol.FinishReason) string {
	switch reason {
	case "":
		return ""
	case protocol.FinishLength:
		return "length"
	case protocol.FinishToolCalls:
		return "tool_calls"
	case protocol.FinishContentFilter, protocol.FinishRefusal:
		return "content_filter"
	}
	return "stop"
}

func ParseMessagesFinish(value string) (protocol.FinishReason, bool) {
	switch value {
	case "end_turn", "stop_sequence":
		return protocol.FinishStop, true
	case "max_tokens", "model_context_window_exceeded":
		return protocol.FinishLength, true
	case "tool_use":
		return protocol.FinishToolCalls, true
	case "refusal":
		return protocol.FinishRefusal, true
	case "pause_turn":
		return protocol.FinishPauseTurn, true
	}
	return protocol.FinishStop, false
}

func MessagesFinish(reason protocol.FinishReason, stopSequence string) string {
	switch reason {
	case protocol.FinishLength:
		return "max_tokens"
	case protocol.FinishToolCalls:
		return "tool_use"
	case protocol.FinishRefusal, protocol.FinishContentFilter:
		return "refusal"
	case protocol.FinishPauseTurn:
		return "pause_turn"
	}
	if stopSequence != "" {
		return "stop_sequence"
	}
	return "end_turn"
}

func ResponsesStatus(reason protocol.FinishReason) string {
	switch reason {
	case protocol.FinishLength, protocol.FinishContentFilter:
		return "incomplete"
	case protocol.FinishError:
		return "failed"
	}
	return "completed"
}

func ResponsesIncompleteReason(reason protocol.FinishReason) string {
	if reason == protocol.FinishContentFilter {
		return "content_filter"
	}
	return "max_output_tokens"
}

func GeminiFinish(reason protocol.FinishReason) string {
	switch reason {
	case protocol.FinishLength:
		return "MAX_TOKENS"
	case protocol.FinishContentFilter, protocol.FinishRefusal:
		return "SAFETY"
	case protocol.FinishError:
		return "OTHER"
	}
	return "STOP"
}

func ParseGeminiFinish(value string) (protocol.FinishReason, bool) {
	switch value {
	case "STOP", "FINISH_REASON_UNSPECIFIED":
		return protocol.FinishStop, true
	case "MAX_TOKENS":
		return protocol.FinishLength, true
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "LANGUAGE", "SPII", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION":
		return protocol.FinishContentFilter, true
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL", "TOO_MANY_TOOL_CALLS", "OTHER":
		return protocol.FinishError, true
	}
	return protocol.FinishStop, false
}
