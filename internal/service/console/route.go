package console

import (
	"context"

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

// normalize requires every target to name an enabled channel and one of its enabled catalog
// models, with at least one format both sides support. All targets must share a model kind.
func (service *RouteService) normalize(ctx context.Context, input RouteInput) (RouteInput, error) {
	name, err := RequireName(input.Name, "invalid_name")
	if err != nil {
		return input, err
	}
	input.Name = name
	channels, err := service.store.Channels(ctx)
	if err != nil {
		return input, Translate(err)
	}
	models, err := service.store.Models(ctx)
	if err != nil {
		return input, Translate(err)
	}
	if err := validateTargets(input.Targets, channels, models); err != nil {
		return input, err
	}
	return input, nil
}

// validateTargets is shared by mutations and startup validation so both accept the same graph.
func validateTargets(targets []entity.Target, channels []entity.Channel, models []entity.Model) error {
	if len(targets) == 0 || len(targets) > MaximumTargets {
		return Invalid("invalid_targets")
	}
	channelByID := make(map[string]entity.Channel, len(channels))
	for _, channel := range channels {
		channelByID[channel.ID] = channel
	}
	modelByID := modelMap(models)
	kind := ""
	seen := make(map[[2]string]bool, len(targets))
	for _, target := range targets {
		channel, channelFound := channelByID[target.ChannelID]
		model, modelFound := modelByID[target.ModelID]
		if !channelFound || !modelFound || !channel.Enabled || !model.Enabled ||
			!channel.HasModel(model.ID) || channel.ProviderID != model.ProviderID {
			return Invalid("invalid_targets")
		}
		if len(effectiveFormats(channel, model)) == 0 {
			return Invalid("invalid_targets")
		}
		if kind == "" {
			kind = string(model.Kind)
		} else if kind != string(model.Kind) {
			return Invalid("invalid_targets")
		}
		if target.Priority < 0 || target.Weight < 0 || target.Weight > MaximumWeight ||
			target.MaxOutputTokens < 0 || target.MaxOutputTokens > MaximumOutputTokens {
			return Invalid("invalid_targets")
		}
		pair := [2]string{target.ChannelID, target.ModelID}
		if seen[pair] {
			return Invalid("invalid_targets")
		}
		seen[pair] = true
	}
	return nil
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
	configurationMu.Lock()
	defer configurationMu.Unlock()
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
	configurationMu.Lock()
	defer configurationMu.Unlock()
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
	configurationMu.Lock()
	defer configurationMu.Unlock()
	route, err := service.store.Route(ctx, projectID, routeID)
	if err != nil {
		return nil, Translate(err)
	}
	return route, Translate(service.store.DeleteRoute(ctx, projectID, routeID))
}
