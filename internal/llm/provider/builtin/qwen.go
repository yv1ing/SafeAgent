package builtin

import (
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
)

func Qwen() provider.ProviderSpec {
	chatCapability := model.Capability{
		Format:        model.Chat,
		Media:         model.MediaCapability{ImageURL: true, ImageData: true, VideoInput: true},
		Tools:         model.ToolCapability{Parallel: true},
		Reasoning:     model.ReasoningCapability{Full: true},
		StopSequences: true,
		StreamUsage:   true,
		Logprobs:      true,
		MultiChoice:   true,
		ToolArgStream: true,
	}
	responsesCapability := chatCapability
	responsesCapability.Format = model.Responses
	responsesCapability.MultiChoice = false

	messagesCapability := model.Capability{
		Format:        model.Messages,
		Media:         model.MediaCapability{ImageURL: true, ImageData: true},
		Tools:         model.ToolCapability{Parallel: true},
		Reasoning:     model.ReasoningCapability{Full: true},
		TopK:          true,
		StopSequences: true,
		StreamUsage:   true,
		NativeCount:   true,
		ToolArgStream: true,
	}

	return provider.ProviderSpec{
		ID:    "qwen",
		Label: "Qwen",
		Endpoints: map[model.Format]provider.EndpointSpec{
			model.Chat: {
				Format:     model.Chat,
				BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
				Path:       "/chat/completions",
				Auth:       provider.AuthBearer,
				Capability: chatCapability,
				Compat:     model.Compat{HTTP200FailedStatus: true},
				Discovery:  provider.DiscoverySpec{Path: "/models", Shape: provider.DiscoveryOpenAI},
				Auxiliary:  []provider.AuxiliarySpec{{Kind: provider.AuxiliaryEmbeddings, Path: "/embeddings"}},
				Transport:  provider.TransportAuto,
			},
			model.Responses: {
				Format:     model.Responses,
				BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
				Path:       "/responses",
				Auth:       provider.AuthBearer,
				Capability: responsesCapability,
				Compat:     model.Compat{HTTP200FailedStatus: true},
				Discovery:  provider.DiscoverySpec{Path: "/models", Shape: provider.DiscoveryOpenAI},
				Transport:  provider.TransportAuto,
			},
			model.Messages: {
				Format:          model.Messages,
				BaseURL:         "https://dashscope.aliyuncs.com/apps/anthropic",
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
