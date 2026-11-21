package provider_test

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/llm/provider/builtin"
)

func registry() *provider.Registry { return provider.NewRegistry(builtin.All()...) }

func endpoint(t *testing.T, id string, format model.Format) provider.EndpointSpec {
	t.Helper()
	spec, found := registry().Get(id)
	if !found {
		t.Fatalf("provider %q is not registered", id)
	}
	foundEndpoint, ok := spec.Endpoint(format)
	if !ok {
		t.Fatalf("provider %q has no %s endpoint", id, format)
	}
	return foundEndpoint
}

func TestCatalogURLs(t *testing.T) {
	cases := []struct {
		provider string
		format   model.Format
		url      string
	}{
		{"anthropic", model.Messages, "https://api.anthropic.com/v1/messages"},
		{"openai", model.Chat, "https://api.openai.com/v1/chat/completions"},
		{"openai", model.Responses, "https://api.openai.com/v1/responses"},
		{"google", model.Gemini, "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:generateContent"},
		{"deepseek", model.Chat, "https://api.deepseek.com/chat/completions"},
		{"deepseek", model.Messages, "https://api.deepseek.com/anthropic/v1/messages"},
		{"zai", model.Chat, "https://api.z.ai/api/paas/v4/chat/completions"},
		{"zai", model.Responses, "https://api.z.ai/api/v1/responses"},
		{"qwen", model.Responses, "https://dashscope.aliyuncs.com/compatible-mode/v1/responses"},
		{"qwen", model.Messages, "https://dashscope.aliyuncs.com/apps/anthropic/v1/messages"},
	}
	for _, testCase := range cases {
		got := endpoint(t, testCase.provider, testCase.format).URL("", "gemini-pro", false)
		if got != testCase.url {
			t.Fatalf("%s/%s: want %s, got %s", testCase.provider, testCase.format, testCase.url, got)
		}
	}
}

func TestCatalogFormats(t *testing.T) {
	cases := map[string][]model.Format{
		"anthropic": {model.Messages},
		"openai":    {model.Chat, model.Responses},
		"google":    {model.Gemini},
		"deepseek":  {model.Chat, model.Responses, model.Messages},
		"zai":       {model.Chat, model.Responses, model.Messages},
		"qwen":      {model.Chat, model.Responses, model.Messages},
		"custom":    {model.Chat},
	}
	for id, want := range cases {
		spec, found := registry().Get(id)
		if !found {
			t.Fatalf("%s missing", id)
		}
		formats := spec.Formats()
		if len(formats) != len(want) {
			t.Fatalf("%s: want %v, got %v", id, want, formats)
		}
		for index := range want {
			if formats[index] != want[index] {
				t.Fatalf("%s: want %v, got %v", id, want, formats)
			}
		}
	}
}

func TestCatalogAuthAndCompatibility(t *testing.T) {
	anthropic := endpoint(t, "anthropic", model.Messages)
	headers := map[string]string{}
	anthropic.ApplyAuth(headers, "secret")
	if headers["x-api-key"] != "secret" || headers["anthropic-version"] != "2023-06-01" {
		t.Fatalf("anthropic auth headers: %v", headers)
	}
	if !anthropic.Compat.RequireMaxTokens {
		t.Fatal("Messages requires an output limit")
	}
	if !anthropic.Capability.Reasoning.Signatures || !anthropic.Capability.NativeCount {
		t.Fatalf("anthropic capability: %+v", anthropic.Capability)
	}

	google := endpoint(t, "google", model.Gemini)
	headers = map[string]string{}
	google.ApplyAuth(headers, "secret")
	if headers["x-goog-api-key"] != "secret" {
		t.Fatalf("google auth headers: %v", headers)
	}
	openai := endpoint(t, "openai", model.Responses)
	if !openai.Capability.Tools.Custom || !openai.Capability.NativeCount {
		t.Fatalf("openai responses capability: %+v", openai.Capability)
	}

	deepseekMessages := endpoint(t, "deepseek", model.Messages)
	if !deepseekMessages.Compat.IgnoreTopK || !deepseekMessages.Compat.NoCustomToolType {
		t.Fatalf("deepseek messages compat: %+v", deepseekMessages.Compat)
	}
	if aliased := deepseekMessages.ApplyAlias("claude-sonnet-4"); aliased != "deepseek-chat" {
		t.Fatalf("alias: %s", aliased)
	}

	qwen := endpoint(t, "qwen", model.Chat)
	if !qwen.Compat.HTTP200FailedStatus {
		t.Fatal("qwen reports failures in a 200 body")
	}
}

func TestAuxiliaryEndpoints(t *testing.T) {
	cases := []struct {
		provider string
		kind     provider.AuxiliaryKind
		format   model.Format
		url      string
	}{
		{"openai", provider.AuxiliaryEmbeddings, model.Chat, "https://api.openai.com/v1/embeddings"},
		{"openai", provider.AuxiliaryModerations, model.Chat, "https://api.openai.com/v1/moderations"},
		{"zai", provider.AuxiliaryEmbeddings, model.Chat, "https://api.z.ai/api/paas/v4/embeddings"},
		{"qwen", provider.AuxiliaryEmbeddings, model.Chat, "https://dashscope.aliyuncs.com/compatible-mode/v1/embeddings"},
	}
	for _, testCase := range cases {
		spec, _ := registry().Get(testCase.provider)
		endpointSpec, found := spec.Endpoint(testCase.format)
		if !found {
			t.Fatalf("%s does not declare %s", testCase.provider, testCase.format)
		}
		var auxiliary provider.AuxiliarySpec
		declared := false
		for _, candidate := range endpointSpec.Auxiliary {
			if candidate.Kind == testCase.kind {
				auxiliary, declared = candidate, true
				break
			}
		}
		if !declared {
			t.Fatalf("%s does not declare %s on %s", testCase.provider, testCase.kind, testCase.format)
		}
		if got := endpointSpec.AuxiliaryURL("", auxiliary); got != testCase.url {
			t.Fatalf("%s %s: want %s, got %s", testCase.provider, testCase.kind, testCase.url, got)
		}
	}
}

func TestCountTokensURLs(t *testing.T) {
	cases := []struct {
		provider string
		format   model.Format
		url      string
	}{
		{"anthropic", model.Messages, "https://api.anthropic.com/v1/messages/count_tokens"},
		{"openai", model.Responses, "https://api.openai.com/v1/responses/input_tokens"},
		{"google", model.Gemini, "https://generativelanguage.googleapis.com/v1beta/models/gemini-pro:countTokens"},
		{"deepseek", model.Messages, "https://api.deepseek.com/anthropic/v1/messages/count_tokens"},
	}
	for _, testCase := range cases {
		got := endpoint(t, testCase.provider, testCase.format).CountTokensURL("", "gemini-pro")
		if got != testCase.url {
			t.Fatalf("%s: want %s, got %s", testCase.provider, testCase.url, got)
		}
	}
}

func TestParseModelsShapes(t *testing.T) {
	openai, err := provider.ParseModels(provider.DiscoveryOpenAI, []byte(`{"data":[{"id":"a"},{"id":"b"}]}`))
	if err != nil || len(openai) != 2 || openai[1] != "b" {
		t.Fatalf("openai shape: %v %v", openai, err)
	}
	gemini, err := provider.ParseModels(provider.DiscoveryGemini, []byte(`{"models":[{"name":"models/gemini-pro"}]}`))
	if err != nil || len(gemini) != 1 || gemini[0] != "gemini-pro" {
		t.Fatalf("gemini shape: %v %v", gemini, err)
	}
}
