// Package gemini implements the Google Gemini wire protocol.
package gemini

import "github.com/yv1ing/SafeAgent/internal/protocol"

type Codec struct{}

func New() Codec { return Codec{} }

func (Codec) Format() protocol.Format { return protocol.Gemini }

func (Codec) Capability() protocol.Capability {
	return protocol.Capability{
		Format: protocol.Gemini,
		Media: protocol.MediaCapability{
			ImageURL: true, ImageData: true,
			DocumentURL: true, DocumentData: true,
			AudioInput: true, VideoInput: true,
			ToolResultMedia: true,
		},
		Tools: protocol.ToolCapability{
			ServerCapabilities: []string{"web_search", "code_execution"},
			Strict:             false,
			Parallel:           false,
		},
		Reasoning:     protocol.ReasoningCapability{Full: true, Signatures: true},
		TopK:          true,
		StopSequences: true,
		StreamUsage:   true,
	}
}

var requestKeys = []string{
	"contents", "systemInstruction.parts", "tools", "toolConfig.functionCallingConfig",
	"generationConfig.maxOutputTokens", "generationConfig.temperature", "generationConfig.topP",
	"generationConfig.topK", "generationConfig.stopSequences", "generationConfig.seed",
	"generationConfig.frequencyPenalty", "generationConfig.presencePenalty",
	"generationConfig.responseMimeType", "generationConfig.responseJsonSchema",
	"generationConfig.responseSchema", "generationConfig.thinkingConfig.thinkingLevel",
	"generationConfig.thinkingConfig.thinkingBudget", "generationConfig.thinkingConfig.includeThoughts",
	"generationConfig.candidateCount",
}

var partKeys = []string{"text", "thought", "thoughtSignature", "functionCall.name", "functionCall.args", "functionCall.id", "functionResponse.name", "functionResponse.response", "functionResponse.id", "inlineData", "fileData"}

func errorStatus(kind string) string {
	switch kind {
	case "invalid_request":
		return "INVALID_ARGUMENT"
	case "unauthorized":
		return "UNAUTHENTICATED"
	case "blocked":
		return "PERMISSION_DENIED"
	case "rate_limited":
		return "RESOURCE_EXHAUSTED"
	case "timeout":
		return "DEADLINE_EXCEEDED"
	}
	return "INTERNAL"
}
