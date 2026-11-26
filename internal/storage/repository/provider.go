package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/uptrace/bun"
	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// providerSeedVersion is the catalog generation the built-in rows are seeded from. A reset writes
// it again so an operator can see which catalog a row was restored from.
const providerSeedVersion = 1

// ProviderSeed is one built-in provider definition. The endpoint catalog travels as its stored JSON
// form because the repository must not import the provider package.
type ProviderSeed struct {
	ID          string
	Label       string
	Endpoints   json.RawMessage
	SeedVersion int
}

type ProviderRecord struct {
	entity.Provider
	ChannelCount int
}

type providerChannelCount struct {
	ProviderID string `bun:"provider_id"`
	Count      int    `bun:"count"`
}

// Providers lists the provider catalog; soft-deleted rows are only listed on request.
func (store *Store) Providers(ctx context.Context, includeDeleted bool) ([]ProviderRecord, error) {
	providers := []entity.Provider{}
	query := store.DB.NewSelect().Model(&providers).Order("label", "id")
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	rows := []providerChannelCount{}
	if err := store.DB.NewRaw(`SELECT provider_id, count(*) AS count FROM channels
		WHERE deleted_at IS NULL GROUP BY 1`).Scan(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.ProviderID] = row.Count
	}
	records := make([]ProviderRecord, 0, len(providers))
	for _, provider := range providers {
		records = append(records, ProviderRecord{Provider: provider, ChannelCount: counts[provider.ID]})
	}
	return records, nil
}

func (store *Store) Provider(ctx context.Context, id string) (*entity.Provider, error) {
	provider := new(entity.Provider)
	if err := store.DB.NewSelect().Model(provider).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return provider, nil
}

// ProviderRecord reads one provider with its live channel count.
func (store *Store) ProviderRecord(ctx context.Context, id string) (*ProviderRecord, error) {
	provider := new(entity.Provider)
	if err := store.DB.NewSelect().Model(provider).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	var count int
	if err := store.DB.NewRaw(`SELECT count(*) FROM channels WHERE provider_id = ? AND deleted_at IS NULL`, id).Scan(ctx, &count); err != nil {
		return nil, err
	}
	return &ProviderRecord{Provider: *provider, ChannelCount: count}, nil
}

func (store *Store) CreateProvider(ctx context.Context, provider *entity.Provider) (*entity.Provider, error) {
	provider.ID = secure.Random(12)
	provider.Source = entity.ProviderCustom
	provider.SeedVersion = 0
	provider.Version = 1
	now := time.Now().UTC()
	provider.CreatedAt, provider.UpdatedAt = now, now
	provider.DeletedAt = nil
	if _, err := store.DB.NewInsert().Model(provider).Exec(ctx); err != nil {
		return nil, err
	}
	return provider, nil
}

// UpdateProvider applies an optimistic edit. Built-in rows are editable, so an operator can correct
// an endpoint without waiting for a catalog release.
func (store *Store) UpdateProvider(ctx context.Context, id string, provider entity.Provider, version int64) (*entity.Provider, error) {
	result, err := store.DB.NewUpdate().Model((*entity.Provider)(nil)).
		Set("label = ?, endpoints = ?, enabled = ?, updated_at = ?, version = version + 1",
			provider.Label, provider.Endpoints, provider.Enabled, time.Now().UTC()).
		Where("id = ? AND deleted_at IS NULL AND version = ?", id, version).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrVersionConflict
	}
	return store.Provider(ctx, id)
}

// DeleteProvider soft-deletes a custom provider. Built-in rows come from the code catalog and are
// restored by a release, so deleting one is refused instead of silently reappearing.
func (store *Store) DeleteProvider(ctx context.Context, id string) error {
	now := time.Now().UTC()
	result, err := store.DB.NewUpdate().Model((*entity.Provider)(nil)).
		Set("deleted_at = ?, enabled = false, updated_at = ?, version = version + 1", now, now).
		Where("id = ? AND deleted_at IS NULL AND source = ?", id, entity.ProviderCustom).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected > 0 {
		return nil
	}
	source, err := store.providerSource(ctx, id)
	if err != nil {
		return err
	}
	if source == entity.ProviderBuiltin {
		return ErrBuiltinProvider
	}
	return ErrNotFound
}

// ResetProvider restores a built-in row to the definition this build ships.
func (store *Store) ResetProvider(ctx context.Context, seed ProviderSeed) (*entity.Provider, error) {
	seedVersion := seed.SeedVersion
	if seedVersion <= 0 {
		seedVersion = providerSeedVersion
	}
	result, err := store.DB.NewUpdate().Model((*entity.Provider)(nil)).
		Set("label = ?, endpoints = ?, seed_version = ?, enabled = true, deleted_at = NULL, updated_at = ?, version = version + 1",
			seed.Label, seed.Endpoints, seedVersion, time.Now().UTC()).
		Where("id = ? AND source = ?", seed.ID, entity.ProviderBuiltin).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		source, err := store.providerSource(ctx, seed.ID)
		if err != nil {
			return nil, err
		}
		if source == entity.ProviderCustom {
			return nil, ErrNotBuiltinProvider
		}
		return nil, ErrNotFound
	}
	return store.Provider(ctx, seed.ID)
}

// SeedProviders inserts the built-in catalog rows this build ships. An existing row is never
// touched, so seeding cannot overwrite an operator edit.
func (store *Store) SeedProviders(ctx context.Context, seeds []ProviderSeed) error {
	if len(seeds) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make([]entity.Provider, 0, len(seeds))
	for _, seed := range seeds {
		seedVersion := seed.SeedVersion
		if seedVersion <= 0 {
			seedVersion = providerSeedVersion
		}
		rows = append(rows, entity.Provider{
			ID: seed.ID, Source: entity.ProviderBuiltin, Label: seed.Label, Endpoints: seed.Endpoints,
			SeedVersion: seedVersion, Enabled: true, Version: 1, CreatedAt: now, UpdatedAt: now,
		})
	}
	_, err := store.DB.NewInsert().Model(&rows).On("CONFLICT (id) DO NOTHING").Exec(ctx)
	return err
}

func (store *Store) providerSource(ctx context.Context, id string) (string, error) {
	var source string
	if err := store.DB.NewRaw(`SELECT source FROM providers WHERE id = ? AND deleted_at IS NULL`, id).Scan(ctx, &source); err != nil {
		if err == sql.ErrNoRows {
			return "", ErrNotFound
		}
		return "", err
	}
	return source, nil
}

// providerRows loads the providers a set of channel rows refer to.
func (store *Store) providerRows(ctx context.Context, ids []string) (map[string]entity.Provider, error) {
	providers := []entity.Provider{}
	if len(ids) == 0 {
		return map[string]entity.Provider{}, nil
	}
	if err := store.DB.NewSelect().Model(&providers).Where("id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	byID := make(map[string]entity.Provider, len(providers))
	for _, provider := range providers {
		byID[provider.ID] = provider
	}
	return byID, nil
}
