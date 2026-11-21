package builtin

import (
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
)

func Anthropic() provider.ProviderSpec {
	return provider.ProviderSpec{
		ID:    "anthropic",
		Label: "Anthropic",
		Endpoints: map[model.Format]provider.EndpointSpec{
			model.Messages: {
				Format:          model.Messages,
				BaseURL:         "https://api.anthropic.com",
				Path:            "/v1/messages",
				CountTokensPath: "/v1/messages/count_tokens",
				Auth:            provider.AuthAPIKey,
				StaticHeaders:   map[string]string{"anthropic-version": "2023-06-01"},
				Capability: model.Capability{
					Format: model.Messages,
					Media: model.MediaCapability{
						ImageURL: true, ImageData: true,
						DocumentURL: true, DocumentData: true,
						DocumentMIME:    []string{"application/pdf", "text/plain"},
						ToolResultMedia: true,
					},
					Tools: model.ToolCapability{
						Strict: true, Parallel: true,
						ServerCapabilities: []string{"web_search", "code_execution"},
					},
					Reasoning:     model.ReasoningCapability{Full: true, Summary: true, Redacted: true, Signatures: true},
					Citations:     true,
					CacheControl:  true,
					TopK:          true,
					StopSequences: true,
					StreamUsage:   true,
					Refusal:       false,
					MultiChoice:   false,
					NativeCount:   true,
					ToolArgStream: true,
				},
				Compat:    model.Compat{RequireMaxTokens: true, DefaultMaxOutput: 8192},
				Discovery: provider.DiscoverySpec{Path: "/v1/models", Shape: provider.DiscoveryAnthropic},
				Transport: provider.TransportAuto,
			},
		},
	}
}
