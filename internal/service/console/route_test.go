package console

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func TestRouteTargetsMustNotMixKindsOrExceedLimits(t *testing.T) {
	channel := entity.Channel{
		ID: "channel", ProviderID: "test", Enabled: true,
		Endpoints: []entity.ChannelEndpoint{{Format: "openai_chat"}},
		ModelIDs:  []string{"generation", "embedding"},
	}
	models := []entity.Model{
		{ID: "generation", ProviderID: "test", Kind: entity.ModelGeneration, Formats: []string{"openai_chat"}, Enabled: true},
		{ID: "embedding", ProviderID: "test", Kind: entity.ModelEmbedding, Formats: []string{"openai_chat"}, Enabled: true},
	}
	channels := []entity.Channel{channel}
	targets := []entity.Target{
		{ChannelID: "channel", ModelID: "generation"},
		{ChannelID: "channel", ModelID: "embedding"},
	}
	if err := validateTargets(targets, channels, models); !isKind(err, KindInvalid) {
		t.Fatalf("a route mixing model kinds must be rejected: %v", err)
	}
	targets = targets[:1]
	if err := validateTargets(targets, channels, models); err != nil {
		t.Fatalf("a homogeneous route must be accepted: %v", err)
	}
	targets[0].MaxOutputTokens = MaximumOutputTokens + 1
	if err := validateTargets(targets, channels, models); !isKind(err, KindInvalid) {
		t.Fatalf("an out-of-range output limit must be rejected: %v", err)
	}
}
