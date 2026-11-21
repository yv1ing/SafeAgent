package builtin

import (
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
)

func ZAI() provider.ProviderSpec {
	chatCapability := model.Capability{
		Format: model.Chat,
		Media:  model.MediaCapability{ImageURL: true, ImageData: true},
		Tools: model.ToolCapability{
			Parallel:           true,
			ServerCapabilities: []string{"web_search"},
		},
		Reasoning:     model.ReasoningCapability{Full: true},
		Citations:     true,
		StopSequences: true,
		StreamUsage:   true,
		ToolArgStream: true,
	}
	responsesCapability := chatCapability
	responsesCapability.Format = model.Responses
	responsesCapability.Citations = false

	messagesCapability := model.Capability{
		Format:        model.Messages,
		Tools:         model.ToolCapability{Parallel: true},
		Reasoning:     model.ReasoningCapability{Full: true},
		TopK:          true,
		StopSequences: true,
		StreamUsage:   true,
		NativeCount:   true,
		ToolArgStream: true,
	}

	return provider.ProviderSpec{
		ID:    "zai",
		Label: "Z.AI",
		Endpoints: map[model.Format]provider.EndpointSpec{
			model.Chat: {
				Format:     model.Chat,
				BaseURL:    "https://api.z.ai/api/paas/v4",
				Path:       "/chat/completions",
				Auth:       provider.AuthBearer,
				Capability: chatCapability,
				Auxiliary:  []provider.AuxiliarySpec{{Kind: provider.AuxiliaryEmbeddings, Path: "/embeddings"}},
				Transport:  provider.TransportAuto,
			},
			model.Responses: {
				Format:     model.Responses,
				BaseURL:    "https://api.z.ai/api/v1",
				Path:       "/responses",
				Auth:       provider.AuthBearer,
				Capability: responsesCapability,
				Transport:  provider.TransportAuto,
			},
			model.Messages: {
				Format:          model.Messages,
				BaseURL:         "https://api.z.ai/api/anthropic",
				Path:            "/v1/messages",
				CountTokensPath: "/v1/messages/count_tokens",
				Auth:            provider.AuthAPIKey,
				StaticHeaders:   map[string]string{"anthropic-version": "2023-06-01"},
				Capability:      messagesCapability,
				Compat:          model.Compat{RequireMaxTokens: true, DefaultMaxOutput: 8192},
				Transport:       provider.TransportAuto,
			},
		},
	}
}
