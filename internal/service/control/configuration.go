package control

import (
	"context"
	"sort"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// Impact is what removing a channel or model affects: the routes that reference it and the projects
// those routes belong to.
type Impact struct {
	Routes   []RouteReferenceInfo
	Projects []string
}

func impactOf(references []repository.RouteReference) *Impact {
	impact := &Impact{Routes: impactReferences(references), Projects: []string{}}
	seen := map[string]bool{}
	for _, reference := range references {
		if seen[reference.ProjectID] {
			continue
		}
		seen[reference.ProjectID] = true
		impact.Projects = append(impact.Projects, reference.ProjectID)
	}
	sort.Strings(impact.Projects)
	return impact
}

func (service *Service) ChannelImpact(ctx context.Context, actor access.Actor, channelID string) (*Impact, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	references, err := service.store.RoutesReferencingChannel(ctx, channelID)
	if err != nil {
		return nil, access.Translate(err)
	}
	return impactOf(references), nil
}

func (service *Service) ModelImpact(ctx context.Context, actor access.Actor, modelID string) (*Impact, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	references, err := service.store.RoutesReferencingModel(ctx, modelID)
	if err != nil {
		return nil, access.Translate(err)
	}
	return impactOf(references), nil
}

// ConfigurationWarning is one degraded fact found in the stored graph. Startup logs it instead of
// refusing to run: a dangling reference degrades routing, it does not corrupt the audit trail.
type ConfigurationWarning struct {
	Code    string
	Subject string
	Message string
}

type ConfigurationHealth struct {
	Providers int
	Channels  int
	Models    int
	Routes    int
	Warnings  []ConfigurationWarning
}

func (service *Service) ConfigurationHealth(ctx context.Context, actor access.Actor) (ConfigurationHealth, error) {
	if err := actor.RequireAdmin(); err != nil {
		return ConfigurationHealth{}, err
	}
	return Health(ctx, service.store, service.providers)
}

// Health checks the stored graph and reports degraded facts instead of refusing to run. The
// composition root logs Warnings at startup; the console shows them on the configuration page.
func Health(ctx context.Context, store *repository.Store, providers *provider.Registry) (ConfigurationHealth, error) {
	health := ConfigurationHealth{Warnings: []ConfigurationWarning{}}
	providerRecords, err := store.Providers(ctx, false)
	if err != nil {
		return health, access.Translate(err)
	}
	channels, err := store.Channels(ctx, false)
	if err != nil {
		return health, access.Translate(err)
	}
	models, err := store.Models(ctx, repository.ModelFilter{})
	if err != nil {
		return health, access.Translate(err)
	}
	routes, err := store.AllRoutes(ctx, false)
	if err != nil {
		return health, access.Translate(err)
	}
	health.Providers, health.Channels, health.Models, health.Routes = len(providerRecords), len(channels), len(models), len(routes)
	providerByID := make(map[string]repository.ProviderRecord, len(providerRecords))
	for _, record := range providerRecords {
		providerByID[record.ID] = record
	}
	for _, record := range providerRecords {
		if _, err := provider.UnmarshalEndpoints(record.Endpoints); err != nil {
			health.Warnings = append(health.Warnings, ConfigurationWarning{
				Code: "provider_endpoints_unreadable", Subject: record.ID,
				Message: "the provider endpoint catalog cannot be decoded",
			})
		}
	}
	channelByID := make(map[string]repository.ChannelRecord, len(channels))
	for _, channel := range channels {
		channelByID[channel.ID] = channel
		if _, found := providerByID[channel.ProviderID]; !found {
			health.Warnings = append(health.Warnings, ConfigurationWarning{
				Code: "channel_provider_missing", Subject: channel.ID,
				Message: "the channel references a provider that is missing or deleted",
			})
		}
	}
	modelByID := make(map[string]entity.Model, len(models))
	for _, model := range models {
		modelByID[model.ID] = model
		spec, found := providers.Get(model.ProviderID)
		if !found {
			health.Warnings = append(health.Warnings, ConfigurationWarning{
				Code: "model_provider_missing", Subject: model.ID,
				Message: "the model references a provider that is missing or deleted",
			})
			continue
		}
		if len(deriveModelFormats(spec, model)) == 0 {
			health.Warnings = append(health.Warnings, ConfigurationWarning{
				Code: "model_formats_empty", Subject: model.ID,
				Message: "the provider exposes no endpoint this model kind can use",
			})
		}
	}
	for _, route := range routes {
		for _, target := range route.Targets {
			if !target.Target.Enabled {
				continue
			}
			channel, channelFound := channelByID[target.Channel.ID]
			model, modelFound := modelByID[target.Model.ID]
			if !channelFound || !modelFound || !channel.Enabled || !model.Enabled {
				health.Warnings = append(health.Warnings, ConfigurationWarning{
					Code: "route_target_degraded", Subject: route.Route.ID,
					Message: "a route target references a disabled or deleted channel or model",
				})
				continue
			}
			if _, found := providers.Get(channel.ProviderID); !found {
				health.Warnings = append(health.Warnings, ConfigurationWarning{
					Code: "route_target_degraded", Subject: route.Route.ID,
					Message: "a route target channel references a missing provider",
				})
			}
		}
	}
	return health, nil
}

// ConnectInput is the console's one-shot connection: an optional new custom provider, one channel,
// its credentials and the catalog models to create. Everything is written in one transaction.
type ConnectInput struct {
	Provider *ProviderInput
	Channel  ChannelInput
	Keys     []string
	Models   []ModelInput
}

type ConnectResult struct {
	Provider *ProviderInfo
	Channel  *ChannelInfo
	Models   []ModelView
}

// MaximumKeysPerChannel bounds how many credentials one connect request may add.
const MaximumKeysPerChannel = 8

func (service *Service) Connect(ctx context.Context, actor access.Actor, input ConnectInput) (*ConnectResult, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	var providerRow *entity.Provider
	var spec provider.ProviderSpec
	if input.Provider != nil {
		normalized, err := normalizeProvider(*input.Provider)
		if err != nil {
			return nil, err
		}
		endpoints, _, err := normalizeEndpoints(normalized.Endpoints)
		if err != nil {
			return nil, err
		}
		endpointMap, err := provider.UnmarshalEndpoints(endpoints)
		if err != nil {
			return nil, access.Invalid("invalid_endpoints")
		}
		spec = provider.ProviderSpec{Label: normalized.Label, Custom: true, Endpoints: endpointMap}
		providerRow = providerEntity(normalized)
	} else {
		found, err := service.spec(input.Channel.ProviderID)
		if err != nil {
			return nil, err
		}
		spec = found
	}
	channelInput, err := normalizeChannel(input.Channel, spec)
	if err != nil {
		return nil, err
	}
	if len(input.Keys) == 0 || len(input.Keys) > MaximumKeysPerChannel {
		return nil, access.Invalid("invalid_secret")
	}
	keys := make([]entity.ChannelKey, 0, len(input.Keys))
	for _, secret := range input.Keys {
		plain := strings.TrimSpace(secret)
		if plain == "" || len(plain) > MaximumSecretLength || strings.ContainsAny(plain, "\r\n\x00") {
			return nil, access.Invalid("invalid_secret")
		}
		sealed, err := secure.Seal(service.store.Secret, []byte(plain))
		if err != nil {
			return nil, err
		}
		suffix := "****"
		if len(plain) > 4 {
			suffix = plain[len(plain)-4:]
		}
		keys = append(keys, entity.ChannelKey{Label: "key", Secret: sealed, Suffix: suffix})
	}
	models := make([]*entity.Model, 0, len(input.Models))
	for _, modelInput := range input.Models {
		normalized, err := normalizeModel(modelInput, spec)
		if err != nil {
			return nil, err
		}
		models = append(models, &normalized)
	}
	channel := channelEntity(channelInput)
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.store.CreateChannelBundle(ctx, providerRow, channel, models, keys); err != nil {
		return nil, translateModelConflict(err)
	}
	service.reloadProviders(ctx)
	createdChannel, err := service.store.Channel(ctx, channel.ID)
	if err != nil {
		return nil, access.Translate(err)
	}
	info := channelInfo(*createdChannel)
	result := &ConnectResult{Channel: &info, Models: []ModelView{}}
	if providerRow != nil {
		info := providerInfoFromEntity(*providerRow)
		result.Provider = &info
	}
	for index := range models {
		result.Models = append(result.Models, service.modelView(models[index]))
	}
	return result, nil
}
