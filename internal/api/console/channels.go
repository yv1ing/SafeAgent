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

type ChannelView struct {
	ID                      string             `json:"id" required:"true"`
	Name                    string             `json:"name" required:"true"`
	Format                  string             `json:"format"`
	Vendor                  string             `json:"vendor"`
	Endpoint                string             `json:"endpoint"`
	CountTokensEndpoint     string             `json:"count_tokens_endpoint"`
	Headers                 map[string]string  `json:"headers"`
	Models                  []ChannelModelView `json:"models"`
	Currency                string             `json:"currency"`
	TimeoutSeconds          int                `json:"timeout_seconds"`
	FirstByteTimeoutSeconds int                `json:"first_byte_timeout_seconds"`
	AllowPrivateNetwork     bool               `json:"allow_private_network"`
	Enabled                 bool               `json:"enabled"`
	Version                 int64              `json:"version"`
}

type ChannelInput struct {
	Name                    string             `json:"name" required:"true"`
	Format                  string             `json:"format" required:"true"`
	Vendor                  string             `json:"vendor"`
	Endpoint                string             `json:"endpoint" required:"true"`
	CountTokensEndpoint     string             `json:"count_tokens_endpoint"`
	Headers                 map[string]string  `json:"headers"`
	Models                  []ChannelModelView `json:"models"`
	Currency                string             `json:"currency"`
	TimeoutSeconds          int                `json:"timeout_seconds"`
	FirstByteTimeoutSeconds int                `json:"first_byte_timeout_seconds"`
	AllowPrivateNetwork     bool               `json:"allow_private_network"`
	Enabled                 bool               `json:"enabled"`
	Version                 int64              `json:"version"`
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
	ID     string   `json:"id" required:"true"`
	Name   string   `json:"name" required:"true"`
	Format string   `json:"format"`
	Models []string `json:"models"`
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

func channelView(value *entity.Channel) ChannelView {
	return ChannelView{ID: value.ID, Name: value.Name, Format: value.Format, Vendor: value.Vendor, Endpoint: value.Endpoint, CountTokensEndpoint: value.CountTokensEndpoint, Headers: service.MaskHeaders(value.Headers), Models: channelModelViews(value.Models), Currency: value.Currency, TimeoutSeconds: value.TimeoutSeconds, FirstByteTimeoutSeconds: value.FirstByteTimeoutSeconds, AllowPrivateNetwork: value.AllowPrivateNetwork, Enabled: value.Enabled, Version: value.Version}
}

func (input ChannelInput) service() service.ChannelInput {
	models := make([]entity.ChannelModel, 0, len(input.Models))
	for _, entry := range input.Models {
		models = append(models, entity.ChannelModel{Name: entry.Name, InputPrice: entry.InputPrice, OutputPrice: entry.OutputPrice, CacheReadPrice: entry.CacheReadPrice, CacheWritePrice: entry.CacheWritePrice})
	}
	return service.ChannelInput{Name: input.Name, Format: input.Format, Vendor: input.Vendor, Endpoint: input.Endpoint, CountTokensEndpoint: input.CountTokensEndpoint, Headers: input.Headers, Models: models, Currency: input.Currency, TimeoutSeconds: input.TimeoutSeconds, FirstByteTimeoutSeconds: input.FirstByteTimeoutSeconds, AllowPrivateNetwork: input.AllowPrivateNetwork, Enabled: input.Enabled, Version: input.Version}
}

func channelKeyView(key *entity.ChannelKey) ChannelKeyView {
	return ChannelKeyView{ID: key.ID, Label: key.Label, Suffix: key.Suffix, Enabled: key.Enabled}
}

func channelOptionView(value *service.ChannelOption) ChannelOption {
	return ChannelOption{ID: value.ID, Name: value.Name, Format: value.Format, Models: nonNil(value.Models)}
}

func (console *Console) registerChannels() {
	console.add("GET", "/channels", "listChannels", nil, []ChannelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		channels, err := console.Channel.List(ctx.Request.Context(), identity.actor())
		if err != nil {
			return nil, err
		}
		return viewAll(channels, channelView), nil
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
		return channelView(created), nil
	})
	console.add("GET", "/channels/{channel_id}", "getChannel", channelPath{}, ChannelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		found, err := console.Channel.Get(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"))
		if err != nil {
			return nil, err
		}
		return channelView(found), nil
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
		return channelView(updated), nil
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
