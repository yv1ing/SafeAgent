package control

import (
	"context"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/llm/provider"
	"github.com/yv1ing/SafeAgent/internal/platform/routing"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

type ChannelInput struct {
	Name                    string
	ProviderID              string
	BaseURL                 string
	Headers                 map[string]string
	ForwardHeaders          []string
	DefaultMaxOutputTokens  int
	TimeoutSeconds          int
	FirstByteTimeoutSeconds int
	AllowPrivateNetwork     bool
	Enabled                 bool
	Version                 int64
}

// normalize validates the input against the provider catalog and applies defaults. The channel
// inherits a provider endpoint's base URL; an endpoint that declares none needs the channel's base
// URL, which is the normal shape of an operator-supplied provider.
func normalizeChannel(input ChannelInput, spec provider.ProviderSpec) (ChannelInput, error) {
	name, err := access.RequireName(input.Name, "invalid_name")
	if err != nil {
		return input, err
	}
	input.Name = name
	input.ProviderID = strings.TrimSpace(input.ProviderID)
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	if input.BaseURL != "" {
		if !validEndpoint(input.BaseURL) {
			return input, access.Invalid("invalid_endpoint")
		}
	} else {
		for format := range spec.Endpoints {
			if spec.Endpoints[format].BaseURL == "" {
				return input, access.Invalid("invalid_endpoint")
			}
		}
	}
	if input.TimeoutSeconds < 0 || input.TimeoutSeconds > MaximumTimeoutSeconds ||
		input.FirstByteTimeoutSeconds < 0 || input.FirstByteTimeoutSeconds > MaximumFirstByteSeconds {
		return input, access.Invalid("invalid_timeout")
	}
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = DefaultTimeoutSeconds
	}
	if input.FirstByteTimeoutSeconds == 0 {
		input.FirstByteTimeoutSeconds = DefaultFirstByteTimeoutSeconds
	}
	if input.Headers, err = validateHeaders(input.Headers); err != nil {
		return input, err
	}
	if input.ForwardHeaders, err = validateForwardHeaders(input.ForwardHeaders); err != nil {
		return input, err
	}
	if input.DefaultMaxOutputTokens < 0 || input.DefaultMaxOutputTokens > MaximumOutputTokens {
		return input, access.Invalid("invalid_limit")
	}
	return input, nil
}

// channelEntity is the stored projection of a normalized channel; create, update and connect share it
// so an input field cannot be dropped on one path only.
func channelEntity(input ChannelInput) *entity.Channel {
	return &entity.Channel{
		Name: input.Name, ProviderID: input.ProviderID, BaseURL: input.BaseURL,
		Headers: input.Headers, ForwardHeaders: input.ForwardHeaders,
		DefaultMaxOutputTokens: input.DefaultMaxOutputTokens, TimeoutSeconds: input.TimeoutSeconds,
		FirstByteTimeoutSeconds: input.FirstByteTimeoutSeconds,
		AllowPrivateNetwork:     input.AllowPrivateNetwork, Enabled: input.Enabled,
	}
}

func (service *Service) Channels(ctx context.Context, actor access.Actor, includeDeleted bool) ([]ChannelInfo, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	records, err := service.store.Channels(ctx, includeDeleted)
	if err != nil {
		return nil, access.Translate(err)
	}
	infos := make([]ChannelInfo, 0, len(records))
	for _, record := range records {
		infos = append(infos, channelInfo(record))
	}
	return infos, nil
}

func (service *Service) Channel(ctx context.Context, actor access.Actor, id string) (*ChannelInfo, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	record, err := service.store.Channel(ctx, id)
	if err != nil {
		return nil, access.Translate(err)
	}
	info := channelInfo(*record)
	return &info, nil
}

func (service *Service) CreateChannel(ctx context.Context, actor access.Actor, input ChannelInput) (*ChannelInfo, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	spec, err := service.spec(input.ProviderID)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeChannel(input, spec)
	if err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	created, err := service.store.CreateChannel(ctx, channelEntity(normalized))
	if err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, access.Conflict("channel_name_taken")
		}
		return nil, access.Translate(err)
	}
	info := channelInfo(*created)
	return &info, nil
}

func (service *Service) UpdateChannel(ctx context.Context, actor access.Actor, id string, input ChannelInput) (*ChannelInfo, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	if err := access.RequireVersion(input.Version); err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	existing, err := service.store.Channel(ctx, id)
	if err != nil {
		return nil, access.Translate(err)
	}
	if input.Headers == nil {
		input.Headers = existing.Headers
	} else {
		input.Headers = mergeHeaders(existing.Headers, input.Headers)
	}
	spec, err := service.spec(input.ProviderID)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeChannel(input, spec)
	if err != nil {
		return nil, err
	}
	updated, err := service.store.UpdateChannel(ctx, id, *channelEntity(normalized), input.Version)
	if err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, access.Conflict("channel_name_taken")
		}
		return nil, access.Translate(err)
	}
	info := channelInfo(*updated)
	return &info, nil
}

// DeleteChannel refuses while routes still point at the channel unless the operator acknowledged the
// impact; detachTargets decides whether those targets are removed or merely disabled.
func (service *Service) DeleteChannel(ctx context.Context, actor access.Actor, id string, acknowledge, detachTargets bool) error {
	if err := actor.RequireAdmin(); err != nil {
		return err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	references, err := service.store.RoutesReferencingChannel(ctx, id)
	if err != nil {
		return access.Translate(err)
	}
	if len(references) > 0 && !acknowledge {
		return access.Conflict("impact_requires_acknowledge")
	}
	return access.Translate(service.store.DeleteChannel(ctx, id, detachTargets))
}

func (service *Service) RestoreChannel(ctx context.Context, actor access.Actor, id string) (*ChannelInfo, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	restored, err := service.store.RestoreChannel(ctx, id)
	if err != nil {
		return nil, access.Translate(err)
	}
	info := channelInfo(*restored)
	return &info, nil
}

func (service *Service) DetachChannelTargets(ctx context.Context, actor access.Actor, id string) (int, error) {
	if err := actor.RequireAdmin(); err != nil {
		return 0, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	detached, err := service.store.DetachRouteTargetsOfChannel(ctx, id)
	return detached, access.Translate(err)
}

func (service *Service) ChannelKeys(ctx context.Context, actor access.Actor, channelID string) ([]entity.ChannelKey, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	if _, err := service.store.Channel(ctx, channelID); err != nil {
		return nil, access.Translate(err)
	}
	keys, err := service.store.ChannelKeys(ctx, channelID)
	return keys, access.Translate(err)
}

func (service *Service) AddChannelKey(ctx context.Context, actor access.Actor, channelID, label, secret string) (*entity.ChannelKey, error) {
	if err := actor.RequireAdmin(); err != nil {
		return nil, err
	}
	label, err := access.OptionalText(label, MaximumKeyLabelLength, "invalid_label")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(secret) == "" || len(secret) > MaximumSecretLength || strings.ContainsAny(secret, "\r\n\x00") {
		return nil, access.Invalid("invalid_secret")
	}
	if _, err := service.store.Channel(ctx, channelID); err != nil {
		return nil, access.Translate(err)
	}
	plain := strings.TrimSpace(secret)
	sealed, err := secure.Seal(service.store.Secret, []byte(plain))
	if err != nil {
		return nil, err
	}
	suffix := "****"
	if len(plain) > 4 {
		suffix = plain[len(plain)-4:]
	}
	key, err := service.store.CreateChannelKey(ctx, channelID, label, sealed, suffix)
	return key, access.Translate(err)
}

func (service *Service) DeleteChannelKey(ctx context.Context, actor access.Actor, channelID, keyID string) error {
	if err := actor.RequireAdmin(); err != nil {
		return err
	}
	return access.Translate(service.store.DeleteChannelKey(ctx, channelID, keyID))
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

func (service *Service) ChannelHealth(ctx context.Context, actor access.Actor, channelID string) ([]ChannelHealth, error) {
	channel, err := service.Channel(ctx, actor, channelID)
	if err != nil {
		return nil, err
	}
	targets, err := service.store.RouteTargetsOfChannel(ctx, channelID)
	if err != nil {
		return nil, access.Translate(err)
	}
	seen := map[[2]string]bool{}
	health := []ChannelHealth{}
	for _, target := range targets {
		spec, found := service.providers.Get(target.Channel.ProviderID)
		if !found {
			continue
		}
		formats := provider.FormatsForKind(spec.Endpoints, string(target.Model.Kind))
		if target.Target.Format != "" {
			formats = []model.Format{model.Format(target.Target.Format)}
		}
		for _, format := range formats {
			key := [2]string{target.Model.ID, string(format)}
			if seen[key] {
				continue
			}
			seen[key] = true
			health = append(health, ChannelHealth{
				ChannelID: channel.ID, ChannelName: channel.Name, ModelID: target.Model.ID,
				ModelName: target.Model.UpstreamName, Format: string(format),
				Health: service.selector.Health(channel.ID, target.Model.ID, string(format)),
			})
		}
	}
	return health, nil
}

// ModelOption is one catalog model a route target may select.
type ModelOption struct {
	ID          string
	Upstream    string
	DisplayName string
	Kind        string
	Formats     []string
}

// ChannelOption is what a route editor needs to point a target at a channel and model.
type ChannelOption struct {
	ID            string
	Name          string
	ProviderID    string
	ProviderLabel string
	Formats       []string
	Models        []ModelOption
}

func (service *Service) ChannelOptions(ctx context.Context, actor access.Actor, projectID string) ([]ChannelOption, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	channels, err := service.store.Channels(ctx, false)
	if err != nil {
		return nil, access.Translate(err)
	}
	models, err := service.store.Models(ctx, repository.ModelFilter{})
	if err != nil {
		return nil, access.Translate(err)
	}
	options := make([]ChannelOption, 0, len(channels))
	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		spec, found := service.providers.Get(channel.ProviderID)
		if !found {
			continue
		}
		channelFormats := provider.FormatsForKind(spec.Endpoints, provider.KindGeneration)
		option := ChannelOption{
			ID: channel.ID, Name: channel.Name, ProviderID: channel.ProviderID,
			ProviderLabel: channel.ProviderLabel, Formats: formatNames(channelFormats),
			Models: []ModelOption{},
		}
		for _, candidate := range models {
			if !candidate.Enabled || candidate.ProviderID != channel.ProviderID {
				continue
			}
			formats := deriveModelFormats(spec, candidate)
			if len(formats) == 0 {
				continue
			}
			option.Models = append(option.Models, ModelOption{
				ID: candidate.ID, Upstream: candidate.UpstreamName, DisplayName: candidate.DisplayName,
				Kind: string(candidate.Kind), Formats: formatNames(formats),
			})
		}
		if len(option.Models) > 0 {
			options = append(options, option)
		}
	}
	return options, nil
}

func channelEndpointBase(channel entity.Channel, endpoint provider.EndpointSpec) string {
	if channel.BaseURL != "" {
		return channel.BaseURL
	}
	return endpoint.BaseURL
}
