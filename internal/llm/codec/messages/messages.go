// Package messages implements the Anthropic Messages wire protocol.
package messages

import "github.com/yv1ing/SafeAgent/internal/llm/model"

type Codec struct{}

func New() Codec { return Codec{} }

func (Codec) Format() model.Format { return model.Messages }

func (Codec) BaseCapability() model.Capability {
	return model.Capability{
		Format: model.Messages,
		Media: model.MediaCapability{
			ImageURL: true, ImageData: true, ImageFileID: true,
			DocumentURL: true, DocumentData: true, DocumentFileID: true,
			DocumentMIME:    []string{"application/pdf"},
			ToolResultMedia: true,
		},
		Tools: model.ToolCapability{
			ServerCapabilities: []string{"web_search", "code_execution"},
			Strict:             true,
			Parallel:           true,
		},
		Reasoning:     model.ReasoningCapability{Full: true, Redacted: true, Signatures: true},
		CacheControl:  true,
		TopK:          true,
		StopSequences: true,
		StreamUsage:   true,
		MultiChoice:   false,
		NativeCount:   true,
		ToolArgStream: true,
	}
}

var requestKeys = []string{
	"model", "system", "messages", "stream", "max_tokens", "temperature", "top_p", "top_k",
	"stop_sequences", "tools", "tool_choice", "thinking.type", "thinking.budget_tokens",
	"thinking.display", "thinking.between_tools", "output_config.effort", "output_config.format",
	"metadata.user_id", "service_tier",
}

var messageKeys = []string{"role", "content", "cache_control"}

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
