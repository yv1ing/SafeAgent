package console

import (
	"context"
	"regexp"
	"slices"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	DefaultTimeoutSeconds          = 600
	DefaultFirstByteTimeoutSeconds = 60
	MaximumTimeoutSeconds          = 3600
	MaximumFirstByteSeconds        = 600
	MaximumChannelModels           = 200
	MaximumHeaders                 = 32
	MaximumOutputTokens            = 1 << 20
	MaximumHeaderValueLength       = 4096
	MaximumSecretLength            = 4096
	MaximumEndpointLength          = 2048
	MaximumKeyLabelLength          = 120
	maskedValue                    = "****"
)

var (
	headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]{1,64}$")

	// secretHeaders are masked in responses and never round-trip from a client.
	secretHeaders = map[string]bool{
		"authorization": true, "x-api-key": true, "x-goog-api-key": true,
		"api-key": true, "proxy-authorization": true,
	}
	// reservedHeaders are owned by the gateway, which drops them from a channel's configuration.
	reservedHeaders = map[string]bool{
		"authorization": true, "x-api-key": true, "x-goog-api-key": true, "host": true,
		"content-length": true, "transfer-encoding": true, "connection": true,
	}
)

type ChannelService struct {
	store     *repository.Store
	selector  *routing.Selector
	providers *provider.Registry
}

func NewChannelService(store *repository.Store, selector *routing.Selector, providers *provider.Registry) *ChannelService {
	return &ChannelService{store: store, selector: selector, providers: providers}
}

type ChannelInput struct {
	Name                    string
	ProviderID              string
	Endpoints               []entity.ChannelEndpoint
	ModelIDs                []string
	Headers                 map[string]string
	ForwardHeaders          []string
	DefaultMaxOutputTokens  int
	TimeoutSeconds          int
	FirstByteTimeoutSeconds int
	AllowPrivateNetwork     bool
	Enabled                 bool
	Version                 int64
}

func (input ChannelInput) entity() entity.Channel {
	return entity.Channel{
		Name: input.Name, ProviderID: input.ProviderID, Endpoints: input.Endpoints, ModelIDs: input.ModelIDs,
		Headers: input.Headers, ForwardHeaders: input.ForwardHeaders, DefaultMaxOutputTokens: input.DefaultMaxOutputTokens,
		TimeoutSeconds: input.TimeoutSeconds, FirstByteTimeoutSeconds: input.FirstByteTimeoutSeconds,
		AllowPrivateNetwork: input.AllowPrivateNetwork, Enabled: input.Enabled,
	}
}

func (service *ChannelService) List(ctx context.Context, actor Actor) ([]entity.Channel, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	return service.store.Channels(ctx)
}

func (service *ChannelService) Get(ctx context.Context, actor Actor, channelID string) (*entity.Channel, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	channel, err := service.store.Channel(ctx, channelID)
	return channel, Translate(err)
}

func (service *ChannelService) Create(ctx context.Context, actor Actor, input ChannelInput) (*entity.Channel, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	normalized, err := input.normalize(service.providers)
	if err != nil {
		return nil, err
	}
	configurationMu.Lock()
	defer configurationMu.Unlock()
	models, err := service.store.Models(ctx)
	if err != nil {
		return nil, Translate(err)
	}
	if err := validateChannelModels(normalized, models); err != nil {
		return nil, err
	}
	channel := normalized.entity()
	created, err := service.store.CreateChannel(ctx, &channel)
	return created, Translate(err)
}

func (service *ChannelService) Update(ctx context.Context, actor Actor, channelID string, input ChannelInput) (*entity.Channel, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	if err := RequireVersion(input.Version); err != nil {
		return nil, err
	}
	configurationMu.Lock()
	defer configurationMu.Unlock()
	existing, err := service.store.Channel(ctx, channelID)
	if err != nil {
		return nil, Translate(err)
	}
	if input.ProviderID != existing.ProviderID {
		return nil, Invalid("provider_immutable")
	}
	if input.Headers == nil {
		input.Headers = existing.Headers
	} else {
		input.Headers = mergeHeaders(existing.Headers, input.Headers)
	}
	normalized, err := input.normalize(service.providers)
	if err != nil {
		return nil, err
	}
	models, err := service.store.Models(ctx)
	if err != nil {
		return nil, Translate(err)
	}
	if err := validateChannelModels(normalized, models); err != nil {
		return nil, err
	}
	if err := service.checkRoutes(ctx, *existing, normalized, models); err != nil {
		return nil, err
	}
	channel := normalized.entity()
	channel.CapabilityOverrides = sanitizeOverrides(existing.CapabilityOverrides, channel)
	updated, err := service.store.UpdateChannel(ctx, channelID, channel, normalized.Version)
	return updated, Translate(err)
}

func (service *ChannelService) Delete(ctx context.Context, actor Actor, channelID string) (*entity.Channel, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	configurationMu.Lock()
	defer configurationMu.Unlock()
	channel, err := service.store.Channel(ctx, channelID)
	if err != nil {
		return nil, Translate(err)
	}
	routes, err := service.store.AllRoutes(ctx)
	if err != nil {
		return nil, Translate(err)
	}
	if routesReferenceChannel(routes, channelID) {
		return nil, Conflict("channel_in_use")
	}
	return channel, Translate(service.store.DeleteChannel(ctx, channelID))
}

// checkRoutes refuses an edit that would disable or detach a model an existing route target needs.
func (service *ChannelService) checkRoutes(ctx context.Context, existing entity.Channel, input ChannelInput, models []entity.Model) error {
	routes, err := service.store.AllRoutes(ctx)
	if err != nil {
		return Translate(err)
	}
	byID := modelMap(models)
	for _, route := range routes {
		for _, target := range route.Targets {
			if target.ChannelID != existing.ID {
				continue
			}
			if !input.Enabled || !slices.Contains(input.ModelIDs, target.ModelID) {
				return Conflict("channel_in_use")
			}
			model, found := byID[target.ModelID]
			if !found || len(effectiveFormats(input.entity(), model)) == 0 {
				return Conflict("channel_in_use")
			}
		}
	}
	return nil
}

func validateChannelModels(input ChannelInput, models []entity.Model) error {
	if len(input.ModelIDs) > MaximumChannelModels {
		return Invalid("invalid_model")
	}
	byID := modelMap(models)
	for _, modelID := range input.ModelIDs {
		model, found := byID[modelID]
		if !found || model.ProviderID != input.ProviderID || len(effectiveFormats(input.entity(), model)) == 0 {
			return Invalid("invalid_model")
		}
	}
	return nil
}

func modelMap(models []entity.Model) map[string]entity.Model {
	result := make(map[string]entity.Model, len(models))
	for _, model := range models {
		result[model.ID] = model
	}
	return result
}

func sanitizeOverrides(overrides []entity.CapabilityOverride, channel entity.Channel) []entity.CapabilityOverride {
	clean := make([]entity.CapabilityOverride, 0, len(overrides))
	for _, override := range overrides {
		if !channel.HasModel(override.ModelID) || !channel.HasFormat(override.Format) {
			continue
		}
		clean = append(clean, override)
	}
	return clean
}

// ChannelHealth is the live breaker state of one channel model format.
type ChannelHealth struct {
	ChannelID   string
	ChannelName string
	ModelID     string
	ModelName   string
	Format      string
	routing.Health
}

func (service *ChannelService) Health(ctx context.Context, actor Actor, channelID string) ([]ChannelHealth, error) {
	channel, err := service.Get(ctx, actor, channelID)
	if err != nil {
		return nil, err
	}
	models, err := service.store.Models(ctx)
	if err != nil {
		return nil, Translate(err)
	}
	byID := modelMap(models)
	health := []ChannelHealth{}
	for _, modelID := range channel.ModelIDs {
		model, found := byID[modelID]
		if !found {
			continue
		}
		for _, format := range effectiveFormats(*channel, model) {
			health = append(health, ChannelHealth{
				ChannelID: channel.ID, ChannelName: channel.Name, ModelID: model.ID,
				ModelName: model.UpstreamName, Format: format,
				Health: service.selector.Health(channel.ID, model.ID, format),
			})
		}
	}
	return health, nil
}

// ChannelModelOption is what a route editor needs to choose a model on a channel.
type ChannelModelOption struct {
	ID          string
	Name        string
	DisplayName string
	Kind        string
	Formats     []string
}

// ChannelOption is what a project member needs to point a route at a channel.
type ChannelOption struct {
	ID      string
	Name    string
	Formats []string
	Models  []ChannelModelOption
}

func (service *ChannelService) Options(ctx context.Context, actor Actor, projectID string) ([]ChannelOption, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	channels, err := service.store.Channels(ctx)
	if err != nil {
		return nil, Translate(err)
	}
	models, err := service.store.Models(ctx)
	if err != nil {
		return nil, Translate(err)
	}
	byID := modelMap(models)
	options := make([]ChannelOption, 0, len(channels))
	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		formats := make([]string, 0, len(channel.Endpoints))
		for _, endpoint := range channel.Endpoints {
			formats = append(formats, endpoint.Format)
		}
		option := ChannelOption{ID: channel.ID, Name: channel.Name, Formats: formats, Models: []ChannelModelOption{}}
		for _, modelID := range channel.ModelIDs {
			model, found := byID[modelID]
			if !found || !model.Enabled {
				continue
			}
			formats := effectiveFormats(channel, model)
			if len(formats) == 0 {
				continue
			}
			option.Models = append(option.Models, ChannelModelOption{
				ID: model.ID, Name: model.UpstreamName, DisplayName: model.DisplayName,
				Kind: string(model.Kind), Formats: formats,
			})
		}
		if len(option.Models) > 0 {
			options = append(options, option)
		}
	}
	return options, nil
}

// ProviderOption is one entry of the built-in provider catalog the console offers.
type ProviderOption struct {
	ID      string
	Label   string
	Custom  bool
	Formats []string
	Hosts   map[string]string
}

func (service *ChannelService) ProviderCatalog(ctx context.Context, actor Actor) ([]ProviderOption, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	specs := service.providers.List()
	options := make([]ProviderOption, 0, len(specs))
	for _, spec := range specs {
		hosts := map[string]string{}
		for _, format := range spec.Formats() {
			hosts[string(format)] = spec.Endpoints[format].BaseURL
		}
		options = append(options, ProviderOption{ID: spec.ID, Label: spec.Label, Custom: spec.Custom, Formats: providerFormats(spec), Hosts: hosts})
	}
	return options, nil
}

func providerFormats(spec provider.ProviderSpec) []string {
	names := make([]string, 0, len(spec.Endpoints))
	for _, format := range spec.Formats() {
		names = append(names, string(format))
	}
	return names
}

func providerEndpoint(spec provider.ProviderSpec, name string) (provider.EndpointSpec, bool) {
	for _, format := range spec.Formats() {
		if string(format) == name {
			return spec.Endpoint(format)
		}
	}
	return provider.EndpointSpec{}, false
}
