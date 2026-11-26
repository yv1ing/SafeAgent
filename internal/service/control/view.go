package control

import (
	"encoding/json"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// ProviderInfo is one provider row plus the format list the console derives from its catalog. The
// transport never sees a repository type, so the storage boundary stays below the service layer.
type ProviderInfo struct {
	entity.Provider
	ChannelCount int
	Formats      []string
}

// ChannelInfo is one channel row with its provider label, route reference count and the capability
// facts learned at runtime.
type ChannelInfo struct {
	entity.Channel
	ProviderLabel       string
	RouteCount          int
	CapabilityOverrides []entity.CapabilityOverride
}

// RouteTargetInfo is one route target with the catalog rows its ids stand for.
type RouteTargetInfo struct {
	Target   entity.RouteTarget
	Channel  entity.Channel
	Model    entity.Model
	Provider entity.Provider
}

// RouteInfo is one route with its targets and aliases.
type RouteInfo struct {
	Route   entity.Route
	Targets []RouteTargetInfo
	Aliases []entity.RouteAlias
}

// RouteReferenceInfo says which route a channel or model removal affects.
type RouteReferenceInfo struct {
	RouteID     string
	RouteName   string
	ProjectID   string
	TargetCount int
}

func providerFormats(endpoints json.RawMessage) []string {
	decoded, err := provider.UnmarshalEndpoints(endpoints)
	if err != nil {
		return []string{}
	}
	return formatNames(provider.FormatsForKind(decoded, provider.KindGeneration))
}

func providerInfo(record repository.ProviderRecord) ProviderInfo {
	return ProviderInfo{Provider: record.Provider, ChannelCount: record.ChannelCount, Formats: providerFormats(record.Endpoints)}
}

func providerInfoFromEntity(provider entity.Provider) ProviderInfo {
	return ProviderInfo{Provider: provider, Formats: providerFormats(provider.Endpoints)}
}

func channelInfo(record repository.ChannelRecord) ChannelInfo {
	return ChannelInfo{
		Channel: record.Channel, ProviderLabel: record.ProviderLabel,
		RouteCount: record.RouteCount, CapabilityOverrides: record.CapabilityOverrides,
	}
}

func routeInfo(record repository.RouteRecord) RouteInfo {
	targets := make([]RouteTargetInfo, 0, len(record.Targets))
	for _, target := range record.Targets {
		targets = append(targets, RouteTargetInfo{
			Target: target.Target, Channel: target.Channel, Model: target.Model, Provider: target.Provider,
		})
	}
	return RouteInfo{Route: record.Route, Targets: targets, Aliases: record.Aliases}
}

func impactReferences(references []repository.RouteReference) []RouteReferenceInfo {
	converted := make([]RouteReferenceInfo, 0, len(references))
	for _, reference := range references {
		converted = append(converted, RouteReferenceInfo{
			RouteID: reference.RouteID, RouteName: reference.RouteName,
			ProjectID: reference.ProjectID, TargetCount: reference.TargetCount,
		})
	}
	return converted
}
