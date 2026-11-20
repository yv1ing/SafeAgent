package repository

import (
	"context"
	"errors"
	"time"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

var errNegativeTokens = errors.New("token counts must not be negative")

// ReserveTokens holds estimate tokens of today's quota until SettleTokens replaces the reservation
// with the actual use. A limit of zero or less means unlimited: the row is still written, so usage
// accumulated before a quota is enabled remains visible.
func (store *Store) ReserveTokens(ctx context.Context, projectID string, estimate, limit int64) (bool, time.Time, error) {
	now := time.Now().UTC()
	day := now.Format("2006-01-02")
	if estimate < 0 {
		return false, now, errNegativeTokens
	}
	if limit > 0 && estimate > limit {
		return false, now, nil
	}
	query := `INSERT INTO quota_usage (project_id, day, used, reserved) VALUES (?, ?, 0, ?)
		ON CONFLICT (project_id, day) DO UPDATE SET reserved = quota_usage.reserved + ?
		RETURNING reserved`
	args := []any{projectID, day, estimate, estimate}
	if limit > 0 {
		query = `INSERT INTO quota_usage (project_id, day, used, reserved) VALUES (?, ?, 0, ?)
			ON CONFLICT (project_id, day) DO UPDATE SET reserved = quota_usage.reserved + ?
			WHERE quota_usage.used + quota_usage.reserved + ? <= ?
			RETURNING reserved`
		args = append(args, estimate, limit)
	}
	reserved, err := store.DB.NewRaw(query, args...).Exec(ctx)
	if err != nil {
		return false, now, err
	}
	affected, err := reserved.RowsAffected()
	if err != nil {
		return false, now, err
	}
	return affected > 0, now, nil
}

func (store *Store) SettleTokens(ctx context.Context, projectID string, day time.Time, estimate, actual int64) error {
	if estimate < 0 || actual < 0 {
		return errNegativeTokens
	}
	_, err := store.DB.NewRaw(
		`INSERT INTO quota_usage (project_id, day, used, reserved) VALUES (?, ?, ?, 0)
		 ON CONFLICT (project_id, day) DO UPDATE SET
			used = quota_usage.used + EXCLUDED.used,
			reserved = GREATEST(quota_usage.reserved - ?, 0)`,
		projectID, day.UTC().Format("2006-01-02"), actual, estimate,
	).Exec(ctx)
	return err
}

func (store *Store) ResetReservations(ctx context.Context) error {
	_, err := store.DB.NewUpdate().Model((*entity.QuotaUsage)(nil)).Set("reserved = 0").Where("reserved > 0").Exec(ctx)
	return err
}
