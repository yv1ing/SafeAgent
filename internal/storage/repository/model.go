package repository

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// ModelFilter narrows the catalog listing. Unassociated selects rows no route target references, so
// an operator can clean up models that are stored but unused.
type ModelFilter struct {
	ProviderID     string
	Unassociated   bool
	IncludeDeleted bool
}

func (store *Store) Models(ctx context.Context, filter ModelFilter) ([]entity.Model, error) {
	models := []entity.Model{}
	query := store.DB.NewSelect().Model(&models).Order("provider_id", "upstream_name")
	if !filter.IncludeDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	if filter.ProviderID != "" {
		query = query.Where("provider_id = ?", filter.ProviderID)
	}
	if filter.Unassociated {
		query = query.Where("NOT EXISTS (SELECT 1 FROM route_targets rt WHERE rt.model_id = m.id)")
	}
	if err := query.Scan(ctx); err != nil {
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
	if err := store.DB.NewSelect().Model(model).
		Where("provider_id = ? AND upstream_name = ? AND deleted_at IS NULL", providerID, upstreamName).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return model, nil
}

func (store *Store) CreateModel(ctx context.Context, model *entity.Model) (*entity.Model, error) {
	model.ID = secure.Random(12)
	model.Version = 1
	model.CreatedAt = time.Now().UTC()
	model.UpdatedAt = model.CreatedAt
	model.DeletedAt = nil
	if _, err := store.DB.NewInsert().Model(model).Exec(ctx); err != nil {
		if IsUniqueViolation(err) {
			return nil, &ModelNameConflictError{UpstreamName: model.UpstreamName}
		}
		return nil, err
	}
	return model, nil
}

// CreateModels inserts a batch in one transaction. A duplicate stops the whole batch and names the
// upstream name that collided, because a partial import would be silently wrong.
func (store *Store) CreateModels(ctx context.Context, models []*entity.Model) error {
	now := time.Now().UTC()
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		for _, model := range models {
			model.ID = secure.Random(12)
			model.Version = 1
			model.CreatedAt, model.UpdatedAt = now, now
			model.DeletedAt = nil
			if _, err := transaction.NewInsert().Model(model).Exec(ctx); err != nil {
				if IsUniqueViolation(err) {
					return &ModelNameConflictError{UpstreamName: model.UpstreamName}
				}
				return err
			}
		}
		return nil
	})
}

func (store *Store) UpdateModel(ctx context.Context, id string, model entity.Model, version int64) (*entity.Model, error) {
	result, err := store.DB.NewUpdate().Model((*entity.Model)(nil)).
		Set("provider_id = ?, upstream_name = ?, display_name = ?, kind = ?, input_price = ?, output_price = ?, cache_read_price = ?, cache_write_price = ?, currency = ?, enabled = ?, updated_at = ?, version = version + 1",
			model.ProviderID, model.UpstreamName, model.DisplayName, model.Kind, model.InputPrice,
			model.OutputPrice, model.CacheReadPrice, model.CacheWritePrice, model.Currency,
			model.Enabled, time.Now().UTC()).
		Where("id = ? AND deleted_at IS NULL AND version = ?", id, version).Exec(ctx)
	if err != nil {
		if IsUniqueViolation(err) {
			return nil, &ModelNameConflictError{UpstreamName: model.UpstreamName}
		}
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrVersionConflict
	}
	return store.Model(ctx, id)
}

// DeleteModel soft-deletes one model. With detachTargets the route targets that point at it are
// removed; otherwise they stay and are disabled. Either way the owning routes advance their version,
// so a stale route edit cannot write the old targets back.
func (store *Store) DeleteModel(ctx context.Context, id string, detachTargets bool) error {
	return store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		now := time.Now().UTC()
		result, err := transaction.NewUpdate().Model((*entity.Model)(nil)).
			Set("deleted_at = ?, enabled = false, updated_at = ?, version = version + 1", now, now).
			Where("id = ? AND deleted_at IS NULL", id).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrNotFound
		}
		if err := bumpRouteVersionsOfTargets(ctx, transaction, now, "model_id = ?", id); err != nil {
			return err
		}
		if detachTargets {
			_, err := transaction.NewDelete().Model((*entity.RouteTarget)(nil)).Where("model_id = ?", id).Exec(ctx)
			return err
		}
		return disableTargetsOfModel(ctx, transaction, id, "model deleted")
	})
}

// RestoreModel clears the soft deletion and re-enables the targets it disabled by itself, advancing
// the owning routes' version so their target set is re-read.
func (store *Store) RestoreModel(ctx context.Context, id string) (*entity.Model, error) {
	err := store.DB.RunInTx(ctx, nil, func(ctx context.Context, transaction bun.Tx) error {
		now := time.Now().UTC()
		result, err := transaction.NewUpdate().Model((*entity.Model)(nil)).
			Set("deleted_at = NULL, enabled = true, updated_at = ?, version = version + 1", now).
			Where("id = ? AND deleted_at IS NOT NULL", id).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrNotFound
		}
		if err := bumpRouteVersionsOfTargets(ctx, transaction, now, "model_id = ? AND enabled = false AND disabled_reason = ?", id, "model deleted"); err != nil {
			return err
		}
		_, err = transaction.NewUpdate().Model((*entity.RouteTarget)(nil)).
			Set("enabled = true, disabled_reason = ''").
			Where("model_id = ? AND disabled_reason = ?", id, "model deleted").Exec(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return store.Model(ctx, id)
}

func disableTargetsOfModel(ctx context.Context, db bun.IDB, modelID, reason string) error {
	_, err := db.NewUpdate().Model((*entity.RouteTarget)(nil)).
		Set("enabled = false, disabled_reason = ?", reason).
		Where("model_id = ? AND enabled", modelID).Exec(ctx)
	return err
}
