// Package responses implements the OpenAI Responses wire protocol.
package responses

import "github.com/yv1ing/SafeAgent/internal/protocol"

type Codec struct{}

func New() Codec { return Codec{} }

func (Codec) Format() protocol.Format { return protocol.Responses }

func (Codec) Capability() protocol.Capability {
	return protocol.Capability{
		Format: protocol.Responses,
		Media: protocol.MediaCapability{
			ImageURL: true, ImageData: true, ImageFileID: true,
			DocumentURL: true, DocumentData: true, DocumentFileID: true,
		},
		Tools: protocol.ToolCapability{
			Custom: true, CustomGrammar: true, Strict: true, Parallel: true,
			ServerCapabilities: []string{"web_search", "code_execution"},
		},
		Reasoning:     protocol.ReasoningCapability{Full: true, Summary: true, Signatures: true},
		Citations:     true,
		StopSequences: false,
		StreamUsage:   true,
	}
}

var requestKeys = []string{
	"model", "previous_response_id", "stream", "instructions", "input", "max_output_tokens",
	"temperature", "top_p", "parallel_tool_calls", "tools", "tool_choice", "reasoning.effort",
	"reasoning.summary", "text.format", "text.verbosity", "user", "metadata", "service_tier",
	"prompt_cache_key", "store", "include", "truncation", "max_tool_calls", "safety_identifier",
	"prompt_cache_retention", "logprobs", "top_logprobs", "logit_bias", "modalities", "background",
	"conversation", "frequency_penalty", "presence_penalty", "seed",
}

var itemKeys = []string{
	"type", "id", "role", "content", "status", "call_id", "name", "arguments", "input", "output",
	"summary", "encrypted_content",
}

func errorType(kind string) string {
	switch kind {
	case "invalid_request", "unsupported", "unauthorized":
		return "invalid_request_error"
	case "rate_limited":
		return "rate_limit_error"
	case "upstream_failure":
		return "server_error"
	}
	return "server_error"
}
