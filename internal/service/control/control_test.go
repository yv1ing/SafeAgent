package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/llm/provider/builtin"
)

// TestReloadFailureIsLoggedNotReturned pins the post-mutation reload contract: the catalog read is
// best effort, because the mutation it follows is already committed.
func TestReloadFailureIsLoggedNotReturned(t *testing.T) {
	registry := provider.NewRegistry(provider.ProviderSpec{ID: "openai", Label: "OpenAI"})
	var logged bytes.Buffer
	service := &Service{
		providers: registry,
		logger:    slog.New(slog.NewTextHandler(&logged, nil)),
		loadCatalog: func(context.Context) ([]provider.ProviderSpec, error) {
			return nil, errors.New("an unrelated provider row is unreadable")
		},
	}
	service.reloadProviders(context.Background())
	if !strings.Contains(logged.String(), "provider catalog could not be reloaded") {
		t.Fatalf("the reload failure was not logged: %q", logged.String())
	}
	if _, found := registry.Get("openai"); !found {
		t.Fatal("a failed reload must keep the live catalog")
	}
}

// TestConnectHonoursTheEnabledInput pins the rows the connect flow stores: both carry the enabled
// flag of the request instead of a hard-coded value.
func TestConnectHonoursTheEnabledInput(t *testing.T) {
	endpoints := json.RawMessage(`{"openai_chat":{"BaseURL":"https://upstream.example.test"}}`)
	for _, enabled := range []bool{false, true} {
		normalized, err := normalizeProvider(ProviderInput{Label: "custom", Endpoints: endpoints, Enabled: enabled})
		if err != nil {
			t.Fatalf("normalize provider (enabled=%v): %v", enabled, err)
		}
		if stored := providerEntity(normalized); stored.Enabled != enabled {
			t.Fatalf("connect provider enabled = %v, want %v", stored.Enabled, enabled)
		}
		channel, err := normalizeChannel(ChannelInput{
			Name: "main", ProviderID: "custom", BaseURL: "https://upstream.example.test", Enabled: enabled,
		}, provider.ProviderSpec{})
		if err != nil {
			t.Fatalf("normalize channel (enabled=%v): %v", enabled, err)
		}
		if stored := channelEntity(channel); stored.Enabled != enabled {
			t.Fatalf("connect channel enabled = %v, want %v", stored.Enabled, enabled)
		}
	}
}

func builtinEndpoints(t *testing.T, id string) json.RawMessage {
	t.Helper()
	for _, spec := range builtin.All() {
		if spec.ID != id {
			continue
		}
		endpoints, err := provider.MarshalEndpoints(spec)
		if err != nil {
			t.Fatalf("marshal endpoints: %v", err)
		}
		return endpoints
	}
	t.Fatalf("provider %s is not builtin", id)
	return nil
}

func TestProviderFormatsFollowTheEndpointCatalog(t *testing.T) {
	formats := providerFormats(builtinEndpoints(t, "openai"))
	if !slices.Equal(formats, []string{string(model.Chat), string(model.Responses)}) {
		t.Fatalf("provider formats = %v", formats)
	}
	if formats := providerFormats(json.RawMessage(`not json`)); len(formats) != 0 {
		t.Fatalf("an unreadable catalog has no formats: %v", formats)
	}
	if formats := providerFormats(nil); len(formats) != 0 {
		t.Fatalf("an empty catalog has no formats: %v", formats)
	}
}
