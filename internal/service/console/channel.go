package console

import (
	"context"
	"regexp"

	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	DefaultTimeoutSeconds          = 600
	DefaultFirstByteTimeoutSeconds = 60
	DefaultCurrency                = "USD"
	MaximumTimeoutSeconds          = 3600
	MaximumFirstByteSeconds        = 600
	MaximumModels                  = 200
	MaximumModelNameLength         = 200
	MaximumHeaders                 = 32
	MaximumOutputTokens            = 1 << 20
	MaximumHeaderValueLength       = 4096
	MaximumSecretLength            = 4096
	MaximumEndpointLength          = 2048
	MaximumKeyLabelLength          = 120
	// MaximumPriceMicros bounds a price per million tokens so that cost arithmetic cannot overflow.
	MaximumPriceMicros = 1_000_000_000_000
	maskedValue        = "****"
)

var (
	Currencies = []string{"USD", "CNY", "EUR", "JPY", "GBP"}

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
	Custom                  bool
	Endpoints               []entity.ChannelEndpoint
	Headers                 map[string]string
	ForwardHeaders          []string
	DefaultMaxOutputTokens  int
	Models                  []entity.ChannelModel
	Currency                string
	TimeoutSeconds          int
	FirstByteTimeoutSeconds int
	AllowPrivateNetwork     bool
	Enabled                 bool
	Version                 int64
}

func (input ChannelInput) entity() entity.Channel {
	return entity.Channel{
		Name: input.Name, ProviderID: input.ProviderID, Custom: input.Custom, Endpoints: input.Endpoints,
		Headers: input.Headers, ForwardHeaders: input.ForwardHeaders, DefaultMaxOutputTokens: input.DefaultMaxOutputTokens,
		Models: input.Models, Currency: input.Currency, TimeoutSeconds: input.TimeoutSeconds,
		FirstByteTimeoutSeconds: input.FirstByteTimeoutSeconds, AllowPrivateNetwork: input.AllowPrivateNetwork, Enabled: input.Enabled,
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
	existing, err := service.store.Channel(ctx, channelID)
	if err != nil {
		return nil, Translate(err)
	}
	input.Headers = mergeHeaders(existing.Headers, input.Headers)
	normalized, err := input.normalize(service.providers)
	if err != nil {
		return nil, err
	}
	updated, err := service.store.UpdateChannel(ctx, channelID, normalized.entity(), normalized.Version)
	return updated, Translate(err)
}

// ChannelOption is what a project member needs to point a route at a channel.
type ChannelOption struct {
	ID      string
	Name    string
	Formats []string
	Models  []string
}

func (service *ChannelService) Options(ctx context.Context, actor Actor, projectID string) ([]ChannelOption, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	channels, err := service.store.Channels(ctx)
	if err != nil {
		return nil, Translate(err)
	}
	options := make([]ChannelOption, 0, len(channels))
	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		names := make([]string, 0, len(channel.Models))
		for _, model := range channel.Models {
			names = append(names, model.Name)
		}
		options = append(options, ChannelOption{ID: channel.ID, Name: channel.Name, Formats: service.formats(channel), Models: names})
	}
	return options, nil
}

// FormatsOf exposes the enabled formats of a channel to the console view.
func (service *ChannelService) FormatsOf(channel entity.Channel) []string {
	return service.formats(channel)
}

func (service *ChannelService) formats(channel entity.Channel) []string {
	spec, found := service.providers.Get(channel.ProviderID)
	if !found {
		return nil
	}
	disabled := map[string]bool{}
	for _, endpoint := range channel.Endpoints {
		if endpoint.Disabled {
			disabled[endpoint.Format] = true
		}
	}
	formats := make([]string, 0, len(spec.Endpoints))
	for _, format := range spec.Formats() {
		if !disabled[string(format)] {
			formats = append(formats, string(format))
		}
	}
	return formats
}

// ChannelHealth is the live breaker state of one channel model.
type ChannelHealth struct {
	ChannelID     string
	ChannelName   string
	UpstreamModel string
	routing.Health
}

func (service *ChannelService) Health(ctx context.Context, actor Actor, channelID string) ([]ChannelHealth, error) {
	channel, err := service.Get(ctx, actor, channelID)
	if err != nil {
		return nil, err
	}
	health := make([]ChannelHealth, 0, len(channel.Models))
	for _, model := range channel.Models {
		health = append(health, ChannelHealth{ChannelID: channel.ID, ChannelName: channel.Name, UpstreamModel: model.Name, Health: service.selector.Health(channel.ID, model.Name)})
	}
	return health, nil
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
