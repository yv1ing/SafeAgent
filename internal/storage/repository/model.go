package repository

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func (store *Store) CreateModel(ctx context.Context, model *entity.Model) (*entity.Model, error) {
	model.ID = secure.Random(12)
	model.Version = 1
	model.CreatedAt = time.Now().UTC()
	if _, err := store.DB.NewInsert().Model(model).Exec(ctx); err != nil {
		return nil, err
	}
	return model, nil
}

func (store *Store) UpdateModel(ctx context.Context, id string, model entity.Model, version int64) (*entity.Model, error) {
	result, err := store.DB.NewUpdate().Model((*entity.Model)(nil)).
		Set("provider_id = ?, upstream_name = ?, display_name = ?, kind = ?, formats = ?, input_price = ?, output_price = ?, cache_read_price = ?, cache_write_price = ?, currency = ?, enabled = ?, version = version + 1",
			model.ProviderID, model.UpstreamName, model.DisplayName, model.Kind, model.Formats, model.InputPrice, model.OutputPrice, model.CacheReadPrice, model.CacheWritePrice, model.Currency, model.Enabled).
		Where("id = ? AND version = ?", id, version).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrVersionConflict
	}
	return store.Model(ctx, id)
}

func (store *Store) DeleteModel(ctx context.Context, id string) error {
	result, err := store.DB.NewDelete().Model((*entity.Model)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (store *Store) Models(ctx context.Context) ([]entity.Model, error) {
	models := []entity.Model{}
	if err := store.DB.NewSelect().Model(&models).Order("provider_id", "upstream_name").Scan(ctx); err != nil {
		return nil, err
	}
	return models, nil
}

func (store *Store) ModelsByIDs(ctx context.Context, ids []string) ([]entity.Model, error) {
	if len(ids) == 0 {
		return []entity.Model{}, nil
	}
	models := []entity.Model{}
	if err := store.DB.NewSelect().Model(&models).Where("id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	return models, nil
}

func (store *Store) Model(ctx context.Context, id string) (*entity.Model, error) {
	model := new(entity.Model)
	if err := store.DB.NewSelect().Model(model).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return model, nil
}

func (store *Store) ModelByProviderName(ctx context.Context, providerID, upstreamName string) (*entity.Model, error) {
	model := new(entity.Model)
	if err := store.DB.NewSelect().Model(model).Where("provider_id = ? AND upstream_name = ?", providerID, upstreamName).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return model, nil
}
