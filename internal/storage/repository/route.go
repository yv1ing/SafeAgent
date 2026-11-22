package repository

import (
	"context"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func (store *Store) CreateRoute(ctx context.Context, projectID, name string, targets []entity.Target) (*entity.Route, error) {
	route := &entity.Route{ID: secure.Random(12), ProjectID: projectID, Name: name, Targets: targets, Version: 1, CreatedAt: time.Now().UTC()}
	if _, err := store.DB.NewInsert().Model(route).Exec(ctx); err != nil {
		return nil, err
	}
	return route, nil
}

func (store *Store) Route(ctx context.Context, projectID, id string) (*entity.Route, error) {
	route := new(entity.Route)
	if err := store.DB.NewSelect().Model(route).Where("id = ? AND project_id = ?", id, projectID).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return route, nil
}

func (store *Store) UpdateRoute(ctx context.Context, projectID, id, name string, targets []entity.Target, version int64) (*entity.Route, error) {
	result, err := store.DB.NewUpdate().Model((*entity.Route)(nil)).
		Set("name = ?, targets = ?, version = version + 1", name, targets).
		Where("id = ? AND project_id = ? AND version = ?", id, projectID, version).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrVersionConflict
	}
	return store.Route(ctx, projectID, id)
}

func (store *Store) DeleteRoute(ctx context.Context, projectID, id string) error {
	result, err := store.DB.NewDelete().Model((*entity.Route)(nil)).Where("id = ? AND project_id = ?", id, projectID).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (store *Store) RouteByName(ctx context.Context, projectID, name string) (*entity.Route, error) {
	route := new(entity.Route)
	if err := store.DB.NewSelect().Model(route).Where("project_id = ? AND name = ?", projectID, name).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return route, nil
}

func (store *Store) Routes(ctx context.Context, projectID string) ([]entity.Route, error) {
	routes := []entity.Route{}
	if err := store.DB.NewSelect().Model(&routes).Where("project_id = ?", projectID).Order("name").Scan(ctx); err != nil {
		return nil, err
	}
	return routes, nil
}

func (store *Store) AllRoutes(ctx context.Context) ([]entity.Route, error) {
	routes := []entity.Route{}
	if err := store.DB.NewSelect().Model(&routes).Order("project_id", "name").Scan(ctx); err != nil {
		return nil, err
	}
	return routes, nil
}
