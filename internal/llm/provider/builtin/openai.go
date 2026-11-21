package builtin

import (
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
)

func OpenAI() provider.ProviderSpec {
	return provider.ProviderSpec{
		ID:    "openai",
		Label: "OpenAI",
		Endpoints: map[model.Format]provider.EndpointSpec{
			model.Chat: {
				Format:  model.Chat,
				BaseURL: "https://api.openai.com",
				Path:    "/v1/chat/completions",
				Auth:    provider.AuthBearer,
				Capability: model.Capability{
					Format: model.Chat,
					Media: model.MediaCapability{
						ImageURL: true, ImageData: true,
						DocumentData: true, DocumentFileID: true,
						DocumentMIME: []string{"application/pdf"},
						AudioInput:   true,
					},
					Tools: model.ToolCapability{
						Strict: true, Parallel: true,
					},
					Citations:     false,
					CacheControl:  false,
					TopK:          false,
					StopSequences: true,
					StreamUsage:   true,
					Logprobs:      true,
					Refusal:       true,
					MultiChoice:   true,
					NativeCount:   false,
					ToolArgStream: true,
				},
				Discovery: provider.DiscoverySpec{Path: "/v1/models", Shape: provider.DiscoveryOpenAI},
				Auxiliary: []provider.AuxiliarySpec{
					{Kind: provider.AuxiliaryEmbeddings, Path: "/v1/embeddings"},
					{Kind: provider.AuxiliaryModerations, Path: "/v1/moderations"},
				},
				Transport: provider.TransportAuto,
			},
			model.Responses: {
				Format:          model.Responses,
				BaseURL:         "https://api.openai.com",
				Path:            "/v1/responses",
				CountTokensPath: "/v1/responses/input_tokens",
				Auth:            provider.AuthBearer,
				Capability: model.Capability{
					Format: model.Responses,
					Media: model.MediaCapability{
						ImageURL: true, ImageData: true,
						DocumentData: true, DocumentFileID: true,
						DocumentMIME: []string{"application/pdf"},
						AudioInput:   true,
					},
					Tools: model.ToolCapability{
						Custom: true, CustomGrammar: true, Strict: true, Parallel: true,
						ServerCapabilities: []string{"web_search", "code_execution", "file_search", "image_generation", "mcp"},
					},
					Reasoning:     model.ReasoningCapability{Full: true, Summary: true, Signatures: true},
					Citations:     true,
					StopSequences: false,
					StreamUsage:   true,
					Refusal:       true,
					MultiChoice:   false,
					NativeCount:   true,
					ToolArgStream: true,
				},
				Compat:    model.Compat{},
				Discovery: provider.DiscoverySpec{Path: "/v1/models", Shape: provider.DiscoveryOpenAI},
				Transport: provider.TransportAuto,
			},
		},
	}
}
