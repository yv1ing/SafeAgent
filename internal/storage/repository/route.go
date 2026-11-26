package repository

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// RouteRecord is one route with its target rows and aliases; the console editor needs all three
// together and the runtime never needs to re-read them piecemeal.
type RouteRecord struct {
	Route   entity.Route
	Targets []RouteTarget
	Aliases []entity.RouteAlias
}

// RouteResolution is the outcome of resolving a client model name: the route, the targets the
// runtime may try, and which rule matched.
type RouteResolution struct {
	Route      entity.Route
	Targets    []RouteTarget
	Resolution string
}

const (
	ResolutionExact    = "exact"
	ResolutionAlias    = "alias"
	ResolutionExplicit = "explicit"
	ResolutionFallback = "fallback"
)

func (store *Store) CreateRoute(ctx context.Context, route *entity.Route, targets []entity.RouteTarget, aliases []string) (*RouteRecord, error) {
	route.ID = secure.Random(12)
	route.Version = 1
	route.CreatedAt = time.Now().UTC()
	route.UpdatedAt = route.CreatedAt
	route.DeletedAt = nil
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		if _, err := transaction.NewInsert().Model(route).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return &RouteNameConflictError{Name: route.Name}
			}
			return err
		}
		if err := replaceTargets(ctx, transaction, route.ID, targets); err != nil {
			return err
		}
		return replaceManualAliases(ctx, transaction, route.ProjectID, route.ID, aliases)
	})
	if err != nil {
		return nil, err
	}
	return store.Route(ctx, route.ProjectID, route.ID)
}

// UpdateRoute replaces the whole target set, the name and the enabled flag of a route in one
// version-checked transaction, so a stale writer can never bring back a replaced target set.
func (store *Store) UpdateRoute(ctx context.Context, projectID, routeID, name string, targets []entity.RouteTarget, aliases []string, enabled bool, version int64) (*RouteRecord, error) {
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		existing := new(entity.Route)
		if err := transaction.NewSelect().Model(existing).
			Where("id = ? AND project_id = ? AND deleted_at IS NULL", routeID, projectID).
			For("UPDATE").Scan(ctx); err != nil {
			return missing(err)
		}
		if existing.Version != version {
			return ErrVersionConflict
		}
		if existing.Name != name {
			if err := recordRenamedAlias(ctx, transaction, existing.ProjectID, existing.ID, existing.Name); err != nil {
				return err
			}
		}
		result, err := transaction.NewUpdate().Model((*entity.Route)(nil)).
			Set("name = ?, enabled = ?, updated_at = ?, version = version + 1", name, enabled, time.Now().UTC()).
			Where("id = ? AND version = ?", routeID, version).Exec(ctx)
		if err != nil {
			if IsUniqueViolation(err) {
				return &RouteNameConflictError{Name: name}
			}
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrVersionConflict
		}
		if err := replaceTargets(ctx, transaction, routeID, targets); err != nil {
			return err
		}
		return replaceManualAliases(ctx, transaction, projectID, routeID, aliases)
	})
	if err != nil {
		return nil, err
	}
	return store.Route(ctx, projectID, routeID)
}

// DeleteRoute soft-deletes one route, removes its aliases and clears a project default that points
// at it. Targets stay so restoring the route restores its failover order.
func (store *Store) DeleteRoute(ctx context.Context, projectID, routeID string) error {
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		now := time.Now().UTC()
		result, err := transaction.NewUpdate().Model((*entity.Route)(nil)).
			Set("deleted_at = ?, enabled = false, updated_at = ?, version = version + 1", now, now).
			Where("id = ? AND project_id = ? AND deleted_at IS NULL", routeID, projectID).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrNotFound
		}
		if _, err := transaction.NewDelete().Model((*entity.RouteAlias)(nil)).
			Where("route_id = ?", routeID).Exec(ctx); err != nil {
			return err
		}
		_, err = transaction.NewUpdate().Model((*entity.Project)(nil)).
			Set("default_route_id = ''").
			Where("id = ? AND default_route_id = ?", projectID, routeID).Exec(ctx)
		return err
	})
}

func (store *Store) RestoreRoute(ctx context.Context, projectID, routeID string) (*RouteRecord, error) {
	var name string
	if err := store.DB.NewRaw(`SELECT name FROM model_routes WHERE id = ? AND project_id = ? AND deleted_at IS NOT NULL`,
		routeID, projectID).Scan(ctx, &name); err != nil {
		return nil, missing(err)
	}
	result, err := store.DB.NewUpdate().Model((*entity.Route)(nil)).
		Set("deleted_at = NULL, enabled = true, updated_at = ?, version = version + 1", time.Now().UTC()).
		Where("id = ? AND project_id = ? AND deleted_at IS NOT NULL", routeID, projectID).Exec(ctx)
	if err != nil {
		if IsUniqueViolation(err) {
			return nil, &RouteNameConflictError{Name: name}
		}
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return store.Route(ctx, projectID, routeID)
}

func (store *Store) Routes(ctx context.Context, projectID string, includeDeleted bool) ([]RouteRecord, error) {
	routes := []entity.Route{}
	query := store.DB.NewSelect().Model(&routes).Where("project_id = ?", projectID).Order("name", "id")
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}
	records := make([]RouteRecord, 0, len(routes))
	for index := range routes {
		record, err := store.loadRoute(ctx, &routes[index])
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}
	return records, nil
}

// AllRoutes lists every route of every project; the configuration health report needs the whole
// graph in one read.
func (store *Store) AllRoutes(ctx context.Context, includeDeleted bool) ([]RouteRecord, error) {
	routes := []entity.Route{}
	query := store.DB.NewSelect().Model(&routes).Order("project_id", "name", "id")
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}
	records := make([]RouteRecord, 0, len(routes))
	for index := range routes {
		record, err := store.loadRoute(ctx, &routes[index])
		if err != nil {
			return nil, err
		}
		records = append(records, *record)
	}
	return records, nil
}

func (store *Store) Route(ctx context.Context, projectID, routeID string) (*RouteRecord, error) {
	route := new(entity.Route)
	if err := store.DB.NewSelect().Model(route).
		Where("id = ? AND project_id = ?", routeID, projectID).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return store.loadRoute(ctx, route)
}

// ResolveRoute maps the model name a client sent onto an enabled route: an exact name first, then
// an alias. ErrNotFound means the client named a model this project does not serve.
func (store *Store) ResolveRoute(ctx context.Context, projectID, name string) (RouteResolution, error) {
	route := new(entity.Route)
	err := store.DB.NewSelect().Model(route).
		Where("project_id = ? AND name = ? AND enabled AND deleted_at IS NULL", projectID, name).Scan(ctx)
	if err == nil {
		targets, err := store.RouteCandidates(ctx, route.ID)
		return RouteResolution{Route: *route, Targets: targets, Resolution: ResolutionExact}, err
	}
	if !errors.Is(missing(err), ErrNotFound) {
		return RouteResolution{}, err
	}
	aliased, err := store.routeByAliasInTransaction(ctx, store.DB, projectID, name, true)
	if err != nil {
		return RouteResolution{}, err
	}
	targets, err := store.RouteCandidates(ctx, aliased.ID)
	return RouteResolution{Route: *aliased, Targets: targets, Resolution: ResolutionAlias}, err
}

// DefaultRoute resolves the project's fallback route for background calls that name no configured
// route, such as an agent asking for a title model it never set up.
func (store *Store) DefaultRoute(ctx context.Context, projectID string) (RouteResolution, error) {
	var defaultRouteID string
	if err := store.DB.NewRaw(`SELECT default_route_id FROM projects WHERE id = ?`, projectID).Scan(ctx, &defaultRouteID); err != nil {
		return RouteResolution{}, missing(err)
	}
	if defaultRouteID == "" {
		return RouteResolution{}, ErrNotFound
	}
	route := new(entity.Route)
	if err := store.DB.NewSelect().Model(route).
		Where("id = ? AND project_id = ? AND enabled AND deleted_at IS NULL", defaultRouteID, projectID).Scan(ctx); err != nil {
		return RouteResolution{}, missing(err)
	}
	targets, err := store.RouteCandidates(ctx, route.ID)
	return RouteResolution{Route: *route, Targets: targets, Resolution: ResolutionFallback}, err
}

func (store *Store) loadRoute(ctx context.Context, route *entity.Route) (*RouteRecord, error) {
	targets, err := store.RouteTargets(ctx, route.ID)
	if err != nil {
		return nil, err
	}
	aliases, err := store.RouteAliases(ctx, route.ID)
	if err != nil {
		return nil, err
	}
	return &RouteRecord{Route: *route, Targets: targets, Aliases: aliases}, nil
}

func replaceTargets(ctx context.Context, db bun.IDB, routeID string, targets []entity.RouteTarget) error {
	if _, err := db.NewDelete().Model((*entity.RouteTarget)(nil)).Where("route_id = ?", routeID).Exec(ctx); err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for index := range targets {
		targets[index].ID = secure.Random(12)
		targets[index].RouteID = routeID
		targets[index].CreatedAt = now
	}
	_, err := db.NewInsert().Model(&targets).Exec(ctx)
	return err
}

// bumpRouteVersionsOfTargets advances the optimistic version of every route whose target rows the
// caller is about to change. Without it, an UpdateRoute prepared before the target change could
// pass its stale version check and write the removed targets back.
func bumpRouteVersionsOfTargets(ctx context.Context, transaction bun.Tx, now time.Time, filter string, arguments ...any) error {
	query := `UPDATE model_routes SET version = version + 1, updated_at = ?
		WHERE id IN (SELECT DISTINCT route_id FROM route_targets WHERE ` + filter + `)`
	_, err := transaction.NewRaw(query, append([]any{now}, arguments...)...).Exec(ctx)
	return err
}
