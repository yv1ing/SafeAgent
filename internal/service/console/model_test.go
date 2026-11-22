package console

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/llm/provider/builtin"
)

func TestModelFormatsMustSuitTheKind(t *testing.T) {
	registry := provider.NewRegistry(builtin.All()...)
	base := ModelInput{ProviderID: "openai", UpstreamName: "text-embedding-3-small", Kind: "embedding"}
	base.Formats = []string{"openai_chat"}
	if _, err := base.normalize(registry); err != nil {
		t.Fatalf("Chat supports embeddings: %v", err)
	}
	base.Formats = []string{"openai_responses"}
	if _, err := base.normalize(registry); err == nil {
		t.Fatal("Responses has no embeddings endpoint and must be rejected")
	}
	base.Kind = "moderation"
	base.Formats = []string{"openai_chat"}
	if _, err := base.normalize(registry); err != nil {
		t.Fatalf("Chat supports moderations: %v", err)
	}
}
