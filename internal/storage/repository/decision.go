package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

func (store *Store) DecisionConfig(ctx context.Context, projectID string) (*entity.DecisionModelConfig, error) {
	config := new(entity.DecisionModelConfig)
	err := store.DB.NewSelect().Model(config).Where("project_id = ?", projectID).Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return config, nil
}

func (store *Store) SaveDecisionConfig(ctx context.Context, config *entity.DecisionModelConfig) error {
	config.UpdatedAt = time.Now().UTC()
	_, err := store.DB.NewInsert().Model(config).
		On("CONFLICT (project_id) DO UPDATE SET enabled = EXCLUDED.enabled, kind = EXCLUDED.kind, endpoint = EXCLUDED.endpoint, model = EXCLUDED.model, api_key = EXCLUDED.api_key, timeout_seconds = EXCLUDED.timeout_seconds, allow_private = EXCLUDED.allow_private, use_raw = EXCLUDED.use_raw, daily_token_budget = EXCLUDED.daily_token_budget, authorize_below = EXCLUDED.authorize_below, updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// ReserveDecisionTokens holds a conservative estimate of one model call before it is made. A
// project without a daily budget still records its use, so a budget introduced later starts from
// real history.
func (store *Store) ReserveDecisionTokens(ctx context.Context, projectID string, estimate, limit int64) (bool, time.Time, error) {
	now := time.Now().UTC()
	day := now.Format("2006-01-02")
	if estimate < 0 {
		return false, now, errNegativeTokens
	}
	if limit > 0 && estimate > limit {
		return false, now, nil
	}
	query := `INSERT INTO decision_model_usage (project_id, day, tokens) VALUES (?, ?, ?)
		ON CONFLICT (project_id, day) DO UPDATE SET tokens = decision_model_usage.tokens + ?
		RETURNING tokens`
	args := []any{projectID, day, estimate, estimate}
	if limit > 0 {
		query = `INSERT INTO decision_model_usage (project_id, day, tokens) VALUES (?, ?, ?)
			ON CONFLICT (project_id, day) DO UPDATE SET tokens = decision_model_usage.tokens + ?
			WHERE decision_model_usage.tokens + ? <= ?
			RETURNING tokens`
		args = append(args, estimate, limit)
	}
	result, err := store.DB.NewRaw(query, args...).Exec(ctx)
	if err != nil {
		return false, now, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, now, err
	}
	return affected > 0, now, nil
}

// SettleDecisionTokens replaces the reservation with the reported use. A failed call still records
// the estimate, because the provider may have billed the input.
func (store *Store) SettleDecisionTokens(ctx context.Context, projectID string, day time.Time, estimate, actual int64) error {
	if estimate < 0 || actual < 0 {
		return errNegativeTokens
	}
	_, err := store.DB.NewRaw(`UPDATE decision_model_usage SET tokens = GREATEST(tokens - ? + ?, 0)
		WHERE project_id = ? AND day = ?`, estimate, actual, projectID, day.UTC().Format("2006-01-02")).Exec(ctx)
	return err
}
