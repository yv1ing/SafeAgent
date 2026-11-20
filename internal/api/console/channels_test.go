package console

import (
	"testing"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func TestChannelViewCarriesForwardingFields(t *testing.T) {
	view := channelView(&entity.Channel{
		ForwardHeaders:         []string{"x-provider-feature"},
		DefaultMaxOutputTokens: 1234,
	})
	if len(view.ForwardHeaders) != 1 || view.ForwardHeaders[0] != "x-provider-feature" {
		t.Fatalf("forward headers missing from the view: %+v", view)
	}
	if view.DefaultMaxOutputTokens != 1234 {
		t.Fatalf("default output limit missing from the view: %+v", view)
	}
}
