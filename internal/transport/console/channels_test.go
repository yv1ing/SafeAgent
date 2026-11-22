package console

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func TestChannelViewCarriesForwardingAndEndpointFields(t *testing.T) {
	view := channelView(&entity.Channel{
		Endpoints:              []entity.ChannelEndpoint{{Format: "openai_chat"}, {Format: "openai_responses"}},
		ModelIDs:               []string{"model_1"},
		ForwardHeaders:         []string{"x-provider-feature"},
		DefaultMaxOutputTokens: 1234,
	})
	if len(view.ForwardHeaders) != 1 || view.ForwardHeaders[0] != "x-provider-feature" {
		t.Fatalf("forward headers missing from the view: %+v", view)
	}
	if view.DefaultMaxOutputTokens != 1234 {
		t.Fatalf("default output limit missing from the view: %+v", view)
	}
	if len(view.Endpoints) != 2 || view.Endpoints[1].Format != "openai_responses" {
		t.Fatalf("channel endpoints missing from the view: %+v", view)
	}
	if len(view.ModelIDs) != 1 || view.ModelIDs[0] != "model_1" {
		t.Fatalf("channel model ids missing from the view: %+v", view)
	}
}
