package repository

import (
	"context"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// RouteTarget is one route candidate with the rows its channel id, model id and provider id stand
// for, so the runtime never re-joins the catalog on the request path.
type RouteTarget struct {
	Target   entity.RouteTarget
	Channel  entity.Channel
	Model    entity.Model
	Provider entity.Provider
}

// RouteReference says which routes a channel or model removal affects.
type RouteReference struct {
	RouteID     string `bun:"route_id" json:"route_id"`
	RouteName   string `bun:"route_name" json:"route_name"`
	ProjectID   string `bun:"project_id" json:"project_id"`
	TargetCount int    `bun:"target_count" json:"target_count"`
}

// RouteTargets lists every target of a route, including disabled and dangling ones, so the editor
// can show what a route still points at.
func (store *Store) RouteTargets(ctx context.Context, routeID string) ([]RouteTarget, error) {
	targets := []entity.RouteTarget{}
	if err := store.DB.NewSelect().Model(&targets).Where("route_id = ?", routeID).
		Order("priority", "created_at", "id").Scan(ctx); err != nil {
		return nil, err
	}
	return store.attachRouteTargets(ctx, targets)
}

// RouteCandidates is the runtime view: an enabled route, enabled targets, and a channel, model and
// provider that are themselves usable.
func (store *Store) RouteCandidates(ctx context.Context, routeID string) ([]RouteTarget, error) {
	targets := []entity.RouteTarget{}
	err := store.DB.NewRaw(`SELECT rt.* FROM route_targets rt
		JOIN model_routes r ON r.id = rt.route_id AND r.enabled AND r.deleted_at IS NULL
		JOIN channels ch ON ch.id = rt.channel_id AND ch.enabled AND ch.deleted_at IS NULL
		JOIN models m ON m.id = rt.model_id AND m.enabled AND m.deleted_at IS NULL
		JOIN providers pr ON pr.id = ch.provider_id AND pr.enabled AND pr.deleted_at IS NULL
		WHERE rt.route_id = ? AND rt.enabled
		ORDER BY rt.priority, rt.created_at, rt.id`, routeID).Scan(ctx, &targets)
	if err != nil {
		return nil, err
	}
	return store.attachRouteTargets(ctx, targets)
}

func (store *Store) attachRouteTargets(ctx context.Context, targets []entity.RouteTarget) ([]RouteTarget, error) {
	result := make([]RouteTarget, 0, len(targets))
	if len(targets) == 0 {
		return result, nil
	}
	channelIDs := make([]string, 0, len(targets))
	modelIDs := make([]string, 0, len(targets))
	seenChannels := map[string]bool{}
	seenModels := map[string]bool{}
	for _, target := range targets {
		if !seenChannels[target.ChannelID] {
			seenChannels[target.ChannelID] = true
			channelIDs = append(channelIDs, target.ChannelID)
		}
		if !seenModels[target.ModelID] {
			seenModels[target.ModelID] = true
			modelIDs = append(modelIDs, target.ModelID)
		}
	}
	channels := []entity.Channel{}
	if err := store.DB.NewSelect().Model(&channels).Where("id IN (?)", bun.In(channelIDs)).Scan(ctx); err != nil {
		return nil, err
	}
	models := []entity.Model{}
	if err := store.DB.NewSelect().Model(&models).Where("id IN (?)", bun.In(modelIDs)).Scan(ctx); err != nil {
		return nil, err
	}
	providerIDs := make([]string, 0, len(channels))
	seenProviders := map[string]bool{}
	for _, channel := range channels {
		if !seenProviders[channel.ProviderID] {
			seenProviders[channel.ProviderID] = true
			providerIDs = append(providerIDs, channel.ProviderID)
		}
	}
	providers, err := store.providerRows(ctx, providerIDs)
	if err != nil {
		return nil, err
	}
	channelByID := make(map[string]entity.Channel, len(channels))
	for _, channel := range channels {
		channelByID[channel.ID] = channel
	}
	modelByID := make(map[string]entity.Model, len(models))
	for _, model := range models {
		modelByID[model.ID] = model
	}
	for _, target := range targets {
		channel := channelByID[target.ChannelID]
		result = append(result, RouteTarget{
			Target: target, Channel: channel, Model: modelByID[target.ModelID],
			Provider: providers[channel.ProviderID],
		})
	}
	return result, nil
}

// RouteTargetsOfChannel lists the target rows of one channel with their model and provider data; the
// channel health view needs the model ids and formats a channel currently serves.
func (store *Store) RouteTargetsOfChannel(ctx context.Context, channelID string) ([]RouteTarget, error) {
	targets := []entity.RouteTarget{}
	if err := store.DB.NewSelect().Model(&targets).Where("channel_id = ?", channelID).
		Order("created_at", "id").Scan(ctx); err != nil {
		return nil, err
	}
	return store.attachRouteTargets(ctx, targets)
}

func (store *Store) RoutesReferencingChannel(ctx context.Context, channelID string) ([]RouteReference, error) {
	references := []RouteReference{}
	err := store.DB.NewRaw(`SELECT r.id AS route_id, r.name AS route_name, r.project_id, count(rt.id) AS target_count
		FROM model_routes r JOIN route_targets rt ON rt.route_id = r.id
		WHERE r.deleted_at IS NULL AND rt.channel_id = ?
		GROUP BY 1, 2, 3 ORDER BY r.project_id, r.name`, channelID).Scan(ctx, &references)
	return references, err
}

func (store *Store) RoutesReferencingModel(ctx context.Context, modelID string) ([]RouteReference, error) {
	references := []RouteReference{}
	err := store.DB.NewRaw(`SELECT r.id AS route_id, r.name AS route_name, r.project_id, count(rt.id) AS target_count
		FROM model_routes r JOIN route_targets rt ON rt.route_id = r.id
		WHERE r.deleted_at IS NULL AND rt.model_id = ?
		GROUP BY 1, 2, 3 ORDER BY r.project_id, r.name`, modelID).Scan(ctx, &references)
	return references, err
}

func (store *Store) DetachRouteTargetsOfChannel(ctx context.Context, channelID string) (int, error) {
	return store.detachTargets(ctx, "channel_id", channelID)
}

func (store *Store) DetachRouteTargetsOfModel(ctx context.Context, modelID string) (int, error) {
	return store.detachTargets(ctx, "model_id", modelID)
}

func (store *Store) detachTargets(ctx context.Context, column, id string) (int, error) {
	result, err := store.DB.NewDelete().Model((*entity.RouteTarget)(nil)).
		Where(column+" = ?", id).Exec(ctx)
	if err != nil {
		return 0, err
	}
	affected, _ := result.RowsAffected()
	return int(affected), nil
}
