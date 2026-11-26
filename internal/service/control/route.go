package control

import (
	"context"
	"errors"
	"strings"

	"github.com/yv1ing/SafeAgent/internal/llm/model"
	"github.com/yv1ing/SafeAgent/internal/service/access"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

// RouteTargetInput is one failover candidate of a route.
type RouteTargetInput struct {
	ChannelID       string
	ModelID         string
	Format          string
	Priority        int
	Weight          int
	MaxOutputTokens int
	Enabled         *bool
}

type RouteInput struct {
	Name    string
	Enabled *bool
	Targets []RouteTargetInput
	Aliases []string
	Version int64
}

func normalizeAliases(name string, aliases []string) ([]string, error) {
	if len(aliases) > MaximumAliases {
		return nil, access.Invalid("invalid_aliases")
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		normalized, err := access.RequireName(alias, "invalid_aliases")
		if err != nil {
			return nil, err
		}
		if normalized == name || seen[normalized] {
			return nil, access.Invalid("invalid_aliases")
		}
		seen[normalized] = true
		clean = append(clean, normalized)
	}
	return clean, nil
}

func (service *Service) normalizeRoute(ctx context.Context, input RouteInput) (string, []entity.RouteTarget, []string, error) {
	name, err := access.RequireName(input.Name, "invalid_name")
	if err != nil {
		return "", nil, nil, err
	}
	aliases, err := normalizeAliases(name, input.Aliases)
	if err != nil {
		return "", nil, nil, err
	}
	if len(input.Targets) == 0 || len(input.Targets) > MaximumTargets {
		return "", nil, nil, access.Invalid("invalid_targets")
	}
	channels, err := service.store.Channels(ctx, false)
	if err != nil {
		return "", nil, nil, access.Translate(err)
	}
	models, err := service.store.Models(ctx, repository.ModelFilter{})
	if err != nil {
		return "", nil, nil, access.Translate(err)
	}
	channelByID := make(map[string]repository.ChannelRecord, len(channels))
	for _, channel := range channels {
		channelByID[channel.ID] = channel
	}
	modelByID := make(map[string]entity.Model, len(models))
	for _, model := range models {
		modelByID[model.ID] = model
	}
	targets := make([]entity.RouteTarget, 0, len(input.Targets))
	kind := ""
	seen := map[[3]string]bool{}
	for _, candidate := range input.Targets {
		channel, channelFound := channelByID[candidate.ChannelID]
		catalogModel, modelFound := modelByID[candidate.ModelID]
		if !channelFound || !modelFound || channel.ProviderID != catalogModel.ProviderID {
			return "", nil, nil, access.Invalid("invalid_targets")
		}
		spec, found := service.providers.Get(channel.ProviderID)
		if !found {
			return "", nil, nil, access.Invalid("invalid_targets")
		}
		formats := formatSet(deriveModelFormats(spec, catalogModel))
		if len(formats) == 0 {
			return "", nil, nil, access.Invalid("invalid_targets")
		}
		format := strings.TrimSpace(candidate.Format)
		if format != "" {
			endpoint, known := spec.Endpoints[model.Format(format)]
			if !known || endpoint.Format == "" || !formats[format] {
				return "", nil, nil, access.Invalid("invalid_targets")
			}
		}
		if kind == "" {
			kind = string(catalogModel.Kind)
		} else if kind != string(catalogModel.Kind) {
			return "", nil, nil, access.Invalid("invalid_targets")
		}
		if candidate.Priority < 0 || candidate.Weight < 0 || candidate.Weight > MaximumWeight ||
			candidate.MaxOutputTokens < 0 || candidate.MaxOutputTokens > MaximumOutputTokens {
			return "", nil, nil, access.Invalid("invalid_targets")
		}
		key := [3]string{candidate.ChannelID, candidate.ModelID, format}
		if seen[key] {
			return "", nil, nil, access.Invalid("invalid_targets")
		}
		seen[key] = true
		enabled := true
		if candidate.Enabled != nil {
			enabled = *candidate.Enabled
		}
		targets = append(targets, entity.RouteTarget{
			ChannelID: candidate.ChannelID, ModelID: candidate.ModelID, Format: format,
			Priority: candidate.Priority, Weight: candidate.Weight,
			MaxOutputTokens: candidate.MaxOutputTokens, Enabled: enabled,
		})
	}
	return name, targets, aliases, nil
}

func (service *Service) Routes(ctx context.Context, actor access.Actor, projectID string, includeDeleted bool) ([]RouteInfo, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	routes, err := service.store.Routes(ctx, projectID, includeDeleted)
	if err != nil {
		return nil, access.Translate(err)
	}
	infos := make([]RouteInfo, 0, len(routes))
	for _, route := range routes {
		infos = append(infos, routeInfo(route))
	}
	return infos, nil
}

func (service *Service) Route(ctx context.Context, actor access.Actor, projectID, routeID string) (*RouteInfo, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	route, err := service.store.Route(ctx, projectID, routeID)
	if err != nil {
		return nil, access.Translate(err)
	}
	info := routeInfo(*route)
	return &info, nil
}

func (service *Service) CreateRoute(ctx context.Context, actor access.Actor, projectID string, input RouteInput) (*RouteInfo, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	name, targets, aliases, err := service.normalizeRoute(ctx, input)
	if err != nil {
		return nil, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	created, err := service.store.CreateRoute(ctx, &entity.Route{ProjectID: projectID, Name: name, Enabled: enabled}, targets, aliases)
	if err != nil {
		return nil, translateRouteConflict(err)
	}
	info := routeInfo(*created)
	return &info, nil
}

func (service *Service) UpdateRoute(ctx context.Context, actor access.Actor, projectID, routeID string, input RouteInput) (*RouteInfo, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	if err := access.RequireVersion(input.Version); err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	existing, err := service.store.Route(ctx, projectID, routeID)
	if err != nil {
		return nil, access.Translate(err)
	}
	name, targets, aliases, err := service.normalizeRoute(ctx, input)
	if err != nil {
		return nil, err
	}
	enabled := existing.Route.Enabled
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	updated, err := service.store.UpdateRoute(ctx, projectID, routeID, name, targets, aliases, enabled, input.Version)
	if err != nil {
		return nil, translateRouteConflict(err)
	}
	info := routeInfo(*updated)
	return &info, nil
}

func (service *Service) DeleteRoute(ctx context.Context, actor access.Actor, projectID, routeID string) error {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	return access.Translate(service.store.DeleteRoute(ctx, projectID, routeID))
}

func (service *Service) RestoreRoute(ctx context.Context, actor access.Actor, projectID, routeID string) (*RouteInfo, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	restored, err := service.store.RestoreRoute(ctx, projectID, routeID)
	if err != nil {
		return nil, translateRouteConflict(err)
	}
	info := routeInfo(*restored)
	return &info, nil
}

func translateRouteConflict(err error) error {
	var conflict *repository.RouteNameConflictError
	if errors.As(err, &conflict) {
		return access.Conflict("route_name_taken")
	}
	return access.Translate(err)
}
