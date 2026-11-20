package repository

import (
	"context"
	"time"

	"github.com/yv1ing/SafeAgent/internal/storage/entity"
)

// OperationLogs lists the newest entries, keyed on (created_at, id); a zero before starts at the newest.
func (store *Store) OperationLogs(ctx context.Context, before time.Time, beforeID string, limit int) ([]entity.OperationLog, error) {
	logs := []entity.OperationLog{}
	query := store.DB.NewSelect().Model(&logs)
	if !before.IsZero() {
		query = query.Where("(created_at, id) < (?, ?)", before, beforeID)
	}
	if err := query.Order("created_at DESC", "id DESC").Limit(limit).Scan(ctx); err != nil {
		return nil, err
	}
	return logs, nil
}

// ProjectOperationLogs lists the newest entries for a specific project, keyed on (created_at, id).
func (store *Store) ProjectOperationLogs(ctx context.Context, projectID string, before time.Time, beforeID string, limit int) ([]entity.OperationLog, error) {
	logs := []entity.OperationLog{}
	query := store.DB.NewSelect().Model(&logs).Where("project_id = ?", projectID)
	if !before.IsZero() {
		query = query.Where("(created_at, id) < (?, ?)", before, beforeID)
	}
	if err := query.Order("created_at DESC", "id DESC").Limit(limit).Scan(ctx); err != nil {
		return nil, err
	}
	return logs, nil
}

func (store *Store) RecordOperation(ctx context.Context, log *entity.OperationLog) error {
	_, err := store.DB.NewInsert().Model(log).Exec(ctx)
	return err
}
