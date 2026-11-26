package console

import (
	"github.com/gin-gonic/gin"
	control "github.com/yv1ing/SafeAgent/internal/service/control"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

type CapabilityOverrideView struct {
	ModelID string `json:"model_id" required:"true"`
	Format  string `json:"format" required:"true"`
	Field   string `json:"field" required:"true"`
	Reason  string `json:"reason,omitempty"`
}

type ChannelView struct {
	ID                      string                   `json:"id" required:"true"`
	Name                    string                   `json:"name" required:"true"`
	ProviderID              string                   `json:"provider_id" required:"true"`
	ProviderLabel           string                   `json:"provider_label" required:"true"`
	BaseURL                 string                   `json:"base_url"`
	Headers                 map[string]string        `json:"headers"`
	ForwardHeaders          []string                 `json:"forward_headers"`
	DefaultMaxOutputTokens  int                      `json:"default_max_output_tokens"`
	TimeoutSeconds          int                      `json:"timeout_seconds"`
	FirstByteTimeoutSeconds int                      `json:"first_byte_timeout_seconds"`
	AllowPrivateNetwork     bool                     `json:"allow_private_network"`
	Enabled                 bool                     `json:"enabled"`
	Version                 int64                    `json:"version" required:"true"`
	RouteCount              int                      `json:"route_count" required:"true"`
	CapabilityOverrides     []CapabilityOverrideView `json:"capability_overrides"`
	CreatedAt               string                   `json:"created_at" required:"true"`
	UpdatedAt               string                   `json:"updated_at" required:"true"`
	DeletedAt               string                   `json:"deleted_at,omitempty"`
}

type ChannelInput struct {
	Name                    string            `json:"name" required:"true"`
	ProviderID              string            `json:"provider_id" required:"true"`
	BaseURL                 string            `json:"base_url" description:"Host for provider endpoints that declare none; empty inherits every provider host"`
	Headers                 map[string]string `json:"headers"`
	ForwardHeaders          []string          `json:"forward_headers"`
	DefaultMaxOutputTokens  int               `json:"default_max_output_tokens" description:"Applied when an upstream requires an output limit and the client omits one"`
	TimeoutSeconds          int               `json:"timeout_seconds"`
	FirstByteTimeoutSeconds int               `json:"first_byte_timeout_seconds"`
	AllowPrivateNetwork     bool              `json:"allow_private_network"`
	Enabled                 bool              `json:"enabled"`
	Version                 int64             `json:"version" description:"Required on update, ignored on create"`
}

type channelPath struct {
	ChannelID string `path:"channel_id" json:"-" required:"true"`
}

type updateChannelInput struct {
	channelPath
	ChannelInput
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

type createChannelKeyInput struct {
	channelPath
	ChannelKeyInput
}

type channelKeyPath struct {
	ChannelID string `path:"channel_id" json:"-" required:"true"`
	KeyID     string `path:"key_id" json:"-" required:"true"`
}

type DiscoveryInput struct {
	Format string `json:"format" required:"true"`
}

type DiscoveryView struct {
	Models []string `json:"models"`
}

type ImportModelsInput struct {
	Format string   `json:"format" required:"true"`
	Models []string `json:"models" required:"true"`
}

type ImportModelsView struct {
	Created []string `json:"created"`
	Skipped []string `json:"skipped"`
}

type ChannelModelOptionView struct {
	ID          string   `json:"id" required:"true"`
	Upstream    string   `json:"upstream_name" required:"true"`
	DisplayName string   `json:"display_name,omitempty"`
	Kind        string   `json:"kind" required:"true" enum:"generation,embedding,moderation"`
	Formats     []string `json:"formats"`
}

type ChannelOptionView struct {
	ID            string                   `json:"id" required:"true"`
	Name          string                   `json:"name" required:"true"`
	ProviderID    string                   `json:"provider_id" required:"true"`
	ProviderLabel string                   `json:"provider_label" required:"true"`
	Formats       []string                 `json:"formats"`
	Models        []ChannelModelOptionView `json:"models"`
}

type HealthView struct {
	ChannelID           string `json:"channel_id" required:"true"`
	ChannelName         string `json:"channel_name" required:"true"`
	ModelID             string `json:"model_id" required:"true"`
	ModelName           string `json:"model_name" required:"true"`
	Format              string `json:"format" required:"true"`
	State               string `json:"state" required:"true" enum:"closed,open,half_open"`
	ConsecutiveFailures int    `json:"consecutive_failures" required:"true"`
	CooldownSeconds     int64  `json:"cooldown_seconds" required:"true"`
}

func (input ChannelInput) service() control.ChannelInput {
	return control.ChannelInput{
		Name: input.Name, ProviderID: input.ProviderID, BaseURL: input.BaseURL,
		Headers: input.Headers, ForwardHeaders: input.ForwardHeaders,
		DefaultMaxOutputTokens: input.DefaultMaxOutputTokens, TimeoutSeconds: input.TimeoutSeconds,
		FirstByteTimeoutSeconds: input.FirstByteTimeoutSeconds,
		AllowPrivateNetwork:     input.AllowPrivateNetwork, Enabled: input.Enabled, Version: input.Version,
	}
}

func channelOverrideViews(overrides []entity.CapabilityOverride) []CapabilityOverrideView {
	result := make([]CapabilityOverrideView, 0, len(overrides))
	for _, entry := range overrides {
		result = append(result, CapabilityOverrideView{ModelID: entry.ModelID, Format: entry.Format, Field: entry.Field, Reason: entry.Reason})
	}
	return result
}

func channelView(value *control.ChannelInfo) ChannelView {
	return ChannelView{
		ID: value.ID, Name: value.Name, ProviderID: value.ProviderID, ProviderLabel: value.ProviderLabel,
		BaseURL: value.BaseURL, Headers: control.MaskHeaders(value.Headers), ForwardHeaders: nonNil(value.ForwardHeaders),
		DefaultMaxOutputTokens: value.DefaultMaxOutputTokens, TimeoutSeconds: value.TimeoutSeconds,
		FirstByteTimeoutSeconds: value.FirstByteTimeoutSeconds, AllowPrivateNetwork: value.AllowPrivateNetwork,
		Enabled: value.Enabled, Version: value.Version, RouteCount: value.RouteCount,
		CapabilityOverrides: channelOverrideViews(value.CapabilityOverrides),
		CreatedAt:           stamp(value.CreatedAt), UpdatedAt: stamp(value.UpdatedAt), DeletedAt: stampOptional(value.DeletedAt),
	}
}

func channelKeyView(key *entity.ChannelKey) ChannelKeyView {
	return ChannelKeyView{ID: key.ID, Label: key.Label, Suffix: key.Suffix, Enabled: key.Enabled}
}

func channelOptionView(value *control.ChannelOption) ChannelOptionView {
	models := make([]ChannelModelOptionView, 0, len(value.Models))
	for _, model := range value.Models {
		models = append(models, ChannelModelOptionView{
			ID: model.ID, Upstream: model.Upstream, DisplayName: model.DisplayName,
			Kind: model.Kind, Formats: nonNil(model.Formats),
		})
	}
	return ChannelOptionView{
		ID: value.ID, Name: value.Name, ProviderID: value.ProviderID,
		ProviderLabel: value.ProviderLabel, Formats: nonNil(value.Formats), Models: models,
	}
}

func healthView(value *control.ChannelHealth) HealthView {
	return HealthView{
		ChannelID: value.ChannelID, ChannelName: value.ChannelName, ModelID: value.ModelID,
		ModelName: value.ModelName, Format: value.Format, State: string(value.Health.State),
		ConsecutiveFailures: value.Health.Failures, CooldownSeconds: value.Health.Cooldown,
	}
}

func (console *Console) registerChannels() {
	console.add("GET", "/channels", "listChannels", nil, []ChannelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		includeDeleted := ctx.Query("include_deleted") == "true"
		records, err := console.Control.Channels(ctx.Request.Context(), identity.actor(), includeDeleted)
		if err != nil {
			return nil, err
		}
		return viewAll(records, channelView), nil
	})
	console.add("POST", "/channels", "createChannel", ChannelInput{}, ChannelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input ChannelInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		created, err := console.Control.CreateChannel(ctx.Request.Context(), identity.actor(), input.service())
		if err != nil {
			return nil, err
		}
		return channelView(created), nil
	})
	console.add("GET", "/channels/{channel_id}", "getChannel", channelPath{}, ChannelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		record, err := console.Control.Channel(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"))
		if err != nil {
			return nil, err
		}
		return channelView(record), nil
	})
	console.add("PUT", "/channels/{channel_id}", "updateChannel", updateChannelInput{}, ChannelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input ChannelInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		updated, err := console.Control.UpdateChannel(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"), input.service())
		if err != nil {
			return nil, err
		}
		return channelView(updated), nil
	})
	console.add("DELETE", "/channels/{channel_id}", "deleteChannel", channelPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		acknowledge := ctx.Query("acknowledge") == "true"
		detachTargets := ctx.Query("detach_targets") == "true"
		return nil, console.Control.DeleteChannel(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"), acknowledge, detachTargets)
	})
	console.add("POST", "/channels/{channel_id}/restore", "restoreChannel", channelPath{}, ChannelView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		restored, err := console.Control.RestoreChannel(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"))
		if err != nil {
			return nil, err
		}
		return channelView(restored), nil
	})
	console.add("DELETE", "/channels/{channel_id}/targets", "detachChannelTargets", channelPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		_, err := console.Control.DetachChannelTargets(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"))
		return nil, err
	})
	console.add("GET", "/channels/{channel_id}/health", "getChannelHealth", channelPath{}, []HealthView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		health, err := console.Control.ChannelHealth(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"))
		if err != nil {
			return nil, err
		}
		return viewAll(health, healthView), nil
	})
	console.add("GET", "/channels/{channel_id}/keys", "listChannelKeys", channelPath{}, []ChannelKeyView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		keys, err := console.Control.ChannelKeys(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"))
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
		created, err := console.Control.AddChannelKey(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"), input.Label, input.Secret)
		if err != nil {
			return nil, err
		}
		return channelKeyView(created), nil
	})
	console.add("DELETE", "/channels/{channel_id}/keys/{key_id}", "deleteChannelKey", channelKeyPath{}, nil, func(ctx *gin.Context, identity *Identity) (any, error) {
		return nil, console.Control.DeleteChannelKey(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"), ctx.Param("key_id"))
	})
	console.add("POST", "/channels/{channel_id}/discover-models", "discoverChannelModels", struct {
		channelPath
		DiscoveryInput
	}{}, DiscoveryView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input DiscoveryInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		models, err := console.Control.DiscoverModels(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"), input.Format)
		if err != nil {
			return nil, err
		}
		return DiscoveryView{Models: nonNil(models)}, nil
	})
	console.add("POST", "/channels/{channel_id}/import-models", "importChannelModels", struct {
		channelPath
		ImportModelsInput
	}{}, ImportModelsView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		var input ImportModelsInput
		if err := decode(ctx, &input); err != nil {
			return nil, err
		}
		result, err := console.Control.ImportModels(ctx.Request.Context(), identity.actor(), ctx.Param("channel_id"), input.Format, input.Models)
		if err != nil {
			return nil, err
		}
		return ImportModelsView{Created: nonNil(result.Created), Skipped: nonNil(result.Skipped)}, nil
	})
	console.add("GET", "/projects/{project_id}/channel-options", "listChannelOptions", projectPath{}, []ChannelOptionView{}, func(ctx *gin.Context, identity *Identity) (any, error) {
		options, err := console.Control.ChannelOptions(ctx.Request.Context(), identity.actor(), ctx.Param("project_id"))
		if err != nil {
			return nil, err
		}
		return viewAll(options, channelOptionView), nil
	})
}
