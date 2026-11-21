package builtin

import (
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
)

func Google() provider.ProviderSpec {
	return provider.ProviderSpec{
		ID:    "google",
		Label: "Google",
		Endpoints: map[model.Format]provider.EndpointSpec{
			model.Gemini: {
				Format:          model.Gemini,
				BaseURL:         "https://generativelanguage.googleapis.com",
				Path:            "/v1beta/models/{model}:{action}",
				CountTokensPath: "/v1beta/models/{model}:countTokens",
				Auth:            provider.AuthGoogle,
				Capability: model.Capability{
					Format: model.Gemini,
					Media: model.MediaCapability{
						ImageURL: true, ImageData: true,
						DocumentURL: true, DocumentData: true,
						DocumentMIME:    []string{"application/pdf", "text/plain", "text/csv", "text/html", "text/x-python"},
						AudioInput:      true,
						VideoInput:      true,
						ToolResultMedia: true,
					},
					Tools: model.ToolCapability{
						Parallel:           true,
						ServerCapabilities: []string{"web_search", "code_execution", "urlContext"},
					},
					Reasoning:     model.ReasoningCapability{Full: true, Summary: true, Signatures: true},
					Citations:     true,
					TopK:          true,
					StopSequences: true,
					StreamUsage:   true,
					MultiChoice:   true,
					NativeCount:   true,
					ToolArgStream: false,
				},
				Compat:    model.Compat{},
				Discovery: provider.DiscoverySpec{Path: "/v1beta/models", Shape: provider.DiscoveryGemini},
				Transport: provider.TransportAuto,
			},
		},
	}
}
