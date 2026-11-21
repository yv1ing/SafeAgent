package builtin

import (
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
)

// Custom is an OpenAI Chat Completions endpoint the operator supplies. It deliberately declares no
// capability beyond the standard text and function-tool baseline, so the gateway never promises a
// feature the endpoint may not have.
func Custom() provider.ProviderSpec {
	return provider.ProviderSpec{
		ID:     "custom",
		Label:  "Custom",
		Custom: true,
		Endpoints: map[model.Format]provider.EndpointSpec{
			model.Chat: {
				Format: model.Chat,
				Path:   "/chat/completions",
				Auth:   provider.AuthBearer,
				Capability: model.Capability{
					Format: model.Chat,
					Media: model.MediaCapability{
						ImageURL: true, ImageData: true,
					},
					Tools:         model.ToolCapability{Strict: true, Parallel: true},
					StopSequences: true,
					StreamUsage:   true,
					Refusal:       true,
					MultiChoice:   true,
					ToolArgStream: true,
				},
				Discovery: provider.DiscoverySpec{Path: "/models", Shape: provider.DiscoveryOpenAI},
				Transport: provider.TransportAuto,
			},
		},
	}
}
