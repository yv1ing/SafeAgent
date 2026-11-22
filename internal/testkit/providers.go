package testkit

import (
	"github.com/yv1ing/SafeAgent/internal/llm/codec"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/chat"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/gemini"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/messages"
	"github.com/yv1ing/SafeAgent/internal/llm/codec/responses"
	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
)

// ProviderRegistry is a one-format provider pointing at a scripted upstream. Tests use it instead
// of the built-in catalog so a failing attempt cannot fall over to a real provider.
func ProviderRegistry(format, baseURL string) *provider.Registry {
	return ProviderRegistryWithTransport(format, baseURL, provider.TransportAuto)
}

// ProviderRegistryWithTransport pins the endpoint transport, so a test can force a JSON reply for a
// client that asked to stream.
func ProviderRegistryWithTransport(format, baseURL string, transport provider.TransportMode) *provider.Registry {
	kind := model.Format(format)
	endpoint := provider.EndpointSpec{
		Format: kind, BaseURL: baseURL, Auth: provider.AuthBearer, Transport: transport,
		Capability: capabilityOf(kind), Compat: model.Compat{},
	}
	switch kind {
	case model.Chat:
		endpoint.Path = "/chat/completions"
		endpoint.Auxiliary = []provider.AuxiliarySpec{
			{Kind: provider.AuxiliaryEmbeddings, Path: "/embeddings"},
			{Kind: provider.AuxiliaryModerations, Path: "/moderations"},
		}
	case model.Responses:
		endpoint.Path = "/responses"
		endpoint.CountTokensPath = "/responses/input_tokens"
	case model.Messages:
		endpoint.Path = "/messages"
		endpoint.CountTokensPath = "/count_tokens"
		endpoint.Auth = provider.AuthAPIKey
		endpoint.StaticHeaders = map[string]string{"anthropic-version": "2023-06-01"}
		endpoint.Compat = model.Compat{RequireMaxTokens: true}
	case model.Gemini:
		endpoint.Path = "/models/{model}:{action}"
		endpoint.CountTokensPath = "/models/{model}:countTokens"
		endpoint.Auth = provider.AuthGoogle
		endpoint.Compat = model.Compat{}
	}
	spec := provider.ProviderSpec{ID: "test", Label: "Test", Endpoints: map[model.Format]provider.EndpointSpec{kind: endpoint}}
	return provider.NewRegistry(spec)
}

func capabilityOf(kind model.Format) model.Capability {
	switch kind {
	case model.Chat:
		return chat.New().BaseCapability()
	case model.Responses:
		return responses.New().BaseCapability()
	case model.Messages:
		return messages.New().BaseCapability()
	case model.Gemini:
		return gemini.New().BaseCapability()
	}
	return codec.Codec(chat.New()).BaseCapability()
}
