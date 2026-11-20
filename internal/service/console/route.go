package console

import (
	"context"
	"slices"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
	"github.com/yv1ing/SafeAgent/internal/storage/repository"
)

const (
	MaximumTargets = 32
	MaximumWeight  = 1000
)

type RouteService struct {
	store *repository.Store
}

func NewRouteService(store *repository.Store) *RouteService { return &RouteService{store: store} }

type RouteInput struct {
	Name    string
	Targets []entity.Target
	Version int64
}

// normalize requires every target to name an enabled channel that lists the upstream model.
func (service *RouteService) normalize(ctx context.Context, input RouteInput) (RouteInput, error) {
	name, err := RequireName(input.Name, "invalid_name")
	if err != nil {
		return input, err
	}
	input.Name = name
	if len(input.Targets) == 0 || len(input.Targets) > MaximumTargets {
		return input, Invalid("invalid_targets")
	}
	channels, err := service.store.Channels(ctx)
	if err != nil {
		return input, Translate(err)
	}
	models := make(map[string][]string, len(channels))
	for _, channel := range channels {
		if !channel.Enabled {
			continue
		}
		for _, model := range channel.Models {
			models[channel.ID] = append(models[channel.ID], model.Name)
		}
	}
	targets := make([]entity.Target, 0, len(input.Targets))
	for _, target := range input.Targets {
		if !slices.Contains(models[target.ChannelID], target.UpstreamModel) {
			return input, Invalid("invalid_targets")
		}
		if target.Priority < 0 || target.Weight < 0 || target.Weight > MaximumWeight {
			return input, Invalid("invalid_targets")
		}
		if slices.ContainsFunc(targets, func(seen entity.Target) bool {
			return seen.ChannelID == target.ChannelID && seen.UpstreamModel == target.UpstreamModel
		}) {
			return input, Invalid("invalid_targets")
		}
		targets = append(targets, target)
	}
	input.Targets = targets
	return input, nil
}

func conflictOnDuplicate(err error) error {
	if repository.IsUniqueViolation(err) {
		return Conflict("route_name_taken")
	}
	return Translate(err)
}

func (service *RouteService) List(ctx context.Context, actor Actor, projectID string) ([]entity.Route, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	return service.store.Routes(ctx, projectID)
}

func (service *RouteService) Create(ctx context.Context, actor Actor, projectID string, input RouteInput) (*entity.Route, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	normalized, err := service.normalize(ctx, input)
	if err != nil {
		return nil, err
	}
	created, err := service.store.CreateRoute(ctx, projectID, normalized.Name, normalized.Targets)
	return created, conflictOnDuplicate(err)
}

func (service *RouteService) Update(ctx context.Context, actor Actor, projectID, routeID string, input RouteInput) (*entity.Route, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	if err := RequireVersion(input.Version); err != nil {
		return nil, err
	}
	normalized, err := service.normalize(ctx, input)
	if err != nil {
		return nil, err
	}
	updated, err := service.store.UpdateRoute(ctx, projectID, routeID, normalized.Name, normalized.Targets, normalized.Version)
	return updated, conflictOnDuplicate(err)
}

func (service *RouteService) Delete(ctx context.Context, actor Actor, projectID, routeID string) (*entity.Route, error) {
	if err := actor.RequireProject(ctx, service.store, projectID); err != nil {
		return nil, err
	}
	route, err := service.store.Route(ctx, projectID, routeID)
	if err != nil {
		return nil, Translate(err)
	}
	return route, Translate(service.store.DeleteRoute(ctx, projectID, routeID))
}
