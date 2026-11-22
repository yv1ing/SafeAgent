package console

import (
	"context"
	"slices"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	MaximumFormats     = 4
	MaximumModelName   = 200
	MaximumDisplayName = 120
	DefaultCurrency    = "USD"
	// MaximumPriceMicros bounds a price per million tokens so that cost arithmetic cannot overflow.
	MaximumPriceMicros = 1_000_000_000_000
)

var Currencies = []string{"USD", "CNY", "EUR", "JPY", "GBP"}

type ModelService struct {
	store     *repository.Store
	providers *provider.Registry
}

func NewModelService(store *repository.Store, providers *provider.Registry) *ModelService {
	return &ModelService{store: store, providers: providers}
}

type ModelInput struct {
	ProviderID      string
	UpstreamName    string
	DisplayName     string
	Kind            string
	Formats         []string
	InputPrice      int64
	OutputPrice     int64
	CacheReadPrice  int64
	CacheWritePrice int64
	Currency        string
	Enabled         bool
	Version         int64
}

func (input ModelInput) entity() entity.Model {
	return entity.Model{
		ProviderID: input.ProviderID, UpstreamName: input.UpstreamName, DisplayName: input.DisplayName,
		Kind: entity.ModelKind(input.Kind), Formats: input.Formats,
		InputPrice: input.InputPrice, OutputPrice: input.OutputPrice, CacheReadPrice: input.CacheReadPrice, CacheWritePrice: input.CacheWritePrice,
		Currency: input.Currency, Enabled: input.Enabled,
	}
}

func modelKindSupports(kind entity.ModelKind, endpoint provider.EndpointSpec) bool {
	if kind == entity.ModelGeneration {
		return true
	}
	wanted := provider.AuxiliaryEmbeddings
	if kind == entity.ModelModeration {
		wanted = provider.AuxiliaryModerations
	}
	return slices.ContainsFunc(endpoint.Auxiliary, func(auxiliary provider.AuxiliarySpec) bool {
		return auxiliary.Kind == wanted
	})
}

func (input ModelInput) normalize(providers *provider.Registry) (ModelInput, error) {
	name, err := RequireText(input.UpstreamName, MaximumModelName, "invalid_model")
	if err != nil {
		return input, err
	}
	input.UpstreamName = name
	displayName, err := OptionalText(input.DisplayName, MaximumDisplayName, "invalid_model")
	if err != nil {
		return input, err
	}
	input.DisplayName = displayName
	spec, found := providers.Get(input.ProviderID)
	if !found {
		return input, Invalid("invalid_provider")
	}
	input.Kind = strings.TrimSpace(input.Kind)
	kind := entity.ModelKind(input.Kind)
	if !kind.Valid() {
		return input, Invalid("invalid_kind")
	}
	if len(input.Formats) == 0 || len(input.Formats) > MaximumFormats {
		return input, Invalid("invalid_model")
	}
	seen := make(map[string]bool, len(input.Formats))
	formats := make([]string, 0, len(input.Formats))
	for _, name := range input.Formats {
		name = strings.TrimSpace(name)
		endpoint, known := providerEndpoint(spec, name)
		if !known || seen[name] || !modelKindSupports(kind, endpoint) {
			return input, Invalid("invalid_format")
		}
		seen[name] = true
		formats = append(formats, name)
	}
	input.Formats = formats
	if input.InputPrice < 0 || input.OutputPrice < 0 || input.CacheReadPrice < 0 || input.CacheWritePrice < 0 ||
		input.InputPrice > MaximumPriceMicros || input.OutputPrice > MaximumPriceMicros ||
		input.CacheReadPrice > MaximumPriceMicros || input.CacheWritePrice > MaximumPriceMicros {
		return input, Invalid("invalid_price")
	}
	if input.Currency == "" {
		input.Currency = DefaultCurrency
	}
	if !slices.Contains(Currencies, input.Currency) {
		return input, Invalid("invalid_currency")
	}
	return input, nil
}

func (service *ModelService) List(ctx context.Context, actor Actor) ([]entity.Model, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	return service.store.Models(ctx)
}

func (service *ModelService) Get(ctx context.Context, actor Actor, modelID string) (*entity.Model, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	model, err := service.store.Model(ctx, modelID)
	return model, Translate(err)
}

func (service *ModelService) Create(ctx context.Context, actor Actor, input ModelInput) (*entity.Model, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	normalized, err := input.normalize(service.providers)
	if err != nil {
		return nil, err
	}
	configurationMu.Lock()
	defer configurationMu.Unlock()
	model := normalized.entity()
	created, err := service.store.CreateModel(ctx, &model)
	if repository.IsUniqueViolation(err) {
		return nil, Conflict("model_name_taken")
	}
	return created, Translate(err)
}

func (service *ModelService) Update(ctx context.Context, actor Actor, modelID string, input ModelInput) (*entity.Model, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	if err := RequireVersion(input.Version); err != nil {
		return nil, err
	}
	normalized, err := input.normalize(service.providers)
	if err != nil {
		return nil, err
	}
	configurationMu.Lock()
	defer configurationMu.Unlock()
	existing, err := service.store.Model(ctx, modelID)
	if err != nil {
		return nil, Translate(err)
	}
	if existing.ProviderID != normalized.ProviderID || existing.Kind != entity.ModelKind(normalized.Kind) {
		return nil, Invalid("model_immutable")
	}
	if err := service.checkBindings(ctx, *existing, normalized); err != nil {
		return nil, err
	}
	if err := service.cleanOverrides(ctx, *existing, normalized); err != nil {
		return nil, Translate(err)
	}
	model := normalized.entity()
	updated, err := service.store.UpdateModel(ctx, modelID, model, normalized.Version)
	if repository.IsUniqueViolation(err) {
		return nil, Conflict("model_name_taken")
	}
	return updated, Translate(err)
}

// checkBindings refuses a formats change that would leave an existing channel binding without a
// usable format, and a disable that would leave a route target disabled.
func (service *ModelService) checkBindings(ctx context.Context, existing entity.Model, input ModelInput) error {
	channels, err := service.store.Channels(ctx)
	if err != nil {
		return Translate(err)
	}
	for _, channel := range channels {
		if !channel.HasModel(existing.ID) {
			continue
		}
		if len(effectiveFormats(channel, input.entity())) == 0 {
			return Conflict("model_in_use")
		}
	}
	if !input.Enabled {
		routes, err := service.store.AllRoutes(ctx)
		if err != nil {
			return Translate(err)
		}
		if routesReferenceModel(routes, existing.ID) {
			return Conflict("model_in_use")
		}
	}
	return nil
}

// cleanOverrides drops machine-written overrides that no longer match a model format. They are
// observations, not operator configuration, so a format removal must not make a model uneditable.
func (service *ModelService) cleanOverrides(ctx context.Context, existing entity.Model, input ModelInput) error {
	formats := make(map[string]bool, len(input.Formats))
	for _, format := range input.Formats {
		formats[format] = true
	}
	channels, err := service.store.Channels(ctx)
	if err != nil {
		return err
	}
	for _, channel := range channels {
		filtered := make([]entity.CapabilityOverride, 0, len(channel.CapabilityOverrides))
		for _, override := range channel.CapabilityOverrides {
			if override.ModelID == existing.ID && !formats[override.Format] {
				continue
			}
			filtered = append(filtered, override)
		}
		if len(filtered) == len(channel.CapabilityOverrides) {
			continue
		}
		channel.CapabilityOverrides = filtered
		if _, err := service.store.UpdateChannel(ctx, channel.ID, channel, channel.Version); err != nil {
			return err
		}
	}
	return nil
}

func (service *ModelService) Delete(ctx context.Context, actor Actor, modelID string) (*entity.Model, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	configurationMu.Lock()
	defer configurationMu.Unlock()
	model, err := service.store.Model(ctx, modelID)
	if err != nil {
		return nil, Translate(err)
	}
	channels, err := service.store.Channels(ctx)
	if err != nil {
		return nil, Translate(err)
	}
	for _, channel := range channels {
		if channel.HasModel(modelID) {
			return nil, Conflict("model_in_use")
		}
	}
	routes, err := service.store.AllRoutes(ctx)
	if err != nil {
		return nil, Translate(err)
	}
	if routesReferenceModel(routes, modelID) {
		return nil, Conflict("model_in_use")
	}
	return model, Translate(service.store.DeleteModel(ctx, modelID))
}

func routesReferenceModel(routes []entity.Route, modelID string) bool {
	for _, route := range routes {
		for _, target := range route.Targets {
			if target.ModelID == modelID {
				return true
			}
		}
	}
	return false
}

func routesReferenceChannel(routes []entity.Route, channelID string) bool {
	for _, route := range routes {
		for _, target := range route.Targets {
			if target.ChannelID == channelID {
				return true
			}
		}
	}
	return false
}

// effectiveFormats returns the ordered channel formats the model can use.
func effectiveFormats(channel entity.Channel, model entity.Model) []string {
	formats := make([]string, 0, len(channel.Endpoints))
	for _, endpoint := range channel.Endpoints {
		if model.Supports(endpoint.Format) {
			formats = append(formats, endpoint.Format)
		}
	}
	return formats
}
