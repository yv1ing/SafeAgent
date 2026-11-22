package console

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/llm/provider/builtin"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func TestChannelFormatsAreExplicitOrderedAndSupported(t *testing.T) {
	registry := provider.NewRegistry(builtin.All()...)
	base := ChannelInput{Name: "channel", ProviderID: "openai"}

	valid := base
	valid.Endpoints = []entity.ChannelEndpoint{{Format: "openai_chat"}, {Format: "openai_responses"}}
	normalized, err := valid.normalize(registry)
	if err != nil || len(normalized.Endpoints) != 2 || normalized.Endpoints[1].Format != "openai_responses" {
		t.Fatalf("supported formats must be accepted in order: %+v %v", normalized, err)
	}

	cases := map[string][]entity.ChannelEndpoint{
		"missing":     nil,
		"unsupported": {{Format: "google_gemini"}},
		"duplicate":   {{Format: "openai_chat"}, {Format: "openai_chat"}},
	}
	for name, endpoints := range cases {
		input := base
		input.Endpoints = endpoints
		if _, err := input.normalize(registry); !isKind(err, KindInvalid) {
			t.Fatalf("%s endpoints must be rejected: %v", name, err)
		}
	}

	custom := ChannelInput{Name: "custom", ProviderID: "custom", Endpoints: []entity.ChannelEndpoint{{Format: "openai_chat"}}}
	if _, err := custom.normalize(registry); !isKind(err, KindInvalid) {
		t.Fatalf("a custom channel needs a base URL: %v", err)
	}
	custom.Endpoints[0].BaseURL = "https://gateway.example.com"
	if _, err := custom.normalize(registry); err != nil {
		t.Fatalf("a custom channel with a base URL must be accepted: %v", err)
	}
	for _, invalid := range []string{"https://gateway.example.com?key=1", "https://user:pass@gateway.example.com"} {
		custom.Endpoints[0].BaseURL = invalid
		if _, err := custom.normalize(registry); !isKind(err, KindInvalid) {
			t.Fatalf("base URL %q must be rejected: %v", invalid, err)
		}
	}
}

func TestChannelRejectsDuplicateModelIDs(t *testing.T) {
	registry := provider.NewRegistry(builtin.All()...)
	input := ChannelInput{
		Name: "channel", ProviderID: "openai", ModelIDs: []string{"one", "one"},
		Endpoints: []entity.ChannelEndpoint{{Format: "openai_chat"}},
	}
	if _, err := input.normalize(registry); !isKind(err, KindInvalid) {
		t.Fatalf("duplicate model ids must be rejected: %v", err)
	}
}

func TestChannelModelsMustShareAFormat(t *testing.T) {
	models := []entity.Model{
		{ID: "messages-only", ProviderID: "custom", Kind: entity.ModelGeneration, Formats: []string{"anthropic_messages"}, Enabled: true},
	}
	input := ChannelInput{
		ProviderID: "custom",
		Endpoints:  []entity.ChannelEndpoint{{Format: "openai_chat", BaseURL: "https://example.com"}},
		ModelIDs:   []string{"messages-only"},
	}
	if err := validateChannelModels(input, models); !isKind(err, KindInvalid) {
		t.Fatalf("a channel and model without a shared format must be rejected: %v", err)
	}
}
