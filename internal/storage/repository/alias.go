package repository

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func (store *Store) RouteAliases(ctx context.Context, routeID string) ([]entity.RouteAlias, error) {
	aliases := []entity.RouteAlias{}
	if err := store.DB.NewSelect().Model(&aliases).Where("route_id = ?", routeID).
		Order("source", "alias").Scan(ctx); err != nil {
		return nil, err
	}
	return aliases, nil
}

// RouteByAlias resolves an additional model name. Only an enabled, non-deleted route answers.
func (store *Store) RouteByAlias(ctx context.Context, projectID, alias string) (*entity.Route, error) {
	return store.routeByAliasInTransaction(ctx, store.DB, projectID, alias, true)
}

// routeByAliasInTransaction resolves an alias without requiring the route to be enabled; callers
// that resolve for a client use ResolveRoute.
func (store *Store) routeByAliasInTransaction(ctx context.Context, db bun.IDB, projectID, alias string, enabledOnly bool) (*entity.Route, error) {
	query := `SELECT r.* FROM route_aliases ra JOIN model_routes r ON r.id = ra.route_id AND r.deleted_at IS NULL
		WHERE ra.project_id = ? AND ra.alias = ?`
	arguments := []any{projectID, alias}
	if enabledOnly {
		query += ` AND r.enabled`
	}
	route := new(entity.Route)
	if err := db.NewRaw(query, arguments...).Scan(ctx, route); err != nil {
		return nil, missing(err)
	}
	return route, nil
}

// replaceManualAliases rewrites the operator-owned aliases of a route. Renamed aliases are history
// and are left in place.
func replaceManualAliases(ctx context.Context, db bun.IDB, projectID, routeID string, aliases []string) error {
	if _, err := db.NewDelete().Model((*entity.RouteAlias)(nil)).
		Where("route_id = ? AND source = ?", routeID, entity.AliasManual).Exec(ctx); err != nil {
		return err
	}
	for _, alias := range aliases {
		row := &entity.RouteAlias{
			ProjectID: projectID, Alias: alias, RouteID: routeID,
			Source: entity.AliasManual, CreatedAt: time.Now().UTC(),
		}
		if _, err := db.NewInsert().Model(row).Exec(ctx); err != nil {
			if IsUniqueViolation(err) {
				return &RouteNameConflictError{Name: alias}
			}
			return err
		}
	}
	return nil
}

// recordRenamedAlias keeps a renamed route reachable under its old name.
func recordRenamedAlias(ctx context.Context, db bun.IDB, projectID, routeID, previousName string) error {
	if previousName == "" {
		return nil
	}
	row := &entity.RouteAlias{
		ProjectID: projectID, Alias: previousName, RouteID: routeID,
		Source: entity.AliasRenamed, CreatedAt: time.Now().UTC(),
	}
	_, err := db.NewInsert().Model(row).On("CONFLICT (project_id, alias) DO NOTHING").Exec(ctx)
	return err
}
