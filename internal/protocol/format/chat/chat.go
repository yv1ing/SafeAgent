// Package chat implements the OpenAI Chat Completions wire protocol.
package chat

import (
	"github.com/yv1ing/SafeAgent/internal/protocol"
)

type Codec struct{}

func New() Codec { return Codec{} }

func (Codec) Format() protocol.Format { return protocol.Chat }

func (Codec) Capability() protocol.Capability {
	return protocol.Capability{
		Format: protocol.Chat,
		Media: protocol.MediaCapability{
			ImageURL: true, ImageData: true,
			DocumentData: true, DocumentFileID: true,
			DocumentMIME: []string{"application/pdf", "text/plain", "text/markdown", "application/json"},
			AudioInput:   true,
		},
		Tools: protocol.ToolCapability{
			Custom: true, CustomGrammar: true, Strict: true, Parallel: true,
		},
		Reasoning:     protocol.ReasoningCapability{Full: true},
		Citations:     true,
		StopSequences: true,
		StreamUsage:   true,
	}
}

var requestKeys = []string{
	"model", "messages", "stream", "stream_options", "n", "max_completion_tokens", "max_tokens",
	"temperature", "top_p", "top_k", "stop", "tools", "tool_choice", "parallel_tool_calls",
	"reasoning_effort", "metadata", "user", "service_tier", "prompt_cache_key", "seed",
	"frequency_penalty", "presence_penalty", "response_format", "logprobs", "top_logprobs", "logit_bias",
	"modalities", "prediction", "audio", "store", "previous_response_id", "background", "conversation", "safety_identifier",
}

var messageKeys = []string{
	"role", "content", "reasoning_content", "reasoning", "refusal", "tool_calls", "tool_call_id",
	"function_call", "name", "annotations", "audio", "cache_control",
}

func errorType(kind string) string {
	switch kind {
	case "invalid_request", "unsupported", "unauthorized":
		return "invalid_request_error"
	case "rate_limited":
		return "rate_limit_error"
	case "upstream_failure":
		return "api_error"
	case "timeout":
		return "timeout_error"
	case "blocked":
		return "permission_error"
	}
	return "server_error"
}
