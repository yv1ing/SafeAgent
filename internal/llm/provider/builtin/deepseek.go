package builtin

import (
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
)

func DeepSeek() provider.ProviderSpec {
	textOnly := model.MediaCapability{}
	reasoning := model.ReasoningCapability{Full: true}
	tools := model.ToolCapability{Parallel: true}
	common := model.Capability{
		Media:         textOnly,
		Tools:         tools,
		Reasoning:     reasoning,
		CacheControl:  false,
		TopK:          false,
		StopSequences: true,
		StreamUsage:   true,
		MultiChoice:   false,
		ToolArgStream: true,
	}
	chat := common
	chat.Format = model.Chat
	chat.Logprobs = true

	responses := common
	responses.Format = model.Responses

	messages := common
	messages.Format = model.Messages
	messages.NativeCount = true
	messages.Tools.ServerCapabilities = []string{"deepseek_web_search", "deepseek_read_url"}

	return provider.ProviderSpec{
		ID:    "deepseek",
		Label: "DeepSeek",
		Endpoints: map[model.Format]provider.EndpointSpec{
			model.Chat: {
				Format:     model.Chat,
				BaseURL:    "https://api.deepseek.com",
				Path:       "/chat/completions",
				Auth:       provider.AuthBearer,
				Capability: chat,
				Discovery:  provider.DiscoverySpec{Path: "/models", Shape: provider.DiscoveryOpenAI},
				Transport:  provider.TransportAuto,
			},
			model.Responses: {
				Format:     model.Responses,
				BaseURL:    "https://api.deepseek.com",
				Path:       "/responses",
				Auth:       provider.AuthBearer,
				Capability: responses,
				Compat:     model.Compat{},
				Discovery:  provider.DiscoverySpec{Path: "/models", Shape: provider.DiscoveryOpenAI},
				Transport:  provider.TransportAuto,
			},
			model.Messages: {
				Format:          model.Messages,
				BaseURL:         "https://api.deepseek.com/anthropic",
				Path:            "/v1/messages",
				CountTokensPath: "/v1/messages/count_tokens",
				Auth:            provider.AuthAPIKey,
				StaticHeaders:   map[string]string{"anthropic-version": "2023-06-01"},
				Capability:      messages,
				Compat: model.Compat{
					RequireMaxTokens:   true,
					IgnoreTopK:         true,
					IgnoreBudgetTokens: true,
					IgnoreServiceTier:  true,
					NoCustomToolType:   true,
					DefaultMaxOutput:   8192,
				},
				Discovery: provider.DiscoverySpec{Path: "/models", Shape: provider.DiscoveryAnthropic},
				Aliases:   []provider.ModelAlias{{Prefix: "claude-", To: "deepseek-chat"}},
				Transport: provider.TransportAuto,
			},
		},
	}
}
