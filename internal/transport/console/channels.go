package console

import (
	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type ChannelModelView struct {
	Name            string `json:"name" required:"true"`
	InputPrice      int64  `json:"input_price"`
	OutputPrice     int64  `json:"output_price"`
	CacheReadPrice  int64  `json:"cache_read_price"`
	CacheWritePrice int64  `json:"cache_write_price"`
}

type ChannelEndpointView struct {
	Format   string `json:"format" required:"true"`
	BaseURL  string `json:"base_url,omitempty"`
	Path     string `json:"path,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type CapabilityOverrideView struct {
	Format string `json:"format" required:"true"`
	Field  string `json:"field" required:"true"`
	Reason string `json:"reason,omitempty"`
}

type ChannelView struct {
	ID                      string                   `json:"id" required:"true"`
	Name                    string                   `json:"name" required:"true"`
	ProviderID              string                   `json:"provider_id" required:"true"`
	Custom                  bool                     `json:"custom"`
	Formats                 []string                 `json:"formats"`
	Endpoints               []ChannelEndpointView    `json:"endpoints"`
	CapabilityOverrides     []CapabilityOverrideView `json:"capability_overrides"`
	Headers                 map[string]string        `json:"headers"`
	ForwardHeaders          []string                 `json:"forward_headers"`
	DefaultMaxOutputTokens  int                      `json:"default_max_output_tokens"`
	Models                  []ChannelModelView       `json:"models"`
	Currency                string                   `json:"currency"`
	TimeoutSeconds          int                      `json:"timeout_seconds"`
	FirstByteTimeoutSeconds int                      `json:"first_byte_timeout_seconds"`
	AllowPrivateNetwork     bool                     `json:"allow_private_network"`
	Enabled                 bool                     `json:"enabled"`
	Version                 int64                    `json:"version"`
}

type ChannelInput struct {
	Name                    string                `json:"name" required:"true"`
	ProviderID              string                `json:"provider_id" required:"true"`
	Endpoints               []ChannelEndpointView `json:"endpoints"`
	Headers                 map[string]string     `json:"headers"`
	ForwardHeaders          []string              `json:"forward_headers"`
	DefaultMaxOutputTokens  int                   `json:"default_max_output_tokens" description:"Applied to an upstream that requires an output limit when the client omits one"`
	Models                  []ChannelModelView    `json:"models"`
	Currency                string                `json:"currency"`
	TimeoutSeconds          int                   `json:"timeout_seconds"`
	FirstByteTimeoutSeconds int                   `json:"first_byte_timeout_seconds"`
	AllowPrivateNetwork     bool                  `json:"allow_private_network"`
	Enabled                 bool                  `json:"enabled"`
	Version                 int64                 `json:"version"`
}

type ChannelKeyInput struct {
	Label  string `json:"label"`
	Secret string `json:"secret" required:"true"`
}

type ChannelKeyView struct {
	ID      string `json:"id" required:"true"`
	Label   string `json:"label"`
	Suffix  string `json:"suffix"`
	Enabled bool   `json:"enabled"`
}

type ChannelOption struct {
	ID      string   `json:"id" required:"true"`
	Name    string   `json:"name" required:"true"`
	Formats []string `json:"formats"`
	Models  []string `json:"models"`
}

type DiscoveryView struct {
	Models []string `json:"models"`
}

type ProviderView struct {
	ID      string            `json:"id" required:"true"`
	Label   string            `json:"label" required:"true"`
	Custom  bool              `json:"custom"`
	Formats []string          `json:"formats"`
	Hosts   map[string]string `json:"hosts"`
}

type HealthView struct {
	ChannelID           string `json:"channel_id" required:"true"`
	ChannelName         string `json:"channel_name" required:"true"`
	UpstreamModel       string `json:"upstream_model" required:"true"`
	State               string `json:"state" required:"true" enum:"closed,open,half_open"`
	ConsecutiveFailures int    `json:"consecutive_failures" required:"true"`
	CooldownSeconds     int64  `json:"cooldown_seconds" required:"true"`
}

type channelPath struct {
	ChannelID string `path:"channel_id" json:"-" required:"true"`
}

type updateChannelInput struct {
	channelPath
	ChannelInput
}

type createChannelKeyInput struct {
	channelPath
	ChannelKeyInput
}

type channelKeyPath struct {
	ChannelID string `path:"channel_id" json:"-" required:"true"`
	KeyID     string `path:"key_id" json:"-" required:"true"`
}

func healthView(value *service.ChannelHealth) HealthView {
	return HealthView{ChannelID: value.ChannelID, ChannelName: value.ChannelName, UpstreamModel: value.UpstreamModel, State: string(value.State), ConsecutiveFailures: value.Failures, CooldownSeconds: value.Cooldown}
}

func channelModelViews(models []entity.ChannelModel) []ChannelModelView {
	result := make([]ChannelModelView, 0, len(models))
	for _, entry := range models {
		result = append(result, ChannelModelView{Name: entry.Name, InputPrice: entry.InputPrice, OutputPrice: entry.OutputPrice, CacheReadPrice: entry.CacheReadPrice, CacheWritePrice: entry.CacheWritePrice})
	}
	return result
}

func channelEndpointViews(endpoints []entity.ChannelEndpoint) []ChannelEndpointView {
	result := make([]ChannelEndpointView, 0, len(endpoints))
	for _, entry := range endpoints {
		result = append(result, ChannelEndpointView{Format: entry.Format, BaseURL: entry.BaseURL, Path: entry.Path, Disabled: entry.Disabled})
	}
	return result
}

func channelOverrideViews(overrides []entity.CapabilityOverride) []CapabilityOverrideView {
	result := make([]CapabilityOverrideView, 0, len(overrides))
	for _, entry := range overrides {
		result = append(result, CapabilityOverrideView{Format: entry.Format, Field: entry.Field, Reason: entry.Reason})
	}
	return result
}

func channelView(value *entity.Channel, formats []string) ChannelView {
	return ChannelView{
		ID: value.ID, Name: value.Name, ProviderID: value.ProviderID, Custom: value.Custom,
		Formats: formats, Endpoints: channelEndpointViews(value.Endpoints), CapabilityOverrides: channelOverrideViews(value.CapabilityOverrides),
		Headers: service.MaskHeaders(value.Headers), ForwardHeaders: value.ForwardHeaders,
		DefaultMaxOutputTokens: value.DefaultMaxOutputTokens, Models: channelModelViews(value.Models),
		Currency: value.Currency, TimeoutSeconds: value.TimeoutSeconds, FirstByteTimeoutSeconds: value.FirstByteTimeoutSeconds,
		AllowPrivateNetwork: value.AllowPrivateNetwork, Enabled: value.Enabled, Version: value.Version,
	}
}

func (input ChannelInput) service() service.ChannelInput {
	models := make([]entity.ChannelModel, 0, len(input.Models))
	for _, entry := range input.Models {
		models = append(models, entity.ChannelModel{Name: entry.Name, InputPrice: entry.InputPrice, OutputPrice: entry.OutputPrice, CacheReadPrice: entry.CacheReadPrice, CacheWritePrice: entry.CacheWritePrice})
	}
	endpoints := make([]entity.ChannelEndpoint, 0, len(input.Endpoints))
	for _, entry := range input.Endpoints {
		endpoints = append(endpoints, entity.ChannelEndpoint{Format: entry.Format, BaseURL: entry.BaseURL, Path: entry.Path, Disabled: entry.Disabled})
	}
	return service.ChannelInput{
		Name: input.Name, ProviderID: input.ProviderID, Endpoints: endpoints,
		Headers: input.Headers, ForwardHeaders: input.ForwardHeaders,
		DefaultMaxOutputTokens: input.DefaultMaxOutputTokens, Models: models, Currency: input.Currency,
		TimeoutSeconds: input.TimeoutSeconds, FirstByteTimeoutSeconds: input.FirstByteTimeoutSeconds,
		AllowPrivateNetwork: input.AllowPrivateNetwork, Enabled: input.Enabled, Version: input.Version,
	}
}

func channelKeyView(key *entity.ChannelKey) ChannelKeyView {
	return ChannelKeyView{ID: key.ID, Label: key.Label, Suffix: key.Suffix, Enabled: key.Enabled}
}

func channelOptionView(value *service.ChannelOption) ChannelOption {
	return ChannelOption{ID: value.ID, Name: value.Name, Formats: value.Formats, Models: nonNil(value.Models)}
}

func providerView(value *service.ProviderOption) ProviderView {
	return ProviderView{ID: value.ID, Label: value.Label, Custom: value.Custom, Formats: value.Formats, Hosts: value.Hosts}
}

func (console *Console) registerChannels() {
	console.add("GET", "/providers", "listProviders", nil, []ProviderView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		providers, err := console.Channel.ProviderCatalog(ctx.Request.Context(), identity.actor())
		if err != nil {
			return nil, err
		}
		return viewAll(providers, providerView), nil
	})
	console.add("GET", "/channels", "listChannels", nil, []ChannelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		channels, err := console.Channel.List(ctx.Request.Context(), identity.actor())
		if err != nil {
			return nil, err
		}
		return viewAll(channels, func(channel *entity.Channel) ChannelView {
			return channelView(channel, console.Channel.FormatsOf(*channel))
		}), nil
	})
	console.add("POST", "/channels", "createChannel", ChannelInput{}, ChannelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input ChannelInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		created, err := console.Channel.Create(ctx.Request.Context(), identity.actor(), input.service())
		if err != nil {
			return nil, err
		}
		return channelView(created, console.Channel.FormatsOf(*created)), nil
	})
	console.add("GET", "/channels/{channel_id}", "getChannel", channelPath{}, ChannelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		found, err := console.Channel.Get(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"))
		if err != nil {
			return nil, err
		}
		return channelView(found, console.Channel.FormatsOf(*found)), nil
	})
	console.add("PUT", "/channels/{channel_id}", "updateChannel", updateChannelInput{}, ChannelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input ChannelInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		updated, err := console.Channel.Update(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"), input.service())
		if err != nil {
			return nil, err
		}
		return channelView(updated, console.Channel.FormatsOf(*updated)), nil
	})
	console.add("POST", "/channels/{channel_id}/discover-models", "discoverChannelModels", channelPath{}, DiscoveryView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		models, err := console.Channel.DiscoverModels(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"))
		if err != nil {
			return nil, err
		}
		return DiscoveryView{Models: nonNil(models)}, nil
	})
	console.add("GET", "/channels/{channel_id}/health", "getChannelHealth", channelPath{}, []HealthView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		health, err := console.Channel.Health(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"))
		if err != nil {
			return nil, err
		}
		return viewAll(health, healthView), nil
	})
	console.add("GET", "/channels/{channel_id}/keys", "listChannelKeys", channelPath{}, []ChannelKeyView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		keys, err := console.Channel.Keys(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"))
		if err != nil {
			return nil, err
		}
		return viewAll(keys, channelKeyView), nil
	})
	console.add("POST", "/channels/{channel_id}/keys", "createChannelKey", createChannelKeyInput{}, ChannelKeyView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input ChannelKeyInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		created, err := console.Channel.AddKey(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"), input.Label, input.Secret)
		if err != nil {
			return nil, err
		}
		return channelKeyView(created), nil
	})
	console.add("DELETE", "/channels/{channel_id}/keys/{key_id}", "deleteChannelKey", channelKeyPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		return nil, console.Channel.DeleteKey(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"), ctx.Param("key_id"))
	})
	console.add("GET", "/projects/{project_id}/channel-options", "listChannelOptions", projectPath{}, []ChannelOption{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		options, err := console.Channel.Options(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"))
		if err != nil {
			return nil, err
		}
		return viewAll(options, channelOptionView), nil
	})
}
