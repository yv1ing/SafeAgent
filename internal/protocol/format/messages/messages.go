// Package messages implements the Anthropic Messages wire protocol.
package messages

import "github.com/yv1ing/SafeAgent/internal/protocol"

type Codec struct{}

func New() Codec { return Codec{} }

func (Codec) Format() protocol.Format { return protocol.Messages }

func (Codec) Capability() protocol.Capability {
	return protocol.Capability{
		Format: protocol.Messages,
		Media: protocol.MediaCapability{
			ImageURL: true, ImageData: true, ImageFileID: true,
			DocumentURL: true, DocumentData: true, DocumentFileID: true,
			DocumentMIME:    []string{"application/pdf"},
			ToolResultMedia: true,
		},
		Tools: protocol.ToolCapability{
			ServerCapabilities: []string{"web_search", "code_execution"},
			Strict:             true,
			Parallel:           true,
			CacheControl:       true,
		},
		Reasoning:     protocol.ReasoningCapability{Full: true, Redacted: true, Signatures: true},
		CacheControl:  true,
		TopK:          true,
		StopSequences: true,
		StreamUsage:   true,
	}
}

var requestKeys = []string{
	"model", "system", "messages", "stream", "max_tokens", "temperature", "top_p", "top_k",
	"stop_sequences", "tools", "tool_choice", "thinking.type", "thinking.budget_tokens",
	"thinking.display", "thinking.between_tools", "output_config.effort", "output_config.format",
	"metadata.user_id", "service_tier",
}

var messageKeys = []string{"role", "content", "cache_control"}

var contentKeys = []string{
	"type", "text", "citations", "cache_control", "source", "id", "name", "input", "tool_use_id",
	"content", "is_error", "thinking", "signature", "data",
}

func errorType(kind string) string {
	switch kind {
	case "invalid_request":
		return "invalid_request_error"
	case "unauthorized":
		return "authentication_error"
	case "rate_limited":
		return "rate_limit_error"
	case "timeout":
		return "timeout_error"
	case "blocked":
		return "permission_error"
	}
	return "api_error"
}
