package repository

import (
	"context"
	"time"

	"github.com/yv1ing/SafeAgent/internal/platform/secure"
	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func prefix(secret string) string {
	if len(secret) <= 8 {
		return secret
	}
	return secret[:8]
}

func (store *Store) AgentKeys(ctx context.Context, projectID string) ([]entity.AgentKey, error) {
	keys := []entity.AgentKey{}
	if err := store.DB.NewSelect().Model(&keys).Where("project_id = ?", projectID).Order("created_at DESC", "id DESC").Scan(ctx); err != nil {
		return nil, err
	}
	return keys, nil
}

func (store *Store) AgentKey(ctx context.Context, projectID, id string) (*entity.AgentKey, error) {
	key := new(entity.AgentKey)
	if err := store.DB.NewSelect().Model(key).Where("id = ? AND project_id = ?", id, projectID).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return key, nil
}

func (store *Store) CreateAgentKey(ctx context.Context, projectID, label, secret string, requestsPerMinute, concurrency, trustLevel int, role string) (*entity.AgentKey, error) {
	key := &entity.AgentKey{
		ID: secure.Random(12), ProjectID: projectID, Label: label, Hash: secure.Digest(secret), Prefix: prefix(secret),
		RequestsPerMinute: requestsPerMinute, Concurrency: concurrency, TrustLevel: trustLevel, Role: role,
		Enabled: true, CreatedAt: time.Now().UTC(),
	}
	if _, err := store.DB.NewInsert().Model(key).Exec(ctx); err != nil {
		return nil, err
	}
	return key, nil
}

func (store *Store) UpdateAgentKey(ctx context.Context, projectID, id, label string, requestsPerMinute, concurrency int, trustLevel int, role string, enabled bool) (*entity.AgentKey, error) {
	result, err := store.DB.NewUpdate().Model((*entity.AgentKey)(nil)).
		Set("label = ?, requests_per_minute = ?, concurrency = ?, trust_level = ?, role = ?, enabled = ?", label, requestsPerMinute, concurrency, trustLevel, role, enabled).
		Where("id = ? AND project_id = ?", id, projectID).Exec(ctx)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return nil, ErrNotFound
	}
	return store.AgentKey(ctx, projectID, id)
}

func (store *Store) RevokeAgentKey(ctx context.Context, projectID, id string) error {
	result, err := store.DB.NewUpdate().Model((*entity.AgentKey)(nil)).
		Set("enabled = false, revoked_at = COALESCE(revoked_at, ?)", time.Now().UTC()).
		Where("id = ? AND project_id = ?", id, projectID).Exec(ctx)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchAgentKey refreshes the last-used time at most once a minute so the hot path stays a single
// conditional UPDATE.
func (store *Store) TouchAgentKey(ctx context.Context, id string) {
	now := time.Now().UTC()
	_, _ = store.DB.NewUpdate().Model((*entity.AgentKey)(nil)).
		Set("last_used_at = ?", now).
		Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)", id, now.Add(-time.Minute)).
		Exec(ctx)
}

// AgentKeyByHash resolves a presented key; disabled and revoked keys do not resolve.
func (store *Store) AgentKeyByHash(ctx context.Context, hash string) (*entity.AgentKey, error) {
	key := new(entity.AgentKey)
	if err := store.DB.NewSelect().Model(key).Where("hash = ? AND enabled AND revoked_at IS NULL", hash).Scan(ctx); err != nil {
		return nil, missing(err)
	}
	return key, nil
}
