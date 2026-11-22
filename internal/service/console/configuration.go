package console

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// ValidateConfiguration refuses to start on a model, channel or route graph that the console
// would not have accepted. It is the startup counterpart of the mutation-time checks.
func ValidateConfiguration(ctx context.Context, store *repository.Store, providers *provider.Registry) error {
	models, err := store.Models(ctx)
	if err != nil {
		return err
	}
	channels, err := store.Channels(ctx)
	if err != nil {
		return err
	}
	routes, err := store.AllRoutes(ctx)
	if err != nil {
		return err
	}
	for _, model := range models {
		if err := validateStoredModel(model, providers); err != nil {
			return err
		}
	}
	for _, channel := range channels {
		if err := validateStoredChannel(channel, models, providers); err != nil {
			return err
		}
	}
	for _, route := range routes {
		if err := validateStoredRoute(route, channels, models); err != nil {
			return err
		}
	}
	return nil
}

func validateStoredModel(model entity.Model, providers *provider.Registry) error {
	if model.ID == "" {
		return fmt.Errorf("a model row has no id")
	}
	input := ModelInput{
		ProviderID: model.ProviderID, UpstreamName: model.UpstreamName, DisplayName: model.DisplayName,
		Kind: string(model.Kind), Formats: model.Formats, InputPrice: model.InputPrice,
		OutputPrice: model.OutputPrice, CacheReadPrice: model.CacheReadPrice, CacheWritePrice: model.CacheWritePrice,
		Currency: model.Currency, Enabled: model.Enabled,
	}
	normalized, err := input.normalize(providers)
	if err != nil {
		return fmt.Errorf("model %s has an invalid input: %w", model.ID, err)
	}
	if normalized.ProviderID != model.ProviderID || normalized.UpstreamName != model.UpstreamName ||
		normalized.DisplayName != model.DisplayName || normalized.Kind != string(model.Kind) ||
		!slices.Equal(normalized.Formats, model.Formats) || normalized.InputPrice != model.InputPrice ||
		normalized.OutputPrice != model.OutputPrice || normalized.CacheReadPrice != model.CacheReadPrice ||
		normalized.CacheWritePrice != model.CacheWritePrice || normalized.Currency != model.Currency ||
		normalized.Enabled != model.Enabled {
		return fmt.Errorf("model %s is not stored in normalized form", model.ID)
	}
	return nil
}

func validateStoredChannel(channel entity.Channel, models []entity.Model, providers *provider.Registry) error {
	if channel.ID == "" {
		return fmt.Errorf("a channel row has no id")
	}
	input := ChannelInput{
		Name: channel.Name, ProviderID: channel.ProviderID, Endpoints: channel.Endpoints,
		ModelIDs: channel.ModelIDs, Headers: channel.Headers, ForwardHeaders: channel.ForwardHeaders,
		DefaultMaxOutputTokens: channel.DefaultMaxOutputTokens, TimeoutSeconds: channel.TimeoutSeconds,
		FirstByteTimeoutSeconds: channel.FirstByteTimeoutSeconds,
		AllowPrivateNetwork:     channel.AllowPrivateNetwork, Enabled: channel.Enabled,
	}
	normalized, err := input.normalize(providers)
	if err != nil {
		return fmt.Errorf("channel %s has an invalid input: %w", channel.ID, err)
	}
	if normalized.Name != channel.Name || normalized.ProviderID != channel.ProviderID ||
		normalized.TimeoutSeconds != channel.TimeoutSeconds ||
		normalized.FirstByteTimeoutSeconds != channel.FirstByteTimeoutSeconds ||
		normalized.DefaultMaxOutputTokens != channel.DefaultMaxOutputTokens ||
		normalized.AllowPrivateNetwork != channel.AllowPrivateNetwork || normalized.Enabled != channel.Enabled ||
		!slices.Equal(normalized.Endpoints, channel.Endpoints) || !slices.Equal(normalized.ModelIDs, channel.ModelIDs) ||
		!maps.Equal(normalized.Headers, channel.Headers) || !slices.Equal(normalized.ForwardHeaders, channel.ForwardHeaders) {
		return fmt.Errorf("channel %s is not stored in normalized form", channel.ID)
	}
	if err := validateChannelModels(normalized, models); err != nil {
		return fmt.Errorf("channel %s has an invalid model binding: %w", channel.ID, err)
	}
	modelByID := modelMap(models)
	for _, override := range channel.CapabilityOverrides {
		model, known := modelByID[override.ModelID]
		if !known || !channel.HasModel(override.ModelID) || !channel.HasFormat(override.Format) ||
			!model.Supports(override.Format) || strings.TrimSpace(override.Field) == "" {
			return fmt.Errorf("channel %s has an invalid capability override", channel.ID)
		}
	}
	return nil
}

func validateStoredRoute(route entity.Route, channels []entity.Channel, models []entity.Model) error {
	if route.ID == "" {
		return fmt.Errorf("a route row has no id")
	}
	name, err := RequireName(route.Name, "invalid_name")
	if err != nil || name != route.Name {
		return fmt.Errorf("route %s has an invalid name", route.ID)
	}
	if err := validateTargets(route.Targets, channels, models); err != nil {
		return fmt.Errorf("route %s has invalid targets: %w", route.ID, err)
	}
	return nil
}
